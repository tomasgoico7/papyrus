// Package jobs is the analysis queue: a table in Postgres, claimed with
// `for update skip locked`.
//
// A broker would have been the obvious choice, and is the wrong one here. The
// volume is low, the durability requirement is the same one the database
// already satisfies, and `skip locked` gives a correct competing-consumer queue
// in a single statement. See docs/adr/0008.
package jobs

import (
	"encoding/json"
	"errors"
	"time"
)

// ErrNotFound reports that no job matched — either it never existed, or it
// belongs to someone else. The two are deliberately indistinguishable to the
// caller, so a probe cannot use the difference to discover another user's ids.
var ErrNotFound = errors.New("jobs: not found")

// State is where a job sits in its lifecycle.
//
//	queued ──claim──> running ──success──> done
//	   ^                  │
//	   └──retry───────────┴──attempts exhausted──> failed
type State string

const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// IsTerminal reports whether a job in this state will never run again.
func (s State) IsTerminal() bool {
	return s == StateDone || s == StateFailed
}

// Job is one queued analysis.
type Job struct {
	ID          string
	UserID      string
	DedupKey    string
	State       State
	Attempts    int
	MaxAttempts int

	// CV holds the upload while the job needs it, and is cleared when the job
	// reaches a terminal state. It is only populated on a claim: listing or
	// polling a job never carries the bytes around.
	CV         []byte
	CVFilename string
	JobOffer   string
	JobTitle   string

	Result       json.RawMessage
	ErrorCode    string
	ErrorMessage string

	// TraceParent is only populated on a claim, like CV: a caller polling a job
	// has its own trace and no use for the one that created it.
	TraceParent string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	FinishedAt *time.Time
}

// NewJob is the set of inputs needed to enqueue an analysis.
type NewJob struct {
	UserID      string
	DedupKey    string
	CV          []byte
	CVFilename  string
	JobOffer    string
	JobTitle    string
	MaxAttempts int
	// TraceParent is the W3C trace context of the request doing the enqueueing,
	// so the worker can continue that trace rather than start its own. Empty
	// when tracing is off, which is a job like any other.
	TraceParent string
}
