package domain

import (
	"fmt"
	"strings"
	"time"
)

// FEED PURCHASES — the buying side of the feed chain (maintainer decision 2026-08-24).
//
// Migration 000174 bootstrapped feed_purchases from the legacy "Feed DB" sheet and froze it
// read-only, recording that "purchase/vendor entry screens belong to the future Procurement
// vertical". This file is that entry path: the same Purchase row the sheet keeps — farm, date,
// feed, quantity, the landed-cost split, vendor and payment state — authored in the app.
//
// Procurement owns the WRITE; feeddirection keeps the READ (the stock/days-left cards on
// /feed/analytics). Nothing here reads a feed table: the one cross-module fact this path needs is
// whether the feed item exists in the ACTIVE catalog, and that check lives in the repository as a
// single existence read, because a purchase of a feed GoatOS cannot ration is a purchase whose
// stock nobody would ever see.

// Farms the business buys feed for. These are the storage forms the ledger's farm_label carries
// and the importer resolves to park locations.
const (
	FeedFarmCBE = "CBE"
	FeedFarmCPT = "CPT"
)

// FeedFarms is the closed farm vocabulary the entry form renders.
var FeedFarms = []string{FeedFarmCBE, FeedFarmCPT}

// Payment states, exactly the sheet's vocabulary (176 "Paid" and 29 "Pending" rows across the
// imported history — there is no third value, and inventing one would put a state on screen that
// no downstream reader understands).
const (
	FeedPaymentPaid    = "Paid"
	FeedPaymentPending = "Pending"
)

// FeedPaymentStatuses is the closed payment vocabulary the entry form renders.
var FeedPaymentStatuses = []string{FeedPaymentPaid, FeedPaymentPending}

// MaxFeedPurchaseOffset bounds the ledger's paging depth, the same way the sales ledger and the
// vendor register bound theirs.
const MaxFeedPurchaseOffset = 10000

// defaultFeedPurchasePageSize / maxFeedPurchasePageSize bound one ledger page.
const (
	defaultFeedPurchasePageSize = 25
	maxFeedPurchasePageSize     = 100
)

// ClampFeedPurchasePageSize resolves a requested page size onto the supported range.
func ClampFeedPurchasePageSize(limit int) int {
	if limit <= 0 {
		return defaultFeedPurchasePageSize
	}
	if limit > maxFeedPurchasePageSize {
		return maxFeedPurchasePageSize
	}
	return limit
}

// IsFeedFarm reports whether raw is one of the two farms, exactly as stored.
func IsFeedFarm(raw string) bool { return raw == FeedFarmCBE || raw == FeedFarmCPT }

// NormalizeFeedFarmFilter resolves the ledger page's farm query parameter. "" and "all" mean both
// farms; anything else must be an exact farm. ok is false otherwise, so the caller REJECTS rather
// than silently widening the filter and showing company numbers under a farm label.
func NormalizeFeedFarmFilter(raw string) (farm string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	switch {
	case trimmed == "" || strings.EqualFold(trimmed, "all"):
		return "", true
	case trimmed == FeedFarmCBE || trimmed == FeedFarmCPT:
		return trimmed, true
	default:
		return "", false
	}
}

// FeedPurchase is one recorded load: one sheet Purchase row, or one purchase entered in the app.
type FeedPurchase struct {
	FeedPurchaseID string
	TenantID       string

	PurchaseDate  string // YYYY-MM-DD business date
	FarmLabel     string
	FeedItemLabel string
	BatchNo       int
	QuantityKg    float64

	FeedCost      *float64
	TransportCost *float64
	LoadingCost   *float64
	UnloadingCost *float64
	TotalCost     *float64
	PerKgCost     *float64

	Vendor          string
	PaymentReleased *float64
	PaymentStatus   string

	// EntrySource is "app" for a purchase recorded on /procurement/feed-purchases and
	// "sheet_import" for bootstrapped history. The ledger shows the difference rather than
	// presenting imported history as something a person typed here.
	EntrySource string
	RecordedBy  *string

	CreatedAt string

	// Payments are the instalments recorded against this load, oldest first. Sheet history has
	// none: its payment_released figure predates the instalment ledger.
	Payments []FeedPurchasePayment
}

// FeedPurchasePayment is one instalment actually handed to the vendor for one purchased load.
type FeedPurchasePayment struct {
	PaymentID      string
	FeedPurchaseID string
	PaidOn         string // YYYY-MM-DD business date
	AmountRupees   float64
	Note           string
	CreatedAt      string
}

// PaymentBalance is the money still owed on this load: total cost minus what has been released.
//
// A load marked Paid owes NOTHING, whatever the released figure says: most sheet-history rows
// carry "Paid" with no released amount recorded, and deriving total-minus-nothing there would
// print a false "remaining" on a settled load. Otherwise nil when the landed cost is not known
// yet -- a balance against an unknown total would be a number nobody computed. Never negative: an
// overpayment reads as a zero balance, not as the vendor owing the farm through this ledger.
func (p FeedPurchase) PaymentBalance() *float64 {
	if p.PaymentStatus == FeedPaymentPaid {
		zero := 0.0
		return &zero
	}
	if p.TotalCost == nil {
		return nil
	}
	released := 0.0
	if p.PaymentReleased != nil {
		released = *p.PaymentReleased
	}
	balance := *p.TotalCost - released
	if balance < 0 {
		balance = 0
	}
	return &balance
}

// maxFeedPurchasePaymentNote bounds the free-text note on one instalment.
const maxFeedPurchasePaymentNote = 300

// FeedPurchasePaymentWrite is the record-payment form: one instalment against one load.
type FeedPurchasePaymentWrite struct {
	PaidOn       string
	AmountRupees float64
	Note         string
}

// Normalize trims the write before validation, for the same reason FeedPurchaseWrite does.
func (w FeedPurchasePaymentWrite) Normalize() FeedPurchasePaymentWrite {
	out := w
	out.PaidOn = strings.TrimSpace(w.PaidOn)
	out.Note = strings.Join(strings.Fields(w.Note), " ")
	return out
}

// Validate applies the instalment rules. today is the caller's IST business date: money cannot be
// recorded as handed over on a day that has not happened.
func (w FeedPurchasePaymentWrite) Validate(today time.Time) error {
	paid, err := time.Parse("2006-01-02", w.PaidOn)
	if err != nil {
		return ErrFeedPurchaseValidation{Field: "paid_on", Reason: "must be a date"}
	}
	if paid.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return ErrFeedPurchaseValidation{Field: "paid_on", Reason: "cannot be in the future"}
	}
	if w.AmountRupees <= 0 {
		return ErrFeedPurchaseValidation{Field: "amount_rupees", Reason: "must be more than zero"}
	}
	if len(w.Note) > maxFeedPurchasePaymentNote {
		return ErrFeedPurchaseValidation{Field: "note", Reason: "is too long"}
	}
	return nil
}

// IsFeedPaymentStatus reports whether raw is one of the two payment words, exactly as stored.
func IsFeedPaymentStatus(raw string) bool {
	return raw == FeedPaymentPaid || raw == FeedPaymentPending
}

// NormalizeFeedPaymentStatus canonicalizes a payment word the same way FeedPurchaseWrite.Normalize
// does ("paid"/"PAID" store as the sheet's "Paid"). ok is false for anything outside the closed
// vocabulary, so the caller rejects rather than silently defaulting a money state.
func NormalizeFeedPaymentStatus(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	for _, known := range FeedPaymentStatuses {
		if strings.EqualFold(trimmed, known) {
			return known, true
		}
	}
	return "", false
}

// FeedPurchaseEdit is the edit-purchase form: the values of an already-recorded load.
//
// The load's IDENTITY -- farm, feed, batch number -- is deliberately not editable: those three are
// the natural key the stock cards and the batch counter group by, and "this load is actually a
// different load" is a delete-and-re-record decision, not a field edit. Payment fields are absent
// too: money moves through the instalment ledger and the status edit, never through here.
type FeedPurchaseEdit struct {
	PurchaseDate string
	QuantityKg   float64

	FeedCost      *float64
	TransportCost *float64
	LoadingCost   *float64
	UnloadingCost *float64
	TotalCost     *float64

	Vendor string
}

// Normalize trims the edit before validation, for the same reason FeedPurchaseWrite does.
func (e FeedPurchaseEdit) Normalize() FeedPurchaseEdit {
	out := e
	out.PurchaseDate = strings.TrimSpace(e.PurchaseDate)
	out.Vendor = strings.Join(strings.Fields(e.Vendor), " ")
	return out
}

// asWrite reuses FeedPurchaseWrite's cost rollup and field rules for the fields an edit carries.
func (e FeedPurchaseEdit) asWrite() FeedPurchaseWrite {
	return FeedPurchaseWrite{
		PurchaseDate: e.PurchaseDate, QuantityKg: e.QuantityKg,
		FeedCost: e.FeedCost, TransportCost: e.TransportCost,
		LoadingCost: e.LoadingCost, UnloadingCost: e.UnloadingCost, TotalCost: e.TotalCost,
		Vendor: e.Vendor,
	}
}

// TotalOrSplitSum resolves the landed cost the edit stores, exactly as the record form does.
func (e FeedPurchaseEdit) TotalOrSplitSum() *float64 { return e.asWrite().TotalOrSplitSum() }

// PerKgCost derives the landed rate from the resolved total, exactly as the record form does.
func (e FeedPurchaseEdit) PerKgCost() *float64 { return e.asWrite().PerKgCost() }

// Validate applies the record form's rules to the editable fields. today is the caller's IST
// business date, same as the record form.
func (e FeedPurchaseEdit) Validate(today time.Time) error {
	purchased, err := time.Parse("2006-01-02", e.PurchaseDate)
	if err != nil {
		return ErrFeedPurchaseValidation{Field: "purchase_date", Reason: "must be a date"}
	}
	if purchased.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return ErrFeedPurchaseValidation{Field: "purchase_date", Reason: "cannot be in the future"}
	}
	if e.QuantityKg <= 0 {
		return ErrFeedPurchaseValidation{Field: "quantity_kg", Reason: "must be more than zero"}
	}
	for field, value := range map[string]*float64{
		"feed_cost":      e.FeedCost,
		"transport_cost": e.TransportCost,
		"loading_cost":   e.LoadingCost,
		"unloading_cost": e.UnloadingCost,
		"total_cost":     e.TotalCost,
	} {
		if value != nil && *value < 0 {
			return ErrFeedPurchaseValidation{Field: field, Reason: "cannot be negative"}
		}
	}
	if e.Vendor == "" {
		return ErrFeedPurchaseValidation{Field: "vendor", Reason: "is required"}
	}
	return nil
}

// DeriveFeedPaymentStatus resolves the status an instalment leaves the load in: Paid once the
// released total covers the landed cost, Pending otherwise. When the landed cost is not known the
// current status is kept -- money against an unknown total proves nothing either way.
func DeriveFeedPaymentStatus(totalCost *float64, releasedTotal float64, current string) string {
	if totalCost == nil {
		return current
	}
	// A half-paisa tolerance: the numeric(14,2) column rounds to the paisa, and a status that flips
	// on a rounding artefact would show a fully-paid load as Pending.
	if releasedTotal >= *totalCost-0.005 {
		return FeedPaymentPaid
	}
	return FeedPaymentPending
}

// FeedPurchaseWrite is the record-purchase form.
//
// BatchNo is a POINTER because "not supplied" and "batch 0" are different requests: the farm
// numbers a feed's loads 1, 2, 3... within a farm, so an absent batch number means "this is the
// next load" and the repository assigns max+1 inside the write transaction. An explicitly supplied
// number is honoured, which is how a load recorded out of order (or a correction) is entered.
type FeedPurchaseWrite struct {
	PurchaseDate  string
	FarmLabel     string
	FeedItemLabel string
	BatchNo       *int
	QuantityKg    float64

	FeedCost      *float64
	TransportCost *float64
	LoadingCost   *float64
	UnloadingCost *float64
	TotalCost     *float64

	Vendor          string
	PaymentReleased *float64
	PaymentStatus   string
}

// ErrFeedPurchaseValidation is a field-level rejection carrying operator-readable copy.
type ErrFeedPurchaseValidation struct {
	Field  string
	Reason string
}

func (e ErrFeedPurchaseValidation) Error() string {
	return fmt.Sprintf("procurement: feed purchase %s %s", e.Field, e.Reason)
}

// Normalize trims and canonicalizes the write.
//
// It runs BEFORE Validate so the rules apply to the values that will actually be stored: a vendor
// of "   " must fail the required check, not pass it because it was non-empty before trimming.
func (w FeedPurchaseWrite) Normalize() FeedPurchaseWrite {
	out := w
	out.PurchaseDate = strings.TrimSpace(w.PurchaseDate)
	out.FarmLabel = strings.ToUpper(strings.TrimSpace(w.FarmLabel))
	out.FeedItemLabel = strings.Join(strings.Fields(w.FeedItemLabel), " ")
	out.Vendor = strings.Join(strings.Fields(w.Vendor), " ")
	out.PaymentStatus = strings.TrimSpace(w.PaymentStatus)
	// Title-case the two known payment words so "paid"/"PAID" from a client store as the sheet's
	// form. An unrecognised value still fails Validate rather than being rewritten to a default —
	// a silently defaulted payment state is a money fact nobody entered.
	for _, known := range FeedPaymentStatuses {
		if strings.EqualFold(out.PaymentStatus, known) {
			out.PaymentStatus = known
			break
		}
	}
	return out
}

// TotalOrSplitSum resolves the landed cost the ledger stores: the explicit total when the form
// carried one, otherwise the sum of whichever split parts were entered.
//
// Returns nil when NOTHING was entered — a purchase whose cost is not yet known is a real state
// (the sheet has such rows), and inventing a 0 would report a free load.
func (w FeedPurchaseWrite) TotalOrSplitSum() *float64 {
	if w.TotalCost != nil {
		return w.TotalCost
	}
	sum := 0.0
	any := false
	for _, part := range []*float64{w.FeedCost, w.TransportCost, w.LoadingCost, w.UnloadingCost} {
		if part != nil {
			sum += *part
			any = true
		}
	}
	if !any {
		return nil
	}
	return &sum
}

// PerKgCost derives the landed rate from the resolved total and the quantity.
//
// DERIVED, never entered: the sheet keeps a "Per kg Cost" column that a person maintained by hand,
// and a hand-kept rate drifts from its own total. Nil when there is no total to divide.
func (w FeedPurchaseWrite) PerKgCost() *float64 {
	total := w.TotalOrSplitSum()
	if total == nil || w.QuantityKg <= 0 {
		return nil
	}
	rate := *total / w.QuantityKg
	return &rate
}

// Validate applies the field rules. today is the caller's IST business date: a purchase cannot be
// dated in the future, because stock the farm does not have yet must not deplete a feed sheet.
func (w FeedPurchaseWrite) Validate(today time.Time) error {
	purchased, err := time.Parse("2006-01-02", w.PurchaseDate)
	if err != nil {
		return ErrFeedPurchaseValidation{Field: "purchase_date", Reason: "must be a date"}
	}
	if purchased.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return ErrFeedPurchaseValidation{Field: "purchase_date", Reason: "cannot be in the future"}
	}
	if !IsFeedFarm(w.FarmLabel) {
		return ErrFeedPurchaseValidation{Field: "farm", Reason: "must be CBE or CPT"}
	}
	if w.FeedItemLabel == "" {
		return ErrFeedPurchaseValidation{Field: "feed_item", Reason: "is required"}
	}
	if w.QuantityKg <= 0 {
		return ErrFeedPurchaseValidation{Field: "quantity_kg", Reason: "must be more than zero"}
	}
	if w.BatchNo != nil && *w.BatchNo < 1 {
		return ErrFeedPurchaseValidation{Field: "batch_no", Reason: "must be 1 or more"}
	}
	for field, value := range map[string]*float64{
		"feed_cost":        w.FeedCost,
		"transport_cost":   w.TransportCost,
		"loading_cost":     w.LoadingCost,
		"unloading_cost":   w.UnloadingCost,
		"total_cost":       w.TotalCost,
		"payment_released": w.PaymentReleased,
	} {
		if value != nil && *value < 0 {
			return ErrFeedPurchaseValidation{Field: field, Reason: "cannot be negative"}
		}
	}
	if w.Vendor == "" {
		return ErrFeedPurchaseValidation{Field: "vendor", Reason: "is required"}
	}
	if w.PaymentStatus != FeedPaymentPaid && w.PaymentStatus != FeedPaymentPending {
		return ErrFeedPurchaseValidation{Field: "payment_status", Reason: "must be Paid or Pending"}
	}
	return nil
}
