package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestConfigurationPageControlsAreCapabilityGated pins both halves of the capability lock on
// /configuration/items (maintainer instruction 2026-09-18): the page is reached on
// configuration.read, and the four register writes are enabled ONLY for a principal holding
// configuration.write -- the CEO/CXO -- and disabled with a backend reason for a reader. The
// route table's half is pinned in permissions/configuration_permissions_test.go.
//
// Mutation-tested when written: compiling the controls from ConfigurationRead turns the
// reader row red.
func TestConfigurationPageControlsAreCapabilityGated(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct {
		name    string
		role    string
		enabled bool
	}{
		{"ceo writes", permissions.RoleCEOInternal, true},
		{"director reads only", permissions.RolePCDirector, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
				TenantID: tenant,
				ActorID:  "00000000-0000-4000-8000-000000000099",
				Grants:   []permissions.ActiveGrant{{Role: tc.role, ScopeType: "tenant", ScopeID: tenant}},
			})
			page := pageByRouteID(t, resp.Pages, "configuration-items")
			if page.SurfaceKind != "module-surface" {
				t.Fatalf("surface kind = %q, want module-surface", page.SurfaceKind)
			}
			if len(page.Tables) != 1 || page.Tables[0].DataSource != "/admin/configuration/{register}" {
				t.Fatalf("tables = %+v", page.Tables)
			}
			seen := map[string]bool{}
			for _, c := range page.Controls {
				switch c.ID {
				case "create_row", "edit_row", "set_row_status", "delete_row":
					seen[c.ID] = true
					if c.Enabled != tc.enabled {
						t.Errorf("%s: control %s enabled = %v, want %v", tc.role, c.ID, c.Enabled, tc.enabled)
					}
					if !tc.enabled && c.DisabledReason == "" {
						t.Errorf("%s: control %s must carry a disabled reason", tc.role, c.ID)
					}
				}
			}
			for _, id := range []string{"create_row", "edit_row", "set_row_status", "delete_row"} {
				if !seen[id] {
					t.Errorf("control %s missing from the /configuration/items contract", id)
				}
			}
		})
	}
}

// TestConfigurationNavIsGatedOnRead pins that the sidebar leaf is disabled -- with a reason --
// for a principal without configuration.read, the way every role-path leaf is gated, and that
// the gate keys on the READ permission alone (the writes are the controls' business).
func TestConfigurationNavIsGatedOnRead(t *testing.T) {
	if got := permissionsForNav("configuration-items"); len(got) != 1 || got[0] != permissions.ConfigurationRead {
		t.Fatalf("permissionsForNav(configuration-items) = %v, want exactly ConfigurationRead", got)
	}
	const tenant = "00000000-0000-4000-8000-000000000001"
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: tenant,
		ActorID:  "00000000-0000-4000-8000-000000000099",
		Grants:   []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: tenant}},
	})
	found := false
	for _, g := range resp.Navigation.Groups {
		if g.ID != "configuration" {
			continue
		}
		for _, leaf := range g.Leaves {
			if leaf.ID == "configuration-items" {
				found = true
				if leaf.Enabled || leaf.DisabledReason == "" {
					t.Fatalf("operator leaf = enabled %v reason %q; want disabled with a reason", leaf.Enabled, leaf.DisabledReason)
				}
			}
		}
	}
	if !found {
		t.Fatalf("configuration-items leaf missing from the Configuration group")
	}
}
