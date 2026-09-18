package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/papyrus/gateway/internal/requestid"
)

// TailorClient talks to the AI service's CV-tailoring endpoints. Unlike the
// analyzer it forwards the upstream JSON verbatim: the gateway only needs to
// authenticate, rate-limit and add the internal token, not understand the CV.
type TailorClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewTailorClient(baseURL, token string, client *http.Client) *TailorClient {
	return &TailorClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    client,
	}
}

// QuestionsRequest carries the multipart inputs for the questions step.
type QuestionsRequest struct {
	CV       io.Reader
	Filename string
	JobOffer string
	JobTitle string
	Locale   string
}

func (c *TailorClient) Questions(ctx context.Context, req QuestionsRequest) ([]byte, error) {
	body, contentType, err := encodeQuestionsMultipart(req)
	if err != nil {
		return nil, fmt.Errorf("encoding request: %w", err)
	}
	return c.post(ctx, "/tailor/questions", contentType, body)
}

// Generate forwards the already-encoded JSON body to the generate step.
func (c *TailorClient) Generate(ctx context.Context, payload []byte) ([]byte, error) {
	return c.post(ctx, "/tailor/generate", "application/json", bytes.NewReader(payload))
}

func (c *TailorClient) post(ctx context.Context, path, contentType string, body io.Reader) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if c.token != "" {
		httpReq.Header.Set("X-Internal-Token", c.token)
	}
	if id := requestid.FromContext(ctx); id != "" {
		httpReq.Header.Set(requestid.Header, id)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling ai service: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading ai response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, decodeUpstreamErrorBytes(resp.StatusCode, resp.Header.Get("Retry-After"), data)
	}
	return data, nil
}

func encodeQuestionsMultipart(req QuestionsRequest) (io.Reader, string, error) {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)

	part, err := writer.CreateFormFile("cv", req.Filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, req.CV); err != nil {
		return nil, "", err
	}

	fields := map[string]string{
		"jobOffer": req.JobOffer,
		"jobTitle": req.JobTitle,
		"locale":   req.Locale,
	}
	for name, value := range fields {
		if value == "" {
			continue
		}
		if err := writer.WriteField(name, value); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &buffer, writer.FormDataContentType(), nil
}
