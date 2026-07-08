package app

import "context"

// Axis identifies which status dimension a bulk job mutates. These mirror the
// bulk_status_job axis CHECK constraint in migration 000145.
const (
	AxisReproductive = "reproductive"
	AxisHealth       = "health"
	AxisExit         = "exit"
)

// Job lifecycle states, mirroring the bulk_status_job state CHECK constraint.
const (
	JobStatePending   = "pending"
	JobStateRunning   = "running"
	JobStateCompleted = "completed"
	JobStateFailed    = "failed"
	JobStateCanceled  = "canceled"
)

// RowOutcome is the terminal (or retryable) result of applying one bulk row. It
// maps 1:1 onto the bulk_status_job_row row_state values the worker persists.
type RowOutcome string

const (
	OutcomeApplied RowOutcome = "applied"
	OutcomeSkipped RowOutcome = "skipped"
	OutcomeRetry   RowOutcome = "retry"
	OutcomeError   RowOutcome = "error"
)

// GoatState is the minimal live snapshot the preview needs to decide, per row,
// whether a transition would apply, is already satisfied, or is out of scope.
type GoatState struct {
	GoatID             string
	Exists             bool
	Merged             bool
	LifecycleStatus    string
	ReproductiveStatus string
	HealthStatus       string
	ManagementStage    string
	RowVersion         int
}

// GoatStateReader reads current goat state for a bounded set of goat ids. It is a
// read-only view over goat identity state; the write path stays inside the
// identity module via GoatTransitionApplier.
type GoatStateReader interface {
	ReadGoatStates(ctx context.Context, tenantID string, goatIDs []string) (map[string]GoatState, error)
}

// EnqueueRow is one goat's requested change carried into the durable job.
type EnqueueRow struct {
	GoatID string
	Target string
	Reason string
}

// EnqueueJobParams is the set-based enqueue payload. Rows are inserted with a
// server-side JOIN to capture each goat's current row_version as
// expected_row_version; no per-row round trip and no goat mutation happens here.
type EnqueueJobParams struct {
	TenantID       string
	ActorID        string
	Axis           string
	IdempotencyKey string
	Fingerprint    string
	Rows           []EnqueueRow
}

// EnqueueJobResult reports the enqueued (or replayed) job.
type EnqueueJobResult struct {
	JobID     string
	TotalRows int
	State     string
	Replayed  bool
}

// ClaimedRow is one row a worker has locked (row_state -> claimed) for apply.
// ActorID carries the enqueuing operator (bulk_status_job.actor_id) so the
// per-goat identity transition is attributed to the human who committed the
// job, not an anonymous worker. The identity write path requires a real actor.
type ClaimedRow struct {
	RowID              string
	GoatID             string
	ActorID            string
	Axis               string
	Target             string
	Reason             string
	ExpectedRowVersion *int
	RetryCount         int
}

// JobCounts is the recomputed ledger rollup after a worker pass.
type JobCounts struct {
	Total     int
	Applied   int
	Skipped   int
	Failed    int
	Remaining int
	State     string
}

// BulkStatusRepository owns the bulk_status_job / bulk_status_job_row tables.
type BulkStatusRepository interface {
	// ReproductiveStatusExists validates a reproductive target against the
	// source-backed vocabulary (active status_definitions on the reproductive axis).
	ReproductiveStatusExists(ctx context.Context, code string) (bool, error)
	// EnqueueJob transactionally inserts a job + one row per goat. It is
	// idempotent on (tenant_id, idempotency_key): an exact replay returns the
	// original job; a same-key different-fingerprint replay is rejected.
	EnqueueJob(ctx context.Context, params EnqueueJobParams) (EnqueueJobResult, error)
	// ClaimRows locks up to limit claimable rows for the tenant+job using FOR
	// UPDATE SKIP LOCKED and marks them claimed. Claimable = pending|retry rows
	// plus stale claimed rows whose claimed_at is older than leaseSeconds (lease
	// recovery for a worker that died mid-apply). leaseSeconds <= 0 disables
	// stale reclaim.
	ClaimRows(ctx context.Context, tenantID, jobID string, limit, leaseSeconds int) ([]ClaimedRow, error)
	MarkRowApplied(ctx context.Context, tenantID, rowID, eventID string) error
	MarkRowSkipped(ctx context.Context, tenantID, rowID, reason string) error
	// MarkRowRetry increments retry_count; when the new count reaches maxRetries
	// the row is parked as 'error' instead of 'retry'. It returns the resulting
	// state so the worker can account it.
	MarkRowRetry(ctx context.Context, tenantID, rowID, reason string, maxRetries int) (RowOutcome, error)
	MarkRowError(ctx context.Context, tenantID, rowID, reason string) error
	// RefreshJobCounts recomputes applied/skipped/failed counts from the row
	// ledger and advances job state (running while rows remain, completed when
	// drained).
	RefreshJobCounts(ctx context.Context, tenantID, jobID string) (JobCounts, error)
	// BumpJobCounts applies a per-pass delta to the job counters (O(1)) instead of
	// a full recount each drain pass; RefreshJobCounts still reconciles at the end.
	BumpJobCounts(ctx context.Context, tenantID, jobID string, appliedDelta, skippedDelta, failedDelta int) error
	// ListJobIDsWithClaimableRows returns tenant jobs that still have pending|retry|claimed rows.
	ListJobIDsWithClaimableRows(ctx context.Context, tenantID string, limit int) ([]string, error)
	// ListJobIDsNeedingRollup returns active jobs whose rows are all terminal but
	// whose state/counters were never refreshed (terminal crash-orphan).
	ListJobIDsNeedingRollup(ctx context.Context, tenantID string, limit int) ([]string, error)
}

// ApplyRowRequest is one goat transition the worker asks the applier to perform.
type ApplyRowRequest struct {
	TenantID           string
	ActorID            string
	JobID              string
	GoatID             string
	Axis               string
	Target             string
	Reason             string
	ExpectedRowVersion int
	TraceID            string
}

// ApplyRowResult is the applier's classified outcome for one row.
type ApplyRowResult struct {
	Outcome RowOutcome
	EventID string
	Reason  string
}

// GoatTransitionApplier applies one bulk row through the real per-goat identity
// transition (so the proper event + guardrails fire) and classifies the result
// into a RowOutcome. Implemented by the identity bridge adapter. A returned
// (non-nil) error means an unexpected/transport failure the worker treats as
// retryable; expected business outcomes are carried in ApplyRowResult.Outcome.
type GoatTransitionApplier interface {
	ApplyBulkStatusRow(ctx context.Context, req ApplyRowRequest) (ApplyRowResult, error)
}
