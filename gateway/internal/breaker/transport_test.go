package breaker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/breaker"
)

func guarded(threshold int) (*http.Client, *breaker.Breaker) {
	b := breaker.New(breaker.Config{FailureThreshold: threshold, Cooldown: time.Hour}, nil)
	return &http.Client{Transport: &breaker.Transport{Breaker: b}}, b
}

func TestServerErrorsAndThrottlingCountAgainstTheUpstream(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		client, b := guarded(2)

		for range 2 {
			resp, err := client.Get(upstream.URL)
			if err != nil {
				t.Fatalf("status %d: get: %v", status, err)
			}
			resp.Body.Close()
		}
		if b.State() != breaker.Open {
			t.Errorf("status %d twice: state = %v, want open", status, b.State())
		}
		upstream.Close()
	}
}

func TestAClientErrorIsNotTheUpstreamsFault(t *testing.T) {
	// A rejected token or an unreadable upload is the caller's problem. An
	// upstream that says so clearly is working, and a run of bad requests must
	// not switch it off for everyone else.
	for _, status := range []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusBadRequest} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		}))
		client, b := guarded(2)

		for range 5 {
			resp, err := client.Get(upstream.URL)
			if err != nil {
				t.Fatalf("status %d: get: %v", status, err)
			}
			resp.Body.Close()
		}
		if b.State() != breaker.Closed {
			t.Errorf("status %d: state = %v, want closed", status, b.State())
		}
		upstream.Close()
	}
}

func TestAnOpenBreakerNeverSendsTheRequest(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()

	client, _ := guarded(1)
	resp, err := client.Get(upstream.URL)
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	resp.Body.Close()

	_, err = client.Get(upstream.URL)
	// Through http.Client the refusal arrives wrapped in a url.Error; callers
	// test for it with errors.Is, so it has to survive the wrapping.
	if !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("err = %v, want ErrOpen through the client", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("upstream received %d requests; the refused one should never have left", got)
	}
}

func TestACancelledRequestSaysNothing(t *testing.T) {
	blocked := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
	}))
	defer func() { close(blocked); upstream.Close() }()

	client, b := guarded(1)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected the cancelled request to fail")
	}

	// The caller walked away. Counting that against the upstream would let one
	// impatient client open the breaker for everyone.
	if b.State() != breaker.Closed {
		t.Errorf("state = %v after a cancelled request, want closed", b.State())
	}
}

func TestATimeoutCountsAgainstTheUpstream(t *testing.T) {
	blocked := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-blocked:
		case <-r.Context().Done():
		}
	}))
	defer func() { close(blocked); upstream.Close() }()

	client, b := guarded(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL, nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("expected the request to time out")
	}

	// Unlike a cancellation, a deadline passing is the upstream not answering
	// in the time it was given.
	if b.State() != breaker.Open {
		t.Errorf("state = %v after a timeout, want open", b.State())
	}
}
