package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/sop/domain"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("write conflict")
	ErrInvalidFilter       = errors.New("invalid filter")
	ErrDenied              = errors.New("denied")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
)

type ListSOPsParams struct {
	TenantID string
	Status   string
	Limit    int
}

type ListTasksParams struct {
	TenantID   string
	ActorID    string
	State      string
	AssignedTo string
	ScopeType  string
	ScopeID    string
	Limit      int
	AppView    bool
}

type CreateSOPCommand struct {
	TenantID string
	ActorID  string
	Body     domain.CreateSOPRequest
}

type CreateVersionCommand struct {
	TenantID string
	ActorID  string
	SOPID    string
	Body     domain.CreateSOPVersionRequest
	Report   domain.ValidationReport
}

type VersionCommand struct {
	TenantID     string
	ActorID      string
	SOPID        string
	SOPVersionID string
	RowVersion   int
}

type CreateTaskCommand struct {
	TenantID string
	ActorID  string
	Body     domain.CreateTaskRequest
}

type AssignTaskCommand struct {
	TenantID string
	ActorID  string
	TaskID   string
	Body     domain.AssignTaskRequest
}

type ReviewTaskCommand struct {
	TenantID string
	ActorID  string
	TaskID   string
	State    string
	Body     domain.ReviewTaskRequest
}

type ReviewFanoutStatusCommand struct {
	TenantID       string
	TaskID         string
	TaskRowVersion int
	Outcome        string
	ActorID        string
	Reason         string
	Status         string
	LastError      string
}

type ReviewFanoutAttempt struct {
	TenantID       string
	TaskID         string
	TaskRowVersion int
	Outcome        string
	ActorID        string
	Reason         string
}

type SubmissionFanoutStatusCommand struct {
	TenantID     string
	TaskID       string
	SubmissionID string
	ActorID      string
	Status       string
	LastError    string
}

type SubmissionFanoutAttempt struct {
	TenantID     string
	TaskID       string
	SubmissionID string
	ActorID      string
}

type ListAgedFailedSubmissionFanoutsParams struct {
	TenantID      string
	UpdatedBefore time.Time
	Now           time.Time
	Limit         int
}

type SubmitTaskCommand struct {
	TenantID                 string
	ActorID                  string
	TaskID                   string
	Body                     domain.SubmitTaskRequest
	Report                   domain.ValidationReport
	ItemState                string
	TaskState                string
	MovementPayload          map[string]any
	SubmissionItems          []SubmissionItemInput
	SubmissionFanoutRequired bool
}

type SubmissionItemInput struct {
	GoatID  string
	ItemKey string
}

type Repository interface {
	ListSOPs(ctx context.Context, params ListSOPsParams) ([]domain.SOPDefinition, error)
	CreateSOP(ctx context.Context, cmd CreateSOPCommand) (domain.SOPDefinition, error)
	GetSOP(ctx context.Context, tenantID, sopID string) (domain.SOPDefinition, *domain.SOPVersion, error)
	// LatestVersionsFor batch-loads the latest version of each requested SOP in a single query,
	// keyed by sop_id. SOPs with no version are absent from the map. This is the O(1) replacement for
	// calling GetSOP once per listed SOP.
	LatestVersionsFor(ctx context.Context, tenantID string, sopIDs []string) (map[string]domain.SOPVersion, error)
	CreateVersion(ctx context.Context, cmd CreateVersionCommand) (domain.SOPVersion, error)
	GetVersion(ctx context.Context, tenantID, sopID, versionID string) (domain.SOPVersion, error)
	GetVersionByID(ctx context.Context, tenantID, versionID string) (domain.SOPVersion, error)
	GetPublishedVersionByCode(ctx context.Context, tenantID, sopCode string) (domain.SOPVersion, error)
	PublishVersion(ctx context.Context, cmd VersionCommand) (domain.SOPVersion, error)
	RetireVersion(ctx context.Context, cmd VersionCommand) (domain.SOPVersion, error)
	ListTasks(ctx context.Context, params ListTasksParams) ([]domain.TaskSummary, error)
	CreateTask(ctx context.Context, cmd CreateTaskCommand) (domain.TaskSummary, error)
	CreateTasksForBatches(ctx context.Context, tenantID, sopVersionID, actorID string, tasks []domain.BatchTaskRequest) (map[string]string, error)
	GetTask(ctx context.Context, tenantID, taskID string) (domain.TaskSummary, *domain.SOPVersion, []domain.SubmissionSummary, error)
	AssignTask(ctx context.Context, cmd AssignTaskCommand) (domain.TaskSummary, error)
	ReviewTask(ctx context.Context, cmd ReviewTaskCommand) (domain.TaskSummary, error)
	ListPendingReviewFanouts(ctx context.Context, tenantID string, limit int) ([]ReviewFanoutAttempt, error)
	RecordReviewFanoutStatus(ctx context.Context, cmd ReviewFanoutStatusCommand) error
	ListPendingSubmissionFanouts(ctx context.Context, tenantID string, limit int) ([]SubmissionFanoutAttempt, error)
	RecordSubmissionFanoutStatus(ctx context.Context, cmd SubmissionFanoutStatusCommand) error
	ListAgedFailedSubmissionFanouts(ctx context.Context, params ListAgedFailedSubmissionFanoutsParams) ([]domain.FailedSubmissionFanout, error)
	SubmitTask(ctx context.Context, cmd SubmitTaskCommand) (domain.SubmissionSummary, domain.TaskSummary, bool, error)
}
