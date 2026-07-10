package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

type CreatePositionCommand struct {
	TenantID string
	ActorID  string
	Body     domain.CreatePositionRequest
}

type ListPositionsParams struct {
	TenantID          string
	WorkforceMemberID string
	ScopeType         string
	ScopeID           string
	PositionCode      string
	Status            string
	Limit             int
}

type ApplyLeaveCommand struct {
	TenantID string
	ActorID  string
	Body     domain.ApplyStaffLeaveRequest
	// StartsAt/EndsAt are the resolved Asia/Kolkata business-day window for
	// Body.StartsOn/EndsOn, computed by the service so the repository stays
	// timezone-agnostic.
	StartsAt time.Time
	EndsAt   time.Time
}

type ApproveLeaveCommand struct {
	TenantID   string
	ActorID    string
	AbsenceID  string
	RowVersion int
	// IdempotencyKey is the client-supplied request-level replay key (repo
	// mandatory write-path contract). Empty means non-idempotent (skip
	// reservation). Reserved + completed in the same tx as the status
	// transition; an exact replay returns the original result without re-running
	// the approval or its downstream coverage resolution.
	IdempotencyKey string
}

// ResolveLeaveCoverageCommand persists the roster service's effective_backup
// resolution (or CEO override) onto an already-approved absence: Status is
// 'approved' (replacement resolved or none needed) or 'escalation_required'
// (no backup resolvable / backup unavailable) -- see design doc S4.5/S4.7.
// IdempotencyKey is set only on the EXPLICIT resolve-coverage endpoint path
// (approve-driven auto-resolution is already guarded by the approve key);
// empty means non-idempotent.
type ResolveLeaveCoverageCommand struct {
	TenantID            string
	ActorID             string
	AbsenceID           string
	Status              string
	ReplacementMemberID *string
	OverrideReason      *string
	IdempotencyKey      string
}

type ListLeaveParams struct {
	TenantID          string
	WorkforceMemberID string
	ScopeType         string
	ScopeID           string
	Status            string
	Limit             int
}

type ListBackupConfigParams struct {
	TenantID  string
	ScopeType string
	ScopeID   string
	Limit     int
}

type ListCoverageParams struct {
	TenantID  string
	ScopeType string
	ScopeID   string
	Active    bool // true = only approved, false = all statuses
	Limit     int
}

// RosterRepository is the HR roster read/write surface: the fixed position
// seat catalog (workforce_positions) and leave/absence (workforce_absences
// reuse). Kept separate from Repository (operator/device/grant CRUD) for file
// cohesion; both live in the same workforce module and share its Postgres
// pool/adapters package. Temporary execution permission (design doc S4.6)
// reuses the EXISTING ports.Repository.AssignCapability/ListCapabilities
// (workforce_member_capabilities) instead of a method here -- see
// app.RosterService's CapabilityGranter dependency.
type RosterRepository interface {
	CreatePosition(ctx context.Context, cmd CreatePositionCommand) (domain.Position, error)
	ListPositions(ctx context.Context, params ListPositionsParams) ([]domain.Position, error)
	// GetActivePositionByCode returns the active seat holder for
	// (scope, position_code) valid at `at`, or ErrNotFound if the seat is
	// currently empty.
	GetActivePositionByCode(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) (domain.Position, error)
	// GetActiveBackupSlot resolves effective_backup (design doc S4.3): the
	// active is_backup_slot=true seat sharing backupGroupCode in the same
	// scope, or ErrNotFound if none is configured.
	GetActiveBackupSlot(ctx context.Context, tenantID, scopeType, scopeID, backupGroupCode string, at time.Time) (domain.Position, error)
	// GetActivePositionForMember returns the (single) active seat a member
	// currently holds, used to resolve coverage on leave approval. Returns
	// ErrNotFound when the member holds no fixed position.
	GetActivePositionForMember(ctx context.Context, tenantID, workforceMemberID string) (domain.Position, error)

	ApplyLeave(ctx context.Context, cmd ApplyLeaveCommand) (domain.StaffLeave, error)
	// ApproveLeave transitions reported->approved. The returned bool is true on
	// an exact idempotent replay (the original result is returned without
	// re-running the transition); the service then MUST NOT re-run coverage
	// resolution / capability grants, avoiding downstream duplicates.
	ApproveLeave(ctx context.Context, cmd ApproveLeaveCommand) (domain.StaffLeave, bool, error)
	// ResolveLeaveCoverage persists a coverage resolution. The returned bool is
	// true on an exact idempotent replay (explicit endpoint only); the service
	// then MUST NOT re-run the temporary capability grant.
	ResolveLeaveCoverage(ctx context.Context, cmd ResolveLeaveCoverageCommand) (domain.StaffLeave, bool, error)
	GetLeave(ctx context.Context, tenantID, absenceID string) (domain.StaffLeave, error)
	ListLeave(ctx context.Context, params ListLeaveParams) ([]domain.StaffLeave, error)
	// IsMemberOnApprovedLeave reports whether the member has an in-effect
	// absence (status 'approved' OR 'escalation_required' -- the latter is
	// still a granted absence, just one whose OWN coverage could not be
	// resolved; being away is a separate fact from whether their coverage
	// resolved) whose [starts_at, ends_at) window contains `at`, and that
	// row's id.
	IsMemberOnApprovedLeave(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (bool, string, error)

	// ListBackupConfig returns all backup slot configurations for a scope,
	// joined with their configured holder names.
	ListBackupConfig(ctx context.Context, params ListBackupConfigParams) ([]domain.BackupConfig, error)

	// ListCoverage returns active or historical coverage (leave, week-off, escalation).
	ListCoverage(ctx context.Context, params ListCoverageParams) ([]domain.Coverage, error)

	// GetCenterTimetable returns all active positions for a center with enriched fields.
	GetCenterTimetable(ctx context.Context, tenantID, centerID string, limit int) ([]domain.Position, error)

	// GetOperatorCoverage returns the current coverage (leave/week-off) for an operator, if any.
	GetOperatorCoverage(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (*domain.Coverage, error)
}

// CapabilityGranter is the slice of the existing ports.Repository the roster
// coverage engine needs to grant/inspect the backup holder's temporary
// execution permission (design doc S4.6) via workforce_member_capabilities.
// Satisfied directly by *postgres.Repository alongside ports.Repository --
// no new table, no new repository.
type CapabilityGranter interface {
	AssignCapability(ctx context.Context, cmd CapabilityCommand) (domain.CapabilityAssignment, error)
	ListCapabilities(ctx context.Context, tenantID, operatorID string) ([]domain.CapabilityAssignment, error)
}
