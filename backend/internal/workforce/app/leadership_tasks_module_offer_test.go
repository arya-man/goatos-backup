package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestLeadershipTasksModuleIsOfferedToDirectorsCEOAndParkHeads pins the Tasks module's
// two-way shape: director jobs raise asks and carry the "For me" pen-visit tab, CEO/CXO can
// raise downward and act, and park heads can act on tasks assigned to them. Operators,
// verifiers and the per-person roles resolve NO such module by role.
//
// Mutation-tested when written: deleting the LeadershipTasksRead branch in
// leadershipModuleKeys, and granting the permission to RoleOperator, each turn a subtest red.
func TestLeadershipTasksModuleIsOfferedToDirectorsCEOAndParkHeads(t *testing.T) {
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
		"park_head":            permissions.RoleParkHead,
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
			// The bar is served ONLY when there is something to switch to (maintainer decisions
			// 2026-09-05 and 2026-09-07) -- and since 2026-09-12 NOBODY has a second Tasks
			// destination: the pen-visit "For me" tab is retired (the visit is the last step of
			// the care work and is reached from that work's own card), so every principal's Tasks
			// module is one list and serves NO bar. The drawer row and the landing href keep it
			// reachable -- this must not decay into "the module vanished".
			if found.Label != "Tasks" || found.Href != "/leadership-tasks" {
				t.Fatalf("Tasks module rendered wrong: %+v", *found)
			}
			if len(found.NavItems) != 0 {
				t.Fatalf("%s Tasks module must serve no bar destinations (For me retired 2026-09-12), got %+v", name, found.NavItems)
			}
		})
	}
	for name, role := range map[string]string{
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
