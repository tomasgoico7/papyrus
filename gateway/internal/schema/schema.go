// Package schema knows which database schema this build of the gateway needs,
// and whether the database it is connected to has it yet.
//
// Migrations are applied by hand and the code deploys on push, so for a while
// after any release the two can disagree. This is what turns that window from
// every queued analysis failing on a missing column into the queue quietly
// standing aside until the migration lands.
package schema

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RequiredVersion is the newest migration this code was written against. A test
// holds it equal to the newest file in supabase/migrations, so adding a
// migration without raising it fails the build rather than slipping through.
const RequiredVersion = "0008"

// undefinedTable is Postgres's code for a relation that does not exist.
const undefinedTable = "42P01"

// Querier is the one method this needs from a connection or a pool.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Current returns the newest migration the database has recorded, or the empty
// string if it predates the record altogether. The versions are zero-padded, so
// the greatest string is the newest migration.
func Current(ctx context.Context, db Querier) (string, error) {
	var version *string
	err := db.QueryRow(ctx, `select max(version) from public.schema_migrations`).Scan(&version)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == undefinedTable {
			// A database from before 0008. Behind, not broken.
			return "", nil
		}
		return "", fmt.Errorf("schema: reading the applied version: %w", err)
	}
	if version == nil {
		return "", nil
	}
	return *version, nil
}

// Recorder is told whether the schema is ready each time that is checked, so
// the state is a number someone can graph rather than a log line someone has to
// be looking at.
type Recorder interface {
	SetSchemaReady(ready bool)
}

// Gate is open when the database has the schema this build needs.
type Gate struct {
	db       Querier
	required string
	interval time.Duration
	metrics  Recorder
	logger   *slog.Logger

	ready atomic.Bool
}

// NewGate builds a gate that checks db against required. It starts closed and
// stays closed until the first check says otherwise.
func NewGate(db Querier, required string, interval time.Duration, metrics Recorder, logger *slog.Logger) *Gate {
	return &Gate{db: db, required: required, interval: interval, metrics: metrics, logger: logger}
}

// Ready reports whether the last check found the schema in place.
func (g *Gate) Ready() bool { return g.ready.Load() }

// Run checks now and then on every interval until ctx ends.
//
// It keeps checking after the schema is found in place, not only while it is
// missing. A database restored from an older backup goes backwards, and a gate
// that stopped looking would stay open over a schema that no longer matches.
func (g *Gate) Run(ctx context.Context) {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()

	for {
		g.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check reads the applied version once and opens or closes the gate to match.
// A check that cannot reach the database leaves the gate as it was: an outage
// is not evidence about the schema either way.
func (g *Gate) Check(ctx context.Context) {
	have, err := Current(ctx, g.db)
	if err != nil {
		if ctx.Err() == nil {
			g.logger.Warn("could not check the database schema", slog.Any("error", err))
		}
		return
	}

	ready := have >= g.required
	was := g.ready.Swap(ready)
	g.metrics.SetSchemaReady(ready)

	switch {
	case ready && !was:
		g.logger.Info("database schema is in place; the queue is on",
			slog.String("have", have), slog.String("need", g.required))
	case !ready:
		// Every check while behind, not just the first: this is the line that
		// tells whoever is reading the log that a migration is waiting for them,
		// and it should not scroll away.
		g.logger.Error("database schema is behind; the queue is off until it is migrated",
			slog.String("have", have), slog.String("need", g.required))
	}
}
