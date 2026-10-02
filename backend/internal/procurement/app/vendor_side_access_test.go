package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// sideAccessRepo serves two vendors -- a supplier and a buyer -- and records list filters.
type sideAccessRepo struct {
	sideRecordingRepo
	statusWrites int
}

func (r *sideAccessRepo) GetVendor(_ context.Context, _, vendorID string, _ bool) (domain.Vendor, error) {
	switch vendorID {
	case "supplier":
		return domain.Vendor{VendorID: "supplier", RecordType: "Sheep Agent"}, nil
	case "buyer":
		return domain.Vendor{VendorID: "buyer", RecordType: "Butcher"}, nil
	case "uncatalogued":
		return domain.Vendor{VendorID: "uncatalogued", RecordType: "Sheds Contractor"}, nil
	}
	return domain.Vendor{}, ports.ErrVendorNotFound
}

func (r *sideAccessRepo) UpdateVendorStatus(_ context.Context, _, vendorID, _ string, _ int64, _ string) (domain.Vendor, error) {
	r.statusWrites++
	return domain.Vendor{VendorID: vendorID}, nil
}

var buyersOnly = VendorSideAccess{ReadSales: true, WriteSales: true}

// TestABuyersOnlyCallerNeverReachesASupplier is bug 4 of the People / HRMS fixes (2026-10-02):
// one permission opened both halves of the register, so a person given only the buyers could
// list every supplier by asking for `?side=procurement` or for no side at all, and open or edit
// any supplier by id. Each of those paths must now refuse.
func TestABuyersOnlyCallerNeverReachesASupplier(t *testing.T) {
	repo := &sideAccessRepo{sideRecordingRepo: sideRecordingRepo{entries: catalogFixture()}}
	svc := NewVendorService(repo)
	ctx := WithVendorSideAccess(context.Background(), buyersOnly)

	if _, err := svc.ListVendors(ctx, "t", VendorListQuery{Filter: domain.VendorFilter{Side: "procurement"}}); !errors.Is(err, ErrVendorSideForbidden) {
		t.Fatalf("listing the suppliers: err = %v, want ErrVendorSideForbidden", err)
	}
	if _, err := svc.ListVendors(ctx, "t", VendorListQuery{}); err != nil {
		t.Fatalf("listing with no side: %v", err)
	}
	if repo.sawFilter.Side != domain.VendorSideSales {
		t.Fatalf("no side read the %q half; a buyers-only caller must get the buyers, never the whole register", repo.sawFilter.Side)
	}
	if _, err := svc.ListVendorOptions(ctx, "t"); err != nil {
		t.Fatalf("listing buyer options: %v", err)
	}
	if repo.sawOptionSide != domain.VendorSideSales {
		t.Fatalf("options read the %q half; a buyers-only caller must get buyer options, never the whole register", repo.sawOptionSide)
	}
	if _, err := svc.GetVendor(ctx, "t", "supplier", false); !errors.Is(err, ports.ErrVendorNotFound) {
		t.Fatalf("opening a supplier: err = %v, want not found", err)
	}
	if _, err := svc.GetVendor(ctx, "t", "uncatalogued", false); !errors.Is(err, ports.ErrVendorNotFound) {
		t.Fatalf("an uncatalogued type is procurement-side; err = %v, want not found", err)
	}
	if _, err := svc.GetVendor(ctx, "t", "buyer", false); err != nil {
		t.Fatalf("opening a buyer: %v", err)
	}
	if _, err := svc.UpdateVendorStatus(ctx, "t", "supplier", "inactive", 1, "a"); !errors.Is(err, ports.ErrVendorNotFound) {
		t.Fatalf("changing a supplier's status: err = %v, want not found", err)
	}
	if repo.statusWrites != 0 {
		t.Fatal("a refused status change reached the repository")
	}
	cat, err := svc.ListVendorCatalog(ctx, "t", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range recordTypeValues(t, cat) {
		if v == "Sheep Agent" || v == "Transport Agent" {
			t.Fatalf("a buyers-only caller was offered the supplier type %q", v)
		}
	}
}

// TestASuppliersOnlyCallerCannotAddABuyer: the write half, the other way round.
func TestASuppliersOnlyCallerCannotAddABuyer(t *testing.T) {
	repo := &formVendorRepo{}
	svc := NewVendorService(repo)
	ctx := WithVendorSideAccess(context.Background(), VendorSideAccess{ReadSupply: true, WriteSupply: true})
	write := domain.VendorWrite{BusinessName: "Ravi Meats", RecordType: "Agent", ContactPersonName: "Ravi", PhoneNumber: "9999999999", State: "Karnataka", City: "Ballari", Status: "active"}
	if _, err := svc.CreateVendor(ctx, "t", write, "a", false); !errors.Is(err, ErrVendorSideForbidden) {
		t.Fatalf("adding a buyer: err = %v, want ErrVendorSideForbidden", err)
	}
	if repo.created.BusinessName != "" {
		t.Fatal("a refused create reached the repository")
	}
	write.RecordType = "Feed Agent"
	if _, err := svc.CreateVendor(ctx, "t", write, "a", false); err != nil {
		t.Fatalf("adding a supplier: %v", err)
	}
}

// TestBothHalvesStillReadTheWholeRegister: holding both is exactly what everyone had before.
func TestBothHalvesStillReadTheWholeRegister(t *testing.T) {
	repo := &sideAccessRepo{sideRecordingRepo: sideRecordingRepo{entries: catalogFixture()}}
	svc := NewVendorService(repo)
	ctx := WithVendorSideAccess(context.Background(), VendorSideAccess{ReadSales: true, ReadSupply: true})
	if _, err := svc.ListVendors(ctx, "t", VendorListQuery{}); err != nil || repo.sawFilter.Side != "" {
		t.Fatalf("both halves: side %q err %v, want the whole register", repo.sawFilter.Side, err)
	}
}

// TestTheOtherHalfsFormIsAForbiddenNotAServerError: the edge sweep (2026-10-02) caught a
// buyers-only caller asking for the supplier form answered with a 500 -- the form route wrapped
// every error as "form unavailable". The service refuses the side; the mapping must say 403.
func TestTheOtherHalfsFormIsAForbiddenNotAServerError(t *testing.T) {
	svc := NewVendorService(&formVendorRepo{}).WithVendorFormSource(formSource{})
	ctx := WithVendorSideAccess(context.Background(), buyersOnly)
	_, err := svc.VendorForm(ctx, "t", "procurement")
	if !errors.Is(err, ErrVendorSideForbidden) {
		t.Fatalf("supplier form for a buyers-only caller: err = %v, want ErrVendorSideForbidden", err)
	}
	if got := VendorHTTPError(err); got == nil || got.HTTPStatus != 403 {
		t.Fatalf("mapped to %+v, want a 403", got)
	}
}
