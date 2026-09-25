package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestQueuedRoutesLookAbsentWhileTheSchemaIsBehind checks the status, because
// the status is the whole contract: the client falls back to the synchronous
// endpoint on a 404 and on nothing else. Anything but a 404 here would show a
// person an error for an analysis that could still have been done.
func TestQueuedRoutesLookAbsentWhileTheSchemaIsBehind(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name  string
		ready func() bool
		want  int
	}{
		{"schema behind", func() bool { return false }, http.StatusNotFound},
		{"schema in place", func() bool { return true }, http.StatusAccepted},
		// A deployment with no queue passes no gate at all; the routes are not
		// registered then, but the middleware must not be the thing that breaks.
		{"no gate", nil, http.StatusAccepted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := gin.New()
			engine.POST("/analyses", whenReady(tc.ready), func(c *gin.Context) {
				c.Status(http.StatusAccepted)
			})

			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/analyses", nil))

			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
