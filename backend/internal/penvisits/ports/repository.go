// Package ports declares the seams the Pen Visit module depends on.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
)

// Sentinel errors the adapters return; the transport maps them.
var (
	ErrTaskNotFound        = errors.New("pen visit: not found")
	ErrIdempotencyConflict = errors.New("pen visit: idempotency conflict")
	ErrInvalidArgument     = errors.New("pen visit: invalid argument")
)

// ListParams selects one page of the visits owed in the parks the caller is configured to
// visit.
type ListParams struct {
	TenantID string
	UserID   string
	States   []string
	Limit    int
	Cursor   string
}

// Page is one page plus the whole-list counts the chips show.
type Page struct {
	Rows       []domain.Task
	NextCursor string
	// StateCounts range over the SAME visitor-park predicate as the rows (never page-local,
	// never tenant-wide), keyed by work state.
	StateCounts map[string]int
}

// SubmitParams records the visit's video and hands it to the verifier.
type SubmitParams struct {
	TenantID       string
	Actor          domain.Actor
	TaskID         string
	ProofRef       string
	RowVersion     int
	IdempotencyKey string
	TraceID        string
}

// MaterializeResult is what one materializer pass did, for the log line.
type MaterializeResult struct {
	SourceDate string
	Created    int
	Widened    int
	// ParksWithoutAssignee names parks whose pens had work but no config row -- the loud gap.
	ParksWithoutAssignee []string
	// PensSkipped counts pens in those parks; nothing was written for them.
	PensSkipped int
}

// CreatedDigest is what the materializer hands the notifier: the pens now owed in one park,
// for the parks that gained tasks on this pass. VisitorIDs are everyone the park's HRMS
// config names; the push reaches each of them.
type CreatedDigest struct {
	ParkID     string
	ParkName   string
	VisitorIDs []string
	DueDate    string
	Tasks      []domain.Task
}

// VerdictParams is the verifier's decision on one visit, applied by the verdict consumer.
type VerdictParams struct {
	TenantID   string
	TaskID     string
	VerifiedBy string
	Reason     string
	TraceID    string
}

// VerdictResult is what a verdict write did, so the consumer can close the parents.
type VerdictResult struct {
	Applied bool
	Task    domain.Task
}

// SweepResult is one roll-forward pass.
type SweepResult struct {
	RolledForward int
	Truncated     bool
}

// Repository persists visits.
type Repository interface {
	ListMine(ctx context.Context, p ListParams) (Page, error)
	GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error)
	Submit(ctx context.Context, p SubmitParams) (domain.Task, error)
	// ApplyVerified flips an approved visit pending_verification -> completed on both
	// dimensions. Idempotent: a replay, or a verdict on a row no longer pending, applies
	// nothing and says so.
	ApplyVerified(ctx context.Context, p VerdictParams) (VerdictResult, error)
	// BounceForRework flips a rejected visit pending_verification -> rework with the
	// verifier's reason; the clip stays as history and the next submit replaces it.
	BounceForRework(ctx context.Context, p VerdictParams) (VerdictResult, error)
	// ForSources reads the visit each parent owes, keyed by the parent's ref id. One batched
	// read per page of parents, never one per row.
	ForSources(ctx context.Context, tenantID, sourceKind string, refIDs []string) (map[string]domain.Task, error)
	// OpenCount answers the badge for one person: visits still awaiting a recording in the
	// parks they are configured to visit.
	OpenCount(ctx context.Context, tenantID, userID string) (int, error)
	// Materialize writes one task per pen that had preventive-care work submitted on sourceDate,
	// due on the later of sourceDate+1 and today, for every park with a configured assignee. It
	// is idempotent on the natural key. It returns the digests of tasks CREATED by this call so
	// the caller can push once per park; a replay returns none.
	Materialize(ctx context.Context, tenantID string, sourceDate, today string, now time.Time) (MaterializeResult, []CreatedDigest, error)
	// DueDigestsForSourceDate rebuilds the bounded notification digest from existing open tasks
	// for sourceDate. Kernel ticks use it after a materialize replay so a transient notification
	// queue failure after the original commit can still be retried.
	DueDigestsForSourceDate(ctx context.Context, tenantID, sourceDate string) ([]CreatedDigest, error)
	// SweepRollForward moves unfinished visits whose due date has passed to today as delayed.
	SweepRollForward(ctx context.Context, tenantID string, asOf time.Time, chunkSize, maxChunks int) (SweepResult, error)
}

// ProofValidator asserts a proof is a finished, tenant-owned, live-camera VIDEO.
type ProofValidator interface {
	ValidateLiveCameraVideos(ctx context.Context, tenantID string, proofIDs []string) error
}
