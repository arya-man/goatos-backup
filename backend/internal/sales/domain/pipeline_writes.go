package domain

// The pipeline and evidence WRITE types: buyer leads, farmer-group (FPO) leads, market quotes,
// sold-tag lists and weight checks, recorded in the app now that the Sales DB sheet is retired
// (maintainer decision 2026-08-18 -- "there will be no sheet in future").
//
// Same conventions as DealWrite: Normalize collapses whitespace BEFORE Validate so the rules apply
// to the stored values; optional text uses "" for "not set" (stored NULL); optional numbers are
// pointers so 0 stays distinct from absent. Call statuses are deliberately FREE TEXT, not an enum:
// the imported sheet rows carry their own spellings ("Not Intrested", "Connected and he will get
// back") and the pipeline charts bucket by exact string, so the server offers the existing
// vocabulary as suggestions (status_options on the list read) rather than forcing a new one that
// would fragment the buckets.

import (
	"fmt"
	"strings"
	"time"
)

// ErrFieldValidation reports a rejected pipeline/evidence write with a field-specific reason.
// Shares the shape of ErrDealValidation; kept separate so the HTTP mapper can label fields per
// form.
type ErrFieldValidation struct {
	Field  string
	Reason string
}

func (e ErrFieldValidation) Error() string {
	return fmt.Sprintf("sales %s: %s", e.Field, e.Reason)
}

func collapseSpace(v string) string { return strings.Join(strings.Fields(v), " ") }

// validDate reports whether v parses as a YYYY-MM-DD business date.
func validDate(v string) bool {
	_, err := time.Parse("2006-01-02", v)
	return err == nil
}

// BuyerLead is one row of the buyer demand pipeline.
type BuyerLead struct {
	LeadID       string
	RecordedDate *string // YYYY-MM-DD when known
	Farm         *string
	BuyerName    string
	BuyerPlace   *string
	AnimalType   *string
	Breed        *string
	CallStatus   *string // nil = not yet called ("uncontacted")
	CreatedAt    string
}

// BuyerLeadWrite records a new buyer lead.
type BuyerLeadWrite struct {
	RecordedDate string // optional
	Farm         string // optional, CBE/CPT when set
	BuyerName    string
	BuyerPlace   string
	AnimalType   string
	Breed        string
	CallStatus   string // optional; "" means not yet called
}

func (w BuyerLeadWrite) Normalize() BuyerLeadWrite {
	out := w
	out.RecordedDate = strings.TrimSpace(w.RecordedDate)
	out.Farm = strings.TrimSpace(w.Farm)
	out.BuyerName = collapseSpace(w.BuyerName)
	out.BuyerPlace = collapseSpace(w.BuyerPlace)
	out.AnimalType = collapseSpace(w.AnimalType)
	out.Breed = collapseSpace(w.Breed)
	out.CallStatus = collapseSpace(w.CallStatus)
	return out
}

func (w BuyerLeadWrite) Validate() error {
	if w.BuyerName == "" {
		return ErrFieldValidation{Field: "buyer_name", Reason: "required"}
	}
	if w.RecordedDate != "" && !validDate(w.RecordedDate) {
		return ErrFieldValidation{Field: "recorded_date", Reason: "must be a date like 2026-08-18"}
	}
	if w.Farm != "" && !IsFarm(w.Farm) {
		return ErrFieldValidation{Field: "farm", Reason: "must be CBE or CPT"}
	}
	for field, v := range map[string]string{
		"buyer_name": w.BuyerName, "buyer_place": w.BuyerPlace,
		"animal_type": w.AnimalType, "breed": w.Breed, "call_status": w.CallStatus,
	} {
		if len(v) > maxDealShortField {
			return ErrFieldValidation{Field: field, Reason: "too long"}
		}
	}
	return nil
}

// FPOLead is one row of the farmer-group pipeline.
type FPOLead struct {
	LeadID     string
	FPOName    string
	Crops      *string
	District   *string
	Taluk      *string
	State      *string
	CallStatus *string
	CreatedAt  string
}

// FPOLeadWrite records a new farmer-group lead.
type FPOLeadWrite struct {
	FPOName    string
	Crops      string
	District   string
	Taluk      string
	State      string
	CallStatus string
}

func (w FPOLeadWrite) Normalize() FPOLeadWrite {
	out := w
	out.FPOName = collapseSpace(w.FPOName)
	out.Crops = collapseSpace(w.Crops)
	out.District = collapseSpace(w.District)
	out.Taluk = collapseSpace(w.Taluk)
	out.State = collapseSpace(w.State)
	out.CallStatus = collapseSpace(w.CallStatus)
	return out
}

func (w FPOLeadWrite) Validate() error {
	if w.FPOName == "" {
		return ErrFieldValidation{Field: "fpo_name", Reason: "required"}
	}
	for field, v := range map[string]string{
		"fpo_name": w.FPOName, "crops": w.Crops, "district": w.District,
		"taluk": w.Taluk, "state": w.State, "call_status": w.CallStatus,
	} {
		if len(v) > maxDealShortField {
			return ErrFieldValidation{Field: field, Reason: "too long"}
		}
	}
	return nil
}

// LeadStatusWrite updates one lead's call status. "" clears it back to "not yet called" -- an
// explicit choice, never a silent default.
type LeadStatusWrite struct {
	CallStatus string
}

func (w LeadStatusWrite) Normalize() LeadStatusWrite {
	return LeadStatusWrite{CallStatus: collapseSpace(w.CallStatus)}
}

func (w LeadStatusWrite) Validate() error {
	if len(w.CallStatus) > maxDealShortField {
		return ErrFieldValidation{Field: "call_status", Reason: "too long"}
	}
	return nil
}

// BenchmarkWrite records one market quote.
type BenchmarkWrite struct {
	Market           string
	Category         string
	Breed            string
	Source           string
	ExFarmRate       string
	TransportRate    string
	LandingCostPerKg *float64
	MarketPricePerKg *float64
}

func (w BenchmarkWrite) Normalize() BenchmarkWrite {
	out := w
	out.Market = collapseSpace(w.Market)
	out.Category = collapseSpace(w.Category)
	out.Breed = collapseSpace(w.Breed)
	out.Source = collapseSpace(w.Source)
	out.ExFarmRate = collapseSpace(w.ExFarmRate)
	out.TransportRate = collapseSpace(w.TransportRate)
	return out
}

func (w BenchmarkWrite) Validate() error {
	if w.Breed == "" {
		return ErrFieldValidation{Field: "breed", Reason: "required"}
	}
	for field, v := range map[string]string{
		"market": w.Market, "category": w.Category, "breed": w.Breed,
		"source": w.Source, "ex_farm_rate": w.ExFarmRate, "transport_rate": w.TransportRate,
	} {
		if len(v) > maxDealShortField {
			return ErrFieldValidation{Field: field, Reason: "too long"}
		}
	}
	for field, v := range map[string]*float64{
		"landing_cost_per_kg": w.LandingCostPerKg, "market_price_per_kg": w.MarketPricePerKg,
	} {
		if v != nil && *v < 0 {
			return ErrFieldValidation{Field: field, Reason: "must not be negative"}
		}
	}
	return nil
}

// SoldTagRow is one animal in a handed-over tag list.
type SoldTagRow struct {
	AnimalLabel string
	TagNumber   string
	WeightKg    *float64
}

// MaxSoldTagRows bounds one tag-list submission. A real handover is tens of animals; the cap stops
// an unbounded batch while never limiting a legitimate one.
const MaxSoldTagRows = 200

// SoldTagsWrite records the tag list handed over at one sale.
type SoldTagsWrite struct {
	Farm string // optional, CBE/CPT when set
	Rows []SoldTagRow
}

func (w SoldTagsWrite) Normalize() SoldTagsWrite {
	out := SoldTagsWrite{Farm: strings.TrimSpace(w.Farm), Rows: make([]SoldTagRow, 0, len(w.Rows))}
	for _, row := range w.Rows {
		out.Rows = append(out.Rows, SoldTagRow{
			AnimalLabel: collapseSpace(row.AnimalLabel),
			TagNumber:   collapseSpace(row.TagNumber),
			WeightKg:    row.WeightKg,
		})
	}
	return out
}

func (w SoldTagsWrite) Validate() error {
	if w.Farm != "" && !IsFarm(w.Farm) {
		return ErrFieldValidation{Field: "farm", Reason: "must be CBE or CPT"}
	}
	if len(w.Rows) == 0 {
		return ErrFieldValidation{Field: "rows", Reason: "add at least one animal"}
	}
	if len(w.Rows) > MaxSoldTagRows {
		return ErrFieldValidation{Field: "rows", Reason: fmt.Sprintf("at most %d animals per list", MaxSoldTagRows)}
	}
	for _, row := range w.Rows {
		if row.AnimalLabel == "" {
			return ErrFieldValidation{Field: "animal_label", Reason: "required on every row"}
		}
		if len(row.AnimalLabel) > maxDealShortField || len(row.TagNumber) > maxDealShortField {
			return ErrFieldValidation{Field: "animal_label", Reason: "too long"}
		}
		if row.WeightKg != nil && *row.WeightKg < 0 {
			return ErrFieldValidation{Field: "weight_kg", Reason: "must not be negative"}
		}
	}
	return nil
}

// WeightCheckWrite records one video-vs-book weight audit row.
type WeightCheckWrite struct {
	TagNumber     string
	BookWeightKg  float64
	VideoWeightKg float64
	FarmBorn      bool
}

func (w WeightCheckWrite) Normalize() WeightCheckWrite {
	out := w
	out.TagNumber = collapseSpace(w.TagNumber)
	return out
}

func (w WeightCheckWrite) Validate() error {
	if len(w.TagNumber) > maxDealShortField {
		return ErrFieldValidation{Field: "tag_number", Reason: "too long"}
	}
	if w.BookWeightKg <= 0 {
		return ErrFieldValidation{Field: "book_weight_kg", Reason: "must be more than zero"}
	}
	if w.VideoWeightKg <= 0 {
		return ErrFieldValidation{Field: "video_weight_kg", Reason: "must be more than zero"}
	}
	return nil
}

// Lead page bounds -- the pipeline lists are authored records of a few hundred rows.
const (
	DefaultLeadPageSize = 20
	MaxLeadPageSize     = 100
	MaxLeadOffset       = 10000
)

// ClampLeadPageSize resolves a requested lead-page size to a supported one.
func ClampLeadPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultLeadPageSize
	case requested > MaxLeadPageSize:
		return MaxLeadPageSize
	default:
		return requested
	}
}
