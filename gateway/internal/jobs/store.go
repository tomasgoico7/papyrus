package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultMaxAttempts = 3

// Store is the queue's persistence. It does storage and nothing else: whether a
// failure deserves another attempt, and how long to wait, is the worker's
// policy, not the table's.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// pollColumns is everything a caller polling a job needs — deliberately without
// the CV bytes, which would mean carrying megabytes through every status check.
const pollColumns = `
	id, user_id, dedup_key, state, attempts, max_attempts,
	cv_filename, job_offer, job_title,
	result, coalesce(error_code, ''), coalesce(error_message, ''),
	created_at, updated_at, finished_at`

// Enqueue adds a job, or returns the live one that already covers these inputs.
//
// The second return value reports whether this call created the job. A double
// submit — an impatient click, a retried request — therefore joins the work
// already queued instead of paying for it twice.
func (s *Store) Enqueue(ctx context.Context, input NewJob) (*Job, bool, error) {
	maxAttempts := input.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = defaultMaxAttempts
	}

	const insert = `
		insert into analysis_jobs (user_id, dedup_key, cv, cv_filename, job_offer, job_title, max_attempts, traceparent)
		values ($1, $2, $3, $4, $5, $6, $7, $8)
		on conflict (dedup_key) where state in ('queued', 'running') do nothing
		returning ` + pollColumns

	row := s.pool.QueryRow(ctx, insert,
		input.UserID, input.DedupKey, input.CV, input.CVFilename,
		input.JobOffer, nullable(input.JobTitle), maxAttempts,
		nullable(input.TraceParent),
	)

	job, err := scanJob(row)
	if err == nil {
		return job, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("jobs: enqueue: %w", err)
	}

	// The insert was skipped, so a live job already holds this key. Returning it
	// is the whole point of the conflict clause. The job keeps the trace of the
	// request that created it rather than this one's: the work belongs to the
	// first caller's trace, and this caller is joining it, not starting it.
	const existing = `
		select ` + pollColumns + `
		from analysis_jobs
		where dedup_key = $1 and state in ('queued', 'running')`

	job, err = scanJob(s.pool.QueryRow(ctx, existing, input.DedupKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// It finished between the two statements. Rare, and the caller can
			// simply try again rather than be handed a job that does not exist.
			return nil, false, ErrNotFound
		}
		return nil, false, fmt.Errorf("jobs: enqueue lookup: %w", err)
	}
	return job, false, nil
}

// Claim takes the oldest job that is due and marks it running, atomically.
//
// `for update skip locked` is what makes several workers safe against each
// other: a row already locked by another claim is passed over instead of
// waited on, so workers never queue up behind one another.
//
// A job whose claim is older than staleAfter is treated as due again: the
// worker that took it is assumed dead. That makes delivery at-least-once, which
// is why the work it guards has to tolerate being repeated.
func (s *Store) Claim(ctx context.Context, staleAfter time.Duration) (*Job, error) {
	const claim = `
		update analysis_jobs
		set state = 'running',
		    attempts = attempts + 1,
		    claimed_at = now(),
		    updated_at = now()
		where id = (
			select id
			from analysis_jobs
			where (state = 'queued' and run_after <= now())
			   or (
			        state = 'running'
			        and claimed_at < now() - make_interval(secs => $1)
			        -- The ceiling has to be enforced here and not only in the
			        -- worker. The worker's check runs when an attempt ends in an
			        -- error; a worker that is killed mid-attempt never reaches
			        -- it, so without this a job is reclaimed forever and its
			        -- attempts climb past the maximum. One job reached seven
			        -- against a limit of three that way, across a run of
			        -- deploys.
			        and attempts < max_attempts
			      )
			order by run_after, created_at
			for update skip locked
			limit 1
		)
		returning ` + pollColumns + `, cv, coalesce(traceparent, '')`

	var job Job
	var title, errCode, errMessage *string
	var result []byte

	var state string
	err := s.pool.QueryRow(ctx, claim, staleAfter.Seconds()).Scan(
		&job.ID, &job.UserID, &job.DedupKey, &state, &job.Attempts, &job.MaxAttempts,
		&job.CVFilename, &job.JobOffer, &title,
		&result, &errCode, &errMessage,
		&job.CreatedAt, &job.UpdatedAt, &job.FinishedAt,
		&job.CV, &job.TraceParent,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("jobs: claim: %w", err)
	}

	job.State = State(state)
	job.JobTitle = deref(title)
	job.ErrorCode = deref(errCode)
	job.ErrorMessage = deref(errMessage)
	job.Result = result
	return &job, nil
}

// Complete stores the result and ends the job, dropping the upload with it: the
// caller has the answer and the cache holds it, so the bytes are dead weight.
func (s *Store) Complete(ctx context.Context, id string, result json.RawMessage) error {
	// The cast is load-bearing. Without a prepared statement to carry the
	// column's type — which is the case in exec mode, the mode a transaction
	// pooler forces — the driver has no way to know this parameter is json, and
	// Postgres rejects it. Passing it as text with an explicit cast says so
	// outright, and works in either mode.
	const query = `
		update analysis_jobs
		set state = 'done', result = $2::jsonb, cv = null,
		    error_code = null, error_message = null,
		    finished_at = now(), updated_at = now()
		where id = $1 and state = 'running'`

	return s.exec(ctx, "complete", query, id, string(result))
}

// Retry puts a job back in the queue, held until runAfter. The delay is the
// worker's to choose; the store only records it.
func (s *Store) Retry(ctx context.Context, id string, runAfter time.Time, code, message string) error {
	const query = `
		update analysis_jobs
		set state = 'queued', run_after = $2, claimed_at = null,
		    error_code = $3, error_message = $4, updated_at = now()
		where id = $1 and state = 'running'`

	return s.exec(ctx, "retry", query, id, runAfter, code, message)
}

// Fail ends a job for good, and deliberately keeps the upload.
//
// A success drops its bytes immediately: the client has the result and the cache
// holds it. A dead letter is the one somebody will want to act on, and a job
// that cannot be re-run is not much of a dead letter queue — so the bytes stay
// until the retention sweep takes the whole row. Failures are rare enough that
// the space is not the constraint.
func (s *Store) Fail(ctx context.Context, id, code, message string) error {
	const query = `
		update analysis_jobs
		set state = 'failed',
		    error_code = $2, error_message = $3,
		    finished_at = now(), updated_at = now()
		where id = $1 and state = 'running'`

	return s.exec(ctx, "fail", query, id, code, message)
}

// Get reads one job, scoped to its owner.
//
// The user id is part of the where clause rather than checked afterwards: a
// filter that runs in the caller is a filter someone eventually forgets.
func (s *Store) Get(ctx context.Context, id, userID string) (*Job, error) {
	const query = `
		select ` + pollColumns + `
		from analysis_jobs
		where id = $1 and user_id = $2`

	job, err := scanJob(s.pool.QueryRow(ctx, query, id, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("jobs: get: %w", err)
	}
	return job, nil
}

// Depth is what the queue looks like right now.
type Depth struct {
	Queued  int
	Running int
	// Dead is the standing dead letter count: jobs that gave up and are still
	// on the table. A counter of failures says how often it happens; this says
	// how much is sitting there unattended, which is the one worth alerting on.
	Dead int
}

// Measure counts the queue for the metrics gauges.
func (s *Store) Measure(ctx context.Context) (Depth, error) {
	const query = `
		select
			count(*) filter (where state = 'queued'),
			count(*) filter (where state = 'running'),
			count(*) filter (where state = 'failed')
		from analysis_jobs`

	var depth Depth
	if err := s.pool.QueryRow(ctx, query).Scan(&depth.Queued, &depth.Running, &depth.Dead); err != nil {
		return Depth{}, fmt.Errorf("jobs: measure: %w", err)
	}
	return depth, nil
}

// PurgeFinished deletes jobs that have outlived their usefulness.
//
// Nothing else removes them: the upload is dropped when a job ends, but the row
// and its result stay. The client collects a result within seconds and the
// cache holds it afterwards, so a finished job is worth keeping only long
// enough to debug — and a dead one longer than a successful one, because that
// is the one somebody will want to look at.
// FailAbandoned ends jobs that were claimed and never finished, and have no
// attempts left to give. They exist because a process can die between claiming
// work and recording what happened to it — a deploy, an eviction, a crash.
//
// Without this they are unreachable: the claim is stale so no worker will look
// at them again now that the reclaim respects the ceiling, and nothing else
// moves a running job. They would sit in the queue counting against its depth
// forever, and the person waiting would never be told.
func (s *Store) FailAbandoned(ctx context.Context, staleAfter time.Duration) (int64, error) {
	const query = `
		update analysis_jobs
		set state = 'failed',
		    error_code = 'worker_lost',
		    error_message = 'The analysis was interrupted and could not be retried.',
		    finished_at = now(), updated_at = now()
		where state = 'running'
		  and claimed_at < now() - make_interval(secs => $1)
		  and attempts >= max_attempts`

	tag, err := s.pool.Exec(ctx, query, staleAfter.Seconds())
	if err != nil {
		return 0, fmt.Errorf("jobs: fail abandoned: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) PurgeFinished(ctx context.Context, doneAfter, deadAfter time.Duration) (int64, error) {
	const query = `
		delete from analysis_jobs
		where (state = 'done' and finished_at < now() - make_interval(secs => $1))
		   or (state = 'failed' and finished_at < now() - make_interval(secs => $2))`

	tag, err := s.pool.Exec(ctx, query, doneAfter.Seconds(), deadAfter.Seconds())
	if err != nil {
		return 0, fmt.Errorf("jobs: purge: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (s *Store) exec(ctx context.Context, op, query string, args ...any) error {
	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("jobs: %s: %w", op, err)
	}
	if tag.RowsAffected() == 0 {
		// The job moved on — reclaimed after a stale timeout, most likely. The
		// caller is holding a result for work somebody else now owns.
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (*Job, error) {
	var job Job
	var state string
	var title, errCode, errMessage *string
	var result []byte

	err := row.Scan(
		&job.ID, &job.UserID, &job.DedupKey, &state, &job.Attempts, &job.MaxAttempts,
		&job.CVFilename, &job.JobOffer, &title,
		&result, &errCode, &errMessage,
		&job.CreatedAt, &job.UpdatedAt, &job.FinishedAt,
	)
	if err != nil {
		return nil, err
	}

	job.State = State(state)
	job.JobTitle = deref(title)
	job.ErrorCode = deref(errCode)
	job.ErrorMessage = deref(errMessage)
	job.Result = result
	return &job, nil
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
