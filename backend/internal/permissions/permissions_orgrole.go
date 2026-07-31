package permissions

import "strings"

// Org role model -- tier x vertical x park.
//
// See context/architecture/org-role-model.md (the derived target model) and
// context/architecture/staff-org-data.md (the source staff/org data) for the
// full write-up. This file composes vertical-scoped org roles alongside the
// remaining flat roles (RoleVerifier, RoleParkHead, RolePCDirector,
// RoleOperator, RoleCEOInternal) declared in permissions.go.
//
// The real org is a 3-axis matrix: TIER (CEO/CxO -> Director -> Head ->
// Manager -> Assistant Manager) x VERTICAL (9 business departments) x PARK.
// This file introduces composite role keys of the shape "<tier>_<vertical>"
// (e.g. "manager_feed", "director_health", "am_health") so a role carries
// BOTH a tier (capability level) and a vertical (business domain scope)
// instead of being one flat string per capability level. Park scope
// continues to use the existing ActiveGrant.ScopeType == "park" mechanism,
// unchanged (see ScopeIDsForPermission below and
// internal/calendar/adapters/http/handler.go's calendarScope, the existing
// consumer of that mechanism).
//
// CEO/CxO stays the flat RoleCEOInternal role and does NOT get a per-vertical
// composite key: per AGENTS.md's founder/builder visibility invariant the 5
// founder accounts must keep role='ceo_internal' + full grants across every
// built visible module, which is a cross-vertical, all-access role by
// definition.
//
// The Verifier (video verification team) also stays the flat, cross-vertical
// RoleVerifier role, gated to task.verify / vaccination.verify. This is the
// deliberate extension point for the Verification module's
// `verification.review` permission, if/when it is added: verification
// crosses every vertical by design (separation of duty -- nobody who
// captures ground proof also verifies it), so RoleVerifier must never be
// narrowed to a single vertical. Wiring verification.review onto RoleVerifier
// (and RoleCEOInternal, for override) is left to that module; this file does
// not declare or reference a verification.review constant so the two changes
// rebase cleanly against each other.

// Tier is a level in the org hierarchy: CEO/CxO -> Director -> Head ->
// Manager -> Assistant Manager (AM). See context/architecture/org-role-model.md.
type Tier string

const (
	TierDirector         Tier = "director"
	TierHead             Tier = "head"
	TierManager          Tier = "manager"
	TierAssistantManager Tier = "am"
)

// AllTiers is every tier that composes into a per-vertical role key via
// RoleKey. CEO/CxO is intentionally excluded -- it stays the flat
// RoleCEOInternal role (see package doc above).
var AllTiers = []Tier{TierDirector, TierHead, TierManager, TierAssistantManager}

// Vertical is one of the 9 business verticals/departments a role can be
// scoped to. See context/architecture/org-role-model.md and
// context/architecture/staff-org-data.md section 3.
type Vertical string

const (
	VerticalProcurement    Vertical = "procurement"
	VerticalPreventiveCare Vertical = "preventive_care"
	VerticalBreeding       Vertical = "breeding"
	VerticalHealth         Vertical = "health"
	VerticalGrowth         Vertical = "growth"
	VerticalInfrastructure Vertical = "infrastructure"
	VerticalFeed           Vertical = "feed"
	VerticalMilk           Vertical = "milk"
	VerticalSales          Vertical = "sales"
)

// AllVerticals is every vertical a tier can be scoped to, in the order
// seeded into org_verticals (migration 000178_org_role_catalog.sql).
var AllVerticals = []Vertical{
	VerticalProcurement, VerticalPreventiveCare, VerticalBreeding, VerticalHealth,
	VerticalGrowth, VerticalInfrastructure, VerticalFeed, VerticalMilk, VerticalSales,
}

// RoleKey composes the concrete grantable role string for a (tier, vertical)
// pair -- e.g. RoleKey(TierManager, VerticalFeed) == "manager_feed". This is
// the string stored in user_scope_grants.role / auth_pending_email_grants.role
// and validated against the org_role_catalog table seeded by the clean-slate baseline.
func RoleKey(tier Tier, vertical Vertical) string {
	return string(tier) + "_" + string(vertical)
}

// ParseRoleKey decomposes a composite role key produced by RoleKey back into
// its tier and vertical. It returns ok=false for anything that is not a
// composite key -- including the flat legacy roles (verifier, park_head,
// pc_director, operator, ceo_internal), which do not carry a
// vertical.
func ParseRoleKey(role string) (tier Tier, vertical Vertical, ok bool) {
	for _, t := range AllTiers {
		prefix := string(t) + "_"
		if !strings.HasPrefix(role, prefix) {
			continue
		}
		candidate := Vertical(strings.TrimPrefix(role, prefix))
		for _, v := range AllVerticals {
			if v == candidate {
				return t, v, true
			}
		}
	}
	return "", "", false
}

// IsOrgRoleKey reports whether role is a valid composite tier x vertical
// role key produced by RoleKey (e.g. "manager_feed"). It does not match the
// flat legacy roles.
func IsOrgRoleKey(role string) bool {
	_, _, ok := ParseRoleKey(role)
	return ok
}

// IsKnownRole reports whether role is any grantable role this backend
// recognizes: a flat legacy role (RoleVerifier, RoleParkHead,
// RolePCDirector, RoleOperator, RoleCEOInternal) or a composite tier x
// vertical org role key (RoleKey). Callers that provision grants (seed CLIs,
// future admin operator-management APIs) should use this instead of a
// hand-rolled role allowlist, so a new vertical or tier does not require
// touching every call site -- mirrors org_role_catalog on the DB side.
func IsKnownRole(role string) bool {
	_, ok := rolePermissions[role]
	return ok
}

// RoleAuthorizedForVertical reports whether role may act within vertical.
//
// Composite org role keys (RoleKey) are vertical-scoped by construction: a
// "manager_feed" grant authorizes vertical == VerticalFeed and nothing else
// -- RoleAuthorizedForVertical("manager_feed", VerticalHealth) is false. This
// is the primitive a future per-vertical module (e.g. a Feed module) uses to
// hard-filter "which vertical's obligations/tasks/data may this actor act
// on", the vertical analog of the park-scope hard filter ScopeIDsForPermission
// provides for ActiveGrant.ScopeType == "park".
//
// Flat legacy/cross-vertical roles (RoleVerifier, RoleCEOInternal, and today's
// RoleParkHead/RolePCDirector/RoleOperator) are cross-vertical by
// definition -- see org-role-model.md's "Current RBAC vs this model" gap
// table, which calls out that these roles have no vertical scope yet -- so
// this function returns true for any vertical when role does not parse as a
// composite key. A future module that wires per-vertical checks for these
// legacy roles must widen this function's mapping (e.g. pc_director ==
// VerticalPreventiveCare only), not bypass it.
func RoleAuthorizedForVertical(role string, vertical Vertical) bool {
	_, roleVertical, ok := ParseRoleKey(role)
	if !ok {
		return true
	}
	return roleVertical == vertical
}

// ScopeIDsForPermission returns the distinct ActiveGrant.ScopeID values of
// scopeType granted to a set of active grants for the given permission --
// e.g. ScopeIDsForPermission(grants, TaskExecute, "park") returns the park
// IDs an actor may execute tasks in. This generalizes the park/shed
// scope-filtering pattern already used by the calendar module
// (internal/calendar/adapters/http/handler.go calendarScope /
// calendarActorGrants) into the permissions package so a future module can
// reuse it instead of re-deriving scope filtering per module.
//
// Returns nil (not an error) when nothing matches; callers must treat an
// empty result as "no access to any scope of this type", never as
// "unscoped/all" -- a role is cross-park/cross-vertical ONLY via a
// scope_type == 'tenant' grant, which callers should check separately (see
// httpmiddleware.routeRoles / calendarScope's own "tenant" case).
func ScopeIDsForPermission(grants []ActiveGrant, permission, scopeType string) []string {
	seen := map[string]struct{}{}
	var ids []string
	for _, grant := range grants {
		if grant.ScopeType != scopeType || grant.ScopeID == "" {
			continue
		}
		if !RoleHasPermission(grant.Role, permission) {
			continue
		}
		if _, ok := seen[grant.ScopeID]; ok {
			continue
		}
		seen[grant.ScopeID] = struct{}{}
		ids = append(ids, grant.ScopeID)
	}
	return ids
}

// tierPermissions is the single source of truth for what each TIER may do,
// independent of vertical -- capability comes from tier, scope (which
// vertical, which park) comes from the role key + grant. Mirrors the
// capability matrix in context/architecture/org-role-model.md ("Truth table
// -- who can DO what (target model)").
//
// This intentionally corrects the business-wrong capture affordance that doc
// calls out: only the explicit Operator role gets TaskExecute. Organization
// managers, heads, and directors supervise/act but never hold the scanner,
// verify is Verifier
// (+ CEO/CxO override) only, act-on-verdict is Head/Director/CEO. The
// existing flat legacy roles (RoleParkHead, RolePCDirector in
// permissions.go's rolePermissions) are UNCHANGED by this file and keep
// their current (broader, vaccination-only-era) permission sets for backward
// compatibility -- see org-role-model.md's "Current RBAC vs this model" gap
// table for why the flat roles and this tier catalog differ on purpose.
//
// Kept in sync with the Postgres-side org_tiers seed in the clean-slate baseline by
// TestOrgRoleCatalogHasEntryForEveryComposedRole /
// TestTierPermissionsCoverOnlyKnownPermissions in
// permissions_orgrole_test.go.
var tierPermissions = map[Tier]map[string]struct{}{
	// Assistant Manager supervises ground execution. The separate Operator role
	// owns RFID/camera capture and submit.
	TierAssistantManager: {
		GoatRead: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		TaskRead:       {},
		ObligationRead: {}, VaccinationRead: {},
		CalendarRead:    {},
		ProcurementRead: {}, ProcurementWrite: {},
		// Ground capture includes recording the three count-moving events from the phone
		// (shifting/birth/death) -- see CountsWrite's doc comment in permissions.go.
		CountsWrite: {},
		// Approvals (maintainer decision 2026-07-21): all four org tiers approve/reject every
		// request type -- birth, death, AND shifting -- on the admin-web Approvals page.
		// AdminWebBootstrap above lets them load admin-web; CountsApproveAccess is the coarse route
		// gate and Lifecycle/Shifting are the per-type decision authorities.
		CountsApproveAccess: {}, CountsApproveLifecycle: {}, CountsApproveShifting: {},
	},
	// Manager tier -- run the vertical's daily ops at a park, supervise AMs,
	// manages the local roster without capturing.
	TierManager: {
		GoatRead: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		TaskRead: {}, TaskAssign: {},
		ObligationRead: {}, VaccinationRead: {},
		CalendarRead: {}, CalendarAction: {},
		OperatorsRead: {}, OperatorsManageRoster: {},
		RosterRead: {}, RosterManage: {},
		ProcurementRead: {}, ProcurementWrite: {},
		CountsWrite: {},
		// Approvals (maintainer decision 2026-07-21): approve/reject birth, death, and shifting.
		CountsApproveAccess: {}, CountsApproveLifecycle: {}, CountsApproveShifting: {},
	},
	// Head (Ops-Head) tier -- park/vertical oversight + standards; act on
	// verified items. No capture, no verify.
	TierHead: {
		GoatRead:      {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {},
		AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		VerificationAct: {},
		// Approvals (maintainer decision 2026-07-21): approve/reject birth, death, and shifting.
		CountsApproveAccess: {}, CountsApproveLifecycle: {}, CountsApproveShifting: {},
	},
	// Director tier -- owns the vertical: plan/logistics/oversee execution,
	// set SOPs/protocols, act, penalise. No capture, no verify.
	TierDirector: {
		GoatRead: {}, GoatWriteHealth: {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {},
		AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {},
		ObligationRead: {}, VaccinationRead: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
		// Approvals (maintainer decision 2026-07-21): approve/reject birth, death, and shifting.
		CountsApproveAccess: {}, CountsApproveLifecycle: {}, CountsApproveShifting: {},
	},
}

// init composes every (tier, vertical) pair in AllTiers x AllVerticals into a
// role_key (RoleKey) and merges its tier's permission set into the same
// rolePermissions map RoleHasPermission already reads -- so RoleHasPermission
// and RolesAuthorize (both declared in permissions.go, unmodified) work for
// the new composite role keys with zero changes to their implementation.
// Panics on any collision with a pre-existing role key: this must never
// silently overwrite one of the remaining flat legacy roles' permission sets.
func init() {
	for _, tier := range AllTiers {
		perms, ok := tierPermissions[tier]
		if !ok {
			continue
		}
		for _, vertical := range AllVerticals {
			key := RoleKey(tier, vertical)
			if _, exists := rolePermissions[key]; exists {
				panic("permissions: composite org role key collides with an existing role: " + key)
			}
			set := make(map[string]struct{}, len(perms))
			for permission := range perms {
				set[permission] = struct{}{}
			}
			if vertical == VerticalHealth {
				set[HealthRead] = struct{}{}
				// Raising a sick-goat report is field work every health tier can do; the
				// clinical course authoring above manager stays on health.diagnose
				// (maintainer decision 2026-07-30).
				set[HealthReport] = struct{}{}
				if tier == TierManager || tier == TierHead || tier == TierDirector {
					set[HealthDiagnose] = struct{}{}
				}
			}
			rolePermissions[key] = set
		}
	}

	// Growth Director is the first production use of a vertical-specific director
	// role on mobile. Keep it intentionally narrow: it owns weighing execution and
	// monitoring, not Preventive Care vaccination, even though the generic Director
	// tier still has vaccination-era read/planning permissions for the broader org
	// catalog. Future vertical directors should get the same explicit permission
	// composition instead of relying on role names in Android.
	rolePermissions[RoleGrowthDirector] = map[string]struct{}{
		GoatRead: {}, GoatWriteHealth: {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {},
		AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {},
		WeighingMonitor: {}, WeighingExecute: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
	}
}
