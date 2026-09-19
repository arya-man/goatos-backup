package http

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

// vendorPayload is the wire shape of one register row.
//
// Field names here, in contracts/openapi/app-api.yaml, and in the generated TypeScript client must
// move together. A rename on one side only is the "contract lie" failure mode: the client compiles,
// renders nothing, and nothing errors.
type vendorPayload struct {
	VendorID     string `json:"vendor_id"`
	RecordType   string `json:"record_type"`
	BusinessName string `json:"business_name"`
	// display_name is composed by the BACKEND so every surface renders a vendor identically.
	// A contact-less vendor is just its business name; with a contact it reads
	// "Bhopal Goat And Agro - Sammer". Composing this client-side is how two screens start
	// disagreeing about what the same vendor is called.
	DisplayName       string  `json:"display_name"`
	ContactPersonName *string `json:"contact_person_name"`
	PhoneNumber       *string `json:"phone_number"`

	Breed             *string `json:"breed"`
	Feed              *string `json:"feed"`
	Status            string  `json:"status"`
	StatusLabel       string  `json:"status_label"`
	FilteredStock     *int    `json:"filtered_stock"`
	PricePerGoat      *string `json:"price_per_goat"`
	ReadyToFiltered   *string `json:"ready_to_filtered"`
	ETAAfterOrderDays *int    `json:"eta_after_order_days"`
	Details           *string `json:"details"`

	State    string  `json:"state"`
	City     *string `json:"city"`
	Location string  `json:"location_display"`

	BankName  *string `json:"bank_name"`
	AccountNo *string `json:"account_no"`
	IFSCCode  *string `json:"ifsc_code"`
	UPIID     *string `json:"upi_id"`
	PANNumber *string `json:"pan_number"`
	// finance_redacted tells the client the payment fields were WITHHELD rather than empty, so it
	// can render "Hidden" instead of a blank that reads as "this vendor has no bank details".
	FinanceRedacted bool `json:"finance_redacted"`

	Comments *string `json:"comments"`

	// Capacity (maintainer decision 2026-09-03). capacity_display is BACKEND-composed from the
	// quantity, the unit label and the frequency label ("5,000 kg · Every 2 weeks"), empty when
	// nothing is recorded; clients render it verbatim and use the raw fields only to prefill a form.
	CapacityQuantity  *string `json:"capacity_quantity"`
	CapacityUnit      *string `json:"capacity_unit"`
	SupplyFrequency   *string `json:"supply_frequency"`
	CapacityDisplay   string  `json:"capacity_display"`
	VoiceNoteProofRef *string `json:"voice_note_proof_ref"`

	// Average animal weight (maintainer decision 2026-09-08): the live weight per animal this
	// buyer expects, in kg. It is a plain recorded value connected to nothing else.
	// average_animal_weight_display is BACKEND-composed ("35 kg"), empty when not recorded;
	// clients render it verbatim and use the raw field only to prefill a form.
	AverageAnimalWeightKg      *string `json:"average_animal_weight_kg"`
	AverageAnimalWeightDisplay string  `json:"average_animal_weight_display"`

	// VENDOR FORM IS AUTHORED (2026-09-19): answers keyed by question id for the questions the
	// published `sales.vendor` form added beyond the register's columns, the form version they
	// were answered on (null before the form existed), and -- on the single-vendor reads -- the
	// same answers labelled by that form's question titles, in form order, for a detail screen.
	Answers              map[string]string        `json:"answers"`
	QuestionnaireVersion *int                     `json:"questionnaire_version"`
	AnswerRows           []vendorAnswerRowPayload `json:"answer_rows,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	// row_version must be echoed back on update. It is the optimistic fence that stops two editors
	// silently overwriting each other.
	RowVersion int64 `json:"row_version"`
}

type vendorListPayload struct {
	Vendors []vendorPayload `json:"vendors"`
	// total is the whole-filter count, never the page length. With limit and offset echoed back, the
	// client can render "Page 2 of 13" and a working Back control without inventing its own state.
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type vendorCatalogEntryPayload struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// is_active false means the entry still renders on vendors that carry it but must not be
	// offered for new rows.
	IsActive bool `json:"is_active"`
}

type vendorCatalogPayload struct {
	RecordTypes []vendorCatalogEntryPayload `json:"record_types"`
	Breeds      []vendorCatalogEntryPayload `json:"breeds"`
	States      []vendorCatalogEntryPayload `json:"states"`
	Cities      []vendorCatalogEntryPayload `json:"cities"`
	Statuses    []vendorCatalogEntryPayload `json:"statuses"`
	Feeds       []vendorCatalogEntryPayload `json:"feeds"`
	// Capacity vocabularies (maintainer decision 2026-09-03): what a capacity is counted in and how
	// often it is available. Values are stored, labels are rendered.
	CapacityUnits     []vendorCatalogEntryPayload `json:"capacity_units"`
	SupplyFrequencies []vendorCatalogEntryPayload `json:"supply_frequencies"`
}

// vendorWritePayload is the create/update body.
//
// Every optional field is a plain string rather than a pointer: an update REPLACES the row, so an
// omitted field and a cleared field must mean the same thing. Pointers would introduce a
// patch-vs-replace distinction the register does not have and that clients would get wrong.
type vendorWritePayload struct {
	RecordType        string  `json:"record_type"`
	BusinessName      string  `json:"business_name"`
	ContactPersonName string  `json:"contact_person_name"`
	PhoneNumber       string  `json:"phone_number"`
	Breed             string  `json:"breed"`
	Feed              string  `json:"feed"`
	Status            string  `json:"status"`
	FilteredStock     *int    `json:"filtered_stock"`
	PricePerGoat      *string `json:"price_per_goat"`
	ReadyToFiltered   string  `json:"ready_to_filtered"`
	ETAAfterOrderDays *int    `json:"eta_after_order_days"`
	Details           string  `json:"details"`
	State             string  `json:"state"`
	City              string  `json:"city"`
	BankName          string  `json:"bank_name"`
	AccountNo         string  `json:"account_no"`
	IFSCCode          string  `json:"ifsc_code"`
	UPIID             string  `json:"upi_id"`
	PANNumber         string  `json:"pan_number"`
	Comments          string  `json:"comments"`
	// Capacity: quantity as a decimal string (null = not recorded) with its unit, plus how often;
	// the audio note's proof id. All optional; quantity and unit must travel together.
	CapacityQuantity  *string `json:"capacity_quantity"`
	CapacityUnit      string  `json:"capacity_unit"`
	SupplyFrequency   string  `json:"supply_frequency"`
	VoiceNoteProofRef string  `json:"voice_note_proof_ref"`
	// Average animal weight in kg as a decimal string; null or "" = not recorded. Optional.
	AverageAnimalWeightKg *string `json:"average_animal_weight_kg"`
	// answers + questionnaire_version: a form-driven client sends EVERY answer (typed questions
	// included) keyed by question id, and the version of the form it rendered. Absent answers
	// mean a typed-only client; stored extra answers are then preserved on update.
	Answers              map[string]string `json:"answers"`
	QuestionnaireVersion int               `json:"questionnaire_version"`
	// row_version is required on update and ignored on create.
	RowVersion int64 `json:"row_version"`
}

type vendorAnswerRowPayload struct {
	QuestionID string `json:"question_id"`
	Label      string `json:"label"`
	Value      string `json:"value"`
}

// vendorFormPayload is the published vendor form: pages of questions, catalog choices filled.
type vendorFormPayload struct {
	Version int                     `json:"version"`
	Pages   []vendorFormPagePayload `json:"pages"`
}

type vendorFormPagePayload struct {
	Key       string                  `json:"key"`
	Title     string                  `json:"title"`
	Hint      string                  `json:"hint,omitempty"`
	Questions []vendorQuestionPayload `json:"questions"`
}

type vendorQuestionPayload struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Hint     string `json:"hint,omitempty"`
	Required bool   `json:"required"`
	// typed is true for a question the register stores in its own column: the client sends its
	// answer under the same id and reads it back from the typed vendor field.
	Typed      bool                         `json:"typed"`
	Options    []vendorQuestionOptPayload   `json:"options,omitempty"`
	AllowOther bool                         `json:"allow_other,omitempty"`
	Min        *float64                     `json:"min,omitempty"`
	Max        *float64                     `json:"max,omitempty"`
	Unit       string                       `json:"unit,omitempty"`
	OnlyIf     *vendorQuestionOnlyIfPayload `json:"only_if,omitempty"`
}

type vendorQuestionOptPayload struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type vendorQuestionOnlyIfPayload struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

func toVendorFormPayload(f domain.VendorForm) vendorFormPayload {
	out := vendorFormPayload{Version: f.Version, Pages: make([]vendorFormPagePayload, 0, len(f.Pages))}
	for _, p := range f.Pages {
		page := vendorFormPagePayload{Key: p.Key, Title: p.Title, Hint: p.Hint, Questions: make([]vendorQuestionPayload, 0, len(p.Questions))}
		for _, q := range p.Questions {
			qp := vendorQuestionPayload{
				ID: q.ID, Kind: q.Kind, Title: q.Title, Hint: q.Hint, Required: q.Required,
				Typed: domain.IsTypedVendorQuestion(q.ID), AllowOther: q.AllowOther,
				Min: q.Min, Max: q.Max, Unit: q.Unit, Options: []vendorQuestionOptPayload{},
			}
			for _, o := range q.Options {
				qp.Options = append(qp.Options, vendorQuestionOptPayload{Value: o.Value, Label: o.Label})
			}
			if q.OnlyIf != nil {
				qp.OnlyIf = &vendorQuestionOnlyIfPayload{QuestionID: q.OnlyIf.QuestionID, Value: q.OnlyIf.Value}
			}
			page.Questions = append(page.Questions, qp)
		}
		out.Pages = append(out.Pages, page)
	}
	return out
}

// vendorStatusPayload is the status-only change body.
type vendorStatusPayload struct {
	Status string `json:"status"`
	// row_version is required: a status flip is still a write that can lose a race.
	RowVersion int64 `json:"row_version"`
}

func (p vendorWritePayload) toDomain() domain.VendorWrite {
	return domain.VendorWrite{
		RecordType: p.RecordType, BusinessName: p.BusinessName,
		ContactPersonName: p.ContactPersonName, PhoneNumber: p.PhoneNumber,
		Breed: p.Breed, Feed: p.Feed, Status: p.Status,
		FilteredStock: p.FilteredStock, PricePerGoat: p.PricePerGoat,
		ReadyToFiltered: p.ReadyToFiltered, ETAAfterOrderDays: p.ETAAfterOrderDays,
		Details: p.Details, State: p.State, City: p.City,
		BankName: p.BankName, AccountNo: p.AccountNo, IFSCCode: p.IFSCCode,
		UPIID: p.UPIID, PANNumber: p.PANNumber, Comments: p.Comments,
		CapacityQuantity: p.CapacityQuantity, CapacityUnit: p.CapacityUnit,
		SupplyFrequency: p.SupplyFrequency, VoiceNoteProofRef: p.VoiceNoteProofRef,
		AverageAnimalWeightKg: p.AverageAnimalWeightKg,
		SOPAnswers:            p.Answers,
		QuestionnaireVersion:  p.QuestionnaireVersion,
	}
}

// catalogLabels resolves catalog VALUES to their LABELS per kind, for the backend-composed lines.
type catalogLabels map[string]map[string]string

func (c catalogLabels) label(kind, value string) string {
	if c == nil {
		return ""
	}
	return c[kind][value]
}

func toVendorPayload(v domain.Vendor, labels catalogLabels) vendorPayload {
	return vendorPayload{
		VendorID:             v.VendorID,
		RecordType:           v.RecordType,
		BusinessName:         v.BusinessName,
		Answers:              vendorAnswersOrEmpty(v.SOPAnswers),
		QuestionnaireVersion: v.QuestionnaireVersion,
		DisplayName:          vendorDisplayName(v),
		ContactPersonName:    v.ContactPersonName,
		PhoneNumber:          v.PhoneNumber,
		Breed:                v.Breed,
		Feed:                 v.Feed,
		Status:               v.Status,
		StatusLabel:          vendorStatusLabel(v.Status),
		FilteredStock:        v.FilteredStock,
		PricePerGoat:         v.PricePerGoat,
		ReadyToFiltered:      v.ReadyToFiltered,
		ETAAfterOrderDays:    v.ETAAfterOrderDays,
		Details:              v.Details,
		State:                v.State,
		City:                 v.City,
		Location:             vendorLocationDisplay(v),
		BankName:             v.BankName,
		AccountNo:            v.AccountNo,
		IFSCCode:             v.IFSCCode,
		UPIID:                v.UPIID,
		PANNumber:            v.PANNumber,
		FinanceRedacted:      v.FinanceRedacted,
		Comments:             v.Comments,
		CapacityQuantity:     v.CapacityQuantity,
		CapacityUnit:         v.CapacityUnit,
		SupplyFrequency:      v.SupplyFrequency,
		CapacityDisplay: v.CapacityDisplay(
			labels.label(domain.CatalogKindCapacityUnit, derefString(v.CapacityUnit)),
			labels.label(domain.CatalogKindSupplyFrequency, derefString(v.SupplyFrequency)),
		),
		VoiceNoteProofRef:          v.VoiceNoteProofRef,
		AverageAnimalWeightKg:      v.AverageAnimalWeightKg,
		AverageAnimalWeightDisplay: v.AverageAnimalWeightDisplay(),
		CreatedAt:                  v.CreatedAt,
		UpdatedAt:                  v.UpdatedAt,
		RowVersion:                 v.RowVersion,
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// vendorDisplayName composes the one name every surface shows for a vendor.
//
// A missing contact is DROPPED rather than rendered as a dangling separator -- the same
// agree-or-go-bare rule the operational-location helper uses, for the same reason: a label with a
// trailing "- " reads as a bug to the person looking at it.
func vendorDisplayName(v domain.Vendor) string {
	name := strings.TrimSpace(v.BusinessName)
	if v.ContactPersonName == nil {
		return name
	}
	contact := strings.TrimSpace(*v.ContactPersonName)
	if contact == "" || strings.EqualFold(contact, name) {
		// The source data often repeats the business name as the contact ("Irshad" / "Irshad").
		// Rendering "Irshad - Irshad" would be noise.
		return name
	}
	return name + " - " + contact
}

// vendorLocationDisplay composes city and state into the one location string the table shows.
func vendorLocationDisplay(v domain.Vendor) string {
	state := strings.TrimSpace(v.State)
	if v.City == nil {
		return state
	}
	city := strings.TrimSpace(*v.City)
	if city == "" {
		return state
	}
	if state == "" {
		return city
	}
	return city + ", " + state
}

// vendorStatusLabel renders a stored status as operator-facing copy.
//
// Backend-owned by the golden rule: the client renders this verbatim rather than mapping the raw
// token itself, so "In Active" cannot come back as "Inactive" on one screen and "In Active" on
// another.
func vendorStatusLabel(status string) string {
	switch status {
	case domain.VendorStatusActive:
		return "Active"
	case domain.VendorStatusInactive:
		return "Inactive"
	case domain.VendorStatusNegotiating:
		return "Negotiating"
	case domain.VendorStatusBanned:
		return "Banned"
	default:
		return status
	}
}

// vendorOptionPayload is one selectable counterparty for a "who is this for" dropdown. It carries
// identity and location only -- never the payment instruments VendorFinanceRead guards, so this
// picklist can never become a side channel around that permission split.
type vendorOptionPayload struct {
	VendorID     string `json:"vendor_id"`
	BusinessName string `json:"business_name"`
	RecordType   string `json:"record_type"`
	City         string `json:"city"`
	State        string `json:"state"`
}

type vendorOptionsPayload struct {
	Vendors []vendorOptionPayload `json:"vendors"`
	// Truncated says the ACTIVE register is larger than one bounded read, so a picker can tell the
	// person to search the Vendors page rather than imply a missing buyer does not exist.
	Truncated bool `json:"truncated"`
}

func vendorAnswersOrEmpty(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
