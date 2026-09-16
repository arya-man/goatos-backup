package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestPenRoutinesModuleIsOfferedOnExecuteNeverToTheCEO pins the Routines module's offer
// (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md): it is offered on
// pen_routines.execute -- a park head and every director on the job -- and NOT to the CXO
// desk, which holds read + configure and writes the rule on /routines instead of walking
// pens. Operators, verifiers and the per-person roles resolve no such module by role.
//
// Mutation-tested when written: deleting the PenRoutinesExecute branch in
// permissionOfferedModuleKeys, and granting the permission to RoleCEOInternal, each turn a
// subtest red.
func TestPenRoutinesModuleIsOfferedOnExecuteNeverToTheCEO(t *testing.T) {
	const en = localization.DefaultTag
	hasKey := func(keys []string, want string) bool {
		for _, k := range keys {
			if k == want {
				return true
			}
		}
		return false
	}
	for name, role := range map[string]string{
		"park_head":            permissions.RoleParkHead,
		"pc_director":          permissions.RolePCDirector,
		"growth_director":      permissions.RoleGrowthDirector,
		"feed_director":        permissions.RoleFeedDirector,
		"health_director":      permissions.RoleHealthDirector,
		"breeding_director":    permissions.RoleBreedingDirector,
		"procurement_director": permissions.RoleProcurementDirector,
	} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		t.Run(name+" is offered Routines", func(t *testing.T) {
			if keys := permissionOfferedModuleKeys(grants); !hasKey(keys, "pen_routines") {
				t.Fatalf("permission-offered module keys = %v, want pen_routines", keys)
			}
			var found *domain.BootstrapModule
			for _, m := range modulesFor(grants, nil, en) {
				if m.Key == "pen_routines" {
					mod := m
					found = &mod
				}
			}
			if found == nil {
				t.Fatal("Routines module did not render for a principal holding pen_routines.execute")
			}
			if found.Label != "Routines" || found.Href != "/pen-routines" {
				t.Fatalf("Routines module rendered wrong: %+v", *found)
			}
			if len(found.NavItems) == 0 || found.NavItems[0].Href != "/pen-routines" || found.NavItems[0].Label != "Routines" {
				t.Fatalf("Routines module must serve its list item first, got %+v", found.NavItems)
			}
		})
	}
	for name, role := range map[string]string{
		"ceo_internal":    permissions.RoleCEOInternal,
		"operator":        permissions.RoleOperator,
		"verifier":        permissions.RoleVerifier,
		"counts_approver": permissions.RoleCountsApprover,
		"toxin_tester":    permissions.RoleToxinTester,
	} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		t.Run(name+" is NOT offered Routines", func(t *testing.T) {
			if keys := permissionOfferedModuleKeys(grants); hasKey(keys, "pen_routines") {
				t.Fatalf("permission-offered module keys = %v; %s must not see the Routines module", keys, name)
			}
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["pen_routines"]; ok {
				t.Fatalf("Routines module rendered for %s", name)
			}
		})
	}
}
