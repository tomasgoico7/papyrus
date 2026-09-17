package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/handlers"
	"github.com/papyrus/gateway/internal/services"
)

func newTailorEngine(aiURL string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	client := services.NewTailorClient(aiURL, "", &http.Client{})
	handler := handlers.NewTailorHandler(client, 5<<20, 10*time.Second, discardLogger())

	engine := gin.New()
	engine.POST("/tailor/questions", handler.Questions)
	engine.POST("/tailor/generate", handler.Generate)
	return engine
}

func TestTailorQuestionsForwardsUpstreamBody(t *testing.T) {
	upstream := `{"questions":[{"topic":"Kubernetes","question":"Do you?"}],"cvText":"cv text"}`
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("ai service could not parse forwarded form: %v", err)
		}
		if r.FormValue("jobOffer") == "" {
			t.Error("expected jobOffer to be forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstream)
	}))
	defer ai.Close()

	body, contentType := buildMultipart(
		t, "cv.pdf", "%PDF-1.4", strings.Repeat("Long enough job description text. ", 2),
	)
	req := httptest.NewRequest(http.MethodPost, "/tailor/questions", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newTailorEngine(ai.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != upstream {
		t.Errorf("expected the upstream body verbatim, got %q", rec.Body.String())
	}
}

func TestTailorGenerateForwardsJSON(t *testing.T) {
	upstream := `{"fullName":"Tomas","skills":["Backend: Go"]}`
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(data), "cvText") {
			t.Error("expected the JSON body to be forwarded")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstream)
	}))
	defer ai.Close()

	payload := `{"cvText":"my cv","jobOffer":"a sufficiently long job offer","answers":[]}`
	req := httptest.NewRequest(http.MethodPost, "/tailor/generate", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newTailorEngine(ai.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != upstream {
		t.Errorf("expected the upstream body verbatim, got %q", rec.Body.String())
	}
}

func TestTailorQuestionsRejectsShortJobOffer(t *testing.T) {
	body, contentType := buildMultipart(t, "cv.pdf", "%PDF-1.4", "too short")
	req := httptest.NewRequest(http.MethodPost, "/tailor/questions", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newTailorEngine("http://unused").ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a short job offer, got %d", rec.Code)
	}
}

func TestTailorPropagatesUpstreamClientError(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":{"code":"unreadable_cv","message":"The CV could not be read."}}`)
	}))
	defer ai.Close()

	body, contentType := buildMultipart(
		t, "cv.pdf", "junk", strings.Repeat("Long enough job offer text here. ", 2),
	)
	req := httptest.NewRequest(http.MethodPost, "/tailor/questions", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	newTailorEngine(ai.URL).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected upstream 422 to be propagated, got %d", rec.Code)
	}
}
