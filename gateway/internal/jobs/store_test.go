package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/papyrus/gateway/internal/jobs"
)

// These run against a real Postgres. `for update skip locked` is the whole
// point of this package, and no fake reproduces it — a test that did not use
// the real engine would only be testing the fake.
var (
	pool    *pgxpool.Pool
	testerA = "11111111-1111-1111-1111-111111111111"
	testerB = "22222222-2222-2222-2222-222222222222"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

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
		// Locally a missing Docker should skip rather than fail; on CI it is a
		// real failure, because there the container is always available.
		if os.Getenv("CI") != "" {
			fmt.Fprintf(os.Stderr, "starting postgres: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "skipping jobs store tests, no docker: %v\n", err)
		os.Exit(0)
	}
	defer func() { _ = testcontainers.TerminateContainer(container) }()

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		fmt.Fprintf(os.Stderr, "connection string: %v\n", err)
		os.Exit(1)
	}

	// Built the way the deployed pool is built, including the query mode. The
	// point of testing against a real engine is lost if the connection is
	// configured differently from the one that runs in production — a statement
	// that works under pgx's default mode can fail under exec mode, and that
	// difference is invisible to a test pool that uses the default.
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parsing dsn: %v\n", err)
		os.Exit(1)
	}
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec

	pool, err = pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connecting: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := applySchema(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "applying schema: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// applySchema runs the real migrations, not a hand-written copy of them: a test
// schema that drifts from the deployed one tests the wrong thing. Only
// `auth.users`, which Supabase provides, is stubbed.
//
// The migrations are discovered rather than named. Naming one meant that adding
// a column to the queue left every test in this file failing against a schema
// from before it — which is the drift the paragraph above warns about, arrived
// at by hand. Anything touching this table is picked up now; the migrations that
// do not are skipped, because they reach into Supabase's storage schema and
// stubbing that would be a lot of scaffolding for a table these tests never
// read.
func applySchema(ctx context.Context) error {
	const authStub = `
		create schema if not exists auth;
		create table if not exists auth.users (id uuid primary key);`

	if _, err := pool.Exec(ctx, authStub); err != nil {
		return fmt.Errorf("auth stub: %w", err)
	}

	dir := filepath.Join("..", "..", "..", "supabase", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("listing migrations: %w", err)
	}
	// Zero-padded names, so lexical order is the order they were written in.
	sort.Strings(files)

	applied := 0
	for _, file := range files {
		migration, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("reading %s: %w", filepath.Base(file), err)
		}
		if !strings.Contains(string(migration), "analysis_jobs") {
			continue
		}
		if _, err := pool.Exec(ctx, string(migration)); err != nil {
			return fmt.Errorf("running %s: %w", filepath.Base(file), err)
		}
		applied++
	}
	if applied == 0 {
		return fmt.Errorf("no migrations for analysis_jobs found in %s", dir)
	}

	for _, id := range []string{testerA, testerB} {
		if _, err := pool.Exec(ctx, "insert into auth.users (id) values ($1) on conflict do nothing", id); err != nil {
			return fmt.Errorf("seeding user: %w", err)
		}
	}
	return nil
}

func newStore(t *testing.T) *jobs.Store {
	t.Helper()
	if _, err := pool.Exec(context.Background(), "truncate analysis_jobs"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return jobs.NewStore(pool)
}

func newJob(key string) jobs.NewJob {
	return jobs.NewJob{
		UserID:     testerA,
		DedupKey:   key,
		CV:         []byte("%PDF-1.4 the bytes"),
		CVFilename: "cv.pdf",
		JobOffer:   "A backend role.",
		JobTitle:   "Backend Engineer",
	}
}

func TestEnqueueCreatesAQueuedJob(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, created, err := store.Enqueue(ctx, newJob("k1"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if !created {
		t.Error("the first enqueue for a key should report that it created the job")
	}
	if job.State != jobs.StateQueued {
		t.Errorf("state = %q, want queued", job.State)
	}
	if job.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 before any claim", job.Attempts)
	}
	if job.ID == "" {
		t.Error("expected an id")
	}
}

func TestEnqueueJoinsALiveJobInsteadOfDuplicating(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	first, _, err := store.Enqueue(ctx, newJob("same"))
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}

	second, created, err := store.Enqueue(ctx, newJob("same"))
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}

	if created {
		t.Error("the second submit should have joined the live job, not created one")
	}
	if second.ID != first.ID {
		t.Errorf("ids differ: %s vs %s", first.ID, second.ID)
	}
}

func TestEnqueueAllowsARerunOnceTheFirstHasFinished(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	first, _, _ := store.Enqueue(ctx, newJob("rerun"))
	claimed, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, claimed.ID, json.RawMessage(`{"score":80}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// The dedup index only covers live jobs, so the same analysis can be asked
	// for again later.
	second, created, err := store.Enqueue(ctx, newJob("rerun"))
	if err != nil {
		t.Fatalf("second enqueue: %v", err)
	}
	if !created || second.ID == first.ID {
		t.Error("a finished job should not block a fresh one for the same inputs")
	}
}

func TestClaimMarksTheJobRunningAndCarriesTheUpload(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("k1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	if job.State != jobs.StateRunning {
		t.Errorf("state = %q, want running", job.State)
	}
	if job.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", job.Attempts)
	}
	if string(job.CV) != "%PDF-1.4 the bytes" {
		t.Errorf("cv = %q, want the upload to reach the worker", job.CV)
	}
	if job.JobTitle != "Backend Engineer" {
		t.Errorf("job title = %q", job.JobTitle)
	}
}

func TestClaimReportsAnEmptyQueue(t *testing.T) {
	store := newStore(t)

	if _, err := store.Claim(context.Background(), time.Minute); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound on an empty queue", err)
	}
}

func TestClaimSkipsAJobHeldForLater(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, _, _ := store.Enqueue(ctx, newJob("later"))
	claimed, _ := store.Claim(ctx, time.Minute)
	if err := store.Retry(ctx, claimed.ID, time.Now().Add(time.Hour), "boom", "upstream failed"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if _, err := store.Claim(ctx, time.Minute); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("a job backed off until later must not be claimable now (job %s)", job.ID)
	}
}

func TestClaimReclaimsAJobAbandonedByADeadWorker(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("abandoned")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	first, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// The worker dies here: the row stays 'running' with nobody working on it.
	// A zero staleness window is the same as "that claim is old enough".
	second, err := store.Claim(ctx, 0)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("reclaimed %s, want the abandoned job %s", second.ID, first.ID)
	}
	if second.Attempts != 2 {
		t.Errorf("attempts = %d, want the reclaim to count as another attempt", second.Attempts)
	}
}

// TestConcurrentClaimsNeverHandOutTheSameJob is the reason this package exists.
// Without `skip locked` the workers would either serialise behind one lock or,
// worse, two of them would run the same analysis.
func TestConcurrentClaimsNeverHandOutTheSameJob(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	const total = 20
	for i := range total {
		if _, _, err := store.Enqueue(ctx, newJob(fmt.Sprintf("job-%d", i))); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	const workers = 8
	var (
		mu      sync.Mutex
		claimed = make(map[string]int)
		wg      sync.WaitGroup
	)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				job, err := store.Claim(ctx, time.Minute)
				if errors.Is(err, jobs.ErrNotFound) {
					return
				}
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				mu.Lock()
				claimed[job.ID]++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claimed) != total {
		t.Errorf("claimed %d distinct jobs, want %d", len(claimed), total)
	}
	for id, times := range claimed {
		if times != 1 {
			t.Errorf("job %s was claimed %d times; every job must go to exactly one worker", id, times)
		}
	}
}

func TestCompleteStoresTheResultAndDropsTheUpload(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("k1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, _ := store.Claim(ctx, time.Minute)

	if err := store.Complete(ctx, claimed.ID, json.RawMessage(`{"score":80}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	job, err := store.Get(ctx, claimed.ID, testerA)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.State != jobs.StateDone {
		t.Errorf("state = %q, want done", job.State)
	}
	if string(job.Result) != `{"score": 80}` && string(job.Result) != `{"score":80}` {
		t.Errorf("result = %s", job.Result)
	}
	if job.FinishedAt == nil {
		t.Error("expected finished_at to be set")
	}
	assertUploadDropped(t, claimed.ID)
}

func TestFailEndsTheJobButKeepsTheUpload(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("k1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, _ := store.Claim(ctx, time.Minute)

	if err := store.Fail(ctx, claimed.ID, "unreadable_cv", "The CV is not readable."); err != nil {
		t.Fatalf("fail: %v", err)
	}

	job, _ := store.Get(ctx, claimed.ID, testerA)
	if job.State != jobs.StateFailed {
		t.Errorf("state = %q, want failed", job.State)
	}
	if job.ErrorCode != "unreadable_cv" {
		t.Errorf("error code = %q", job.ErrorCode)
	}
	// Kept on purpose: a dead letter nobody can re-run is not much of a queue.
	assertUploadKept(t, claimed.ID)
}

func TestRetryReturnsTheJobToTheQueue(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("k1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, _ := store.Claim(ctx, time.Minute)

	if err := store.Retry(ctx, claimed.ID, time.Now().Add(-time.Second), "upstream_timeout", "took too long"); err != nil {
		t.Fatalf("retry: %v", err)
	}

	again, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("a job due again should be claimable: %v", err)
	}
	if again.ID != claimed.ID || again.Attempts != 2 {
		t.Errorf("claimed %s attempt %d, want %s attempt 2", again.ID, again.Attempts, claimed.ID)
	}
	// The upload has to survive a retry, or the next attempt has nothing to run.
	if len(again.CV) == 0 {
		t.Error("the upload must be kept while the job can still run")
	}
}

func TestTerminalOperationsRejectAJobSomebodyElseOwns(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("k1")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, _ := store.Claim(ctx, time.Minute)
	if err := store.Complete(ctx, claimed.ID, json.RawMessage(`{}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// A worker that comes back after its job was reclaimed and finished must not
	// be able to overwrite the outcome.
	if err := store.Complete(ctx, claimed.ID, json.RawMessage(`{"stale":true}`)); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound when the job is no longer running", err)
	}
}

func TestGetIsScopedToItsOwner(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, _, err := store.Enqueue(ctx, newJob("k1"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := store.Get(ctx, job.ID, testerA); err != nil {
		t.Fatalf("the owner should be able to read it: %v", err)
	}

	// Not "forbidden": telling one user that another user's id exists is itself
	// a leak.
	if _, err := store.Get(ctx, job.ID, testerB); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound for a job owned by someone else", err)
	}
}

func TestGetReportsAnUnknownID(t *testing.T) {
	store := newStore(t)
	unknown := "33333333-3333-3333-3333-333333333333"

	if _, err := store.Get(context.Background(), unknown, testerA); !errors.Is(err, jobs.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestMeasureCountsWaitingRunningAndDead(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	for i := range 4 {
		if _, _, err := store.Enqueue(ctx, newJob(fmt.Sprintf("d-%d", i))); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
	}

	running, _ := store.Claim(ctx, time.Minute)
	dead, _ := store.Claim(ctx, time.Minute)
	if err := store.Fail(ctx, dead.ID, "unreadable_cv", "no"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	depth, err := store.Measure(ctx)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	if depth.Queued != 2 || depth.Running != 1 || depth.Dead != 1 {
		t.Errorf("depth = %+v, want 2 queued, 1 running, 1 dead (running job %s)", depth, running.ID)
	}
}

func TestPurgeKeepsLiveJobsAndDeadLettersLongerThanSuccesses(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// One of each: finished, given up on, and still queued. The order matters —
	// Claim takes the oldest job that is due, so the one meant to stay queued is
	// enqueued last, after there is nothing left to claim.
	if _, _, err := store.Enqueue(ctx, newJob("finished")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	done, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Complete(ctx, done.ID, json.RawMessage(`{"score":80}`)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if _, _, err := store.Enqueue(ctx, newJob("gave-up")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	failed, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := store.Fail(ctx, failed.ID, "unreadable_cv", "no"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	queued, _, err := store.Enqueue(ctx, newJob("still-waiting"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Both finished a moment ago, so a retention of an hour removes neither.
	removed, err := store.PurgeFinished(ctx, time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 0 {
		t.Errorf("removed %d rows, want none inside the retention window", removed)
	}

	// Now with the successful one out of its window and the dead one still in.
	removed, err = store.PurgeFinished(ctx, 0, time.Hour)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d rows, want just the finished one", removed)
	}

	if _, err := store.Get(ctx, done.ID, testerA); !errors.Is(err, jobs.ErrNotFound) {
		t.Error("the finished job should have been swept away")
	}
	if _, err := store.Get(ctx, failed.ID, testerA); err != nil {
		t.Error("a dead letter is the one somebody wants to look at; it should outlive a success")
	}
	// A job that never finished has no business being deleted by a retention
	// sweep, whatever its age.
	if _, err := store.Get(ctx, queued.ID, testerA); err != nil {
		t.Error("a job still waiting to run must never be purged")
	}
}

func assertUploadKept(t *testing.T, id string) {
	t.Helper()
	if uploadSize(t, id) == nil {
		t.Error("the upload was dropped; a dead letter has to stay re-runnable")
	}
}

func assertUploadDropped(t *testing.T, id string) {
	t.Helper()
	if size := uploadSize(t, id); size != nil {
		t.Errorf("the upload is still stored (%d bytes) after the job succeeded", *size)
	}
}

func uploadSize(t *testing.T, id string) *int {
	t.Helper()
	var size *int
	if err := pool.QueryRow(context.Background(),
		"select octet_length(cv) from analysis_jobs where id = $1", id,
	).Scan(&size); err != nil {
		t.Fatalf("reading cv size: %v", err)
	}
	return size
}

// TestClaimStopsReclaimingOnceTheAttemptsAreSpent covers the half of the ceiling
// the worker cannot enforce. The worker gives up after the last attempt fails,
// but a worker that is killed mid-attempt never gets to decide anything. Without
// a ceiling here the job is handed out again on every sweep: one reached seven
// attempts against a limit of three across a run of deploys.
func TestClaimStopsReclaimingOnceTheAttemptsAreSpent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("spent")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	const maxAttempts = 3
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// A zero window means every claim looks abandoned, so each pass is a
		// worker dying without recording anything.
		job, err := store.Claim(ctx, 0)
		if err != nil {
			t.Fatalf("claim %d: %v", attempt, err)
		}
		if job.Attempts != attempt {
			t.Fatalf("attempts = %d on claim %d", job.Attempts, attempt)
		}
	}

	_, err := store.Claim(ctx, 0)
	if !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("claim past the ceiling returned %v, want ErrNotFound", err)
	}
}

func TestFailAbandonedClosesAJobNoWorkerWillTakeBack(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("abandoned-for-good")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	for range 3 {
		if _, err := store.Claim(ctx, 0); err != nil {
			t.Fatalf("claim: %v", err)
		}
	}

	closed, err := store.FailAbandoned(ctx, 0)
	if err != nil {
		t.Fatalf("fail abandoned: %v", err)
	}
	if closed != 1 {
		t.Fatalf("closed %d jobs, want 1", closed)
	}

	depth, err := store.Measure(ctx)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}
	// Left running it would count against the queue's depth forever, and the
	// person who asked for it would never be told anything.
	if depth.Running != 0 {
		t.Errorf("running = %d, want the abandoned job closed", depth.Running)
	}
	if depth.Dead != 1 {
		t.Errorf("dead = %d, want the abandoned job counted as a dead letter", depth.Dead)
	}
}

func TestFailAbandonedLeavesAJobThatStillHasAttempts(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	if _, _, err := store.Enqueue(ctx, newJob("still-retriable")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := store.Claim(ctx, 0); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// One attempt of three used. This job is stale, but it is the reclaim's to
	// pick up, not the sweep's to bury.
	closed, err := store.FailAbandoned(ctx, 0)
	if err != nil {
		t.Fatalf("fail abandoned: %v", err)
	}
	if closed != 0 {
		t.Errorf("closed %d jobs, want the retriable one left alone", closed)
	}

	if _, err := store.Claim(ctx, 0); err != nil {
		t.Errorf("the job should still be claimable: %v", err)
	}
}

func TestTheTraceCrossesTheQueue(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	const traceParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	input := newJob("traced")
	input.TraceParent = traceParent

	if _, _, err := store.Enqueue(ctx, input); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	claimed, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.TraceParent != traceParent {
		t.Errorf("traceparent = %q, want %q", claimed.TraceParent, traceParent)
	}
}

func TestAJobEnqueuedWithoutATraceClaimsCleanly(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	// Tracing off, or a row written before the column existed. The column is
	// nullable, and a null must arrive as an empty string rather than as a scan
	// error that stops the queue.
	if _, _, err := store.Enqueue(ctx, newJob("untraced")); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	claimed, err := store.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.TraceParent != "" {
		t.Errorf("traceparent = %q, want empty", claimed.TraceParent)
	}
}
