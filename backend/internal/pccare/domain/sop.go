package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// PC CARE SOP (maintainer decision 2026-09-22, docs/decisions/pc-care-sop.md).
//
// The five hands-on-the-animal PC Care categories -- deworming, anti protozoan, ticks removal,
// hoof trimming, hair trimming -- stop running on a slot table typed into Go. WHAT the operator
// captures per animal, WHAT they answer, and whether a tablet-in-feed deworming carries the
// evening-before feed & water removal are the `pc_care` section of the PUBLISHED `pc_care.tasks`
// SOP version, authored on Preventive Care › PC Care SOP (/pc-care/sops):
//
//   - FEED & WATER REMOVAL: `required` (every task of a category it applies to carries it, no
//     one is asked), `optional` (the planner decides per task -- the seed, which is what the
//     module always did), `off` (never offered); WHICH categories it applies to (the seed says
//     deworming alone: the tablet goes in the feed, the anti-protozoan dose does not); the SOP's
//     OWN evening cutoff (blank = the farm-wide feed_water_removal_config evening shared with
//     weighing); the instruction on the removal card; the captures the card asks for per pen
//     (1..8 slots, video / photo / either, compulsory or not); extra questions per pen.
//   - PER CATEGORY: the instruction, the capture slots (1..8, at least one compulsory -- the
//     work must be proven by something the verifier can see) and the questions answered at
//     submit. The slots are PER ANIMAL for the five hands-on-the-animal categories and PER PEN
//     for fumigation (2026-09-30), whose task scans no animal: the capture mode decides the
//     grain, the document decides what is captured.
//
// A task is stamped with the SOP version it was PLANNED on (pc_care_tasks.sop_version, the same
// number on every pen task of a round and on the round's removal card) and runs on that version
// to the end: publishing a new version changes the next task planned, never a task already on
// the calendar (the pin-at-start rule of the herd-operations, procurement, weighing and feed
// SOPs). Version 0 / NULL is the seeded document, which is the pre-SOP behaviour byte for byte.
//
// NOT authorable here, on purpose: the CAPTURE MODE of a category (scan-and-record for the
// 2-second jobs, roster-pick for the trimming jobs -- CaptureModeForCategory), free-flow scan
// (a tag is stored verbatim, never resolved against the herd), the one business rule (no
// duplicate tag in a task), the whole-task submit grain, one verifier item per task, the
// assignee gate, the trimming planner carve-out (an HRMS fact, never a rulebook's), and the
// kernel-owned inventory_vaccine check. The document says what the farm asks of the operator,
// not what the kernel does with it.

// PCCareSOPSchemaVersion is the document's own version tag (not the SOP version number).
const PCCareSOPSchemaVersion = "goatos.sop-pc-care.v1"

// SOPCodePCCare is the SOP library code the PC Care rules are published under.
const SOPCodePCCare = "pc_care.tasks"

// Feed & water removal modes -- the weighing SOP's vocabulary, same words, same meaning.
const (
	// RemovalModeRequired: every task of an applicable category carries the precondition.
	RemovalModeRequired = "required"
	// RemovalModeOptional: the planner chooses per task (the toggle on the wizard). The seed.
	RemovalModeOptional = "optional"
	// RemovalModeOff: no task carries the precondition; the wizard offers no removal step.
	RemovalModeOff = "off"
)

// SOPCategories are the categories the document authors: exactly the planner categories.
// Kernel-owned work (inventory_vaccine) and the removal card itself keep their fixed slots
// (SlotsForCategory) -- the removal card's slots are authored under feed_water_removal.
var SOPCategories = PlannerCategories

// CaptureSlot is one per-animal capture: the shared authored slot plus the recorder-chrome
// duration HINT the trimming "while" clip carries (~10 s). The hint is guidance the phone shows
// on the recorder, never a client-enforced cap -- the original Slot contract, kept.
type CaptureSlot struct {
	authored.ProofSlot
	// MinSeconds is the recorder-chrome guidance ("record about 10 seconds"); 0 = no hint.
	MinSeconds int `json:"min_seconds,omitempty"`
}

// UnmarshalJSON keeps the shared slot's "required ABSENT reads as compulsory" rule while reading
// the extra hint: embedding alone would promote the inner UnmarshalJSON and drop min_seconds.
func (s *CaptureSlot) UnmarshalJSON(data []byte) error {
	var inner authored.ProofSlot
	if err := json.Unmarshal(data, &inner); err != nil {
		return err
	}
	var extra struct {
		MinSeconds int `json:"min_seconds"`
	}
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	s.ProofSlot = inner
	s.MinSeconds = extra.MinSeconds
	return nil
}

// MarshalJSON writes the flat shape the seed and the editor use (the inner slot's fields beside
// min_seconds), never a nested object.
func (s CaptureSlot) MarshalJSON() ([]byte, error) {
	type flat struct {
		Key        string `json:"key"`
		Title      string `json:"title"`
		Hint       string `json:"hint,omitempty"`
		Kind       string `json:"kind"`
		Required   bool   `json:"required"`
		MinSeconds int    `json:"min_seconds,omitempty"`
	}
	return json.Marshal(flat{Key: s.Key, Title: s.Title, Hint: s.Hint, Kind: s.Kind, Required: s.Required, MinSeconds: s.MinSeconds})
}

// CategoryRules is one category's card: what the operator is told, what they capture per
// animal, what they answer per animal.
type CategoryRules struct {
	Instruction string              `json:"instruction,omitempty"`
	Proofs      []CaptureSlot       `json:"proofs"`
	Questions   []authored.Question `json:"questions"`
	// RepeatEveryDays (maintainer instruction 2026-09-30): when set, the kernel plans the NEXT task
	// of this work for the same pen(s) and the same operator(s) this many days after the last
	// one's PLANNED date, whether or not the last one is finished. 0 / absent = no repeat. Read
	// from the PUBLISHED document when the next task is made (a planning act), never from a pin.
	RepeatEveryDays int `json:"repeat_every_days,omitempty"`
	// RepeatMode (maintainer instruction 2026-10-02, docs/decisions/pc-care-rotation.md) is blank
	// for the per-pen interval above (or no repeat at all) and RepeatModeRotation for a ROUND
	// ROBIN: one pen per day, in the park's pen order, through every pen with animals in it, then
	// round again. The two are exclusive per card -- a card says one or the other.
	RepeatMode string `json:"repeat_mode,omitempty"`
	// RotationGapDays is how many days the farm waits after the LAST pen of a rotation before the
	// first pen of the next one; 0 = start again the next day. Only meaningful under rotation.
	RotationGapDays int `json:"rotation_gap_days,omitempty"`
}

// MaxRepeatEveryDays bounds the repeat interval an author may set (one year). It bounds the
// rotation gap too.
const MaxRepeatEveryDays = 365

// RepeatModeRotation is the round-robin repeat mode (2026-10-02).
const RepeatModeRotation = "rotation"

// ProofSlots returns the category's slots as the shared authored type (for the validators).
func (c CategoryRules) ProofSlots() []authored.ProofSlot {
	out := make([]authored.ProofSlot, 0, len(c.Proofs))
	for _, p := range c.Proofs {
		out = append(out, p.ProofSlot)
	}
	return out
}

// RemovalRules governs the feed & water removal precondition of a tablet-in-feed deworming.
type RemovalRules struct {
	Mode string `json:"mode"`
	// AppliesTo names the planner categories whose tasks may (optional) or must (required)
	// carry the removal. The seed says deworming alone. Under `off` the list is ignored.
	AppliesTo []string `json:"applies_to"`
	// CutoffTime is the PC Care SOP's OWN removal evening ("HH:MM", Asia/Kolkata wall clock).
	// Blank means the farm-wide evening in feed_water_removal_config. On a SERVED rule set it
	// is always filled with the EFFECTIVE value so the phone never resolves it.
	CutoffTime string `json:"cutoff_time,omitempty"`
	// Instruction is the operator-facing sentence on the removal card, rendered verbatim.
	Instruction string               `json:"instruction,omitempty"`
	Proofs      []authored.ProofSlot `json:"proofs"`
	// Questions are answered once per pen on the removal card, beside the captures.
	Questions []authored.Question `json:"questions"`
}

// PCCareSOP is form_dsl.pc_care.
type PCCareSOP struct {
	SchemaVersion    string                    `json:"schema_version"`
	FeedWaterRemoval RemovalRules              `json:"feed_water_removal"`
	Categories       map[string]*CategoryRules `json:"categories"`
}

// Rules is one COMPILED, VERSIONED rule set: what a task was planned on and runs under. Version
// is the SOP version number (sop_versions.version); 0 means the seeded document.
type Rules struct {
	Version int `json:"version"`
	PCCareSOP
}

//go:embed sopseed/pc_care.json
var seededPCCareSOPJSON []byte

// SeededPCCareSOPJSON is the day-one document, embedded verbatim in the migration that seeds
// the pc_care.care definition and its v1 (pinned by TestMigrationEmbedsTheSeededPCCareSOP).
func SeededPCCareSOPJSON() []byte { return append([]byte(nil), seededPCCareSOPJSON...) }

// seededRulesOnce memoizes the compiled seed: the store's fallback path reads it per request and
// the document is a constant.
var seededRulesOnce = sync.OnceValue(compileSeededRules)

// SeededRules compiles the embedded document; a tenant with no published version runs it. It
// reproduces the pre-SOP behaviour exactly: removal OPTIONAL on deworming alone with the two
// clips, one video per animal on the 2-second jobs, the before / while / after triple on the
// trimming jobs, no questions -- pinned by TestSeededPCCareSOPIsThePreSOPBehaviour.
func SeededRules() Rules { return seededRulesOnce() }

func compileSeededRules() Rules {
	dsl, err := ParsePCCareSOP(map[string]any{"pc_care": json.RawMessage(seededPCCareSOPJSON)})
	if err != nil {
		panic("pccare: seeded sop does not parse: " + err.Error())
	}
	if problems := ValidatePCCareSOP(dsl); len(problems) > 0 {
		panic("pccare: seeded sop invalid: " + problems[0])
	}
	return Rules{Version: 0, PCCareSOP: dsl}
}

var ErrPCCareSOPInvalid = errors.New("pc care sop document invalid")

// ParsePCCareSOP reads form_dsl.pc_care. A form_dsl without one is not a PC Care SOP.
func ParsePCCareSOP(formDSL map[string]any) (PCCareSOP, error) {
	raw, ok := formDSL["pc_care"]
	if !ok || raw == nil {
		return PCCareSOP{}, fmt.Errorf("%w: form_dsl.pc_care missing", ErrPCCareSOPInvalid)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return PCCareSOP{}, fmt.Errorf("%w: %v", ErrPCCareSOPInvalid, err)
	}
	var dsl PCCareSOP
	if err := json.Unmarshal(b, &dsl); err != nil {
		return PCCareSOP{}, fmt.Errorf("%w: %v", ErrPCCareSOPInvalid, err)
	}
	return dsl, nil
}

// IsPCCareSOPCode reports whether a SOP code carries a PC Care document.
func IsPCCareSOPCode(sopCode string) bool { return sopCode == SOPCodePCCare }

// ValidatePCCareSOP returns every problem in the document, each naming its path, so the web
// editor can point at the field. An empty slice means the document compiles and can be
// published.
func ValidatePCCareSOP(dsl PCCareSOP) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if dsl.SchemaVersion != PCCareSOPSchemaVersion {
		add("pc_care.schema_version: want %q", PCCareSOPSchemaVersion)
	}

	// Feed & water removal.
	fwr := dsl.FeedWaterRemoval
	switch fwr.Mode {
	case RemovalModeRequired, RemovalModeOptional, RemovalModeOff:
	default:
		add("pc_care.feed_water_removal.mode: %q is not required / optional / off", fwr.Mode)
	}
	seenApplies := map[string]bool{}
	for i, c := range fwr.AppliesTo {
		if !isSOPCategory(c) {
			add("pc_care.feed_water_removal.applies_to.%d: %q is not a PC Care work category", i, c)
		} else if IsPenProofCategory(c) {
			// Fasting is for animals about to be dosed; a pen spray doses no animal.
			add("pc_care.feed_water_removal.applies_to.%d: %q is pen work -- feed and water removal does not apply to it", i, c)
		}
		if seenApplies[c] {
			add("pc_care.feed_water_removal.applies_to.%d: %q is listed twice", i, c)
		}
		seenApplies[c] = true
	}
	if fwr.Mode != RemovalModeOff && len(fwr.AppliesTo) == 0 {
		add("pc_care.feed_water_removal.applies_to: name at least one category the removal applies to, or switch the removal off")
	}
	if strings.TrimSpace(fwr.CutoffTime) != "" {
		if _, err := fwrdomain.ParseCutoff(fwr.CutoffTime); err != nil {
			add("pc_care.feed_water_removal.cutoff_time: %q is not an HH:MM time", fwr.CutoffTime)
		}
	}
	if len(fwr.Instruction) > authored.MaxTextLength {
		add("pc_care.feed_water_removal.instruction: too long")
	}
	if fwr.Mode != RemovalModeOff {
		authored.ValidateProofSlots("pc_care.feed_water_removal.proofs", fwr.Proofs, true, add)
		authored.ValidateQuestions("pc_care.feed_water_removal.questions", fwr.Questions, add)
	} else {
		authored.ValidateProofSlots("pc_care.feed_water_removal.proofs", fwr.Proofs, false, add)
		authored.ValidateQuestions("pc_care.feed_water_removal.questions", fwr.Questions, add)
	}

	// Categories: every planner category authored, nothing else.
	for c := range dsl.Categories {
		if !isSOPCategory(c) {
			add("pc_care.categories.%s: not a PC Care work category", c)
		}
	}
	for _, c := range SOPCategories {
		block := dsl.Categories[c]
		if block == nil {
			add("pc_care.categories.%s: required -- every work category needs its capture card", c)
			continue
		}
		if len(block.Instruction) > authored.MaxTextLength {
			add("pc_care.categories.%s.instruction: too long", c)
		}
		authored.ValidateProofSlots("pc_care.categories."+c+".proofs", block.ProofSlots(), true, add)
		for i, p := range block.Proofs {
			if p.MinSeconds < 0 || p.MinSeconds > 600 {
				add("pc_care.categories.%s.proofs.%d.min_seconds: 0..600", c, i)
			}
			if p.MinSeconds > 0 && p.Kind == authored.KindPhoto {
				add("pc_care.categories.%s.proofs.%d.min_seconds: a photo has no duration", c, i)
			}
		}
		authored.ValidateQuestions("pc_care.categories."+c+".questions", block.Questions, add)
		if block.RepeatEveryDays < 0 || block.RepeatEveryDays > MaxRepeatEveryDays {
			add("pc_care.categories.%s.repeat_every_days: 0 (no repeat) to %d days", c, MaxRepeatEveryDays)
		}
		switch block.RepeatMode {
		case "":
			if block.RotationGapDays != 0 {
				add("pc_care.categories.%s.rotation_gap_days: only a rotation has a gap between rounds", c)
			}
		case RepeatModeRotation:
			if block.RepeatEveryDays != 0 {
				add("pc_care.categories.%s.repeat_mode: a card repeats every N days OR rotates through the pens, not both", c)
			}
			if block.RotationGapDays < 0 || block.RotationGapDays > MaxRepeatEveryDays {
				add("pc_care.categories.%s.rotation_gap_days: 0 to %d days", c, MaxRepeatEveryDays)
			}
			if (Rules{PCCareSOP: dsl}).RemovalAppliesTo(c) {
				// A rotation plans one pen for the next day; the removal crew's evening before would
				// already be gone. Rotation is offered only for work with no removal.
				add("pc_care.categories.%s.repeat_mode: a rotation cannot carry feed and water removal; take %s off the removal list first", c, c)
			}
		default:
			add("pc_care.categories.%s.repeat_mode: %q is not blank or rotation", c, block.RepeatMode)
		}
	}
	return problems
}

func isSOPCategory(c string) bool {
	for _, category := range SOPCategories {
		if c == category {
			return true
		}
	}
	return false
}

// UnknownPCCareSOPKeys names every key the document carries that the schema does not, each by
// path. A save refuses them: a misspelt `proofs` would be dropped by the lenient parser and the
// farm would publish a card with no captures believing it authored three.
func UnknownPCCareSOPKeys(formDSL map[string]any) []string {
	raw, ok := formDSL["pc_care"].(map[string]any)
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
	slotKeys := map[string]bool{"key": true, "title": true, "hint": true, "kind": true, "required": true}
	captureKeys := map[string]bool{"key": true, "title": true, "hint": true, "kind": true, "required": true, "min_seconds": true}
	questionKeys := map[string]bool{"id": true, "kind": true, "title": true, "hint": true, "required": true, "options": true, "allow_other": true, "min": true, "max": true, "unit": true, "only_if": true}
	walk("", raw, map[string]bool{"schema_version": true, "feed_water_removal": true, "categories": true})
	walk("feed_water_removal.", raw["feed_water_removal"], map[string]bool{"mode": true, "applies_to": true, "cutoff_time": true, "instruction": true, "proofs": true, "questions": true})
	if fwr, ok := raw["feed_water_removal"].(map[string]any); ok {
		if proofs, ok := fwr["proofs"].([]any); ok {
			for i, p := range proofs {
				walk(fmt.Sprintf("feed_water_removal.proofs.%d.", i), p, slotKeys)
			}
		}
		if qs, ok := fwr["questions"].([]any); ok {
			for i, q := range qs {
				walk(fmt.Sprintf("feed_water_removal.questions.%d.", i), q, questionKeys)
			}
		}
	}
	if cats, ok := raw["categories"].(map[string]any); ok {
		for c, block := range cats {
			walk("categories."+c+".", block, map[string]bool{"instruction": true, "proofs": true, "questions": true, "repeat_every_days": true, "repeat_mode": true, "rotation_gap_days": true})
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if proofs, ok := b["proofs"].([]any); ok {
				for i, p := range proofs {
					walk(fmt.Sprintf("categories.%s.proofs.%d.", c, i), p, captureKeys)
				}
			}
			if qs, ok := b["questions"].([]any); ok {
				for i, q := range qs {
					walk(fmt.Sprintf("categories.%s.questions.%d.", c, i), q, questionKeys)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- Rule readers ---------------------------------------------------------------------------

// Category returns a category's card. A category the document does not carry (a kernel-owned
// one, or a document that somehow lost a block) reads the seeded card so a task is always
// workable; the validator refuses saving such a document.
func (r Rules) Category(category string) CategoryRules {
	if block := r.Categories[category]; block != nil {
		out := *block
		if out.Proofs == nil {
			out.Proofs = []CaptureSlot{}
		}
		if out.Questions == nil {
			out.Questions = []authored.Question{}
		}
		return out
	}
	if r.Version != 0 || r.Categories != nil {
		if block := SeededRules().Categories[category]; block != nil {
			return *block
		}
	}
	return CategoryRules{Proofs: []CaptureSlot{}, Questions: []authored.Question{}}
}

// CategorySlots returns a category's per-animal slots in the legacy Slot contract shape
// (field_key / label / description / min_duration_hint_seconds) so every served task detail
// keeps its wire fields while the LIST comes from the document. Kernel-owned categories keep
// SlotsForCategory.
func (r Rules) CategorySlots(category string) []Slot {
	if !isSOPCategory(category) {
		return SlotsForCategory(category)
	}
	block := r.Category(category)
	out := make([]Slot, 0, len(block.Proofs))
	for _, p := range block.Proofs {
		out = append(out, Slot{
			FieldKey:               p.Key,
			Label:                  p.Title,
			Description:            p.Hint,
			MinDurationHintSeconds: p.MinSeconds,
			Kind:                   p.Kind,
			Required:               p.Required,
		})
	}
	return out
}

// RepeatEveryDays is the category's repeat interval in days under these rules; 0 = no repeat.
func (r Rules) RepeatEveryDays(category string) int {
	if block := r.Categories[category]; block != nil && block.RepeatEveryDays > 0 {
		return block.RepeatEveryDays
	}
	return 0
}

// RotationGapDays reports whether a category rotates through the pens under these rules and, if
// so, the gap in days between the last pen of a round and the first of the next.
func (r Rules) RotationGapDays(category string) (int, bool) {
	if block := r.Categories[category]; block != nil && block.RepeatMode == RepeatModeRotation {
		return block.RotationGapDays, true
	}
	return 0, false
}

// CategoryQuestions returns a category's per-animal questions (never nil).
func (r Rules) CategoryQuestions(category string) []authored.Question {
	return r.Category(category).Questions
}

// SlotForCategory finds one authored slot of a category by key.
func (r Rules) SlotForCategory(category, key string) (CaptureSlot, bool) {
	for _, p := range r.Category(category).Proofs {
		if p.Key == key {
			return p, true
		}
	}
	return CaptureSlot{}, false
}

// RequiredSlotKeys lists the compulsory slot keys of a category, in slot order -- the submit
// readiness predicate's input.
func (r Rules) RequiredSlotKeys(category string) []string {
	out := []string{}
	for _, p := range r.Category(category).Proofs {
		if p.Required {
			out = append(out, p.Key)
		}
	}
	return out
}

// RemovalProofs returns the removal card's slots (never nil).
func (r Rules) RemovalProofs() []authored.ProofSlot {
	if r.FeedWaterRemoval.Proofs == nil {
		return []authored.ProofSlot{}
	}
	return r.FeedWaterRemoval.Proofs
}

// RemovalQuestions returns the removal card's questions (never nil).
func (r Rules) RemovalQuestions() []authored.Question {
	if r.FeedWaterRemoval.Questions == nil {
		return []authored.Question{}
	}
	return r.FeedWaterRemoval.Questions
}

// RemovalProof finds one removal slot by key.
func (r Rules) RemovalProof(key string) (authored.ProofSlot, bool) {
	for _, p := range r.FeedWaterRemoval.Proofs {
		if p.Key == key {
			return p, true
		}
	}
	return authored.ProofSlot{}, false
}

// RemovalAppliesTo reports whether the removal precondition may / must accompany a task of
// this category under these rules: never under `off`, otherwise only for a listed category.
func (r Rules) RemovalAppliesTo(category string) bool {
	if r.FeedWaterRemoval.Mode == RemovalModeOff {
		return false
	}
	for _, c := range r.FeedWaterRemoval.AppliesTo {
		if c == category {
			return true
		}
	}
	return false
}

// RemovalDecision is the SOP half of a create: whether THIS task carries the removal, given
// the mode, the category and what the planner asked (requested == nil means "not said").
//
//	required -> applies whenever the category is listed; a decline is ignored;
//	optional -> the planner's word; not said = not requested (the pre-SOP toggle default);
//	off      -> never; an explicit ask is refused by name (ErrRemovalNotOffered).
//
// A request on a category the removal does not apply to is refused (ErrFeedRemovalNotApplicable)
// rather than dropped, so a planner who ticked the toggle is never silently un-ticked.
func (r Rules) RemovalDecision(category string, requested *bool) (bool, error) {
	asked := requested != nil && *requested
	switch r.FeedWaterRemoval.Mode {
	case RemovalModeOff:
		if asked {
			return false, ErrRemovalNotOffered
		}
		return false, nil
	case RemovalModeRequired:
		if !r.RemovalAppliesTo(category) {
			if asked {
				return false, ErrFeedRemovalNotApplicable
			}
			return false, nil
		}
		return true, nil
	default:
		if asked && !r.RemovalAppliesTo(category) {
			return false, ErrFeedRemovalNotApplicable
		}
		return asked, nil
	}
}

// ServedRules is the rule set as CLIENTS read it: every list non-nil and every category filled,
// so a phone iterates the document without null checks. The version travels with it.
func (r Rules) ServedRules() Rules {
	out := r
	out.Categories = map[string]*CategoryRules{}
	for _, c := range SOPCategories {
		block := r.Category(c)
		out.Categories[c] = &block
	}
	out.FeedWaterRemoval.Proofs = r.RemovalProofs()
	out.FeedWaterRemoval.Questions = r.RemovalQuestions()
	if out.FeedWaterRemoval.AppliesTo == nil {
		out.FeedWaterRemoval.AppliesTo = []string{}
	}
	return out
}

// ErrRemovalNotOffered refuses a plan that asks for the removal while the SOP has it OFF.
// Surfaces as 422 feed_water_removal_not_offered.
var ErrRemovalNotOffered = errors.New("pccare: feed and water removal is switched off by the PC Care SOP")

// ErrSOPVersionUnknown: a task is pinned to a version the farm never published. Surfaces as
// 409 pc_care_sop_version_unknown.
var ErrSOPVersionUnknown = errors.New("pccare: sop version unknown")
