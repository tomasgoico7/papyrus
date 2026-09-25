// Package schematest stands up the production schema in a throwaway Postgres
// for tests that need a real database.
//
// It exists so that no test builds its schema by hand. Two did, and both drifted
// from production in ways that let them pass while testing the wrong thing: one
// ran under pgx's default query mode where production runs exec mode, the other
// applied only the queue's migrations and never met the trigger on auth.users
// that every real signup fires.
package schematest

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

//go:embed supabase_stubs.sql
var supabaseStubs string

// Start runs Postgres in a container, connects to it the way the deployed pool
// connects, and applies the stubs and every migration in order.
//
// The returned function stops the container. An error from Start usually means
// Docker is not available; whether that should skip or fail is the caller's
// decision, because it differs between a laptop and CI.
func Start(ctx context.Context) (*pgxpool.Pool, func(), error) {
	container, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("papyrus"),
		tcpostgres.WithUsername("papyrus"),
		tcpostgres.WithPassword("papyrus"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("starting postgres: %w", err)
	}
	stop := func() { _ = testcontainers.TerminateContainer(container) }

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		stop()
		return nil, nil, fmt.Errorf("connection string: %w", err)
	}

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		stop()
		return nil, nil, fmt.Errorf("parsing dsn: %w", err)
	}
	// The mode production is forced into by the transaction pooler. A statement
	// that works under the default mode can fail under this one, and a test pool
	// that differs from the deployed one cannot see it.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		stop()
		return nil, nil, fmt.Errorf("connecting: %w", err)
	}

	if err := Apply(ctx, pool); err != nil {
		pool.Close()
		stop()
		return nil, nil, err
	}

	return pool, func() { pool.Close(); stop() }, nil
}

// Apply runs the stubs and then every migration, in the order they were written.
// It stops at the first failure and names the file that caused it.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, supabaseStubs); err != nil {
		return fmt.Errorf("applying supabase stubs: %w", err)
	}

	files, err := Migrations()
	if err != nil {
		return err
	}
	for _, file := range files {
		migration, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filepath.Base(file), err)
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			return fmt.Errorf("applying %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

// Migrations lists the migration files in the order they apply. The names are
// zero-padded, so lexical order is the order they were written in.
func Migrations() ([]string, error) {
	dir, err := migrationsDir()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("listing migrations: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no migrations found in %s", dir)
	}
	sort.Strings(files)
	return files, nil
}

// migrationsDir finds supabase/migrations from this file's own location rather
// than the working directory, which differs for every package that imports
// this one.
func migrationsDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("locating the schematest package on disk")
	}
	dir := filepath.Join(filepath.Dir(here), "..", "..", "..", "..", "supabase", "migrations")
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("finding the migrations directory: %w", err)
	}
	return filepath.Clean(dir), nil
}
