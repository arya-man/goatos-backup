package domain

import "testing"

// TestVendorCapacityTravelsAsAPair pins the 2026-09-03 capacity rules: quantity and unit are
// accepted together, refused apart, zero is not a capacity, and the display line is composed
// from labels with Indian digit grouping.
func TestVendorCapacityTravelsAsAPair(t *testing.T) {
	base := func() VendorWrite {
		return VendorWrite{RecordType: "Feed Agent", BusinessName: "Ravi Feeds", Status: "active", State: "KA"}
	}
	q := "5000"
	w := base()
	w.CapacityQuantity, w.CapacityUnit, w.SupplyFrequency = &q, "kg", "per_2_weeks"
	if err := w.Normalize().Validate(); err != nil {
		t.Fatalf("pair: %v", err)
	}
	unitOnly := base()
	unitOnly.CapacityUnit = "kg"
	if err := unitOnly.Normalize().Validate(); err == nil {
		t.Fatal("a unit with no quantity was accepted")
	}
	qtyOnly := base()
	qtyOnly.CapacityQuantity = &q
	if err := qtyOnly.Normalize().Validate(); err == nil {
		t.Fatal("a quantity with no unit was accepted")
	}
	zero := "0"
	z := base()
	z.CapacityQuantity, z.CapacityUnit = &zero, "kg"
	if err := z.Normalize().Validate(); err == nil {
		t.Fatal("a zero capacity was accepted")
	}
	badRef := base()
	badRef.VoiceNoteProofRef = "not-a-uuid"
	if err := badRef.Normalize().Validate(); err == nil {
		t.Fatal("a malformed voice note ref was accepted")
	}

	qty, unit, freq := "12500.500", "kg", "per_2_weeks"
	v := Vendor{CapacityQuantity: &qty, CapacityUnit: &unit, SupplyFrequency: &freq}
	if got := v.CapacityDisplay("kg", "Every 2 weeks"); got != "12,500.5 kg · Every 2 weeks" {
		t.Fatalf("display = %q", got)
	}
	// A frequency alone is still a fact; a missing label falls back to the stored value.
	only := Vendor{SupplyFrequency: &freq}
	if got := only.CapacityDisplay("", ""); got != "per_2_weeks" {
		t.Fatalf("frequency-only display = %q", got)
	}
	if got := (Vendor{}).CapacityDisplay("", ""); got != "" {
		t.Fatalf("empty display = %q", got)
	}
}
