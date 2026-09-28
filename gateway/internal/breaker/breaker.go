// Package breaker stops calling a dependency that has stopped answering, and
// finds out cheaply when it starts again.
//
// Retrying a failing upstream per request multiplies the load on something
// already in trouble, and makes every caller wait out the failure one at a
// time. A breaker turns a run of failures into a decision made once: stop, wait,
// try one call, and only then let the rest through.
package breaker

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// State is where the breaker is in its cycle.
type State int

const (
	// Closed lets every call through and counts consecutive failures.
	Closed State = iota
	// HalfOpen lets exactly one trial call through to find out whether the
	// upstream has recovered.
	HalfOpen
	// Open refuses every call until the cooldown has passed.
	Open
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case HalfOpen:
		return "half_open"
	case Open:
		return "open"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// ErrOpen is returned for a call refused without being attempted.
var ErrOpen = errors.New("circuit breaker is open")

// OpenError carries how long until the breaker will next try, for a caller that
// wants to tell its own client when to come back.
type OpenError struct {
	RetryAfter time.Duration
}

func (e *OpenError) Error() string {
	return fmt.Sprintf("%v: next attempt in %v", ErrOpen, e.RetryAfter.Round(time.Second))
}

func (e *OpenError) Is(target error) bool { return target == ErrOpen }

// Outcome is what a call told the breaker about the upstream.
type Outcome int

const (
	// Success means the upstream answered in a way that says it is working —
	// including a refusal of the caller's own request, which is the caller's
	// problem and not the upstream's.
	Success Outcome = iota
	// Failure means the upstream is not serving: throttling, a server error, a
	// connection that failed or timed out.
	Failure
	// Ignored says nothing either way. The caller gave up before an answer came,
	// which is no evidence about the upstream at all.
	Ignored
)

// Config tunes the breaker. Every field has a working default.
type Config struct {
	// FailureThreshold is how many consecutive failures open the breaker. Five,
	// because a single analysis into a sleeping upstream produces two or three
	// failures on its own — the version fetch and the call — and that is a cold
	// start, not an outage.
	FailureThreshold int
	// Cooldown is how long the breaker stays open before its first trial. About
	// the length of a cold start, so a service being woken is tried again soon
	// after it can answer.
	Cooldown time.Duration
	// MaxCooldown caps the cooldown, which doubles after each failed trial. An
	// upstream that is properly down gets asked less and less often; one that
	// is back is still found within a few minutes.
	MaxCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.FailureThreshold < 1 {
		c.FailureThreshold = 5
	}
	if c.Cooldown <= 0 {
		c.Cooldown = 30 * time.Second
	}
	if c.MaxCooldown < c.Cooldown {
		c.MaxCooldown = 5 * time.Minute
	}
	return c
}

// Breaker is safe for concurrent use. One is meant to be shared by every
// caller of the same upstream in a process, so what one learns all of them act
// on.
type Breaker struct {
	cfg      Config
	now      func() time.Time
	onChange func(from, to State)

	mu       sync.Mutex
	state    State
	failures int
	openedAt time.Time
	cooldown time.Duration
	trialOut bool
}

// New builds a closed breaker. onChange, if not nil, is called on every
// transition, outside the lock.
func New(cfg Config, onChange func(from, to State)) *Breaker {
	cfg = cfg.withDefaults()
	return &Breaker{cfg: cfg, now: time.Now, onChange: onChange, cooldown: cfg.Cooldown}
}

// State reports where the breaker is now, moving from open to half-open if the
// cooldown has passed.
func (b *Breaker) State() State {
	b.mu.Lock()
	moved := b.advanceLocked()
	state := b.state
	b.mu.Unlock()
	b.announce(moved)
	return state
}

// Ready reports whether a call made now would be let through, without making
// one. It is for a caller deciding whether to start work that will need the
// upstream — a worker about to claim a job — where finding out halfway through
// would waste the work.
func (b *Breaker) Ready() bool {
	b.mu.Lock()
	moved := b.advanceLocked()
	var ready bool
	switch b.state {
	case Closed:
		ready = true
	case HalfOpen:
		ready = !b.trialOut
	}
	b.mu.Unlock()
	b.announce(moved)
	return ready
}

// Allow asks to make one call. It returns a function that must be called with
// the outcome, or an *OpenError if the call should not be made at all.
func (b *Breaker) Allow() (func(Outcome), error) {
	b.mu.Lock()
	moved := b.advanceLocked()

	var record func(Outcome)
	var err error
	switch b.state {
	case Open:
		wait := b.cooldown - b.now().Sub(b.openedAt)
		err = &OpenError{RetryAfter: max(wait, 0)}
	case HalfOpen:
		if b.trialOut {
			// One trial at a time. The point of the half-open state is to learn
			// with a single call; letting a crowd through defeats it.
			err = &OpenError{RetryAfter: 0}
		} else {
			b.trialOut = true
			record = b.recordTrial
		}
	default:
		record = b.record
	}
	b.mu.Unlock()

	b.announce(moved)
	return record, err
}

func (b *Breaker) record(outcome Outcome) {
	b.mu.Lock()
	var from, to State
	changed := false

	switch outcome {
	case Success:
		b.failures = 0
	case Failure:
		b.failures++
		if b.state == Closed && b.failures >= b.cfg.FailureThreshold {
			from, to, changed = b.state, Open, true
			b.openLocked(b.cfg.Cooldown)
		}
	}
	b.mu.Unlock()

	if changed {
		b.notify(from, to)
	}
}

func (b *Breaker) recordTrial(outcome Outcome) {
	b.mu.Lock()
	b.trialOut = false
	from := b.state
	var to State
	changed := false

	if b.state == HalfOpen {
		switch outcome {
		case Success:
			b.state, b.failures, b.cooldown = Closed, 0, b.cfg.Cooldown
			to, changed = Closed, true
		case Failure:
			b.openLocked(min(b.cooldown*2, b.cfg.MaxCooldown))
			to, changed = Open, true
		}
		// Ignored leaves it half-open, with the trial slot free for the next
		// caller: an abandoned call learned nothing.
	}
	b.mu.Unlock()

	if changed {
		b.notify(from, to)
	}
}

func (b *Breaker) openLocked(cooldown time.Duration) {
	b.state = Open
	b.openedAt = b.now()
	b.cooldown = cooldown
	b.failures = 0
}

// advanceLocked moves an open breaker whose cooldown has passed to half-open,
// and reports whether it did. Done lazily, on the next look, rather than on a
// timer: nothing needs to happen at that moment unless someone wants to make a
// call. The caller announces the move once it has let go of the lock.
func (b *Breaker) advanceLocked() bool {
	if b.state == Open && b.now().Sub(b.openedAt) >= b.cooldown {
		b.state = HalfOpen
		b.trialOut = false
		return true
	}
	return false
}

func (b *Breaker) announce(moved bool) {
	if moved {
		b.notify(Open, HalfOpen)
	}
}

func (b *Breaker) notify(from, to State) {
	if b.onChange != nil && from != to {
		b.onChange(from, to)
	}
}
