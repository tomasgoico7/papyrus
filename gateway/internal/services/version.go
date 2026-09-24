package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/papyrus/gateway/internal/requestid"
)

// versionRefreshTimeout bounds a refresh that nobody is waiting on. It has to
// clear a cold start of the AI service — measured at up to three minutes on a
// free instance — because abandoning the request abandons the start it
// triggered, and the next caller then begins again from nothing.
const versionRefreshTimeout = 4 * time.Minute

// Version describes what an analysis depends on. A cached result is only valid
// for the prompt and model that produced it, so both belong in the cache key.
type Version struct {
	PromptVersion string `json:"promptVersion"`
	Model         string `json:"model"`
}

// VersionClient reads the AI service's version and caches it, the same shape as
// the JWKS cache: a value that only changes on deploy, refreshed on a timer
// rather than fetched per request.
type VersionClient struct {
	baseURL string
	token   string
	http    *http.Client
	ttl     time.Duration

	mu      sync.RWMutex
	cached  Version
	fetched time.Time

	// refresh collapses concurrent fetches into one. Without it every request
	// arriving while the value is stale starts its own, and against a sleeping
	// upstream that is a burst of identical requests at the worst moment.
	refresh singleflight.Group
}

func NewVersionClient(baseURL, token string, client *http.Client, ttl time.Duration) *VersionClient {
	return &VersionClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    client,
		ttl:     ttl,
	}
}

// Current returns an up-to-date version, waiting for a refresh when the held
// one has expired.
//
// This is for whoever writes to the cache. A result stored under a stale
// fingerprint is filed under the wrong prompt — the new prompt's answer kept
// where the old prompt's key points — so the caller that produces analyses
// waits for the real value. It is about to call the AI service anyway, so the
// wait costs it nothing it was not already spending.
//
// When the refresh fails and a previous value is held, that value is returned
// rather than an error. Losing caching entirely over a blip on an endpoint whose
// answer changes at deploy time is the worse trade, and the ttl bounds how long
// a genuinely outdated fingerprint can stay in use.
func (c *VersionClient) Current(ctx context.Context) (Version, error) {
	return c.get(ctx, false)
}

// Recent returns whatever version is held, however old, and refreshes it in
// the background. With nothing held yet, it waits up to the caller's deadline.
//
// This is for the request path, which must not wait on the AI service. The
// answer only changes on deploy, so an expired value is almost always the
// right one, and when it is not the cost is one cache miss. Waiting on a
// sleeping upstream to confirm a value that had not changed cost a person
// twenty four seconds on the endpoint that exists so they would not wait.
func (c *VersionClient) Recent(ctx context.Context) (Version, error) {
	return c.get(ctx, true)
}

// get holds what the two share. How long the caller waits and how long the
// refresh runs are separate on purpose, and the caller's context governs only
// the first: the refresh starts detached and runs to completion however soon
// the caller gives up. Cancelling it would abandon the cold start it triggered,
// and the next caller would begin that start again from nothing.
func (c *VersionClient) get(ctx context.Context, staleIsFine bool) (Version, error) {
	if version, ok := c.fresh(); ok {
		return version, nil
	}

	done := c.refresh.DoChan("version", func() (any, error) {
		// Detached from the caller's cancellation but not its values, so the
		// fetch still carries the trace and the correlation id.
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), versionRefreshTimeout)
		defer cancel()

		version, err := c.fetch(fetchCtx)
		if err != nil {
			return Version{}, err
		}
		c.mu.Lock()
		c.cached = version
		c.fetched = time.Now()
		c.mu.Unlock()
		return version, nil
	})

	if staleIsFine {
		if stale, ok := c.last(); ok {
			return stale, nil
		}
	}

	select {
	case result := <-done:
		if result.Err != nil {
			if stale, ok := c.last(); ok {
				return stale, nil
			}
			return Version{}, result.Err
		}
		return result.Val.(Version), nil
	case <-ctx.Done():
		if stale, ok := c.last(); ok {
			return stale, nil
		}
		return Version{}, fmt.Errorf("waiting for the ai service version: %w", ctx.Err())
	}
}

func (c *VersionClient) fresh() (Version, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.cached.PromptVersion == "" || time.Since(c.fetched) >= c.ttl {
		return Version{}, false
	}
	return c.cached, true
}

func (c *VersionClient) last() (Version, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cached, c.cached.PromptVersion != ""
}

func (c *VersionClient) fetch(ctx context.Context) (Version, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/version", nil)
	if err != nil {
		return Version{}, fmt.Errorf("building version request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("X-Internal-Token", c.token)
	}
	if id := requestid.FromContext(ctx); id != "" {
		req.Header.Set(requestid.Header, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Version{}, fmt.Errorf("calling ai service version: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Version{}, fmt.Errorf("version endpoint returned status %d", resp.StatusCode)
	}

	var version Version
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		return Version{}, fmt.Errorf("decoding version: %w", err)
	}
	if version.PromptVersion == "" {
		return Version{}, fmt.Errorf("version endpoint returned no prompt version")
	}
	return version, nil
}
