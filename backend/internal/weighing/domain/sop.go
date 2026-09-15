package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// WEIGHING SOP (maintainer decision 2026-09-15, docs/decisions/weighing-sop.md).
//
// The Weighing Session SOP stops being a library document: the rules a weighing task runs
// under are the `weighing` section of the PUBLISHED `weighing.session` SOP version, authored
// on /weighing/sops --
//
//   - PLANNING: which capture modes the planner may pick and the default cap per day;
//   - FEED & WATER REMOVAL: whether the evening-before removal is REQUIRED on every task,
//     OPTIONAL (the planner decides per task) or OFF, the instruction shown on the removal
//     card, the copy on the two proof slots, and any extra QUESTIONS the removal operator
//     answers when submitting a pen;
//   - CAPTURE: the per-animal video (locked on -- it is the evidence the verifier reviews)
//     and how many videos a lump-sum pen carries.
//
// A task is stamped with the SOP version it was PLANNED on (weighing_campaigns.sop_version)
// and runs on that version to the end: publishing a new version changes the next task planned,
// never a task already on the calendar (the pin-at-start rule the herd-operations and
// procurement SOPs use). A tenant with no published version runs the seeded document.
//
// Weighing is ISOLATED (AGENTS.md): this package never names sop_versions. The rules arrive
// through ports.SOPRulesSource, whose Postgres adapter lives OUTSIDE backend/internal/weighing.
//
// The scan-and-submit rule set is NOT authorable here and never will be from this document:
// free-flow capture, no roster, the no-duplicate-scan rule, evidence-grain verification, the
// approve-carries-the-weight correction and the unconditional close gate are maintainer locks.
// The document says what the farm asks of the operator, not what the kernel does with it.

// WeighingSOPSchemaVersion is the document's own version tag (not the SOP version number).
const WeighingSOPSchemaVersion = "goatos.sop-weighing.v1"

// SOPCodeWeighingSession is the SOP library code the weighing rules are published under.
const SOPCodeWeighingSession = "weighing.session"

// Feed & water removal modes.
const (
	// RemovalModeRequired: every task carries the removal precondition (the 2026-09-03 rule).
	RemovalModeRequired = "required"
	// RemovalModeOptional: the planner chooses per task; the default offered is ON.
	RemovalModeOptional = "optional"
	// RemovalModeOff: no task carries the precondition; the wizard offers no removal step.
	RemovalModeOff = "off"
)

// The seeded removal proof slots. Since 2026-09-15 (second decision the same day) the slot
// LIST is the author's: a slot may be added, removed, re-worded, switched between video / photo /
// either, or made optional. These two keys are only the seed's -- and the legacy wire fields
// `feed_proof_ref` / `water_proof_ref` map onto them when an older phone submits.
const (
	RemovalProofFeed  = "feed_video"
	RemovalProofWater = "water_video"

	RemovalProofKindVideo  = "video"
	RemovalProofKindPhoto  = "photo"
	RemovalProofKindEither = "either"
)

// MaxRemovalProofSlots bounds the removal card: a card asking for more captures than this is
// not an evening's work.
const MaxRemovalProofSlots = 8

// Removal-card question kinds. The phone renders each with its own widget; there is no media
// kind here because the two proof slots ARE the card's media.
const (
	SOPQuestionChoice = "choice"
	SOPQuestionMulti  = "multi"
	SOPQuestionText   = "text"
	SOPQuestionNumber = "number"
)

// WeighingSOP is form_dsl.weighing.
type WeighingSOP struct {
	SchemaVersion    string        `json:"schema_version"`
	Planning         PlanningRules `json:"planning"`
	FeedWaterRemoval RemovalRules  `json:"feed_water_removal"`
	Capture          CaptureRules  `json:"capture"`
	// WeightsPages governs the admin-web Weights and ADG Analytics pages (maintainer request
	// 2026-09-16): the period they OPEN on and the earliest day their calendars offer. Page
	// settings, not task rules -- read from the PUBLISHED version, never pinned. A document
	// without the block (published before it existed) reads as the seeded values.
	WeightsPages *WeightsPagesRules `json:"weights_pages,omitempty"`
}

// The two ways the Weights pages' default period may start.
const (
	// WeightsFromFixedDate: the pages open from an authored calendar date.
	WeightsFromFixedDate = "fixed_date"
	// WeightsFromRollingDays: the pages open from N days before today, moving every day.
	WeightsFromRollingDays = "rolling_days"
)

// WeightsPagesRules is form_dsl.weighing.weights_pages.
type WeightsPagesRules struct {
	// DefaultFromMode is WeightsFromFixedDate or WeightsFromRollingDays.
	DefaultFromMode string `json:"default_from_mode"`
	// DefaultFromDate is the day the pages open from under fixed_date ("YYYY-MM-DD").
	DefaultFromDate string `json:"default_from_date,omitempty"`
	// DefaultFromDays is how many days back the pages open from under rolling_days (1..3650).
	DefaultFromDays int `json:"default_from_days,omitempty"`
	// EarliestDate is the first day the calendars offer; earlier days are disabled.
	EarliestDate string `json:"earliest_date"`
}

// PlanningRules governs the plan wizard.
type PlanningRules struct {
	// Modes lists the capture modes the planner may assign (CategoryIndividualAnimal,
	// CategoryPerShedPartition). At least one.
	Modes []string `json:"modes"`
	// DefaultCapPerDay prefills the wizard's cap and is applied when a create sends none.
	DefaultCapPerDay int `json:"default_cap_per_day"`
}

// RemovalRules governs the feed & water removal precondition.
type RemovalRules struct {
	Mode string `json:"mode"`
	// CutoffTime is the weighing SOP's OWN removal evening ("HH:MM", Asia/Kolkata wall clock),
	// authored on the SOP page (third 2026-09-15 decision: "cutoff time is configurable").
	// Blank means the farm-wide evening in feed_water_removal_config, which deworming still
	// shares; set, weighing runs on this instead. On a SERVED rule set (planner catalog, task
	// read, removal card) it is always filled with the EFFECTIVE value so the phone never
	// resolves it.
	CutoffTime string `json:"cutoff_time,omitempty"`
	// Instruction is the operator-facing sentence on every removal card, rendered verbatim.
	Instruction string             `json:"instruction,omitempty"`
	Proofs      []RemovalProofSlot `json:"proofs"`
	// Questions are answered once per pen card, alongside the two clips, and stored on the
	// pen's evidence row.
	Questions []SOPQuestion `json:"questions"`
}

// RemovalProofSlot is one capture a pen card asks for: a live-camera VIDEO, a PHOTO, or EITHER,
// compulsory or not. One capture per slot; a card wanting two captures asks two slots.
type RemovalProofSlot struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	Kind  string `json:"kind"`
	// Required: the pen cannot be submitted without this capture. At least one slot of a
	// document is required, so a removal is always proven by something the verifier can see.
	Required bool `json:"required"`
}

// UnmarshalJSON reads a slot with `required` ABSENT as compulsory: documents published before
// the flag existed (the first 2026-09-15 shape) carried two compulsory clips, and reading them
// as optional would refuse every rule read on a farm that published that day.
func (p *RemovalProofSlot) UnmarshalJSON(data []byte) error {
	type raw struct {
		Key      string `json:"key"`
		Title    string `json:"title"`
		Hint     string `json:"hint"`
		Kind     string `json:"kind"`
		Required *bool  `json:"required"`
	}
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	p.Key, p.Title, p.Hint, p.Kind = r.Key, r.Title, r.Hint, r.Kind
	p.Required = r.Required == nil || *r.Required
	return nil
}

// Accepts reports whether a slot takes a capture of the given proof type (video / photo).
func (p RemovalProofSlot) Accepts(proofType string) bool {
	switch p.Kind {
	case RemovalProofKindEither:
		return proofType == RemovalProofKindVideo || proofType == RemovalProofKindPhoto
	default:
		return proofType == p.Kind
	}
}

// SOPQuestion is one authored question. Wire-shaped the same way the procurement inspection's
// questions are, so the phone renders it with the widgets it already has.
type SOPQuestion struct {
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Title    string      `json:"title"`
	Hint     string      `json:"hint,omitempty"`
	Required bool        `json:"required"`
	Options  []SOPOption `json:"options,omitempty"`
	// AllowOther lets a pick-one carry free text under the "other" option ("<id>_other").
	AllowOther bool     `json:"allow_other,omitempty"`
	Min        *float64 `json:"min,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	// OnlyIf hides the question unless an EARLIER pick-one holds the given value.
	OnlyIf *SOPCondition `json:"only_if,omitempty"`
}

// SOPOption is one choice of a pick-one / pick-many question.
type SOPOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// SOPCondition is a single-question dependency.
type SOPCondition struct {
	QuestionID string `json:"question_id"`
	Value      string `json:"value"`
}

// CaptureRules governs the weighing capture itself.
type CaptureRules struct {
	Individual IndividualCaptureRules `json:"individual"`
	LumpSum    LumpSumCaptureRules    `json:"lump_sum"`
}

// IndividualCaptureRules: the per-animal video. VideoRequired is LOCKED true -- one video per
// animal is the evidence the verifier reviews (ledger B-5); the field exists so the document
// states the rule, and a version that sets it false is refused.
type IndividualCaptureRules struct {
	VideoRequired bool `json:"video_required"`
}

// LumpSumCaptureRules: how many pen videos one lump-sum submission carries. VideoMax is bounded
// by MaxShedProofArtifacts, the proof policy leadership reads ("3 of 5").
type LumpSumCaptureRules struct {
	VideoMin int `json:"video_min"`
	VideoMax int `json:"video_max"`
}

// Rules is one COMPILED, VERSIONED rule set: what a task was planned on and runs under.
// Version is the SOP version number (sop_versions.version); 0 means the seeded document on a
// tenant that never published one.
type Rules struct {
	Version int `json:"version"`
	WeighingSOP
}

//go:embed sopseed/weighing_session.json
var seededWeighingSOPJSON []byte

// SeededWeighingSOPJSON is the day-one document, embedded verbatim in the migration that adds
// it to each tenant's published weighing.session version.
func SeededWeighingSOPJSON() []byte { return append([]byte(nil), seededWeighingSOPJSON...) }

// SeededRules compiles the embedded document; a tenant with no published version runs it. It
// reproduces the pre-SOP behaviour exactly: removal REQUIRED on every task, both clips, no
// questions, cap 100, 1..5 lump-sum videos.
func SeededRules() Rules {
	dsl, err := ParseWeighingSOP(map[string]any{"weighing": json.RawMessage(seededWeighingSOPJSON)})
	if err != nil {
		panic("weighing: seeded sop does not parse: " + err.Error())
	}
	if problems := ValidateWeighingSOP(dsl); len(problems) > 0 {
		panic("weighing: seeded sop invalid: " + problems[0])
	}
	return Rules{Version: 0, WeighingSOP: dsl}
}

var ErrWeighingSOPInvalid = errors.New("weighing sop document invalid")

// ParseWeighingSOP reads form_dsl.weighing. A form_dsl without one is not a weighing SOP.
func ParseWeighingSOP(formDSL map[string]any) (WeighingSOP, error) {
	raw, ok := formDSL["weighing"]
	if !ok || raw == nil {
		return WeighingSOP{}, fmt.Errorf("%w: form_dsl.weighing missing", ErrWeighingSOPInvalid)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return WeighingSOP{}, fmt.Errorf("%w: %v", ErrWeighingSOPInvalid, err)
	}
	var dsl WeighingSOP
	if err := json.Unmarshal(b, &dsl); err != nil {
		return WeighingSOP{}, fmt.Errorf("%w: %v", ErrWeighingSOPInvalid, err)
	}
	if dsl.WeightsPages == nil {
		// Published before the block existed: the pages keep the seeded window.
		seeded := seededWeightsPages()
		dsl.WeightsPages = &seeded
	}
	return dsl, nil
}

func seededWeightsPages() WeightsPagesRules {
	var seed struct {
		WeightsPages WeightsPagesRules `json:"weights_pages"`
	}
	_ = json.Unmarshal(seededWeighingSOPJSON, &seed)
	return seed.WeightsPages
}

// UnknownWeighingSOPKeys names every key the document carries that the schema does not,
// each by path ("feed_water_removal.cutoff_tme"). A save refuses them: a misspelt key is
// silently dropped by the lenient parser, and a farm that typed `cutoff_tme: 21:30` would
// otherwise run on the farm evening believing it set its own. The read paths stay lenient
// so a stored document never fails to load.
func UnknownWeighingSOPKeys(formDSL map[string]any) []string {
	raw, ok := formDSL["weighing"].(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	walk := func(path string, node any, allowed map[string]bool) {
		m, ok := node.(map[string]any)
		if !ok {
			return
		}
		for k := range m {
			if !allowed[k] {
				out = append(out, path+k)
			}
		}
	}
	walk("", raw, map[string]bool{"schema_version": true, "planning": true, "feed_water_removal": true, "capture": true, "weights_pages": true})
	walk("weights_pages.", raw["weights_pages"], map[string]bool{"default_from_mode": true, "default_from_date": true, "default_from_days": true, "earliest_date": true})
	walk("planning.", raw["planning"], map[string]bool{"modes": true, "default_cap_per_day": true})
	walk("feed_water_removal.", raw["feed_water_removal"], map[string]bool{"mode": true, "cutoff_time": true, "instruction": true, "proofs": true, "questions": true})
	if fwr, ok := raw["feed_water_removal"].(map[string]any); ok {
		if proofs, ok := fwr["proofs"].([]any); ok {
			for i, p := range proofs {
				walk(fmt.Sprintf("feed_water_removal.proofs.%d.", i), p, map[string]bool{"key": true, "title": true, "hint": true, "kind": true, "required": true})
			}
		}
		if qs, ok := fwr["questions"].([]any); ok {
			for i, q := range qs {
				walk(fmt.Sprintf("feed_water_removal.questions.%d.", i), q, map[string]bool{"id": true, "kind": true, "title": true, "hint": true, "required": true, "options": true, "allow_other": true, "min": true, "max": true, "unit": true, "only_if": true})
			}
		}
	}
	walk("capture.", raw["capture"], map[string]bool{"individual": true, "lump_sum": true})
	if c, ok := raw["capture"].(map[string]any); ok {
		walk("capture.individual.", c["individual"], map[string]bool{"video_required": true})
		walk("capture.lump_sum.", c["lump_sum"], map[string]bool{"video_min": true, "video_max": true})
	}
	sort.Strings(out)
	return out
}

var sopIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,47}$`)

const (
	maxRemovalQuestions = 50
	maxSOPTextLength    = 2000
)

// ValidateWeighingSOP returns every problem in the document, each naming its path, so the web
// editor can point at the field. An empty slice means the document compiles and can be
// published.
func ValidateWeighingSOP(dsl WeighingSOP) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if dsl.SchemaVersion != WeighingSOPSchemaVersion {
		add("weighing.schema_version: want %q", WeighingSOPSchemaVersion)
	}

	// Planning.
	if len(dsl.Planning.Modes) == 0 {
		add("weighing.planning.modes: at least one capture mode")
	}
	seenMode := map[string]bool{}
	for i, m := range dsl.Planning.Modes {
		switch m {
		case CategoryIndividualAnimal, CategoryPerShedPartition:
		default:
			add("weighing.planning.modes.%d: %q is not a capture mode (%s / %s)", i, m, CategoryIndividualAnimal, CategoryPerShedPartition)
		}
		if seenMode[m] {
			add("weighing.planning.modes.%d: %q is listed twice", i, m)
		}
		seenMode[m] = true
	}
	if dsl.Planning.DefaultCapPerDay < 1 || dsl.Planning.DefaultCapPerDay > 10000 {
		add("weighing.planning.default_cap_per_day: 1..10000")
	}

	// Feed & water removal.
	switch dsl.FeedWaterRemoval.Mode {
	case RemovalModeRequired, RemovalModeOptional, RemovalModeOff:
	default:
		add("weighing.feed_water_removal.mode: %q is not required / optional / off", dsl.FeedWaterRemoval.Mode)
	}
	if len(dsl.FeedWaterRemoval.Instruction) > maxSOPTextLength {
		add("weighing.feed_water_removal.instruction: too long")
	}
	if ct := strings.TrimSpace(dsl.FeedWaterRemoval.CutoffTime); ct != "" {
		if _, err := time.Parse("15:04", ct); err != nil {
			add("weighing.feed_water_removal.cutoff_time: %q is not HH:MM (24-hour, Asia/Kolkata)", ct)
		}
	}
	seenSlot := map[string]bool{}
	requiredSlots := 0
	if len(dsl.FeedWaterRemoval.Proofs) > MaxRemovalProofSlots {
		add("weighing.feed_water_removal.proofs: at most %d captures per pen", MaxRemovalProofSlots)
	}
	for i, p := range dsl.FeedWaterRemoval.Proofs {
		pp := fmt.Sprintf("weighing.feed_water_removal.proofs.%d", i)
		if !sopIDPattern.MatchString(p.Key) {
			add("%s.key: %q must be a-z, 0-9 and _ (start with a letter)", pp, p.Key)
		}
		if seenSlot[p.Key] {
			add("%s.key: %q is listed twice", pp, p.Key)
		}
		seenSlot[p.Key] = true
		if strings.TrimSpace(p.Title) == "" {
			add("%s.title: required", pp)
		}
		switch p.Kind {
		case RemovalProofKindVideo, RemovalProofKindPhoto, RemovalProofKindEither:
		default:
			add("%s.kind: %q is not video / photo / either", pp, p.Kind)
		}
		if p.Required {
			requiredSlots++
		}
	}
	if dsl.FeedWaterRemoval.Mode != RemovalModeOff && requiredSlots == 0 {
		add("weighing.feed_water_removal.proofs: at least one compulsory capture -- a removal must be proven by something the verifier can see")
	}
	if len(dsl.FeedWaterRemoval.Questions) > maxRemovalQuestions {
		add("weighing.feed_water_removal.questions: at most %d", maxRemovalQuestions)
	}
	validateSOPQuestions("weighing.feed_water_removal.questions", dsl.FeedWaterRemoval.Questions, add)

	// Capture.
	if !dsl.Capture.Individual.VideoRequired {
		add("weighing.capture.individual.video_required: fixed to true -- one video per animal is the evidence the verifier reviews")
	}
	ls := dsl.Capture.LumpSum
	if ls.VideoMin < 1 {
		add("weighing.capture.lump_sum.video_min: at least 1 -- a lump-sum weigh without a pen video has no evidence to review")
	}
	if ls.VideoMax < 1 || ls.VideoMax > MaxShedProofArtifacts {
		add("weighing.capture.lump_sum.video_max: 1..%d", MaxShedProofArtifacts)
	}
	if ls.VideoMin > ls.VideoMax {
		add("weighing.capture.lump_sum.video_min: must not exceed video_max")
	}
	if wp := dsl.WeightsPages; wp != nil {
		switch wp.DefaultFromMode {
		case WeightsFromFixedDate:
			if _, err := time.Parse("2006-01-02", wp.DefaultFromDate); err != nil {
				add("weighing.weights_pages.default_from_date: %q is not YYYY-MM-DD", wp.DefaultFromDate)
			}
		case WeightsFromRollingDays:
			if wp.DefaultFromDays < 1 || wp.DefaultFromDays > 3650 {
				add("weighing.weights_pages.default_from_days: 1..3650")
			}
		default:
			add("weighing.weights_pages.default_from_mode: %q is not fixed_date / rolling_days", wp.DefaultFromMode)
		}
		if _, err := time.Parse("2006-01-02", wp.EarliestDate); err != nil {
			add("weighing.weights_pages.earliest_date: %q is not YYYY-MM-DD", wp.EarliestDate)
		} else if wp.DefaultFromMode == WeightsFromFixedDate && wp.DefaultFromDate != "" && wp.DefaultFromDate < wp.EarliestDate {
			add("weighing.weights_pages.default_from_date: must not be before earliest_date")
		}
	}

	return problems
}

func validateSOPQuestions(path string, questions []SOPQuestion, add func(string, ...any)) {
	seen := map[string]SOPQuestion{}
	for qi, q := range questions {
		qp := fmt.Sprintf("%s.%d", path, qi)
		if !sopIDPattern.MatchString(q.ID) {
			add("%s.id: %q must be a-z, 0-9 and _ (start with a letter)", qp, q.ID)
		}
		if strings.HasSuffix(q.ID, "_other") {
			add("%s.id: %q -- the _other suffix is reserved for the free text of a pick-one", qp, q.ID)
		}
		if _, dup := seen[q.ID]; dup {
			add("%s.id: %q is used twice", qp, q.ID)
		}
		if strings.TrimSpace(q.Title) == "" {
			add("%s.title: required", qp)
		}
		switch q.Kind {
		case SOPQuestionChoice, SOPQuestionMulti:
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
		case SOPQuestionNumber:
			if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
				add("%s.min: must not exceed max", qp)
			}
		case SOPQuestionText:
		default:
			add("%s.kind: %q is not a question kind (choice / multi / text / number)", qp, q.Kind)
		}
		if q.OnlyIf != nil {
			dep, ok := seen[q.OnlyIf.QuestionID]
			switch {
			case !ok:
				add("%s.only_if.question_id: %q must be an EARLIER question", qp, q.OnlyIf.QuestionID)
			case dep.Kind != SOPQuestionChoice:
				add("%s.only_if.question_id: %q must be a pick-one question", qp, q.OnlyIf.QuestionID)
			case !hasSOPOption(dep.Options, q.OnlyIf.Value):
				add("%s.only_if.value: %q is not a choice of %q", qp, q.OnlyIf.Value, q.OnlyIf.QuestionID)
			}
		}
		seen[q.ID] = q
	}
}

func hasSOPOption(options []SOPOption, v string) bool {
	for _, o := range options {
		if o.Value == v {
			return true
		}
	}
	return false
}

// --- Rule readers -----------------------------------------------------------------------

// ModeAllowed reports whether the planner may assign a capture mode under these rules.
func (r Rules) ModeAllowed(category string) bool {
	for _, m := range r.Planning.Modes {
		if m == category {
			return true
		}
	}
	return false
}

// RemovalApplies decides whether ONE task carries the feed & water removal precondition:
// always under `required`, never under `off`, and the planner's own choice under `optional`
// (requested nil means "not said", which under optional reads as the default: ON).
func (r Rules) RemovalApplies(requested *bool) bool {
	switch r.FeedWaterRemoval.Mode {
	case RemovalModeOff:
		return false
	case RemovalModeOptional:
		if requested == nil {
			return true
		}
		return *requested
	default:
		return true
	}
}

// RemovalProof returns the slot with the given key, if the document asks for it.
func (r Rules) RemovalProof(key string) (RemovalProofSlot, bool) {
	for _, p := range r.FeedWaterRemoval.Proofs {
		if p.Key == key {
			return p, true
		}
	}
	return RemovalProofSlot{}, false
}

// RemovalProofs is the ordered slot list; never nil.
func (r Rules) RemovalProofs() []RemovalProofSlot {
	if r.FeedWaterRemoval.Proofs == nil {
		return []RemovalProofSlot{}
	}
	return r.FeedWaterRemoval.Proofs
}

// RemovalProofRefs is {slot key: proof artifact ref} as a pen submit carries it. A blank ref is
// "not captured"; the map is stored on the evidence row and is what the verifier item is built
// from, in slot order.
type RemovalProofRefs map[string]string

// ProofError names the slot a submit failed on, for the phone to point at.
type ProofError struct {
	SlotKey string
	Message string
}

func (e *ProofError) Error() string { return e.SlotKey + ": " + e.Message }

// ErrSOPProofInvalid wraps every ProofError so callers can errors.Is it.
var ErrSOPProofInvalid = errors.New("weighing: sop proof invalid")

func proofInvalid(key, msg string) error {
	return fmt.Errorf("%w: %w", ErrSOPProofInvalid, &ProofError{SlotKey: key, Message: msg})
}

// ValidateRemovalProofRefs checks a pen submit's captures against THESE rules: every compulsory
// slot carries a ref, no ref lands in a slot the document does not ask for, and no ref is used
// in two slots (one capture cannot prove two things). The refs' TYPES are checked by the store
// against the proof register (a photo in a video slot is refused there). Returns the refs in
// slot order for the verifier item.
func (r Rules) ValidateRemovalProofRefs(refs RemovalProofRefs) ([]string, error) {
	known := map[string]bool{}
	for _, p := range r.RemovalProofs() {
		known[p.Key] = true
	}
	for key := range refs {
		if !known[key] {
			return nil, proofInvalid(key, "This capture is not part of the removal card.")
		}
	}
	seen := map[string]string{}
	ordered := make([]string, 0, len(refs))
	for _, p := range r.RemovalProofs() {
		ref := strings.TrimSpace(refs[p.Key])
		if ref == "" {
			if p.Required {
				return nil, proofInvalid(p.Key, "Record: "+p.Title)
			}
			continue
		}
		if other, dup := seen[ref]; dup {
			return nil, proofInvalid(p.Key, "The same capture cannot prove both "+other+" and "+p.Title+".")
		}
		seen[ref] = p.Title
		ordered = append(ordered, ref)
	}
	return ordered, nil
}

// NormalizeRemovalProofRefs drops blank entries so the stored map holds only real captures.
func NormalizeRemovalProofRefs(refs RemovalProofRefs) RemovalProofRefs {
	out := RemovalProofRefs{}
	for k, v := range refs {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// --- Removal-card answers -----------------------------------------------------------------

// SOPAnswers is {question id: answer}: a pick-one answer is its option value; an "other" free
// text rides under "<id>_other"; pick-many answers are arrays of option values; numbers are
// JSON numbers or numeric strings; text is text.
type SOPAnswers map[string]json.RawMessage

// AnswerError names the question a submit failed on, for the phone to point at.
type AnswerError struct {
	QuestionID string
	Message    string
}

func (e *AnswerError) Error() string { return e.QuestionID + ": " + e.Message }

// ErrSOPAnswerInvalid wraps every AnswerError so callers can errors.Is it.
var ErrSOPAnswerInvalid = errors.New("weighing: sop answer invalid")

func answerInvalid(id, msg string) error {
	return fmt.Errorf("%w: %w", ErrSOPAnswerInvalid, &AnswerError{QuestionID: id, Message: msg})
}

// ValidateRemovalAnswers checks a pen card's answers against THESE rules: every applicable
// required question answered, every choice among the offered options, every number in range,
// no answer to a question the document does not ask. A question whose only_if does not hold is
// skipped -- an answer given to it is dropped by NormalizeRemovalAnswers, not refused.
func (r Rules) ValidateRemovalAnswers(a SOPAnswers) error {
	questions := r.FeedWaterRemoval.Questions
	known := map[string]bool{}
	for _, q := range questions {
		known[q.ID] = true
		if q.Kind == SOPQuestionChoice && q.AllowOther {
			known[q.ID+"_other"] = true
		}
	}
	for id := range a {
		if !known[id] {
			return answerInvalid(id, "This question is not part of the removal card.")
		}
	}
	for _, q := range questions {
		if !a.applies(q) {
			continue
		}
		switch q.Kind {
		case SOPQuestionChoice:
			v := a.choice(q.ID)
			if v == "" {
				if q.Required {
					return answerInvalid(q.ID, "Answer: "+q.Title)
				}
				continue
			}
			if !hasSOPOption(q.Options, v) {
				return answerInvalid(q.ID, "Pick one of the offered answers for: "+q.Title)
			}
			if v == "other" && q.AllowOther && strings.TrimSpace(a.text(q.ID+"_other")) == "" {
				return answerInvalid(q.ID, "Say which, for: "+q.Title)
			}
		case SOPQuestionMulti:
			vals, ok := a.multi(q.ID)
			if !ok || len(vals) == 0 {
				if q.Required {
					return answerInvalid(q.ID, "Tick at least one for: "+q.Title)
				}
				continue
			}
			for _, v := range vals {
				if !hasSOPOption(q.Options, v) {
					return answerInvalid(q.ID, "Pick only the offered answers for: "+q.Title)
				}
			}
		case SOPQuestionNumber:
			n, present, err := a.number(q.ID)
			if !present {
				if q.Required {
					return answerInvalid(q.ID, "Enter: "+q.Title)
				}
				continue
			}
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return answerInvalid(q.ID, "Enter a number for: "+q.Title)
			}
			if (q.Min != nil && n < *q.Min) || (q.Max != nil && n > *q.Max) {
				return answerInvalid(q.ID, fmt.Sprintf("Enter a value between %g and %g for: %s", deref(q.Min), deref(q.Max), q.Title))
			}
		case SOPQuestionText:
			t := a.text(q.ID)
			if q.Required && strings.TrimSpace(t) == "" {
				return answerInvalid(q.ID, "Enter: "+q.Title)
			}
			if len(t) > maxSOPTextLength {
				return answerInvalid(q.ID, "Too long: "+q.Title)
			}
		}
	}
	return nil
}

// NormalizeRemovalAnswers keeps only the answers of questions that apply (a conditional whose
// condition failed is dropped rather than stored), so the stored row reads exactly as the card
// asked. Returns an empty (non-nil) map when the document asks nothing.
func (r Rules) NormalizeRemovalAnswers(a SOPAnswers) SOPAnswers {
	out := SOPAnswers{}
	for _, q := range r.FeedWaterRemoval.Questions {
		if !a.applies(q) {
			continue
		}
		if raw, ok := a[q.ID]; ok {
			out[q.ID] = raw
		}
		if q.Kind == SOPQuestionChoice && q.AllowOther {
			if raw, ok := a[q.ID+"_other"]; ok && a.choice(q.ID) == "other" {
				out[q.ID+"_other"] = raw
			}
		}
	}
	return out
}

func (a SOPAnswers) applies(q SOPQuestion) bool {
	if q.OnlyIf == nil {
		return true
	}
	return a.choice(q.OnlyIf.QuestionID) == q.OnlyIf.Value
}

func (a SOPAnswers) choice(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

func (a SOPAnswers) text(id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func (a SOPAnswers) multi(id string) ([]string, bool) {
	raw, ok := a[id]
	if !ok {
		return nil, false
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		var one string
		if err := json.Unmarshal(raw, &one); err != nil || strings.TrimSpace(one) == "" {
			return nil, false
		}
		return []string{one}, true
	}
	return vals, true
}

func (a SOPAnswers) number(id string) (float64, bool, error) {
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

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// WeighDateAllowsPlainCreate is the create rule for a task WITHOUT the feed & water removal
// precondition (the SOP has it off, or optional and the planner declined it): the weigh date
// may be today or any later business day. No evening cutoff applies because no evening's
// work is being scheduled; the date only cannot lie in the past.
func WeighDateAllowsPlainCreate(weighDate string, now time.Time) bool {
	return weighDate >= biztime.BusinessDate(now)
}
