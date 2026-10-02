package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestProcurementDirectorSalesVendorsFollowsTheHrmsTick pins the one page the Procurement
// Director's fixed page set defers to /people: Sales > Vendors shows exactly when it is
// ticked for the person, and the other four pages stay as they are either way.
func TestProcurementDirectorSalesVendorsFollowsTheHrmsTick(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	input := BootstrapInput{
		TenantID: tenant, ActorID: "hemant",
		Grants: []permissions.ActiveGrant{
			{Role: permissions.RoleFeedDirector, ScopeType: "tenant", ScopeID: tenant},
			{Role: permissions.RoleProcurementDirector, ScopeType: "tenant", ScopeID: tenant},
		},
	}
	// Since 2026-10-02 Sales > Vendors is ticked on the BUYERS half of the register
	// (vendors_sales); migration 000468 wrote that row for everyone who had the leaf.
	rows := func(buyers bool, salesPages ...string) permissions.PageAccess {
		extra := []permissions.ModuleAssignment{}
		if buyers {
			extra = append(extra, permissions.ModuleAssignment{Surface: permissions.SurfaceWeb, Module: "vendors_sales", Capabilities: []string{"view", "do", "oversee"}, Pages: []string{"sales-vendors"}})
		}
		return permissions.PageAccessForAssignments(append(extra, []permissions.ModuleAssignment{
			{Surface: permissions.SurfaceWeb, Module: "sales", Capabilities: []string{"view", "do"}, Pages: salesPages},
			{Surface: permissions.SurfaceWeb, Module: "vendors", Capabilities: []string{"view", "do", "oversee"}, Pages: []string{"procurement-vendors"}},
			{Surface: permissions.SurfaceWeb, Module: "feed_direction", Capabilities: []string{"view", "oversee", "configure"}, Pages: []string{"feed-analytics", "feed-sops"}},
			{Surface: permissions.SurfaceWeb, Module: "feed_purchases", Capabilities: []string{"view", "do"}, Pages: []string{"procurement-feed-purchases"}},
			{Surface: permissions.SurfaceWeb, Module: "procurement", Capabilities: []string{"view", "do", "oversee"}, Pages: []string{"procurement-source-entry"}},
		}...))
	}
	fixed := []string{"/work-board", "/sales/config", "/feed/analytics", "/procurement/vendors", "/procurement/feed-purchases"}

	for _, tc := range []struct {
		name   string
		access permissions.PageAccess
		want   []string
	}{
		// The Work Board leads the primary nav for every director (sprint instruction 2026-09-10).
		{"ticked", rows(true, "sales-board", "sales-config"), []string{"/work-board", "/sales/vendors", "/sales/config", "/feed/analytics", "/procurement/vendors", "/procurement/feed-purchases"}},
		{"not ticked", rows(false, "sales-board", "sales-config"), fixed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService().WithPersonPageAccess(stubPageAccess{access: tc.access, assigned: true})
			got := leafHrefs(svc.Bootstrap(context.Background(), input))
			if len(got) != len(tc.want) {
				t.Fatalf("sidebar is %v; want exactly %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("sidebar is %v; want %v", got, tc.want)
				}
			}
		})
	}
}
