package services_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/papyrus/gateway/internal/services"
)

// slowUpstream answers /version after a delay, counting how often it is asked.
// The delay stands in for a cold start: the thing the client must be able to
// wait out without making its caller wait out too.
type slowUpstream struct {
	// Atomic because tests change it while the server's goroutines read it,
	// and the race detector cannot see an ordering across a socket.
	delay  atomic.Int64
	prompt atomic.Value
	calls  atomic.Int64
	server *httptest.Server
}

func newSlowUpstream(t *testing.T, delay time.Duration, prompt string) *slowUpstream {
	t.Helper()
	u := &slowUpstream{}
	u.delay.Store(int64(delay))
	u.prompt.Store(prompt)
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		select {
		case <-time.After(time.Duration(u.delay.Load())):
		case <-r.Context().Done():
			// The client hung up. On a real scale-to-zero platform this is what
			// abandons the start, so the test should notice if it happens.
			return
		}
		_ = json.NewEncoder(w).Encode(services.Version{
			PromptVersion: u.prompt.Load().(string),
			Model:         "gemini-test",
		})
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *slowUpstream) client(ttl time.Duration) *services.VersionClient {
	return services.NewVersionClient(u.server.URL, "", u.server.Client(), ttl)
}

// eventually polls until check passes or the deadline expires. The refresh runs
// in the background by design, so its effect has to be waited for rather than
// asserted on the spot.
func eventually(t *testing.T, within time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return check()
}

// TestACallerThatGivesUpDoesNotCancelTheRefresh is the property the enqueue
// path depends on. The request gives up on the version after a couple of
// seconds so the person is not kept waiting — but if giving up also cancelled
// the fetch, the cold start that fetch triggered would be abandoned, and the
// worker picking up the job would find the service still asleep.
func TestACallerThatGivesUpDoesNotCancelTheRefresh(t *testing.T) {
	upstream := newSlowUpstream(t, 300*time.Millisecond, "v1")
	versions := upstream.client(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := versions.Recent(ctx)
	waited := time.Since(started)

	if err == nil {
		t.Fatal("expected an error: nothing was cached and the caller's deadline passed first")
	}
	if waited > 200*time.Millisecond {
		t.Errorf("the caller waited %v; its own deadline was 30ms", waited)
	}

	// The fetch it started should still land.
	landed := eventually(t, 2*time.Second, func() bool {
		quick, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		version, err := versions.Recent(quick)
		return err == nil && version.PromptVersion == "v1"
	})
	if !landed {
		t.Error("the refresh was cancelled along with the caller; the next caller starts from nothing")
	}
	if got := upstream.calls.Load(); got != 1 {
		t.Errorf("upstream asked %d times, want 1: the second caller should have found the result", got)
	}
}

func TestRecentServesAStaleVersionAtOnceWhileItRefreshes(t *testing.T) {
	upstream := newSlowUpstream(t, 0, "v1")
	versions := upstream.client(20 * time.Millisecond)

	if _, err := versions.Current(context.Background()); err != nil {
		t.Fatalf("priming: %v", err)
	}

	// The value expires, the upstream becomes slow, and a new prompt ships.
	time.Sleep(40 * time.Millisecond)
	upstream.delay.Store(int64(300 * time.Millisecond))
	upstream.prompt.Store("v2")

	started := time.Now()
	version, err := versions.Recent(context.Background())
	waited := time.Since(started)

	if err != nil {
		t.Fatalf("a stale value is held, so there should be no error: %v", err)
	}
	// Waiting on a slow upstream to confirm a value that changes only on deploy
	// is what made the enqueue path take twenty four seconds.
	if waited > 100*time.Millisecond {
		t.Errorf("waited %v with a stale value in hand", waited)
	}
	if version.PromptVersion != "v1" {
		t.Errorf("prompt = %q, want the stale v1 served immediately", version.PromptVersion)
	}

	refreshed := eventually(t, 2*time.Second, func() bool {
		version, _ := versions.Recent(context.Background())
		return version.PromptVersion == "v2"
	})
	if !refreshed {
		t.Error("the refresh started in the background never replaced the stale value")
	}
}

func TestConcurrentCallersShareOneFetch(t *testing.T) {
	upstream := newSlowUpstream(t, 100*time.Millisecond, "v1")
	versions := upstream.client(time.Hour)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = versions.Current(context.Background())
		}()
	}
	wg.Wait()

	// Twenty requests arriving while a sleeping service wakes up must not be
	// twenty requests to it. That burst is what trips an edge rate limit.
	if got := upstream.calls.Load(); got != 1 {
		t.Errorf("upstream asked %d times by 20 concurrent callers, want 1", got)
	}
}

func TestAFreshVersionIsNotFetchedAgain(t *testing.T) {
	upstream := newSlowUpstream(t, 0, "v1")
	versions := upstream.client(time.Hour)

	for range 5 {
		if _, err := versions.Current(context.Background()); err != nil {
			t.Fatalf("current: %v", err)
		}
	}
	if got := upstream.calls.Load(); got != 1 {
		t.Errorf("upstream asked %d times inside the ttl, want 1", got)
	}
}

func TestACallerWithTimeWaitsForTheFirstFetch(t *testing.T) {
	// The worker's side: it is about to call the AI service anyway, so it can
	// afford to wait for the version and should get the real one, not an error.
	upstream := newSlowUpstream(t, 100*time.Millisecond, "v1")
	versions := upstream.client(time.Hour)

	version, err := versions.Current(context.Background())
	if err != nil {
		t.Fatalf("a caller with no deadline should wait for the fetch: %v", err)
	}
	if version.PromptVersion != "v1" {
		t.Errorf("prompt = %q, want v1", version.PromptVersion)
	}
}

// TestCurrentWaitsForTheNewVersionAfterAPromptChange is the other half of the
// contract, and the reason there are two methods. The worker stores results
// under the version it read; reading a stale one files the new prompt's answer
// under the old prompt's key, which is exactly what the fingerprint in the key
// exists to prevent.
func TestCurrentWaitsForTheNewVersionAfterAPromptChange(t *testing.T) {
	upstream := newSlowUpstream(t, 0, "v1")
	versions := upstream.client(20 * time.Millisecond)

	if _, err := versions.Current(context.Background()); err != nil {
		t.Fatalf("priming: %v", err)
	}

	time.Sleep(40 * time.Millisecond)
	upstream.prompt.Store("v2")

	version, err := versions.Current(context.Background())
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if version.PromptVersion != "v2" {
		t.Errorf("prompt = %q after the ttl lapsed, want the new v2 rather than the stale v1", version.PromptVersion)
	}
}

func TestCurrentFallsBackToTheHeldValueWhenTheRefreshFails(t *testing.T) {
	upstream := newSlowUpstream(t, 0, "v1")
	versions := upstream.client(20 * time.Millisecond)

	if _, err := versions.Current(context.Background()); err != nil {
		t.Fatalf("priming: %v", err)
	}

	// The upstream goes away. Losing caching over a blip on an endpoint whose
	// answer changes at deploy time would be the worse trade.
	time.Sleep(40 * time.Millisecond)
	upstream.server.Close()

	version, err := versions.Current(context.Background())
	if err != nil {
		t.Fatalf("a held value should cover a failed refresh: %v", err)
	}
	if version.PromptVersion != "v1" {
		t.Errorf("prompt = %q, want the held v1", version.PromptVersion)
	}
}
