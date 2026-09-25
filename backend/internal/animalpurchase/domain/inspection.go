package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PROCUREMENT SOP (maintainer decision 2026-09-14, docs/decisions/procurement-sop.md).
//
// The per-animal inspection is no longer a Go catalog: it is the `inspection` section of the
// PUBLISHED `procurement.animal_purchase` SOP version, authored on /procurement/sops -- which
// questions, on which PAGE, in which order, which take a photo / a video / either, which are
// compulsory. The phone renders the compiled catalog exactly as before (pages are section rows);
// each recorded animal is stamped with the SOP version it was answered on, and that version --
// not the latest -- validates the write and labels the CEO's review. Publishing changes the next
// animal recorded; a form already open on a phone submits against the version it rendered.
//
// The legacy Questionnaire() stays ONLY as the golden oracle for the seeded document
// (TestSeededInspectionCompilesToTheLegacyQuestionnaire).

// InspectionSchemaVersion is the document's own version tag (not the SOP version number).
const InspectionSchemaVersion = "goatos.sop-inspection.v1"

// SOPCodeAnimalPurchase is the SOP library code the inspection is published under.
const SOPCodeAnimalPurchase = "procurement.animal_purchase"

// InspectionDSL is form_dsl.inspection: the LOAD form (one page, recorded once per purchase
// load) and the per-animal inspection as ordered pages of ordered questions.
type InspectionDSL struct {
	SchemaVersion string           `json:"schema_version"`
	LoadForm      LoadForm         `json:"load_form"`
	Pages         []InspectionPage `json:"pages"`
}

// LoadForm is what the desk answers when it opens a load. vendor / farm / load_ref are the
// load's identity and stay locked; expected_count and notes may be re-worded or made
// compulsory; any further question (pick-one, pick-many, number, text) may be added.
// Photos and videos are recorded per ANIMAL, never on the load (nothing reviews a load capture).
type LoadForm struct {
	Questions []Question `json:"questions"`
}

// KindVendor is the load form's vendor picker: the phone renders the vendor register search;
// the answer is the vendor id.
const KindVendor = "vendor"

// InspectionPage is one phone page. A page with a title renders a section heading and starts a
// new page; the first page may be untitled (the SOP form's opening questions).
type InspectionPage struct {
	Key       string     `json:"key"`
	Title     string     `json:"title,omitempty"`
	Hint      string     `json:"hint,omitempty"`
	Questions []Question `json:"questions"`
}

// Catalog is one compiled, versioned questionnaire: what a phone rendered and what a write is
// validated against. Version is the SOP version number (sop_versions.version). LoadQuestions is
// the load form of the same version.
type Catalog struct {
	Version       int
	Questions     []Question
	LoadQuestions []Question
}

//go:embed inspectionseed/animal_purchase.json
var seededInspectionJSON []byte

// SeededInspectionJSON is the day-one document, embedded verbatim in the migration that
// publishes it as procurement.animal_purchase v1.
func SeededInspectionJSON() []byte { return append([]byte(nil), seededInspectionJSON...) }

// SeededCatalog compiles the embedded document; a tenant with no published version runs it.
func SeededCatalog() Catalog {
	dsl, err := ParseInspection(map[string]any{"inspection": json.RawMessage(seededInspectionJSON)})
	if err != nil {
		panic("animalpurchase: seeded inspection does not parse: " + err.Error())
	}
	return Catalog{Version: QuestionnaireVersion, Questions: CompileInspection(dsl), LoadQuestions: dsl.LoadForm.Questions}
}

var ErrInspectionInvalid = errors.New("inspection document invalid")

// ParseInspection reads form_dsl.inspection. A form_dsl without one is not an inspection SOP.
func ParseInspection(formDSL map[string]any) (InspectionDSL, error) {
	raw, ok := formDSL["inspection"]
	if !ok || raw == nil {
		return InspectionDSL{}, fmt.Errorf("%w: form_dsl.inspection missing", ErrInspectionInvalid)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return InspectionDSL{}, fmt.Errorf("%w: %v", ErrInspectionInvalid, err)
	}
	var dsl InspectionDSL
	if err := json.Unmarshal(b, &dsl); err != nil {
		return InspectionDSL{}, fmt.Errorf("%w: %v", ErrInspectionInvalid, err)
	}
	return dsl, nil
}

// CompileInspection flattens pages into the catalog the phone and the validator consume: a
// titled page contributes a section row (id "sec_<key>") followed by its questions.
func CompileInspection(dsl InspectionDSL) []Question {
	out := make([]Question, 0, 48)
	for _, p := range dsl.Pages {
		if strings.TrimSpace(p.Title) != "" {
			out = append(out, Question{ID: "sec_" + p.Key, Kind: KindSection, Title: p.Title, Hint: p.Hint})
		}
		for _, q := range p.Questions {
			if q.Kind == KindMedia && q.Slot == "" {
				q.Slot = q.ID
			}
			out = append(out, q)
		}
	}
	return out
}

// Locked questions: the ones the typed columns, list titles and review chips are derived from.
// Their id, kind and (for the closed vocabularies) options are fixed; title, hint, required,
// page and position are the author's.
type lockedQuestion struct {
	Kind    string
	Options []Option // nil = author's own
}

var lockedQuestions = map[string]lockedQuestion{
	// Species and sex CHOICES are not fixed here and not read from the document: they are the
	// tenant's active Configuration lists, filled in at read time by Catalog.WithAnimalVocabulary
	// (OPEN UP TO NEW SPECIES, maintainer decision 2026-09-25), so a species added there is offered
	// on every published version at once. The document's own options are a placeholder.
	"species":       {Kind: KindChoice},
	"goat_id":       {Kind: KindText},
	"sex":           {Kind: KindChoice},
	"weight_kg":     {Kind: KindNumber},
	"height_cm":     {Kind: KindNumber},
	"rectal_temp_c": {Kind: KindNumber},
	"field_verdict": {Kind: KindChoice, Options: []Option{{FieldVerdictSelected, "Selected"}, {FieldVerdictOnHold, "On Hold"}}},
	"breed":         {Kind: KindText},
	"notes":         {Kind: KindText},
}

// LockedQuestionIDs lists the ids whose kind is fixed by the engine, for the editor.
func LockedQuestionIDs() []string {
	return []string{"species", "goat_id", "sex", "weight_kg", "height_cm", "rectal_temp_c", "field_verdict", "breed", "notes"}
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

// Locked load-form questions: the load's identity and the columns the list reads.
var lockedLoadQuestions = map[string]lockedQuestion{
	"load_ref": {Kind: KindText},
	"vendor":   {Kind: KindVendor},
	// The farm's CHOICES are not fixed here and not read from the document: they are the tenant's
	// active parks, filled in at read time by Catalog.WithFarms, so a park added on Configuration
	// > Items & settings is offered on every published version at once.
	"farm":           {Kind: KindChoice},
	"expected_count": {Kind: KindNumber},
	"notes":          {Kind: KindText},
}

// WithFarms returns a copy of the catalog whose load-form "farm" question offers exactly the
// given park codes. The document's own farm options are a placeholder: which parks exist is
// Configuration's answer, never the form author's.
func (c Catalog) WithFarms(codes []string) Catalog {
	out := c
	out.LoadQuestions = append([]Question(nil), c.LoadQuestions...)
	for i := range out.LoadQuestions {
		if out.LoadQuestions[i].ID == "farm" {
			out.LoadQuestions[i].Options = Farms(codes)
		}
	}
	return out
}

// WithAnimalVocabulary returns a copy of the catalog whose inspection "species" and "sex" questions
// offer exactly the given choices -- the tenant's active species and genders. Which species and
// genders exist is Configuration's answer, never the form author's. An empty list leaves the
// document's own choices, so a vocabulary that failed to load never empties a compulsory question.
func (c Catalog) WithAnimalVocabulary(species, sexes []Option) Catalog {
	out := c
	out.Questions = append([]Question(nil), c.Questions...)
	for i := range out.Questions {
		switch {
		case out.Questions[i].ID == "species" && len(species) > 0:
			out.Questions[i].Options = append([]Option(nil), species...)
		case out.Questions[i].ID == "sex" && len(sexes) > 0:
			out.Questions[i].Options = append([]Option(nil), sexes...)
		}
	}
	return out
}

// LockedLoadQuestionIDs lists the load-form ids whose kind is fixed, for the editor.
func LockedLoadQuestionIDs() []string {
	return []string{"load_ref", "vendor", "farm", "expected_count", "notes"}
}

// requiredLoadQuestionIDs must stay compulsory: a load without them cannot be created.
var requiredLoadQuestionIDs = []string{"load_ref", "vendor", "farm"}

const maxInspectionQuestions = 200

// ValidateInspection returns every problem in the document, each naming its path, so the web
// editor can point at the field. An empty slice means the document compiles and can be published.
func ValidateInspection(dsl InspectionDSL) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if dsl.SchemaVersion != InspectionSchemaVersion {
		add("inspection.schema_version: want %q", InspectionSchemaVersion)
	}
	if len(dsl.Pages) == 0 {
		add("inspection.pages: at least one page")
	}
	validateLoadForm(dsl.LoadForm, add)
	seenPages := map[string]bool{}
	seen := map[string]Question{}
	total := 0
	for pi, p := range dsl.Pages {
		pp := fmt.Sprintf("inspection.pages.%d", pi)
		if !idPattern.MatchString(p.Key) {
			add("%s.key: %q must be a-z, 0-9 and _ (start with a letter)", pp, p.Key)
		}
		if seenPages[p.Key] {
			add("%s.key: %q is used twice", pp, p.Key)
		}
		seenPages[p.Key] = true
		if pi > 0 && strings.TrimSpace(p.Title) == "" {
			add("%s.title: every page after the first needs a title (it is the page heading)", pp)
		}
		if len(p.Questions) == 0 {
			add("%s.questions: a page needs at least one question", pp)
		}
		for qi, q := range p.Questions {
			total++
			qp := fmt.Sprintf("%s.questions.%d", pp, qi)
			if !idPattern.MatchString(q.ID) {
				add("%s.id: %q must be a-z, 0-9 and _ (start with a letter)", qp, q.ID)
			}
			if strings.HasPrefix(q.ID, "sec_") {
				add("%s.id: %q -- the sec_ prefix is reserved for page headings", qp, q.ID)
			}
			if _, dup := seen[q.ID]; dup {
				add("%s.id: %q is used twice", qp, q.ID)
			}
			if strings.TrimSpace(q.Title) == "" {
				add("%s.title: required", qp)
			}
			switch q.Kind {
			case KindChoice, KindMulti:
				if len(q.Options) == 0 {
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
					add("%s.allow_other: needs an option with value \"other\" to attach the free text to", qp)
				}
			case KindNumber:
				if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
					add("%s.min: must not exceed max", qp)
				}
			case KindMedia:
				if len(q.Accepts) == 0 {
					add("%s.accepts: say whether the capture is a photo, a video or either", qp)
				}
				for _, a := range q.Accepts {
					if a != "photo" && a != "video" {
						add("%s.accepts: %q is not photo or video", qp, a)
					}
				}
				if q.MaxFiles < 0 || q.MaxFiles > 10 {
					add("%s.max_files: 0..10", qp)
				}
				slot := q.Slot
				if slot == "" {
					slot = q.ID
				}
				if !idPattern.MatchString(slot) {
					add("%s.slot: %q must be a-z, 0-9 and _", qp, slot)
				}
			case KindText:
			case KindSection:
				add("%s.kind: page headings are pages, not questions", qp)
			default:
				add("%s.kind: %q is not a question kind", qp, q.Kind)
			}
			if q.OnlyIf != nil {
				dep, ok := seen[q.OnlyIf.QuestionID]
				switch {
				case !ok:
					add("%s.only_if.question_id: %q must be an EARLIER question", qp, q.OnlyIf.QuestionID)
				case dep.Kind != KindChoice:
					add("%s.only_if.question_id: %q must be a pick-one question", qp, q.OnlyIf.QuestionID)
				case !hasOption(dep.Options, q.OnlyIf.Value):
					add("%s.only_if.value: %q is not a choice of %q", qp, q.OnlyIf.Value, q.OnlyIf.QuestionID)
				}
			}
			if lk, locked := lockedQuestions[q.ID]; locked {
				if q.Kind != lk.Kind {
					add("%s.kind: %q is fixed to %q (the register reads it)", qp, q.ID, lk.Kind)
				}
				if lk.Options != nil && !sameOptionValues(q.Options, lk.Options) {
					add("%s.options: the choices of %q are fixed (%s)", qp, q.ID, optionValues(lk.Options))
				}
			}
			seen[q.ID] = q
		}
	}
	for _, id := range LockedQuestionIDs() {
		if _, ok := seen[id]; !ok {
			add("inspection: question %q must be present (the register reads it)", id)
		}
	}
	if total > maxInspectionQuestions {
		add("inspection: at most %d questions", maxInspectionQuestions)
	}
	return problems
}

// validateLoadForm checks the load form: locked ids present with their kinds, identity questions
// compulsory, every other question a non-media kind with the same rules as the inspection.
func validateLoadForm(lf LoadForm, add func(string, ...any)) {
	seen := map[string]Question{}
	for qi, q := range lf.Questions {
		qp := fmt.Sprintf("inspection.load_form.questions.%d", qi)
		if !idPattern.MatchString(q.ID) {
			add("%s.id: %q must be a-z, 0-9 and _ (start with a letter)", qp, q.ID)
		}
		if _, dup := seen[q.ID]; dup {
			add("%s.id: %q is used twice", qp, q.ID)
		}
		if strings.TrimSpace(q.Title) == "" {
			add("%s.title: required", qp)
		}
		switch q.Kind {
		case KindChoice, KindMulti:
			if len(q.Options) == 0 {
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
		case KindNumber:
			if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
				add("%s.min: must not exceed max", qp)
			}
		case KindText:
		case KindVendor:
			if q.ID != "vendor" {
				add("%s.kind: the vendor picker is the locked \"vendor\" question", qp)
			}
		case KindMedia:
			add("%s.kind: photos and videos are recorded per animal, not on the load", qp)
		default:
			add("%s.kind: %q is not a load-form question kind", qp, q.Kind)
		}
		if q.OnlyIf != nil {
			dep, ok := seen[q.OnlyIf.QuestionID]
			switch {
			case !ok:
				add("%s.only_if.question_id: %q must be an EARLIER question", qp, q.OnlyIf.QuestionID)
			case dep.Kind != KindChoice:
				add("%s.only_if.question_id: %q must be a pick-one question", qp, q.OnlyIf.QuestionID)
			case !hasOption(dep.Options, q.OnlyIf.Value):
				add("%s.only_if.value: %q is not a choice of %q", qp, q.OnlyIf.Value, q.OnlyIf.QuestionID)
			}
		}
		if lk, locked := lockedLoadQuestions[q.ID]; locked {
			if q.Kind != lk.Kind {
				add("%s.kind: %q is fixed to %q (the load reads it)", qp, q.ID, lk.Kind)
			}
			if lk.Options != nil && !sameOptionValues(q.Options, lk.Options) {
				add("%s.options: the choices of %q are fixed (%s)", qp, q.ID, optionValues(lk.Options))
			}
		}
		seen[q.ID] = q
	}
	for _, id := range LockedLoadQuestionIDs() {
		if _, ok := seen[id]; !ok {
			add("inspection.load_form: question %q must be present (the load reads it)", id)
		}
	}
	for _, id := range requiredLoadQuestionIDs {
		if q, ok := seen[id]; ok && !q.Required {
			add("inspection.load_form: %q must stay compulsory (a load cannot exist without it)", id)
		}
	}
}

func sameOptionValues(a, b []Option) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func optionValues(o []Option) string {
	vals := make([]string, 0, len(o))
	for _, x := range o {
		vals = append(vals, x.Value)
	}
	return strings.Join(vals, " / ")
}

// --- Catalog reads -------------------------------------------------------------------------

func (c Catalog) MediaSlots() []Question {
	var out []Question
	for _, q := range c.Questions {
		if q.Kind == KindMedia {
			out = append(out, q)
		}
	}
	return out
}

func (c Catalog) ByID(id string) (Question, bool) {
	for _, q := range c.Questions {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}

// AllMediaRefs flattens the media map in slot order.
func (c Catalog) AllMediaRefs(media MediaRefs) []string {
	var out []string
	for _, q := range c.MediaSlots() {
		out = append(out, media[q.Slot]...)
	}
	return out
}

// ValidateLoadAnswers checks a load's extra answers against THIS catalog's load form. The
// locked identity questions are validated by LoadWrite (typed columns); everything else --
// including a locked question the author made compulsory -- is checked here.
func (c Catalog) ValidateLoadAnswers(a Answers) error {
	for _, q := range c.LoadQuestions {
		if q.Kind == KindVendor || !a.Applies(q) {
			continue
		}
		switch q.ID {
		case "load_ref", "farm":
			continue // typed columns, validated by LoadWrite
		}
		switch q.Kind {
		case KindChoice:
			v := a.choice(q.ID)
			if v == "" {
				if q.Required {
					return invalid(q.ID, "Answer: "+q.Title)
				}
				continue
			}
			if !hasOption(q.Options, v) {
				return invalid(q.ID, "Pick one of the offered answers for: "+q.Title)
			}
			if v == "other" && q.AllowOther && strings.TrimSpace(a.text(q.ID+"_other")) == "" {
				return invalid(q.ID, "Write the other answer for: "+q.Title)
			}
		case KindMulti:
			vals, ok := a.multi(q.ID)
			if !ok || len(vals) == 0 {
				if q.Required {
					return invalid(q.ID, "Tick at least one for: "+q.Title)
				}
				continue
			}
			for _, v := range vals {
				if !hasOption(q.Options, v) {
					return invalid(q.ID, "Pick only the offered answers for: "+q.Title)
				}
			}
		case KindNumber:
			n, present, err := a.number(q.ID)
			if !present {
				if q.Required {
					return invalid(q.ID, "Enter: "+q.Title)
				}
				continue
			}
			if err != nil {
				return invalid(q.ID, "Enter a number for: "+q.Title)
			}
			if (q.Min != nil && n < *q.Min) || (q.Max != nil && n > *q.Max) {
				return invalid(q.ID, numberRangeMessage(q.Min, q.Max, q.Title))
			}
		case KindText:
			t := a.text(q.ID)
			if q.Required && strings.TrimSpace(t) == "" {
				return invalid(q.ID, "Enter: "+q.Title)
			}
			if len(t) > maxTextLength {
				return invalid(q.ID, "Write a shorter answer for: "+q.Title)
			}
		}
	}
	return nil
}
