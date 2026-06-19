package permissions

import "context"

const (
	RoleAdmin       = "admin"
	RoleVerifier    = "verifier"
	RoleParkHead    = "park_head"
	RoleOperator    = "operator"
	RoleCEOInternal = "ceo_internal"

	GoatRead                  = "goat.read"
	CorrectionCreate          = "correction.create"
	GoatViewDirtyData         = "goat.view_dirty_data"
	GoatReviewIdentity        = "goat.review_identity"
	GoatWriteIdentity         = "goat.write_identity"
	AnalyticsIdentityRead     = "analytics.identity.read"
	ImportRunManage           = "import.run.manage"
	ImportRunView             = "import.run.view"
	LocationsRead             = "locations.read"
	LocationsWrite            = "locations.write"
	LocationsReview           = "locations.review"
	LocationsRetire           = "locations.retire"
	AnalyticsCountsRead       = "analytics.counts.read"
	AnalyticsCountsSync       = "analytics.counts.sync"
	AnalyticsCountsReview     = "analytics.counts.review"
	AnalyticsMortalityRead    = "analytics.mortality.read"
	AnalyticsMortalitySync    = "analytics.mortality.sync"
	AnalyticsMortalityReview  = "analytics.mortality.review"
	OperatorsRead             = "operators.read"
	OperatorsWrite            = "operators.write"
	OperatorsActivate         = "operators.activate"
	OperatorsDeactivate       = "operators.deactivate"
	OperatorsMapLegacySource  = "operators.map_legacy_source"
	OperatorsManageDevice     = "operators.manage_device"
	OperatorsManageCapability = "operators.manage_capability"
	OperatorsManageRoster     = "operators.manage_roster"
	OperatorsViewAudit        = "operators.view_audit"
	AppBootstrap              = "app.bootstrap"
	SOPRead                   = "sop.read"
	SOPWrite                  = "sop.write"
	SOPPublish                = "sop.publish"
	TaskRead                  = "task.read"
	TaskAssign                = "task.assign"
	TaskExecute               = "task.execute"
	TaskVerify                = "task.verify"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleAdmin: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunManage: {}, ImportRunView: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		AnalyticsCountsRead: {}, AnalyticsCountsSync: {}, AnalyticsCountsReview: {},
		AnalyticsMortalityRead: {}, AnalyticsMortalitySync: {}, AnalyticsMortalityReview: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsMapLegacySource: {}, OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, AppBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
	},
	RoleVerifier: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunView: {},
		LocationsRead: {}, LocationsReview: {},
		AnalyticsCountsRead: {}, AnalyticsCountsReview: {},
		AnalyticsMortalityRead: {}, AnalyticsMortalityReview: {},
		OperatorsRead: {},
		TaskRead:      {}, TaskVerify: {},
	},
	RoleParkHead: {
		GoatRead: {}, CorrectionCreate: {}, AnalyticsIdentityRead: {},
		LocationsRead: {}, AnalyticsCountsRead: {}, AnalyticsMortalityRead: {},
		OperatorsRead: {}, OperatorsManageRoster: {}, OperatorsManageDevice: {}, AppBootstrap: {},
		SOPRead: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
	},
	RoleOperator: {
		GoatRead: {}, CorrectionCreate: {}, AppBootstrap: {}, TaskRead: {}, TaskExecute: {},
	},
	RoleCEOInternal: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunManage: {}, ImportRunView: {},
		LocationsRead: {}, LocationsWrite: {}, LocationsReview: {}, LocationsRetire: {},
		AnalyticsCountsRead: {}, AnalyticsCountsSync: {}, AnalyticsCountsReview: {},
		AnalyticsMortalityRead: {}, AnalyticsMortalitySync: {}, AnalyticsMortalityReview: {},
		OperatorsRead: {}, OperatorsWrite: {}, OperatorsActivate: {}, OperatorsDeactivate: {},
		OperatorsMapLegacySource: {}, OperatorsManageDevice: {}, OperatorsManageCapability: {},
		OperatorsManageRoster: {}, OperatorsViewAudit: {}, AppBootstrap: {},
		SOPRead: {}, SOPWrite: {}, SOPPublish: {}, TaskRead: {}, TaskAssign: {}, TaskExecute: {}, TaskVerify: {},
	},
}

type GrantSource interface {
	ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error)
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
