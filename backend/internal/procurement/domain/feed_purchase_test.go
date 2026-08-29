package domain

import (
	"errors"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

// validWrite is a purchase that passes every rule, so each test can break exactly one thing.
func validWrite() FeedPurchaseWrite {
	return FeedPurchaseWrite{
		PurchaseDate:  "2026-08-20",
		FarmLabel:     FeedFarmCPT,
		FeedItemLabel: "Dry Sorghum Forage",
		QuantityKg:    5420,
		FeedCost:      f(48980),
		TransportCost: f(28000),
		Vendor:        "Siddi Srilekha",
		PaymentStatus: FeedPaymentPaid,
	}
}

var pinnedToday = time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

// TestFeedPurchaseValidateRejectsEachBadField pins the field rules one at a time. Every case names
// the field it expects to fail on, so a rule that starts rejecting for the WRONG reason is caught
// too -- a validator that returned one blanket error would pass a weaker test.
func TestFeedPurchaseValidateRejectsEachBadField(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*FeedPurchaseWrite)
		field  string
	}{
		{"blank date", func(w *FeedPurchaseWrite) { w.PurchaseDate = "" }, "purchase_date"},
		{"not a date", func(w *FeedPurchaseWrite) { w.PurchaseDate = "20-Aug-2026" }, "purchase_date"},
		// Stock the farm does not have yet must not deplete a feed sheet.
		{"future date", func(w *FeedPurchaseWrite) { w.PurchaseDate = "2026-08-25" }, "purchase_date"},
		{"unknown farm", func(w *FeedPurchaseWrite) { w.FarmLabel = "HYD" }, "farm"},
		{"blank feed", func(w *FeedPurchaseWrite) { w.FeedItemLabel = "" }, "feed_item"},
		{"zero quantity", func(w *FeedPurchaseWrite) { w.QuantityKg = 0 }, "quantity_kg"},
		{"negative quantity", func(w *FeedPurchaseWrite) { w.QuantityKg = -1 }, "quantity_kg"},
		{"batch below one", func(w *FeedPurchaseWrite) { n := 0; w.BatchNo = &n }, "batch_no"},
		{"negative feed cost", func(w *FeedPurchaseWrite) { w.FeedCost = f(-1) }, "feed_cost"},
		{"negative transport cost", func(w *FeedPurchaseWrite) { w.TransportCost = f(-1) }, "transport_cost"},
		{"negative total cost", func(w *FeedPurchaseWrite) { w.TotalCost = f(-1) }, "total_cost"},
		{"negative payment released", func(w *FeedPurchaseWrite) { w.PaymentReleased = f(-1) }, "payment_released"},
		{"blank vendor", func(w *FeedPurchaseWrite) { w.Vendor = "" }, "vendor"},
		{"whitespace vendor", func(w *FeedPurchaseWrite) { w.Vendor = "   " }, "vendor"},
		{"unknown payment status", func(w *FeedPurchaseWrite) { w.PaymentStatus = "Partly paid" }, "payment_status"},
		{"blank payment status", func(w *FeedPurchaseWrite) { w.PaymentStatus = "" }, "payment_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := validWrite()
			tc.mutate(&w)
			// Normalize first, exactly as the service does: a whitespace-only vendor must fail the
			// required check, not pass it because it was non-empty before trimming.
			err := w.Normalize().Validate(pinnedToday)
			var v ErrFeedPurchaseValidation
			if !errors.As(err, &v) {
				t.Fatalf("want a field validation error, got %v", err)
			}
			if v.Field != tc.field {
				t.Fatalf("rejected on %q, want %q", v.Field, tc.field)
			}
		})
	}
}

// TestFeedPurchaseValidateAcceptsAGoodWrite proves the rules above are not simply rejecting
// everything, and that TODAY is allowed -- the boundary the future-date rule sits on.
func TestFeedPurchaseValidateAcceptsAGoodWrite(t *testing.T) {
	if err := validWrite().Normalize().Validate(pinnedToday); err != nil {
		t.Fatalf("valid write rejected: %v", err)
	}
	today := validWrite()
	today.PurchaseDate = "2026-08-24"
	if err := today.Normalize().Validate(pinnedToday); err != nil {
		t.Fatalf("a load bought TODAY must be recordable: %v", err)
	}
}

// TestFeedPurchaseNormalizeCanonicalizes pins the normalization the storage layer depends on.
func TestFeedPurchaseNormalizeCanonicalizes(t *testing.T) {
	w := FeedPurchaseWrite{
		PurchaseDate:  "  2026-08-20 ",
		FarmLabel:     " cpt ",
		FeedItemLabel: "  Dry   Sorghum  Forage ",
		Vendor:        "  Siddi   Srilekha ",
		PaymentStatus: " paid ",
	}.Normalize()
	if w.PurchaseDate != "2026-08-20" || w.FarmLabel != "CPT" {
		t.Fatalf("date/farm = %q/%q", w.PurchaseDate, w.FarmLabel)
	}
	if w.FeedItemLabel != "Dry Sorghum Forage" || w.Vendor != "Siddi Srilekha" {
		t.Fatalf("feed/vendor = %q/%q", w.FeedItemLabel, w.Vendor)
	}
	// The two known payment words are canonicalized to the sheet's spelling; an UNKNOWN one is
	// left alone so Validate rejects it rather than a default being written for the operator.
	if w.PaymentStatus != FeedPaymentPaid {
		t.Fatalf("payment status = %q", w.PaymentStatus)
	}
	unknown := FeedPurchaseWrite{PaymentStatus: " part-paid "}.Normalize()
	if unknown.PaymentStatus != "part-paid" {
		t.Fatalf("an unknown payment status must not be rewritten, got %q", unknown.PaymentStatus)
	}
}

// TestFeedPurchaseCostRollupUsesTheSplitWhenNoTotalIsGiven pins the landed-cost rules: an explicit
// total wins, the split sums when no total is given, and NOTHING entered stays nil rather than
// becoming a zero that would report a free load.
func TestFeedPurchaseCostRollupUsesTheSplitWhenNoTotalIsGiven(t *testing.T) {
	split := validWrite() // feed 48980 + transport 28000, no explicit total
	total := split.TotalOrSplitSum()
	if total == nil || *total != 76980 {
		t.Fatalf("split sum = %v want 76980", total)
	}
	// Per-kg is DERIVED, never entered, so it cannot drift from its own total.
	perKg := split.PerKgCost()
	if perKg == nil || *perKg < 14.20 || *perKg > 14.21 {
		t.Fatalf("per kg = %v want ~14.20", perKg)
	}

	explicit := validWrite()
	explicit.TotalCost = f(80000)
	if got := explicit.TotalOrSplitSum(); got == nil || *got != 80000 {
		t.Fatalf("explicit total = %v want 80000", got)
	}

	// A load whose cost is not yet known is a real state; zero would report a free load.
	unknown := validWrite()
	unknown.FeedCost, unknown.TransportCost = nil, nil
	if got := unknown.TotalOrSplitSum(); got != nil {
		t.Fatalf("no cost entered must stay nil, got %v", *got)
	}
	if got := unknown.PerKgCost(); got != nil {
		t.Fatalf("per kg with no total must stay nil, got %v", *got)
	}

	// An explicit ZERO is a recorded fact and must survive as 0, not collapse to "not entered".
	free := validWrite()
	free.FeedCost, free.TransportCost = f(0), nil
	if got := free.TotalOrSplitSum(); got == nil || *got != 0 {
		t.Fatalf("an entered zero must stay a zero, got %v", got)
	}
}

// TestNormalizeFeedFarmFilterRejectsAnUnknownFarm pins that the ledger's read filter is REJECTED
// rather than silently widened: an unrecognised farm must not show company numbers under a farm
// label.
func TestNormalizeFeedFarmFilterRejectsAnUnknownFarm(t *testing.T) {
	for _, raw := range []string{"", "all", "ALL"} {
		if farm, ok := NormalizeFeedFarmFilter(raw); !ok || farm != "" {
			t.Fatalf("%q => %q/%v, want the whole company", raw, farm, ok)
		}
	}
	if farm, ok := NormalizeFeedFarmFilter("CBE"); !ok || farm != FeedFarmCBE {
		t.Fatalf("CBE => %q/%v", farm, ok)
	}
	if _, ok := NormalizeFeedFarmFilter("cbe"); ok {
		t.Fatal("a farm value must be exact: lower case must be rejected, not coerced")
	}
	if _, ok := NormalizeFeedFarmFilter("HYD"); ok {
		t.Fatal("an unknown farm must be rejected, never widened to the whole company")
	}
}
