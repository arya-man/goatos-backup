package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestLeadershipTasksModuleIsOfferedToDirectorsAndCEO pins the 2026-09-04 maintainer
// decision: the Tasks module (a director's ask of the CXO desk) is on the phone of every
// director job that carries a phone, and of ceo_internal -- and of nobody below leadership.
// A park head, an operator, a verifier and the per-person roles resolve NO such module.
//
// Mutation-tested when written: deleting the LeadershipTasksRead branch in
// leadershipModuleKeys, and granting the permission to RoleParkHead, each turn a subtest red.
func TestLeadershipTasksModuleIsOfferedToDirectorsAndCEO(t *testing.T) {
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
		"pc_director":          permissions.RolePCDirector,
		"growth_director":      permissions.RoleGrowthDirector,
		"feed_director":        permissions.RoleFeedDirector,
		"health_director":      permissions.RoleHealthDirector,
		"breeding_director":    permissions.RoleBreedingDirector,
		"procurement_director": permissions.RoleProcurementDirector,
		"ceo_internal":         permissions.RoleCEOInternal,
	} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		t.Run(name+" is offered Tasks", func(t *testing.T) {
			if keys := leadershipModuleKeys(grants); !hasKey(keys, "leadership_tasks") {
				t.Fatalf("leadership module keys = %v, want leadership_tasks offered", keys)
			}
			var found *domain.BootstrapModule
			for _, m := range modulesFor(grants, nil, en) {
				if m.Key == "leadership_tasks" {
					mod := m
					found = &mod
				}
			}
			if found == nil {
				t.Fatal("Tasks module did not render for a principal holding leadership_tasks.read")
			}
			// NavItems is EMPTY on purpose (maintainer decision 2026-09-05): the module is one
			// list, so it serves NO bar destinations and the phone draws no bar. The drawer row
			// and the landing href are what keep it reachable, so both are still asserted --
			// this must not decay into "the module vanished".
			if found.Label != "Tasks" || found.Href != "/leadership-tasks" || len(found.NavItems) != 0 {
				t.Fatalf("Tasks module rendered wrong: %+v", *found)
			}
		})
	}
	for name, role := range map[string]string{
		"park_head":       permissions.RoleParkHead,
		"operator":        permissions.RoleOperator,
		"verifier":        permissions.RoleVerifier,
		"counts_approver": permissions.RoleCountsApprover,
		"toxin_tester":    permissions.RoleToxinTester,
	} {
		grants := []domain.GrantSummary{grantWithRole(role)}
		t.Run(name+" is NOT offered Tasks", func(t *testing.T) {
			if keys := leadershipModuleKeys(grants); hasKey(keys, "leadership_tasks") {
				t.Fatalf("leadership module keys = %v; %s must not see the Tasks module", keys, name)
			}
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["leadership_tasks"]; ok {
				t.Fatalf("Tasks module rendered for %s", name)
			}
		})
	}
}
