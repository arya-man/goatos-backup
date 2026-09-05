package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestSalesIsItsOwnPhoneModuleCarryingItsOwnVendorsTab pins the 2026-09-05 maintainer decision:
// selling gets its own phone module, the way it got its own web vertical on 2026-08-27.
//
// TWO assertions, and the second is the one a later change is most likely to undo by accident:
//
//  1. Sales has MOVED. The ledger is at /sales, not /vendors/sales, and the Procurement module no
//     longer carries it. A tab living in both modules would be two doors onto one ledger, which is
//     what splitting the desks was meant to end -- so the Procurement half is asserted here too,
//     next to the Sales half, rather than only in vendors_module_offer_test.go where a reader
//     fixing one would not see the other.
//  2. The VENDORS tab inside Sales is the register's selling half, gated on the REGISTER's own
//     permission. Sales and Vendors are different authorities; the module is offered on one and
//     this tab is gated on the other.
//
// Mutation-tested when written: deleting the SalesRead branch in permissionOfferedModuleKeys,
// re-adding the sales contribution to the vendors module, and gating the vendors tab on SalesRead
// each turn a case red.
func TestSalesIsItsOwnPhoneModuleCarryingItsOwnVendorsTab(t *testing.T) {
	const en = localization.DefaultTag

	moduleNamed := func(t *testing.T, modules []domain.BootstrapModule, key string) *domain.BootstrapModule {
		t.Helper()
		for i := range modules {
			if modules[i].Key == key {
				return &modules[i]
			}
		}
		return nil
	}

	for name, grants := range map[string][]domain.GrantSummary{
		"CEO/CXO":              {grantWithRole(permissions.RoleCEOInternal)},
		"procurement_director": {grantWithRole(permissions.RoleProcurementDirector)},
		"procurement_manager":  {grantWithRole(permissions.RoleProcurementManager)},
	} {
		t.Run(name+" is offered Sales as its own module", func(t *testing.T) {
			modules := modulesFor(grants, nil, en)

			sales := moduleNamed(t, modules, "sales")
			if sales == nil {
				t.Fatalf("Sales module did not render; modules = %v", moduleKeySet(modules))
			}
			if sales.Label != "Sales" {
				t.Errorf("Sales module label = %q, want Sales", sales.Label)
			}
			// The phone hosts these hrefs VERBATIM -- GoatOsShell refuses one the build does not
			// host -- so an exact match here is what keeps the tab from being dead on the device.
			if len(sales.NavItems) != 2 ||
				sales.NavItems[0].Href != "/sales" || sales.NavItems[0].Label != "Sales" ||
				sales.NavItems[1].Href != "/sales/vendors" || sales.NavItems[1].Label != "Vendors" {
				t.Fatalf("Sales nav items = %+v", sales.NavItems)
			}

			// The Sales tab is GONE from Procurement. Asserted as "no item points at the ledger"
			// rather than by counting tabs, so re-adding it under any key or label still fails.
			vendors := moduleNamed(t, modules, "vendors")
			if vendors == nil {
				t.Fatal("Procurement module disappeared; it keeps its own Vendors tab and Feed Purchases")
			}
			for _, item := range vendors.NavItems {
				if item.Href == "/vendors/sales" || item.Href == "/sales" {
					t.Errorf("Procurement still carries a Sales tab (%+v); the ledger lives in the Sales module now", item)
				}
			}
			// And Procurement KEEPS its own Vendors tab -- the buying desk still adds suppliers.
			// One table, two sides, one tab each.
			if len(vendors.NavItems) == 0 || vendors.NavItems[0].Href != "/vendors" {
				t.Errorf("Procurement lost its Vendors tab: %+v", vendors.NavItems)
			}
		})
	}

	// The negative half. Nobody without sales read is offered the module, so it has not quietly
	// become a per-job grant for a job the decision excluded.
	for name, grants := range map[string][]domain.GrantSummary{
		"feed_director": {grantWithRole(permissions.RoleFeedDirector)},
		"pc_director":   {grantWithRole(permissions.RolePCDirector)},
		"park_head":     {grantWithRole(permissions.RoleParkHead)},
		"operator":      {grantWithRole(permissions.RoleOperator)},
	} {
		t.Run(name+" is NOT offered Sales", func(t *testing.T) {
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["sales"]; ok {
				t.Fatal("Sales module rendered for a principal without sales read")
			}
		})
	}
}

// TestSalesModuleAndItsVendorsTabAnswerToDifferentPermissions states the split of authority
// directly, because it is the thing a reader is most likely to "simplify".
//
// The module is offered on SalesRead; the Vendors tab inside it is gated on VendorRead. Today the
// same three principals hold both, so the difference is invisible on screen -- which is exactly why
// it needs a test. If the sets ever diverge, a sales reader must still get the Sales module (with
// the ledger tab alone) rather than losing the module, and must NOT reach the vendor register
// through it.
func TestSalesModuleAndItsVendorsTabAnswerToDifferentPermissions(t *testing.T) {
	def, ok := moduleNavRegistry["sales"]
	if !ok {
		t.Fatal("sales is missing from moduleNavRegistry")
	}
	if def.status != moduleStatusAvailable {
		t.Fatalf("sales module status = %q, want available", def.status)
	}
	if def.landingHref != "/sales" {
		t.Fatalf("sales landing href = %q, want /sales", def.landingHref)
	}
	want := map[string]string{
		"sales":         permissions.SalesRead,
		"sales_vendors": permissions.VendorRead,
	}
	if len(def.contributions) != len(want) {
		t.Fatalf("sales contributions = %+v", def.contributions)
	}
	for _, c := range def.contributions {
		if got := want[c.key]; got == "" || c.requiredPermission != got {
			t.Errorf("contribution %q requires %q, want %q", c.key, c.requiredPermission, want[c.key])
		}
	}
}
