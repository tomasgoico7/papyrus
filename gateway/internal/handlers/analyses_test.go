package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/httpx"
	"github.com/papyrus/gateway/internal/jobs"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

const (
	ownerID   = "user-owner"
	strangeID = "user-stranger"
	jobUUID   = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

type stubLookup struct {
	analysis *transport.Analysis
	key      string
	err      error
}

func (s stubLookup) Lookup(_ context.Context, req services.AnalyzeRequest) (services.Lookup, error) {
	if s.err != nil {
		return services.Lookup{}, s.err
	}
	cv := make([]byte, 0, 32)
	buf := make([]byte, 32)
	for {
		n, err := req.CV.Read(buf)
		cv = append(cv, buf[:n]...)
		if err != nil {
			break
		}
	}
	return services.Lookup{Key: s.key, CV: cv, Analysis: s.analysis}, nil
}

type stubQueue struct {
	enqueued   []jobs.NewJob
	returned   *jobs.Job
	created    bool
	enqueueErr error

	job    *jobs.Job
	getErr error
	gotFor string
}

func (q *stubQueue) Enqueue(_ context.Context, input jobs.NewJob) (*jobs.Job, bool, error) {
	if q.enqueueErr != nil {
		return nil, false, q.enqueueErr
	}
	q.enqueued = append(q.enqueued, input)
	return q.returned, q.created, nil
}

func (q *stubQueue) Get(_ context.Context, id, userID string) (*jobs.Job, error) {
	q.gotFor = userID
	if q.getErr != nil {
		return nil, q.getErr
	}
	if q.job == nil || q.job.ID != id {
		return nil, jobs.ErrNotFound
	}
	return q.job, nil
}

// analysesEngine mounts the handler behind a stand-in for the auth middleware,
// so the tests exercise the same "who is asking" plumbing the real routes use.
func analysesEngine(lookup handlers.AnalysisLookup, queue handlers.JobQueue, as string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := handlers.NewAnalysesHandler(lookup, queue, 5<<20, 10*time.Second)

	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		if as != "" {
			c.Set(httpx.ContextUserID, as)
		}
		c.Next()
	})
	engine.POST("/analyses", handler.Submit)
	engine.GET("/analyses/:id", handler.Status)
	return engine
}

func submit(t *testing.T, engine *gin.Engine) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := buildMultipart(
		t, "cv.pdf", "%PDF-1.4 bytes",
		strings.Repeat("Backend role needing Go and Postgres. ", 2),
	)
	req := httptest.NewRequest(http.MethodPost, "/analyses", body)
	req.Header.Set("Content-Type", contentType)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

func queuedJob() *jobs.Job {
	return &jobs.Job{ID: jobUUID, UserID: ownerID, State: jobs.StateQueued}
}

func TestSubmitQueuesAnAnalysisAndSaysWhereToLook(t *testing.T) {
	queue := &stubQueue{returned: queuedJob(), created: true}
	rec := submit(t, analysesEngine(stubLookup{key: "cache-key"}, queue, ownerID))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/analyses/"+jobUUID {
		t.Errorf("Location = %q", got)
	}

	var accepted transport.JobAccepted
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if accepted.JobID != jobUUID || accepted.Status != "queued" {
		t.Errorf("accepted = %+v", accepted)
	}

	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
	input := queue.enqueued[0]
	if input.UserID != ownerID {
		t.Errorf("user = %q, want the authenticated subject", input.UserID)
	}
	// The dedup key is the cache key: the two have to agree on what counts as
	// the same analysis, or a queued job and a cached result drift apart.
	if input.DedupKey != "cache-key" {
		t.Errorf("dedup key = %q, want the cache key", input.DedupKey)
	}
	if string(input.CV) != "%PDF-1.4 bytes" {
		t.Errorf("cv = %q, want the upload carried into the job", input.CV)
	}
}

func TestSubmitAnswersImmediatelyWhenTheResultIsAlreadyKnown(t *testing.T) {
	cached := &transport.Analysis{
		Score:   82,
		Verdict: "strong",
		Summary: transport.Localized{En: "Strong.", Es: "Fuerte."},
	}
	queue := &stubQueue{returned: queuedJob(), created: true}

	rec := submit(t, analysesEngine(stubLookup{key: "k", analysis: cached}, queue, ownerID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for a result the cache already holds", rec.Code)
	}
	if len(queue.enqueued) != 0 {
		t.Error("there is no job to create for work that is already done")
	}

	var result transport.AnalysisResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Score != 82 || result.CVFilename != "cv.pdf" {
		t.Errorf("result = %+v", result)
	}
}

func TestSubmitGivesAnUnkeyableRequestAKeyOfItsOwn(t *testing.T) {
	queue := &stubQueue{returned: queuedJob(), created: true}

	// The cache could not build a key — the prompt version was unreachable. The
	// job must still run, and must not be folded into anything else.
	rec := submit(t, analysesEngine(stubLookup{key: ""}, queue, ownerID))

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	key := queue.enqueued[0].DedupKey
	if key == "" {
		t.Fatal("a job still needs a dedup key")
	}
	if !strings.HasPrefix(key, "unkeyed:") {
		t.Errorf("dedup key = %q, want one that cannot collide with a cache key", key)
	}
}

func TestSubmitRejectsAnUnauthenticatedCaller(t *testing.T) {
	queue := &stubQueue{returned: queuedJob()}
	rec := submit(t, analysesEngine(stubLookup{}, queue, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if len(queue.enqueued) != 0 {
		t.Error("nothing should be queued without a subject to own it")
	}
}

func TestSubmitReportsAQueueItCannotReach(t *testing.T) {
	queue := &stubQueue{enqueueErr: errors.New("database is down")}
	rec := submit(t, analysesEngine(stubLookup{key: "k"}, queue, ownerID))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 — the request is fine, the queue is not", rec.Code)
	}
}

func TestStatusCarriesTheResultOfAFinishedJob(t *testing.T) {
	stored, _ := json.Marshal(transport.AnalysisResponse{
		Analysis:   transport.Analysis{Score: 74, Verdict: "moderate"},
		CVFilename: "cv.pdf",
	})
	queue := &stubQueue{job: &jobs.Job{
		ID: jobUUID, UserID: ownerID, State: jobs.StateDone, Attempts: 1, Result: stored,
	}}

	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, queue, ownerID).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/"+jobUUID, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var status transport.JobStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.Status != "done" || status.Result == nil || status.Result.Score != 74 {
		t.Errorf("status = %+v", status)
	}
	if status.Error != nil {
		t.Error("a finished job should not carry an error")
	}
}

func TestStatusReportsAFailureInTheBodyNotTheStatusCode(t *testing.T) {
	queue := &stubQueue{job: &jobs.Job{
		ID: jobUUID, UserID: ownerID, State: jobs.StateFailed, Attempts: 3,
		ErrorCode: "unreadable_cv", ErrorMessage: "The CV is not readable.",
	}}

	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, queue, ownerID).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/"+jobUUID, nil))

	// Reading the job succeeded; the job is what failed.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var status transport.JobStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &status)

	if status.Status != "failed" || status.Error == nil {
		t.Fatalf("status = %+v", status)
	}
	if status.Error.Code != "unreadable_cv" {
		t.Errorf("error code = %q, want the reason preserved", status.Error.Code)
	}
	if status.Result != nil {
		t.Error("a failed job should not carry a result")
	}
}

// TestStatusScopesTheLookupToTheCaller is the one that matters: the worker
// writes past RLS, so this handler is the only thing standing between one
// user's job id and another user's CV.
func TestStatusScopesTheLookupToTheCaller(t *testing.T) {
	queue := &stubQueue{job: &jobs.Job{ID: jobUUID, UserID: ownerID, State: jobs.StateQueued}}

	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, queue, strangeID).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/"+jobUUID, nil))

	if queue.gotFor != strangeID {
		t.Errorf("the store was queried for %q, want the caller's own id", queue.gotFor)
	}
}

func TestStatusHidesJobsThatAreNotYours(t *testing.T) {
	queue := &stubQueue{getErr: jobs.ErrNotFound}

	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, queue, strangeID).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/"+jobUUID, nil))

	// Not 403: saying "forbidden" would confirm the id exists.
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestStatusRejectsAnIDThatIsNotAnIdentifier(t *testing.T) {
	queue := &stubQueue{}

	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, queue, ownerID).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/not-a-uuid", nil))

	// Postgres would reject the malformed value with an error, which would
	// surface as a 500 for what is really a bad path parameter.
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if queue.gotFor != "" {
		t.Error("a malformed id should never reach the database")
	}
}

func TestStatusRejectsAnUnauthenticatedCaller(t *testing.T) {
	rec := httptest.NewRecorder()
	analysesEngine(stubLookup{}, &stubQueue{}, "").
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/analyses/"+jobUUID, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
