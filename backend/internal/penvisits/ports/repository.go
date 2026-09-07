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

// ListParams selects one page of the caller's own visits.
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
	// StateCounts range over the SAME assignee predicate as the rows (never page-local, never
	// tenant-wide), keyed by work state.
	StateCounts map[string]int
}

// SubmitParams records the visit's video and completes the task.
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

// CreatedDigest is what the materializer hands the notifier: the pens now owed per assignee,
// for the parks that gained tasks on this pass.
type CreatedDigest struct {
	ParkID     string
	ParkName   string
	AssigneeID string
	DueDate    string
	Tasks      []domain.Task
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
	// OpenCount answers the module badge for one person: visits still owed.
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
