package worker

import (
	"context"
	"time"

	"github.com/papyrus/gateway/internal/jobs"
)

// Gated returns a queue that hands out no work while open reports false.
//
// It exists for the window after a release and before its migration. Claiming
// a job then means reading columns that may not be there yet, so every claim
// fails, and the worker logs the same error every poll until someone applies
// the migration. Reporting the queue as empty instead is the truthful thing
// from the worker's side — there is nothing it can safely take — and when the
// schema catches up the next poll finds the work that was waiting.
//
// Only claiming is held back. Everything else the worker does touches columns
// that have existed since the queue did, and recording the outcome of a job
// already in hand must never be refused.
func Gated(queue Queue, open func() bool) Queue {
	return gated{Queue: queue, open: open}
}

type gated struct {
	Queue
	open func() bool
}

func (g gated) Claim(ctx context.Context, staleAfter time.Duration) (*jobs.Job, error) {
	if !g.open() {
		return nil, jobs.ErrNotFound
	}
	return g.Queue.Claim(ctx, staleAfter)
}
