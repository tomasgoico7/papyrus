package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/papyrus/gateway/internal/middleware"
)

type countedShed struct{ n atomic.Int64 }

func (c *countedShed) RecordShed(string) { c.n.Add(1) }

// limitedEngine puts two routes behind one ceiling of two, with handlers that
// hold their slot until released — standing in for a model call in progress.
func limitedEngine(shed middleware.ShedRecorder) (*gin.Engine, chan struct{}) {
	gin.SetMode(gin.TestMode)
	release := make(chan struct{})
	hold := func(c *gin.Context) {
		<-release
		c.Status(http.StatusOK)
	}

	limit := middleware.Limit(2, 5, shed)
	engine := gin.New()
	engine.POST("/analyze", limit, hold)
	engine.POST("/tailor", limit, hold)
	return engine, release
}

func send(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
	return rec
}

// occupy starts n requests and waits until they are all inside the handler.
func occupy(t *testing.T, engine *gin.Engine, paths ...string) *sync.WaitGroup {
	t.Helper()
	var wg sync.WaitGroup
	for _, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			send(engine, path)
		}()
	}
	// Give them a moment to take their slots; nothing here waits on the clock
	// for correctness, only to let the goroutines get scheduled.
	time.Sleep(50 * time.Millisecond)
	return &wg
}

func TestTheRequestPastTheCeilingIsTurnedAwayAtOnce(t *testing.T) {
	shed := &countedShed{}
	engine, release := limitedEngine(shed)
	busy := occupy(t, engine, "/analyze", "/analyze")

	started := time.Now()
	rec := send(engine, "/analyze")
	took := time.Since(started)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 with both slots taken", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "5" {
		t.Errorf("Retry-After = %q, want 5", rec.Header().Get("Retry-After"))
	}
	// Turned away, not queued: waiting here would be waiting on the same model
	// as everyone else, with a browser that has its own limit on patience.
	if took > 500*time.Millisecond {
		t.Errorf("the refusal took %v; it should not wait for a slot", took)
	}
	if shed.n.Load() != 1 {
		t.Errorf("shed = %d, want the refusal counted", shed.n.Load())
	}

	close(release)
	busy.Wait()
}

func TestTheCeilingIsSharedAcrossTheRoutes(t *testing.T) {
	engine, release := limitedEngine(nil)
	// Both slots taken by analyses...
	busy := occupy(t, engine, "/analyze", "/analyze")

	// ...leave none for the tailor. They share an upstream and a memory budget,
	// so a ceiling per route would allow twice what was meant.
	if rec := send(engine, "/tailor"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d on another route with the ceiling reached, want 503", rec.Code)
	}

	close(release)
	busy.Wait()
}

func TestASlotIsFreedWhenTheRequestEnds(t *testing.T) {
	engine, release := limitedEngine(nil)
	close(release) // handlers return at once

	for range 5 {
		if rec := send(engine, "/analyze"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d; sequential requests should never hit the ceiling", rec.Code)
		}
	}
}
