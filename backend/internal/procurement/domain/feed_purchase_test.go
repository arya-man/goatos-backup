package domain

import (
	"errors"
	"strings"
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

func TestFeedPurchasePaymentWriteValidate(t *testing.T) {
	good := FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: 5000, Note: "advance at loading"}
	if err := good.Normalize().Validate(pinnedToday); err != nil {
		t.Fatalf("valid payment rejected: %v", err)
	}
	for _, tc := range []struct {
		name  string
		write FeedPurchasePaymentWrite
		field string
	}{
		{"garbage date", FeedPurchasePaymentWrite{PaidOn: "yesterday", AmountRupees: 1}, "paid_on"},
		{"future date", FeedPurchasePaymentWrite{PaidOn: "2026-08-25", AmountRupees: 1}, "paid_on"},
		{"zero amount", FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: 0}, "amount_rupees"},
		{"negative amount", FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: -5}, "amount_rupees"},
		{"note too long", FeedPurchasePaymentWrite{PaidOn: "2026-08-24", AmountRupees: 1, Note: strings.Repeat("x", 301)}, "note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.write.Normalize().Validate(pinnedToday)
			var v ErrFeedPurchaseValidation
			if !errors.As(err, &v) || v.Field != tc.field {
				t.Fatalf("want validation error on %q, got %v", tc.field, err)
			}
		})
	}
}

func TestDeriveFeedPaymentStatus(t *testing.T) {
	total := 10000.0
	if got := DeriveFeedPaymentStatus(&total, 10000, FeedPaymentPending); got != FeedPaymentPaid {
		t.Fatalf("fully released should read Paid, got %q", got)
	}
	// The half-paisa tolerance: a numeric(14,2) rounding artefact must not hold a settled load open.
	if got := DeriveFeedPaymentStatus(&total, 9999.996, FeedPaymentPending); got != FeedPaymentPaid {
		t.Fatalf("rounding-artefact shortfall should read Paid, got %q", got)
	}
	if got := DeriveFeedPaymentStatus(&total, 4000, FeedPaymentPaid); got != FeedPaymentPending {
		t.Fatalf("partly released should read Pending even if it was marked Paid, got %q", got)
	}
	// Unknown landed cost keeps whatever the load already says: money against an unknown total
	// proves nothing either way.
	if got := DeriveFeedPaymentStatus(nil, 4000, FeedPaymentPaid); got != FeedPaymentPaid {
		t.Fatalf("unknown total must keep the current status, got %q", got)
	}
}

func TestFeedPurchasePaymentBalance(t *testing.T) {
	total, released := 10000.0, 4000.0
	p := FeedPurchase{TotalCost: &total, PaymentReleased: &released, PaymentStatus: FeedPaymentPending}
	if got := p.PaymentBalance(); got == nil || *got != 6000 {
		t.Fatalf("balance = %v want 6000", got)
	}
	// A load marked Paid owes nothing even when no released figure was ever recorded -- the shape
	// most sheet-history rows have. Deriving total-minus-nothing there would print a false
	// remaining on a settled load.
	paidNoFigure := FeedPurchase{TotalCost: &total, PaymentStatus: FeedPaymentPaid}
	if got := paidNoFigure.PaymentBalance(); got == nil || *got != 0 {
		t.Fatalf("Paid with no released figure: balance = %v want 0", got)
	}
	over := 12000.0
	p.PaymentReleased = &over
	if got := p.PaymentBalance(); got == nil || *got != 0 {
		t.Fatalf("overpaid balance must floor at zero, got %v", got)
	}
	p.TotalCost = nil
	if got := p.PaymentBalance(); got != nil {
		t.Fatalf("unknown total must yield nil balance, got %v", got)
	}
	p = FeedPurchase{TotalCost: &total}
	if got := p.PaymentBalance(); got == nil || *got != total {
		t.Fatalf("nothing released: balance should equal the total, got %v", got)
	}
}

func TestNormalizeFeedPaymentStatus(t *testing.T) {
	for raw, want := range map[string]string{"paid": "Paid", " PENDING ": "Pending", "Paid": "Paid"} {
		got, ok := NormalizeFeedPaymentStatus(raw)
		if !ok || got != want {
			t.Fatalf("NormalizeFeedPaymentStatus(%q) = %q,%v want %q", raw, got, ok, want)
		}
	}
	if _, ok := NormalizeFeedPaymentStatus("Partial"); ok {
		t.Fatal("a word outside the closed vocabulary must be rejected, never defaulted")
	}
}

// TestDeriveFeedPerKgCostUsesTheWeightTheFarmHas pins the ONE landed-rate rule (maintainer decision
// 2026-09-03): landed cost over the received weight once it is entered, over the buying weight
// until then, and absent when there is no total or nothing to divide by.
func TestDeriveFeedPerKgCostUsesTheWeightTheFarmHas(t *testing.T) {
	total := 76980.0
	received := 5000.0
	if got := DeriveFeedPerKgCost(nil, 5420, &received); got != nil {
		t.Fatalf("no total: rate = %v want nil", *got)
	}
	if got := DeriveFeedPerKgCost(&total, 0, nil); got != nil {
		t.Fatalf("no weight: rate = %v want nil", *got)
	}
	buying := DeriveFeedPerKgCost(&total, 5420, nil)
	if buying == nil || *buying < 14.20 || *buying > 14.21 {
		t.Fatalf("buying-weight rate = %v want ~14.2", buying)
	}
	// A load that shrank on the road costs MORE per kg in the store: 76980 / 5000.
	shrunk := DeriveFeedPerKgCost(&total, 5420, &received)
	if shrunk == nil || *shrunk < 15.39 || *shrunk > 15.40 {
		t.Fatalf("received-weight rate = %v want ~15.396", shrunk)
	}
}

// TestFeedPurchaseDeliveryWriteValidate pins the arrival rules: a real date, not in the future,
// not before the load's purchase date, and a positive received weight when one is given -- with
// the weight itself OPTIONAL, because entering it is deferrable.
func TestFeedPurchaseDeliveryWriteValidate(t *testing.T) {
	today := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	zero := 0.0
	kg := 5380.0
	for name, tc := range map[string]struct {
		write FeedPurchaseDeliveryWrite
		field string
	}{
		"bad date":              {FeedPurchaseDeliveryWrite{ReachedOn: "yesterday"}, "reached_on"},
		"future":                {FeedPurchaseDeliveryWrite{ReachedOn: "2026-09-04"}, "reached_on"},
		"before purchase":       {FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-29"}, "reached_on"},
		"zero weight":           {FeedPurchaseDeliveryWrite{ReachedOn: "2026-09-02", ReachedWeightKg: &zero}, "reached_weight_kg"},
		"deferred weight is ok": {FeedPurchaseDeliveryWrite{ReachedOn: "2026-09-02"}, ""},
		"weighed is ok":         {FeedPurchaseDeliveryWrite{ReachedOn: "2026-09-03", ReachedWeightKg: &kg}, ""},
		"same day as purchase":  {FeedPurchaseDeliveryWrite{ReachedOn: "2026-08-30"}, ""},
	} {
		err := tc.write.Normalize().Validate("2026-08-30", today)
		if tc.field == "" {
			if err != nil {
				t.Fatalf("%s: unexpected %v", name, err)
			}
			continue
		}
		var v ErrFeedPurchaseValidation
		if !errors.As(err, &v) || v.Field != tc.field {
			t.Fatalf("%s: err = %v want field %s", name, err, tc.field)
		}
	}
}

// TestFeedPurchaseStockKgIsAbsentOnTheRoad pins what a load is worth in the store: nothing (nil,
// not zero) while on the road, the buying weight once reached with no weighbridge figure, and the
// received weight once that is entered. Mirrors the feed_purchases.stock_kg generated column.
func TestFeedPurchaseStockKgIsAbsentOnTheRoad(t *testing.T) {
	received := 5000.0
	if got := (FeedPurchase{DeliveryStatus: FeedDeliveryPurchased, QuantityKg: 5420, ReachedWeightKg: &received}).StockKg(); got != nil {
		t.Fatalf("on the road: stock = %v want nil", *got)
	}
	if got := (FeedPurchase{DeliveryStatus: FeedDeliveryReached, QuantityKg: 5420}).StockKg(); got == nil || *got != 5420 {
		t.Fatalf("reached unweighed: stock = %v want 5420", got)
	}
	if got := (FeedPurchase{DeliveryStatus: FeedDeliveryReached, QuantityKg: 5420, ReachedWeightKg: &received}).StockKg(); got == nil || *got != 5000 {
		t.Fatalf("reached weighed: stock = %v want 5000", got)
	}
}

// TestNormalizeFeedDeliveryFilter pins the closed filter vocabulary: blank/all widen, an exact
// state (case-insensitive, trimmed) passes, anything else is refused rather than defaulted.
func TestNormalizeFeedDeliveryFilter(t *testing.T) {
	for raw, want := range map[string]string{"": "", "all": "", " Reached ": FeedDeliveryReached, "PURCHASED": FeedDeliveryPurchased} {
		got, ok := NormalizeFeedDeliveryFilter(raw)
		if !ok || got != want {
			t.Fatalf("%q -> %q,%v want %q", raw, got, ok, want)
		}
	}
	if _, ok := NormalizeFeedDeliveryFilter("in transit"); ok {
		t.Fatal("an unknown delivery filter was accepted")
	}
}
