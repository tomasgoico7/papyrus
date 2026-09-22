package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/papyrus/gateway/internal/requestid"
)

// HealthClient asks the AI service whether it is able to take work.
//
// It exists because "not answering" and "not there yet" look the same from a
// failed analysis, and only one of them is worth waiting for. The health
// endpoint needs no token, which matters: a probe that could be rejected on
// credentials would not tell us anything about readiness.
type HealthClient struct {
	baseURL string
	http    *http.Client
	timeout time.Duration
}

func NewHealthClient(baseURL string, client *http.Client, timeout time.Duration) *HealthClient {
	return &HealthClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    client,
		timeout: timeout,
	}
}

// Ready returns nil when the upstream answered. Any other outcome is an error:
// the caller only cares whether it can proceed, not why it cannot.
func (c *HealthClient) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("building health request: %w", err)
	}
	if id := requestid.FromContext(ctx); id != "" {
		req.Header.Set(requestid.Header, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("probing health: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health returned status %d", resp.StatusCode)
	}
	return nil
}
