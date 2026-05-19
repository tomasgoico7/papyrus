package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/papyrus/gateway/internal/transport"
)

// UpstreamError represents a non-2xx response from the AI service. It carries
// the upstream status so the handler can decide whether the failure is the
// caller's fault (4xx, surfaced as-is) or ours (5xx, surfaced as a 502).
type UpstreamError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("ai service responded %d: %s", e.StatusCode, e.Message)
}

func (e *UpstreamError) IsClientError() bool {
	return e.StatusCode >= 400 && e.StatusCode < 500
}

// AnalyzerClient talks to the Python AI service.
type AnalyzerClient struct {
	baseURL string
	http    *http.Client
}

func NewAnalyzerClient(baseURL string, client *http.Client) *AnalyzerClient {
	return &AnalyzerClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    client,
	}
}

// AnalyzeRequest is the gateway-internal representation of an analysis call.
type AnalyzeRequest struct {
	CV       io.Reader
	Filename string
	JobOffer string
	JobTitle string
}

func (c *AnalyzerClient) Analyze(ctx context.Context, req AnalyzeRequest) (*transport.Analysis, error) {
	body, contentType, err := encodeMultipart(req)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/analyze", body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling ai service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeUpstreamError(resp)
	}

	var analysis transport.Analysis
	if err := json.NewDecoder(resp.Body).Decode(&analysis); err != nil {
		return nil, fmt.Errorf("decoding ai response: %w", err)
	}
	return &analysis, nil
}

func encodeMultipart(req AnalyzeRequest) (io.Reader, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	part, err := writer.CreateFormFile("cv", req.Filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, req.CV); err != nil {
		return nil, "", err
	}

	if err := writer.WriteField("jobOffer", req.JobOffer); err != nil {
		return nil, "", err
	}
	if req.JobTitle != "" {
		if err := writer.WriteField("jobTitle", req.JobTitle); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buffer, writer.FormDataContentType(), nil
}

func decodeUpstreamError(resp *http.Response) error {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil || envelope.Error.Message == "" {
		return &UpstreamError{
			StatusCode: resp.StatusCode,
			Code:       "ai_service_error",
			Message:    "The analysis service returned an unexpected response.",
		}
	}

	return &UpstreamError{
		StatusCode: resp.StatusCode,
		Code:       envelope.Error.Code,
		Message:    envelope.Error.Message,
	}
}
