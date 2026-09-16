package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestAlertsPageConfigureControlIsCapabilityGated pins the Alerts page's one write control
// (maintainer decision 2026-09-16): the top-right Configure button follows alerts.configure --
// the "configure" level HRMS ticks per person -- never alerts.read, which every director holds.
//
// The pc_director row is the load-bearing one: he holds AlertsRead, opens the page and reads the
// rows, and must NOT see the Configure drawer enabled. Keying the control on AlertsRead instead
// would compile, pass the CEO row, and hand rule authoring to every director -- which is exactly
// the per-job widening the per-person tick exists to prevent.
func TestAlertsPageConfigureControlIsCapabilityGated(t *testing.T) {
	for _, tc := range []struct {
		name      string
		role      string
		configure bool
	}{
		{"ceo_internal", permissions.RoleCEOInternal, true},
		{"pc_director", permissions.RolePCDirector, false},
		{"feed_director", permissions.RoleFeedDirector, false},
		{"growth_director", permissions.RoleGrowthDirector, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: "00000000-0000-4000-8000-000000000001",
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants: []permissions.ActiveGrant{
					{Role: tc.role, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
				},
			})
			page := pageByRouteID(t, resp.Pages, "alerts")
			configure := controlByID(t, page.Controls, "configure_alerts")
			if configure.Enabled != tc.configure {
				t.Fatalf("%s configure_alerts.enabled = %v want %v (%#v)", tc.name, configure.Enabled, tc.configure, configure)
			}
			if !tc.configure && configure.DisabledReason == "" {
				t.Fatalf("%s: disabled configure control must carry a backend disabled reason", tc.name)
			}
			// The control names the read it stands in front of, and the SAME capability gates that
			// route (permissions/routes.go), so a hidden button never fronts an open endpoint.
			if configure.Action != "GET /alerts/config" {
				t.Fatalf("%s configure_alerts.action = %q want the config read", tc.name, configure.Action)
			}
			// Every director opens the page itself: the rows are for anyone with alerts.read.
			alerts := primaryNavByID(t, resp.Navigation.Primary, "alerts")
			if !alerts.Enabled {
				t.Fatalf("%s must reach the Alerts page (alerts.read): %#v", tc.name, alerts)
			}
		})
	}
}

// TestAlertsNavSitsDirectlyBelowWorkBoard pins the placement the maintainer asked for: Alerts is
// the primary item immediately after Work Board.
func TestAlertsNavSitsDirectlyBelowWorkBoard(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: "00000000-0000-4000-8000-000000000001"},
		},
	})
	items := resp.Navigation.Primary
	for i, item := range items {
		if item.ID == "work-board" {
			if i+1 >= len(items) || items[i+1].ID != "alerts" {
				t.Fatalf("Alerts must be the primary item directly after Work Board, got %#v", items)
			}
			if items[i+1].Href != "/alerts" {
				t.Fatalf("alerts href = %q", items[i+1].Href)
			}
			return
		}
	}
	t.Fatalf("work-board primary item not found: %#v", items)
}
