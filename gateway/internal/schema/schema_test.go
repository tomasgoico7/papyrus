package schema_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

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
