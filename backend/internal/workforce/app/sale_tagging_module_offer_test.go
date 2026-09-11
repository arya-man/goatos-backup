package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestParkHeadIsOfferedTagOnlySalesAndNothingElseOfSales pins the 2026-09-11 maintainer
// decision: a park head tags animals to a sale from the pen and sees NOTHING else of Sales.
//
// THREE assertions, each the thing a later change is most likely to undo by accident:
//
//  1. The park head gets the `sale_allocation` module with exactly ONE item, the tagging queue.
//     No ledger, no pipeline, no vendors tab -- the module has no place to put one.
//  2. The park head gets NEITHER the Sales module nor the Procurement one. Adding SalesRead to the
//     job "so the queue can show the buyer" would light both up.
//  3. A principal who holds SalesRead (CXO, procurement director, procurement manager) is NOT
//     offered the tag-only module beside their Sales module: they reach the same tag flow inside
//     the sale drill, and two doors onto one flow is what the 2026-09-05 split test bans.
//
// Mutation-tested when written: deleting the SalesAllocateAnimals branch in
// permissionOfferedModuleKeys, dropping its !SalesRead half, and granting SalesRead to park_head
// each turn a case red.
func TestParkHeadIsOfferedTagOnlySalesAndNothingElseOfSales(t *testing.T) {
	const en = localization.DefaultTag

	moduleNamed := func(modules []domain.BootstrapModule, key string) *domain.BootstrapModule {
		for i := range modules {
			if modules[i].Key == key {
				return &modules[i]
			}
		}
		return nil
	}

	t.Run("park_head gets the tag-only module with one item", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
		modules := modulesFor(grants, []string{"vaccination", "weighing"}, en)

		tagging := moduleNamed(modules, "sale_allocation")
		if tagging == nil {
			t.Fatalf("tag-only Sales module did not render for park_head; modules = %v", moduleKeySet(modules))
		}
		if tagging.Label != "Sales" {
			t.Errorf("module label = %q, want Sales", tagging.Label)
		}
		// The phone hosts this href VERBATIM; an exact match keeps the item from being dead on
		// the device.
		if len(tagging.NavItems) != 1 || tagging.NavItems[0].Href != "/sale-tagging" || tagging.NavItems[0].Label != "Tag animals" {
			t.Fatalf("tag-only nav items = %+v, want exactly one: /sale-tagging 'Tag animals'", tagging.NavItems)
		}
		for _, banned := range []string{"sales", "vendors"} {
			if moduleNamed(modules, banned) != nil {
				t.Errorf("park_head must NOT see the %q module; got %v", banned, moduleKeySet(modules))
			}
		}
	})

	t.Run("park_head holds the allocation authority and no other sales permission", func(t *testing.T) {
		if !permissions.RoleHasPermission(permissions.RoleParkHead, permissions.SalesAllocateAnimals) {
			t.Fatal("park_head must hold sales.allocate_animals")
		}
		for _, banned := range []string{permissions.SalesRead, permissions.SalesWrite, permissions.VendorRead, permissions.VendorFinanceRead} {
			if permissions.RoleHasPermission(permissions.RoleParkHead, banned) {
				t.Errorf("park_head must NOT hold %s: tagging is the whole of its Sales access", banned)
			}
		}
	})

	for name, grants := range map[string][]domain.GrantSummary{
		"CEO/CXO":              {grantWithRole(permissions.RoleCEOInternal)},
		"procurement_director": {grantWithRole(permissions.RoleProcurementDirector)},
		"procurement_manager":  {grantWithRole(permissions.RoleProcurementManager)},
	} {
		t.Run(name+" is NOT offered the tag-only module beside Sales", func(t *testing.T) {
			modules := modulesFor(grants, nil, en)
			if moduleNamed(modules, "sales") == nil {
				t.Fatalf("%s lost the Sales module; modules = %v", name, moduleKeySet(modules))
			}
			if moduleNamed(modules, "sale_allocation") != nil {
				t.Fatalf("%s was offered the tag-only module beside Sales -- two doors onto one flow", name)
			}
		})
	}

	for name, grants := range map[string][]domain.GrantSummary{
		"operator":      {grantWithRole(permissions.RoleOperator)},
		"feed_director": {grantWithRole(permissions.RoleFeedDirector)},
		"pc_director":   {grantWithRole(permissions.RolePCDirector)},
	} {
		t.Run(name+" is NOT offered the tag-only module", func(t *testing.T) {
			if moduleNamed(modulesFor(grants, nil, en), "sale_allocation") != nil {
				t.Fatalf("%s was offered the tag-only Sales module without the allocation authority", name)
			}
		})
	}
}

// TestTagOnlySalesFollowsThePersonsTicks pins "also configurable from HRMS" (maintainer
// instruction 2026-09-11): the tick on /people is the fact. An OPERATOR ticked for
// `sale_allocation` on the phone gets the module although the operator job carries no sales
// authority at all, and a PARK HEAD whose tick was cleared loses it although the job carries it.
//
// Mutation-tested when written: reading the role grants instead of the person's held set in the
// tick-driven offer turns the operator case red; deleting the offer turns both red.
func TestTagOnlySalesFollowsThePersonsTicks(t *testing.T) {
	modulesFromTicks := func(grants []domain.GrantSummary, dept []string, ticked []string, held []string) map[string]string {
		scope := scopeOf(grants)
		scope.held = map[string]struct{}{}
		for _, p := range held {
			scope.held[p] = struct{}{}
		}
		return moduleKeySet(modulesForScope(scope, dept, "en", true, ticked))
	}

	t.Run("an operator ticked for sale_allocation on the phone gets the module", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleOperator)}
		ticked := []string{"vaccination", "sale_allocation"}
		held := permissions.PermissionsForAssignmentsWithBaseline([]permissions.ModuleAssignment{
			{Module: "vaccination", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
			{Module: "sale_allocation", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
		})
		keys := modulesFromTicks(grants, []string{"vaccination"}, ticked, held)
		if _, ok := keys["sale_allocation"]; !ok {
			t.Fatalf("the HRMS tick must give the operator the tag-only module; got %v", keys)
		}
		if _, ok := keys["sales"]; ok {
			t.Fatalf("the tick must not widen to the Sales module; got %v", keys)
		}
	})

	t.Run("a park head whose tick was cleared loses the module", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
		ticked := []string{"vaccination"}
		held := permissions.PermissionsForAssignmentsWithBaseline([]permissions.ModuleAssignment{
			{Module: "vaccination", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView, permissions.LevelOversee}},
		})
		keys := modulesFromTicks(grants, []string{"vaccination"}, ticked, held)
		if _, ok := keys["sale_allocation"]; ok {
			t.Fatalf("clearing the tick on /people must take the tag-only module away; got %v", keys)
		}
	})

	t.Run("a park head with the tick keeps it", func(t *testing.T) {
		grants := []domain.GrantSummary{grantWithRole(permissions.RoleParkHead)}
		ticked := []string{"vaccination", "sale_allocation"}
		held := permissions.PermissionsForAssignmentsWithBaseline([]permissions.ModuleAssignment{
			{Module: "vaccination", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelView, permissions.LevelOversee}},
			{Module: "sale_allocation", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}},
		})
		if keys := modulesFromTicks(grants, []string{"vaccination"}, ticked, held); keys["sale_allocation"] == "" {
			t.Fatalf("a ticked park head must keep the tag-only module; got %v", keys)
		}
	})
}
