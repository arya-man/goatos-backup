package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestPenRoutinesPageControlsAreCapabilityGated pins both halves of the capability lock on
// /routines (maintainer instruction 2026-09-16): the page is reached on pen_routines.read (a
// director sees it), and the three authoring controls are enabled ONLY for a principal
// holding pen_routines.configure -- the CXO -- and disabled with a backend reason for a
// reader. The route table's half is pinned in permissions/pen_routines_permissions_test.go.
//
// Mutation-tested when written: compiling the controls from PenRoutinesRead turns the
// director row red.
func TestPenRoutinesPageControlsAreCapabilityGated(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo writes", permissions.RoleCEOInternal, true},
		{"director reads", permissions.RolePCDirector, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: tenant,
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants:   []permissions.ActiveGrant{{Role: tc.role, ScopeType: "tenant", ScopeID: tenant}},
			})
			page := pageByRouteID(t, resp.Pages, "pen-routines")
			if page.SurfaceKind != "module-surface" {
				t.Fatalf("routines surface kind = %q, want module-surface", page.SurfaceKind)
			}
			if len(page.Tables) != 2 || page.Tables[0].ID != "pen-routines" || page.Tables[1].ID != "pen-routine-tasks" {
				t.Fatalf("routines tables = %+v", page.Tables)
			}
			if page.Tables[0].DataSource != "/admin/pen-routines" || page.Tables[1].DataSource != "/admin/pen-routines/tasks" {
				t.Fatalf("routines table sources = %q / %q", page.Tables[0].DataSource, page.Tables[1].DataSource)
			}
			seen := map[string]bool{}
			for _, c := range page.Controls {
				switch c.ID {
				case "create_routine", "edit_routine", "set_routine_status":
					seen[c.ID] = true
					if c.Enabled != tc.enabled {
						t.Errorf("%s: control %s enabled = %v, want %v", tc.role, c.ID, c.Enabled, tc.enabled)
					}
					if !tc.enabled && c.DisabledReason == "" {
						t.Errorf("%s: control %s must carry a disabled reason", tc.role, c.ID)
					}
				}
			}
			for _, id := range []string{"create_routine", "edit_routine", "set_routine_status"} {
				if !seen[id] {
					t.Errorf("control %s missing from the /routines contract", id)
				}
			}
		})
	}
}
