package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/papyrus/gateway/internal/requestid"
)

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
}

func NewVersionClient(baseURL, token string, client *http.Client, ttl time.Duration) *VersionClient {
	return &VersionClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    client,
		ttl:     ttl,
	}
}

// Current returns the cached version, refreshing it when stale.
//
// When a refresh fails but a previous value is held, that value is returned
// rather than an error. The alternative is losing caching entirely over a blip
// on an endpoint whose answer changes at deploy time; the ttl bounds how long a
// genuinely outdated fingerprint can stay in use.
func (c *VersionClient) Current(ctx context.Context) (Version, error) {
	if version, ok := c.fresh(); ok {
		return version, nil
	}

	version, err := c.fetch(ctx)
	if err != nil {
		if stale, ok := c.last(); ok {
			return stale, nil
		}
		return Version{}, err
	}

	c.mu.Lock()
	c.cached = version
	c.fetched = time.Now()
	c.mu.Unlock()

	return version, nil
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
