package requestid_test

import (
	"context"
	"strings"
	"testing"

	"github.com/papyrus/gateway/internal/requestid"
)

func TestNewProducesDistinctHexIdentifiers(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for range 100 {
		id := requestid.New()
		if len(id) != 32 {
			t.Fatalf("id = %q, want 32 hex characters", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("id %q was generated twice", id)
		}
		seen[id] = struct{}{}
	}
}

func TestSanitizeAcceptsOnlySafeIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "hex is kept", raw: "9f2c1ab34de5", want: "9f2c1ab34de5"},
		{name: "dashes and underscores are kept", raw: "req-42_a", want: "req-42_a"},
		{name: "empty stays empty", raw: "", want: ""},
		{name: "a newline is rejected", raw: "abc\ndef", want: ""},
		{name: "a carriage return is rejected", raw: "abc\r\nlevel=fatal", want: ""},
		{name: "spaces are rejected", raw: "two words", want: ""},
		{name: "punctuation is rejected", raw: "abc;rm -rf", want: ""},
		{name: "an overlong value is rejected", raw: strings.Repeat("a", 65), want: ""},
		{name: "the longest allowed value is kept", raw: strings.Repeat("a", 64), want: strings.Repeat("a", 64)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := requestid.Sanitize(tc.raw); got != tc.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := requestid.NewContext(context.Background(), "abc123")
	if got := requestid.FromContext(ctx); got != "abc123" {
		t.Errorf("FromContext = %q, want abc123", got)
	}
}

func TestFromContextIsEmptyWhenUnset(t *testing.T) {
	if got := requestid.FromContext(context.Background()); got != "" {
		t.Errorf("FromContext = %q, want an empty string", got)
	}
}
