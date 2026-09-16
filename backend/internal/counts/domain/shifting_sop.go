package domain

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md).
//
// WHAT a raise asks for, WHAT a completion must capture and answer, and WHAT extra evidence a
// HIGH-PRIORITY movement carries are the `shifting` section of the PUBLISHED `shifting` SOP
// version, authored on /counts/sops. Three cards:
//
//   - raise         -- extras on the raise form (questions, optional captures). Shown to the park
//     head before approval and carried to the verifier with the completion. May be empty; may be
//     questions-only.
//   - completion    -- the captures and questions every completion carries (seeded: the one
//     shifting video).
//   - high_priority -- ADDED to the completion card when the movement is high priority (seeded:
//     the feed packing and feed given clips). The Feed Config fingerprint rule is not authored
//     here and still gates a high-priority completion; that is why this section keeps at least one
//     compulsory capture.
//
// THE PIN. A movement is stamped with the version in force when it is RAISED
// (shifting_events.sop_version); its raise card was judged on it and its completion runs on it to
// the end, whatever is published in between. Version 0 / NULL is the seeded document, which is
// the pre-SOP behaviour byte for byte. Queued verifier items are never relabelled.
//
// CARD-WITH-SLOTS, NOT THE TASKS ENGINE. Shifting keeps its atomic apply transaction, the
// approve-first gate, the fingerprint re-check, its idempotency and BounceShiftingEventForRework;
// the SOP decides only what the operator captures and answers. The seeded slot keys are the exact
// proof-register field keys the phones have always stamped (`shifting_<step>_video`), so a capture
// uploaded before this change is a capture of the seeded slot.

// ShiftingSOPSchemaVersion is the document's own version tag (not the SOP version number).
const ShiftingSOPSchemaVersion = "goatos.sop-shifting.v1"

// SOPCodeShifting is the library code the shifting rules are published under (000175; kept so the
// existing versions stay attached).
const SOPCodeShifting = "shifting"

// Sections of the shifting document.
const (
	SectionRaise        = "raise"
	SectionCompletion   = "completion"
	SectionHighPriority = "high_priority"
)

// The seeded slot keys: the proof-register field keys the phones already stamp.
const (
	SlotShiftingVideo        = "shifting_shifting_video"
	SlotShiftingPackingVideo = "shifting_packing_video"
	SlotShiftingFeedingVideo = "shifting_feeding_video"
)


// ShiftingCard is one section: what the operator is told, captures and answers.
type ShiftingCard struct {
	Instruction string               `json:"instruction,omitempty"`
	Proofs      []authored.ProofSlot `json:"proofs"`
	Questions   []authored.Question  `json:"questions"`
}

// ShiftingSOP is form_dsl.shifting.
type ShiftingSOP struct {
	SchemaVersion string        `json:"schema_version"`
	Raise         *ShiftingCard `json:"raise,omitempty"`
	Completion    *ShiftingCard `json:"completion,omitempty"`
	HighPriority  *ShiftingCard `json:"high_priority,omitempty"`
}

// ShiftingRules is the COMPILED, VERSIONED document a movement is pinned to and runs under.
// Version is sop_versions.version; 0 means the seeded document.
type ShiftingRules struct {
	Version      int
	Raise        ShiftingCard
	Completion   ShiftingCard
	HighPriority ShiftingCard
}

//go:embed sopseed/shifting.json
var seededShiftingJSON []byte

// SeededShiftingSOPJSON is the day-one document, embedded verbatim in migration 000324.
func SeededShiftingSOPJSON() []byte { return append([]byte(nil), seededShiftingJSON...) }

// SeededShiftingRules compiles the embedded document; a tenant with no published version runs it.
// It reproduces the pre-SOP behaviour exactly: one shifting video on every completion, the two
// feed clips on a high-priority one, nothing at raise, no questions.
func SeededShiftingRules() ShiftingRules {
	dsl, err := ParseShiftingSOP(map[string]any{"shifting": json.RawMessage(SeededShiftingSOPJSON())})
	if err != nil {
		panic("counts: seeded shifting sop does not parse: " + err.Error())
	}
	if problems := ValidateShiftingSOP(dsl); len(problems) > 0 {
		panic("counts: seeded shifting sop invalid: " + problems[0])
	}
	return dsl.Rules(0)
}

var ErrShiftingSOPInvalid = errors.New("shifting sop document invalid")

// ParseShiftingSOP reads form_dsl.shifting. A form_dsl without one is not a shifting SOP.
func ParseShiftingSOP(formDSL map[string]any) (ShiftingSOP, error) {
	raw, ok := formDSL["shifting"]
	if !ok || raw == nil {
		return ShiftingSOP{}, fmt.Errorf("%w: form_dsl.shifting missing", ErrShiftingSOPInvalid)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return ShiftingSOP{}, fmt.Errorf("%w: %v", ErrShiftingSOPInvalid, err)
	}
	var dsl ShiftingSOP
	if err := json.Unmarshal(b, &dsl); err != nil {
		return ShiftingSOP{}, fmt.Errorf("%w: %v", ErrShiftingSOPInvalid, err)
	}
	return dsl, nil
}

// Rules compiles the document at a version. A missing section reads as empty (never nil-sliced).
func (d ShiftingSOP) Rules(version int) ShiftingRules {
	return ShiftingRules{
		Version:      version,
		Raise:        normalizeShiftingCard(d.Raise),
		Completion:   normalizeShiftingCard(d.Completion),
		HighPriority: normalizeShiftingCard(d.HighPriority),
	}
}

func normalizeShiftingCard(c *ShiftingCard) ShiftingCard {
	if c == nil {
		return ShiftingCard{Proofs: []authored.ProofSlot{}, Questions: []authored.Question{}}
	}
	out := *c
	if out.Proofs == nil {
		out.Proofs = []authored.ProofSlot{}
	}
	if out.Questions == nil {
		out.Questions = []authored.Question{}
	}
	return out
}

// ValidateShiftingSOP returns every problem in the document, each naming its path, so the web
// editor can point at the field. An empty slice means the document compiles and can be published.
//
//   - completion: 1..8 slots with at least one compulsory -- the move must be proven by something
//     the verifier can see;
//   - high_priority: 1..8 slots with at least one compulsory -- the feed evidence cannot be
//     authored away while the Feed Config fingerprint still gates a high-priority completion;
//   - raise: 0..8 slots, may be questions-only or empty;
//   - slot keys unique across sections, question ids unique across sections.
func ValidateShiftingSOP(dsl ShiftingSOP) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if dsl.SchemaVersion != ShiftingSOPSchemaVersion {
		add("shifting.schema_version: want %q", ShiftingSOPSchemaVersion)
	}
	if dsl.Completion == nil {
		add("shifting.completion: required -- every completion must capture at least one proof")
	}
	if dsl.HighPriority == nil {
		add("shifting.high_priority: required -- a high-priority movement must carry feed evidence")
	}
	sections := []struct {
		name       string
		card       *ShiftingCard
		requireOne bool
	}{
		{SectionRaise, dsl.Raise, false},
		{SectionCompletion, dsl.Completion, true},
		{SectionHighPriority, dsl.HighPriority, true},
	}
	seenKeys := map[string]string{}
	seenQuestions := map[string]string{}
	for _, s := range sections {
		if s.card == nil {
			continue
		}
		path := "shifting." + s.name
		if len(s.card.Instruction) > authored.MaxTextLength {
			add("%s.instruction: too long", path)
		}
		if s.requireOne && len(s.card.Proofs) == 0 {
			add("%s.proofs: at least one compulsory capture is required", path)
		}
		authored.ValidateProofSlots(path+".proofs", s.card.Proofs, s.requireOne, add)
		authored.ValidateQuestions(path+".questions", s.card.Questions, add)
		for i, p := range s.card.Proofs {
			if other, dup := seenKeys[p.Key]; dup {
				add("%s.proofs.%d.key: %q is already a capture of %s", path, i, p.Key, other)
				continue
			}
			seenKeys[p.Key] = s.name
		}
		for i, q := range s.card.Questions {
			if other, dup := seenQuestions[q.ID]; dup {
				add("%s.questions.%d.id: %q is already a question of %s", path, i, q.ID, other)
				continue
			}
			seenQuestions[q.ID] = s.name
		}
	}
	return problems
}

// UnknownShiftingSOPKeys names every key the document carries that the schema does not, each by
// path. A save refuses them: a misspelt `proofs` would be dropped by the lenient parser and the
// farm would publish a card with no captures believing it authored one.
func UnknownShiftingSOPKeys(formDSL map[string]any) []string {
	raw, ok := formDSL["shifting"].(map[string]any)
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
	walk("", raw, map[string]bool{"schema_version": true, SectionRaise: true, SectionCompletion: true, SectionHighPriority: true})
	for _, s := range []string{SectionRaise, SectionCompletion, SectionHighPriority} {
		block, ok := raw[s].(map[string]any)
		if !ok {
			continue
		}
		walk(s+".", block, map[string]bool{"instruction": true, "proofs": true, "questions": true})
		if proofs, ok := block["proofs"].([]any); ok {
			for i, p := range proofs {
				walk(fmt.Sprintf("%s.proofs.%d.", s, i), p, map[string]bool{"key": true, "title": true, "hint": true, "kind": true, "required": true})
			}
		}
		if qs, ok := block["questions"].([]any); ok {
			for i, q := range qs {
				walk(fmt.Sprintf("%s.questions.%d.", s, i), q, map[string]bool{"id": true, "kind": true, "title": true, "hint": true, "required": true, "options": true, "allow_other": true, "min": true, "max": true, "unit": true, "only_if": true})
			}
		}
	}
	sort.Strings(out)
	return out
}

// --- Rule readers ---------------------------------------------------------------------------

func isHighPriority(priority string) bool {
	return strings.EqualFold(strings.TrimSpace(priority), ShiftingPriorityHigh)
}

// CompletionSlots is the capture list a completion of the given priority is judged against: the
// completion card, plus the high_priority card for a high movement, in that order.
func (r ShiftingRules) CompletionSlots(priority string) []authored.ProofSlot {
	out := append([]authored.ProofSlot{}, r.Completion.Proofs...)
	if isHighPriority(priority) {
		out = append(out, r.HighPriority.Proofs...)
	}
	return out
}

// CompletionQuestions is the question list a completion of the given priority answers.
func (r ShiftingRules) CompletionQuestions(priority string) []authored.Question {
	out := append([]authored.Question{}, r.Completion.Questions...)
	if isHighPriority(priority) {
		out = append(out, r.HighPriority.Questions...)
	}
	return out
}

// CompletionCard is the combined card (slots + questions) a completion runs under, as compiled
// rules a judge can validate against and a phone can render.
func (r ShiftingRules) CompletionCard(priority string) ShiftingCardRules {
	instruction := strings.TrimSpace(r.Completion.Instruction)
	return ShiftingCardRules{Version: r.Version, Stage: SectionCompletion, Instruction: instruction,
		Proofs: r.CompletionSlots(priority), Questions: r.CompletionQuestions(priority)}
}

// RaiseCard is the raise form's extras as compiled rules.
func (r ShiftingRules) RaiseCard() ShiftingCardRules {
	return ShiftingCardRules{Version: r.Version, Stage: SectionRaise, Instruction: strings.TrimSpace(r.Raise.Instruction),
		Proofs: append([]authored.ProofSlot{}, r.Raise.Proofs...), Questions: append([]authored.Question{}, r.Raise.Questions...)}
}

// HighPriorityCard is the high-priority section alone (for the phone's second controller).
func (r ShiftingRules) HighPriorityCard() ShiftingCardRules {
	return ShiftingCardRules{Version: r.Version, Stage: SectionHighPriority, Instruction: strings.TrimSpace(r.HighPriority.Instruction),
		Proofs: append([]authored.ProofSlot{}, r.HighPriority.Proofs...), Questions: append([]authored.Question{}, r.HighPriority.Questions...)}
}

// ShiftingCardRules is one compiled card: the SERVED shape the phone renders verbatim and the
// shape a submit is judged against. Never nil-sliced.
type ShiftingCardRules struct {
	Version     int                  `json:"version"`
	Stage       string               `json:"stage"`
	Instruction string               `json:"instruction,omitempty"`
	Proofs      []authored.ProofSlot `json:"proofs"`
	Questions   []authored.Question  `json:"questions"`
}

// Proof returns the slot for a key.
func (c ShiftingCardRules) Proof(key string) (authored.ProofSlot, bool) {
	for _, p := range c.Proofs {
		if p.Key == key {
			return p, true
		}
	}
	return authored.ProofSlot{}, false
}

// ValidateProofRefs judges a submit's captures against this card.
func (c ShiftingCardRules) ValidateProofRefs(refs authored.ProofRefs) ([]authored.OrderedProof, error) {
	return authored.ValidateProofRefs(c.Proofs, refs)
}

// ValidateAnswers judges a submit's answers against this card.
func (c ShiftingCardRules) ValidateAnswers(a authored.Answers) error {
	return authored.ValidateAnswers(c.Questions, a)
}

// NormalizeAnswers keeps the answers this card asked for.
func (c ShiftingCardRules) NormalizeAnswers(a authored.Answers) authored.Answers {
	return authored.NormalizeAnswers(c.Questions, a)
}

// LegacyShiftingProofRefs maps the fixed wire fields an OLDER phone still sends (proof_ref,
// feed_packing_proof_ref, feed_given_proof_ref) onto the seeded slot keys. The feed refs count
// only for a high-priority movement -- the pre-SOP write path stored them for high only. A blank
// legacy field contributes nothing. Explicit slot refs win over legacy fields for the same key.
func LegacyShiftingProofRefs(priority string, legacy map[string]string, explicit authored.ProofRefs) authored.ProofRefs {
	out := authored.NormalizeProofRefs(explicit)
	mapping := map[string]string{"proof_ref": SlotShiftingVideo}
	if isHighPriority(priority) {
		mapping["feed_packing_proof_ref"] = SlotShiftingPackingVideo
		mapping["feed_given_proof_ref"] = SlotShiftingFeedingVideo
	}
	for field, slot := range mapping {
		v := strings.TrimSpace(legacy[field])
		if v == "" {
			continue
		}
		if _, has := out[slot]; !has {
			out[slot] = v
		}
	}
	return out
}

// LegacyColumnsFromRefs is the reverse: the legacy columns every pre-existing reader still reads
// (proof_ref, feed_packing_proof_ref, feed_given_proof_ref). proof_ref mirrors the seeded shifting
// slot when the card still has it, else the FIRST judged capture -- it must never be NULL on a
// completed move (shiftingOutstandingActionSQL keys on it). orderedRefs is the judged captures in
// slot order.
func LegacyColumnsFromRefs(refs authored.ProofRefs, orderedRefs []string) (proofRef, packingRef, feedingRef string) {
	get := func(k string) string { return strings.TrimSpace(refs[k]) }
	proofRef = get(SlotShiftingVideo)
	if proofRef == "" && len(orderedRefs) > 0 {
		proofRef = strings.TrimSpace(orderedRefs[0])
	}
	return proofRef, get(SlotShiftingPackingVideo), get(SlotShiftingFeedingVideo)
}

// ShiftingVerificationKey is the verification item's idempotency key for one completion: the event
// plus its complete proof set. A seeded submission (no answers, no raise proofs) keeps the exact
// pre-SOP shape, so a retry from an older phone collapses onto the item it already created; answers
// or raise captures fold a digest in, so a rework carrying new answers is a new review.
func ShiftingVerificationKey(eventID string, orderedRefs []string, answers authored.Answers, raiseRefs []string) string {
	key := "counts-shifting-verification:" + eventID + ":" + strings.Join(orderedRefs, ":")
	if len(answers) == 0 && len(raiseRefs) == 0 {
		return key
	}
	h := sha256.New()
	ids := make([]string, 0, len(answers))
	for id := range answers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{0})
		h.Write(answers[id])
		h.Write([]byte{0})
	}
	h.Write([]byte{1})
	for _, r := range raiseRefs {
		h.Write([]byte(r))
		h.Write([]byte{0})
	}
	return key + ":" + hex.EncodeToString(h.Sum(nil))[:16]
}
