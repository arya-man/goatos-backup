package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Vendor is one row of the procurement vendor register: a counterparty the farm buys from or
// contracts with. The register spans livestock agents/stockists, transport, feed, manure, pellet
// factories, labour, insurance, test labs and site trades -- 22 record types in the imported data.
//
// It is NOT a party in the `orgs`/`parties` sense. See migration 000156 for why the register owns
// its own table; PartyID is the optional back-link for when a vendor first actually transacts.
type Vendor struct {
	VendorID string
	TenantID string

	RecordType        string
	BusinessName      string
	ContactPersonName *string
	PhoneNumber       *string

	Breed             *string
	Feed              *string
	Status            string
	FilteredStock     *int
	PricePerGoat      *string // decimal carried as string so money never round-trips through float64
	ReadyToFiltered   *string
	ETAAfterOrderDays *int
	Details           *string

	State string
	City  *string

	// Finance fields. Populated on a caller's read ONLY when they hold VendorFinanceRead;
	// otherwise every one of these is nil and FinanceRedacted is true. See RedactFinance.
	BankName  *string
	AccountNo *string
	IFSCCode  *string
	UPIID     *string
	PANNumber *string
	// FinanceRedacted reports that finance fields were withheld rather than absent, so the UI can
	// say "hidden" instead of rendering a misleading blank.
	FinanceRedacted bool

	Comments *string
	PartyID  *string

	// Capacity (maintainer decision 2026-09-03): how much this vendor can supply per delivery and
	// how often. Quantity is a decimal carried as a string, like PricePerGoat, and travels with its
	// unit -- nil quantity means "not recorded", never zero.
	CapacityQuantity *string
	CapacityUnit     *string
	SupplyFrequency  *string
	// VoiceNoteProofRef is the audio note recorded on the phone (a completed 'audio' proof in
	// this tenant), nil when none was recorded.
	VoiceNoteProofRef *string

	SourceRow *int

	CreatedAt  string
	UpdatedAt  string
	RowVersion int64
}

// Vendor status values. These are the CHECK-constrained storage forms, all lower case.
//
// The source sheet stores title case with stray trailing spaces ("Active", "In Active ",
// "Negotiating "), which the database rejects outright -- so NormalizeStatus is not cosmetic, it is
// the difference between the import loading and failing.
const (
	VendorStatusActive      = "active"
	VendorStatusInactive    = "inactive"
	VendorStatusNegotiating = "negotiating"
	// VendorStatusBanned has no rows in the imported data, but the sheet's Validation tab offered it
	// and it is the register's only way to record "never buy here again". Dropping it on import would
	// have silently removed that capability.
	VendorStatusBanned = "banned"
)

// Catalog kinds -- the business-managed dropdown vocabularies in procurement_vendor_catalog.
const (
	CatalogKindRecordType = "record_type"
	CatalogKindBreed      = "breed"
	CatalogKindState      = "state"
	CatalogKindCity       = "city"
	CatalogKindStatus     = "status"
	CatalogKindFeed       = "feed"
	// CatalogKindCapacityUnit / CatalogKindSupplyFrequency (maintainer decision 2026-09-03): what a
	// vendor's capacity is counted in, and how often it is available. Stable VALUES the app stores
	// (kg, per_2_weeks); the LABEL is what screens render.
	CatalogKindCapacityUnit    = "capacity_unit"
	CatalogKindSupplyFrequency = "supply_frequency"
)

// VendorCatalogKinds is the closed set of vocabulary kinds, mirroring the CHECK on
// procurement_vendor_catalog.kind.
var VendorCatalogKinds = []string{
	CatalogKindRecordType, CatalogKindBreed, CatalogKindState,
	CatalogKindCity, CatalogKindStatus, CatalogKindFeed,
	CatalogKindCapacityUnit,
	CatalogKindSupplyFrequency,
}

// The two sides of the vendor register (maintainer decision 2026-09-05). A record_type belongs to
// exactly one of them, and each side is a page: Procurement > Vendors and Sales > Vendors.
//
// The side lives on the CATALOG row, not on the vendor: a vendor's side is implied entirely by its
// record type, and storing it twice would let the two disagree. See migration 000256.
const (
	VendorSideProcurement = "procurement"
	VendorSideSales       = "sales"
)

// VendorSides is the closed set, mirroring the CHECK on procurement_vendor_catalog.register_side.
var VendorSides = []string{VendorSideProcurement, VendorSideSales}

// NormalizeVendorSide maps a requested side onto its storage form, reporting whether it is one.
//
// An EMPTY side is valid and means "both": a caller that names no side reads the whole register,
// which is what every reader did before the split and what the vendor picklist still does. A
// caller that names an UNKNOWN side is refused rather than silently widened to both -- quietly
// serving the whole register to a page that asked for one half would put buyers on the buying
// desk's screen, which is the defect the split exists to fix.
func NormalizeVendorSide(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return "", true
	case VendorSideProcurement:
		return VendorSideProcurement, true
	case VendorSideSales:
		return VendorSideSales, true
	default:
		return "", false
	}
}

// VendorCatalogEntry is one selectable option in a register dropdown.
type VendorCatalogEntry struct {
	Kind      string
	Value     string
	Label     string
	SortOrder int
	IsActive  bool
	// RegisterSide is which register offers this entry. It is meaningful only for
	// CatalogKindRecordType; every other kind carries the inert storage default and readers ignore
	// it. A record_type entry is offered by the page whose side it names, and by no other.
	RegisterSide string
}

// Field length caps. These bound what a write may store so a pasted document cannot become a
// vendor name. They are generous relative to real data (the longest imported business name is well
// under 100 characters) and exist to stop abuse, not to police legitimate input.
const (
	maxVendorShortField  = 160
	maxVendorDetailField = 2000
	// A phone field may legitimately hold several numbers separated by a slash in this data.
	maxVendorPhoneField = 64
)

// NormalizeStatus maps a human/sheet status spelling onto its storage form.
//
// It collapses internal whitespace before matching, which is what makes the sheet's "In Active "
// resolve to "inactive": the space is not incidental formatting to be trimmed at the edges, it sits
// in the middle of the token.
func NormalizeStatus(raw string) (string, bool) {
	key := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	switch key {
	case "active":
		return VendorStatusActive, true
	case "inactive", "in active", "in-active":
		return VendorStatusInactive, true
	case "negotiating":
		return VendorStatusNegotiating, true
	case "banned":
		return VendorStatusBanned, true
	default:
		return "", false
	}
}

var nonDigits = regexp.MustCompile(`\D`)

// NormalizePhoneDigits reduces a phone to digits only.
//
// This mirrors the digits-only comparison inside procurement_vendors_natural_uq exactly. The
// duplicate rule is "the same person at the same business in the same state", and the imported data
// spells one number as "97550 44183" and others without the space -- so a formatting difference
// must not be allowed to defeat the constraint. Any caller doing a pre-write duplicate check must
// use THIS function, or its answer will disagree with the database's.
func NormalizePhoneDigits(raw string) string {
	return nonDigits.ReplaceAllString(raw, "")
}

// NormalizeVendorText collapses whitespace and trims. Applied to every short text field on write so
// that " Goat  Wala " and "Goat Wala" are the same vendor to the natural key.
func NormalizeVendorText(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// VendorWrite is the validated payload for a create or update.
//
// Every field is a value rather than a pointer, with empty string meaning "not set", because this
// crosses an HTTP boundary where a caller omitting a field and a caller clearing it must both
// resolve to the same stored NULL. An update replaces the whole row; there is no partial-patch
// semantics to get wrong.
type VendorWrite struct {
	RecordType        string
	BusinessName      string
	ContactPersonName string
	PhoneNumber       string
	Breed             string
	Feed              string
	Status            string
	FilteredStock     *int
	PricePerGoat      *string
	ReadyToFiltered   string
	ETAAfterOrderDays *int
	Details           string
	State             string
	City              string
	BankName          string
	AccountNo         string
	IFSCCode          string
	UPIID             string
	PANNumber         string
	Comments          string

	// CapacityQuantity is a decimal string (up to three places); nil means not recorded. Unit and
	// frequency are catalog VALUES; "" means not recorded. VoiceNoteProofRef is a proof id or "".
	CapacityQuantity  *string
	CapacityUnit      string
	SupplyFrequency   string
	VoiceNoteProofRef string
}

// ErrVendorValidation reports a rejected write with a field-specific, operator-readable reason.
type ErrVendorValidation struct {
	Field  string
	Reason string
}

func (e ErrVendorValidation) Error() string {
	return fmt.Sprintf("vendor %s: %s", e.Field, e.Reason)
}

// Normalize cleans every field in place and returns the normalized copy. It does NOT validate;
// call Validate after. Splitting the two keeps the importer able to normalize a sheet row and
// report precisely which normalized value then failed.
func (w VendorWrite) Normalize() VendorWrite {
	out := w
	out.RecordType = NormalizeVendorText(w.RecordType)
	out.BusinessName = NormalizeVendorText(w.BusinessName)
	out.ContactPersonName = NormalizeVendorText(w.ContactPersonName)
	out.PhoneNumber = NormalizeVendorText(w.PhoneNumber)
	out.Breed = NormalizeVendorText(w.Breed)
	out.Feed = NormalizeVendorText(w.Feed)
	out.ReadyToFiltered = NormalizeVendorText(w.ReadyToFiltered)
	out.State = NormalizeVendorText(w.State)
	out.City = NormalizeVendorText(w.City)
	out.BankName = NormalizeVendorText(w.BankName)
	out.AccountNo = NormalizeVendorText(w.AccountNo)
	out.IFSCCode = strings.ToUpper(NormalizeVendorText(w.IFSCCode))
	out.UPIID = NormalizeVendorText(w.UPIID)
	out.PANNumber = strings.ToUpper(NormalizeVendorText(w.PANNumber))
	// Details and Comments are free prose: trim the edges but keep the author's line breaks.
	out.Details = strings.TrimSpace(w.Details)
	out.Comments = strings.TrimSpace(w.Comments)
	out.CapacityUnit = NormalizeVendorText(w.CapacityUnit)
	out.SupplyFrequency = NormalizeVendorText(w.SupplyFrequency)
	out.VoiceNoteProofRef = strings.TrimSpace(w.VoiceNoteProofRef)
	if w.CapacityQuantity != nil {
		q := strings.TrimSpace(*w.CapacityQuantity)
		if q == "" {
			out.CapacityQuantity = nil
		} else {
			out.CapacityQuantity = &q
		}
	}

	if status, ok := NormalizeStatus(w.Status); ok {
		out.Status = status
	} else {
		out.Status = NormalizeVendorText(strings.ToLower(w.Status))
	}
	return out
}

// ValidateForCreate enforces everything Validate does PLUS the three fields the Slack intake flow
// has always demanded of a new vendor: contact person, phone number and city.
//
// It is deliberately separate from Validate, which the UPDATE path uses, because 52 of the 306
// imported rows are missing at least one of them (51 have no contact person, 30 no city, 1 no
// phone). Enforcing these on update would make those rows UNEDITABLE -- someone correcting a typo
// in a business name would first have to invent a contact person the farm may not know. So the rule
// binds NEW data, where it costs nothing and matches the Slack questionnaire, and leaves the legacy
// backlog fixable.
//
// Phone earns its place beyond parity: it is part of procurement_vendors_natural_uq, so it is what
// keeps two contacts at the same business distinguishable. Two phone-less rows for one business
// collide, and the second is refused as a duplicate.
func (w VendorWrite) ValidateForCreate() error {
	if err := w.Validate(); err != nil {
		return err
	}
	if w.ContactPersonName == "" {
		return ErrVendorValidation{Field: "contact_person_name", Reason: "required"}
	}
	if w.PhoneNumber == "" {
		return ErrVendorValidation{Field: "phone_number", Reason: "required"}
	}
	if w.City == "" {
		return ErrVendorValidation{Field: "city", Reason: "required"}
	}
	return nil
}

// Validate enforces the required fields and bounds, returning the FIRST failure.
//
// It deliberately does not check record_type / breed / state / city / feed against the catalog.
// That vocabulary is business-managed data that grows without a deploy, and a stale in-process copy
// rejecting a value an admin just added would be worse than accepting an unknown one. The database
// enforces what must never vary (status, non-blank required fields, non-negative numbers).
func (w VendorWrite) Validate() error {
	if w.BusinessName == "" {
		return ErrVendorValidation{Field: "business_name", Reason: "required"}
	}
	if len(w.BusinessName) > maxVendorShortField {
		return ErrVendorValidation{Field: "business_name", Reason: "too long"}
	}
	if w.RecordType == "" {
		return ErrVendorValidation{Field: "record_type", Reason: "required"}
	}
	if len(w.RecordType) > maxVendorShortField {
		return ErrVendorValidation{Field: "record_type", Reason: "too long"}
	}
	if w.State == "" {
		return ErrVendorValidation{Field: "state", Reason: "required"}
	}
	if len(w.State) > maxVendorShortField {
		return ErrVendorValidation{Field: "state", Reason: "too long"}
	}
	if _, ok := NormalizeStatus(w.Status); !ok {
		return ErrVendorValidation{Field: "status", Reason: "must be one of active, inactive, negotiating, banned"}
	}
	if len(w.PhoneNumber) > maxVendorPhoneField {
		return ErrVendorValidation{Field: "phone_number", Reason: "too long"}
	}
	for field, value := range map[string]string{
		"contact_person_name": w.ContactPersonName,
		"breed":               w.Breed,
		"feed":                w.Feed,
		"ready_to_filtered":   w.ReadyToFiltered,
		"city":                w.City,
		"bank_name":           w.BankName,
		"account_no":          w.AccountNo,
		"ifsc_code":           w.IFSCCode,
		"upi_id":              w.UPIID,
		"pan_number":          w.PANNumber,
	} {
		if len(value) > maxVendorShortField {
			return ErrVendorValidation{Field: field, Reason: "too long"}
		}
	}
	if len(w.Details) > maxVendorDetailField {
		return ErrVendorValidation{Field: "details", Reason: "too long"}
	}
	if len(w.Comments) > maxVendorDetailField {
		return ErrVendorValidation{Field: "comments", Reason: "too long"}
	}
	// Non-negative rather than positive: a filtered stock of 0 is a real, meaningful reading, and
	// an ETA of 0 days means "ready now".
	if w.FilteredStock != nil && *w.FilteredStock < 0 {
		return ErrVendorValidation{Field: "filtered_stock", Reason: "must not be negative"}
	}
	if w.ETAAfterOrderDays != nil && *w.ETAAfterOrderDays < 0 {
		return ErrVendorValidation{Field: "eta_after_order_days", Reason: "must not be negative"}
	}
	if w.PricePerGoat != nil {
		if !decimalPattern.MatchString(*w.PricePerGoat) {
			return ErrVendorValidation{Field: "price_per_goat", Reason: "must be a non-negative amount"}
		}
	}
	// Capacity is OPTIONAL in every part (maintainer instruction 2026-09-03: "keep capacity as
	// optional only"): quantity, unit and frequency are each recorded when given and never
	// required together. A quantity that IS entered must still be a real positive amount; that
	// is a shape check on typed input, not a required-ness rule.
	if w.CapacityQuantity != nil {
		if !capacityPattern.MatchString(*w.CapacityQuantity) || *w.CapacityQuantity == "0" {
			return ErrVendorValidation{Field: "capacity_quantity", Reason: "must be more than zero"}
		}
	}
	for field, value := range map[string]string{
		"capacity_unit":    w.CapacityUnit,
		"supply_frequency": w.SupplyFrequency,
	} {
		if len(value) > maxVendorShortField {
			return ErrVendorValidation{Field: field, Reason: "too long"}
		}
	}
	if w.VoiceNoteProofRef != "" && !uuidPattern.MatchString(w.VoiceNoteProofRef) {
		return ErrVendorValidation{Field: "voice_note_proof_ref", Reason: "must be a proof id"}
	}
	return nil
}

// capacityPattern accepts a positive amount with up to three decimal places (numeric(14,3)). Zero
// is refused separately: "can supply nothing" is not a capacity worth recording.
var capacityPattern = regexp.MustCompile(`^\d{1,11}(\.\d{1,3})?$`)

// uuidPattern is the shape a proof id must have before the proof store is even asked.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CapacityDisplay composes the ONE capacity sentence every surface renders ("5,000 kg · Every 2
// weeks"), from the stored values and the catalog labels the caller resolved. Backend-owned so the
// phone and the web cannot phrase the same fact two ways. Empty when no capacity is recorded; a
// frequency with no quantity still reads ("Every 2 weeks") because it is a real fact on its own,
// and a quantity with no unit reads as the bare number ("5,000") since every part is optional.
func (v Vendor) CapacityDisplay(unitLabel, frequencyLabel string) string {
	parts := make([]string, 0, 2)
	if v.CapacityQuantity != nil {
		unit := strings.TrimSpace(unitLabel)
		if unit == "" && v.CapacityUnit != nil {
			unit = *v.CapacityUnit
		}
		parts = append(parts, strings.TrimSpace(formatCapacityQuantity(*v.CapacityQuantity)+" "+unit))
	}
	if v.SupplyFrequency != nil && *v.SupplyFrequency != "" {
		freq := strings.TrimSpace(frequencyLabel)
		if freq == "" {
			freq = *v.SupplyFrequency
		}
		parts = append(parts, freq)
	}
	return strings.Join(parts, " · ")
}

// formatCapacityQuantity renders a decimal string with Indian digit grouping and no trailing
// zeros: "5000.000" -> "5,000", "2.500" -> "2.5".
func formatCapacityQuantity(raw string) string {
	whole, frac := raw, ""
	if i := strings.IndexByte(raw, '.'); i >= 0 {
		whole, frac = raw[:i], strings.TrimRight(raw[i+1:], "0")
	}
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
		whole = strings.Join(append(groups, tail), ",")
	}
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// decimalPattern accepts a non-negative amount with up to two decimal places, matching
// numeric(12,2) storage. A leading '-' is rejected here rather than relying on the CHECK so the
// caller gets a field-specific message instead of a constraint violation.
var decimalPattern = regexp.MustCompile(`^\d{1,10}(\.\d{1,2})?$`)

// RedactFinance strips every payment instrument from a vendor and marks it redacted.
//
// Called on the read path for any caller lacking VendorFinanceRead. It clears the fields rather
// than never selecting them so there is exactly ONE place the rule lives -- a second SELECT variant
// would be a second thing to forget to update when a finance column is added.
func (v Vendor) RedactFinance() Vendor {
	v.BankName = nil
	v.AccountNo = nil
	v.IFSCCode = nil
	v.UPIID = nil
	v.PANNumber = nil
	v.FinanceRedacted = true
	return v
}

// HasFinanceDetails reports whether any payment instrument is recorded. It reads the UNREDACTED
// row, so it must be evaluated before RedactFinance if a caller wants to show "banking on file"
// without showing the values.
func (v Vendor) HasFinanceDetails() bool {
	for _, f := range []*string{v.BankName, v.AccountNo, v.IFSCCode, v.UPIID, v.PANNumber} {
		if f != nil && strings.TrimSpace(*f) != "" {
			return true
		}
	}
	return false
}

// VendorFilter is the whole-filter scope of a list read. Every field is optional; the zero value
// lists the register unfiltered.
type VendorFilter struct {
	Search     string
	RecordType string
	Status     string
	State      string
	City       string
	Breed      string
	// Side narrows the register to one of its two halves by the record type's declared side. Empty
	// reads both, which is what the whole register meant before the split. See NormalizeVendorSide.
	Side string
}

// MaxVendorSearchLength bounds the search term. A trigram index degrades on very long inputs and no
// legitimate vendor search is near this long.
const MaxVendorSearchLength = 120

// Normalize lower-cases and collapses the filter so it matches the generated search_text column and
// the stored facet values.
func (f VendorFilter) Normalize() VendorFilter {
	out := f
	out.Search = strings.ToLower(NormalizeVendorText(f.Search))
	if len(out.Search) > MaxVendorSearchLength {
		out.Search = out.Search[:MaxVendorSearchLength]
	}
	out.RecordType = NormalizeVendorText(f.RecordType)
	out.State = NormalizeVendorText(f.State)
	out.City = NormalizeVendorText(f.City)
	out.Breed = NormalizeVendorText(f.Breed)
	if status, ok := NormalizeStatus(f.Status); ok {
		out.Status = status
	} else {
		out.Status = ""
	}
	// An unrecognised side normalizes to empty here, matching how Status behaves. The API layer
	// REJECTS an unknown side before it reaches this point (see NormalizeVendorSide); this is the
	// second line of defence for a caller that builds a filter directly.
	if side, ok := NormalizeVendorSide(f.Side); ok {
		out.Side = side
	} else {
		out.Side = ""
	}
	return out
}

// VendorPageSize bounds one page of the register.
//
// The list is a desktop table rather than a phone list, so the page is larger than the ~20 the
// mobile rule mandates; it is still a bounded single screen-page, never the whole register.
const (
	DefaultVendorPageSize = 25
	MaxVendorPageSize     = 100
)

// MaxVendorOffset bounds how deep the register can be paged.
//
// The bound is what keeps LIMIT/OFFSET legitimate here: an offset that GROWS without limit is the
// banned anti-pattern, because the database must walk and discard every skipped row. This register
// is an authored contact book of a few hundred rows that does not grow with the herd, so a hard
// ceiling makes the cost bounded by construction. Beyond it a caller should be filtering, not
// paging, and the service rejects the request rather than silently clamping -- a silently clamped
// page shows page 1's rows under page 400's number.
const MaxVendorOffset = 10000

// ClampVendorPageSize resolves a requested page size to a supported one. A non-positive request
// takes the default rather than erroring, so a client that omits the parameter still works.
func ClampVendorPageSize(requested int) int {
	switch {
	case requested <= 0:
		return DefaultVendorPageSize
	case requested > MaxVendorPageSize:
		return MaxVendorPageSize
	default:
		return requested
	}
}

// ---------------------------------------------------------------------------------------------
// Vendor picklist (maintainer decision 2026-08-27: every sale names its buyer from the register)
// ---------------------------------------------------------------------------------------------

// VendorOption is one selectable counterparty: the minimum a picker needs to identify a vendor and
// nothing more. Deliberately NOT domain.Vendor -- a picklist must never be a path to the payment
// instruments VendorFinanceRead guards, and shipping the full row to every dropdown would make it
// one.
type VendorOption struct {
	VendorID     string
	BusinessName string
	RecordType   string
	City         string
	State        string
}

// MaxVendorOptions bounds the picklist in ONE read.
//
// The register is a contact book -- 307 rows at import, growing by a handful a month -- so the
// active set fits comfortably inside this cap and the read stays a single bounded query rather
// than the paged full-walk that admin-web-request-reads-guard exists to ban. The cap is enforced
// in SQL and reported back through VendorOptions.Truncated, so a register that ever outgrows it
// says so instead of silently offering a partial list of buyers.
const MaxVendorOptions = 1000

// VendorOptions is the bounded picklist plus an honest statement of whether it is complete.
type VendorOptions struct {
	Vendors []VendorOption
	// Truncated is true when the active register holds more vendors than MaxVendorOptions, so a
	// picker can tell the person to search the Vendors page instead of implying the buyer they
	// cannot find does not exist.
	Truncated bool
}
