package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
	"github.com/papyrus/gateway/internal/worker"
)

// The unit tests drive the worker against a fake queue, and the store tests
// drive the queue without a worker. These run the two together against the real
// database, which is where a mismatch between them would actually show up.

type scriptedAnalyzer struct {
	calls  atomic.Int64
	failFn func(call int64) error
}

func (a *scriptedAnalyzer) Analyze(_ context.Context, req services.AnalyzeRequest) (*transport.Analysis, error) {
	call := a.calls.Add(1)
	if _, err := io.ReadAll(req.CV); err != nil {
		return nil, err
	}
	if a.failFn != nil {
		if err := a.failFn(call); err != nil {
			return nil, err
		}
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

func runWorker(t *testing.T, store *jobs.Store, analyzer services.Analyzer, until func() bool) {
	t.Helper()

	w := worker.New(
		store,
		analyzer,
		observability.NewMetrics(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		worker.Config{
			Concurrency:   2,
			PollInterval:  10 * time.Millisecond,
			JobTimeout:    5 * time.Second,
			BaseBackoff:   time.Millisecond,
			MaxBackoff:    5 * time.Millisecond,
			DepthInterval: time.Hour,
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(done)
	}()

	deadline := time.After(20 * time.Second)
	for !until() {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("the worker did not reach the expected state in time")
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker did not stop when cancelled")
	}
}

func TestWorkerDrainsRealJobsEndToEnd(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	const total = 6
	ids := make([]string, 0, total)
	for i := range total {
		job, _, err := store.Enqueue(ctx, newJob(string(rune('a'+i))))
		if err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		ids = append(ids, job.ID)
	}

	analyzer := &scriptedAnalyzer{}
	runWorker(t, store, analyzer, func() bool {
		queued, running, err := store.Depth(ctx)
		return err == nil && queued == 0 && running == 0
	})

	if got := analyzer.calls.Load(); got != total {
		t.Errorf("the analyzer ran %d times, want %d — every job exactly once", got, total)
	}

	for _, id := range ids {
		job, err := store.Get(ctx, id, testerA)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if job.State != jobs.StateDone {
			t.Errorf("job %s is %q, want done (%s)", id, job.State, job.ErrorMessage)
		}

		var stored transport.AnalysisResponse
		if err := json.Unmarshal(job.Result, &stored); err != nil {
			t.Fatalf("decoding the stored result: %v", err)
		}
		if stored.Score != 74 || stored.CVFilename != "cv.pdf" {
			t.Errorf("stored result = %+v", stored)
		}
		assertUploadDropped(t, id)
	}
}

func TestWorkerRetriesThroughTheDatabaseAndThenSucceeds(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, _, err := store.Enqueue(ctx, newJob("flaky"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Fails twice, then works. The retry has to survive a round trip through the
	// table, keeping the upload so the next attempt has something to run.
	analyzer := &scriptedAnalyzer{failFn: func(call int64) error {
		if call <= 2 {
			return errors.New("upstream had a moment")
		}
		return nil
	}}

	runWorker(t, store, analyzer, func() bool {
		current, err := store.Get(ctx, job.ID, testerA)
		return err == nil && current.State.IsTerminal()
	})

	final, err := store.Get(ctx, job.ID, testerA)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if final.State != jobs.StateDone {
		t.Fatalf("state = %q, want done after the upstream recovered", final.State)
	}
	if final.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", final.Attempts)
	}
}

func TestWorkerSendsAJobToTheDeadLetterStateOnceSpent(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, _, err := store.Enqueue(ctx, newJob("doomed"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	analyzer := &scriptedAnalyzer{failFn: func(int64) error {
		return errors.New("permanently broken upstream")
	}}

	runWorker(t, store, analyzer, func() bool {
		current, err := store.Get(ctx, job.ID, testerA)
		return err == nil && current.State.IsTerminal()
	})

	final, _ := store.Get(ctx, job.ID, testerA)
	if final.State != jobs.StateFailed {
		t.Fatalf("state = %q, want failed", final.State)
	}
	if final.Attempts != final.MaxAttempts {
		t.Errorf("attempts = %d, want it to stop at max_attempts (%d)", final.Attempts, final.MaxAttempts)
	}
	if final.ErrorCode == "" {
		t.Error("a dead job should carry why it died")
	}
	// Nothing is going to be fixed by keeping the bytes around.
	assertUploadDropped(t, job.ID)
}

func TestWorkerDoesNotRetryAnInputThatWillNeverWork(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	job, _, err := store.Enqueue(ctx, newJob("bad-pdf"))
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	analyzer := &scriptedAnalyzer{failFn: func(int64) error {
		return &services.UpstreamError{
			StatusCode: http.StatusUnprocessableEntity,
			Code:       "unreadable_cv",
			Message:    "The CV is not readable.",
		}
	}}

	runWorker(t, store, analyzer, func() bool {
		current, err := store.Get(ctx, job.ID, testerA)
		return err == nil && current.State.IsTerminal()
	})

	final, _ := store.Get(ctx, job.ID, testerA)
	if final.State != jobs.StateFailed {
		t.Fatalf("state = %q, want failed", final.State)
	}
	// A scanned PDF will not become readable on the third try; burning the
	// attempts on it just delays telling the user.
	if final.Attempts != 1 {
		t.Errorf("attempts = %d, want the job to stop after one", final.Attempts)
	}
	if final.ErrorCode != "unreadable_cv" {
		t.Errorf("error code = %q, want the upstream reason preserved", final.ErrorCode)
	}
}
