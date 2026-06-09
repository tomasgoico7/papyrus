package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

func newEngine(aiURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	client := services.NewAnalyzerClient(aiURL, "", &http.Client{})
	handler := handlers.NewAnalyzeHandler(client, 5<<20, 10*time.Second)

	engine := gin.New()
	engine.POST("/analyze", handler.Handle)
	return engine
}

func buildMultipart(t *testing.T, filename, fileBody, jobOffer string) (io.Reader, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	part, err := writer.CreateFormFile("cv", filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte(fileBody)); err != nil {
		t.Fatalf("write file body: %v", err)
	}
	if err := writer.WriteField("jobOffer", jobOffer); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buffer, writer.FormDataContentType()
}

func TestAnalyzeHandlerForwardsAndAugments(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ai service could not parse forwarded form: %v", err)
		}
		if r.FormValue("jobOffer") == "" {
			t.Error("expected jobOffer to be forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(transport.Analysis{
			Score:         80,
			Verdict:       "strong",
			Summary:       transport.Localized{En: "Strong overlap.", Es: "Buena coincidencia."},
			MatchedSkills: transport.LocalizedList{En: []string{"Go"}, Es: []string{"Go"}},
			MissingSkills: transport.LocalizedList{En: []string{}, Es: []string{}},
			Suggestions:   []transport.Suggestion{},
		})
	}))
	defer ai.Close()

	body, contentType := buildMultipart(
		t,
		"jane-resume.pdf",
		"%PDF-1.4 minimal",
		strings.Repeat("Backend role needing Go and Postgres. ", 3),
	)
	req := httptest.NewRequest(http.MethodPost, "/analyze", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newEngine(ai.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp transport.AnalysisResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Score != 80 {
		t.Errorf("expected score 80, got %d", resp.Score)
	}
	if resp.CVFilename != "jane-resume.pdf" {
		t.Errorf("expected gateway to add the filename, got %q", resp.CVFilename)
	}
}

func TestAnalyzeHandlerRejectsShortJobOffer(t *testing.T) {
	body, contentType := buildMultipart(t, "cv.pdf", "%PDF-1.4", "too short")
	req := httptest.NewRequest(http.MethodPost, "/analyze", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newEngine("http://unused").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a short job offer, got %d", rec.Code)
	}
}

func TestAnalyzeHandlerPropagatesUpstreamClientError(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{
				"code":    "unreadable_cv",
				"message": "The CV could not be read.",
			},
		})
	}))
	defer ai.Close()

	body, contentType := buildMultipart(
		t,
		"cv.pdf",
		"not a real pdf",
		strings.Repeat("Some sufficiently long job description text. ", 2),
	)
	req := httptest.NewRequest(http.MethodPost, "/analyze", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newEngine(ai.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected upstream 422 to be propagated, got %d", rec.Code)
	}
}
