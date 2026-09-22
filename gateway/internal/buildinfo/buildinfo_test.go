package buildinfo_test

import (
	"testing"

	"github.com/papyrus/gateway/internal/buildinfo"
)

func TestRevisionIsStableAndShort(t *testing.T) {
	first := buildinfo.Revision()
	if first == "" {
		t.Fatal("the revision is empty; it should say so explicitly instead")
	}
	if len(first) > 7 {
		t.Errorf("revision = %q, want it shortened to at most 7 characters", first)
	}
	// Reported on every health check, so it is computed once rather than read
	// from the environment on each call.
	if second := buildinfo.Revision(); second != first {
		t.Errorf("revision changed between calls: %q then %q", first, second)
	}
}
