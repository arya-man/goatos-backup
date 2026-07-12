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

// UpdatePositionCommand edits a seat's attributes in place. Each optional
// column carries a Set<Field> flag: when false the column is left unchanged;
// when true the paired value is written (empty string clears a nullable
// column). RowVersion is the optimistic-lock guard. IdempotencyKey is the
// client request-level replay key (empty = non-idempotent).
type UpdatePositionCommand struct {
	TenantID       string
	ActorID        string
	PositionID     string
	RowVersion     int
	IdempotencyKey string

	SetPositionTier bool
	PositionTier    string
	SetBackupGroup  bool
	BackupGroupCode string
	SetWeekOff      bool
	WeekOffWeekday  string
	SetValidTo      bool
	ValidTo         string
	SetStatus       bool
	Status          string
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
	// UpdatePosition edits an active seat's attributes in place (never its
	// holder). Optimistically locked on cmd.RowVersion; returns ErrConflict
	// when the row_version does not match (or the seat is not active), and
	// ErrNotFound when no such position exists in the tenant. The returned bool
	// is true on an exact idempotent replay (the original updated row is
	// returned without re-running the update).
	UpdatePosition(ctx context.Context, cmd UpdatePositionCommand) (domain.Position, bool, error)
	// GetPositionByID returns a single enriched seat by id within the tenant, or
	// ErrNotFound. Backs the position profile drawer read.
	GetPositionByID(ctx context.Context, tenantID, positionID string) (domain.Position, error)
	ListPositions(ctx context.Context, params ListPositionsParams) ([]domain.Position, error)
	// GetActivePositionByCode returns the active seat holder for
	// (scope, position_code) valid at `at`, or ErrNotFound if the seat is
	// currently empty.
	GetActivePositionByCode(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) (domain.Position, error)
	// GetActiveBackupSlot resolves effective_backup (design doc S4.3): the
	// active is_backup_slot=true seat sharing backupGroupCode in the same
	// scope, or ErrNotFound if none is configured.
	GetActiveBackupSlot(ctx context.Context, tenantID, scopeType, scopeID, backupGroupCode string, at time.Time) (domain.Position, error)
	// ShedOwnerships resolves manager/backup owner cells for a page of sheds in one bounded read. Backup
	// resolution prefers a shed-scoped backup seat, then falls back to the center/park backup seat.
	ShedOwnerships(ctx context.Context, tenantID string, sheds []domain.ShedOwnershipScope, at time.Time) (map[string]domain.ShedOwnership, error)
	// GetActivePositionForMember returns the (single) active seat a member
	// currently holds, used to resolve coverage on leave approval. Returns
	// ErrNotFound when the member holds no fixed position.
	GetActivePositionForMember(ctx context.Context, tenantID, workforceMemberID string) (domain.Position, error)
	// MemberExistsInTenant reports whether workforceMemberID is a
	// workforce_members row scoped to tenantID. workforce_positions.
	// workforce_member_id's FK (000151) is global, not tenant-scoped, so every
	// write that accepts a client-supplied workforce_member_id (CreatePosition,
	// ApplyLeave) MUST call this first and reject cross-tenant references --
	// otherwise a caller in one tenant could link another tenant's workforce
	// member into a position/leave (P1 tenant-isolation gap).
	MemberExistsInTenant(ctx context.Context, tenantID, workforceMemberID string) (bool, error)

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

	// HasApprovedLeaveInWindow reports whether the member has an approved (or
	// escalation_required) absence overlapping the half-open window
	// [startsAt, endsAt) -- not just a single instant -- and that row's id.
	// Used to reject a backup who is free at a coverage window's start but
	// absent on a later day of it (design doc S4.7).
	HasApprovedLeaveInWindow(ctx context.Context, tenantID, workforceMemberID string, startsAt, endsAt time.Time) (bool, string, error)

	// ListBackupConfig returns all backup slot configurations for a scope,
	// joined with their configured holder names.
	ListBackupConfig(ctx context.Context, params ListBackupConfigParams) ([]domain.BackupConfig, error)

	// ListCoverage returns active or historical coverage (leave, week-off, escalation).
	ListCoverage(ctx context.Context, params ListCoverageParams) ([]domain.Coverage, error)

	// GetCenterTimetable returns all active positions for a center with enriched fields.
	GetCenterTimetable(ctx context.Context, tenantID, centerID string, limit int) ([]domain.Position, error)

	// GetOperatorCoverage returns the current coverage (leave/week-off) for an operator, if any.
	GetOperatorCoverage(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (*domain.Coverage, error)

	// GetHolderCoverage returns the currently-in-effect coverage record for a
	// position HOLDER who is themselves absent (their seat is being covered
	// right now, covering_member = the resolved replacement), or (nil, nil) when
	// the holder is present. Distinct from GetOperatorCoverage, which returns the
	// records where the member is the COVERER. Backs the profile drawer's
	// active_coverage field.
	GetHolderCoverage(ctx context.Context, tenantID, workforceMemberID string, at time.Time) (*domain.Coverage, error)

	// GetMemberForActor resolves a user_id to an operator profile, used for IDOR validation and identity resolution (P1).
	GetMemberForActor(ctx context.Context, tenantID, actorID string) (domain.OperatorProfile, error)

	// ResolveExecuteCapability returns the execution capability code a temporary
	// backup grant confers when covering positionCode, read from
	// position_module_duties (migration 000157) at `at`: the capability_code
	// recorded on that position's active, in-effect module duty. Returns "" (no
	// error) when the covered position has no built-module capability mapping --
	// the coverage/leave/escalation flow still resolves, only the
	// execution-permission side effect is a no-op. Replaces the previously
	// hardcoded positionExecuteCapability Go map (design doc S4.6).
	ResolveExecuteCapability(ctx context.Context, tenantID, positionCode string, at time.Time) (string, error)

	// ListDutiesForPositions returns the active, in-effect module duties (from
	// position_module_duties, migration 000157) for each of positionCodes at
	// `at`, keyed by position_code. Batched (single query) to avoid an N+1 read
	// when enriching a page of positions. Position codes with no duties are
	// simply absent from the map.
	ListDutiesForPositions(ctx context.Context, tenantID string, positionCodes []string, at time.Time) (map[string][]domain.PositionDuty, error)

	// ---- Notification recipient resolution (vaccination-notification-rules.md §4c) ----------------
	// Bounded, indexed reads over small workforce config tables (positions/duties/devices are
	// per-tenant handfuls of rows, never herd-scale) -- one set-based query each, no N+1.

	// ResolveModuleDutyRecipients returns active, reachable devices held by members whose position
	// carries (moduleCode, dutyType) at (scopeType, scopeID) `at` -- e.g. the verifier(s) for
	// pc.vaccination at a park. Empty when no position currently holds that duty in scope (e.g. no
	// duty_type='verify' seat seeded -- seed-position-duties derives only execute/manage today).
	ResolveModuleDutyRecipients(ctx context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string, at time.Time) ([]domain.NotificationRecipient, error)

	// ResolveMemberRecipients returns active, reachable devices for one specific workforce member
	// (e.g. the operator who executed a completion, vaccination_completions.recorded_by).
	ResolveMemberRecipients(ctx context.Context, tenantID, workforceMemberID string) ([]domain.NotificationRecipient, error)

	// ResolvePositionRecipients returns active, reachable devices for whoever actively holds
	// positionCode at (scopeType, scopeID) `at` -- e.g. the park head.
	ResolvePositionRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) ([]domain.NotificationRecipient, error)
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
