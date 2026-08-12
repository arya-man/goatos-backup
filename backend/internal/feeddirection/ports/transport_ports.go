package ports

import (
	"context"
	"errors"
	"time"
)

var (
	ErrTransportProofRequired     = errors.New("feeddirection: a live feed-transport video is required")
	ErrTransportTaskNotActionable = errors.New("feeddirection: feed-transport task is not actionable")
	// ErrTransportParkForbidden: the task belongs to a park the caller is not scoped to. This is the
	// CLAMP that lets a park-scoped operator submit at all -- httpmiddleware.routeAllowsScopedGrants
	// admits a park grant on this route only because the park is checked here, against the TASK's own
	// park rather than a park named by the request (this route names none).
	ErrTransportParkForbidden             = errors.New("feeddirection: feed-transport task is outside the actor's park scope")
	ErrTransportAssignedToAnotherOperator = errors.New("feeddirection: feed-transport task is assigned to another operator")
	ErrInvalidTransportStatus             = errors.New("feeddirection: invalid feed-transport status")
)

// FeedTransportTask keeps PartitionLabel for pre-000152 rows only: transport tasks are created per
// physical shed and every row written since then carries an empty partition. Do not reintroduce a
// pen grain here -- packing and distribution own that.
type FeedTransportTask struct {
	TaskID, ParkID, ParkLabel, ShedID, ShedLabel, BusinessDate, Status string
	OperatorID, CurrentAttemptID, ReworkReason                         string
	PartitionLabel, OperationalLocationDisplay                         string
	ScheduledAt                                                        time.Time
}

// FeedTransportFilterOption.ID is a park UUID or a shed UUID -- never a composite pen key.
type FeedTransportFilterOption struct {
	ID, Label, PartitionLabel string
}

type FeedTransportFilterOptions struct {
	Parks, Sheds []FeedTransportFilterOption
}

type FeedTransportTaskPage struct {
	Items      []FeedTransportTask
	NextCursor string
	Filters    FeedTransportFilterOptions
}

// ListTransportTasksParams has NO partition filter. Transport is one task per physical shed, so
// there is no pen to narrow to; see MaterializeTransportTasks for why the grain is the shed.
type ListTransportTasksParams struct {
	TenantID, ActorID, ParkID, ShedID, Status, Cursor string
	Day                                               time.Time
	Limit                                             int
}

type MaterializeTransportParams struct {
	TenantID string
	AsOf     time.Time
}
type MaterializeTransportResult struct {
	BusinessDate string
	Inserted     int64
}

type SubmitTransportParams struct {
	TenantID, TaskID, ProofRef, OperatorID, IdempotencyKey, ActorID, ActorType, TraceID string
}
type SubmitTransportResult struct {
	AttemptID, Status, ParkID, ShedID string
	ShedName, PartitionLabel          string
	AttemptNo                         int32
	NewlyPending                      bool
}

type ApplyTransportParams struct{ TenantID, AttemptID, VerifiedBy, TraceID string }
type BounceTransportParams struct{ TenantID, AttemptID, Reason, VerifiedBy, TraceID string }

type TransportStore interface {
	MaterializeTransportTasks(context.Context, MaterializeTransportParams) (MaterializeTransportResult, error)
	GetTransportTask(context.Context, string, string) (FeedTransportTask, error)
	ListTransportTasks(context.Context, ListTransportTasksParams) (FeedTransportTaskPage, error)
	SubmitTransportAttempt(context.Context, SubmitTransportParams) (SubmitTransportResult, error)
	ApplyVerifiedTransport(context.Context, ApplyTransportParams) (bool, error)
	BounceTransportForRework(context.Context, BounceTransportParams) (bool, error)
}
