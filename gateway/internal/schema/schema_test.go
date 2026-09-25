package schema_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/papyrus/gateway/internal/schema"
	"github.com/papyrus/gateway/internal/schema/schematest"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()

	var stop func()
	var err error
	pool, stop, err = schematest.Start(ctx)
	if err != nil {
		// Locally a missing Docker should skip rather than fail; on CI it is a
		// real failure, because there the container is always available.
		if os.Getenv("CI") != "" {
			fmt.Fprintf(os.Stderr, "schema: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "skipping schema tests: %v\n", err)
		os.Exit(0)
	}

	code := m.Run()
	stop()
	os.Exit(code)
}

// TestEveryMigrationApplies is what lets a migration be trusted before it is
// pasted into the Supabase SQL editor. TestMain already applied them all in
// order; reaching this point means none failed. What is checked here is that
// the result is the schema the application expects to find.
func TestEveryMigrationApplies(t *testing.T) {
	for _, table := range []string{"profiles", "cvs", "analyses", "analysis_jobs"} {
		var exists bool
		err := pool.QueryRow(context.Background(),
			`select exists (select from information_schema.tables
			                where table_schema = 'public' and table_name = $1)`, table,
		).Scan(&exists)
		if err != nil {
			t.Fatalf("checking %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s is missing after every migration ran", table)
		}
	}
}

// TestMigrationsCanBeAppliedTwice holds the migrations to what the way they are
// applied demands. They go in by hand, through a SQL editor, and a migration
// pasted a second time — because nobody was sure the first run took — has to be
// harmless. One that is not fails there, on the live database, halfway through.
func TestMigrationsCanBeAppliedTwice(t *testing.T) {
	if err := schematest.Apply(context.Background(), pool); err != nil {
		t.Fatalf("re-applying every migration over an up-to-date schema: %v", err)
	}
}

// TestASignupProvisionsAProfile exercises the one trigger the migrations put on
// a table Supabase owns. It is the path every real user goes through first, and
// the reason a test that inserts users has to give them an email.
func TestASignupProvisionsAProfile(t *testing.T) {
	ctx := context.Background()

	var id string
	err := pool.QueryRow(ctx,
		`insert into auth.users (email, raw_user_meta_data)
		 values ('someone@example.com', '{"full_name": "Some One"}')
		 returning id`,
	).Scan(&id)
	if err != nil {
		t.Fatalf("creating a user: %v", err)
	}

	var email, name string
	err = pool.QueryRow(ctx,
		`select email, full_name from public.profiles where id = $1`, id,
	).Scan(&email, &name)
	if err != nil {
		t.Fatalf("the trigger did not provision a profile: %v", err)
	}
	if email != "someone@example.com" || name != "Some One" {
		t.Errorf("profile = (%q, %q), want it copied from the signup", email, name)
	}
}

// newestMigration is the version of the last file in supabase/migrations.
func newestMigration(t *testing.T) string {
	t.Helper()
	files, err := schematest.Migrations()
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	return versionOf(filepath.Base(files[len(files)-1]))
}

func versionOf(file string) string {
	version, _, _ := strings.Cut(file, "_")
	return version
}

// TestRequiredVersionMatchesTheNewestMigration is the half of the convention
// that lives in Go. Adding a migration without raising RequiredVersion would
// let the code go out believing an older schema is enough, and the gate would
// open over a database missing what the new code reads.
func TestRequiredVersionMatchesTheNewestMigration(t *testing.T) {
	if newest := newestMigration(t); schema.RequiredVersion != newest {
		t.Errorf("RequiredVersion = %q, but the newest migration is %q; raise it with the migration",
			schema.RequiredVersion, newest)
	}
}

// TestEveryMigrationRecordsItself is the half that lives in SQL. A migration
// that forgets to insert its own version applies cleanly and leaves the record
// behind, so the gate stays shut on a database that is in fact up to date. That
// failure is silent and permanent, which is why it is checked here rather than
// left to memory.
func TestEveryMigrationRecordsItself(t *testing.T) {
	files, err := schematest.Migrations()
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	for _, file := range files {
		version := versionOf(filepath.Base(file))
		// The record starts with 0008; everything earlier is backfilled by it.
		if version < "0008" {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", filepath.Base(file), err)
		}
		want := fmt.Sprintf("insert into public.schema_migrations (version) values ('%s')", version)
		if !strings.Contains(string(body), want) {
			t.Errorf("%s does not record itself; end it with:\n  %s\n  on conflict (version) do nothing;",
				filepath.Base(file), want)
		}
	}
}

func TestCurrentReportsTheNewestAppliedMigration(t *testing.T) {
	have, err := schema.Current(context.Background(), pool)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if want := newestMigration(t); have != want {
		t.Errorf("current = %q after applying every migration, want %q", have, want)
	}
}

func TestCurrentOnADatabaseFromBeforeTheRecord(t *testing.T) {
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// What production looked like until 0008: no record at all. That has to
	// read as "behind", which it is, and not as an error that leaves the gate
	// guessing.
	if _, err := tx.Exec(ctx, "drop table public.schema_migrations"); err != nil {
		t.Fatalf("drop: %v", err)
	}

	have, err := schema.Current(ctx, tx)
	if err != nil {
		t.Fatalf("a database from before the record should not be an error: %v", err)
	}
	if have != "" {
		t.Errorf("current = %q with no record, want empty", have)
	}
}

type recordedReadiness struct{ last, set bool }

func (r *recordedReadiness) SetSchemaReady(ready bool) { r.last, r.set = ready, true }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestTheGateFollowsTheSchemaBothWays checks the two transitions that matter:
// opening when the migration lands, without a restart, and closing again if
// the database goes backwards — a restore from an older backup does exactly
// that, and a gate that stopped looking would stay open over the wrong schema.
func TestTheGateFollowsTheSchemaBothWays(t *testing.T) {
	ctx := context.Background()
	// A release one migration ahead of this database.
	ahead := "9990"
	recorder := &recordedReadiness{}
	gate := schema.NewGate(pool, ahead, time.Hour, recorder, quiet())

	gate.Check(ctx)
	if gate.Ready() {
		t.Fatal("the gate opened for a schema the database does not have")
	}
	if !recorder.set || recorder.last {
		t.Error("the closed state was not recorded")
	}

	// The migration is applied.
	if _, err := pool.Exec(ctx, "insert into public.schema_migrations (version) values ($1)", ahead); err != nil {
		t.Fatalf("recording: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "delete from public.schema_migrations where version = $1", ahead)
	}()

	gate.Check(ctx)
	if !gate.Ready() {
		t.Fatal("the gate stayed shut after the migration landed; it should open without a restart")
	}

	// And the database goes backwards.
	if _, err := pool.Exec(ctx, "delete from public.schema_migrations where version = $1", ahead); err != nil {
		t.Fatalf("removing: %v", err)
	}
	gate.Check(ctx)
	if gate.Ready() {
		t.Error("the gate stayed open over a schema that went backwards")
	}
}

// flaky answers through the real pool until it is taken down.
type flaky struct {
	real schema.Querier
	down atomic.Bool
}

func (f *flaky) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.down.Load() {
		return failingRow{}
	}
	return f.real.QueryRow(ctx, sql, args...)
}

type failingRow struct{}

func (failingRow) Scan(...any) error { return errors.New("connection refused") }

// TestAnOutageLeavesTheGateAsItWas separates "the database is down" from "the
// schema is behind". Only the second is a reason to shut the queue; closing it
// on the first would turn a blip into lost queued analyses for no gain. It has
// to be the same gate, open, that meets the outage — a fresh one starts shut,
// and staying shut would prove nothing.
func TestAnOutageLeavesTheGateAsItWas(t *testing.T) {
	ctx := context.Background()
	db := &flaky{real: pool}
	gate := schema.NewGate(db, schema.RequiredVersion, time.Hour, &recordedReadiness{}, quiet())

	gate.Check(ctx)
	if !gate.Ready() {
		t.Fatal("setup: the gate should open over an up-to-date schema")
	}

	db.down.Store(true)
	gate.Check(ctx)
	if !gate.Ready() {
		t.Error("an outage closed an open gate; it says nothing about the schema")
	}
}
