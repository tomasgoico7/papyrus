package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/papyrus/gateway/internal/requestid"
	"github.com/papyrus/gateway/internal/services"
	"github.com/papyrus/gateway/internal/transport"
)

func sampleAnalysis() transport.Analysis {
	return transport.Analysis{
		Score:         74,
		Verdict:       "moderate",
		Summary:       transport.Localized{En: "Decent fit.", Es: "Encaje razonable."},
		MatchedSkills: transport.LocalizedList{En: []string{"Go"}, Es: []string{"Go"}},
		MissingSkills: transport.LocalizedList{En: []string{"gRPC"}, Es: []string{"gRPC"}},
		Suggestions:   []transport.Suggestion{},
	}
}

func analyzeRequest() services.AnalyzeRequest {
	return services.AnalyzeRequest{
		CV:       strings.NewReader("%PDF-1.4 minimal"),
		Filename: "cv.pdf",
		JobOffer: "Backend role needing Go and Postgres.",
	}
}

func TestAnalyzeEncodesTheRequestAndDecodesTheResponse(t *testing.T) {
	var gotFilename, gotFileBody, gotOffer, gotTitle, gotToken string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse forwarded form: %v", err)
			return
		}
		file, header, err := r.FormFile("cv")
		if err != nil {
			t.Errorf("read forwarded cv: %v", err)
			return
		}
		defer file.Close()

		body, err := io.ReadAll(file)
		if err != nil {
			t.Errorf("read cv body: %v", err)
			return
		}

		gotFilename = header.Filename
		gotFileBody = string(body)
		gotOffer = r.FormValue("jobOffer")
		gotTitle = r.FormValue("jobTitle")
		gotToken = r.Header.Get("X-Internal-Token")

		_ = json.NewEncoder(w).Encode(sampleAnalysis())
	}))
	defer ai.Close()

	// The trailing slash must not survive into the upstream path.
	client := services.NewAnalyzerClient(ai.URL+"/", "shared-secret", ai.Client())

	analysis, err := client.Analyze(context.Background(), services.AnalyzeRequest{
		CV:       strings.NewReader("%PDF-1.4 minimal"),
		Filename: "jane-resume.pdf",
		JobOffer: "Backend role needing Go and Postgres.",
		JobTitle: "Backend Engineer",
	})
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if gotFilename != "jane-resume.pdf" {
		t.Errorf("filename = %q, want jane-resume.pdf", gotFilename)
	}
	if gotFileBody != "%PDF-1.4 minimal" {
		t.Errorf("cv body = %q, want the bytes copied verbatim", gotFileBody)
	}
	if gotOffer != "Backend role needing Go and Postgres." {
		t.Errorf("jobOffer = %q", gotOffer)
	}
	if gotTitle != "Backend Engineer" {
		t.Errorf("jobTitle = %q", gotTitle)
	}
	if gotToken != "shared-secret" {
		t.Errorf("X-Internal-Token = %q, want the configured token", gotToken)
	}
	if analysis.Score != 74 || analysis.Verdict != "moderate" {
		t.Errorf("decoded analysis = %+v", analysis)
	}
}

func TestAnalyzeOmitsAnEmptyJobTitleAndToken(t *testing.T) {
	var titlePresent, tokenPresent bool

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse forwarded form: %v", err)
			return
		}
		_, titlePresent = r.MultipartForm.Value["jobTitle"]
		_, tokenPresent = r.Header["X-Internal-Token"]
		_ = json.NewEncoder(w).Encode(sampleAnalysis())
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	if _, err := client.Analyze(context.Background(), analyzeRequest()); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	if titlePresent {
		t.Error("an empty job title should not be sent as a form field")
	}
	if tokenPresent {
		t.Error("an empty internal token should not be sent as a header")
	}
}

func TestAnalyzeMapsUpstreamFailures(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantCode     string
		wantMessage  string
		wantIsClient bool
	}{
		{
			name:         "a well-formed envelope is surfaced as-is",
			status:       http.StatusUnprocessableEntity,
			body:         `{"error":{"code":"unreadable_cv","message":"The CV is not readable."}}`,
			wantCode:     "unreadable_cv",
			wantMessage:  "The CV is not readable.",
			wantIsClient: true,
		},
		{
			name:         "a body that is not an envelope falls back",
			status:       http.StatusBadGateway,
			body:         "<html>502 Bad Gateway</html>",
			wantCode:     "ai_service_error",
			wantMessage:  "The AI service returned an unexpected response.",
			wantIsClient: false,
		},
		{
			name:         "an envelope carrying no message falls back",
			status:       http.StatusInternalServerError,
			body:         `{"error":{"code":"boom"}}`,
			wantCode:     "ai_service_error",
			wantMessage:  "The AI service returned an unexpected response.",
			wantIsClient: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ai.Close()

			client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
			_, err := client.Analyze(context.Background(), analyzeRequest())

			var upstream *services.UpstreamError
			if !errors.As(err, &upstream) {
				t.Fatalf("error = %v, want an *UpstreamError", err)
			}
			if upstream.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", upstream.StatusCode, tc.status)
			}
			if upstream.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", upstream.Code, tc.wantCode)
			}
			if upstream.Message != tc.wantMessage {
				t.Errorf("message = %q, want %q", upstream.Message, tc.wantMessage)
			}
			if upstream.IsClientError() != tc.wantIsClient {
				t.Errorf("IsClientError() = %t, want %t", upstream.IsClientError(), tc.wantIsClient)
			}
		})
	}
}

func TestAnalyzeRejectsAMalformedSuccessBody(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	_, err := client.Analyze(context.Background(), analyzeRequest())
	if err == nil {
		t.Fatal("expected a decode error for a 200 that is not an analysis")
	}

	var upstream *services.UpstreamError
	if errors.As(err, &upstream) {
		t.Error("a malformed 200 is our failure to decode, not an upstream status error")
	}
}

func TestAnalyzeReportsAnUnreachableUpstream(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	ai.Close()

	if _, err := client.Analyze(context.Background(), analyzeRequest()); err == nil {
		t.Fatal("expected an error when the upstream is not listening")
	}
}

func TestAnalyzeHonoursContextCancellation(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(sampleAnalysis())
	}))
	defer ai.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	_, err := client.Analyze(ctx, analyzeRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want it to wrap context.Canceled", err)
	}
}

func TestAnalyzeForwardsTheCorrelationID(t *testing.T) {
	var withID, withoutID string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(requestid.Header); got != "" {
			withID = got
		} else {
			withoutID = "absent"
		}
		_ = json.NewEncoder(w).Encode(sampleAnalysis())
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())

	ctx := requestid.NewContext(context.Background(), "abc123")
	if _, err := client.Analyze(ctx, analyzeRequest()); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if withID != "abc123" {
		t.Errorf("forwarded id = %q, want abc123", withID)
	}

	if _, err := client.Analyze(context.Background(), analyzeRequest()); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if withoutID != "absent" {
		t.Error("a context without an id should not send an empty header")
	}
}

func TestUpstreamErrorClassifiesStatusBands(t *testing.T) {
	tests := []struct {
		status int
		want   bool
	}{
		{status: http.StatusMovedPermanently, want: false},
		{status: http.StatusBadRequest, want: true},
		{status: http.StatusTooManyRequests, want: true},
		{status: http.StatusInternalServerError, want: false},
		{status: http.StatusGatewayTimeout, want: false},
	}

	for _, tc := range tests {
		err := &services.UpstreamError{StatusCode: tc.status, Code: "c", Message: "m"}
		if got := err.IsClientError(); got != tc.want {
			t.Errorf("IsClientError() for %d = %t, want %t", tc.status, got, tc.want)
		}
	}
}

func TestUpstreamErrorKeepsWhatAnUnrecognisedUpstreamSaid(t *testing.T) {
	// A proxy page, a platform error and a service still booting all arrive as
	// "not our envelope". Without the status and a piece of the body they are
	// indistinguishable, and the failure cannot be diagnosed after the fact.
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<html>
  <head><title>502 Bad Gateway</title></head>
</html>`))
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	_, err := client.Analyze(context.Background(), analyzeRequest())

	var upstream *services.UpstreamError
	if !errors.As(err, &upstream) {
		t.Fatalf("error = %v, want an *UpstreamError", err)
	}

	// Collapsed onto one line, because a multi-line HTML page inside a log
	// record is unreadable. Comparing to the exact expected line checks the
	// content and the flattening at once.
	const want = "<html> <head><title>502 Bad Gateway</title></head> </html>"
	if upstream.Body != want {
		t.Errorf("body = %q, want %q", upstream.Body, want)
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error string = %q, want the status in it", err)
	}
}

func TestUpstreamErrorTruncatesALongBody(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	_, err := client.Analyze(context.Background(), analyzeRequest())

	var upstream *services.UpstreamError
	_ = errors.As(err, &upstream)
	if len(upstream.Body) > 250 {
		t.Errorf("body is %d characters; a log line is not the place for the whole page", len(upstream.Body))
	}
}

func TestEnvelopeErrorsCarryNoBodySnippet(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"unreadable_cv","message":"Not readable."}}`))
	}))
	defer ai.Close()

	client := services.NewAnalyzerClient(ai.URL, "", ai.Client())
	_, err := client.Analyze(context.Background(), analyzeRequest())

	var upstream *services.UpstreamError
	_ = errors.As(err, &upstream)
	// The upstream explained itself; there is nothing to salvage from the body.
	if upstream.Body != "" {
		t.Errorf("body = %q, want none when the envelope was understood", upstream.Body)
	}
}
