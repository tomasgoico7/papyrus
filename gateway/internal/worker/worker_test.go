package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
	"github.com/papyrus/gateway/internal/worker"
)

// fakeQueue hands out a fixed set of jobs and records what the worker did with
// them. The SQL behind the real queue is covered by the store's own tests
// against Postgres; what matters here is the policy.
type fakeQueue struct {
	mu sync.Mutex

	pending   []*jobs.Job
	completed map[string][]byte
	retried   map[string]time.Time
	failed    map[string]string

	completeErr   error
	purges        [][2]time.Duration
	abandonSweeps []time.Duration
	drained       chan struct{}
	closeOnce     sync.Once
}

func newFakeQueue(pending ...*jobs.Job) *fakeQueue {
	return &fakeQueue{
		pending:   pending,
		completed: map[string][]byte{},
		retried:   map[string]time.Time{},
		failed:    map[string]string{},
		drained:   make(chan struct{}),
	}
}

func (q *fakeQueue) Claim(context.Context, time.Duration) (*jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		q.closeOnce.Do(func() { close(q.drained) })
		return nil, jobs.ErrNotFound
	}
	job := q.pending[0]
	q.pending = q.pending[1:]
	return job, nil
}

// The writes below all check the context first, because a real store does. A
// fake that ignores it hides the whole class of bug where an outcome is written
// through a context that has already expired.
func (q *fakeQueue) Complete(ctx context.Context, id string, result json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.completeErr != nil {
		return q.completeErr
	}
	q.completed[id] = result
	return nil
}

func (q *fakeQueue) Retry(ctx context.Context, id string, runAfter time.Time, _, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.retried[id] = runAfter
	return nil
}

func (q *fakeQueue) Fail(ctx context.Context, id, code, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed[id] = code
	return nil
}

func (q *fakeQueue) Measure(context.Context) (jobs.Depth, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return jobs.Depth{Queued: len(q.pending), Dead: len(q.failed)}, nil
}

func (q *fakeQueue) PurgeFinished(_ context.Context, doneAfter, deadAfter time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.purges = append(q.purges, [2]time.Duration{doneAfter, deadAfter})
	return 0, nil
}

func (q *fakeQueue) FailAbandoned(_ context.Context, staleAfter time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.abandonSweeps = append(q.abandonSweeps, staleAfter)
	return 0, nil
}

func (q *fakeQueue) snapshot() (completed map[string][]byte, retried map[string]time.Time, failed map[string]string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return maps(q.completed), times(q.retried), strs(q.failed)
}

func maps(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func times(in map[string]time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func strs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type stubAnalyzer struct {
	err error
}

func (a stubAnalyzer) Analyze(_ context.Context, req services.AnalyzeRequest) (*transport.Analysis, error) {
	_, _ = io.ReadAll(req.CV)
	if a.err != nil {
		return nil, a.err
	}
	return &transport.Analysis{
		Score:         74,
		Verdict:       "moderate",
		Summary:       transport.Localized{En: "Decent fit.", Es: "Encaje razonable."},
		MatchedSkills: transport.LocalizedList{En: []string{"Go"}, Es: []string{"Go"}},
		MissingSkills: transport.LocalizedList{En: []string{}, Es: []string{}},
		Suggestions:   []transport.Suggestion{},
	}, nil
}

func job(id string, attempts, maxAttempts int) *jobs.Job {
	return &jobs.Job{
		ID:          id,
		UserID:      "user-1",
		DedupKey:    "key-" + id,
		State:       jobs.StateRunning,
		Attempts:    attempts,
		MaxAttempts: maxAttempts,
		CV:          []byte("%PDF-1.4 bytes"),
		CVFilename:  "cv.pdf",
		JobOffer:    "A backend role.",
		JobTitle:    "Backend Engineer",
	}
}

// run drains the queue once and stops, so a test never waits on a poll tick.
func run(t *testing.T, queue *fakeQueue, analyzer services.Analyzer) {
	t.Helper()

	w := worker.New(
		queue,
		analyzer,
		nil,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:   1,
			PollInterval:  time.Millisecond,
			JobTimeout:    2 * time.Second,
			BaseBackoff:   10 * time.Millisecond,
			MaxBackoff:    50 * time.Millisecond,
			DepthInterval: time.Hour,
			PurgeInterval: time.Hour,
			DoneRetention: 24 * time.Hour,
			DeadRetention: 7 * 24 * time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-queue.drained:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the worker never drained the queue")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker did not stop when its context was cancelled")
	}
}

func TestWorkerCompletesASuccessfulJob(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	run(t, queue, stubAnalyzer{})

	completed, _, failed := queue.snapshot()
	if len(failed) != 0 {
		t.Errorf("unexpected failures: %v", failed)
	}
	raw, ok := completed["j1"]
	if !ok {
		t.Fatal("the job was not completed")
	}

	var stored transport.AnalysisResponse
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatalf("decoding the stored result: %v", err)
	}
	if stored.Score != 74 {
		t.Errorf("score = %d, want 74", stored.Score)
	}
	// The filename is the gateway's to add, and the client needs it back.
	if stored.CVFilename != "cv.pdf" {
		t.Errorf("cvFilename = %q, want it carried into the stored result", stored.CVFilename)
	}
}

func TestWorkerRetriesATransientFailure(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	run(t, queue, stubAnalyzer{err: errors.New("connection reset")})

	completed, retried, failed := queue.snapshot()
	if len(completed) != 0 || len(failed) != 0 {
		t.Errorf("completed=%v failed=%v, want neither", completed, failed)
	}
	runAfter, ok := retried["j1"]
	if !ok {
		t.Fatal("the job was not rescheduled")
	}
	if !runAfter.After(time.Now()) {
		t.Error("a retry must be held until some point in the future")
	}
}

func TestWorkerGivesUpOnceTheAttemptsAreSpent(t *testing.T) {
	// Third attempt of three: there is nothing left to try.
	queue := newFakeQueue(job("j1", 3, 3))
	run(t, queue, stubAnalyzer{err: errors.New("connection reset")})

	_, retried, failed := queue.snapshot()
	if len(retried) != 0 {
		t.Errorf("retried = %v, want the job to stop being retried", retried)
	}
	if failed["j1"] == "" {
		t.Error("the job should have been marked failed")
	}
}

func TestWorkerClassifiesUpstreamFailures(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{
			name:      "an unreadable cv is permanent",
			err:       &services.UpstreamError{StatusCode: http.StatusUnprocessableEntity, Code: "unreadable_cv", Message: "Not readable."},
			wantRetry: false,
		},
		{
			// A 4xx, but the only one where trying the same thing again is right.
			name:      "throttling is worth another attempt",
			err:       &services.UpstreamError{StatusCode: http.StatusTooManyRequests, Code: "upstream_rate_limited", Message: "Busy."},
			wantRetry: true,
		},
		{
			// Throttling that arrived without our envelope is still throttling.
			name:      "throttling behind a proxy page is worth another attempt",
			err:       &services.UpstreamError{StatusCode: http.StatusTooManyRequests, Code: "ai_service_error", Body: "Too Many Requests"},
			wantRetry: true,
		},
		{
			name:      "an upstream 5xx is worth another attempt",
			err:       &services.UpstreamError{StatusCode: http.StatusBadGateway, Code: "ai_service_error", Message: "Boom."},
			wantRetry: true,
		},
		{
			name:      "an unrecognised error is assumed transient",
			err:       errors.New("something odd"),
			wantRetry: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queue := newFakeQueue(job("j1", 1, 3))
			run(t, queue, stubAnalyzer{err: tc.err})

			_, retried, failed := queue.snapshot()
			gotRetry := len(retried) == 1

			if gotRetry != tc.wantRetry {
				t.Errorf("retried = %t, want %t (failed: %v)", gotRetry, tc.wantRetry, failed)
			}
			if !tc.wantRetry && failed["j1"] == "" {
				t.Error("a permanent failure should end the job")
			}
		})
	}
}

func TestWorkerSurvivesAResultItCannotRecord(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	queue.completeErr = errors.New("job was reclaimed")

	// The analysis ran and the result is unrecordable — wasted, not wrong. The
	// worker must carry on rather than treat it as a job failure.
	run(t, queue, stubAnalyzer{})

	_, retried, failed := queue.snapshot()
	if len(retried) != 0 || len(failed) != 0 {
		t.Errorf("retried=%v failed=%v; a lost result is neither", retried, failed)
	}
}

func TestWorkerProcessesEveryQueuedJob(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3), job("j2", 1, 3), job("j3", 1, 3))
	run(t, queue, stubAnalyzer{})

	completed, _, _ := queue.snapshot()
	if len(completed) != 3 {
		t.Errorf("completed %d jobs, want 3", len(completed))
	}
}

func TestWorkerStopsOnAnEmptyQueueWithoutSpinning(t *testing.T) {
	queue := newFakeQueue()
	run(t, queue, stubAnalyzer{})

	completed, retried, failed := queue.snapshot()
	if len(completed)+len(retried)+len(failed) != 0 {
		t.Error("an empty queue should produce no work at all")
	}
}

func TestWorkerSweepsFinishedJobsOnItsOwn(t *testing.T) {
	// Nothing else deletes a finished job, so the worker has to — and it has to
	// keep a dead one longer than a successful one, because the dead one is what
	// somebody will want to look at.
	queue := newFakeQueue()
	run(t, queue, stubAnalyzer{})

	queue.mu.Lock()
	defer queue.mu.Unlock()

	if len(queue.purges) == 0 {
		t.Fatal("the worker never swept finished jobs")
	}
	done, dead := queue.purges[0][0], queue.purges[0][1]
	if done <= 0 || dead <= 0 {
		t.Fatalf("retentions = %v / %v, want both positive", done, dead)
	}
	if dead <= done {
		t.Errorf("dead letters kept for %v, successes for %v; the dead ones should outlive them", dead, done)
	}
}

// hangingAnalyzer never answers on its own; it waits for the deadline.
type hangingAnalyzer struct{}

func (hangingAnalyzer) Analyze(ctx context.Context, _ services.AnalyzeRequest) (*transport.Analysis, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestWorkerRecordsAJobThatRanOutOfTime is a regression test for a bug that only
// showed up in production: the outcome was written through the same context that
// bounded the work, so when the failure *was* the deadline, recording it failed
// too. The job stayed claimed and running, invisible, until the stale sweep
// reclaimed it minutes later — turning every timeout into a five-minute stall.
func TestWorkerRecordsAJobThatRanOutOfTime(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))

	w := worker.New(
		queue,
		hangingAnalyzer{},
		nil,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:        1,
			PollInterval:       time.Millisecond,
			JobTimeout:         30 * time.Millisecond,
			BookkeepingTimeout: 2 * time.Second,
			BaseBackoff:        10 * time.Millisecond,
			MaxBackoff:         50 * time.Millisecond,
			DepthInterval:      time.Hour,
			PurgeInterval:      time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	select {
	case <-queue.drained:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("the worker never drained the queue")
	}
	// Give the outcome a moment to land before stopping.
	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	_, retried, failed := queue.snapshot()
	if len(retried) == 0 && len(failed) == 0 {
		t.Fatal("the job timed out and nothing was recorded; it would sit claimed until the stale sweep")
	}
	if len(retried) != 1 {
		t.Errorf("retried = %v, want the timed-out job rescheduled", retried)
	}
}

func TestWorkerRecordsThrottlingAsThrottling(t *testing.T) {
	// The same 429 reaches the request path and the worker by different routes.
	// If only one of them calls it throttling, a capacity problem looks like two
	// unrelated faults depending on which path saw it.
	queue := newFakeQueue(job("j1", 3, 3))
	run(t, queue, stubAnalyzer{err: &services.UpstreamError{
		StatusCode: 429,
		Code:       "ai_service_error",
		Body:       "Too Many Requests",
	}})

	_, _, failed := queue.snapshot()
	if got := failed["j1"]; got != "upstream_rate_limited" {
		t.Errorf("recorded %q, want upstream_rate_limited", got)
	}
}

func TestWorkerWaitsLongerWhenTheUpstreamIsThrottling(t *testing.T) {
	// The case this exists for: a platform parks an idle service and takes tens
	// of seconds to bring it back, answering 429 in the meantime. Retrying on
	// the ordinary schedule spends all three attempts inside that window — and
	// the retries are themselves the concurrency that provokes the 429.
	throttled := &services.UpstreamError{StatusCode: 429, Code: "ai_service_error", Body: "Too Many Requests"}

	delays := map[string]time.Duration{}
	for name, cause := range map[string]error{
		"ordinary":  errors.New("connection reset"),
		"throttled": throttled,
	} {
		queue := newFakeQueue(job("j1", 1, 3))
		before := time.Now()

		w := worker.New(
			queue,
			stubAnalyzer{err: cause},
			nil,
			observability.NewMetrics(),
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			worker.Config{
				Concurrency:     1,
				PollInterval:    time.Millisecond,
				JobTimeout:      time.Second,
				BaseBackoff:     20 * time.Millisecond,
				ThrottleBackoff: 2 * time.Second,
				MaxBackoff:      time.Minute,
				DepthInterval:   time.Hour,
				PurgeInterval:   time.Hour,
			},
		)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { w.Run(ctx); close(done) }()
		<-queue.drained
		time.Sleep(100 * time.Millisecond)
		cancel()
		<-done

		_, retried, _ := queue.snapshot()
		runAfter, ok := retried["j1"]
		if !ok {
			t.Fatalf("%s: the job was not rescheduled", name)
		}
		delays[name] = runAfter.Sub(before)
	}

	if delays["throttled"] <= delays["ordinary"] {
		t.Errorf("throttled waits %v, ordinary waits %v; being told to slow down should mean waiting longer",
			delays["throttled"], delays["ordinary"])
	}
	if delays["throttled"] < time.Second {
		t.Errorf("throttled retry scheduled in %v; too soon to let the upstream come back", delays["throttled"])
	}
}

// wakingUpstream is asleep for the first few probes and then answers, the way a
// parked instance does once something reaches it.
type wakingUpstream struct {
	probes    atomic.Int64
	readyFrom int64
}

func (u *wakingUpstream) Ready(context.Context) error {
	if u.probes.Add(1) < u.readyFrom {
		return errors.New("still starting")
	}
	return nil
}

func runWith(t *testing.T, queue *fakeQueue, analyzer services.Analyzer, readiness worker.Readiness) {
	t.Helper()

	w := worker.New(
		queue,
		analyzer,
		readiness,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:       1,
			PollInterval:      time.Millisecond,
			JobTimeout:        time.Second,
			BaseBackoff:       10 * time.Millisecond,
			ThrottleBackoff:   10 * time.Second,
			ReadyBackoff:      5 * time.Millisecond,
			WakeBudget:        2 * time.Second,
			WakeProbeInterval: 10 * time.Millisecond,
			MaxBackoff:        time.Minute,
			DepthInterval:     time.Hour,
			PurgeInterval:     time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	<-queue.drained
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done
}

func throttled() error {
	return &services.UpstreamError{StatusCode: 429, Code: "ai_service_error", Body: "Too Many Requests"}
}

// TestWorkerWakesAThrottledUpstreamInsteadOfWaitingBlind is the behaviour this
// exists for. A parked instance answers 429 through its proxy, and those
// refusals never reach the thing that would start it — so backing off longer
// only changes how long it takes to fail. A readiness probe does get through.
func TestWorkerWakesAThrottledUpstreamInsteadOfWaitingBlind(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	upstream := &wakingUpstream{readyFrom: 3}
	before := time.Now()

	runWith(t, queue, stubAnalyzer{err: throttled()}, upstream)

	if got := upstream.probes.Load(); got < 3 {
		t.Errorf("probed %d times, want it to keep asking until the upstream answered", got)
	}

	_, retried, _ := queue.snapshot()
	runAfter, ok := retried["j1"]
	if !ok {
		t.Fatal("the job was not rescheduled")
	}

	// Scheduled soon, not after the long throttle backoff: the upstream has
	// already said it is ready, so there is nothing left to wait for.
	if wait := runAfter.Sub(before); wait > 2*time.Second {
		t.Errorf("next attempt in %v; once the upstream is ready it should be retried promptly", wait)
	}
}

func TestWorkerFallsBackToBackoffWhenTheUpstreamStaysDown(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	// readyFrom beyond what the budget allows: it never comes back.
	upstream := &wakingUpstream{readyFrom: 1 << 30}
	before := time.Now()

	runWith(t, queue, stubAnalyzer{err: throttled()}, upstream)

	_, retried, _ := queue.snapshot()
	runAfter, ok := retried["j1"]
	if !ok {
		t.Fatal("the job was not rescheduled")
	}
	// Still scheduled, and this time with the long delay: giving up on the probe
	// must not turn into giving up on the job.
	if wait := runAfter.Sub(before); wait < time.Second {
		t.Errorf("next attempt in %v; an upstream that never answered deserves the full backoff", wait)
	}
}

func TestWorkerDoesNotProbeForAnOrdinaryFailure(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	upstream := &wakingUpstream{readyFrom: 1}

	runWith(t, queue, stubAnalyzer{err: errors.New("connection reset")}, upstream)

	// A reset connection says nothing about the upstream being asleep, and a
	// probe on every ordinary failure is just more traffic at a bad moment.
	if got := upstream.probes.Load(); got != 0 {
		t.Errorf("probed %d times for a non-throttled failure, want none", got)
	}
}

// TestWorkerStillRecordsTheOutcomeAfterALongWait guards the interaction between
// the two. The bookkeeping context is deliberately short, and waiting for a
// sleeping upstream is deliberately long; if the first is opened before the
// second runs, it expires while waiting and the reschedule fails silently. The
// job then sits in running until the stale sweep reclaims it minutes later —
// which is the exact stall the separate bookkeeping context exists to prevent.
func TestWorkerStillRecordsTheOutcomeAfterALongWait(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	upstream := &wakingUpstream{readyFrom: 1 << 30}

	w := worker.New(
		queue,
		stubAnalyzer{err: throttled()},
		upstream,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:  1,
			PollInterval: time.Millisecond,
			JobTimeout:   time.Second,
			BaseBackoff:  10 * time.Millisecond,
			MaxBackoff:   time.Minute,
			// The wait outlasts the budget for writing the result down.
			WakeBudget:         300 * time.Millisecond,
			WakeProbeInterval:  10 * time.Millisecond,
			BookkeepingTimeout: 20 * time.Millisecond,
			DepthInterval:      time.Hour,
			PurgeInterval:      time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	<-queue.drained
	time.Sleep(600 * time.Millisecond)
	cancel()
	<-done

	if _, retried, _ := queue.snapshot(); len(retried) == 0 {
		t.Error("the job was never rescheduled: the outcome was written through an expired context")
	}
}

// blockingUpstream answers only when released, or when the caller gives up.
// The difference from wakingUpstream matters: a probe that returns instantly
// cannot show whether the wait is bounded, and the real one blocks for as long
// as the platform takes to start.
type blockingUpstream struct {
	probes  atomic.Int64
	release chan struct{}
}

func (u *blockingUpstream) Ready(ctx context.Context) error {
	u.probes.Add(1)
	select {
	case <-u.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestWorkerBoundsTheWaitEvenWhenAProbeHangs(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	// Never released: the probe blocks until something cuts it off.
	upstream := &blockingUpstream{release: make(chan struct{})}

	start := time.Now()
	runWith(t, queue, stubAnalyzer{err: throttled()}, upstream)
	elapsed := time.Since(start)

	// runWith allows a 2 second budget. A probe with no timeout of its own must
	// still be cut off by it, or the budget bounds only the gaps between probes
	// and the worker slot is held for as long as the upstream cares to hang.
	if elapsed > 4*time.Second {
		t.Errorf("the wait took %v; the budget is 2s and must bound the probe too", elapsed)
	}
	if upstream.probes.Load() == 0 {
		t.Error("the upstream was never probed")
	}
	if _, retried, _ := queue.snapshot(); len(retried) == 0 {
		t.Error("the job was not rescheduled after the wait was cut off")
	}
}

func TestWorkerRetriesPromptlyOnceALongProbeSucceeds(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	upstream := &blockingUpstream{release: make(chan struct{})}

	// The upstream comes back part way through the wait, as a starting instance
	// does: the probe is still open when it happens.
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(upstream.release)
	}()

	before := time.Now()
	runWith(t, queue, stubAnalyzer{err: throttled()}, upstream)

	_, retried, _ := queue.snapshot()
	runAfter, ok := retried["j1"]
	if !ok {
		t.Fatal("the job was not rescheduled")
	}
	// Once it has answered there is nothing left to wait for, so the next
	// attempt is due almost immediately rather than after the throttle delay.
	if wait := runAfter.Sub(before); wait > time.Second {
		t.Errorf("next attempt in %v; an upstream that answered should be retried at once", wait)
	}
}

func TestWorkerShutsDownPromptlyWhileWaitingForAnUpstream(t *testing.T) {
	queue := newFakeQueue(job("j1", 1, 3))
	upstream := &blockingUpstream{release: make(chan struct{})}

	w := worker.New(
		queue,
		stubAnalyzer{err: throttled()},
		upstream,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:  1,
			PollInterval: time.Millisecond,
			JobTimeout:   time.Second,
			BaseBackoff:  10 * time.Millisecond,
			MaxBackoff:   time.Minute,
			// Far longer than this test is willing to wait: shutdown must not
			// be paced by the budget.
			WakeBudget:        30 * time.Second,
			WakeProbeInterval: 10 * time.Millisecond,
			DepthInterval:     time.Hour,
			PurgeInterval:     time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()

	// Wait until a probe is actually in flight. Waiting on queue.drained instead
	// would be waiting for the slot to free — which only happens once the wait
	// is over, so the cancel would arrive after the thing it is meant to
	// interrupt and the test would pass without testing anything.
	for upstream.probes.Load() == 0 {
		time.Sleep(time.Millisecond)
	}

	// A deploy lands while a job is waiting out a cold start. Every platform
	// that restarts a process gives it a grace period measured in seconds; a
	// worker that ignores the signal until its own budget expires gets killed
	// mid-write instead of stopping cleanly.
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the worker kept waiting for the upstream after being told to stop")
	}
}

func TestClaimStaleAfterOutlastsTheLongestLegitimateAttempt(t *testing.T) {
	// Reclaiming a job whose worker is only slow runs the analysis twice and
	// bills for it twice. The threshold is derived rather than written down so
	// that raising any one of these cannot quietly push it under the sum.
	cases := []worker.Config{
		{},
		{JobTimeout: 10 * time.Minute},
		{WakeBudget: 8 * time.Minute},
		{BookkeepingTimeout: 4 * time.Minute},
		{JobTimeout: 3 * time.Minute, WakeBudget: 3 * time.Minute, BookkeepingTimeout: time.Minute},
	}

	for _, cfg := range cases {
		got := worker.Defaults(cfg)
		longest := got.JobTimeout + got.WakeBudget + got.BookkeepingTimeout
		if got.ClaimStaleAfter <= longest {
			t.Errorf("ClaimStaleAfter = %v for %+v; an attempt can legitimately take %v",
				got.ClaimStaleAfter, cfg, longest)
		}
	}
}

func TestWorkerSweepsJobsAbandonedByADeadWorker(t *testing.T) {
	queue := newFakeQueue()

	w := worker.New(
		queue,
		stubAnalyzer{},
		nil,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:   1,
			PollInterval:  time.Millisecond,
			DepthInterval: time.Hour,
			PurgeInterval: 10 * time.Millisecond,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	<-queue.drained
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	queue.mu.Lock()
	sweeps := append([]time.Duration(nil), queue.abandonSweeps...)
	queue.mu.Unlock()

	if len(sweeps) == 0 {
		t.Fatal("no sweep for abandoned jobs; a job whose worker died is never closed")
	}
	// Swept at the same threshold the reclaim uses, or the two disagree about
	// which jobs are abandoned and a job can be both too old to retry and too
	// young to close.
	want := worker.Defaults(worker.Config{}).ClaimStaleAfter
	for _, got := range sweeps {
		if got != want {
			t.Errorf("swept at %v, want the claim threshold %v", got, want)
		}
	}
}

// TestWorkerContinuesTheTraceThatEnqueuedTheJob is the reason the queue carries
// a traceparent at all. Without it the request and the work it caused are two
// unrelated traces, and the part everybody actually wants to see — how long the
// job sat before anyone picked it up — is the gap between them, visible in
// neither.
func TestWorkerContinuesTheTraceThatEnqueuedTheJob(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	restore := swapTracerProvider(t, provider)
	defer restore()

	// The trace the request would have been on, serialised the way the row
	// stores it.
	const enqueued = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	queued := job("j1", 1, 3)
	queued.TraceParent = enqueued

	runWith(t, newFakeQueue(queued), stubAnalyzer{}, nil)

	spans := recorder.Ended()
	if len(spans) == 0 {
		t.Fatal("the attempt produced no span")
	}

	var found bool
	for _, span := range spans {
		if span.Name() != "analysis job" {
			continue
		}
		found = true
		if got := span.SpanContext().TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
			t.Errorf("trace id = %s, want the one the job was enqueued on", got)
		}
		if got := span.Parent().SpanID().String(); got != "00f067aa0ba902b7" {
			t.Errorf("parent span = %s, want the enqueueing span", got)
		}
		if !span.Parent().IsRemote() {
			t.Error("the parent should be remote: it happened in another process")
		}
	}
	if !found {
		t.Error("no span named for the attempt")
	}
}

func TestWorkerStartsItsOwnTraceForAJobThatHasNone(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	restore := swapTracerProvider(t, provider)
	defer restore()

	// Enqueued before the column existed, or by a process with tracing off.
	// Neither is a reason not to run the analysis.
	runWith(t, newFakeQueue(job("j1", 1, 3)), stubAnalyzer{}, nil)

	for _, span := range recorder.Ended() {
		if span.Name() == "analysis job" && !span.SpanContext().TraceID().IsValid() {
			t.Error("the attempt ran without a trace of its own")
		}
	}
}
