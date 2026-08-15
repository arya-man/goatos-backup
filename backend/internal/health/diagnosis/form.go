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

	// Stage is the milk/weaning sub-stage: K0 colostrum, K1 milk-bar training,
	// K2 free-choice bar, K3 weaning. It comes from GoatOS, NOT from the form,
	// for the same reason species and sex do: it decides how a missed feed is
	// read, and a manager who could type it could turn a real refusal into a
	// learner's miss.
	//
	// Empty for adults and fattening kids, which have no sub-stage.
	Stage string `json:"stage"`

	// WeightKg is carried because the source registers band fattening kids by
	// weight. No rule reads it yet; it is on the struct so the catalogs decode
	// without a loader that silently drops it.
	WeightKg *float64 `json:"weight_kg"`
}

// isKid reports whether this animal is on one of the three kid registers. The
// kid slices share a form, a temperature band and a crash ladder; where they
// differ, the specific class is tested instead.
func (a Animal) isKid() bool {
	switch a.class() {
	case ClassKidMilk, ClassKidWeaning, ClassKidFattening:
		return true
	}
	return false
}

// isMilkBar reports the K2 free-choice stage, where the animal has already
// proved it drinks unaided. A K2 refusal is therefore a problem on the first
// miss, while the same first miss on K1 is still training.
func (a Animal) isMilkBar() bool { return a.class() == ClassKidMilk && a.Stage == "K2" }

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

// Flag is a yes/no observation that may also arrive as a descriptive string.
//
// Diarrhea is the case that needs it: the form records presence, but an observer
// may report `bloody`. Blood is a severity detail, NOT a different diagnosis --
// the spec is explicit that bloody diarrhea is still Diarrhea and must not be
// read as coccidiosis. Decoding it as a truthy Flag keeps that rule in the type
// rather than leaving a `"bloody"` string to be tested for somewhere downstream
// and eventually forgotten.
type Flag struct {
	Set    bool
	Detail string
}

func (fl *Flag) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	switch trimmed {
	case "null":
		*fl = Flag{}
		return nil
	case "true":
		*fl = Flag{Set: true}
		return nil
	case "false":
		*fl = Flag{}
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("flag: want bool or string, got %s", trimmed)
	}
	// An explicit negative spelling is still a negative. Anything else is a
	// present finding with a detail attached.
	switch s {
	case "", "no", "none", "normal", "false":
		*fl = Flag{}
	default:
		*fl = Flag{Set: true, Detail: s}
	}
	return nil
}

func (fl Flag) MarshalJSON() ([]byte, error) {
	if fl.Detail != "" {
		return json.Marshal(fl.Detail)
	}
	return json.Marshal(fl.Set)
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
	Diarrhea      Flag       `json:"diarrhea"`

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

	// --- Kids form. These rows are HIDDEN on the adult form, and hidden means
	// ABSENT, not N/A: the adult form's convention of recording N/A for a
	// sex-hidden row exists so a blank cannot mean "nobody looked", but a row
	// that is not on this animal's form was never a question to answer.

	// Suckle is the finger test, and it is a treatment gate rather than a
	// symptom: a kid that sucks can be given milk by mouth, and one that cannot
	// must never be. It is asked on all three kid slices for that reason.
	Suckle string `json:"suckle"`

	// Responsiveness is the kid's own dullness axis. It is deliberately
	// non-specific: dull alone names no disease and must not enter `explained`.
	Responsiveness string `json:"responsiveness"`

	// Navel is milk-kids only; it has closed and healed by weaning.
	Navel string `json:"navel"`

	// Landing is the drop test, and it is the whole point of the milk-kid form:
	// from about 20 cm a well kid lands like Spider-Man, a floppy one barely
	// stays up or falls. It is the ONLY way floppy kid is caught while the animal
	// is still standing and still cheap to treat.
	//
	// Compulsory when the kid is standing, and `na` when it is already down --
	// you do not drop a recumbent kid to see what happens. Weaning and fattening
	// never ask it, and sending it there is a reject rather than an ignored field.
	Landing string `json:"landing"`

	// MilkIntake is the K2 bar reading: `normal` or `not_drinking`. On K1 and K3
	// the drinking axis is the refusal COUNT instead, because those are counted
	// sessions and this is free choice.
	//
	// Multi-valued for the same reason `eating` is: it is the milk analogue of
	// the feed row, and `not_drinking` together with `normal` is the same
	// contradiction that `not_eating` with `normal` is on the adult form. A
	// single-valued field could not express it, which would leave the documented
	// reject unreachable rather than merely unexercised.
	MilkIntake MultiValue `json:"milk_intake"`

	// RefusalsToday is how many feeds were refused today, with carry-forward
	// already applied by GoatOS. Milk kids run 0-3 (three bar sessions), weaning
	// 0-2 (two measured bottles).
	//
	// A pointer because zero refusals and "not asked" are different facts: zero
	// is a kid that drank, and on K1 the count is compulsory, so a missing value
	// is a reject rather than a quiet zero.
	RefusalsToday *int `json:"refusals_today"`

	// Session is which feed this observation belongs to: 1-3 on the milk bar,
	// 1-2 (morning/evening) on weaning.
	Session *int `json:"session"`
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

	// Kids form.
	RejectNotDrinkingWithMilk = "not_drinking_with_milk"
	RejectLandingRequired     = "landing_required"
	RejectLandingWhenDown     = "landing_when_down"
	RejectRefusalsRequired    = "refusals_today_required"
	RejectSessionRequired     = "session_required"
	RejectLandingNotOnWeaning = "landing_not_on_weaning"
	RejectNavelNotOnWeaning   = "navel_not_on_weaning"

	// RejectRegisterClassMismatch means a register was asked to diagnose an
	// animal of a class it does not serve. It is a wiring defect, not a form
	// defect, and it is surfaced rather than silently corrected.
	RejectRegisterClassMismatch = "register_class_mismatch"
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
	if animal.isKid() {
		return validateKidsForm(animal, f)
	}
	return ""
}

// validateKidsForm enforces the rows the kids form actually asks for.
//
// Two of these are structural rather than clinical, and they are the reason a
// wrong row is a REJECT and not an ignored field. Landing is not on the weaning
// or fattening form at all, so a landing value arriving on one of those means
// the wrong form was rendered or the wrong animal was opened; accepting it would
// let a floppy-kid finding be recorded against a slice where floppy cannot exist.
// The refusal count is compulsory where feeds are counted, because a missing
// count read as zero would silently turn a kid that refused every bottle into a
// kid that drank.
func validateKidsForm(animal Animal, f Findings) string {
	if f.MilkIntake.has("not_drinking") && f.MilkIntake.has("normal") {
		return RejectNotDrinkingWithMilk
	}

	switch animal.class() {
	case ClassKidMilk:
		// The drop test is compulsory while the kid is on its feet, and refused
		// once it is down: dropping a recumbent kid to grade its landing is the
		// one thing this test must never cause.
		standing := f.Activity == "" || f.Activity == "standing" ||
			f.Activity == "weak" || f.Activity == "limping"
		switch {
		case f.Activity == "down" && f.Landing != "na":
			return RejectLandingWhenDown
		case standing && !isOneOf(f.Landing, "spiderman", "barely", "falls"):
			return RejectLandingRequired
		}

		// K1 counts three bar sessions, so the count is compulsory there. K0 and
		// K2 may omit it, but a value that IS sent must still be in range.
		if animal.Stage == "K1" {
			if f.RefusalsToday == nil || *f.RefusalsToday < 0 || *f.RefusalsToday > 3 {
				return RejectRefusalsRequired
			}
		} else if f.RefusalsToday != nil && (*f.RefusalsToday < 0 || *f.RefusalsToday > 3) {
			return RejectRefusalsRequired
		}
		if f.Session != nil && (*f.Session < 1 || *f.Session > 3) {
			return RejectSessionRequired
		}

	case ClassKidWeaning:
		if f.Landing != "" {
			return RejectLandingNotOnWeaning
		}
		if f.Navel != "" {
			return RejectNavelNotOnWeaning
		}
		// Two measured bottles, both counted.
		if f.RefusalsToday == nil || *f.RefusalsToday < 0 || *f.RefusalsToday > 2 {
			return RejectRefusalsRequired
		}
		if f.Session != nil && (*f.Session < 1 || *f.Session > 2) {
			return RejectSessionRequired
		}
	}
	return ""
}

func isOneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}

// milkProblem reports whether this observation is a not-drinking ENERGY problem,
// which is a different question from "did the kid miss a feed".
//
// The three slices count a miss differently, and the differences are the whole
// clinical point:
//
//   - K1 is learning at the bar across three sessions. ONE miss is gut capacity
//     or inexperience, not a disease. Two is a problem.
//   - K2 has already earned free choice by drinking unaided. Any miss is a
//     problem on the FIRST one, because this kid has proved it can drink.
//   - K3 (weaning) has two measured 200 ml bottles. Any miss is a problem.
//
// Reading K2 or K3 as a learner's miss under-treats a kid that has demonstrably
// stopped drinking; reading a K1 first miss as a disease sends a healthy kid to
// the ward. Neither is a rounding error.
func milkProblem(animal Animal, f Findings) bool {
	n := f.RefusalsToday
	notDrinking := f.MilkIntake.has("not_drinking")

	switch animal.class() {
	case ClassKidWeaning:
		return notDrinking || (n != nil && *n >= 1)
	case ClassKidMilk:
		if animal.isMilkBar() {
			return notDrinking || (n != nil && *n >= 1)
		}
		if n != nil {
			return *n >= 2
		}
		return notDrinking
	}
	return notDrinking
}

// refusals is the refusal count, or zero when the slice does not count feeds.
func (f Findings) refusals() int {
	if f.RefusalsToday == nil {
		return 0
	}
	return *f.RefusalsToday
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
func deriveTokens(animal Animal, f Findings) derived {
	d := derived{tokens: map[string]bool{}}

	temp := f.temp()
	// The fever cut differs by class at exactly one value. A kid at 103.5degF IS
	// febrile; an adult at 103.5 is not. It is a single tenth of a degree and it
	// decides whether a kid is treated at all, so the band is taken from the
	// class rather than shared.
	febrile := temp > 103.5
	if animal.isKid() {
		febrile = temp >= 103.5
	}
	switch {
	case temp > 106.0:
		d.tokens["HIGH_FEVER"] = true
		d.tokens["FEVER"] = true
	case febrile:
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
	if f.Diarrhea.Set {
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

	// `none` / `normal` are the absence of a neuro sign, not a sign named
	// "none". Emitting them would put a normal finding into the evidence set,
	// where it could anchor a clause and surface as an unexplained finding.
	for _, n := range f.Neuro.except("none", "normal") {
		ev["neuro:"+n] = true
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

	addKidEvidence(ev, animal, f)
	return ev
}

// addKidEvidence emits the rows that exist only on the kids form.
//
// The one non-obvious mapping is milk. A milk PROBLEM (not merely a missed feed
// -- see milkProblem) also emits `eating:not_eating`, because on a milk kid the
// bar IS the diet and refusing it is being off feed. Weaning is the deliberate
// exception: a K3 kid that skips a 200 ml bottle is still eating concentrate,
// so calling it off-feed would misread a missed bottle as a rumen problem and,
// downstream, hand it an acidosis emergency it does not have.
func addKidEvidence(ev map[string]bool, animal Animal, f Findings) {
	if !animal.isKid() {
		return
	}

	if animal.Stage != "" {
		ev["stage:"+animal.Stage] = true
	}

	if milkProblem(animal, f) {
		ev["milk_intake:not_drinking"] = true
		if animal.class() != ClassKidWeaning {
			ev["eating:not_eating"] = true
		}
	} else if animal.Stage != "K1" && f.MilkIntake.has("reduced") {
		ev["milk_intake:reduced"] = true
	}

	if isOneOf(f.Suckle, "present", "absent") {
		ev["suckle:"+f.Suckle] = true
	}
	if isOneOf(f.Navel, "wet", "swollen", "painful") {
		ev["navel:"+f.Navel] = true
	}
	if isOneOf(f.Responsiveness, "alert", "dull", "unresponsive") {
		ev["responsiveness:"+f.Responsiveness] = true
	}
	if isOneOf(f.Landing, "spiderman", "barely", "falls", "na") {
		ev["landing:"+f.Landing] = true
	}
}

func sortedTokens(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
