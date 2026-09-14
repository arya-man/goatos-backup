package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The Procurement SOP questionnaire (maintainer decision 2026-09-13, from the farm's
// "Procurement SOP" Google Form).
//
// This is the ONE definition of the per-animal inspection: the phone renders these questions
// in this order, the write validates the answers against them, and the CEO's web review shows
// each answer under its own question text. The wording is the SOP's own, so the inspector on
// the farm reads the same instruction they were trained on ("REJECT if the animal is visibly
// empty…"). Changing a question is a version bump here, never a client release.
//
// Vendor and batch are NOT questions: they live on the load. The SOP's "Goat ID" is the
// animal's temporary tag. Species is kept (the SOP is goat-only; the farm buys sheep too).

// QuestionnaireVersion is stamped on every answered candidate row.
const QuestionnaireVersion = 1

// Question kinds. The phone renders each kind with its own widget.
const (
	KindChoice  = "choice"  // one option (radio)
	KindMulti   = "multi"   // several options (checkbox)
	KindText    = "text"    // short free text
	KindNumber  = "number"  // decimal number, optional min/max
	KindMedia   = "media"   // one or more in-app captures into a named slot
	KindSection = "section" // heading only, no answer
)

// Media slots. These are the animal_purchase_candidate_media.slot values.
const (
	SlotTeeth       = "teeth"
	SlotWeight      = "weight"
	SlotTemperature = "temperature"
	SlotAnimal      = "animal"
	SlotSuspicious  = "suspicious"
	SlotUdder       = "udder"
)

// Field verdicts: the inspector's OWN recommendation, never the decision.
const (
	FieldVerdictSelected = "selected"
	FieldVerdictOnHold   = "on_hold"
)

// Question is one item of the questionnaire, wire-shaped for the phone and the web.
type Question struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Hint     string   `json:"hint,omitempty"`
	Required bool     `json:"required"`
	Options  []Option `json:"options,omitempty"`
	// AllowOther lets a choice carry free text under the "other" option (the SOP's "If Yes,
	// please mention area in others").
	AllowOther bool `json:"allow_other,omitempty"`
	// Media questions.
	Slot     string   `json:"slot,omitempty"`
	MaxFiles int      `json:"max_files,omitempty"`
	Accepts  []string `json:"accepts,omitempty"` // "photo", "video"
	// Number questions.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Unit string   `json:"unit,omitempty"`
	// OnlyIf hides the question unless another question holds the given value.
	OnlyIf *Condition `json:"only_if,omitempty"`
}

// Condition is a single-question dependency.
type Condition struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

func f(v float64) *float64 { return &v }

var yesNo = []Option{{"yes", "Yes"}, {"no", "No"}}
var noYes = []Option{{"no", "No"}, {"yes", "Yes"}}

// Questionnaire returns the SOP in display order. A fresh slice each call: callers may not
// mutate the catalog.
func Questionnaire() []Question {
	return []Question{
		// ---- Identity and condition -------------------------------------------------------
		{ID: "species", Kind: KindChoice, Title: "Goat or sheep", Required: true, Options: Species()},
		{ID: "goat_id", Kind: KindText, Title: "Goat ID", Hint: "The tag or number the vendor uses for this animal.", Required: true},
		{ID: "well_fed", Kind: KindChoice, Title: "Is the animal well fed and walking actively?", Hint: "REJECT if the animal is visibly empty or walking weakly. Yes if well fed, No if ribs are visible.", Required: true, Options: yesNo},
		// A typed count, not a 0/2/4/6/8 pick (maintainer 2026-09-14): half-formed teeth make
		// odd counts real, and a goat has at most eight incisors.
		{ID: "teeth", Kind: KindNumber, Title: "How many fully and half formed teeth?", Hint: "If its mouth can't be opened, REJECT IMMEDIATELY.", Required: true, Min: f(0), Max: f(8)},
		{ID: "teeth_media", Kind: KindMedia, Title: "Photo of teeth", Required: true, Slot: SlotTeeth, MaxFiles: 5, Accepts: []string{"photo", "video"}},
		{ID: "sex", Kind: KindChoice, Title: "Gender of the animal?", Required: true, Options: Sexes()},
		{ID: "pregnant", Kind: KindChoice, Title: "Is the animal pregnant?", Required: true, Options: noYes, OnlyIf: &Condition{QuestionID: "sex", Value: SexFemale}},
		{ID: "weight_kg", Kind: KindNumber, Title: "Weight of the animal in KG", Unit: "kg", Min: f(0.5), Max: f(300)},
		{ID: "weight_media", Kind: KindMedia, Title: "Weight of the animal media", Hint: "The scale reading with the animal on it.", Slot: SlotWeight, MaxFiles: 1, Accepts: []string{"photo", "video"}},
		{ID: "height_cm", Kind: KindNumber, Title: "Height of the animal in CM", Hint: "Measure from the front knee to where the neck meets the body.", Unit: "cm", Min: f(10), Max: f(200)},
		{ID: "rectal_temp_c", Kind: KindNumber, Title: "Rectal temperature of the goat?", Unit: "°C", Required: true, Min: f(30), Max: f(45)},
		{ID: "temperature_media", Kind: KindMedia, Title: "Rectal temperature media", Slot: SlotTemperature, MaxFiles: 1, Accepts: []string{"photo", "video"}},
		{ID: "animal_media", Kind: KindMedia, Title: "Goat photo and video with face, udder, body and activity", Hint: "Walk around the animal so the whole body is seen.", Required: true, Slot: SlotAnimal, MaxFiles: 5, Accepts: []string{"photo", "video"}},

		// ---- Face visual productivity check ------------------------------------------------
		{ID: "sec_face", Kind: KindSection, Title: "Face visual productivity check"},
		{ID: "anaemic", Kind: KindChoice, Title: "Is the animal anaemic?", Required: true, Options: yesNo},
		{ID: "mouth_breathing", Kind: KindChoice, Title: "Is it breathing through its mouth?", Hint: "Yes for mouth, No for nose.", Required: true, Options: yesNo},
		{ID: "watery_eyes", Kind: KindChoice, Title: "Does it have watery eyes?", Hint: "If so, is it clear and watery or yellow and mucus like?", Required: true,
			Options: []Option{{"clear", "Yes - clear and watery"}, {"yellow", "Yes - yellow and mucus like"}, {"no", "No"}}},
		{ID: "eye_colour", Kind: KindChoice, Title: "Does it have red or cloudy eyes?", Required: true,
			Options: []Option{{"red", "Yes - red eyes"}, {"cloudy", "Yes - cloudy eyes"}, {"no", "No"}}},
		{ID: "nasal_discharge", Kind: KindChoice, Title: "Does it have a nasal discharge?", Hint: "If so, is it clear and runny or yellow and mucus like?", Required: true,
			Options: []Option{{"clear", "Yes - clear and runny"}, {"yellow", "Yes - yellow and mucus like"}, {"no", "No"}}},
		{ID: "face_scabs", Kind: KindChoice, Title: "Any signs of scabby skin or rashes on the face, even one small bump?", Hint: "Places: eyes, nose, outside mouth, ears, inside mouth around jaws and tongue.", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention the area"}}, AllowOther: true},

		// ---- Body visual productivity check ------------------------------------------------
		{ID: "sec_body", Kind: KindSection, Title: "Body visual productivity check", Hint: "Checks for what is visible on the body, marked and rejected as per the SOP."},
		{ID: "acidosis", Kind: KindChoice, Title: "Does the animal have acidosis?", Hint: "Is there a fluid sensation on pressing its stomach? It should feel doughy, like kneaded atta.", Required: true, Options: yesNo},
		{ID: "diarrhea", Kind: KindChoice, Title: "Any signs of diarrhea, present or past?", Hint: "Check below its tail and look for traces around both back legs.", Required: true, Options: yesNo},
		{ID: "ticks_hair_loss", Kind: KindChoice, Title: "Any presence of ticks or patches of hair loss on the body?", Hint: "Face | Neck | Visible trunk | Bottom trunk | Front legs (left and right + hoofs) | Rear legs (left and right + hoofs)", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention where"}}, AllowOther: true},
		{ID: "wounds", Kind: KindChoice, Title: "Any wounds or physical injuries on the body?", Hint: "Face | Neck | Visible trunk | Bottom trunk | Front legs (left and right + hoofs) | Rear legs (left and right + hoofs)", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention where"}}, AllowOther: true},
		{ID: "body_scabs", Kind: KindChoice, Title: "Any signs of scabby skin or rashes on the body, even one small bump?", Hint: "Places: neck and visible trunk, bottom of trunk, front legs and below hoof (both), rear legs and below hoof (both).", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention where"}}, AllowOther: true},
		{ID: "lumps", Kind: KindChoice, Title: "Any signs of lumps on the face and body?", Hint: "Tick each of the points 1-14 and mention the numbers where a lump is present.", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention where"}}, AllowOther: true},
		{ID: "arthritis", Kind: KindChoice, Title: "Any signs of arthritis in the knees?", Hint: "Look for swelling in the knees.", Required: true,
			Options: []Option{{"no", "No"}, {"other", "Yes, please mention where"}}, AllowOther: true},
		{ID: "suspicious_media", Kind: KindMedia, Title: "If anything is suspicious, add a picture of it", Slot: SlotSuspicious, MaxFiles: 5, Accepts: []string{"photo", "video"}},

		// ---- Udder / testicles ---------------------------------------------------------------
		{ID: "sec_udder", Kind: KindSection, Title: "Udder or testicles"},
		{ID: "udder_media", Kind: KindMedia, Title: "Udder or testicles media", Required: true, Slot: SlotUdder, MaxFiles: 1, Accepts: []string{"photo", "video"}},
		{ID: "milk_yield", Kind: KindText, Title: "Milk yield", OnlyIf: &Condition{QuestionID: "sex", Value: SexFemale}},
		{ID: "udder_state", Kind: KindMulti, Title: "Is the udder or testicle normal or has issues?", Required: true,
			Options: []Option{{"normal", "Normal"}, {"swollen", "Swollen"}, {"rashes", "Rashes"}, {"lumps", "Lumps"}, {"wounds", "Wounds"}, {"teats_not_down", "Teats not pointing down"}, {"other", "Other"}}, AllowOther: true},
		{ID: "lactating", Kind: KindChoice, Title: "Is it lactating?", Required: true, Options: yesNo, OnlyIf: &Condition{QuestionID: "sex", Value: SexFemale}},
		{ID: "mastitis", Kind: KindChoice, Title: "If lactating, results of mastitis test are?", Options: []Option{{"positive", "Positive"}, {"negative", "Negative"}}, OnlyIf: &Condition{QuestionID: "lactating", Value: "yes"}},
		{ID: "teats", Kind: KindChoice, Title: "How many teats are present?", Required: true, Options: []Option{{"1", "1"}, {"2", "2"}, {"3", "3"}, {"4", "4"}}, OnlyIf: &Condition{QuestionID: "sex", Value: SexFemale}},
		{ID: "teat_discharge", Kind: KindChoice, Title: "Any pus like discharge from the teats that does not look like milk or colostrum?", Required: true, Options: yesNo, OnlyIf: &Condition{QuestionID: "sex", Value: SexFemale}},
		{ID: "scrotum_cm", Kind: KindNumber, Title: "Male scrotum circumference in CM", Unit: "cm", Min: f(5), Max: f(60), OnlyIf: &Condition{QuestionID: "sex", Value: SexMale}},

		// ---- Field verdict --------------------------------------------------------------------
		{ID: "sec_decision", Kind: KindSection, Title: "Your verdict", Hint: "Your recommendation on the farm. The CEO decides on the web."},
		{ID: "field_verdict", Kind: KindChoice, Title: "Decision", Required: true, Options: []Option{{FieldVerdictSelected, "Selected"}, {FieldVerdictOnHold, "On Hold"}}},

		// Extras the SOP does not carry but the register benefits from.
		{ID: "breed", Kind: KindText, Title: "Breed"},
		{ID: "notes", Kind: KindText, Title: "Note"},
	}
}

// questionIndex is the catalog by id, built once.
var questionIndex = func() map[string]Question {
	m := map[string]Question{}
	for _, q := range Questionnaire() {
		m[q.ID] = q
	}
	return m
}()

// QuestionByID looks a question up; ok is false for an unknown id.
func QuestionByID(id string) (Question, bool) {
	q, ok := questionIndex[id]
	return q, ok
}

// MediaSlots lists the media questions in order.
func MediaSlots() []Question {
	var out []Question
	for _, q := range Questionnaire() {
		if q.Kind == KindMedia {
			out = append(out, q)
		}
	}
	return out
}

// Answers is the raw {question_id: value} map as the phone sends it. Choice answers are the
// option value; an "other" free text rides under "<id>_other"; multi answers are arrays of
// option values; numbers are JSON numbers or numeric strings; text is text.
type Answers map[string]json.RawMessage

// MediaRefs is {slot: [proof refs in position order]}.
type MediaRefs map[string][]string

// Applies reports whether a question is shown/required given the other answers.
func (a Answers) Applies(q Question) bool {
	if q.OnlyIf == nil {
		return true
	}
	return a.choice(q.OnlyIf.QuestionID) == q.OnlyIf.Value
}

func (a Answers) choice(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// Validate checks the answers and media against the catalog: every applicable required question
// answered, every choice within its options, every number in range, every required media slot
// filled and no slot over its file limit. The first problem is returned naming the question.
// ValidateAnswers checks answers against the LEGACY catalog. Production validates against the
// candidate's own SOP version (Catalog.ValidateAnswers); this stays for the golden tests.
func ValidateAnswers(a Answers, media MediaRefs) error {
	return Catalog{Version: QuestionnaireVersion, Questions: Questionnaire()}.ValidateAnswers(a, media)
}

// ValidateAnswers checks answers and media against THIS catalog: required questions present
// (when their only_if holds), choices among the offered options, numbers in range, media slots
// known, mandatory slots filled and no slot over its file cap.
func (c Catalog) ValidateAnswers(a Answers, media MediaRefs) error {
	for _, q := range c.Questions {
		if q.Kind == KindSection || !a.Applies(q) {
			continue
		}
		switch q.Kind {
		case KindMedia:
			refs := media[q.Slot]
			if q.Required && len(refs) == 0 {
				return invalid(q.ID, "Add the required photo or video: "+q.Title)
			}
			if q.MaxFiles > 0 && len(refs) > q.MaxFiles {
				return invalid(q.ID, fmt.Sprintf("At most %d files for: %s", q.MaxFiles, q.Title))
			}
			for _, r := range refs {
				if strings.TrimSpace(r) == "" {
					return invalid(q.ID, "A capture did not finish uploading. Record it again.")
				}
			}
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
				return invalid(q.ID, "Say where, for: "+q.Title)
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
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return invalid(q.ID, "Enter a number for: "+q.Title)
			}
			if (q.Min != nil && n < *q.Min) || (q.Max != nil && n > *q.Max) {
				return invalid(q.ID, fmt.Sprintf("Enter a value between %g and %g for: %s", nz(q.Min), nz(q.Max), q.Title))
			}
		case KindText:
			t := a.text(q.ID)
			if q.Required && strings.TrimSpace(t) == "" {
				return invalid(q.ID, "Enter: "+q.Title)
			}
			if len(t) > maxTextLength {
				return invalid(q.ID, "Too long: "+q.Title)
			}
		}
	}
	for slot := range media {
		known := false
		for _, q := range c.MediaSlots() {
			if q.Slot == slot {
				known = true
			}
		}
		if !known {
			return invalid("media", "Unknown media slot.")
		}
	}
	return nil
}

func nz(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func hasOption(options []Option, v string) bool {
	for _, o := range options {
		if o.Value == v {
			return true
		}
	}
	return false
}

func (a Answers) text(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return strings.Trim(string(raw), `"`)
	}
	return s
}

func (a Answers) multi(id string) ([]string, bool) {
	raw, ok := a[id]
	if !ok {
		return nil, false
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		return nil, false
	}
	for i := range vals {
		vals[i] = strings.ToLower(strings.TrimSpace(vals[i]))
	}
	return vals, true
}

// number reads a JSON number or a numeric string; present=false when absent or blank.
func (a Answers) number(id string) (float64, bool, error) {
	raw, ok := a[id]
	if !ok {
		return 0, false, nil
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, true, err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	return n, true, err
}

// Number returns an answered number (nil when absent), for the typed columns.
func (a Answers) Number(id string) *float64 {
	n, present, err := a.number(id)
	if !present || err != nil {
		return nil
	}
	return &n
}

// Multi returns an answered multi-choice list.
func (a Answers) Multi(id string) ([]string, bool) { return a.multi(id) }

// Choice returns an answered choice value ("" when absent).
func (a Answers) Choice(id string) string { return a.choice(id) }

// Text returns an answered text ("" when absent).
func (a Answers) Text(id string) string { return strings.TrimSpace(a.text(id)) }

// FieldVerdictLabel is the chip the web and the phone show for the inspector's verdict.
func FieldVerdictLabel(v string) string {
	switch v {
	case FieldVerdictSelected:
		return "Selected on farm"
	case FieldVerdictOnHold:
		return "On hold on farm"
	default:
		return ""
	}
}

// AnswerLabel renders an answer for display: the option label for choices (with the "other"
// text appended), the labels joined for multi, the number with its unit, or the text.
func AnswerLabel(q Question, a Answers) string {
	switch q.Kind {
	case KindChoice:
		v := a.choice(q.ID)
		if v == "" {
			return ""
		}
		label := labelOf(q.Options, v)
		if v == "other" && q.AllowOther {
			// The option's form wording is an instruction ("Yes, please mention where"); the
			// reviewer reads the fact: "Yes · left rear hoof".
			if other := a.Text(q.ID + "_other"); other != "" {
				return "Yes · " + other
			}
			return "Yes"
		}
		return label
	case KindMulti:
		vals, _ := a.multi(q.ID)
		var labels []string
		for _, v := range vals {
			labels = append(labels, labelOf(q.Options, v))
		}
		out := strings.Join(labels, ", ")
		if other := a.Text(q.ID + "_other"); other != "" {
			out += " · " + other
		}
		return out
	case KindNumber:
		n := a.Number(q.ID)
		if n == nil {
			return ""
		}
		s := strconv.FormatFloat(*n, 'f', -1, 64)
		if q.Unit != "" {
			return s + " " + q.Unit
		}
		return s
	case KindText:
		return a.Text(q.ID)
	}
	return ""
}
