package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// People / HRMS > Notifications (maintainer decision 2026-09-08): which designation hears which
// alert. Since 2026-09-30 it is its own HRMS page rather than a tab of /people: its contract
// carries the table over the matrix read, and a WRITE control gated on OperatorsManageCapability
// -- the same authority as editing access, because deciding what every holder of a job title is
// told is deciding what they may do.
func TestNotificationsPageCarriesTheAudienceTable(t *testing.T) {
	page := pageByRouteID(t, pages(), "people-notifications")
	if page.Href != "/people/notifications" {
		t.Fatalf("notifications page href = %q, want /people/notifications", page.Href)
	}
	var found bool
	for _, tbl := range page.Tables {
		if tbl.ID == "notification-audiences" {
			found = true
			if tbl.DataSource != "/admin/notifications/designations" {
				t.Fatalf("notification-audiences source = %q", tbl.DataSource)
			}
		}
	}
	if !found {
		t.Fatal("notifications page must carry the notification-audiences table contract")
	}
}

func TestEditNotificationsControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		// The PC Director oversees the vaccination work the alerts are about and still may not
		// decide who is told: the mutation test for keying the control on a broader permission.
		{"pc_director reads people but does not manage capability", permissions.RolePCDirector, false},
		{"park_head", permissions.RoleParkHead, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			page := pageByRouteID(t, resp.Pages, "people-notifications")
			control := controlByID(t, page.Controls, "edit_notifications")
			if control.Enabled != tc.enabled {
				t.Fatalf("%s edit_notifications.enabled = %v want %v (%#v)", tc.name, control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("%s: disabled control must carry a backend disabled reason", tc.name)
			}
			if control.Action != "PUT /admin/notifications/designations/{alert_key}" {
				t.Fatalf("edit_notifications.action = %q", control.Action)
			}
			access := controlByID(t, page.Controls, "edit_access")
			if access.Enabled != control.Enabled {
				t.Fatalf("edit_notifications must ride the same authority as edit_access: %v vs %v", control.Enabled, access.Enabled)
			}
		})
	}
}
