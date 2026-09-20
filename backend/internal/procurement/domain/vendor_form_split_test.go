package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationEmbedsTheSeededProcurementVendorForm pins migration 000370 to the SAME seed
// document the sales form ships, byte for byte. That is the whole day-one claim of the split
// (maintainer decision 2026-09-20): the supply register keeps rendering exactly what it rendered
// before, and only diverges when somebody edits it on Procurement > Procurement SOP.
func TestMigrationEmbedsTheSeededProcurementVendorForm(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "postgres", "000370_procurement_vendor_form_sop.sql"))
	if err != nil {
		t.Fatal(err)
	}
	seed := strings.TrimSpace(string(SeededVendorFormJSON()))
	if !strings.Contains(string(raw), "$seed$"+seed+"$seed$") {
		t.Fatal("migration 000370 does not embed vendorformseed/vendor.json verbatim; regenerate the SQL")
	}
	if !strings.Contains(string(raw), "'procurement.vendor'") {
		t.Fatal("migration 000370 does not seed the procurement.vendor SOP code")
	}
}

// TestVendorFormSOPCodeFollowsTheRegisterSide is the split's routing rule. The supply side reads
// its own document; the sales side and an unnamed side keep the sales one, so a screen or a
// legacy row that names no side behaves exactly as it did before the split.
func TestVendorFormSOPCodeFollowsTheRegisterSide(t *testing.T) {
	cases := map[string]string{
		VendorSideProcurement: SOPCodeProcurementVendor,
		VendorSideSales:       SOPCodeVendor,
		"":                    SOPCodeVendor,
		"nonsense":            SOPCodeVendor,
	}
	for side, want := range cases {
		if got := VendorFormSOPCode(side); got != want {
			t.Fatalf("VendorFormSOPCode(%q) = %q, want %q", side, got, want)
		}
	}
}

// TestVendorSideComesFromTheCatalogNotTheCaller proves which document a vendor is judged on is
// resolved from the register's own catalog row for its record type. A type the catalog does not
// carry resolves to no side (and therefore the sales document), rather than guessing.
func TestVendorSideComesFromTheCatalogNotTheCaller(t *testing.T) {
	catalog := []VendorCatalogEntry{
		{Kind: CatalogKindRecordType, Value: "Feed Supplier", Label: "Feed Supplier", IsActive: true, RegisterSide: VendorSideProcurement},
		{Kind: CatalogKindRecordType, Value: "Butcher", Label: "Butcher", IsActive: true, RegisterSide: VendorSideSales},
		{Kind: CatalogKindState, Value: "Karnataka", Label: "Karnataka", IsActive: true, RegisterSide: VendorSideSales},
	}
	if got := VendorSideForRecordType(catalog, "Feed Supplier"); got != VendorSideProcurement {
		t.Fatalf("supply record type resolved to %q", got)
	}
	if got := VendorFormSOPCode(VendorSideForRecordType(catalog, "Feed Supplier")); got != SOPCodeProcurementVendor {
		t.Fatalf("supply record type reads %q", got)
	}
	if got := VendorSideForRecordType(catalog, "butcher"); got != VendorSideSales {
		t.Fatalf("record type match must ignore case, got %q", got)
	}
	if got := VendorSideForRecordType(catalog, "Karnataka"); got != "" {
		t.Fatalf("a non record_type catalog row must not answer the side, got %q", got)
	}
	if got := VendorSideForRecordType(catalog, "Unknown Type"); got != "" {
		t.Fatalf("an unknown record type must resolve to no side, got %q", got)
	}
}
