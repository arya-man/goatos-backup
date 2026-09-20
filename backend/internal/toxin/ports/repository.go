// Package ports declares the seams the toxin module's app service depends on.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
)

var (
	// ErrTaskNotFound reports a task id that does not exist in the caller's tenant.
	ErrTaskNotFound = errors.New("toxin: task not found")
	// ErrVersionConflict reports a verdict fenced on a row_version the task has moved past.
	ErrVersionConflict = errors.New("toxin: task changed underneath the verdict")
	// ErrInvalidProof reports a capture ref that is unknown, wrong-tenant, unfinished, the
	// wrong media kind, or not captured by the in-app camera.
	ErrInvalidProof = errors.New("toxin: proof capture is not usable")
	// ErrInvalidArgument reports malformed caller input that reached the persistence seam,
	// such as a keyset cursor that was not minted by this endpoint.
	ErrInvalidArgument = errors.New("toxin: invalid argument")
	// ErrIdempotencyConflict reports a same-key different-payload replay.
	ErrIdempotencyConflict = errors.New("toxin: idempotency key was already used for a different request")
)

// CreateTaskParams carries the load context a feed purchase event denormalizes onto the
// round-1 task at creation. Creation is idempotent on (tenant_id, feed_purchase_id, 1).
type CreateTaskParams struct {
	TenantID       string
	FeedPurchaseID string
	FarmLabel      string
	FeedItemKey    string
	FeedItemLabel  string
	Vendor         string
	BatchNo        int
	PurchaseDate   string // business DATE, YYYY-MM-DD
	QuantityKg     float64
	// SourceEventID threads the triggering event id into audit metadata.
	SourceEventID string
}

// ListTasksParams pages the task list by keyset. Statuses filters to the requested
// buckets; empty means every status.
type ListTasksParams struct {
	TenantID string
	Statuses []string
	Limit    int
	Cursor   string
}

// TaskRow is one list row: the task, its step completions (bounded -- at most one per working
// step), and the PROCEDURE the round runs, so the chip and the step list render without a
// per-row query and without any surface assuming a fixed seven steps.
type TaskRow struct {
	Task        domain.Task
	Completions []domain.StepCompletion
	// Procedure is the authored procedure of this round's own sop_version, attached by the app
	// service. A row read straight off the repository carries the zero value; transports are
	// served by the service and always have it.
	Procedure domain.Procedure
}

// TaskPage is one keyset page plus the whole-filter status counts the screens badge from.
type TaskPage struct {
	Rows       []TaskRow
	NextCursor string
	// StatusCounts is whole-tenant (per status), never page-local.
	StatusCounts map[string]int
}

// CompleteStepParams records one working step's video.
type CompleteStepParams struct {
	TenantID       string
	TaskID         string
	StepNo         int
	ProofRef       string
	ActorID        string
	IdempotencyKey string
	// Now is the server clock the wait gates are checked against.
	Now time.Time
	// Procedure is the round's own authored procedure, resolved by the app service before the
	// call. The repository re-checks it against the locked row's sop_version, so a procedure
	// published between the service's read and the lock is REFUSED rather than silently applied.
	Procedure domain.Procedure
}

// SubmitParams records step 7: the strip photo plus the reading.
type SubmitParams struct {
	TenantID       string
	TaskID         string
	Outcome        string
	StripPhotoRef  string
	ActorID        string
	IdempotencyKey string
	Now            time.Time
	// Procedure is the round's own authored procedure (see CompleteStepParams).
	Procedure domain.Procedure
}

// VerdictParams records the CEO/CXO accept/reject on a submitted round.
type VerdictParams struct {
	TenantID       string
	TaskID         string
	Decision       string
	Reason         string
	RowVersion     int64
	ActorID        string
	IdempotencyKey string
}

// Repository is the toxin module's persistence seam.
type Repository interface {
	// CreateTaskFromPurchase creates the load's round-1 task if it does not already
	// exist. A replayed event is a no-op, never a second task.
	CreateTaskFromPurchase(ctx context.Context, p CreateTaskParams) error
	ListTasks(ctx context.Context, p ListTasksParams) (TaskPage, error)
	GetTask(ctx context.Context, tenantID, taskID string) (TaskRow, error)
	// CompleteStep records one working step under the task row lock, enforcing order and
	// the server-clock wait gates.
	CompleteStep(ctx context.Context, p CompleteStepParams) (TaskRow, error)
	// SubmitReading records step 7. An Invalid reading cancels the round and mints its
	// retest in the SAME transaction.
	SubmitReading(ctx context.Context, p SubmitParams) (TaskRow, error)
	// RecordVerdict applies the CEO/CXO decision. A reject cancels the round and mints
	// its retest in the SAME transaction.
	RecordVerdict(ctx context.Context, p VerdictParams) (TaskRow, error)
}

// ErrProcedureChanged reports a round whose procedure version moved between the service's read
// and the row lock -- a step is then judged against a document that is no longer the round's, so
// the write is refused and the caller reopens the task.
var ErrProcedureChanged = errors.New("toxin: the procedure changed while the step was being recorded")

// ErrProcedureVersionUnknown names a procedure version no published or retired
// `procurement.toxin_test` SOP carries.
var ErrProcedureVersionUnknown = errors.New("toxin: procedure version unknown")

// ProcedureSource reads an authored toxin procedure from the SOP library. A version the library
// no longer carries is an error, except version 1, which is always the seeded document.
type ProcedureSource interface {
	// PublishedProcedure is what a round opening now would run.
	PublishedProcedure(ctx context.Context, tenantID string) (domain.Procedure, error)
	// ProcedureVersion is the exact procedure a round was opened on.
	ProcedureVersion(ctx context.Context, tenantID string, version int) (domain.Procedure, error)
}

// ProofValidator asserts a submitted capture is real evidence: a completed upload in the
// caller's tenant, declared AND stored as the expected kind, captured by the in-app
// camera.
type ProofValidator interface {
	ValidateToxinStepVideo(ctx context.Context, tenantID, proofRef string) error
	ValidateToxinStripPhoto(ctx context.Context, tenantID, proofRef string) error
}
