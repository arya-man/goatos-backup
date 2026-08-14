package diagnosis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Animal is what GoatOS supplies. None of it is typed by the health manager.
type Animal struct {
	Class   string `json:"klass"`
	Species string `json:"species"`
	Sex     string `json:"sex"`
	Status  string `json:"status"`
	Breed   string `json:"breed"` // deliberately unused: the engine must not use breed
	Shed    string `json:"shed"`
}

func (a Animal) species() string {
	if a.Species == "" {
		return "goat"
	}
	return a.Species
}

func (a Animal) status() string {
	if a.Status == "" {
		return "normal"
	}
	return a.Status
}

func (a Animal) class() string {
	if a.Class == "" {
		return "adult"
	}
	return a.Class
}

// MultiValue is a form field that may carry one value or several. The wire form
// accepts a bare string or an array; both decode to the same set.
type MultiValue []string

func (m *MultiValue) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*m = nil
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var list []string
		if err := json.Unmarshal(data, &list); err != nil {
			return fmt.Errorf("multi-value array: %w", err)
		}
		*m = list
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err != nil {
		return fmt.Errorf("multi-value string: %w", err)
	}
	*m = MultiValue{single}
	return nil
}

func (m MultiValue) has(value string) bool {
	for _, v := range m {
		if v == value {
			return true
		}
	}
	return false
}

func (m MultiValue) hasAny(values ...string) bool {
	for _, v := range values {
		if m.has(v) {
			return true
		}
	}
	return false
}

// except returns the members not in the excluded set, used to ask "is anything
// abnormal here" without treating `normal` / `none` as a finding.
func (m MultiValue) except(excluded ...string) []string {
	skip := toSet(excluded)
	var out []string
	for _, v := range m {
		if v != "" && !skip[v] {
			out = append(out, v)
		}
	}
	return out
}

func (m MultiValue) orDefault(fallback string) MultiValue {
	if len(m) == 0 {
		return MultiValue{fallback}
	}
	return m
}

// Findings is the observation form: one complete head-to-toe pass over one
// animal. Every field is compulsory in the product — sex-hidden fields record
// N/A rather than blank — because a blank cannot distinguish "nobody looked"
// from "normal", and the unexplained-findings channel depends on that
// distinction. Wrong ticks still happen; missing ticks must not.
type Findings struct {
	Temp *float64 `json:"temp"`

	Eating   MultiValue `json:"eating"`
	Activity string     `json:"activity"`

	Breathing MultiValue `json:"breathing"`
	Nasal     bool       `json:"nasal"`

	LeftStomach   MultiValue `json:"left_stomach"`
	FrothyMouth   bool       `json:"frothy_mouth"`
	RumenMovement string     `json:"rumen_movement"`
	Diarrhea      bool       `json:"diarrhea"`

	SkinTent string `json:"skin_tent"`

	CMT       string `json:"cmt"`
	Lactation string `json:"lactation"`
	Udder     string `json:"udder"`
	Vulva     string `json:"vulva"`

	Famacha *int `json:"famacha"`
	Yellow  bool `json:"yellow"`

	Straining string `json:"straining"`
	RedUrine  bool   `json:"red_urine"`
	BodyEdema bool   `json:"body_edema"`

	Competition   bool `json:"competition"`
	StomachInside bool `json:"stomach_inside"`

	Mouth         string     `json:"mouth"`
	Eyes          MultiValue `json:"eyes"`
	LockedJaw     bool       `json:"locked_jaw"`
	Neuro         MultiValue `json:"neuro"`
	RashCharacter string     `json:"rash_character"`
	Hairloss      bool       `json:"hairloss"`

	Leg    string     `json:"leg"`
	Lumps  string     `json:"lumps"`
	Wounds MultiValue `json:"wounds"`

	Flystrike       bool `json:"flystrike"`
	EartagFlystrike bool `json:"eartag_flystrike"`
	EartagWound     bool `json:"eartag_wound"`
	Ticks           bool `json:"ticks"`
}

const defaultTemp = 102.0

func (f Findings) temp() float64 {
	if f.Temp == nil {
		return defaultTemp
	}
	return *f.Temp
}

func (f Findings) eating() MultiValue { return f.Eating.orDefault("normal") }
func (f Findings) wounds() MultiValue { return f.Wounds.orDefault("none") }
func (f Findings) famacha() int {
	if f.Famacha == nil {
		return 0
	}
	return *f.Famacha
}

func (f Findings) notEating() bool { return f.eating().has("not_eating") }
func (f Findings) down() bool      { return f.Activity == "down" }

// Context is the follow-up state a form alone cannot carry. It is what turns a
// second form on the same animal into a reconcile rather than a fresh diagnosis.
type Context struct {
	// Open is the set of disease ids already on a course for this animal.
	Open []string `json:"open"`
	// ClosedRecent is used to tell a relapse from a new problem.
	ClosedRecent []string `json:"closed_recent"`
	// Day is the follow-up day, where day 1 is T0.
	Day *int `json:"day"`

	CMTNegStreak   int  `json:"cmt_neg_streak"`
	NADPrior7d     int  `json:"nad_prior_7d"`
	ShiftedOutDays int  `json:"shifted_out_days"`
	ShedSimilar    *int `json:"shed_similar"`
	DownFollowups  int  `json:"down_followups"`
	Hour           *int `json:"hour"`

	PriorImproved    bool `json:"prior_improved"`
	ProblemImproving bool `json:"problem_improving"`
	AnimalWorsening  bool `json:"animal_worsening"`
	HeatConfirmed    bool `json:"heat_confirmed"`
	Died             bool `json:"died"`
	OffRegister      bool `json:"off_register"`
}

func (c Context) day() int {
	if c.Day == nil {
		return 1
	}
	return *c.Day
}

func (c Context) shedSimilar() int {
	if c.ShedSimilar == nil {
		return 1
	}
	return *c.ShedSimilar
}

func (c Context) hour() int {
	if c.Hour == nil {
		return 8
	}
	return *c.Hour
}

// Reject reasons. An invalid form is not diagnosed at all — a contradictory
// observation is a data-entry defect, and guessing which half is true would put
// an invented finding into a medical record.
const (
	RejectNotEatingWithFeed = "not_eating_with_feed"
	RejectWoundsExclusive   = "wounds_exclusive"
	RejectFemaleStraining   = "female_straining"
	RejectCMTWithoutMilk    = "cmt_without_milk"
)

// validateForm returns the reject reason, or "" when the form is internally
// consistent. These are exclusivity and sex gates only; they never judge whether
// a finding is clinically plausible.
func validateForm(animal Animal, f Findings) string {
	eating := f.eating()
	if eating.has("not_eating") && eating.hasAny("concentrate", "green_feed", "normal") {
		if len(eating.except("not_eating")) > 0 {
			return RejectNotEatingWithFeed
		}
	}
	wounds := f.wounds()
	if wounds.has("none") && len(wounds.except("none")) > 0 {
		return RejectWoundsExclusive
	}
	if animal.Sex == "F" && (f.Straining == "straining" || f.Straining == "no_urine") {
		return RejectFemaleStraining
	}
	if f.Lactation == "no" && (f.CMT == "pos" || f.CMT == "neg") {
		return RejectCMTWithoutMilk
	}
	return ""
}

// derived holds the tokens computed from raw numbers, plus the corrected skin
// tent. It is a value, not a mutation of the form: the emaciation correction
// must not silently rewrite what the manager actually observed.
type derived struct {
	tokens        map[string]bool
	correctedTent string
}

func (d derived) has(token string) bool { return d.tokens[token] }
func (d derived) any() bool             { return len(d.tokens) > 0 }

// deriveTokens converts numbers into meaning.
//
// Two things here are clinical decisions, not arithmetic:
//
//   - `pant` is NOT fever and NOT heat stress. Panting opens neither; it raises a
//     Director-confirm flag instead, because heat and infection look alike and
//     only one of them is the engine's call.
//   - EMACIATION CORRECTION: a skinny animal tents slowly without being
//     dehydrated. When the flank is sunken, a >4s tent is read as 2-4s BEFORE the
//     fluids emergency is considered, so an emaciated animal does not trigger an
//     emergency drip on the strength of its body condition.
func deriveTokens(f Findings) derived {
	d := derived{tokens: map[string]bool{}}

	temp := f.temp()
	switch {
	case temp > 106.0:
		d.tokens["HIGH_FEVER"] = true
		d.tokens["FEVER"] = true
	case temp > 103.5:
		d.tokens["FEVER"] = true
	}
	if temp < 100.0 {
		d.tokens["HYPOTHERMIA"] = true
	}

	tent := f.SkinTent
	if tent == "" {
		tent = "lt2"
	}
	if f.StomachInside && tent == "gt4" {
		tent = "2-4"
	}
	d.correctedTent = tent
	if tent == "gt4" {
		d.tokens["TENT_GT4"] = true
	}
	return d
}

// buildEvidence maps the animal and the form onto register vocabulary tokens.
//
// Production maps the real form to these SAME tokens. The register, the
// validators and the acceptance catalog all speak this vocabulary, so a token
// invented here that no rule references is dead evidence, and a token a rule
// expects but this never emits is a silently unreachable rule.
func buildEvidence(animal Animal, f Findings, d derived) map[string]bool {
	ev := map[string]bool{}
	for tok := range d.tokens {
		ev[tok] = true
	}

	ev["species:"+animal.species()] = true
	if animal.Sex != "" {
		ev["sex:"+animal.Sex] = true
	}
	ev["status:"+animal.status()] = true

	if f.notEating() {
		ev["eating:not_eating"] = true
	}

	switch f.Activity {
	case "down":
		ev["activity:not_able_to_stand"] = true
	case "front_knees":
		ev["activity:front_leg_knees"] = true
	case "weak", "limping", "back_leg_drag":
		ev["activity:"+f.Activity] = true
	}

	stomach := f.LeftStomach.orDefault("normal")
	if stomach.has("bloating") {
		ev["left_stomach:bloating"] = true
	}
	if stomach.has("acidosis") {
		ev["left_stomach:acidosis"] = true
	}
	if f.FrothyMouth {
		ev["frothy_mouth"] = true
	}
	if f.Diarrhea {
		ev["diarrhea"] = true
	}
	if f.RumenMovement == "not_felt" {
		ev["rumen_movement:not_felt"] = true
	}

	for _, b := range []string{"cough", "labored", "fast", "pant"} {
		if f.Breathing.has(b) {
			ev["breathing:"+b] = true
		}
	}
	if f.Nasal {
		ev["nasal_discharge"] = true
	}

	switch f.CMT {
	case "pos":
		ev["cmt:positive"] = true
	case "neg":
		ev["cmt:negative"] = true
	}

	switch f.Lactation {
	case "no":
		ev["lactation:no"] = true
	case "pus":
		ev["lactation:pus"] = true
		ev["has_milk"] = true
	case "milk", "colostrum", "water":
		ev["has_milk"] = true
	}

	switch f.Udder {
	case "swollen_hard", "rashes", "wound", "lumps":
		ev["udder:"+f.Udder] = true
	}

	switch f.famacha() {
	case 3, 4, 5:
		ev[fmt.Sprintf("famacha:%d", f.famacha())] = true
	}
	if f.Yellow {
		ev["famacha:yellow"] = true
	}

	switch f.Straining {
	case "no_urine":
		ev["straining_urine:no_urine_passed"] = true
	case "straining":
		ev["straining_urine:straining"] = true
	}
	if f.RedUrine {
		ev["misc:red_urine"] = true
	}
	if f.BodyEdema {
		ev["misc:body_edema"] = true
	}
	if f.Competition {
		ev["misc:competition"] = true
	}
	if f.StomachInside {
		ev["misc:stomach_inside"] = true
	}

	for _, n := range f.Neuro {
		if n != "" {
			ev["neuro:"+n] = true
		}
	}

	switch f.Vulva {
	case "discharge_bad_smell", "foul_smelling":
		ev["vulva:foul_smelling"] = true
	case "pus":
		ev["vulva:pus"] = true
	case "prolapse", "tissue_protruding":
		ev["vulva:tissue_protruding"] = true
	}

	if f.Mouth == "orf_scabs" {
		ev["mouth:orf_scabs"] = true
	}
	switch f.RashCharacter {
	case "nodular":
		ev["rash_character:nodular"] = true
	case "flat_itchy":
		ev["rash_character:flat_itchy"] = true
		ev["rashes:present"] = true
	}
	if f.Hairloss {
		ev["skin_coat:hairloss_body"] = true
	}

	for _, e := range f.Eyes.except("normal") {
		ev["eyes:"+e] = true
	}

	if f.LockedJaw {
		ev["locked_jaw"] = true
	}

	switch f.Leg {
	case "foot_rot", "fracture", "arthritis", "normal":
		ev["leg:"+f.Leg] = true
	}

	if f.Flystrike {
		ev["flystrike"] = true
	}
	if f.EartagFlystrike {
		ev["eartag:flystrike"] = true
	}
	if f.EartagWound {
		ev["eartag:wound"] = true
	}

	switch f.Lumps {
	case "neck", "body":
		ev["lumps:"+f.Lumps] = true
	}

	for _, w := range f.wounds().except("none", "no") {
		ev["wounds:"+w] = true
		ev["wounds:present"] = true
	}
	// An udder wound and an eartag wound are wounds even though they are
	// recorded on their own fields.
	if f.Udder == "wound" || f.EartagWound {
		ev["wounds:present"] = true
	}

	if f.Ticks {
		ev["skin_coat:ticks"] = true
	}

	switch d.correctedTent {
	case "gt4":
		ev["skin_tent:>4s"] = true
		ev["TENT_GT4"] = true
	case "2-4", "2-4s", "2–4s":
		ev["skin_tent:2-4s"] = true
	}

	return ev
}

func sortedTokens(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
