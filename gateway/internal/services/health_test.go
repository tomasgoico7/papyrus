package services_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/requestid"
	"github.com/papyrus/gateway/internal/services"
)

func TestReadyAcceptsAHealthyUpstream(t *testing.T) {
	var gotPath, gotAuth, gotCorrelation string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-Internal-Token")
		gotCorrelation = r.Header.Get(requestid.Header)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ai.Close()

	client := services.NewHealthClient(ai.URL+"/", ai.Client(), time.Second)
	ctx := requestid.NewContext(context.Background(), "abc123")

	if err := client.Ready(ctx); err != nil {
		t.Fatalf("ready: %v", err)
	}

	if gotPath != "/health" {
		t.Errorf("path = %q, want /health", gotPath)
	}
	// Deliberately unauthenticated: a probe that could be rejected on
	// credentials would say nothing about whether the service is up.
	if gotAuth != "" {
		t.Errorf("the probe sent a token (%q); readiness must not depend on one", gotAuth)
	}
	if gotCorrelation != "abc123" {
		t.Errorf("correlation id = %q, want it carried onto the probe", gotCorrelation)
	}
}

func TestReadyRejectsAnUpstreamThatIsNotServing(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable} {
		ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))

		client := services.NewHealthClient(ai.URL, ai.Client(), time.Second)
		if err := client.Ready(context.Background()); err == nil {
			t.Errorf("status %d reported as ready", status)
		}
		ai.Close()
	}
}

func TestReadyRejectsAnUnreachableUpstream(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := services.NewHealthClient(ai.URL, ai.Client(), time.Second)
	ai.Close()

	if err := client.Ready(context.Background()); err == nil {
		t.Error("expected an error when the upstream is not listening")
	}
}

func TestReadyGivesUpOnItsOwnTimeout(t *testing.T) {
	blocked := make(chan struct{})
	ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-blocked
	}))
	defer func() { close(blocked); ai.Close() }()

	client := services.NewHealthClient(ai.URL, ai.Client(), 50*time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- client.Ready(context.Background()) }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("a probe that never answered was reported as ready")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the probe ignored its own timeout")
	}
}
