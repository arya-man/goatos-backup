package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// advanceOnlyWrite is an advance taken before anything is chosen: who, when, and the money.
func advanceOnlyWrite() DealWrite {
	return DealWrite{
		SaleDate: "2026-10-02", Farm: "CBE", BuyerName: "Tanveer",
		BuyerVendorID: "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
		AdvanceAmount: fp(50000), AdvanceOnly: true,
	}
}

func TestAdvanceOnlySaleIsAcceptedAsAdvancePaid(t *testing.T) {
	w := advanceOnlyWrite().Normalize(builtinCatalog())
	if err := w.Validate(builtinCatalog(), testFarms); err != nil {
		t.Fatalf("advance-only write rejected: %v", err)
	}
	if w.Status != StatusAdvancePaid {
		t.Fatalf("blank status on an advance-only sale = %q, want Advance Paid", w.Status)
	}
	if len(w.Lines) != 0 || w.ProductType != "" || w.SalesValue != 0 {
		t.Fatalf("advance-only sale grew products: %+v", w)
	}
}

// The flag is what makes a body with no lines an advance. Without it the same body is still the
// "needs at least one product line" refusal it always was, so an older phone that forgot its lines
// never records an advance by accident.
func TestNoLinesWithoutTheFlagIsStillRefused(t *testing.T) {
	w := advanceOnlyWrite()
	w.AdvanceOnly = false
	err := w.Normalize(builtinCatalog()).Validate(builtinCatalog(), testFarms)
	var v ErrDealValidation
	if !errors.As(err, &v) || v.Field != "lines" {
		t.Fatalf("no-lines body without the flag: got %v, want lines refusal", err)
	}
}

func TestAdvanceOnlyRefusesAnythingItCannotHoldYet(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DealWrite)
		field  string
	}{
		{"no advance", func(w *DealWrite) { w.AdvanceAmount = nil }, "advance_amount"},
		{"zero advance", func(w *DealWrite) { w.AdvanceAmount = fp(0) }, "advance_amount"},
		{"a product line", func(w *DealWrite) {
			w.Lines = []DealLineWrite{{ProductType: ProductSheep, Breed: "Anantapur", SalesValue: 1000}}
		}, "lines"},
		{"a sale value", func(w *DealWrite) { w.SalesValue = 1000 }, "sales_value"},
		{"an animal count", func(w *DealWrite) { w.AnimalCount = fp(4) }, "animal_count"},
		{"closed", func(w *DealWrite) { w.Status = StatusDealClosed }, "status"},
		{"in discussion", func(w *DealWrite) { w.Status = StatusInDiscussion }, "status"},
		{"no vendor", func(w *DealWrite) { w.BuyerVendorID = "" }, "buyer_vendor_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := advanceOnlyWrite()
			tc.mutate(&w)
			err := w.Normalize(builtinCatalog()).Validate(builtinCatalog(), testFarms)
			var v ErrDealValidation
			if !errors.As(err, &v) || v.Field != tc.field {
				t.Fatalf("got %v, want field %q", err, tc.field)
			}
		})
	}
}

func TestAdvanceOnlySaleIsNeverOfferedClosed(t *testing.T) {
	bare := Deal{Status: StatusAdvancePaid}
	if got := NextStatusesForDeal(bare); reflect.DeepEqual(got, Statuses) || contains(got, StatusDealClosed) {
		t.Fatalf("advance-only options = %v, must not offer Deal Closed", got)
	}
	sold := Deal{Status: StatusAdvancePaid, ProductType: ProductSheep}
	if got := NextStatusesForDeal(sold); !reflect.DeepEqual(got, Statuses) {
		t.Fatalf("sale with products options = %v, want every status", got)
	}
	if got := NextStatusesForDeal(Deal{Status: StatusDealFailed}); len(got) != 0 {
		t.Fatalf("failed advance options = %v, want none", got)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestAddingLinesUsesTheRecordSaleRules(t *testing.T) {
	ok := DealLinesWrite{Lines: []DealLineWrite{{ProductType: ProductSheep, Breed: "Anantapur", AnimalCount: fp(10), SalesValue: 90000}}}
	n, r := ok.Normalize(builtinCatalog())
	if err := n.Validate(builtinCatalog(), r); err != nil {
		t.Fatalf("valid lines rejected: %v", err)
	}
	if r.SalesValue != 90000 || r.ProductType != ProductSheep {
		t.Fatalf("rollup = %+v", r)
	}
	if !n.AsDealWrite(r).HasLiveAnimals() {
		t.Fatal("ten sheep must open the animal steps")
	}
	for name, w := range map[string]DealLinesWrite{
		"none":      {},
		"unknown":   {Lines: []DealLineWrite{{ProductType: "Cattle", Breed: "x", SalesValue: 1}}},
		"worthless": {Lines: []DealLineWrite{{ProductType: ProductSheep, Breed: "Anantapur", SalesValue: 0}}},
	} {
		n, r := w.Normalize(builtinCatalog())
		if err := n.Validate(builtinCatalog(), r); err == nil {
			t.Fatalf("%s lines accepted", name)
		}
	}
}

func TestSettlementRulesAndOutcomes(t *testing.T) {
	today := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		w     AdvanceSettlementWrite
		field string
	}{
		"keep all":         {AdvanceSettlementWrite{}, ""},
		"refund dated":     {AdvanceSettlementWrite{RefundedRupees: 20000, RefundedOn: "2026-10-01"}, ""},
		"refund undated":   {AdvanceSettlementWrite{RefundedRupees: 20000}, "refunded_on"},
		"refund in future": {AdvanceSettlementWrite{RefundedRupees: 20000, RefundedOn: "2026-10-03"}, "refunded_on"},
		"keep with a date": {AdvanceSettlementWrite{RefundedOn: "2026-10-01"}, "refunded_on"},
		"negative refund":  {AdvanceSettlementWrite{RefundedRupees: -1}, "refunded_rupees"},
	} {
		err := tc.w.Normalize().Validate(today)
		var v ErrDealValidation
		if tc.field == "" && err != nil {
			t.Fatalf("%s: rejected: %v", name, err)
		}
		if tc.field != "" && (!errors.As(err, &v) || v.Field != tc.field) {
			t.Fatalf("%s: got %v, want field %q", name, err, tc.field)
		}
	}
	if got := SettlementOutcome(AdvanceSettlement{}, 50000); got != SettlementKept {
		t.Fatalf("nothing refunded = %q", got)
	}
	if got := SettlementOutcome(AdvanceSettlement{RefundedRupees: 50000}, 50000); got != SettlementRefunded {
		t.Fatalf("all refunded = %q", got)
	}
	part := AdvanceSettlement{RefundedRupees: 20000}
	if got := SettlementOutcome(part, 50000); got != SettlementPartRefunded || part.KeptRupees(50000) != 30000 {
		t.Fatalf("part refund = %q kept %v", got, part.KeptRupees(50000))
	}
	for _, o := range []string{SettlementKept, SettlementRefunded, SettlementPartRefunded} {
		if SettlementOutcomeLabel(o) == "" {
			t.Fatalf("outcome %q has no words", o)
		}
	}
}
