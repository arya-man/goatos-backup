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
		GoatRead: {}, GoatViewDirtyData: {}, AnalyticsIdentityRead: {},
	},
}

type GrantSource interface {
	ActiveTenantRoles(ctx context.Context, userID, tenantID string) ([]string, error)
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
			if role == RoleAdmin {
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
