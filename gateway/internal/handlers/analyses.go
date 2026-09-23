package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/requestid"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// AnalysisLookup reports what the cache already knows about a request, without
// doing the work.
type AnalysisLookup interface {
	Lookup(ctx context.Context, req services.AnalyzeRequest) (services.Lookup, error)
}

// JobQueue is the slice of the job store this handler needs.
type JobQueue interface {
	Enqueue(ctx context.Context, input jobs.NewJob) (*jobs.Job, bool, error)
	Get(ctx context.Context, id, userID string) (*jobs.Job, error)
}

// Postgres rejects a malformed uuid with an error, which would surface as a 500
// for what is really a bad path parameter.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// AnalysesHandler serves the asynchronous analyze endpoints: submitting returns
// immediately, and the result is collected later.
type AnalysesHandler struct {
	lookup         AnalysisLookup
	queue          JobQueue
	maxUploadBytes int64
	requestTimeout time.Duration
}

func NewAnalysesHandler(
	lookup AnalysisLookup,
	queue JobQueue,
	maxUploadBytes int64,
	requestTimeout time.Duration,
) *AnalysesHandler {
	return &AnalysesHandler{
		lookup:         lookup,
		queue:          queue,
		maxUploadBytes: maxUploadBytes,
		requestTimeout: requestTimeout,
	}
}

// Submit accepts an analysis and answers without waiting for the model.
//
// A result the cache already holds comes back as a 200 on the spot: there is no
// job to create for work that is already done. Everything else is queued and
// answered with a 202 and somewhere to look.
func (h *AnalysesHandler) Submit(c *gin.Context) {
	userID := httpx.UserID(c)
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, "unauthorized", "A valid access token is required.")
		return
	}

	upload, ok := parseAnalysisUpload(c, h.maxUploadBytes)
	if !ok {
		return
	}
	defer upload.File.Close()

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()
	logger := observability.LoggerFrom(ctx)

	found, err := h.lookup.Lookup(ctx, services.AnalyzeRequest{
		CV:       upload.File,
		Filename: upload.Filename,
		JobOffer: upload.JobOffer,
		JobTitle: upload.JobTitle,
	})
	if err != nil {
		logger.Error("reading the upload failed", slog.Any("error", err))
		httpx.RespondError(c, http.StatusInternalServerError, "upload_error", "The CV file could not be read.")
		return
	}

	if found.Analysis != nil {
		c.JSON(http.StatusOK, transport.AnalysisResponse{
			Analysis:   *found.Analysis,
			CVFilename: upload.Filename,
		})
		return
	}

	job, created, err := h.queue.Enqueue(ctx, jobs.NewJob{
		UserID:     userID,
		DedupKey:   dedupKey(found.Key),
		CV:         found.CV,
		CVFilename: upload.Filename,
		JobOffer:   upload.JobOffer,
		JobTitle:   upload.JobTitle,
		// Stored so the worker can continue this trace minutes from now, in
		// another process, rather than starting an unrelated one.
		TraceParent: observability.TraceParentFrom(ctx),
	})
	if err != nil {
		logger.Error("queueing the analysis failed", slog.Any("error", err))
		httpx.RespondError(c, http.StatusServiceUnavailable, "queue_unavailable",
			"The analysis could not be queued. Please try again in a moment.")
		return
	}

	logger.Info("analysis queued",
		slog.String("job_id", job.ID),
		slog.Bool("created", created),
	)

	// A resubmission joins the job already in flight rather than starting a
	// second one, and is answered identically: the caller polls the same place.
	c.Header("Location", "/analyses/"+job.ID)
	c.JSON(http.StatusAccepted, transport.JobAccepted{
		JobID:  job.ID,
		Status: string(job.State),
	})
}

// Status reports where a job has got to, and carries the result once there is
// one.
//
// A finished job that failed is still a successful read, so the outcome travels
// in the body rather than in the status code: the caller asked how the job went
// and got a complete answer.
func (h *AnalysesHandler) Status(c *gin.Context) {
	userID := httpx.UserID(c)
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, "unauthorized", "A valid access token is required.")
		return
	}

	id := c.Param("id")
	if !uuidPattern.MatchString(id) {
		httpx.RespondError(c, http.StatusNotFound, "job_not_found", "No such analysis.")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.requestTimeout)
	defer cancel()

	job, err := h.queue.Get(ctx, id, userID)
	if err != nil {
		if errors.Is(err, jobs.ErrNotFound) {
			// The same answer whether it never existed or belongs to somebody
			// else: the difference is itself worth hiding.
			httpx.RespondError(c, http.StatusNotFound, "job_not_found", "No such analysis.")
			return
		}
		observability.LoggerFrom(ctx).Error("reading the job failed", slog.Any("error", err))
		httpx.RespondError(c, http.StatusServiceUnavailable, "queue_unavailable",
			"The analysis status could not be read. Please try again in a moment.")
		return
	}

	status := transport.JobStatus{
		JobID:   job.ID,
		Status:  string(job.State),
		Attempt: job.Attempts,
	}

	switch job.State {
	case jobs.StateDone:
		var result transport.AnalysisResponse
		if err := json.Unmarshal(job.Result, &result); err != nil {
			observability.LoggerFrom(ctx).Error("the stored result could not be decoded",
				slog.String("job_id", job.ID),
				slog.Any("error", err),
			)
			httpx.RespondError(c, http.StatusInternalServerError, "analysis_failed",
				"The finished analysis could not be read back.")
			return
		}
		status.Result = &result

	case jobs.StateFailed:
		status.Error = &transport.JobError{
			Code:    orDefault(job.ErrorCode, "analysis_failed"),
			Message: orDefault(job.ErrorMessage, "The analysis could not be completed."),
		}
	}

	c.JSON(http.StatusOK, status)
}

// dedupKey falls back to a value nothing else can match. A request whose cache
// key could not be built still has to run; it just must not be folded into
// another request on the strength of a key that was never trustworthy.
func dedupKey(key string) string {
	if key != "" {
		return key
	}
	return "unkeyed:" + requestid.New()
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
