package permissions

import "context"

const (
	RoleAdmin       = "admin"
	RoleVerifier    = "verifier"
	RoleParkHead    = "park_head"
	RolePCDirector  = "pc_director"
	RoleOperator    = "operator"
	RoleCEOInternal = "ceo_internal"

	GoatRead                  = "goat.read"
	GoatWriteIdentity         = "goat.write_identity"
	GoatWriteHealth           = "goat.write_health"
	LocationsRead             = "locations.read"
	LocationsWrite            = "locations.write"
	LocationsReview           = "locations.review"
	LocationsRetire           = "locations.retire"
	OperatorsRead             = "operators.read"
	OperatorsWrite            = "operators.write"
	OperatorsActivate         = "operators.activate"
	OperatorsDeactivate       = "operators.deactivate"
	OperatorsManageDevice     = "operators.manage_device"
	OperatorsManageCapability = "operators.manage_capability"
	OperatorsManageRoster     = "operators.manage_roster"
	OperatorsViewAudit        = "operators.view_audit"
	OperationsRepair          = "operations.repair"
	AppBootstrap              = "app.bootstrap"
	AdminWebBootstrap         = "admin_web.bootstrap"
	SOPRead                   = "sop.read"
	SOPWrite                  = "sop.write"
	SOPPublish                = "sop.publish"
	TaskRead                  = "task.read"
	TaskAssign                = "task.assign"
	TaskExecute               = "task.execute"
	TaskVerify                = "task.verify"
	ProtocolRead              = "protocol.read"
	ProtocolWrite             = "protocol.write"
	ProtocolPublish           = "protocol.publish"
	ObligationRead            = "obligation.read"
	VaccinationRead           = "vaccination.read"
	VaccinationVerify         = "vaccination.verify"
	VaccinationCampaign       = "vaccination.campaign"
	CalendarRead              = "calendar.read"
	CalendarAction            = "calendar.action"
	ProcurementRead           = "procurement.read"
	ProcurementWrite          = "procurement.write"
	ProcurementReview         = "procurement.review"
	RosterRead                = "roster.read"
	RosterManage              = "roster.manage"
	// CountsWrite gates the app-tier Counts write surface: an operator recording a shifting
	// (movement) event, a birth, or a death from the phone (/app/counts/*).
	//
	// It is a DEDICATED permission rather than a reuse of goat.write_identity/goat.write_health
	// on purpose. Per the maintainer decision recorded in AGENTS.md's business-rule lock, field
	// operators may record birth AND death from the mobile app, but granting them the admin goat
	// write permissions would also hand them every /admin/goats/* route. This permission widens
	// exactly the approved surface and nothing else; the guardrailed death semantics (the
	// dead+died pairing, the separate route, the obligation-cancelling goat.exited event) are
	// unchanged and still enforced inside the identity module.
	//
	// It is granted to the roles that already perform ground capture (the roles holding
	// task.execute), never to RoleVerifier (separation of duty: whoever captures does not verify)
	// and never to the Head/Director tiers, which act on verified work rather than capturing it.
	CountsWrite = "counts.write"
	// CountsRead gates the Counts CENSUS surface: the herd-register summary and the
	// counts breakdown (/counts/breakdown, /herd-register/summary), i.e. whole-herd
	// population figures.
	//
	// It is deliberately SEPARATE from CountsWrite and NARROWER than goat.read. Per the
	// maintainer decision (2026-07-18), field capture and census visibility are different
	// authorities: an Operator or Park Head records births/deaths/shiftings for their own
	// ground truth but does not get a tenant-wide population view, while Admin/CEO do.
	// Splitting it off goat.read is what makes that enforceable — goat.read is held by
	// nearly every role, so reusing it would have made the census effectively public.
	//
	// Nav consequence: the Counts module's census page declares this permission, so a
	// principal without it simply does not receive that nav item (and a principal with
	// NEITHER counts permission does not receive the Counts module at all). Hiding the
	// tab is not the control — the routes above require this same permission, so a hidden
	// page is unreachable, not merely invisible.
	CountsRead = "counts.read"
	// CountsApproveLifecycle gates approving/rejecting a BIRTH or DEATH request
	// (/app/counts/approvals/{id}/{approve,reject} for those two types).
	//
	// Per the maintainer decision (2026-07-19), birth and death are pending until approved: the
	// submission writes no goats row and emits no goat.created/goat.exited, so approving one is
	// the act that creates a kid (and generates its vaccination obligations) or exits an animal
	// (and cancels its open obligations). That is a herd-composition decision, so it sits with the
	// CEO/internal tier, not with the ground roles that capture the event.
	//
	// Deliberately NOT granted to RoleParkHead: a park head runs a park, and the whole point of
	// splitting this from CountsApproveShifting is that the person who authorizes a movement is
	// not thereby authorized to write animals into or out of existence. Never granted to
	// RoleOperator (they capture; capture is not approval) or RoleVerifier (separation of duty:
	// the verifier checks media, and giving them lifecycle approval would collapse the review
	// chain into one role).
	CountsApproveLifecycle = "counts.approve_lifecycle"
	// CountsApproveShifting gates approving/rejecting a SHIFTING request.
	//
	// A shifting event is a movement between sheds inside a park, so its approver is the park head
	// who owns that ground. Approving one is not a paperwork flip: it MOVES THE ANIMALS, updating
	// each named animal's canonical location and re-scoping its shed-scoped vaccination
	// obligations to the destination shed.
	//
	// Granted to RoleParkHead and the admin tier. RoleCEOInternal holds it too because the
	// platform-owner cohort must be able to unblock any queue, but that is a separate grant from
	// CountsApproveLifecycle by design -- the two authorities are independent, and holding one
	// never implies the other. Never granted to RoleOperator or RoleVerifier.
	CountsApproveShifting = "counts.approve_shifting"
	// CountsApproveAccess is the COARSE route gate on the approvals surface. It is not an
	// authority by itself and must never be treated as one.
	//
	// It exists because route-level authorization is static per (method, pattern) and the decision
	// endpoints address a request by ID -- the middleware cannot see whether that ID is a birth or
	// a shifting, because the type lives in the stored row. Route.Permissions is also ANDed, so
	// listing both fine-grained permissions on the route would lock out a park_head who legitimately
	// holds only one of them.
	//
	// So this permission answers only "may this caller reach the approvals surface at all", and the
	// REAL check is the type-specific one applied in the handler after the row is read
	// (DecidableApprovalRequestTypes). It is granted to exactly the roles that hold at least one of
	// CountsApproveLifecycle / CountsApproveShifting, so it never widens access on its own.
	CountsApproveAccess = "counts.approve_access"
	// VerificationReview is the generic Verification vertical's queue-read + verdict-write
	// permission (context/architecture/verification-module-design.md). It is granted ONLY to the
	// Verifier role and, per the org-role-model truth table, as a CEO/CxO override — never to
	// Operator/Manager (capture) or Head/Director (act). Separation of duty: capturer != verifier.
	VerificationReview = "verification.review"
	VerificationAct    = "verification.act"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleAdmin: {
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, OperationsRepair: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		CountsWrite:            {},
		CountsRead:             {},
		CountsApproveLifecycle: {},
		CountsApproveShifting:  {},
		CountsApproveAccess:    {},
		VerificationReview:     {},
		VerificationAct:        {},
	},
	RoleVerifier: {
		GoatRead: {}, GoatWriteIdentity: {},
		LocationsRead: {}, LocationsReview: {},
		OperatorsRead: {}, AppBootstrap: {},
		TaskRead: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {},
		CalendarRead:    {},
		ProcurementRead: {}, ProcurementReview: {},
		RosterRead: {},
		// The Video Verification Team's exclusive permission (verification-module-design.md §2.4 /
		// org-role-model.md truth table): Verify media (approve/reject + reason). No other role holds
		// this except the CEO/CxO override above.
		VerificationReview: {},
	},
	RoleParkHead: {
		GoatRead:      {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, AppBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		CountsWrite: {},
		// A park head authorizes MOVEMENTS on their ground -- and nothing else. They deliberately
		// do NOT hold CountsApproveLifecycle, so they cannot approve a birth or a death.
		CountsApproveShifting: {},
		CountsApproveAccess:   {},
		VerificationAct:       {},
	},
	RolePCDirector: {
		GoatRead: {}, GoatWriteHealth: {},
		LocationsRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, OperatorsViewAudit: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {},
		RosterRead:      {}, RosterManage: {},
		VerificationAct: {},
	},
	RoleOperator: {
		GoatRead: {}, AppBootstrap: {}, TaskRead: {}, TaskExecute: {}, CalendarRead: {}, ProcurementRead: {}, ProcurementWrite: {},
		CountsWrite: {},
	},
	RoleCEOInternal: {
		GoatRead: {}, GoatWriteIdentity: {}, GoatWriteHealth: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, OperationsRepair: {}, AppBootstrap: {}, AdminWebBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskVerify: {},
		ProtocolRead: {}, ProtocolWrite: {}, ProtocolPublish: {}, ObligationRead: {}, VaccinationRead: {}, VaccinationVerify: {}, VaccinationCampaign: {},
		CalendarRead: {}, CalendarAction: {},
		ProcurementRead: {}, ProcurementWrite: {}, ProcurementReview: {},
		RosterRead: {}, RosterManage: {},
		CountsWrite:            {},
		CountsRead:             {},
		CountsApproveLifecycle: {},
		CountsApproveShifting:  {},
		CountsApproveAccess:    {},
		VerificationReview:     {},
		VerificationAct:        {},
	},
}

// DecidableApprovalRequestTypes maps a caller's roles onto the Counts approval request types they
// may decide.
//
// The decision endpoints are a single route pair addressed by request id, so the route-level
// permission check cannot tell a birth from a shifting -- the type lives in the stored row, not in
// the URL. This function is the authority the handler applies AFTER reading the request, and it is
// also what restricts the pending list so an approver is only shown work they can act on.
//
// A caller with neither permission gets an empty set, which fails closed: no listable rows and no
// decidable type.
func DecidableApprovalRequestTypes(roles []string) []string {
	types := make([]string, 0, 3)
	if RolesAuthorize(roles, []string{CountsApproveLifecycle}, false) {
		types = append(types, "birth", "death")
	}
	if RolesAuthorize(roles, []string{CountsApproveShifting}, false) {
		types = append(types, "shifting")
	}
	return types
}

type GrantSource interface {
	ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error)
	ActiveTenantGrants(ctx context.Context, userID, tenantID string) ([]ActiveGrant, error)
}

type ActiveGrant struct {
	Role      string
	ScopeType string
	ScopeID   string
}

type PendingEmailGrantClaim struct {
	TenantID        string
	UserID          string
	Email           string
	ExternalSubject string
	Issuer          string
	Source          string
	TraceID         string
}

type PendingEmailGrantResult struct {
	Matched         bool
	InsertedGrants  []ClaimedEmailGrant
	ExistingGrants  []ClaimedEmailGrant
	PendingGrantIDs []string
}

type ClaimedEmailGrant struct {
	PendingGrantID string `json:"pending_grant_id"`
	GrantID        string `json:"grant_id"`
	Role           string `json:"role"`
	ScopeType      string `json:"scope_type"`
	ScopeID        string `json:"scope_id"`
}

type PendingEmailGrantClaimer interface {
	ClaimPendingEmailGrant(ctx context.Context, claim PendingEmailGrantClaim) (PendingEmailGrantResult, error)
}

func RoleHasPermission(role, permission string) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	_, ok = perms[permission]
	return ok
}

func RolesAuthorize(roles []string, required []string, adminOnly bool) bool {
	if len(required) == 0 {
		return false
	}
	if adminOnly {
		for _, role := range roles {
			if isProductAdminRole(role) {
				return true
			}
		}
		return false
	}
	for _, permission := range required {
		allowed := false
		for _, role := range roles {
			if RoleHasPermission(role, permission) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func isProductAdminRole(role string) bool {
	return role == RoleAdmin || role == RoleCEOInternal
}
