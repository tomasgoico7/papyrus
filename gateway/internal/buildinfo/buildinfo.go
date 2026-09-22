// Package buildinfo reports which build of the gateway is running.
//
// It exists because a deploy is otherwise invisible from outside: two versions
// answer a health check identically, so diagnosing a fix means inferring from
// timings whether it is even live yet. That inference is unreliable and it is
// made exactly when something is already wrong.
package buildinfo

import (
	"os"
	"runtime/debug"
	"sync"
)

// Unknown is reported when nothing stamped the build, which is the normal
// answer for `go run` and for a container built without its repository.
const Unknown = "unknown"

const shortLength = 7

var revision = sync.OnceValue(func() string {
	// The platform hands the commit to the process rather than the build, which
	// is what makes this work at all: the image is built from a context that
	// does not carry the repository, so nothing is stamped into the binary.
	for _, key := range []string{"RENDER_GIT_COMMIT", "GIT_COMMIT", "SOURCE_COMMIT"} {
		if value := os.Getenv(key); value != "" {
			return short(value)
		}
	}

	// A build that did have the repository stamps it here on its own.
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				return short(setting.Value)
			}
		}
	}

	return Unknown
})

// Revision is the commit this binary was built from, shortened for reading.
func Revision() string { return revision() }

func short(value string) string {
	if len(value) <= shortLength {
		return value
	}
	return value[:shortLength]
}
