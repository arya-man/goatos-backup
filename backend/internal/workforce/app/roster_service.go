package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// RosterService implements the HR roster / RBAC position-coverage model
// approved in docs/hr/roster-rbac-design.md (2026-07-10). It is deliberately
// thin on schema: the ONLY new table is workforce_positions (fixed
// operational position, design doc S4.2). Everything else reuses an
// already-committed table:
//   - Leave/absence: workforce_absences (000050) -- its existing
//     replacement_member_id column is the coverage pointer (S4.5); this
//     service fills it automatically via effectiveBackup, it is never
//     operator-chosen.
//   - Temporary execution permission: workforce_member_capabilities (000050)
//     via the injected CapabilityGranter (S4.6) -- a normal time-bounded
//     capability row for the backup holder, no new grants table.
//   - Department ownership / CEO superuser tier / escalation target: reused
//     as-is (department_module_grants, user_scope_grants.ceo_internal,
//     workforce_roster_assignments.escalation_owner_user_id / the scope's
//     park_head position) -- not modeled here.
//
// Cross-cover rejection ("a Feed Manager never vaccinates") is structural,
// not a runtime check: effectiveBackup only ever resolves an
// is_backup_slot=true seat, so a different functional position can never be
// selected as cover, by construction.
type RosterService struct {
	repo         ports.RosterRepository
	capabilities ports.CapabilityGranter
	now          func() time.Time
}

func NewRosterService(repo ports.RosterRepository, capabilities ports.CapabilityGranter) *RosterService {
	return &RosterService{repo: repo, capabilities: capabilities, now: time.Now}
}

// positionExecuteCapability maps a covered position_code to the capability
// code its temporary backup grant carries (S4.6). Only pc.vaccination is a
// BUILT module today (scope-lock); positions without a built-module mapping
// simply get no temporary capability grant -- coverage/leave/escalation still
// resolve correctly, only the execution-permission side-effect is a no-op.
var positionExecuteCapability = map[string]string{
	"preventive_care_manager": "vaccination.execute",
}

// vaccinationPositionCode is the only position the ownership-resolution read
// (ResolveVaccinationOwner) needs today -- scope-lock: pc.vaccination is the
// only built + surfaced module.
const vaccinationPositionCode = "preventive_care_manager"

// ---- Positions ------------------------------------------------------------

func (s *RosterService) CreatePosition(ctx context.Context, cmd ports.CreatePositionCommand, traceID string) (*domain.PositionResponse, error) {
	if err := validateTenantAndActor(cmd.TenantID, cmd.ActorID); err != nil {
		return nil, err
	}
	body := &cmd.Body
	body.WorkforceMemberID = strings.TrimSpace(body.WorkforceMemberID)
	body.ScopeType = strings.TrimSpace(body.ScopeType)
	body.ScopeID = strings.TrimSpace(body.ScopeID)
	body.PositionCode = strings.TrimSpace(body.PositionCode)
	body.PositionTier = strings.TrimSpace(body.PositionTier)
	if !uuidutil.IsUUIDString(body.WorkforceMemberID) {
		return nil, BadRequest("invalid_workforce_member_id", "workforce_member_id must be a UUID")
	}
	if err := s.requireMemberInTenant(ctx, cmd.TenantID, body.WorkforceMemberID); err != nil {
		return nil, err
	}
	if !validRosterScope(cmd.TenantID, body.ScopeType, body.ScopeID) {
		return nil, BadRequest("invalid_scope", "scope_type must be tenant or center, and scope_id a UUID")
	}
	if body.PositionCode == "" {
		return nil, BadRequest("invalid_position_code", "position_code is required")
	}
	if !validPositionTier(body.PositionTier) {
		return nil, BadRequest("invalid_position_tier", "position_tier must be assistant, manager, head, director, or cxo")
	}
	if body.BackupGroupCode != nil {
		v := strings.TrimSpace(*body.BackupGroupCode)
		body.BackupGroupCode = &v
		if v == "" {
			body.BackupGroupCode = nil
		}
	}
	if body.WeekOffWeekday != nil {
		v := strings.ToLower(strings.TrimSpace(*body.WeekOffWeekday))
		body.WeekOffWeekday = &v
		if v == "" {
			body.WeekOffWeekday = nil
		} else if !validWeekdayName(v) {
			return nil, BadRequest("invalid_week_off_weekday", "week_off_weekday must be a full lowercase weekday name")
		}
	}
	if body.ValidTo != nil {
		v := strings.TrimSpace(*body.ValidTo)
		body.ValidTo = &v
		if v != "" {
			if _, err := time.Parse(time.RFC3339, v); err != nil {
				return nil, BadRequest("invalid_valid_to", "valid_to must be RFC3339")
			}
		}
	}
	item, err := s.repo.CreatePosition(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.PositionResponse{Position: item, TraceID: traceID}, nil
}

func (s *RosterService) ListPositions(ctx context.Context, params ports.ListPositionsParams, traceID string) (*domain.PositionListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Limit = boundedLimit(params.Limit, 200)
	items, err := s.repo.ListPositions(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	// Enrich positions with display fields from related tables
	enriched, err := s.enrichPositions(ctx, params.TenantID, items)
	if err != nil {
		return nil, err
	}
	return &domain.PositionListResponse{Items: enriched, TraceID: traceID}, nil
}

// ---- Leave / absence (workforce_absences reuse) --------------------------

func (s *RosterService) ApplyLeave(ctx context.Context, tenantID, actorID string, body domain.ApplyStaffLeaveRequest, traceID string) (*domain.StaffLeaveResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	body.WorkforceMemberID = strings.TrimSpace(body.WorkforceMemberID)
	body.ScopeType = strings.TrimSpace(body.ScopeType)
	body.ScopeID = strings.TrimSpace(body.ScopeID)
	body.ReasonCode = strings.TrimSpace(body.ReasonCode)
	if !uuidutil.IsUUIDString(body.WorkforceMemberID) {
		return nil, BadRequest("invalid_workforce_member_id", "workforce_member_id must be a UUID")
	}
	if err := s.requireMemberInTenant(ctx, tenantID, body.WorkforceMemberID); err != nil {
		return nil, err
	}
	if !validRosterScope(tenantID, body.ScopeType, body.ScopeID) {
		return nil, BadRequest("invalid_scope", "scope_type must be tenant or center, and scope_id a UUID")
	}
	if body.ReasonCode == "" {
		return nil, BadRequest("invalid_reason_code", "reason_code is required")
	}
	startsAt, err := parseBusinessDate(body.StartsOn)
	if err != nil {
		return nil, BadRequest("invalid_starts_on", "starts_on must be YYYY-MM-DD")
	}
	endsOnDay, err := parseBusinessDate(body.EndsOn)
	if err != nil {
		return nil, BadRequest("invalid_ends_on", "ends_on must be YYYY-MM-DD")
	}
	endsAt := endsOnDay.AddDate(0, 0, 1) // exclusive end: covers the whole ends_on business day
	if !endsAt.After(startsAt) {
		return nil, BadRequest("invalid_leave_window", "ends_on must be on or after starts_on")
	}
	leave, err := s.repo.ApplyLeave(ctx, ports.ApplyLeaveCommand{
		TenantID: tenantID,
		ActorID:  actorID,
		Body:     body,
		StartsAt: startsAt,
		EndsAt:   endsAt,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.StaffLeaveResponse{Leave: leave, TraceID: traceID}, nil
}

// ApproveLeave transitions the absence to approved and immediately runs the
// coverage invariant (design doc S4.5): resolve effective_backup for the
// leave-taker's position (if any) and fill replacement_member_id, or mark
// escalation_required (S4.7) if no backup can be resolved / the backup is
// itself unavailable. Ownership (workforce_positions) is never touched.
func (s *RosterService) ApproveLeave(ctx context.Context, tenantID, actorID, absenceID string, body domain.ApproveStaffLeaveRequest, traceID string) (*domain.StaffLeaveResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	absenceID = strings.TrimSpace(absenceID)
	if !uuidutil.IsUUIDString(absenceID) {
		return nil, BadRequest("invalid_absence_id", "absence_id must be a UUID")
	}
	if body.RowVersion <= 0 {
		return nil, BadRequest("invalid_row_version", "row_version is required")
	}
	leave, replayed, err := s.repo.ApproveLeave(ctx, ports.ApproveLeaveCommand{
		TenantID:       tenantID,
		ActorID:        actorID,
		AbsenceID:      absenceID,
		RowVersion:     body.RowVersion,
		IdempotencyKey: ptrString(body.IdempotencyKey),
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if replayed {
		// Exact idempotent replay: the first call already ran coverage
		// resolution + any capability grant. Return the original resolved leave
		// without re-running side effects (downstream duplicate prevention).
		return &domain.StaffLeaveResponse{Leave: leave, TraceID: traceID}, nil
	}
	resolved, err := s.resolveLeaveCoverage(ctx, tenantID, actorID, leave, nil, nil, "")
	if err != nil {
		return nil, err
	}
	return &domain.StaffLeaveResponse{Leave: resolved, TraceID: traceID}, nil
}

// ResolveLeaveCoverage re-runs (auto) or overrides (CEO tier, explicit
// ReplacementMemberID) the coverage resolution for an already-approved or
// escalated absence. Useful when a backup was configured after initial
// approval, or to force-escalate. An explicit ReplacementMemberID MUST equal
// the position's current effective_backup holder (design doc S5.4: never a
// free pick) -- anything else is rejected.
func (s *RosterService) ResolveLeaveCoverage(ctx context.Context, tenantID, actorID, absenceID string, body domain.ResolveLeaveCoverageRequest, traceID string) (*domain.StaffLeaveResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	absenceID = strings.TrimSpace(absenceID)
	if !uuidutil.IsUUIDString(absenceID) {
		return nil, BadRequest("invalid_absence_id", "absence_id must be a UUID")
	}
	leave, err := s.repo.GetLeave(ctx, tenantID, absenceID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if leave.Status != domain.LeaveStatusApproved && leave.Status != domain.LeaveStatusEscalationRequired {
		return nil, Conflict("leave_not_resolvable", "coverage can only be (re)resolved for an approved or escalated absence")
	}
	var explicit *string
	if body.ReplacementMemberID != nil {
		v := strings.TrimSpace(*body.ReplacementMemberID)
		if v != "" && !uuidutil.IsUUIDString(v) {
			return nil, BadRequest("invalid_replacement_member_id", "replacement_member_id must be a UUID")
		}
		explicit = &v
	}
	resolved, err := s.resolveLeaveCoverage(ctx, tenantID, actorID, leave, explicit, body.OverrideReason, ptrString(body.IdempotencyKey))
	if err != nil {
		return nil, err
	}
	return &domain.StaffLeaveResponse{Leave: resolved, TraceID: traceID}, nil
}

func (s *RosterService) GetLeave(ctx context.Context, tenantID, absenceID, traceID string) (*domain.StaffLeaveResponse, error) {
	if err := validateTenant(tenantID); err != nil {
		return nil, err
	}
	absenceID = strings.TrimSpace(absenceID)
	if !uuidutil.IsUUIDString(absenceID) {
		return nil, BadRequest("invalid_absence_id", "absence_id must be a UUID")
	}
	leave, err := s.repo.GetLeave(ctx, tenantID, absenceID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.StaffLeaveResponse{Leave: leave, TraceID: traceID}, nil
}

func (s *RosterService) ListLeave(ctx context.Context, params ports.ListLeaveParams, traceID string) (*domain.StaffLeaveListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Limit = boundedLimit(params.Limit, 200)
	items, err := s.repo.ListLeave(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.StaffLeaveListResponse{Items: items, TraceID: traceID}, nil
}

// resolveLeaveCoverage is the shared engine behind ApproveLeave's automatic
// resolution and ResolveLeaveCoverage's re-resolution/override. explicitReplacement
// nil means "auto-resolve via effectiveBackup"; non-nil (even "") means a
// human is setting/clearing the replacement directly, which is validated
// against the SAME effectiveBackup result -- never a free pick. idempotencyKey
// is non-empty ONLY on the explicit resolve-coverage endpoint path (the
// approve-driven auto path is guarded by the approve key instead): it is
// reserved in the repo write's tx, and when the write replays, the temporary
// capability grant is skipped so a replay creates no duplicate capability row.
func (s *RosterService) resolveLeaveCoverage(ctx context.Context, tenantID, actorID string, leave domain.StaffLeave, explicitReplacement, overrideReason *string, idempotencyKey string) (domain.StaffLeave, error) {
	startsAt, err := time.Parse(time.RFC3339, leave.StartsAt)
	if err != nil {
		return domain.StaffLeave{}, Internal("leave has an invalid starts_at")
	}
	endsAt, err := time.Parse(time.RFC3339, leave.EndsAt)
	if err != nil {
		return domain.StaffLeave{}, Internal("leave has an invalid ends_at")
	}

	holder, err := s.repo.GetActivePositionForMember(ctx, tenantID, leave.WorkforceMemberID)
	if errors.Is(err, ports.ErrNotFound) || (err == nil && (holder.ScopeType != leave.ScopeType || holder.ScopeID != leave.ScopeID)) {
		// No fixed position at this leave's scope -- nothing to cover.
		resolvedLeave, _, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
			TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID, Status: domain.LeaveStatusApproved,
			IdempotencyKey: idempotencyKey,
		})
		return resolvedLeave, mapRepoErr(err)
	}
	if err != nil {
		return domain.StaffLeave{}, mapRepoErr(err)
	}

	if explicitReplacement != nil {
		if *explicitReplacement == "" {
			// Explicit clear/escalate.
			resolvedLeave, _, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
				TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID,
				Status: domain.LeaveStatusEscalationRequired, OverrideReason: overrideReason,
				IdempotencyKey: idempotencyKey,
			})
			return resolvedLeave, mapRepoErr(err)
		}
		backup, err := s.effectiveBackup(ctx, tenantID, holder, startsAt)
		if err != nil {
			return domain.StaffLeave{}, err
		}
		if backup == nil || backup.WorkforceMemberID != *explicitReplacement {
			return domain.StaffLeave{}, Conflict("cross_cover_rejected", "replacement_member_id must be the configured backup for this position's group")
		}
		resolvedLeave, replayed, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
			TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID,
			Status: domain.LeaveStatusApproved, ReplacementMemberID: explicitReplacement, OverrideReason: overrideReason,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return domain.StaffLeave{}, mapRepoErr(err)
		}
		if replayed {
			// Exact replay: the temporary capability grant already ran on the
			// first call -- do not re-grant (downstream duplicate prevention).
			return resolvedLeave, nil
		}
		if err := s.grantTemporaryExecuteCapability(ctx, tenantID, actorID, holder.PositionCode, *explicitReplacement, holder.ScopeType, holder.ScopeID, startsAt, endsAt); err != nil {
			return domain.StaffLeave{}, err
		}
		return resolvedLeave, nil
	}

	backup, err := s.effectiveBackup(ctx, tenantID, holder, startsAt)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	if backup == nil {
		resolvedLeave, _, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
			TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID, Status: domain.LeaveStatusEscalationRequired,
			IdempotencyKey: idempotencyKey,
		})
		return resolvedLeave, mapRepoErr(err)
	}
	// P1a: check backup availability across the ENTIRE coverage window (design doc S4.7)
	unavailable, err := s.isPositionHolderUnavailableInWindow(ctx, tenantID, *backup, startsAt, endsAt)
	if err != nil {
		return domain.StaffLeave{}, err
	}
	if unavailable {
		resolvedLeave, _, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
			TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID, Status: domain.LeaveStatusEscalationRequired,
			IdempotencyKey: idempotencyKey,
		})
		return resolvedLeave, mapRepoErr(err)
	}
	backupMemberID := backup.WorkforceMemberID
	resolvedLeave, replayed, err := s.repo.ResolveLeaveCoverage(ctx, ports.ResolveLeaveCoverageCommand{
		TenantID: tenantID, ActorID: actorID, AbsenceID: leave.AbsenceID,
		Status: domain.LeaveStatusApproved, ReplacementMemberID: &backupMemberID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return domain.StaffLeave{}, mapRepoErr(err)
	}
	if replayed {
		// Exact replay: capability already granted on the first call; skip.
		return resolvedLeave, nil
	}
	// P1b: pass validFrom = startsAt so capability is active only during the coverage window (design doc S4.6)
	if err := s.grantTemporaryExecuteCapability(ctx, tenantID, actorID, holder.PositionCode, backupMemberID, holder.ScopeType, holder.ScopeID, startsAt, endsAt); err != nil {
		return domain.StaffLeave{}, err
	}
	return resolvedLeave, nil
}

// effectiveBackup resolves design doc S4.3: the active is_backup_slot seat
// sharing the covered position's backup_group_code, in the same scope. A
// nil, nil return means no backup is configured for this group at this
// scope (S4.7 escalation applies) -- which is also what a backup-slot
// holder's OWN leave/week-off resolves to: a position can never be its own
// backup (the backup slot necessarily shares its group's backup_group_code
// so the lookup can find it, so without this guard resolving coverage for
// the Backup Manager's own leave would incorrectly match themselves; the
// real org chart has no further fallback within the tier, matching how
// Park Head's own "Backup" cell is "--" in the source timetable).
func (s *RosterService) effectiveBackup(ctx context.Context, tenantID string, holder domain.Position, at time.Time) (*domain.Position, error) {
	if holder.BackupGroupCode == nil || strings.TrimSpace(*holder.BackupGroupCode) == "" {
		return nil, nil
	}
	backup, err := s.repo.GetActiveBackupSlot(ctx, tenantID, holder.ScopeType, holder.ScopeID, *holder.BackupGroupCode, at)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if backup.WorkforceMemberID == holder.WorkforceMemberID {
		return nil, nil
	}
	return &backup, nil
}

// isPositionHolderUnavailable is true when the position's holder has an
// approved leave overlapping `at`, or `at`'s weekday matches their recurring
// week-off -- the two confirmed coverage triggers (design doc S4.5), treated
// identically.
func (s *RosterService) isPositionHolderUnavailable(ctx context.Context, tenantID string, position domain.Position, at time.Time) (bool, error) {
	onLeave, _, err := s.repo.IsMemberOnApprovedLeave(ctx, tenantID, position.WorkforceMemberID, at)
	if err != nil {
		return false, mapRepoErr(err)
	}
	if onLeave {
		return true, nil
	}
	if position.WeekOffWeekday != nil && *position.WeekOffWeekday == weekdayName(at) {
		return true, nil
	}
	return false, nil
}

// isPositionHolderUnavailableInWindow checks whether the position's holder
// is unavailable on ANY day within [startsAt, endsAt] (design doc S4.7).
// Returns true if the holder has any approved leave overlapping the window,
// or any day in the window matches their recurring week-off.
func (s *RosterService) isPositionHolderUnavailableInWindow(ctx context.Context, tenantID string, position domain.Position, startsAt, endsAt time.Time) (bool, error) {
	// Check for approved leave overlapping the window
	onLeave, _, err := s.repo.IsMemberOnApprovedLeave(ctx, tenantID, position.WorkforceMemberID, startsAt)
	if err != nil {
		return false, mapRepoErr(err)
	}
	if onLeave {
		return true, nil
	}

	// Check each day in the window for week-off match
	if position.WeekOffWeekday != nil {
		weekOff := *position.WeekOffWeekday
		for d := startsAt; !d.After(endsAt); d = d.AddDate(0, 0, 1) {
			if weekdayName(d) == weekOff {
				return true, nil
			}
		}
	}

	return false, nil
}

// grantTemporaryExecuteCapability implements design doc S4.6: a normal
// time-bounded workforce_member_capabilities row for the backup holder,
// scoped to the covered position's center, valid for the coverage window.
// P1b: ValidFrom is set to startsAt (coverage window start), not now(), so
// the capability is active ONLY during the window (design doc S4.6).
// Skips silently when the covered position has no built-module capability
// mapping (scope-lock) and dedupes an identical grant already covering the
// same window (maintainer 2026-07-10: write/notify once when leave and
// week-off land on the same day).
func (s *RosterService) grantTemporaryExecuteCapability(ctx context.Context, tenantID, actorID, positionCode, memberID, scopeType, scopeID string, startsAt, endsAt time.Time) error {
	capCode, ok := positionExecuteCapability[positionCode]
	if !ok {
		return nil
	}
	validFrom := startsAt.UTC().Format(time.RFC3339)
	validTo := endsAt.UTC().Format(time.RFC3339)
	existing, err := s.capabilities.ListCapabilities(ctx, tenantID, memberID)
	if err != nil {
		return mapRepoErr(err)
	}
	for _, c := range existing {
		if c.CapabilityCode == capCode && c.ScopeType == scopeType && c.ScopeID == scopeID && c.Status == "active" && c.ValidFrom == validFrom && c.ValidTo != nil && *c.ValidTo == validTo {
			return nil
		}
	}
	_, err = s.capabilities.AssignCapability(ctx, ports.CapabilityCommand{
		TenantID: tenantID, ActorID: actorID, OperatorID: memberID,
		Body: domain.CreateCapabilityRequest{CapabilityCode: capCode, ScopeType: scopeType, ScopeID: scopeID, ValidFrom: &validFrom, ValidTo: &validTo},
	})
	if err != nil {
		return mapRepoErr(err)
	}
	return nil
}

// ---- Vaccination ownership resolution (design doc S4.8) -------------------

func (s *RosterService) ResolveVaccinationOwner(ctx context.Context, tenantID, actorID, scopeType, scopeID, dateStr, traceID string) (*domain.VaccinationOwnerResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if !validRosterScope(tenantID, scopeType, scopeID) {
		return nil, BadRequest("invalid_scope", "scope_type must be tenant or center, and scope_id a UUID")
	}
	date, err := parseBusinessDate(dateStr)
	if err != nil {
		return nil, BadRequest("invalid_date", "date must be YYYY-MM-DD")
	}
	owner, err := s.resolveEffectiveOwner(ctx, tenantID, actorID, scopeType, scopeID, vaccinationPositionCode, date)
	if err != nil {
		return nil, err
	}
	return &domain.VaccinationOwnerResponse{Owner: owner, TraceID: traceID}, nil
}

// resolveEffectiveOwner implements design doc S4.8's effective_owner exactly:
//  1. find the active position holder;
//  2. an already-approved leave with a resolved replacement wins first (its
//     window/capability grant is already durable from approval time -- this
//     ordering is what avoids writing/granting a second time for the same day
//     when leave and week-off happen to overlap, per the maintainer's
//     2026-07-10 dedupe decision);
//  3. else the recurring week-off, resolved live with a just-in-time
//     temporary capability grant for that single day;
//  4. else the position holder themselves.
func (s *RosterService) resolveEffectiveOwner(ctx context.Context, tenantID, actorID, scopeType, scopeID, positionCode string, date time.Time) (domain.VaccinationOwner, error) {
	out := domain.VaccinationOwner{ScopeType: scopeType, ScopeID: scopeID, Date: date.Format("2006-01-02")}
	holder, err := s.repo.GetActivePositionByCode(ctx, tenantID, scopeType, scopeID, positionCode, date)
	if errors.Is(err, ports.ErrNotFound) {
		return s.escalate(ctx, tenantID, scopeType, scopeID, date, out, domain.OwnerReasonNoHolder), nil
	}
	if err != nil {
		return domain.VaccinationOwner{}, mapRepoErr(err)
	}
	positionID := holder.PositionID
	out.PositionID = &positionID

	onLeave, _, err := s.repo.IsMemberOnApprovedLeave(ctx, tenantID, holder.WorkforceMemberID, date)
	if err != nil {
		return domain.VaccinationOwner{}, mapRepoErr(err)
	}
	if onLeave {
		// No status filter here: IsMemberOnApprovedLeave already matched
		// either 'approved' or 'escalation_required' -- both mean the holder
		// is genuinely away, so this listing must see both to find the same
		// row again.
		leaves, err := s.repo.ListLeave(ctx, ports.ListLeaveParams{
			TenantID: tenantID, WorkforceMemberID: holder.WorkforceMemberID, Limit: 50,
		})
		if err != nil {
			return domain.VaccinationOwner{}, mapRepoErr(err)
		}
		for _, l := range leaves {
			if l.Status != domain.LeaveStatusApproved && l.Status != domain.LeaveStatusEscalationRequired {
				continue
			}
			starts, errStart := time.Parse(time.RFC3339, l.StartsAt)
			ends, errEnd := time.Parse(time.RFC3339, l.EndsAt)
			if errStart != nil || errEnd != nil || date.Before(starts) || !date.Before(ends) {
				continue
			}
			if l.ReplacementMemberID != nil {
				memberID := *l.ReplacementMemberID
				out.OwnerWorkforceMemberID = &memberID
				out.OwnerSource = domain.OwnerSourceReplacement
				return out, nil
			}
			return s.escalate(ctx, tenantID, scopeType, scopeID, date, out, domain.OwnerReasonEscalated), nil
		}
	}

	if holder.WeekOffWeekday != nil && *holder.WeekOffWeekday == weekdayName(date) {
		backup, err := s.effectiveBackup(ctx, tenantID, holder, date)
		if err != nil {
			return domain.VaccinationOwner{}, err
		}
		if backup == nil {
			return s.escalate(ctx, tenantID, scopeType, scopeID, date, out, domain.OwnerReasonNoBackupConfigured), nil
		}
		unavailable, err := s.isPositionHolderUnavailable(ctx, tenantID, *backup, date)
		if err != nil {
			return domain.VaccinationOwner{}, err
		}
		if unavailable {
			return s.escalate(ctx, tenantID, scopeType, scopeID, date, out, domain.OwnerReasonEscalated), nil
		}
		dayStart := biztime.BusinessDayStart(date)
		dayEnd := dayStart.AddDate(0, 0, 1)
		if err := s.grantTemporaryExecuteCapability(ctx, tenantID, actorID, holder.PositionCode, backup.WorkforceMemberID, scopeType, scopeID, dayStart, dayEnd); err != nil {
			return domain.VaccinationOwner{}, err
		}
		memberID := backup.WorkforceMemberID
		out.OwnerWorkforceMemberID = &memberID
		out.OwnerSource = domain.OwnerSourceWeekOffBackup
		return out, nil
	}

	memberID := holder.WorkforceMemberID
	out.OwnerWorkforceMemberID = &memberID
	out.OwnerSource = domain.OwnerSourcePositionHolder
	return out, nil
}

// escalate attaches the scope's Park Head (if resolvable) as the escalation
// target so a caller always knows who to route to, per design doc S4.7.
func (s *RosterService) escalate(ctx context.Context, tenantID, scopeType, scopeID string, date time.Time, out domain.VaccinationOwner, reason string) domain.VaccinationOwner {
	out.OwnerSource = domain.OwnerSourceNone
	r := reason
	out.Reason = &r
	if parkHead, err := s.repo.GetActivePositionByCode(ctx, tenantID, scopeType, scopeID, "park_head", date); err == nil {
		id := parkHead.WorkforceMemberID
		out.EscalationParkHeadMemberID = &id
	}
	return out
}

// ---- Backup config + coverage lists ----------------------------------------

func (s *RosterService) ListBackupConfig(ctx context.Context, params ports.ListBackupConfigParams, traceID string) (*domain.BackupConfigListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Limit = boundedLimit(params.Limit, 200)
	items, err := s.repo.ListBackupConfig(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.BackupConfigListResponse{Items: items, TraceID: traceID}, nil
}

func (s *RosterService) ListCoverage(ctx context.Context, params ports.ListCoverageParams, traceID string) (*domain.CoverageListResponse, error) {
	if err := validateTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.Limit = boundedLimit(params.Limit, 200)
	items, err := s.repo.ListCoverage(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CoverageListResponse{Items: items, TraceID: traceID}, nil
}

// ---- Operator timetable + my-coverage  ------------------------------------

func (s *RosterService) GetOperatorTimetable(ctx context.Context, tenantID, actorID, centerID string, limit int, traceID string) (*domain.PositionListResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	if !uuidutil.IsUUIDString(centerID) {
		return nil, BadRequest("invalid_center_id", "center_id must be a UUID")
	}
	// IDOR: operator may only read their own center's timetable (P1)
	member, err := s.repo.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if member.PrimaryLocationID == nil || *member.PrimaryLocationID != centerID {
		return nil, Forbidden("center_scope_mismatch", "not authorized to read this center's timetable")
	}
	limit = boundedLimit(limit, 200)
	items, err := s.repo.GetCenterTimetable(ctx, tenantID, centerID, limit)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	// Enrich positions with display fields
	enriched, err := s.enrichPositions(ctx, tenantID, items)
	if err != nil {
		return nil, err
	}
	return &domain.PositionListResponse{Items: enriched, TraceID: traceID}, nil
}

func (s *RosterService) GetMyCoverage(ctx context.Context, tenantID, actorID, traceID string) (*domain.MyCoverageResponse, error) {
	if err := validateTenantAndActor(tenantID, actorID); err != nil {
		return nil, err
	}
	// P1: Resolve actor (user_id) to workforce_member_id
	member, err := s.repo.GetMemberForActor(ctx, tenantID, actorID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	at := s.now()
	// Query for coverage where THIS member (replacement_member_id) is the one covering someone else
	coverage, err := s.repo.GetOperatorCoverage(ctx, tenantID, member.OperatorID, at)
	if err != nil {
		return nil, mapRepoErr(err)
	}

	kolkata, _ := time.LoadLocation("Asia/Kolkata")
	result := domain.MyCoverage{
		HasCoverage: coverage != nil,
		Timezone:    "Asia/Kolkata",
	}
	if coverage != nil {
		result.PositionID = &coverage.PositionID
		result.CoveringPersonName = coverage.CoveringMemberName
		result.CoveringPositionTitle = coverage.CoveredPositionTitle
		result.WindowStart = &coverage.StartDate
		result.WindowEnd = &coverage.EndDate
		// Generate banner text
		if coverage.CoveringMemberName != nil && coverage.CoveredPositionTitle != nil {
			endDate, err := time.Parse(time.RFC3339, coverage.EndDate)
			if err == nil {
				endDate = endDate.In(kolkata)
				bannerText := "Covering " + strings.ToLower(*coverage.CoveredPositionTitle) + " until " + endDate.Format("January 2, 2006")
				result.BannerText = &bannerText
			}
		}
	}

	return &domain.MyCoverageResponse{Coverage: result, TraceID: traceID}, nil
}

// ---- Enrichment helpers --------------------------------------------------

// enrichPositions populates display fields (position_title, tier, week_off, backup_group)
// from Position domain fields already loaded via repository JOINs.
func (s *RosterService) enrichPositions(ctx context.Context, tenantID string, positions []domain.Position) ([]domain.Position, error) {
	for i := range positions {
		// Position title: prettified position_code
		title := formatPositionCode(positions[i].PositionCode)
		positions[i].PositionTitle = &title
		// Tier: from position_tier
		positions[i].Tier = &positions[i].PositionTier
		// Week off: week_off_weekday
		positions[i].WeekOff = positions[i].WeekOffWeekday
		// Backup group: backup_group_code
		positions[i].BackupGroup = positions[i].BackupGroupCode
	}
	return positions, nil
}

// formatPositionCode converts a position code like "preventive_care_manager"
// to a display title like "Preventive Care Manager"
func formatPositionCode(code string) string {
	words := strings.Split(code, "_")
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// ---- shared roster helpers ------------------------------------------------

// ptrString dereferences an optional string, returning "" when nil. Used to
// pass optional client idempotency keys through to the repository as plain
// strings (empty means "non-idempotent, skip reservation").
func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// requireMemberInTenant rejects a workforce_member_id that does not belong to
// the caller's tenant (P1: workforce_positions.workforce_member_id's FK,
// migration 000151, is global -- not tenant-scoped -- so without this check a
// caller in one tenant could link another tenant's workforce member into a
// position or leave). Called on every roster write that accepts a
// client-supplied workforce_member_id before any other validation of that id.
func (s *RosterService) requireMemberInTenant(ctx context.Context, tenantID, workforceMemberID string) error {
	ok, err := s.repo.MemberExistsInTenant(ctx, tenantID, workforceMemberID)
	if err != nil {
		return mapRepoErr(err)
	}
	if !ok {
		return BadRequest("workforce_member_wrong_tenant", "workforce_member_id does not belong to the caller's tenant")
	}
	return nil
}

func validRosterScope(tenantID, scopeType, scopeID string) bool {
	switch scopeType {
	case "tenant":
		return scopeID == tenantID
	case "center":
		return uuidutil.IsUUIDString(scopeID)
	default:
		return false
	}
}

func validPositionTier(tier string) bool {
	switch tier {
	case domain.PositionTierAssistant, domain.PositionTierManager, domain.PositionTierHead, domain.PositionTierDirector, domain.PositionTierCXO:
		return true
	default:
		return false
	}
}

var weekdayNames = [...]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

func validWeekdayName(value string) bool {
	for _, name := range weekdayNames {
		if value == name {
			return true
		}
	}
	return false
}

// weekdayName returns t's lowercase English weekday name in the Goat OS
// business calendar (Asia/Kolkata) -- matches workforce_positions.
// week_off_weekday's vocabulary.
func weekdayName(t time.Time) string {
	return weekdayNames[int(t.In(biztime.DefaultLocation()).Weekday())]
}

func parseBusinessDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", strings.TrimSpace(value), biztime.DefaultLocation())
}
