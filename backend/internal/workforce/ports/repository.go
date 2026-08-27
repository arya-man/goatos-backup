package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("write conflict")
	ErrInvalidFilter       = errors.New("invalid filter")
	ErrDenied              = errors.New("denied")
	ErrIdempotencyConflict = errors.New("idempotency key conflict: same key with different payload")
	ErrIdempotencyInFlight = errors.New("idempotency key is already in flight")
	// ErrMinOperatorCoverage signals that a leave-approval transition, if committed, would drop a
	// park's available vaccination-operator count below 1 on some business day it covers. Raised
	// from INSIDE the same transaction as the approval's status write (behind a per
	// tenant+park+day pg_advisory_xact_lock), so it also serializes concurrent approvals racing
	// for the same park's last remaining operator -- see RosterRepository.ApproveLeave.
	ErrMinOperatorCoverage = errors.New("approving this leave would leave fewer than 1 available vaccination operator")
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

// DeregisterDeviceCommand is the app-facing (self-service) logout decouple: the caller drops the
// FCM push binding + revokes its OWN device (scoped by registered_by = actor), so a logged-out user
// stops receiving pushes on that device. Distinct from the admin RevokeDeviceCommand (operator-scoped).
type DeregisterDeviceCommand struct {
	TenantID string
	ActorID  string
	DeviceID string
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
	// ListGrantedModuleKeys resolves the module keys granted to a user through their
	// workforce member's department (department_module_grants). Returns an empty slice
	// when the user has no active member row or the department has no grants.
	ListGrantedModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error)

	// ListPersonMobileModuleKeys resolves the modules a person is ticked for on the
	// PHONE (per-person access, maintainer decision 2026-08-27). Reports an empty
	// slice for someone with no stored rows, and the caller then falls back to the
	// department grants above -- a person the backfill has not reached must not lose
	// their bar.
	ListPersonMobileModuleKeys(ctx context.Context, tenantID, userID string) ([]string, error)
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
	DeregisterDevice(ctx context.Context, cmd DeregisterDeviceCommand) (domain.DeviceSummary, error)
}
