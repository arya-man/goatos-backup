package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// VENDOR FORM IS AUTHORED (maintainer instruction 2026-09-19: "in future I want to add any vendor
// data, any optional, anything -- rendered from SOP, reflecting on mobile on the spot"). What the
// Add vendor / Edit vendor forms ask -- which questions, on which page, in which order, which are
// compulsory, plus any question the farm adds tomorrow -- is `form_dsl.vendor_form` of the
// published `sales.vendor` SOP, authored on /sales/sops, served to the phone and the web drawer
// per request. The procurement-SOP load form is the precedent (docs/decisions/procurement-sop.md).
//
// TYPED questions are the vendor register's own columns (`business_name`, `record_type`, ...):
// their id and kind are LOCKED (the register reads them) and, for the catalog-backed ones, their
// choices come from the vendor catalog at compile time; title, hint, page, position and -- except
// for the identity fields -- the compulsory flag are the author's. Every other question is stored
// in `procurement_vendors.sop_answers` with the version it was answered on.

const (
	// SOPCodeVendor is the BUYER register's form, authored on Sales > Sales SOP.
	SOPCodeVendor = "sales.vendor"
	// SOPCodeProcurementVendor is the SUPPLY register's form, authored on Procurement >
	// Procurement SOP (maintainer decision 2026-09-20: "split -- procurement.vendor for
	// suppliers"). One register, two documents: the buying desk and the sales desk ask
	// different things of the people they deal with, and asking a feed supplier for a
	// slaughterhouse's questions is how a form grows fields nobody fills. Which document a
	// vendor is offered is decided by the register side; submitted and historical answers
	// retain the rendered SOP code and version so cached forms and history stay readable.
	SOPCodeProcurementVendor = "procurement.vendor"
	VendorFormSchemaVersion  = "goatos.sop-vendor-form.v1"

	VendorQuestionChoice = "choice"
	VendorQuestionMulti  = "multi"
	VendorQuestionText   = "text"
	VendorQuestionNumber = "number"

	maxVendorFormPages     = 12
	maxVendorFormQuestions = 80
)

// VendorFormDSL is form_dsl.vendor_form.
type VendorFormDSL struct {
	SchemaVersion string           `json:"schema_version"`
	Pages         []VendorFormPage `json:"pages"`
}

// VendorFormPage is one phone page / web section.
type VendorFormPage struct {
	Key       string           `json:"key"`
	Title     string           `json:"title,omitempty"`
	Hint      string           `json:"hint,omitempty"`
	Questions []VendorQuestion `json:"questions"`
}

// VendorQuestion is one authored question. Catalog names the vendor catalog kind whose active
// entries are the choices (typed choice questions); Options are authored choices otherwise.
type VendorQuestion struct {
	ID         string                `json:"id"`
	Kind       string                `json:"kind"`
	Title      string                `json:"title"`
	Hint       string                `json:"hint,omitempty"`
	Required   bool                  `json:"required"`
	Catalog    string                `json:"catalog,omitempty"`
	Options    []VendorQuestionOpt   `json:"options,omitempty"`
	AllowOther bool                  `json:"allow_other,omitempty"`
	Min        *float64              `json:"min,omitempty"`
	Max        *float64              `json:"max,omitempty"`
	Unit       string                `json:"unit,omitempty"`
	OnlyIf     *VendorQuestionOnlyIf `json:"only_if,omitempty"`
}

// VendorQuestionOpt is one choice.
type VendorQuestionOpt struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// VendorQuestionOnlyIf asks the question only after an earlier pick-one answer.
type VendorQuestionOnlyIf struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// VendorForm is the compiled, versioned form: what a phone rendered and what a write is checked
// against. Catalog-backed questions carry their live choices; a titled page keeps its title.
type VendorForm struct {
	SOPCode string
	Version int
	Pages   []VendorFormPage
}

// lockedVendorQuestions are the typed columns: id -> fixed kind (+ catalog for the choice ones).
var lockedVendorQuestions = map[string]VendorQuestion{
	"business_name":            {Kind: VendorQuestionText},
	"record_type":              {Kind: VendorQuestionChoice, Catalog: CatalogKindRecordType},
	"contact_person_name":      {Kind: VendorQuestionText},
	"phone_number":             {Kind: VendorQuestionText},
	"state":                    {Kind: VendorQuestionChoice, Catalog: CatalogKindState},
	"city":                     {Kind: VendorQuestionText},
	"status":                   {Kind: VendorQuestionChoice, Catalog: CatalogKindStatus},
	"capacity_quantity":        {Kind: VendorQuestionNumber},
	"capacity_unit":            {Kind: VendorQuestionChoice, Catalog: CatalogKindCapacityUnit},
	"supply_frequency":         {Kind: VendorQuestionChoice, Catalog: CatalogKindSupplyFrequency},
	"feed":                     {Kind: VendorQuestionChoice, Catalog: CatalogKindFeed},
	"breed":                    {Kind: VendorQuestionChoice, Catalog: CatalogKindBreed},
	"price_per_goat":           {Kind: VendorQuestionNumber},
	"eta_after_order_days":     {Kind: VendorQuestionNumber},
	"average_animal_weight_kg": {Kind: VendorQuestionNumber},
	"comments":                 {Kind: VendorQuestionText},
	"details":                  {Kind: VendorQuestionText},
	"bank_name":                {Kind: VendorQuestionText},
	"account_no":               {Kind: VendorQuestionText},
	"ifsc_code":                {Kind: VendorQuestionText},
	"upi_id":                   {Kind: VendorQuestionText},
	"pan_number":               {Kind: VendorQuestionText},
	"filtered_stock":           {Kind: VendorQuestionNumber},
}

// requiredVendorQuestionIDs must be present AND compulsory: a vendor cannot exist without them.
var requiredVendorQuestionIDs = []string{"business_name", "record_type", "state", "status"}

// VendorFormSOPCode is the form document a given register side is answered on. An unknown or
// blank side resolves to the sales document, which is what the whole register answered on before
// the split -- a legacy vendor keeps reading the form it was written against.
func VendorFormSOPCode(side string) string {
	if normalized, ok := NormalizeVendorSide(side); ok && normalized == VendorSideProcurement {
		return SOPCodeProcurementVendor
	}
	return SOPCodeVendor
}

// VendorSideForRecordType resolves which register half a record type belongs to, from the
// catalog itself. Blank when the catalog does not carry the type: the caller then falls back to
// the sales document rather than guessing a side, and the write's own record-type validation is
// what refuses an unknown type.
func VendorSideForRecordType(catalog []VendorCatalogEntry, recordType string) string {
	recordType = strings.TrimSpace(recordType)
	if recordType == "" {
		return ""
	}
	for _, e := range catalog {
		if e.Kind != CatalogKindRecordType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(e.Value), recordType) {
			return e.RegisterSide
		}
	}
	return ""
}

// IsTypedVendorQuestion reports a question the register stores in its own column.
func IsTypedVendorQuestion(id string) bool { _, ok := lockedVendorQuestions[id]; return ok }

//go:embed vendorformseed/vendor.json
var seededVendorFormJSON []byte

// SeededVendorFormJSON is the day-one document, embedded verbatim in the migration that publishes
// it as sales.vendor v1 (pinned by TestMigrationEmbedsTheSeededVendorForm).
func SeededVendorFormJSON() []byte { return append([]byte(nil), seededVendorFormJSON...) }

// SeededVendorFormDSL parses the embedded document.
func SeededVendorFormDSL() VendorFormDSL {
	dsl, err := ParseVendorForm(map[string]any{"vendor_form": json.RawMessage(seededVendorFormJSON)})
	if err != nil {
		panic("procurement: seeded vendor form does not parse: " + err.Error())
	}
	return dsl
}

// ErrVendorFormMissing / ErrVendorFormInvalid name a document the register cannot run.
var (
	ErrVendorFormMissing = errors.New("procurement: SOP version has no vendor_form section")
	ErrVendorFormInvalid = errors.New("procurement: vendor_form is invalid")
)

// ParseVendorForm extracts and type-checks form_dsl.vendor_form.
func ParseVendorForm(formDSL map[string]any) (VendorFormDSL, error) {
	return ParseEntryForm(VendorFormProfile(), formDSL)
}

// ParseEntryForm extracts and type-checks a pages-of-questions document from the section the
// profile names. The SHAPE is one engine -- pages, questions, locked typed ids, catalog-backed
// choices, only_if -- and each form differs only in which section it lives in, which ids the
// module reads into its own columns, and which catalogs fill its choices.
func ParseEntryForm(profile EntryFormProfile, formDSL map[string]any) (VendorFormDSL, error) {
	raw, ok := formDSL[profile.Section]
	if !ok || raw == nil {
		return VendorFormDSL{}, ErrVendorFormMissing
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return VendorFormDSL{}, fmt.Errorf("%w: %v", ErrVendorFormInvalid, err)
	}
	var out VendorFormDSL
	dec := json.NewDecoder(strings.NewReader(string(encoded)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return VendorFormDSL{}, fmt.Errorf("%w: %v", ErrVendorFormInvalid, err)
	}
	if out.SchemaVersion != profile.SchemaVersion {
		return VendorFormDSL{}, fmt.Errorf("%w: schema_version %q", ErrVendorFormInvalid, out.SchemaVersion)
	}
	return out, nil
}

// EntryFormProfile is what makes one pages-of-questions document different from another: the
// form_dsl section it lives in, its schema tag, the LOCKED ids its module reads into typed
// columns, the ids that must stay present and compulsory, and the catalogs its choice questions
// may draw from. Everything else -- parsing, validation, compilation, answer checking -- is shared,
// because a second copy of those rules is a second place for them to drift.
type EntryFormProfile struct {
	Section       string
	SchemaVersion string
	Locked        map[string]VendorQuestion
	RequiredIDs   []string
	CatalogKinds  []string
	// Noun names the record in a problem message ("a vendor cannot exist without it").
	Noun string
	// Reserved are question ids an author may NOT ask, with why: a value the system gives itself
	// (the feed purchase's load number, 2026-09-26) must not come back as a question on the form.
	Reserved map[string]string
}

// VendorFormProfile is the vendor register's document.
func VendorFormProfile() EntryFormProfile {
	return EntryFormProfile{
		Section:       "vendor_form",
		SchemaVersion: VendorFormSchemaVersion,
		Locked:        lockedVendorQuestions,
		RequiredIDs:   requiredVendorQuestionIDs,
		CatalogKinds:  VendorCatalogKinds,
		Noun:          "a vendor",
	}
}

var vendorQuestionIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidateVendorForm names every problem by path. Publishing runs it; a document with any problem
// never becomes the published version.
func ValidateVendorForm(dsl VendorFormDSL) []string {
	return ValidateEntryForm(VendorFormProfile(), dsl)
}

// ValidateEntryForm is the shared validator, told by the profile which ids are locked, which are
// compulsory and which catalogs exist.
func ValidateEntryForm(profile EntryFormProfile, dsl VendorFormDSL) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(dsl.Pages) == 0 {
		add("%s.pages: at least one page is required", profile.Section)
	}
	if len(dsl.Pages) > maxVendorFormPages {
		add("%s.pages: at most %d pages", profile.Section, maxVendorFormPages)
	}
	seen := map[string]VendorQuestion{}
	seenPages := map[string]bool{}
	total := 0
	for pi, p := range dsl.Pages {
		pp := fmt.Sprintf("%s.pages.%d", profile.Section, pi)
		if !vendorQuestionIDPattern.MatchString(p.Key) {
			add("%s.key: %q must be a-z, 0-9 and _ (start with a letter)", pp, p.Key)
		}
		if seenPages[p.Key] {
			add("%s.key: %q is used twice", pp, p.Key)
		}
		seenPages[p.Key] = true
		if len(p.Questions) == 0 {
			add("%s.questions: a page needs at least one question", pp)
		}
		for qi, q := range p.Questions {
			total++
			qp := fmt.Sprintf("%s.questions.%d", pp, qi)
			if !vendorQuestionIDPattern.MatchString(q.ID) {
				add("%s.id: %q must be a-z, 0-9 and _ (start with a letter)", qp, q.ID)
			}
			if _, dup := seen[q.ID]; dup {
				add("%s.id: %q is used twice", qp, q.ID)
			}
			if why, reserved := profile.Reserved[q.ID]; reserved {
				add("%s.id: %q cannot be asked: %s", qp, q.ID, why)
			}
			if strings.TrimSpace(q.Title) == "" {
				add("%s.title: required", qp)
			}
			switch q.Kind {
			case VendorQuestionChoice, VendorQuestionMulti:
				if q.Catalog == "" && len(q.Options) == 0 {
					add("%s.options: a pick-one / pick-many question needs at least one choice", qp)
				}
				seenOpt := map[string]bool{}
				for oi, o := range q.Options {
					if strings.TrimSpace(o.Value) == "" || strings.TrimSpace(o.Label) == "" {
						add("%s.options.%d: value and label are required", qp, oi)
					}
					if seenOpt[o.Value] {
						add("%s.options.%d: value %q is used twice", qp, oi, o.Value)
					}
					seenOpt[o.Value] = true
				}
				if q.AllowOther && !seenOpt["other"] {
					add("%s.allow_other: needs an option with value \"other\"", qp)
				}
			case VendorQuestionNumber:
				if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
					add("%s.min: must not exceed max", qp)
				}
			case VendorQuestionText:
			default:
				add("%s.kind: %q is not a vendor-form question kind (choice, multi, text, number)", qp, q.Kind)
			}
			if q.Catalog != "" {
				known := false
				for _, k := range profile.CatalogKinds {
					if k == q.Catalog {
						known = true
					}
				}
				if !known {
					add("%s.catalog: %q is not a catalog this form can read", qp, q.Catalog)
				}
			}
			if q.OnlyIf != nil {
				if strings.TrimSpace(q.OnlyIf.Value) == "" {
					add("%s.only_if.value: a nonblank choice is required", qp)
				}
				dep, ok := seen[q.OnlyIf.QuestionID]
				switch {
				case !ok:
					add("%s.only_if.question_id: %q must be an EARLIER question", qp, q.OnlyIf.QuestionID)
				case dep.Kind != VendorQuestionChoice:
					add("%s.only_if.question_id: %q must be a pick-one question", qp, q.OnlyIf.QuestionID)
				case dep.Catalog == "" && !hasVendorOption(dep.Options, q.OnlyIf.Value):
					add("%s.only_if.value: %q is not a choice of %q", qp, q.OnlyIf.Value, q.OnlyIf.QuestionID)
				}
			}
			if lk, locked := profile.Locked[q.ID]; locked {
				if q.Kind != lk.Kind {
					add("%s.kind: %q is fixed to %q (the register reads it)", qp, q.ID, lk.Kind)
				}
				if lk.Catalog != "" && q.Catalog != lk.Catalog {
					add("%s.catalog: %q takes its choices from the %q catalog", qp, q.ID, lk.Catalog)
				}
			}
			seen[q.ID] = q
		}
	}
	for id, question := range seen {
		if question.AllowOther {
			if _, collision := seen[id+"_other"]; collision {
				add("%s: question %q collides with the Other explanation of %q", profile.Section, id+"_other", id)
			}
		}
	}
	if total > maxVendorFormQuestions {
		add("%s: at most %d questions", profile.Section, maxVendorFormQuestions)
	}
	for _, id := range profile.RequiredIDs {
		q, ok := seen[id]
		if !ok {
			add("%s: question %q must be present (%s cannot exist without it)", profile.Section, id, profile.Noun)
			continue
		}
		if !q.Required {
			add("%s: %q must stay compulsory (%s cannot exist without it)", profile.Section, id, profile.Noun)
		}
	}
	return problems
}

func hasVendorOption(opts []VendorQuestionOpt, value string) bool {
	for _, o := range opts {
		if o.Value == value {
			return true
		}
	}
	return false
}

// CompileVendorForm fills each catalog-backed question's choices from the live catalog (active
// entries, in catalog order) and returns the form the phone renders. Version is the SOP version.
func CompileVendorForm(dsl VendorFormDSL, version int, catalog []VendorCatalogEntry) VendorForm {
	byKind := map[string][]VendorQuestionOpt{}
	for _, e := range catalog {
		if !e.IsActive {
			continue
		}
		byKind[e.Kind] = append(byKind[e.Kind], VendorQuestionOpt{Value: e.Value, Label: e.Label})
	}
	out := VendorForm{Version: version, Pages: make([]VendorFormPage, 0, len(dsl.Pages))}
	for _, p := range dsl.Pages {
		page := VendorFormPage{Key: p.Key, Title: p.Title, Hint: p.Hint, Questions: make([]VendorQuestion, 0, len(p.Questions))}
		for _, q := range p.Questions {
			if q.Catalog != "" {
				q.Options = append([]VendorQuestionOpt(nil), byKind[q.Catalog]...)
			}
			page.Questions = append(page.Questions, q)
		}
		out.Pages = append(out.Pages, page)
	}
	return out
}

// Questions flattens the form in page order.
func (f VendorForm) Questions() []VendorQuestion {
	var out []VendorQuestion
	for _, p := range f.Pages {
		out = append(out, p.Questions...)
	}
	return out
}

// ErrVendorAnswer is one refused answer, named by question so the form can show it in place.
type ErrVendorAnswer struct {
	QuestionID string
	Reason     string
}

func (e ErrVendorAnswer) Error() string {
	return "procurement: vendor answer " + e.QuestionID + ": " + e.Reason
}

// ValidateVendorAnswers checks a submission against the form it was rendered from: compulsory
// questions answered (a question hidden by only_if is not owed), choices among the offered
// ones (or "other" text when allowed), numbers numeric and inside min/max, and no answer to a
// question the form does not carry. Typed answers are checked here too; the register's own
// length/format rules still run on the mapped columns.
// VisibleVendorAnswers evaluates dependencies in document order across every page.
// A hidden answer cannot activate a later descendant, even if an older client sends it.
func VisibleVendorAnswers(form VendorForm, answers map[string]string) map[string]string {
	visible := map[string]string{}
	for _, q := range form.Questions() {
		if q.OnlyIf != nil {
			parent, present := visible[q.OnlyIf.QuestionID]
			if !present || strings.TrimSpace(q.OnlyIf.Value) == "" || strings.TrimSpace(parent) != q.OnlyIf.Value {
				continue
			}
		}
		if value, ok := answers[q.ID]; ok {
			visible[q.ID] = value
		}
		if q.AllowOther && strings.TrimSpace(answers[q.ID]) == "other" {
			if value, ok := answers[q.ID+"_other"]; ok {
				visible[q.ID+"_other"] = value
			}
		}
	}
	return visible
}

func ValidateVendorAnswers(form VendorForm, answers map[string]string) error {
	known := map[string]VendorQuestion{}
	for _, q := range form.Questions() {
		known[q.ID] = q
	}
	for id := range answers {
		if _, exact := known[id]; exact {
			continue
		}
		base := strings.TrimSuffix(id, "_other")
		_, knownParent := known[base]
		// Older installed clients retain an explanation after Other is retired.
		// Accept its legacy wire shape, then discard it in VisibleVendorAnswers.
		if base == id || !knownParent {
			return ErrVendorAnswer{QuestionID: id, Reason: "is not a question on this form"}
		}
	}
	answers = VisibleVendorAnswers(form, answers)
	for _, q := range form.Questions() {
		if q.OnlyIf != nil {
			parent, present := answers[q.OnlyIf.QuestionID]
			if !present || strings.TrimSpace(q.OnlyIf.Value) == "" || strings.TrimSpace(parent) != q.OnlyIf.Value {
				continue
			}
		}
		value := strings.TrimSpace(answers[q.ID])
		if value == "" {
			if q.Required {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: "required"}
			}
			continue
		}
		switch q.Kind {
		case VendorQuestionChoice:
			if !hasVendorOption(q.Options, value) {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: "must be one of the offered choices"}
			}
			if value == "other" && q.AllowOther && strings.TrimSpace(answers[q.ID+"_other"]) == "" {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: "say what the other is"}
			}
		case VendorQuestionMulti:
			for _, v := range strings.Split(value, "|") {
				if !hasVendorOption(q.Options, strings.TrimSpace(v)) {
					return ErrVendorAnswer{QuestionID: q.ID, Reason: "must be among the offered choices"}
				}
			}
		case VendorQuestionNumber:
			n, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: "must be a number"}
			}
			if q.Min != nil && n < *q.Min {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: fmt.Sprintf("must be at least %v", *q.Min)}
			}
			if q.Max != nil && n > *q.Max {
				return ErrVendorAnswer{QuestionID: q.ID, Reason: fmt.Sprintf("must be at most %v", *q.Max)}
			}
		}
	}
	return nil
}

// ApplyVendorAnswers copies the typed answers onto the write's columns and returns the extra
// (untyped) answers to store as sop_answers. The write is a REPLACE, so a typed answer that is
// PRESENT but blank clears its column; a typed question the form did not ask (absent key) leaves
// the column as the write already carries it (the client carries unasked columns forward).
func ApplyVendorAnswers(w VendorWrite, answers map[string]string) (VendorWrite, map[string]string) {
	extras := map[string]string{}
	set := func(dst *string, v string) { *dst = strings.TrimSpace(v) }
	setPtrStr := func(dst **string, v string) {
		if strings.TrimSpace(v) == "" {
			*dst = nil
			return
		}
		s := strings.TrimSpace(v)
		*dst = &s
	}
	setPtrInt := func(dst **int, v string) {
		if strings.TrimSpace(v) == "" {
			*dst = nil
			return
		}
		if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			i := int(n)
			*dst = &i
		}
	}
	for id, v := range answers {
		switch id {
		case "business_name":
			set(&w.BusinessName, v)
		case "record_type":
			set(&w.RecordType, v)
		case "contact_person_name":
			set(&w.ContactPersonName, v)
		case "phone_number":
			set(&w.PhoneNumber, v)
		case "state":
			set(&w.State, v)
		case "city":
			set(&w.City, v)
		case "status":
			set(&w.Status, v)
		case "capacity_quantity":
			setPtrStr(&w.CapacityQuantity, v)
		case "capacity_unit":
			set(&w.CapacityUnit, v)
		case "supply_frequency":
			set(&w.SupplyFrequency, v)
		case "feed":
			set(&w.Feed, v)
		case "breed":
			set(&w.Breed, v)
		case "price_per_goat":
			setPtrStr(&w.PricePerGoat, v)
		case "eta_after_order_days":
			setPtrInt(&w.ETAAfterOrderDays, v)
		case "average_animal_weight_kg":
			setPtrStr(&w.AverageAnimalWeightKg, v)
		case "comments":
			set(&w.Comments, v)
		case "details":
			set(&w.Details, v)
		case "bank_name":
			set(&w.BankName, v)
		case "account_no":
			set(&w.AccountNo, v)
		case "ifsc_code":
			set(&w.IFSCCode, v)
		case "upi_id":
			set(&w.UPIID, v)
		case "pan_number":
			set(&w.PANNumber, v)
		case "filtered_stock":
			setPtrInt(&w.FilteredStock, v)
		default:
			if strings.TrimSpace(v) != "" {
				extras[id] = strings.TrimSpace(v)
			}
		}
	}
	return w, extras
}

// VendorAnswerRow is one extra answer as a detail screen lists it, labelled by the version's form.
type VendorAnswerRow struct {
	QuestionID string
	Label      string
	Value      string
}

// VendorAnswerRows labels stored extras by the form they were answered on; an answer whose
// question the form no longer carries is listed under its id rather than dropped.
func VendorAnswerRows(form VendorForm, answers map[string]string) []VendorAnswerRow {
	labels := map[string]VendorQuestion{}
	for _, q := range form.Questions() {
		labels[q.ID] = q
	}
	out := []VendorAnswerRow{}
	for _, q := range form.Questions() {
		if IsTypedVendorQuestion(q.ID) {
			continue
		}
		v, ok := answers[q.ID]
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		display := v
		if q.Kind == VendorQuestionChoice || q.Kind == VendorQuestionMulti {
			parts := []string{}
			for _, raw := range strings.Split(v, "|") {
				raw = strings.TrimSpace(raw)
				label := raw
				for _, o := range q.Options {
					if o.Value == raw {
						label = o.Label
					}
				}
				if q.AllowOther && raw == "other" && strings.TrimSpace(answers[q.ID+"_other"]) != "" {
					label = strings.TrimSpace(answers[q.ID+"_other"])
				}
				parts = append(parts, label)
			}
			display = strings.Join(parts, ", ")
		}
		if q.Unit != "" && q.Kind == VendorQuestionNumber {
			display = display + " " + q.Unit
		}
		out = append(out, VendorAnswerRow{QuestionID: q.ID, Label: q.Title, Value: display})
	}
	out = append(out, unresolvedAnswerRows(labels, answers)...)
	return out
}

// Preserve every historical value even when its authored document cannot be read.
// Pair an Other explanation with its choice and keep fallback ordering stable.
func unresolvedAnswerRows(known map[string]VendorQuestion, answers map[string]string) []VendorAnswerRow {
	ids := make([]string, 0, len(answers))
	for id := range answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []VendorAnswerRow{}
	for _, id := range ids {
		value := strings.TrimSpace(answers[id])
		if _, ok := known[id]; ok || value == "" {
			continue
		}
		if strings.HasSuffix(id, "_other") {
			base := strings.TrimSuffix(id, "_other")
			if parent, ok := known[base]; ok && parent.AllowOther {
				continue
			}
			if strings.TrimSpace(answers[base]) == "other" {
				continue
			}
		}
		if value == "other" && strings.TrimSpace(answers[id+"_other"]) != "" {
			value = strings.TrimSpace(answers[id+"_other"])
		}
		out = append(out, VendorAnswerRow{QuestionID: id, Label: id, Value: value})
	}
	return out
}
