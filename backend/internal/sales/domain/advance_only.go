package domain

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AN ADVANCE CAN BE TAKEN BEFORE THE SALE IS DECIDED (maintainer decision 2026-10-02,
// docs/decisions/sales-sop.md -> "An advance before anything is chosen").
//
// A buyer hands the farm money for a future sale before anyone knows whether it will be animals,
// feed or manure. The desk records it as an ADVANCE-ONLY sale -- buyer, farm, date and the
// advance, with no lines and no value -- and adds what was sold to the SAME sale later. Three
// rules hold it together:
//
//  1. It is asked for EXPLICITLY (DealWrite.AdvanceOnly). An older client that sends a body with
//     no lines still gets "needs at least one product line"; nothing becomes an advance by omission.
//  2. It never closes and is worth nothing until its lines arrive, so it never reaches revenue,
//     buyer analytics or the load-wise report, which all read Deal Closed only.
//  3. Its workflow opens only when the lines are added: the record write emits nothing, the
//     add-lines write emits sales.deal.recorded, and the existing consumer opens the steps then.
//
// If the sale never happens the money is SETTLED on the failed deal, one of two ways chosen per
// case: some or all of it refunded, the rest kept by the farm (AdvanceSettlementWrite).

var (
	// ErrAdvanceOnlyCannotClose refuses closing a sale whose products were never added: a closed
	// sale with nothing in it would count an advance as revenue for goods nobody named.
	ErrAdvanceOnlyCannotClose = errors.New("sales: an advance-only sale cannot close before its products are added")
	// ErrDealAlreadyHasLines refuses adding lines to a sale that already names what it sold. Lines
	// are added to an advance-only sale ONCE; editing a recorded sale's lines is not built.
	ErrDealAlreadyHasLines = errors.New("sales: this sale already names what was sold")
	// ErrSettlementNeedsFailedDeal refuses settling money on a sale that has not failed: a live
	// sale's money is still an advance against it.
	ErrSettlementNeedsFailedDeal = errors.New("sales: only a failed sale's money can be refunded or kept")
	// ErrNothingToSettle refuses settling a failed sale the buyer never paid towards.
	ErrNothingToSettle = errors.New("sales: no money was received on this sale")
)

// AdvanceOnly reports whether this deal is an advance whose products were never added: it names no
// product and carries no lines. Sheet history always names a product, so it never reads as one.
func (d Deal) AdvanceOnly() bool {
	return strings.TrimSpace(d.ProductType) == "" && len(d.Lines) == 0
}

// NextStatusesForDeal is the status picker for THIS deal. It is NextStatuses with one narrowing:
// an advance-only sale is never offered Deal Closed, because the server refuses it.
func NextStatusesForDeal(d Deal) []string {
	all := NextStatuses(d.Status)
	if !d.AdvanceOnly() {
		return all
	}
	out := make([]string, 0, len(all))
	for _, s := range all {
		if s != StatusDealClosed {
			out = append(out, s)
		}
	}
	return out
}

// validateAdvanceOnly is Validate for an advance-only body. Everything about WHO and WHEN is the
// same as any sale; everything about WHAT must be absent, and the advance must be real money.
func (w DealWrite) validateAdvanceOnly() error {
	if len(w.Lines) > 0 || w.ProductType != "" || w.Breed != "" {
		return ErrDealValidation{Field: "lines", Reason: "is added to an advance later, not when the advance is recorded"}
	}
	if w.SalesValue != 0 {
		return ErrDealValidation{Field: "sales_value", Reason: "is set when what was sold is added to the advance"}
	}
	for field, v := range map[string]*float64{
		"animal_count":    w.AnimalCount,
		"male_count":      w.MaleCount,
		"female_count":    w.FemaleCount,
		"total_weight_kg": w.TotalWeightKg,
	} {
		if v != nil {
			return ErrDealValidation{Field: field, Reason: "is set when what was sold is added to the advance"}
		}
	}
	if err := w.validateBuyerAndComments(); err != nil {
		return err
	}
	if w.Status != StatusAdvancePaid {
		return ErrDealValidation{Field: "status", Reason: "of an advance is Advance Paid"}
	}
	if w.AdvanceAmount == nil || *w.AdvanceAmount <= 0 {
		return ErrDealValidation{Field: "advance_amount", Reason: "is needed: enter what the buyer paid"}
	}
	return nil
}

// validateBuyerAndComments is the WHO half of Validate, shared by every kind of sale.
func (w DealWrite) validateBuyerAndComments() error {
	if w.BuyerName == "" {
		return ErrDealValidation{Field: "buyer_name", Reason: "required"}
	}
	if len(w.BuyerName) > maxDealShortField {
		return ErrDealValidation{Field: "buyer_name", Reason: "too long"}
	}
	if len(w.BuyerPlace) > maxDealShortField {
		return ErrDealValidation{Field: "buyer_place", Reason: "too long"}
	}
	// Validate-or-reject, never silently defaulted: a sale with no vendor is refused rather than
	// recorded against nobody. Only the SHAPE is checked here -- the domain must not read the
	// procurement register (the 000173 lock), so that the id names a real vendor is the caller's
	// guarantee, exactly as 000177 validates its sales_deal_id against the deal read.
	if w.BuyerVendorID == "" {
		return ErrDealValidation{Field: "buyer_vendor_id", Reason: "required; pick the buyer from the vendor register"}
	}
	if _, err := uuid.Parse(w.BuyerVendorID); err != nil {
		return ErrDealValidation{Field: "buyer_vendor_id", Reason: "must be a vendor from the register"}
	}
	if len(w.Comments) > maxDealLongField {
		return ErrDealValidation{Field: "comments", Reason: "too long"}
	}
	return nil
}

// DealLinesWrite adds what was sold to an advance-only sale. The deal row becomes the rollup of
// these lines exactly as a sale recorded with them would have.
type DealLinesWrite struct {
	Lines []DealLineWrite
}

// Normalize resolves every line against the tenant's active product registry, as DealWrite does,
// and returns the rollup the deal row will store.
func (w DealLinesWrite) Normalize(cat ProductCatalog) (DealLinesWrite, DealRollup) {
	out := DealLinesWrite{Lines: make([]DealLineWrite, 0, len(w.Lines))}
	for _, l := range w.Lines {
		out.Lines = append(out.Lines, l.normalize(cat))
	}
	return out, RollupLines(out.Lines)
}

// Validate applies the same line rules as recording a sale. That the value covers the money
// already received is checked by the repository under the deal's row lock, where the received
// total is known.
func (w DealLinesWrite) Validate(cat ProductCatalog, rollup DealRollup) error {
	if len(w.Lines) == 0 {
		return ErrDealValidation{Field: "lines", Reason: "needs at least one product line"}
	}
	if len(w.Lines) > MaxDealLines {
		return ErrDealValidation{Field: "lines", Reason: "has too many lines for one sale"}
	}
	for i, l := range w.Lines {
		if err := l.validate(i+1, cat); err != nil {
			return err
		}
	}
	if rollup.SalesValue <= 0 {
		return ErrDealValidation{Field: "sales_value", Reason: "must be more than zero"}
	}
	return nil
}

// AsDealWrite returns the lines as the DealWrite fields the shared line helpers read
// (HasLiveAnimals, LineKinds) and the recorded-event payload carries.
func (w DealLinesWrite) AsDealWrite(r DealRollup) DealWrite {
	return DealWrite{
		Lines: w.Lines, ProductType: r.ProductType, Breed: r.Breed,
		AnimalCount: r.AnimalCount, MaleCount: r.MaleCount, FemaleCount: r.FemaleCount,
		TotalWeightKg: r.TotalWeightKg, SalesValue: r.SalesValue,
	}
}

// AdvanceSettlement is what became of a failed sale's money.
type AdvanceSettlement struct {
	RefundedRupees float64
	RefundedOn     *string // YYYY-MM-DD; nil when nothing was refunded
	Note           string
	SettledBy      string
	UpdatedAt      string
}

// Settlement outcomes, the key every surface renders. The words are backend copy
// (SettlementOutcomeLabel), never composed by a client.
const (
	SettlementRefunded     = "refunded"
	SettlementPartRefunded = "part_refunded"
	SettlementKept         = "kept"
)

// SettlementOutcome names the settlement against what the buyer handed over. Refunding nothing is
// the farm keeping it all; refunding everything (to the paisa) is a full refund.
func SettlementOutcome(s AdvanceSettlement, received float64) string {
	switch {
	case s.RefundedRupees <= 0:
		return SettlementKept
	case s.RefundedRupees >= received-0.005:
		return SettlementRefunded
	default:
		return SettlementPartRefunded
	}
}

// SettlementOutcomeLabel is the farm wording for an outcome.
func SettlementOutcomeLabel(outcome string) string {
	switch outcome {
	case SettlementRefunded:
		return "Refunded to the buyer"
	case SettlementPartRefunded:
		return "Part refunded, rest kept by the farm"
	case SettlementKept:
		return "Kept by the farm"
	default:
		return ""
	}
}

// KeptRupees is the part of the received money the farm keeps, never negative.
func (s AdvanceSettlement) KeptRupees(received float64) float64 {
	kept := received - s.RefundedRupees
	if kept < 0 {
		return 0
	}
	return kept
}

// AdvanceSettlementWrite records what became of a failed sale's money. RefundedRupees 0 is the
// "farm keeps it" decision and carries no date; anything more is handed back on RefundedOn.
type AdvanceSettlementWrite struct {
	RefundedRupees float64
	RefundedOn     string
	Note           string
}

// Normalize trims the write before validation.
func (w AdvanceSettlementWrite) Normalize() AdvanceSettlementWrite {
	out := w
	out.RefundedOn = strings.TrimSpace(w.RefundedOn)
	out.Note = strings.Join(strings.Fields(w.Note), " ")
	return out
}

// Validate applies the settlement rules that need no stored state. today is the caller's IST
// business date: money cannot have gone back on a day that has not happened. That the refund is
// no more than what was received is checked under the deal's row lock.
func (w AdvanceSettlementWrite) Validate(today time.Time) error {
	if w.RefundedRupees < 0 {
		return ErrDealValidation{Field: "refunded_rupees", Reason: "must not be negative"}
	}
	if w.RefundedRupees == 0 {
		if w.RefundedOn != "" {
			return ErrDealValidation{Field: "refunded_on", Reason: "is only for money handed back"}
		}
	} else {
		on, err := time.Parse("2006-01-02", w.RefundedOn)
		if err != nil {
			return ErrDealValidation{Field: "refunded_on", Reason: "is needed: pick the day the money went back"}
		}
		if on.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
			return ErrDealValidation{Field: "refunded_on", Reason: "cannot be in the future"}
		}
	}
	if len(w.Note) > maxDealPaymentNote {
		return ErrDealValidation{Field: "note", Reason: "too long"}
	}
	return nil
}

// Rupees renders money for a refusal the sales desk reads: Indian grouping, whole rupees unless
// there are paise -- 15000 -> "₹15,000", 1234.5 -> "₹1,234.50".
func Rupees(v float64) string {
	paise := int64(math.Round(v * 100))
	sign := ""
	if paise < 0 {
		sign, paise = "-", -paise
	}
	whole := strconv.FormatInt(paise/100, 10)
	if len(whole) > 3 {
		head, tail := whole[:len(whole)-3], whole[len(whole)-3:]
		groups := []string{}
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		if head != "" {
			groups = append([]string{head}, groups...)
		}
		whole = strings.Join(groups, ",") + "," + tail
	}
	if paise%100 == 0 {
		return sign + "₹" + whole
	}
	return sign + "₹" + whole + "." + strconv.FormatInt(paise%100+100, 10)[1:]
}
