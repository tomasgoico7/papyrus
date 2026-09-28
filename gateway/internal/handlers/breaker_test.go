package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/breaker"
	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/services"
)

// TestAnOpenBreakerAnswersAtOnceWithWhenToComeBack is the request path's half
// of the breaker. Once the upstream is known to be down, each new caller used to
// wait out the same failure the last one did; now it is told immediately, with
// a status that says the fault is temporary and a header that says when to try.
func TestAnOpenBreakerAnswersAtOnceWithWhenToComeBack(t *testing.T) {
	var hits atomic.Int64
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ai.Close()

	guard := breaker.New(breaker.Config{FailureThreshold: 1, Cooldown: time.Minute}, nil)
	client := &http.Client{Transport: &breaker.Transport{Breaker: guard}}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/analyze", handlers.NewAnalyzeHandler(
		services.NewAnalyzerClient(ai.URL, "", client), 5<<20, 10*time.Second).Handle)

	send := func() *httptest.ResponseRecorder {
		body, contentType := buildMultipart(t, "cv.pdf", "%PDF-1.4",
			strings.Repeat("Some sufficiently long job description text. ", 2))
		req := httptest.NewRequest(http.MethodPost, "/analyze", body)
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	// The failure that opens it.
	send()

	started := time.Now()
	rec := send()
	took := time.Since(started)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 while the breaker is open", rec.Code)
	}
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retry < 1 || retry > 60 {
		t.Errorf("Retry-After = %q, want the seconds left of the cooldown", rec.Header().Get("Retry-After"))
	}

	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// A code the client already maps to a message; a new one would fall through
	// to the generic error.
	if envelope.Error.Code != "upstream_unavailable" {
		t.Errorf("code = %q, want upstream_unavailable", envelope.Error.Code)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("the upstream saw %d requests; the refused one should never have reached it", got)
	}
	if took > time.Second {
		t.Errorf("the refusal took %v; it should not wait on anything", took)
	}
}
