package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/localization"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// TestVendorsModuleIsOfferedOnVendorRead pins the 2026-09-03 maintainer decision: the Vendors
// phone module (vendor register + feed purchases) is offered to the CXO and to the procurement
// director and manager -- the holders of procurement.vendor.read -- and to nobody else. The
// negative half is the point: feed_director sees the feed purchase ledger on the web but holds no
// vendor read, so the module must NOT reach that phone, or the offer has silently become a per-job
// one for a job the decision excluded.
//
// Mutation-tested when written: deleting the VendorRead branch in permissionOfferedModuleKeys, and
// keying it on RoleFeedDirector, each turn a subtest red.
func TestVendorsModuleIsOfferedOnVendorRead(t *testing.T) {
	const en = localization.DefaultTag

	for name, grants := range map[string][]domain.GrantSummary{
		"CEO/CXO":              {grantWithRole(permissions.RoleCEOInternal)},
		"procurement_director": {grantWithRole(permissions.RoleProcurementDirector)},
		"procurement_manager":  {grantWithRole(permissions.RoleProcurementManager)},
		"Hemant (feed_director + procurement_director)": {
			grantWithRole(permissions.RoleFeedDirector),
			grantWithRole(permissions.RoleProcurementDirector),
		},
	} {
		t.Run(name+" is offered Vendors", func(t *testing.T) {
			modules := modulesFor(grants, nil, en)
			var m *domain.BootstrapModule
			for i := range modules {
				if modules[i].Key == "vendors" {
					m = &modules[i]
				}
			}
			if m == nil {
				t.Fatalf("Vendors module did not render; modules = %v", moduleKeySet(modules))
			}
			// Both tabs, in order, on backend-owned hrefs the phone hosts verbatim.
			//
			// TWO since 2026-09-05, not three: the Sales tab MOVED to the Sales module. The
			// assertion is exact rather than a minimum precisely so a re-added third tab here
			// fails -- Sales must live in one module, not both.
			if len(m.NavItems) != 2 || m.NavItems[0].Href != "/vendors" || m.NavItems[1].Href != "/vendors/feed-purchases" {
				t.Fatalf("Vendors nav items = %+v", m.NavItems)
			}
			if m.NavItems[0].Label != "Vendors" || m.NavItems[1].Label != "Feed Purchases" {
				t.Fatalf("Vendors nav labels = %+v", m.NavItems)
			}
			// The module is shown as Procurement (maintainer instruction 2026-09-04) while its key stays vendors.
			if m.Label != "Procurement" {
				t.Fatalf("Vendors module label = %q, want Procurement", m.Label)
			}
		})
	}

	for name, grants := range map[string][]domain.GrantSummary{
		"feed_director": {grantWithRole(permissions.RoleFeedDirector)},
		"pc_director":   {grantWithRole(permissions.RolePCDirector)},
		"park_head":     {grantWithRole(permissions.RoleParkHead)},
		"operator":      {grantWithRole(permissions.RoleOperator)},
	} {
		t.Run(name+" is NOT offered Vendors", func(t *testing.T) {
			if _, ok := moduleKeySet(modulesFor(grants, nil, en))["vendors"]; ok {
				t.Fatal("Vendors module rendered for a principal without vendor read")
			}
		})
	}
}
