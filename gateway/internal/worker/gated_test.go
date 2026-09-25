package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/worker"
)

func TestAGatedQueueHandsOutNothingWhileClosed(t *testing.T) {
	inner := newFakeQueue(job("j1", 1, 3))
	open := false
	queue := worker.Gated(inner, func() bool { return open })

	// A release ahead of its migration: the job is there, but claiming it means
	// reading columns that may not exist yet. Empty is the honest answer.
	if _, err := queue.Claim(context.Background(), time.Minute); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("claim while closed = %v, want the queue to look empty", err)
	}

	// The migration lands, and the job that waited is picked up on the next poll.
	open = true
	claimed, err := queue.Claim(context.Background(), time.Minute)
	if err != nil {
		t.Fatalf("claim once open: %v", err)
	}
	if claimed.ID != "j1" {
		t.Errorf("claimed %q, want the job that was waiting", claimed.ID)
	}
}

func TestAGatedQueueStillRecordsOutcomes(t *testing.T) {
	inner := newFakeQueue()
	queue := worker.Gated(inner, func() bool { return false })

	// A job already in hand when the gate shut still has to be written down.
	// Refusing that would strand it as running until the stale sweep.
	if err := queue.Retry(context.Background(), "j1", time.Now(), "code", "message"); err != nil {
		t.Fatalf("retry through a closed gate: %v", err)
	}
	if err := queue.Fail(context.Background(), "j2", "code", "message"); err != nil {
		t.Fatalf("fail through a closed gate: %v", err)
	}

	_, retried, failed := inner.snapshot()
	if _, ok := retried["j1"]; !ok {
		t.Error("the retry did not reach the queue")
	}
	if _, ok := failed["j2"]; !ok {
		t.Error("the failure did not reach the queue")
	}
}
