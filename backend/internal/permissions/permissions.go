package permissions

import "context"

const (
	RoleAdmin       = "admin"
	RoleVerifier    = "verifier"
	RoleParkHead    = "park_head"
	RoleOperator    = "operator"
	RoleCEOInternal = "ceo_internal"

	GoatRead              = "goat.read"
	CorrectionCreate      = "correction.create"
	GoatViewDirtyData     = "goat.view_dirty_data"
	GoatReviewIdentity    = "goat.review_identity"
	GoatWriteIdentity     = "goat.write_identity"
	AnalyticsIdentityRead = "analytics.identity.read"
	ImportRunManage       = "import.run.manage"
	ImportRunView         = "import.run.view"
)

var rolePermissions = map[string]map[string]struct{}{
	RoleAdmin: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunManage: {}, ImportRunView: {},
	},
	RoleVerifier: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunView: {},
	},
	RoleParkHead: {
		GoatRead: {}, CorrectionCreate: {}, AnalyticsIdentityRead: {},
	},
	RoleOperator: {
		GoatRead: {}, CorrectionCreate: {},
	},
	RoleCEOInternal: {
		GoatRead: {}, CorrectionCreate: {}, GoatViewDirtyData: {}, GoatReviewIdentity: {},
		GoatWriteIdentity: {}, AnalyticsIdentityRead: {}, ImportRunManage: {}, ImportRunView: {},
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
