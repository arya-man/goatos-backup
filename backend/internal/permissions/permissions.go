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
		VerificationReview: {},
		VerificationAct:    {},
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
		VerificationAct: {},
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
		VerificationReview: {},
		VerificationAct:    {},
	},
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
