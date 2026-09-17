package services_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/papyrus/gateway/internal/services"
)

func questionsRequest() services.QuestionsRequest {
	return services.QuestionsRequest{
		CV:       strings.NewReader("%PDF-1.4 minimal"),
		Filename: "cv.pdf",
		JobOffer: "Backend role needing Go and Postgres.",
	}
}

func TestQuestionsForwardsEveryFieldAndReturnsTheBodyVerbatim(t *testing.T) {
	const upstreamBody = `{"questions":[{"topic":"Kubernetes","question":"Do you?"}],"cvText":"cv"}`

	var gotPath, gotFilename, gotOffer, gotTitle, gotLocale, gotToken string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse forwarded form: %v", err)
			return
		}
		_, header, err := r.FormFile("cv")
		if err != nil {
			t.Errorf("read forwarded cv: %v", err)
			return
		}

		gotPath = r.URL.Path
		gotFilename = header.Filename
		gotOffer = r.FormValue("jobOffer")
		gotTitle = r.FormValue("jobTitle")
		gotLocale = r.FormValue("locale")
		gotToken = r.Header.Get("X-Internal-Token")

		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer ai.Close()

	client := services.NewTailorClient(ai.URL+"/", "shared-secret", ai.Client())
	data, err := client.Questions(context.Background(), services.QuestionsRequest{
		CV:       strings.NewReader("%PDF-1.4 minimal"),
		Filename: "jane-resume.pdf",
		JobOffer: "Backend role needing Go and Postgres.",
		JobTitle: "Backend Engineer",
		Locale:   "es",
	})
	if err != nil {
		t.Fatalf("questions: %v", err)
	}

	if gotPath != "/tailor/questions" {
		t.Errorf("path = %q, want /tailor/questions", gotPath)
	}
	if gotFilename != "jane-resume.pdf" {
		t.Errorf("filename = %q", gotFilename)
	}
	if gotOffer != "Backend role needing Go and Postgres." {
		t.Errorf("jobOffer = %q", gotOffer)
	}
	if gotTitle != "Backend Engineer" {
		t.Errorf("jobTitle = %q", gotTitle)
	}
	if gotLocale != "es" {
		t.Errorf("locale = %q", gotLocale)
	}
	if gotToken != "shared-secret" {
		t.Errorf("X-Internal-Token = %q, want the configured token", gotToken)
	}
	if string(data) != upstreamBody {
		t.Errorf("body = %q, want it passed through untouched", data)
	}
}

func TestQuestionsOmitsEmptyOptionalFields(t *testing.T) {
	var present []string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse forwarded form: %v", err)
			return
		}
		for _, name := range []string{"jobTitle", "locale"} {
			if _, ok := r.MultipartForm.Value[name]; ok {
				present = append(present, name)
			}
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer ai.Close()

	client := services.NewTailorClient(ai.URL, "", ai.Client())
	if _, err := client.Questions(context.Background(), questionsRequest()); err != nil {
		t.Fatalf("questions: %v", err)
	}

	if len(present) != 0 {
		t.Errorf("empty optional fields were sent: %v", present)
	}
}

func TestGenerateForwardsTheJSONBodyUntouched(t *testing.T) {
	const payload = `{"cvText":"cv","jobOffer":"offer","answers":[]}`

	var gotPath, gotContentType, gotBody string

	ai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read forwarded body: %v", err)
			return
		}
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotBody = string(body)

		_, _ = w.Write([]byte(`{"fullName":"Jane"}`))
	}))
	defer ai.Close()

	client := services.NewTailorClient(ai.URL, "", ai.Client())
	data, err := client.Generate(context.Background(), []byte(payload))
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if gotPath != "/tailor/generate" {
		t.Errorf("path = %q, want /tailor/generate", gotPath)
	}
	if gotContentType != "application/json" {
		t.Errorf("content type = %q", gotContentType)
	}
	if gotBody != payload {
		t.Errorf("body = %q, want it forwarded byte for byte", gotBody)
	}
	if string(data) != `{"fullName":"Jane"}` {
		t.Errorf("response = %q, want it returned untouched", data)
	}
}

func TestTailorMapsUpstreamFailures(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantCode     string
		wantIsClient bool
	}{
		{
			name:         "a well-formed envelope is surfaced as-is",
			status:       http.StatusUnprocessableEntity,
			body:         `{"error":{"code":"unreadable_cv","message":"The CV is not readable."}}`,
			wantCode:     "unreadable_cv",
			wantIsClient: true,
		},
		{
			name:         "a body that is not an envelope falls back",
			status:       http.StatusServiceUnavailable,
			body:         "upstream is down",
			wantCode:     "ai_service_error",
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

			client := services.NewTailorClient(ai.URL, "", ai.Client())
			_, err := client.Generate(context.Background(), []byte("{}"))

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
			if upstream.IsClientError() != tc.wantIsClient {
				t.Errorf("IsClientError() = %t, want %t", upstream.IsClientError(), tc.wantIsClient)
			}
		})
	}
}

func TestTailorReportsAnUnreachableUpstream(t *testing.T) {
	ai := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := services.NewTailorClient(ai.URL, "", ai.Client())
	ai.Close()

	if _, err := client.Generate(context.Background(), []byte("{}")); err == nil {
		t.Fatal("expected an error when the upstream is not listening")
	}
}
