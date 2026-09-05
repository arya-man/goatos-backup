package app

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestSalesVendorsIsTheSameRegisterNarrowedToItsSellingHalf pins the 2026-09-05 split on the
// admin-web contract.
//
// THE SIDE IS DECLARED IN THE CONTRACT, not chosen by the renderer, and that is the whole point of
// asserting it here. Both pages render through ONE component (features/procurement/vendor-board),
// which reads its side back out of its own table `data_source`. If the contract stopped naming a
// side, that component would fall back to the whole register and the buying desk's page would
// quietly start listing butchers -- with nothing on screen saying so. A renderer free to pick its
// own half is the same defect one layer over.
//
// Mutation-tested when written: dropping `?side=sales` from the sales-vendors data source, and
// dropping `?side=procurement` from the vendors one, each turn this red.
func TestSalesVendorsIsTheSameRegisterNarrowedToItsSellingHalf(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})

	sales := pageByRouteID(t, resp.Pages, "sales-vendors")
	procurement := pageByRouteID(t, resp.Pages, "vendors")

	if sales.Href != "/sales/vendors" || sales.PathPattern != "/sales/vendors" {
		t.Fatalf("sales-vendors href/pattern = %q/%q", sales.Href, sales.PathPattern)
	}
	if sales.SurfaceKind != "module-surface" {
		t.Fatalf("sales-vendors surface kind = %q, want module-surface", sales.SurfaceKind)
	}

	// Each page names its OWN side, and neither leaves it to a default.
	for name, want := range map[string]string{"sales-vendors": "side=sales", "vendors": "side=procurement"} {
		page := sales
		if name == "vendors" {
			page = procurement
		}
		if len(page.Tables) != 1 {
			t.Fatalf("%s declares %d tables, want exactly the register", name, len(page.Tables))
		}
		if !strings.Contains(page.Tables[0].DataSource, want) {
			t.Errorf("%s data source = %q, must declare %q; without it the page serves the WHOLE register", name, page.Tables[0].DataSource, want)
		}
	}

	// SAME endpoint, SAME columns, SAME row key. It is one table split by vocabulary, not two
	// registers -- a divergence here would mean two screens to keep in step for one dataset.
	if got, want := strings.Split(sales.Tables[0].DataSource, "?")[0], "/procurement/vendors"; got != want {
		t.Errorf("sales-vendors reads %q, want the one register at %q", got, want)
	}
	columnKeys := func(cols []domain.Column) string {
		keys := make([]string, 0, len(cols))
		for _, c := range cols {
			keys = append(keys, c.Key)
		}
		return strings.Join(keys, ",")
	}
	if columnKeys(sales.Tables[0].Columns) != columnKeys(procurement.Tables[0].Columns) {
		t.Errorf("the two registers declare different columns:\n sales = %s\n procurement = %s", columnKeys(sales.Tables[0].Columns), columnKeys(procurement.Tables[0].Columns))
	}
	if sales.Tables[0].RowClick.Param != procurement.Tables[0].RowClick.Param {
		t.Errorf("row click param differs: %q vs %q", sales.Tables[0].RowClick.Param, procurement.Tables[0].RowClick.Param)
	}

	// The copy map is SHARED so the two pages cannot word the same form differently, with only the
	// sentences that would be wrong on the other page overridden.
	for _, key := range []string{"action.add", "field.record_type", "drawer.add.title", "required.hint.create"} {
		if sales.Copy[key] != procurement.Copy[key] {
			t.Errorf("copy key %q differs between the registers: %q vs %q", key, sales.Copy[key], procurement.Copy[key])
		}
	}
	if sales.Copy["crumb"] != "Sales" {
		t.Errorf("sales-vendors crumb = %q, want Sales", sales.Copy["crumb"])
	}
	if procurement.Copy["crumb"] != "Procurement" {
		t.Errorf("vendors crumb = %q, want Procurement", procurement.Copy["crumb"])
	}
}

// TestSalesVendorsIsGatedOnTheRegistersOwnPermission pins that putting the register under Sales did
// NOT create a second door into it.
//
// The leaf sits in the Sales group and ticks with the sales module, but it is REACHED on VendorRead
// -- the same permission Procurement > Vendors uses -- because it renders vendor rows, contact
// numbers and a drawer that reaches payment instruments. Gating it on SalesRead would hand the
// register to a sales reader who was deliberately never given it.
//
// The nav gate and the page catalog are asserted to AGREE, because a leaf offered on one permission
// while its route enforces another is the dead-leaf defect: it renders, and then 403s.
func TestSalesVendorsIsGatedOnTheRegistersOwnPermission(t *testing.T) {
	nav := permissionsForNav("sales-vendors")
	if len(nav) != 1 || nav[0] != permissions.VendorRead {
		t.Fatalf("sales-vendors nav gate = %v, want [%s]", nav, permissions.VendorRead)
	}
	if same := permissionsForNav("procurement-vendors"); len(same) != 1 || same[0] != permissions.VendorRead {
		t.Fatalf("procurement-vendors nav gate = %v; the two halves of one register must share one authority", same)
	}
	// Not SalesRead, stated directly so a later "tidy-up" that groups it with the other sales
	// leaves fails here rather than in production.
	for _, p := range nav {
		if p == permissions.SalesRead {
			t.Fatal("sales-vendors is gated on SalesRead; the register has its own permission and must keep it")
		}
	}

	var found bool
	for _, page := range permissions.ModulePages() {
		if page.Key != "sales-vendors" {
			continue
		}
		found = true
		if page.Module != "sales" {
			t.Errorf("sales-vendors ticks with module %q, want sales -- it is a Sales leaf", page.Module)
		}
		if len(page.Permissions) != 1 || page.Permissions[0] != permissions.VendorRead {
			t.Errorf("sales-vendors catalog permissions = %v, want [%s]", page.Permissions, permissions.VendorRead)
		}
	}
	if !found {
		t.Fatal("sales-vendors is missing from the page catalog; a leaf with no catalog row can never be withheld from anyone")
	}
}
