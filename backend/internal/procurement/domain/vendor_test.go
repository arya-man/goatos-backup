package domain

import "testing"

// TestVendorCapacityIsOptionalInEveryPart pins the 2026-09-03 capacity rules: quantity, unit
// and frequency are each optional on their own ("keep capacity as optional only"), zero is not a
// capacity, and the display line is composed from labels with Indian digit grouping.
func TestVendorCapacityIsOptionalInEveryPart(t *testing.T) {
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
	if err := unitOnly.Normalize().Validate(); err != nil {
		t.Fatalf("a unit with no quantity must be accepted: %v", err)
	}
	qtyOnly := base()
	qtyOnly.CapacityQuantity = &q
	if err := qtyOnly.Normalize().Validate(); err != nil {
		t.Fatalf("a quantity with no unit must be accepted: %v", err)
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
	bare := Vendor{CapacityQuantity: &qty}
	if got := bare.CapacityDisplay("", ""); got != "12,500.5" {
		t.Fatalf("quantity-only display = %q", got)
	}
	if got := (Vendor{}).CapacityDisplay("", ""); got != "" {
		t.Fatalf("empty display = %q", got)
	}
}

// TestVendorSideIsRefusedRatherThanWidened pins the fail-closed half of the 2026-09-05 register
// split: an EMPTY side means "the whole register" and is legal, while an UNRECOGNISED side is
// refused outright.
//
// The asymmetry is the rule. Empty has to stay legal because the vendor picklist -- which names the
// buyer of a sale -- reads the whole register and always did. But a page that ASKED for one half
// and silently received both would put the five buyer categories back on the buying desk's screen
// with nothing on the page admitting it, which is the exact mix the split exists to end.
//
// Mutation-tested when written: returning `("", true)` from the default branch turns the
// unrecognised subtests green-to-red.
func TestVendorSideIsRefusedRatherThanWidened(t *testing.T) {
	for raw, want := range map[string]string{
		"":            "",
		"procurement": VendorSideProcurement,
		"sales":       VendorSideSales,
		"  Sales  ":   VendorSideSales,
		"PROCUREMENT": VendorSideProcurement,
	} {
		got, ok := NormalizeVendorSide(raw)
		if !ok || got != want {
			t.Fatalf("NormalizeVendorSide(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}

	// Every one of these is a plausible near-miss a client could send. None may resolve to a side,
	// and none may resolve to "both".
	for _, raw := range []string{"buying", "selling", "buy", "sell", "vendors", "both", "all", "procurment"} {
		if got, ok := NormalizeVendorSide(raw); ok {
			t.Fatalf("NormalizeVendorSide(%q) accepted, resolving to %q; an unknown side must be refused", raw, got)
		}
	}
}

// TestVendorFilterNormalizeDropsAnUnknownSide pins the second line of defence. The API layer
// refuses an unknown side before a filter is ever built; this covers a caller that constructs a
// domain.VendorFilter directly, where dropping to "the whole register" matches how Status already
// behaves and is the only option that cannot silently mislabel a page.
func TestVendorFilterNormalizeDropsAnUnknownSide(t *testing.T) {
	if got := (VendorFilter{Side: "selling"}).Normalize().Side; got != "" {
		t.Fatalf("unknown side normalized to %q, want empty", got)
	}
	if got := (VendorFilter{Side: " SALES "}).Normalize().Side; got != VendorSideSales {
		t.Fatalf("side normalized to %q, want %q", got, VendorSideSales)
	}
}
