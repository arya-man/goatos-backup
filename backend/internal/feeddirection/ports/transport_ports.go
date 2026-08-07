package ports

import (
	"context"
	"errors"
	"time"
)

var (
	ErrTransportProofRequired             = errors.New("feeddirection: a live feed-transport video is required")
	ErrTransportTaskNotActionable         = errors.New("feeddirection: feed-transport task is not actionable")
	ErrTransportAssignedToAnotherOperator = errors.New("feeddirection: feed-transport task is assigned to another operator")
	ErrInvalidTransportStatus             = errors.New("feeddirection: invalid feed-transport status")
)

type FeedTransportTask struct {
	TaskID, ParkID, ParkLabel, ShedID, ShedLabel, BusinessDate, Status string
	OperatorID, CurrentAttemptID, ReworkReason                         string
	PartitionLabel, OperationalLocationDisplay                         string
	ScheduledAt                                                        time.Time
}

type FeedTransportFilterOption struct {
	ID, Label string
}

type FeedTransportFilterOptions struct {
	Parks, Sheds []FeedTransportFilterOption
}

type FeedTransportTaskPage struct {
	Items      []FeedTransportTask
	NextCursor string
	Filters    FeedTransportFilterOptions
}

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
