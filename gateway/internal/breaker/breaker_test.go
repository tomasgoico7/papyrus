package breaker

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// clock is a hand-wound time source, so a cooldown can pass without the test
// waiting for it.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type transitions struct {
	mu   sync.Mutex
	seen []string
}

func (tr *transitions) record(from, to State) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.seen = append(tr.seen, from.String()+"->"+to.String())
}

func (tr *transitions) all() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.seen...)
}

func newTestBreaker(cfg Config) (*Breaker, *clock, *transitions) {
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	tr := &transitions{}
	b := New(cfg, tr.record)
	b.now = c.Now
	return b, c, tr
}

func call(t *testing.T, b *Breaker, outcome Outcome) error {
	t.Helper()
	record, err := b.Allow()
	if err != nil {
		return err
	}
	record(outcome)
	return nil
}

func TestItOpensAfterConsecutiveFailures(t *testing.T) {
	b, _, _ := newTestBreaker(Config{FailureThreshold: 3, Cooldown: time.Minute})

	for i := range 2 {
		if err := call(t, b, Failure); err != nil {
			t.Fatalf("failure %d was refused before the threshold: %v", i+1, err)
		}
	}
	if b.State() != Closed {
		t.Fatalf("state = %v after two of three failures", b.State())
	}

	_ = call(t, b, Failure)
	if b.State() != Open {
		t.Fatalf("state = %v after three consecutive failures, want open", b.State())
	}
	if err := call(t, b, Success); !errors.Is(err, ErrOpen) {
		t.Errorf("a call while open = %v, want ErrOpen without being attempted", err)
	}
}

func TestASuccessResetsTheCount(t *testing.T) {
	b, _, _ := newTestBreaker(Config{FailureThreshold: 3, Cooldown: time.Minute})

	// Failures have to be consecutive. An upstream that fails now and then but
	// answers in between is flaky, not down, and stopping calls to it would turn
	// occasional errors into a total outage.
	for range 5 {
		_ = call(t, b, Failure)
		_ = call(t, b, Failure)
		_ = call(t, b, Success)
	}
	if b.State() != Closed {
		t.Errorf("state = %v; failures separated by successes should never open it", b.State())
	}
}

func TestItReportsHowLongUntilTheNextTrial(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	_ = call(t, b, Failure)

	clk.Advance(20 * time.Second)
	_, err := b.Allow()

	var open *OpenError
	if !errors.As(err, &open) {
		t.Fatalf("err = %v, want an *OpenError", err)
	}
	if open.RetryAfter != 40*time.Second {
		t.Errorf("retry after = %v, want the 40s left of the cooldown", open.RetryAfter)
	}
}

func TestAfterTheCooldownExactlyOneTrialGoesThrough(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	_ = call(t, b, Failure)
	clk.Advance(time.Minute)

	record, err := b.Allow()
	if err != nil {
		t.Fatalf("the trial was refused after the cooldown: %v", err)
	}
	if b.State() != HalfOpen {
		t.Errorf("state = %v while the trial is out, want half-open", b.State())
	}

	// A second caller while the trial is still out. Letting a crowd through at
	// the first sign of recovery is how a recovering upstream gets knocked down
	// again.
	if _, err := b.Allow(); !errors.Is(err, ErrOpen) {
		t.Errorf("a second call during the trial = %v, want it refused", err)
	}
	record(Success)
}

func TestASuccessfulTrialCloses(t *testing.T) {
	b, clk, tr := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	_ = call(t, b, Failure)
	clk.Advance(time.Minute)
	_ = call(t, b, Success)

	if b.State() != Closed {
		t.Fatalf("state = %v after a successful trial, want closed", b.State())
	}
	want := []string{"closed->open", "open->half_open", "half_open->closed"}
	if got := tr.all(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("transitions = %v, want %v", got, want)
	}
}

func TestAFailedTrialReopensForLonger(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute, MaxCooldown: 3 * time.Minute})
	_ = call(t, b, Failure)

	// Each failed trial doubles the wait, up to the cap: an upstream that is
	// properly down gets asked less and less often.
	for _, want := range []time.Duration{2 * time.Minute, 3 * time.Minute, 3 * time.Minute} {
		clk.Advance(time.Hour) // well past any cooldown
		_ = call(t, b, Failure)

		_, err := b.Allow()
		var open *OpenError
		if !errors.As(err, &open) {
			t.Fatalf("err = %v after a failed trial, want open", err)
		}
		if open.RetryAfter != want {
			t.Errorf("cooldown = %v, want %v", open.RetryAfter, want)
		}
	}
}

func TestTheCooldownResetsOnceClosed(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute, MaxCooldown: 10 * time.Minute})
	_ = call(t, b, Failure)
	clk.Advance(time.Hour)
	_ = call(t, b, Failure) // failed trial: cooldown now two minutes
	clk.Advance(time.Hour)
	_ = call(t, b, Success) // closed

	_ = call(t, b, Failure) // opens again
	_, err := b.Allow()
	var open *OpenError
	if !errors.As(err, &open) || open.RetryAfter != time.Minute {
		t.Errorf("err = %v; a fresh outage should start from the base cooldown, not the last one's", err)
	}
}

func TestAnAbandonedTrialFreesTheSlot(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	_ = call(t, b, Failure)
	clk.Advance(time.Minute)

	_ = call(t, b, Ignored)
	if b.State() != HalfOpen {
		t.Errorf("state = %v; a trial nobody waited for says nothing either way", b.State())
	}
	if _, err := b.Allow(); err != nil {
		t.Errorf("the next caller should get the trial: %v", err)
	}
}

func TestReadySaysWhetherACallWouldGoThrough(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	if !b.Ready() {
		t.Error("closed: should be ready")
	}

	_ = call(t, b, Failure)
	if b.Ready() {
		t.Error("open: should not be ready")
	}

	clk.Advance(time.Minute)
	if !b.Ready() {
		t.Error("half-open with no trial out: should be ready")
	}
	record, _ := b.Allow()
	if b.Ready() {
		// A worker that claimed a job now would have its call refused and lose
		// the attempt for nothing.
		t.Error("half-open with the trial out: should not be ready")
	}
	record(Success)
	if !b.Ready() {
		t.Error("closed again: should be ready")
	}
}

func TestReadyDoesNotSpendTheTrial(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})
	_ = call(t, b, Failure)
	clk.Advance(time.Minute)

	for range 3 {
		_ = b.Ready()
	}
	if _, err := b.Allow(); err != nil {
		t.Errorf("asking whether a call would go through used up the trial: %v", err)
	}
}

func TestALateAnswerFromBeforeTheOutageDoesNotCloseIt(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 1, Cooldown: time.Minute})

	// A slow call granted while closed...
	slow, err := b.Allow()
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	// ...the breaker opens and moves to half-open meanwhile...
	_ = call(t, b, Failure)
	clk.Advance(time.Minute)
	if b.State() != HalfOpen {
		t.Fatalf("setup: state = %v", b.State())
	}
	// ...and then the slow call finally answers.
	slow(Success)

	// Only the trial decides a half-open breaker. A success that started before
	// the outage says nothing about whether the upstream is back now.
	if b.State() != HalfOpen {
		t.Errorf("state = %v; a stale answer closed the breaker", b.State())
	}
}

func TestItIsSafeUnderConcurrentUse(t *testing.T) {
	b, clk, _ := newTestBreaker(Config{FailureThreshold: 3, Cooldown: time.Millisecond})

	// Meant for the race detector: many callers, a clock moving underneath them,
	// outcomes of every kind. It asserts only that nothing tears.
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range 200 {
				if record, err := b.Allow(); err == nil {
					record(Outcome((i + j) % 3))
				}
				_ = b.Ready()
				if j%50 == 0 {
					clk.Advance(time.Millisecond)
				}
			}
		}()
	}
	wg.Wait()

	if s := b.State(); s != Closed && s != Open && s != HalfOpen {
		t.Errorf("state = %v after concurrent use", s)
	}
}
