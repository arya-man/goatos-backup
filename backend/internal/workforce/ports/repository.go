package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

var (
	ErrNotFound              = errors.New("not found")
	ErrConflict              = errors.New("write conflict")
	ErrInvalidFilter         = errors.New("invalid filter")
	ErrDenied                = errors.New("denied")
	ErrIdempotencyConflict   = errors.New("idempotency key conflict: same key with different payload")
)

type ListOperatorsParams struct {
	TenantID   string
	Status     string
	RoleHint   string
	LocationID string
	Search     string
	Limit      int
}

type ListSourceCandidatesParams struct {
	TenantID     string
	Status       string
	SourceSystem string
	Limit        int
}

type CreateOperatorCommand struct {
	TenantID string
	ActorID  string
	Body     domain.CreateOperatorRequest
}

type UpdateOperatorCommand struct {
	TenantID   string
	ActorID    string
	OperatorID string
	Body       domain.UpdateOperatorRequest
}

type StatusCommand struct {
	TenantID   string
	ActorID    string
	OperatorID string
	Reason     string
	RowVersion int
	Status     string
}

type CreateGrantCommand struct {
	TenantID   string
	ActorID    string
	OperatorID string
	Body       domain.CreateGrantRequest
}

type CapabilityCommand struct {
	TenantID   string
	ActorID    string
	OperatorID string
	Body       domain.CreateCapabilityRequest
}

type RemoveCapabilityCommand struct {
	TenantID     string
	ActorID      string
	OperatorID   string
	CapabilityID string
}

type RevokeDeviceCommand struct {
	TenantID   string
	ActorID    string
	OperatorID string
	DeviceID   string
	Reason     string
	RowVersion int
}

type MapSourceCandidateCommand struct {
	TenantID    string
	ActorID     string
	CandidateID string
	Body        domain.MapSourceCandidateRequest
}

type RejectSourceCandidateCommand struct {
	TenantID    string
	ActorID     string
	CandidateID string
	Body        domain.RejectSourceCandidateRequest
}

type RegisterDeviceCommand struct {
	TenantID string
	ActorID  string
	Body     domain.RegisterDeviceRequest
}

type HeartbeatDeviceCommand struct {
	TenantID string
	ActorID  string
	DeviceID string
	Body     domain.HeartbeatDeviceRequest
}

type Repository interface {
	ListOperators(ctx context.Context, params ListOperatorsParams) ([]domain.OperatorProfile, error)
	CreateOperator(ctx context.Context, cmd CreateOperatorCommand) (domain.OperatorProfile, error)
	GetOperator(ctx context.Context, tenantID, operatorID string) (domain.OperatorProfile, error)
	UpdateOperator(ctx context.Context, cmd UpdateOperatorCommand) (domain.OperatorProfile, error)
	SetOperatorStatus(ctx context.Context, cmd StatusCommand) (domain.OperatorProfile, error)
	ListGrants(ctx context.Context, tenantID, operatorID string) ([]domain.GrantSummary, error)
	CreateGrant(ctx context.Context, cmd CreateGrantCommand) (domain.GrantSummary, error)
	AssignCapability(ctx context.Context, cmd CapabilityCommand) (domain.CapabilityAssignment, error)
	RemoveCapability(ctx context.Context, cmd RemoveCapabilityCommand) error
	ListCapabilities(ctx context.Context, tenantID, operatorID string) ([]domain.CapabilityAssignment, error)
	ListDevices(ctx context.Context, tenantID, operatorID string) ([]domain.DeviceSummary, error)
	RevokeDevice(ctx context.Context, cmd RevokeDeviceCommand) (domain.DeviceSummary, error)
	ListSourceCandidates(ctx context.Context, params ListSourceCandidatesParams) ([]domain.SourceCandidate, error)
	MapSourceCandidate(ctx context.Context, cmd MapSourceCandidateCommand) (domain.SourceCandidate, error)
	RejectSourceCandidate(ctx context.Context, cmd RejectSourceCandidateCommand) (domain.SourceCandidate, error)
	GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)
	ListActiveGrantsForActor(ctx context.Context, tenantID, actorID string) ([]domain.GrantSummary, error)
	RegisterDevice(ctx context.Context, cmd RegisterDeviceCommand) (domain.DeviceSummary, error)
	HeartbeatDevice(ctx context.Context, cmd HeartbeatDeviceCommand) (domain.DeviceSummary, error)
	GetDeviceForActor(ctx context.Context, tenantID, actorID, deviceID string) (domain.DeviceSummary, error)
}
