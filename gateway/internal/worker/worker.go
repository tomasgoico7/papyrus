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
	"fmt"
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
	FailAbandoned(ctx context.Context, staleAfter time.Duration) (int64, error)
}

// Readiness reports whether the upstream can take work. Optional: without one
// the worker simply backs off, which is what it did before.
type Readiness interface {
	Ready(ctx context.Context) error
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
	// ThrottleBackoff replaces BaseBackoff when the upstream refused for
	// capacity reasons. It is deliberately much larger: being throttled means
	// the far side needs time, and asking again sooner is what caused it.
	ThrottleBackoff time.Duration
	// WakeBudget is how long to wait for a throttled upstream to report itself
	// ready before giving up and falling back to a plain backoff.
	WakeBudget time.Duration
	// WakeProbeInterval is the gap between readiness probes.
	WakeProbeInterval time.Duration
	// ReadyBackoff is the short delay used once the upstream has said it is
	// ready: there is nothing left to wait for.
	ReadyBackoff time.Duration
	// BookkeepingTimeout bounds writing an outcome back to the queue. It is
	// deliberately separate from JobTimeout: recording that a job timed out
	// must not itself be subject to the deadline that just expired.
	BookkeepingTimeout time.Duration
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
	if c.JobTimeout <= 0 {
		c.JobTimeout = 90 * time.Second
	}
	if c.BaseBackoff <= 0 {
		// Wide enough that three attempts outlast a cold start on a platform
		// that sleeps idle services: roughly 35 to 70 seconds of waiting before
		// the job gives up, against the 20 to 50 one takes to wake.
		c.BaseBackoff = 10 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.WakeBudget <= 0 {
		// One measured cold start is 31 seconds, and the probe has to be able to
		// ride out a whole one rather than most of one: a wait that gives up at
		// the 30 second mark is not a short wait, it is a wait that never
		// succeeds. The room above that covers a slower start, and the ceiling
		// still leaves the first attempt inside the caller's own patience.
		c.WakeBudget = 100 * time.Second
	}
	if c.WakeProbeInterval <= 0 {
		c.WakeProbeInterval = 5 * time.Second
	}
	if c.ReadyBackoff <= 0 {
		c.ReadyBackoff = 2 * time.Second
	}
	if c.ThrottleBackoff <= 0 {
		// Three attempts then span roughly 90 to 135 seconds, which outlasts a
		// platform that parks an idle service and takes tens of seconds to bring
		// it back — the case this exists for.
		c.ThrottleBackoff = 30 * time.Second
	}
	if c.BookkeepingTimeout <= 0 {
		c.BookkeepingTimeout = 15 * time.Second
	}

	if c.ClaimStaleAfter <= 0 {
		// Derived, not chosen. Reclaiming a job whose worker is merely slow runs
		// it twice, so the threshold has to clear the longest an attempt can
		// legitimately take — the attempt itself, plus a wait for a sleeping
		// upstream, plus writing the outcome down — with room to spare. Setting
		// it by hand is how it silently falls under that sum when one of the
		// three grows.
		longest := c.JobTimeout + c.WakeBudget + c.BookkeepingTimeout
		c.ClaimStaleAfter = 2 * longest
	}
	if c.DepthInterval <= 0 {
		c.DepthInterval = 15 * time.Second
	}
	if c.PurgeInterval <= 0 {
		// This paces two different things: sweeping old rows, which could happen
		// daily, and closing jobs whose worker died, which is somebody waiting.
		// The second one sets the interval.
		c.PurgeInterval = 2 * time.Minute
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
	queue     Queue
	analyzer  services.Analyzer
	readiness Readiness
	metrics   *observability.Metrics
	logger    *slog.Logger
	cfg       Config
}

func New(
	queue Queue,
	analyzer services.Analyzer,
	readiness Readiness,
	metrics *observability.Metrics,
	logger *slog.Logger,
	cfg Config,
) *Worker {
	return &Worker{
		queue:     queue,
		analyzer:  analyzer,
		readiness: readiness,
		metrics:   metrics,
		logger:    logger,
		cfg:       cfg.withDefaults(),
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
	runCtx, cancelRun := context.WithTimeout(context.WithoutCancel(ctx), w.cfg.JobTimeout)
	runCtx = observability.ContextWithLogger(runCtx, logger)

	analysis, err := w.analyzer.Analyze(runCtx, services.AnalyzeRequest{
		CV:       bytes.NewReader(job.CV),
		Filename: job.CVFilename,
		JobOffer: job.JobOffer,
		JobTitle: job.JobTitle,
	})
	elapsed := time.Since(started)
	cancelRun()

	// A throttled upstream is usually one that is asleep rather than busy, and
	// retrying blindly bounces off the proxy in front of it without ever waking
	// it. A readiness probe is a request that does get through, so it is both
	// the question and the thing that starts the answer.
	//
	// This runs before the bookkeeping context is opened, not after: the wait
	// lasts up to a minute and that context is measured in seconds.
	ready := false
	if err != nil && upstreamThrottled(err) {
		ready = w.waitForUpstream(ctx, logger)
	}

	// Recording the outcome gets a context of its own, and this is not a detail.
	// When the failure *is* the deadline, the work context is already dead, and
	// writing the result through it fails too — leaving the job stuck as running
	// until the stale sweep reclaims it minutes later. Every timeout became a
	// five-minute stall that way.
	bookCtx, cancelBook := context.WithTimeout(
		observability.ContextWithLogger(context.WithoutCancel(ctx), logger),
		w.cfg.BookkeepingTimeout,
	)
	defer cancelBook()

	if err != nil {
		w.handleFailure(bookCtx, job, err, elapsed, ready, logger)
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
		w.finish(bookCtx, job, "internal_error", "The analysis could not be stored.", elapsed, logger)
		return
	}

	if err := w.queue.Complete(bookCtx, job.ID, encoded); err != nil {
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
	upstreamReady bool,
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

	// A throttled upstream is not a flaky one: it is telling us it needs room.
	// Retrying on the ordinary schedule is what turns one cold start into three
	// refusals.
	base := w.cfg.BaseBackoff
	if upstreamThrottled(cause) {
		base = w.cfg.ThrottleBackoff
	}

	delay := backoff(job.Attempts, base, w.cfg.MaxBackoff)
	if upstreamReady {
		// It has since said it is ready. Waiting out a backoff sized for an
		// upstream that needs time would only delay an attempt that is now
		// expected to work.
		delay = w.cfg.ReadyBackoff
	}
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

		abandoned, err := w.queue.FailAbandoned(ctx, w.cfg.ClaimStaleAfter)
		switch {
		case err != nil && ctx.Err() == nil:
			w.logger.Warn("failing abandoned jobs failed", slog.Any("error", err))
		case abandoned > 0:
			// Worth a warning rather than an info line: every one of these is
			// somebody who asked for an analysis and would otherwise still be
			// waiting for it.
			w.logger.Warn("failed jobs abandoned by a dead worker", slog.Int64("count", abandoned))
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

// waitForUpstream polls readiness until the upstream answers or the budget runs
// out, and reports whether it came back. The wait happens on a worker slot on
// purpose: the job is already claimed, and holding it is cheaper than releasing
// it only to reclaim it moments later.
func (w *Worker) waitForUpstream(ctx context.Context, logger *slog.Logger) bool {
	if w.readiness == nil {
		return false
	}

	started := time.Now()

	// The budget bounds the whole wait, probes included. A probe is deliberately
	// allowed to block for most of it — holding the request open is what keeps a
	// scale-to-zero platform starting up, and hanging up resets that — so a
	// budget that only gated the gaps between probes would not bound anything.
	waitCtx, cancel := context.WithTimeout(ctx, w.cfg.WakeBudget)
	defer cancel()

	for waitCtx.Err() == nil {
		if err := w.readiness.Ready(waitCtx); err == nil {
			logger.Info("upstream came back", slog.Duration("waited", time.Since(started)))
			return true
		}
		if !sleep(waitCtx, w.cfg.WakeProbeInterval) {
			break
		}
	}

	logger.Warn("upstream did not come back", slog.Duration("waited", time.Since(started)))
	return false
}

func upstreamThrottled(err error) bool {
	var upstream *services.UpstreamError
	return errors.As(err, &upstream) && upstream.IsThrottled()
}

func describe(err error) (code, message string) {
	var upstream *services.UpstreamError
	if errors.As(err, &upstream) {
		// Classified the same way the request path classifies it. A 429 that
		// broke the error envelope still arrived as throttling, and recording it
		// as a generic upstream fault would hide a capacity problem behind a
		// code that says nothing.
		if upstream.IsThrottled() {
			return "upstream_rate_limited", fmt.Sprintf(
				"upstream status %d: %s", upstream.StatusCode, upstream.Body,
			)
		}
		if upstream.Code == "ai_service_error" {
			// The client localises by code, so this message is only ever read
			// while diagnosing — which is exactly when the status and what the
			// upstream actually said are the only things worth having.
			return upstream.Code, fmt.Sprintf(
				"upstream status %d: %s", upstream.StatusCode, upstream.Body,
			)
		}
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
