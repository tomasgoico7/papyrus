// Package worker drains the analysis queue.
//
// It is deliberately separate from the API: the two scale on different things —
// the API on how many people are using the site, the worker on how fast the
// model answers — and keeping them apart is what lets one grow without the
// other. On a free tier they still ship in the same binary, behind a flag; that
// is a deployment decision, not a design one. See docs/adr/0008.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// Queue is the slice of the job store the worker needs. Defined here rather
// than imported wholesale so a test can drive the loop without a database.
type Queue interface {
	Claim(ctx context.Context, staleAfter time.Duration) (*jobs.Job, error)
	Complete(ctx context.Context, id string, result json.RawMessage) error
	Retry(ctx context.Context, id string, runAfter time.Time, code, message string) error
	Fail(ctx context.Context, id, code, message string) error
	Measure(ctx context.Context) (jobs.Depth, error)
	PurgeFinished(ctx context.Context, doneAfter, deadAfter time.Duration) (int64, error)
}

// Config tunes the loop. Every field has a working default.
type Config struct {
	// Concurrency is how many jobs run at once in this process.
	Concurrency int
	// PollInterval is the wait after finding the queue empty.
	PollInterval time.Duration
	// ClaimStaleAfter is how long a claim may sit untouched before another
	// worker assumes the holder died and takes the job back.
	ClaimStaleAfter time.Duration
	// JobTimeout bounds a single attempt.
	JobTimeout time.Duration
	// BaseBackoff and MaxBackoff bound the retry delay.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// DepthInterval is how often the queue gauges are refreshed.
	DepthInterval time.Duration
	// PurgeInterval is how often finished jobs are swept away, and
	// DoneRetention / DeadRetention are how long each kind is kept first.
	PurgeInterval time.Duration
	DoneRetention time.Duration
	DeadRetention time.Duration
}

func (c Config) withDefaults() Config {
	if c.Concurrency < 1 {
		c.Concurrency = 2
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.ClaimStaleAfter <= 0 {
		c.ClaimStaleAfter = 5 * time.Minute
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = 90 * time.Second
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 5 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.DepthInterval <= 0 {
		c.DepthInterval = 15 * time.Second
	}
	if c.PurgeInterval <= 0 {
		c.PurgeInterval = time.Hour
	}
	if c.DoneRetention <= 0 {
		c.DoneRetention = 24 * time.Hour
	}
	if c.DeadRetention <= 0 {
		c.DeadRetention = 7 * 24 * time.Hour
	}
	return c
}

type Worker struct {
	queue    Queue
	analyzer services.Analyzer
	metrics  *observability.Metrics
	logger   *slog.Logger
	cfg      Config
}

func New(
	queue Queue,
	analyzer services.Analyzer,
	metrics *observability.Metrics,
	logger *slog.Logger,
	cfg Config,
) *Worker {
	return &Worker{
		queue:    queue,
		analyzer: analyzer,
		metrics:  metrics,
		logger:   logger,
		cfg:      cfg.withDefaults(),
	}
}

// Run drains the queue until ctx is cancelled, then waits for the jobs already
// in flight. A job that is mid-flight when shutdown starts is allowed to finish
// rather than abandoned: it would only be reclaimed and redone.
func (w *Worker) Run(ctx context.Context) {
	w.logger.Info("worker starting",
		slog.Int("concurrency", w.cfg.Concurrency),
		slog.Duration("job_timeout", w.cfg.JobTimeout),
	)

	var wg sync.WaitGroup
	for i := range w.cfg.Concurrency {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			w.loop(ctx, slot)
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.reportDepth(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		w.purge(ctx)
	}()

	wg.Wait()
	w.logger.Info("worker stopped")
}

func (w *Worker) loop(ctx context.Context, slot int) {
	logger := w.logger.With(slog.Int("slot", slot))

	for {
		if ctx.Err() != nil {
			return
		}

		job, err := w.queue.Claim(ctx, w.cfg.ClaimStaleAfter)
		switch {
		case errors.Is(err, jobs.ErrNotFound):
			if !sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			// A database hiccup must not spin the loop at full speed.
			logger.Error("claiming a job failed", slog.Any("error", err))
			if !sleep(ctx, w.cfg.PollInterval) {
				return
			}
			continue
		}

		w.process(ctx, job, logger)
	}
}

func (w *Worker) process(ctx context.Context, job *jobs.Job, logger *slog.Logger) {
	logger = logger.With(
		slog.String("job_id", job.ID),
		slog.Int("attempt", job.Attempts),
	)
	started := time.Now()

	// Detached from the shutdown signal on purpose: a job already claimed is
	// finished or explicitly released, never silently dropped half-done.
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), w.cfg.JobTimeout)
	defer cancel()
	runCtx = observability.ContextWithLogger(runCtx, logger)

	analysis, err := w.analyzer.Analyze(runCtx, services.AnalyzeRequest{
		CV:       bytes.NewReader(job.CV),
		Filename: job.CVFilename,
		JobOffer: job.JobOffer,
		JobTitle: job.JobTitle,
	})
	elapsed := time.Since(started)

	if err != nil {
		w.handleFailure(runCtx, job, err, elapsed, logger)
		return
	}

	encoded, err := json.Marshal(transport.AnalysisResponse{
		Analysis:   *analysis,
		CVFilename: job.CVFilename,
	})
	if err != nil {
		// The result cannot be stored, and running it again will not change
		// that. Ending the job is more honest than retrying forever.
		logger.Error("encoding the analysis failed", slog.Any("error", err))
		w.finish(runCtx, job, "internal_error", "The analysis could not be stored.", elapsed, logger)
		return
	}

	if err := w.queue.Complete(runCtx, job.ID, encoded); err != nil {
		// Most likely the job was reclaimed and somebody else already finished
		// it. The work is wasted, not wrong.
		logger.Warn("completing the job failed", slog.Any("error", err))
		w.metrics.RecordJob(observability.JobLost, elapsed)
		return
	}

	logger.Info("job completed", slog.Duration("took", elapsed))
	w.metrics.RecordJob(observability.JobDone, elapsed)
}

func (w *Worker) handleFailure(
	ctx context.Context,
	job *jobs.Job,
	cause error,
	elapsed time.Duration,
	logger *slog.Logger,
) {
	code, message := describe(cause)

	if !retriable(cause) {
		logger.Warn("job failed permanently",
			slog.String("code", code),
			slog.Any("error", cause),
		)
		w.finish(ctx, job, code, message, elapsed, logger)
		return
	}

	if job.Attempts >= job.MaxAttempts {
		logger.Error("job exhausted its attempts",
			slog.Int("attempts", job.Attempts),
			slog.Any("error", cause),
		)
		w.finish(ctx, job, code, message, elapsed, logger)
		return
	}

	delay := backoff(job.Attempts, w.cfg.BaseBackoff, w.cfg.MaxBackoff)
	if err := w.queue.Retry(ctx, job.ID, time.Now().Add(delay), code, message); err != nil {
		logger.Warn("rescheduling the job failed", slog.Any("error", err))
		w.metrics.RecordJob(observability.JobLost, elapsed)
		return
	}

	logger.Warn("job will be retried",
		slog.Duration("in", delay),
		slog.String("code", code),
		slog.Any("error", cause),
	)
	w.metrics.RecordJob(observability.JobRetried, elapsed)
}

// finish moves a job to the dead state: no attempt left, or none worth making.
func (w *Worker) finish(
	ctx context.Context,
	job *jobs.Job,
	code, message string,
	elapsed time.Duration,
	logger *slog.Logger,
) {
	if err := w.queue.Fail(ctx, job.ID, code, message); err != nil {
		logger.Warn("marking the job failed did not stick", slog.Any("error", err))
		w.metrics.RecordJob(observability.JobLost, elapsed)
		return
	}
	w.metrics.RecordJob(observability.JobFailed, elapsed)
}

// reportDepth keeps the queue gauges current. Depth is what says whether the
// workers are keeping up: a rising queue with healthy jobs means too few
// workers, not a broken one.
func (w *Worker) reportDepth(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.DepthInterval)
	defer ticker.Stop()

	for {
		depth, err := w.queue.Measure(ctx)
		if err == nil {
			w.metrics.SetQueueDepth(depth.Queued, depth.Running, depth.Dead)
		} else if ctx.Err() == nil {
			w.logger.Warn("reading the queue depth failed", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// purge sweeps away jobs that have outlived their usefulness.
//
// Nothing else does: a finished job keeps its row and its result forever, and on
// a database sized in hundreds of megabytes that is a slow leak rather than a
// harmless one. Every worker runs this; the delete is idempotent, so several of
// them racing costs a wasted statement, not a wrong answer.
func (w *Worker) purge(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.PurgeInterval)
	defer ticker.Stop()

	for {
		removed, err := w.queue.PurgeFinished(ctx, w.cfg.DoneRetention, w.cfg.DeadRetention)
		switch {
		case err != nil && ctx.Err() == nil:
			w.logger.Warn("purging finished jobs failed", slog.Any("error", err))
		case removed > 0:
			w.logger.Info("purged finished jobs", slog.Int64("removed", removed))
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// retriable separates "the upstream is having a moment" from "this input will
// never work". A throttled or timed-out call deserves another go; an unreadable
// PDF will be just as unreadable in five minutes.
func retriable(err error) bool {
	var upstream *services.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.IsThrottled() || !upstream.IsClientError()
	}
	// Anything unrecognised — a transport failure, a timeout — is assumed
	// transient. Retrying something permanent costs a few attempts; refusing to
	// retry something transient loses the analysis.
	return true
}

func describe(err error) (code, message string) {
	var upstream *services.UpstreamError
	if errors.As(err, &upstream) {
		return upstream.Code, upstream.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout", "The analysis took too long."
	}
	return "analysis_failed", "The analysis could not be completed."
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
