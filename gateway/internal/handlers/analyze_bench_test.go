package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/middleware"
	"github.com/papyrus/gateway/internal/observability"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

// BenchmarkAnalyzeHandler measures what the gateway itself costs per analysis:
// multipart parsing, validation, the round trip to the upstream and encoding the
// reply, with the request-id and metrics middleware mounted. The AI service is a
// local stub, so the number excludes the model call — which is the point. This
// is the slice of latency the gateway can actually change.
//
// JWT verification is left out because it would need a live JWKS endpoint.
func BenchmarkAnalyzeHandler(b *testing.B) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = json.NewEncoder(w).Encode(transport.Analysis{
			Score:         74,
			Verdict:       "moderate",
			Summary:       transport.Localized{En: "Decent fit.", Es: "Encaje razonable."},
			MatchedSkills: transport.LocalizedList{En: []string{"Go"}, Es: []string{"Go"}},
			MissingSkills: transport.LocalizedList{En: []string{"gRPC"}, Es: []string{"gRPC"}},
			Suggestions:   []transport.Suggestion{},
		})
	}))
	defer ai.Close()

	gin.SetMode(gin.ReleaseMode)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := observability.NewMetrics()

	engine := gin.New()
	engine.Use(gin.Recovery(), middleware.RequestID(logger), metrics.Middleware())
	engine.POST("/analyze", handlers.NewAnalyzeHandler(
		services.NewAnalyzerClient(ai.URL, "", ai.Client()),
		5<<20,
		10*time.Second,
	).Handle)

	payload, contentType := benchPayload(b)

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		req := httptest.NewRequest(http.MethodPost, "/analyze", bytes.NewReader(payload))
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()

		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			b.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
	}
}

// benchPayload builds a request body once: a CV of a realistic size and a job
// posting long enough to clear validation.
func benchPayload(b *testing.B) ([]byte, string) {
	b.Helper()

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	part, err := writer.CreateFormFile("cv", "candidate.pdf")
	if err != nil {
		b.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("%PDF-1.4 sample bytes "), 4096)); err != nil {
		b.Fatalf("write cv: %v", err)
	}
	if err := writer.WriteField("jobOffer", strings.Repeat("Backend role needing Go and Postgres. ", 20)); err != nil {
		b.Fatalf("write job offer: %v", err)
	}
	if err := writer.Close(); err != nil {
		b.Fatalf("close writer: %v", err)
	}

	return buffer.Bytes(), writer.FormDataContentType()
}
