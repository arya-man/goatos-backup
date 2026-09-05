package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// sideRecordingRepo is a VendorRepository that answers the catalog read with a fixed vocabulary and
// remembers the filter it was handed. Only the two methods these tests exercise do anything; the
// rest satisfy the interface.
type sideRecordingRepo struct {
	ports.VendorRepository
	sawFilter domain.VendorFilter
	entries   []domain.VendorCatalogEntry
}

func (r *sideRecordingRepo) ListVendors(_ context.Context, _ string, filter domain.VendorFilter, _, _ int, _ bool) (ports.VendorPage, error) {
	r.sawFilter = filter
	return ports.VendorPage{}, nil
}

func (r *sideRecordingRepo) ListVendorCatalog(context.Context, string, bool) ([]domain.VendorCatalogEntry, error) {
	return r.entries, nil
}

func catalogFixture() []domain.VendorCatalogEntry {
	return []domain.VendorCatalogEntry{
		{Kind: domain.CatalogKindRecordType, Value: "Sheep Agent", Label: "Sheep Agent", RegisterSide: domain.VendorSideProcurement},
		{Kind: domain.CatalogKindRecordType, Value: "Transport Agent", Label: "Transport Agent", RegisterSide: domain.VendorSideProcurement},
		{Kind: domain.CatalogKindRecordType, Value: "Butcher", Label: "Butcher", RegisterSide: domain.VendorSideSales},
		{Kind: domain.CatalogKindRecordType, Value: "Slaughter House", Label: "Slaughter House", RegisterSide: domain.VendorSideSales},
		// Every other vocabulary is SHARED. These carry the inert storage default and must survive
		// a narrowing untouched -- a butcher and a feed stockist are in the same states and towns.
		{Kind: domain.CatalogKindState, Value: "KA", Label: "Karnataka", RegisterSide: domain.VendorSideProcurement},
		{Kind: domain.CatalogKindCity, Value: "Ballari", Label: "Ballari", RegisterSide: domain.VendorSideProcurement},
		{Kind: domain.CatalogKindBreed, Value: "Sojat", Label: "Sojat", RegisterSide: domain.VendorSideProcurement},
		{Kind: domain.CatalogKindStatus, Value: "active", Label: "Active", RegisterSide: domain.VendorSideProcurement},
	}
}

func recordTypeValues(t *testing.T, entries []domain.VendorCatalogEntry) []string {
	t.Helper()
	out := []string{}
	for _, e := range entries {
		if e.Kind == domain.CatalogKindRecordType {
			out = append(out, e.Value)
		}
	}
	return out
}

// TestCatalogSideNarrowsRecordTypesAndNothingElse pins where the Add-vendor dropdown's contents
// come from on each register.
//
// This is what makes Sales > Vendors offer five buyer categories rather than all forty, and it
// happens on the SERVER: the whole list never reaches the browser, so the form physically cannot
// offer a category the page does not own. A client-side filter would leave the full vocabulary in
// the payload, one render away from being shown.
func TestCatalogSideNarrowsRecordTypesAndNothingElse(t *testing.T) {
	repo := &sideRecordingRepo{entries: catalogFixture()}
	svc := NewVendorService(repo)
	ctx := context.Background()

	sales, err := svc.ListVendorCatalog(ctx, "t", domain.VendorSideSales)
	if err != nil {
		t.Fatalf("sales catalog: %v", err)
	}
	if got := recordTypeValues(t, sales); len(got) != 2 || got[0] != "Butcher" || got[1] != "Slaughter House" {
		t.Fatalf("sales record types = %v, want the two buyer categories", got)
	}

	procurement, err := svc.ListVendorCatalog(ctx, "t", domain.VendorSideProcurement)
	if err != nil {
		t.Fatalf("procurement catalog: %v", err)
	}
	if got := recordTypeValues(t, procurement); len(got) != 2 || got[0] != "Sheep Agent" || got[1] != "Transport Agent" {
		t.Fatalf("procurement record types = %v, want the two supply categories", got)
	}

	// Both sides keep every OTHER vocabulary whole. The count is asserted rather than the values so
	// that adding a shared kind later does not silently start being narrowed.
	for name, entries := range map[string][]domain.VendorCatalogEntry{"sales": sales, "procurement": procurement} {
		shared := 0
		for _, e := range entries {
			if e.Kind != domain.CatalogKindRecordType {
				shared++
			}
		}
		if shared != 4 {
			t.Errorf("%s side kept %d shared vocabulary entries, want all 4; only record types have a side", name, shared)
		}
	}

	// No side at all is the WHOLE vocabulary. The vendor picklist and the label resolver both read
	// it this way, and narrowing them would break naming the buyer of a sale.
	whole, err := svc.ListVendorCatalog(ctx, "t", "")
	if err != nil {
		t.Fatalf("whole catalog: %v", err)
	}
	if len(whole) != len(catalogFixture()) {
		t.Fatalf("unnarrowed catalog returned %d entries, want the whole %d", len(whole), len(catalogFixture()))
	}
}

// TestAnUnknownSideIsRefusedOnBothReads pins the fail-closed rule at the SERVICE boundary, where
// the refusal has to happen for the HTTP layer to turn it into a 400.
//
// Refusing rather than widening matters because the alternative is silent: a page asking for one
// half and receiving the whole register looks exactly like a working page, and the buying desk
// would simply start seeing butchers.
func TestAnUnknownSideIsRefusedOnBothReads(t *testing.T) {
	repo := &sideRecordingRepo{entries: catalogFixture()}
	svc := NewVendorService(repo)
	ctx := context.Background()

	if _, err := svc.ListVendors(ctx, "t", VendorListQuery{Filter: domain.VendorFilter{Side: "selling"}}); !errors.Is(err, ErrVendorSideUnknown) {
		t.Fatalf("ListVendors with an unknown side: err = %v, want ErrVendorSideUnknown", err)
	}
	if repo.sawFilter.Side != "" {
		t.Fatal("the repository was queried despite an unknown side; the refusal must happen before the read")
	}
	if _, err := svc.ListVendorCatalog(ctx, "t", "selling"); !errors.Is(err, ErrVendorSideUnknown) {
		t.Fatalf("ListVendorCatalog with an unknown side: err = %v, want ErrVendorSideUnknown", err)
	}

	// And the legal values still reach the repository verbatim, so the refusal cannot be a blanket
	// rejection that happens to make the test above pass.
	if _, err := svc.ListVendors(ctx, "t", VendorListQuery{Filter: domain.VendorFilter{Side: domain.VendorSideSales}}); err != nil {
		t.Fatalf("ListVendors(side=sales): %v", err)
	}
	if repo.sawFilter.Side != domain.VendorSideSales {
		t.Fatalf("repository saw side %q, want %q", repo.sawFilter.Side, domain.VendorSideSales)
	}
}
