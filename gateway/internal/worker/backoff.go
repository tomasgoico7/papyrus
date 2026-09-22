package worker

import (
	"math"
	"math/rand/v2"
	"time"
)

// backoff returns how long to hold a job before its next attempt.
//
// The delay doubles per attempt, capped, and then half of it is kept and half
// randomised. Both halves earn their place:
//
// The random half stops the queue synchronising itself. When an upstream falls
// over, every job in flight fails at roughly the same moment, and a fixed
// schedule brings them all back at the same moment too — knocking it down again
// just as it recovers.
//
// The fixed half is the floor, and it was learned the hard way. Pure jitter can
// draw a delay of a few hundred milliseconds, which is useless against the
// failure this actually sees: a service that is not down but asleep, and needs
// tens of seconds to come back. Three attempts spread over fourteen seconds all
// landed before it had finished waking up, and the job died of impatience.
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

	half := time.Duration(ceiling / 2)
	if half < 1 {
		return time.Duration(ceiling)
	}
	return half + time.Duration(rand.Int64N(int64(half))+1)
}
