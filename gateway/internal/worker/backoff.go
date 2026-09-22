package worker

import (
	"math"
	"math/rand/v2"
	"time"
)

// backoff returns how long to hold a job before its next attempt.
//
// The delay doubles per attempt, capped, and then a random point *below* it is
// taken rather than the delay itself. That last part is the one that matters:
// when an upstream falls over, every job in flight fails at roughly the same
// moment, and a deterministic backoff schedules them all to come back at the
// same moment too — the retries arrive as a wave and knock the upstream down
// again just as it recovers. Spreading them across the window is what stops the
// queue from synchronising itself into a herd.
func backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	ceiling := float64(base) * math.Pow(2, float64(attempt-1))
	if ceiling > float64(max) || math.IsInf(ceiling, 0) {
		ceiling = float64(max)
	}
	if ceiling <= 0 {
		return 0
	}

	// A jittered delay can be very short, which is fine: the point is that the
	// attempts spread out, not that each one waits a minimum.
	return time.Duration(rand.Int64N(int64(ceiling)) + 1)
}
