package worker

import (
	"testing"
	"time"
)

// TestBackoffNeverDrawsADelayTooShortToHelp is the one this file exists for.
// Three attempts once burned themselves out in fourteen seconds against an
// upstream that takes twenty to fifty to wake up, because pure jitter is free to
// draw almost nothing.
func TestBackoffNeverDrawsADelayTooShortToHelp(t *testing.T) {
	const base = 10 * time.Second

	for range 500 {
		if got := backoff(1, base, time.Minute); got < base/2 {
			t.Fatalf("first attempt waited %v, below the %v floor", got, base/2)
		}
	}

	// What matters is the total: the three attempts a job gets have to span
	// longer than the upstream needs to come back.
	var worst time.Duration
	for range 500 {
		total := backoff(1, base, time.Minute) +
			backoff(2, base, time.Minute) +
			backoff(3, base, time.Minute)
		if worst == 0 || total < worst {
			worst = total
		}
	}
	if worst < 30*time.Second {
		t.Errorf("the unluckiest run waits only %v across three attempts; a cold start outlasts it", worst)
	}
}

func TestBackoffGrowsAndThenStopsAtTheCeiling(t *testing.T) {
	const (
		base = time.Second
		max  = 8 * time.Second
	)

	// Jitter makes any single draw uninformative, so the bound is what gets
	// asserted: the delay never exceeds the doubling schedule, and never the cap.
	ceilings := map[int]time.Duration{1: base, 2: 2 * base, 3: 4 * base, 4: max, 9: max}

	for attempt, ceiling := range ceilings {
		for range 200 {
			got := backoff(attempt, base, max)
			if got <= 0 {
				t.Fatalf("attempt %d produced %v, want a positive delay", attempt, got)
			}
			if got > ceiling {
				t.Fatalf("attempt %d produced %v, above its ceiling of %v", attempt, got, ceiling)
			}
		}
	}
}

func TestBackoffSpreadsRetriesOut(t *testing.T) {
	// The point of the jitter: jobs that fail together must not come back
	// together. A schedule with no spread would knock the upstream over again
	// the moment it recovers.
	seen := make(map[time.Duration]struct{})
	for range 200 {
		seen[backoff(4, time.Second, time.Minute)] = struct{}{}
	}

	if len(seen) < 50 {
		t.Errorf("only %d distinct delays in 200 draws; the retries are not spread out", len(seen))
	}

	// Spread, but never all the way down to nothing.
	for delay := range seen {
		if delay < 4*time.Second {
			t.Errorf("drew %v for attempt 4; the floor should keep it well above that", delay)
		}
	}
}

func TestBackoffHandlesAnAttemptCountItShouldNeverSee(t *testing.T) {
	if got := backoff(0, time.Second, time.Minute); got <= 0 {
		t.Errorf("backoff(0) = %v, want it treated as the first attempt", got)
	}
	// A huge attempt count must not overflow into a negative or absurd delay.
	if got := backoff(1000, time.Second, time.Minute); got <= 0 || got > time.Minute {
		t.Errorf("backoff(1000) = %v, want it clamped to the ceiling", got)
	}
}
