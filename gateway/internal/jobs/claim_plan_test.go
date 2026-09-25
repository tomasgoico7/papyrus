package jobs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/jobs"
)

// seedHistory fills the queue the way a real one fills up: almost all of it
// finished work, kept until the retention sweep, with a thin layer of live jobs
// on top. The claim only ever wants the live layer, so the question is whether
// it can find it without reading everything underneath.
func seedHistory(t *testing.T, rows int) {
	t.Helper()
	ctx := context.Background()

	// One statement, so a hundred thousand rows take a second rather than a
	// minute. Every two hundredth row is running, half of those stale; three in
	// two hundred are queued, some not due yet; twelve are dead letters; the rest
	// are done. Everything falls inside the retention windows, spread over the
	// last twenty two hours: a table in steady state, where the purge has already
	// taken what expired and finds almost nothing left to remove.
	_, err := pool.Exec(ctx, `
		insert into analysis_jobs (
			user_id, dedup_key, state, attempts, max_attempts,
			run_after, claimed_at, cv, cv_filename, job_offer,
			result, error_code, finished_at, created_at, updated_at
		)
		select
			$1::uuid,
			'seed-' || i,
			case
				when i % 200 = 0            then 'running'
				when i % 200 between 1 and 3  then 'queued'
				when i % 200 between 4 and 15 then 'failed'
				else 'done'
			end,
			case when i % 200 = 0 then 1 else 0 end,
			3,
			case
				when i % 200 = 1 then now() + interval '10 minutes'
				else now() - (i * interval '0.8 seconds')
			end,
			case
				when i % 200 = 0 and i % 400 = 0 then now() - interval '20 minutes'
				when i % 200 = 0                  then now() - interval '10 seconds'
			end,
			case when i % 200 between 0 and 15 then '\x255044462d'::bytea end,
			'cv.pdf',
			'A backend role.',
			case when i % 200 > 15 then '{}'::jsonb end,
			case when i % 200 between 4 and 15 then 'upstream_timeout' end,
			case when i % 200 > 3 then now() - (i * interval '0.8 seconds') end,
			now() - (i * interval '0.8 seconds'),
			now() - (i * interval '0.8 seconds')
		from generate_series(1, $2) as i`, testerA, rows)
	if err != nil {
		t.Fatalf("seeding %d rows: %v", rows, err)
	}

	// Without fresh statistics the planner guesses at the table's shape, and a
	// plan chosen from a guess says nothing about the plan production gets.
	if _, err := pool.Exec(ctx, "analyze analysis_jobs"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
}

// planNode is the part of an EXPLAIN (FORMAT JSON) node worth looking at.
type planNode struct {
	NodeType     string     `json:"Node Type"`
	RelationName string     `json:"Relation Name"`
	IndexName    string     `json:"Index Name"`
	Filter       string     `json:"Filter"`
	ActualRows   float64    `json:"Actual Rows"`
	RowsRemoved  float64    `json:"Rows Removed by Filter"`
	Plans        []planNode `json:"Plans"`
}

type explained struct {
	Plan          planNode `json:"Plan"`
	ExecutionTime float64  `json:"Execution Time"`
	PlanningTime  float64  `json:"Planning Time"`
}

// explain runs a statement under EXPLAIN ANALYZE inside a transaction that is
// rolled back, so the table is the same afterwards as before — which matters
// for the ones that update or delete.
func explain(t *testing.T, sql string, args ...any) explained {
	t.Helper()
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var raw []byte
	if err := tx.QueryRow(ctx, "explain (analyze, buffers, format json) "+sql, args...).Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}

	var plans []explained
	if err := json.Unmarshal(raw, &plans); err != nil {
		t.Fatalf("decoding the plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one plan, got %d", len(plans))
	}
	return plans[0]
}

// render prints a plan tree one node per line, for reading in the test log.
func render(node planNode, depth int, out *strings.Builder) {
	fmt.Fprintf(out, "%s%s", strings.Repeat("  ", depth), node.NodeType)
	if node.RelationName != "" {
		fmt.Fprintf(out, " on %s", node.RelationName)
	}
	if node.IndexName != "" {
		fmt.Fprintf(out, " using %s", node.IndexName)
	}
	fmt.Fprintf(out, "  (rows=%.0f", node.ActualRows)
	if node.RowsRemoved > 0 {
		fmt.Fprintf(out, ", removed by filter=%.0f", node.RowsRemoved)
	}
	out.WriteString(")\n")
	for _, child := range node.Plans {
		render(child, depth+1, out)
	}
}

// scansOf walks a plan and returns every node that reads the queue table.
func scansOf(node planNode) []planNode {
	var found []planNode
	if node.RelationName == "analysis_jobs" {
		found = append(found, node)
	}
	for _, child := range node.Plans {
		found = append(found, scansOf(child)...)
	}
	return found
}

// rowsRead is what a scan node actually touched: what it returned plus what it
// looked at and threw away.
func rowsRead(scans []planNode) float64 {
	var total float64
	for _, scan := range scans {
		total += scan.ActualRows + scan.RowsRemoved
	}
	return total
}

const seededRows = 100_000

// TestTheQueueQueriesDoNotReadTheWholeTable holds every statement the worker
// runs on a timer to one rule: its cost follows the work in front of it, not
// the history behind it. Measured before the finished index existed, the depth
// gauge read all hundred thousand rows every fifteen seconds and the retention
// sweep read them all to delete nothing.
//
// It checks the property rather than naming indexes, so a reasonable change to
// the schema does not break it — only a change that sends one of these back to
// reading everything does.
func TestTheQueueQueriesDoNotReadTheWholeTable(t *testing.T) {
	newStore(t)
	seedHistory(t, seededRows)

	stale := (5 * time.Minute).Seconds()
	for _, q := range []struct {
		name string
		sql  string
		args []any
	}{
		{"claim", jobs.ClaimSQL, []any{stale}},
		{"measure", jobs.MeasureSQL, nil},
		{"fail abandoned", jobs.FailAbandonedSQL, []any{stale}},
		{"purge finished", jobs.PurgeFinishedSQL, []any{(24 * time.Hour).Seconds(), (7 * 24 * time.Hour).Seconds()}},
	} {
		t.Run(q.name, func(t *testing.T) {
			plan := explain(t, q.sql, q.args...)

			var tree strings.Builder
			render(plan.Plan, 1, &tree)
			t.Logf("execution %.3f ms", plan.ExecutionTime)

			for _, scan := range scansOf(plan.Plan) {
				if scan.NodeType == "Seq Scan" {
					t.Errorf("reads the whole table:%s", "\n"+tree.String())
				}
			}
		})
	}
}

// TestAClaimReadsTheLiveJobsAndNotTheHistory is the property the queue rests
// on. A worker claims every couple of seconds while idle; if that meant reading
// every finished job still inside its retention window, the queue would slow
// down as it was used, for no reason to do with the work waiting in it.
//
// For a while this held by accident. The claim was served by the dedup index,
// whose predicate happened to cover both the queued jobs and the stale running
// ones the claim takes back, and widening that index for a reason of its own
// sent the claim to reading all hundred thousand rows. It now has an index of
// its own, ordered the way it reads, and stops at the first row it can take.
// This asserts what is read rather than which index reads it, so it holds
// across any reasonable change to either.
func TestAClaimReadsTheLiveJobsAndNotTheHistory(t *testing.T) {
	newStore(t)
	seedHistory(t, seededRows)

	plan := explain(t, jobs.ClaimSQL, (5 * time.Minute).Seconds())
	read := rowsRead(scansOf(plan.Plan))

	var tree strings.Builder
	render(plan.Plan, 1, &tree)

	// Two in a hundred rows are live in the seed. Allowing five in a hundred
	// leaves room for the planner to be imprecise, and none for a scan of the
	// finished rows beneath them.
	if limit := seededRows * 0.05; read > limit {
		t.Errorf("a claim read %.0f rows of %d; it should touch the live jobs only, under %.0f:%s",
			read, seededRows, limit, "\n"+tree.String())
	}
	t.Logf("claim read %.0f of %d rows in %.3f ms\n%s", read, seededRows, plan.ExecutionTime, tree.String())
}
