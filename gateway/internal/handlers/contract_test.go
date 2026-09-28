package handlers_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// notShownToPeople are codes the gateway sends that the client never puts on
// screen, so they need no message of their own.
var notShownToPeople = map[string]string{
	// Answered with the session-expired copy by a rule of its own, not by code.
	"unauthorized": "handled before the code lookup",
	// Only returned while polling a job, and the client treats any polling
	// error as transient and keeps going; it never reaches the page.
	"job_not_found": "swallowed by polling",
}

// TestEveryErrorCodeHasAMessageInTheClient holds the gateway and the client to
// the same list of error codes.
//
// The client localises by code and falls back to a generic message for any it
// does not know. So a code added here and forgotten there does not break
// anything visibly — it just turns a specific, actionable error into "something
// went wrong". That happened twice before this test existed: once for
// ai_service_error, a condition that retrying usually fixes, and once for
// worker_lost. TypeScript cannot see Go, so the check lives on this side.
func TestEveryErrorCodeHasAMessageInTheClient(t *testing.T) {
	sent := codesTheGatewaySends(t)
	mapped := codesTheClientMaps(t)

	var missing []string
	for code := range sent {
		if _, skip := notShownToPeople[code]; skip {
			continue
		}
		if !mapped[code] {
			missing = append(missing, code)
		}
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("the gateway can send %v, but frontend/lib/api/gateway.ts has no message for it; "+
			"add it to ERROR_CODE_KEYS with a line in both dictionaries, or to notShownToPeople "+
			"here if it genuinely never reaches the page", missing)
	}
	if len(sent) < 10 {
		// A sanity floor, so a pattern that silently stops matching cannot turn
		// this into a test that checks nothing.
		t.Fatalf("found only %d codes in the gateway; the scan is probably broken", len(sent))
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating this test file")
	}
	return filepath.Join(filepath.Dir(here), "..", "..", "..")
}

var (
	// httpx.RespondError(c, http.StatusX, "code", ...), the envelope every
	// handler answers with.
	respondedCode = regexp.MustCompile(`RespondError\(\s*c,\s*http\.Status\w+,\s*"([a-z_]+)"`)
	// The worker's classifications, returned as (code, message) and stored on a
	// failed job, which the client reads when it polls.
	describedCode = regexp.MustCompile(`return "([a-z_]+)", `)
	// Codes written straight into a job row by SQL.
	storedCode = regexp.MustCompile(`error_code = '([a-z_]+)'`)
)

func codesTheGatewaySends(t *testing.T) map[string]bool {
	t.Helper()
	codes := map[string]bool{}
	internal := filepath.Join(repoRoot(t), "gateway", "internal")

	err := filepath.WalkDir(internal, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range []*regexp.Regexp{respondedCode, describedCode, storedCode} {
			for _, match := range pattern.FindAllSubmatch(source, -1) {
				codes[string(match[1])] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning the gateway: %v", err)
	}
	return codes
}

// The keys of ERROR_CODE_KEYS, one `code: "dictionaryKey",` per line.
var mappedCode = regexp.MustCompile(`(?m)^\s+([a-z_]+):\s*"[A-Za-z]+",`)

func codesTheClientMaps(t *testing.T) map[string]bool {
	t.Helper()
	path := filepath.Join(repoRoot(t), "frontend", "lib", "api", "gateway.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the client's error map: %v", err)
	}

	text := string(source)
	start := strings.Index(text, "const ERROR_CODE_KEYS")
	if start < 0 {
		t.Fatal("ERROR_CODE_KEYS not found in frontend/lib/api/gateway.ts")
	}
	end := strings.Index(text[start:], "};")
	if end < 0 {
		t.Fatal("the end of ERROR_CODE_KEYS was not found")
	}

	codes := map[string]bool{}
	for _, match := range mappedCode.FindAllStringSubmatch(text[start:start+end], -1) {
		codes[match[1]] = true
	}
	return codes
}
