package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/handlers"
)

func TestHealthReportsTheBuildItIsServing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/health", handlers.Health)

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Status   string `json:"status"`
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	// The point of the field is telling two deploys apart, so it must always be
	// present — an absent one reads as a build that is somehow older than the
	// field itself.
	if body.Revision == "" {
		t.Error("the health check did not say which build answered it")
	}
}
