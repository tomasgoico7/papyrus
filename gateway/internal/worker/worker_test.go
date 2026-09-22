package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

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

	completeErr error
	purges      [][2]time.Duration
	drained     chan struct{}
	closeOnce   sync.Once
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
