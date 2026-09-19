package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// The Assumptions button on the Weighing SOP page (maintainer decision 2026-09-19) is gated on
// weighing.assumptions.write, on BOTH halves of the capability-gated lock: the control here and
// the PUT route in permissions/routes.go. Three rows carry the rule --
//
//   - the CEO holds it on the role;
//   - the growth_director, who monitors weighing and does NOT hold it on the role, gets it the
//     moment /people ticks weighing at Configure, because the control reads the person's held
//     set and not only their roles (the person row is the mutation test: gating on roles alone
//     turns it red);
//   - the same growth_director with no such tick is disabled with a backend reason.
func TestWeighingAssumptionsControlFollowsThePersonsTicks(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	// The SOP page itself needs sop.read, which is the Config (Protocols & SOPs) module's View: a person
	// ticked on Weighing alone cannot open Weighing SOP at all, so both fixtures carry it.
	sops := permissions.ModuleAssignment{Module: "config", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView}}
	configureRows := []permissions.ModuleAssignment{
		{Module: "weighing", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView, permissions.LevelConfigure}}, sops,
	}
	viewOnlyRows := []permissions.ModuleAssignment{
		{Module: "weighing", Surface: permissions.SurfaceWeb, Capabilities: []string{permissions.LevelView, permissions.LevelOversee}}, sops,
	}
	configure := permissions.PageAccessForAssignments(configureRows)
	viewOnly := permissions.PageAccessForAssignments(viewOnlyRows)
	for _, tc := range []struct {
		name    string
		role    string
		src     PersonPageAccessSource
		enabled bool
	}{
		{"ceo on the role", permissions.RoleCEOInternal, nil, true},
		{"growth_director, no person rows", permissions.RoleGrowthDirector, nil, false},
		{"growth_director ticked Configure on /people", permissions.RoleGrowthDirector, stubPageAccess{assigned: true, access: configure, perms: permissions.PermissionsForAssignments(configureRows)}, true},
		{"growth_director ticked View+Oversee only", permissions.RoleGrowthDirector, stubPageAccess{assigned: true, access: viewOnly, perms: permissions.PermissionsForAssignments(viewOnlyRows)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewService(fakeFamilies{})
			if tc.src != nil {
				svc = svc.WithPersonPageAccess(tc.src)
			}
			resp := svc.Bootstrap(context.Background(), BootstrapInput{
				TenantID: tenant,
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants:   []permissions.ActiveGrant{{Role: tc.role, ScopeType: "tenant", ScopeID: tenant}},
			})
			control := controlByID(t, pageByRouteID(t, resp.Pages, "weighing-sops").Controls, "edit_assumptions")
			if control.Enabled != tc.enabled {
				t.Fatalf("edit_assumptions.enabled = %v want %v (%#v)", control.Enabled, tc.enabled, control)
			}
			if !tc.enabled && control.DisabledReason == "" {
				t.Fatalf("a disabled control must carry a backend reason")
			}
			if control.Action != "PUT /growth-director/assumptions" {
				t.Fatalf("action = %q", control.Action)
			}
		})
	}
}
