package http

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// HR authors the HRMS SOP and nothing else (maintainer answer 2026-09-30). A caller admitted
// only by HRMSSOPAuthor is narrowed to "hrms." codes; a full sop.* holder is never narrowed.
func TestHRMSAuthorIsNarrowedToTheHRMSSOPs(t *testing.T) {
	hrOnly := httpmiddleware.WithPersonPermissions(context.Background(), []string{permissions.HRMSSOPAuthor, permissions.WorkforceViolationsRead})
	full := httpmiddleware.WithPersonPermissions(context.Background(), []string{permissions.SOPRead, permissions.SOPWrite, permissions.SOPPublish, permissions.HRMSSOPAuthor})
	byRole := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleHR}})
	ceo := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal}})

	for name, tc := range map[string]struct {
		ctx       context.Context
		narrowed  bool
		requested string
		want      string
	}{
		"HR person rows, no prefix":        {hrOnly, true, "", "hrms."},
		"HR person rows, another module":   {hrOnly, true, "weighing.", "hrms."},
		"HR person rows, hrms prefix kept": {hrOnly, true, "hrms.violations", "hrms.violations"},
		"HR on the role path":              {byRole, true, "feed.", "hrms."},
		"full SOP holder":                  {full, false, "weighing.", "weighing."},
		"CEO on the role path":             {ceo, false, "", ""},
	} {
		for _, perm := range []string{permissions.SOPRead, permissions.SOPWrite, permissions.SOPPublish} {
			if got := hrmsOnly(tc.ctx, perm); got != tc.narrowed {
				t.Fatalf("%s: hrmsOnly(%s) = %v, want %v", name, perm, got, tc.narrowed)
			}
		}
		if got := hrmsListPrefix(tc.ctx, tc.requested); got != tc.want {
			t.Fatalf("%s: list prefix = %q, want %q", name, got, tc.want)
		}
	}
}
