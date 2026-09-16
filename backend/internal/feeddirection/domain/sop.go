package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// FEED SOP (maintainer decision 2026-09-16, docs/decisions/feed-sop.md).
//
// The three feed SOPs stop being library documents. WHAT the crew must capture and answer at
// each stage of the feed chain is the `feed` section of the PUBLISHED SOP version, authored on
// /feed/sops:
//
//   - feed.direction  -> the DISTRIBUTION card (one pen-session, shared by every operator of the
//     park: today a feed weight PHOTO, a feed VIDEO and a water VIDEO) and the WASTAGE card of
//     an experiment pen (today one leftover-feed video);
//   - feed.packing    -> the PACKING card, one per bag (today one packing video);
//   - feed.transport  -> the TRANSPORT card, one per shed (today one transport video).
//
// Each card is a list of CAPTURE SLOTS -- key, title, hint, kind video / photo / either,
// compulsory or not -- plus optional QUESTIONS, the same building blocks as the weighing removal
// card (backend/internal/sop/authored). The maintainer's words: "now we have one photo and two
// videos for feed and water; if I add any other option in between it should come, if I remove
// anything it should go; same for packing, transport, wastage -- everything backend-driven."
//
// THE PIN. A feed day's sheet is stamped with the version in force when it is ISSUED
// (feed_direction_issues.sop_version) and every card of that sheet -- distribution, packing and
// wastage -- runs on that version to the end; a transport task is stamped when it is
// materialized. Publishing a new version changes tomorrow's sheet, never a sheet the crew is
// already packing against (the pin-at-start rule the herd-operations, procurement and weighing
// SOPs use). Version 0 is the seeded document, which is the pre-SOP behaviour byte for byte.
//
// THE SHARED SESSION IS UNCHANGED. A distribution pen-session is still ONE workspace filled by
// any mix of operators, slots stay independent and parallel, and the slot KEY is the proof
// register's field_key -- which is how every phone discovers what its teammates already
// captured. The seeded keys are the exact field keys the phones have always stamped, so a
// capture uploaded before this change is a capture of the seeded slot.

// FeedSOPSchemaVersion is the document's own version tag (not the SOP version number).
const FeedSOPSchemaVersion = "goatos.sop-feed.v1"

// The SOP library codes the feed rules are published under.
const (
	SOPCodeFeedDirection = "feed.direction"
	SOPCodeFeedPacking   = "feed.packing"
	SOPCodeFeedTransport = "feed.transport"
)

// Stages of the feed chain that carry an authored card.
const (
	StageDistribution = "distribution"
	StageWastage      = "wastage"
	StagePacking      = "packing"
	StageTransport    = "transport"
)

// The seeded slot keys. They are the proof register field keys the phones have always stamped,
// and the legacy wire fields (feed_weight_proof_ref / distribution_proof_ref / water_proof_ref,
// packing_proof_ref, proof_ref, wastage_proof_ref) map onto them when an older phone submits.
const (
	SlotFeedWeightPhoto = "feed_distribution_feed_weight_photo"
	SlotFeedVideo       = "feed_distribution_video"
	SlotWaterVideo      = "feed_distribution_water_video"
	SlotPackingVideo    = "feed_packing_video"
	SlotTransportVideo  = "feed_transport_video"
	SlotWastageVideo    = "feed_wastage_video"
)

// SOPCodeForStage names the SOP document a stage's card is authored in.
func SOPCodeForStage(stage string) (string, bool) {
	switch stage {
	case StageDistribution, StageWastage:
		return SOPCodeFeedDirection, true
	case StagePacking:
		return SOPCodeFeedPacking, true
	case StageTransport:
		return SOPCodeFeedTransport, true
	}
	return "", false
}

// StageRules is one card: what the crew is told, what they capture, what they answer.
type StageRules struct {
	Instruction string               `json:"instruction,omitempty"`
	Proofs      []authored.ProofSlot `json:"proofs"`
	Questions   []authored.Question  `json:"questions"`
}

// FeedSOP is form_dsl.feed. Which blocks are present depends on the SOP code the document is
// published under; ValidateFeedSOP requires the code's own blocks.
type FeedSOP struct {
	SchemaVersion string      `json:"schema_version"`
	Distribution  *StageRules `json:"distribution,omitempty"`
	Wastage       *StageRules `json:"wastage,omitempty"`
	Packing       *StageRules `json:"packing,omitempty"`
	Transport     *StageRules `json:"transport,omitempty"`
}

// Stage returns the block for a stage, nil when the document does not carry it.
func (d FeedSOP) Stage(stage string) *StageRules {
	switch stage {
	case StageDistribution:
		return d.Distribution
	case StageWastage:
		return d.Wastage
	case StagePacking:
		return d.Packing
	case StageTransport:
		return d.Transport
	}
	return nil
}

// Rules is one COMPILED, VERSIONED card: what a sheet or task was pinned to and runs under.
// Version is the SOP version number (sop_versions.version); 0 means the seeded document.
type Rules struct {
	Version int    `json:"version"`
	Stage   string `json:"stage"`
	StageRules
}

//go:embed sopseed/feed_direction.json
var seededFeedDirectionJSON []byte

//go:embed sopseed/feed_packing.json
var seededFeedPackingJSON []byte

//go:embed sopseed/feed_transport.json
var seededFeedTransportJSON []byte

// SeededFeedSOPJSON is the day-one document for a SOP code, embedded verbatim in the migration
// that adds it to each tenant's published version.
func SeededFeedSOPJSON(sopCode string) []byte {
	switch sopCode {
	case SOPCodeFeedDirection:
		return append([]byte(nil), seededFeedDirectionJSON...)
	case SOPCodeFeedPacking:
		return append([]byte(nil), seededFeedPackingJSON...)
	case SOPCodeFeedTransport:
		return append([]byte(nil), seededFeedTransportJSON...)
	}
	return nil
}

// SeededRules compiles the embedded document for a stage; a tenant with no published version
// runs it. It reproduces the pre-SOP behaviour exactly: the distribution card's photo + two
// videos, one packing video per bag, one transport video per shed, one wastage video per pen.
func SeededRules(stage string) Rules {
	code, ok := SOPCodeForStage(stage)
	if !ok {
		panic("feeddirection: unknown feed sop stage " + stage)
	}
	dsl, err := ParseFeedSOP(map[string]any{"feed": json.RawMessage(SeededFeedSOPJSON(code))})
	if err != nil {
		panic("feeddirection: seeded feed sop does not parse: " + err.Error())
	}
	if problems := ValidateFeedSOP(code, dsl); len(problems) > 0 {
		panic("feeddirection: seeded feed sop invalid: " + problems[0])
	}
	rules, err := dsl.Rules(0, stage)
	if err != nil {
		panic("feeddirection: seeded feed sop: " + err.Error())
	}
	return rules
}

var ErrFeedSOPInvalid = errors.New("feed sop document invalid")

// ErrFeedSOPStageMissing: the document carries no card for the requested stage.
var ErrFeedSOPStageMissing = errors.New("feed sop document has no card for this stage")

// ParseFeedSOP reads form_dsl.feed. A form_dsl without one is not a feed SOP.
func ParseFeedSOP(formDSL map[string]any) (FeedSOP, error) {
	raw, ok := formDSL["feed"]
	if !ok || raw == nil {
		return FeedSOP{}, fmt.Errorf("%w: form_dsl.feed missing", ErrFeedSOPInvalid)
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return FeedSOP{}, fmt.Errorf("%w: %v", ErrFeedSOPInvalid, err)
	}
	var dsl FeedSOP
	if err := json.Unmarshal(b, &dsl); err != nil {
		return FeedSOP{}, fmt.Errorf("%w: %v", ErrFeedSOPInvalid, err)
	}
	return dsl, nil
}

// Rules compiles the card for a stage at a version. A feed.direction document published before
// the wastage block existed reads the seeded wastage card, so a farm that authored only its
// distribution slots keeps its wastage task.
func (d FeedSOP) Rules(version int, stage string) (Rules, error) {
	block := d.Stage(stage)
	if block == nil {
		if stage == StageWastage {
			seeded := SeededRules(StageWastage)
			seeded.Version = version
			return seeded, nil
		}
		return Rules{}, fmt.Errorf("%w: %s", ErrFeedSOPStageMissing, stage)
	}
	out := Rules{Version: version, Stage: stage, StageRules: *block}
	if out.Proofs == nil {
		out.Proofs = []authored.ProofSlot{}
	}
	if out.Questions == nil {
		out.Questions = []authored.Question{}
	}
	return out, nil
}

// stagesForCode is the block set a SOP code's document must / may carry.
func stagesForCode(sopCode string) (required []string, optional []string, ok bool) {
	switch sopCode {
	case SOPCodeFeedDirection:
		return []string{StageDistribution}, []string{StageWastage}, true
	case SOPCodeFeedPacking:
		return []string{StagePacking}, nil, true
	case SOPCodeFeedTransport:
		return []string{StageTransport}, nil, true
	}
	return nil, nil, false
}

// IsFeedSOPCode reports whether a SOP code carries a feed document.
func IsFeedSOPCode(sopCode string) bool {
	_, _, ok := stagesForCode(sopCode)
	return ok
}

// ValidateFeedSOP returns every problem in the document for the SOP code it is published
// under, each naming its path, so the web editor can point at the field. An empty slice means
// the document compiles and can be published.
func ValidateFeedSOP(sopCode string, dsl FeedSOP) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if dsl.SchemaVersion != FeedSOPSchemaVersion {
		add("feed.schema_version: want %q", FeedSOPSchemaVersion)
	}
	required, optional, ok := stagesForCode(sopCode)
	if !ok {
		add("feed: %q is not a feed SOP code", sopCode)
		return problems
	}
	allowed := map[string]bool{}
	for _, s := range required {
		allowed[s] = true
		if dsl.Stage(s) == nil {
			add("feed.%s: required -- this document is the %s card", s, s)
		}
	}
	for _, s := range optional {
		allowed[s] = true
	}
	for _, s := range []string{StageDistribution, StageWastage, StagePacking, StageTransport} {
		block := dsl.Stage(s)
		if block == nil {
			continue
		}
		if !allowed[s] {
			add("feed.%s: not a card of %s", s, sopCode)
			continue
		}
		if len(block.Instruction) > authored.MaxTextLength {
			add("feed.%s.instruction: too long", s)
		}
		authored.ValidateProofSlots("feed."+s+".proofs", block.Proofs, true, add)
		authored.ValidateQuestions("feed."+s+".questions", block.Questions, add)
	}
	return problems
}

// UnknownFeedSOPKeys names every key the document carries that the schema does not, each by
// path. A save refuses them: a misspelt `proofs` would be dropped by the lenient parser and the
// farm would publish a card with no captures believing it authored three.
func UnknownFeedSOPKeys(formDSL map[string]any) []string {
	raw, ok := formDSL["feed"].(map[string]any)
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
	walk("", raw, map[string]bool{"schema_version": true, StageDistribution: true, StageWastage: true, StagePacking: true, StageTransport: true})
	for _, s := range []string{StageDistribution, StageWastage, StagePacking, StageTransport} {
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

// Proof returns the slot for a key.
func (r Rules) Proof(key string) (authored.ProofSlot, bool) {
	for _, p := range r.Proofs {
		if p.Key == key {
			return p, true
		}
	}
	return authored.ProofSlot{}, false
}

// SlotKeys lists the card's slot keys in order.
func (r Rules) SlotKeys() []string {
	out := make([]string, 0, len(r.Proofs))
	for _, p := range r.Proofs {
		out = append(out, p.Key)
	}
	return out
}

// ValidateProofRefs judges a submit's captures against this card (see authored.ValidateProofRefs).
func (r Rules) ValidateProofRefs(refs authored.ProofRefs) ([]authored.OrderedProof, error) {
	return authored.ValidateProofRefs(r.Proofs, refs)
}

// ValidateAnswers judges a submit's answers against this card.
func (r Rules) ValidateAnswers(a authored.Answers) error {
	return authored.ValidateAnswers(r.Questions, a)
}

// NormalizeAnswers keeps the answers this card asked for.
func (r Rules) NormalizeAnswers(a authored.Answers) authored.Answers {
	return authored.NormalizeAnswers(r.Questions, a)
}

// LegacyProofRefs maps the fixed wire fields an OLDER phone still sends onto the seeded slot
// keys, so a phone that predates the authored card is judged by the same rules. A blank legacy
// field contributes nothing. Explicit slot refs win over legacy fields for the same key.
func LegacyProofRefs(stage string, legacy map[string]string, explicit authored.ProofRefs) authored.ProofRefs {
	out := authored.NormalizeProofRefs(explicit)
	var mapping map[string]string
	switch stage {
	case StageDistribution:
		mapping = map[string]string{"feed_weight_proof_ref": SlotFeedWeightPhoto, "distribution_proof_ref": SlotFeedVideo, "water_proof_ref": SlotWaterVideo}
	case StagePacking:
		mapping = map[string]string{"packing_proof_ref": SlotPackingVideo}
	case StageTransport:
		mapping = map[string]string{"proof_ref": SlotTransportVideo}
	case StageWastage:
		mapping = map[string]string{"wastage_proof_ref": SlotWastageVideo}
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

// LegacyFieldsFromRefs is the reverse: the seeded slots' refs, for the legacy columns every
// pre-existing reader (the verifier item, leadership reads, exports) still reads.
func LegacyFieldsFromRefs(stage string, refs authored.ProofRefs) (feedWeight, feed, water, packing, transport, wastage string) {
	get := func(k string) string { return strings.TrimSpace(refs[k]) }
	switch stage {
	case StageDistribution:
		return get(SlotFeedWeightPhoto), get(SlotFeedVideo), get(SlotWaterVideo), "", "", ""
	case StagePacking:
		return "", "", "", get(SlotPackingVideo), "", ""
	case StageTransport:
		return "", "", "", "", get(SlotTransportVideo), ""
	case StageWastage:
		return "", "", "", "", "", get(SlotWastageVideo)
	}
	return "", "", "", "", "", ""
}

// CardContract is the SERVED shape of a card: what the phone renders verbatim -- the version it is
// pinned to, the instruction, the capture slots and the questions. It rides every stage read
// (distribution captures, packing worklist, transport task, wastage worklist) so a phone never
// holds a slot list of its own.
type CardContract struct {
	Version     int                  `json:"version"`
	Stage       string               `json:"stage"`
	Instruction string               `json:"instruction,omitempty"`
	Proofs      []authored.ProofSlot `json:"proofs"`
	Questions   []authored.Question  `json:"questions"`
}

// NewCardContract projects compiled rules onto the wire shape, never nil-sliced.
func NewCardContract(r Rules) CardContract {
	proofs := r.Proofs
	if proofs == nil {
		proofs = []authored.ProofSlot{}
	}
	questions := r.Questions
	if questions == nil {
		questions = []authored.Question{}
	}
	return CardContract{Version: r.Version, Stage: r.Stage, Instruction: strings.TrimSpace(r.Instruction), Proofs: proofs, Questions: questions}
}

// --- Older app (program decision 7, 2026-09-16) -----------------------------------------------

// legacyFieldOrder is each stage's fixed wire fields in capture order, with the kind an older phone
// always captured for it. Order matters: a field whose seeded slot the farm authored away is
// re-filed onto the first free slot of that kind, and the earlier field gets first pick.
func legacyFieldOrder(stage string) []struct{ Field, Slot, Kind string } {
	type f = struct{ Field, Slot, Kind string }
	switch stage {
	case StageDistribution:
		return []f{{"feed_weight_proof_ref", SlotFeedWeightPhoto, authored.KindPhoto}, {"distribution_proof_ref", SlotFeedVideo, authored.KindVideo}, {"water_proof_ref", SlotWaterVideo, authored.KindVideo}}
	case StagePacking:
		return []f{{"packing_proof_ref", SlotPackingVideo, authored.KindVideo}}
	case StageTransport:
		return []f{{"proof_ref", SlotTransportVideo, authored.KindVideo}}
	case StageWastage:
		return []f{{"wastage_proof_ref", SlotWastageVideo, authored.KindVideo}}
	}
	return nil
}

// LegacyCardRefs lays an OLDER phone's fixed proof fields onto the card it is judged by. A field
// lands on its seeded slot when the card still has that slot and the slot still takes what the
// phone captured (a photo for the weight, a video otherwise); otherwise it is re-filed onto the
// first free slot of the card that takes it; otherwise it has nowhere to land and is kept under the
// reserved older-app key of its kind (authored.OlderAppKey) -- never dropped, and never refusing the
// pen, which would force an update the farm has ruled out. Nothing an older phone sends is ever
// re-filed onto a slot of another KIND.
func LegacyCardRefs(r Rules, legacy map[string]string) authored.ProofRefs {
	out := authored.ProofRefs{}
	taken := map[string]bool{}
	var pending []struct{ ref, kind string }
	slotByKey := map[string]authored.ProofSlot{}
	for _, p := range r.Proofs {
		slotByKey[p.Key] = p
	}
	for _, f := range legacyFieldOrder(r.Stage) {
		ref := strings.TrimSpace(legacy[f.Field])
		if ref == "" {
			continue
		}
		if slot, ok := slotByKey[f.Slot]; ok && slot.Accepts(f.Kind) {
			out[f.Slot], taken[f.Slot] = ref, true
			continue
		}
		pending = append(pending, struct{ ref, kind string }{ref, f.Kind})
	}
	reserved := map[string]int{}
	for _, p := range pending {
		placed := false
		for _, slot := range r.Proofs {
			if taken[slot.Key] || !slot.Accepts(p.kind) {
				continue
			}
			// A seeded slot still on the card is reserved for its own field, even when this phone
			// left that field blank, so a re-filed capture never impersonates the seeded one.
			if isSeededSlot(r.Stage, slot.Key) {
				continue
			}
			out[slot.Key], taken[slot.Key] = p.ref, true
			placed = true
			break
		}
		if !placed {
			reserved[p.kind]++
			out[authored.OlderAppKey(p.kind, reserved[p.kind])] = p.ref
		}
	}
	return out
}

// OlderAppSlotsFor appends a pseudo slot for every reserved older-app key in refs (sorted), so the
// card an older phone is judged by accepts, orders and labels those captures.
func (r Rules) OlderAppSlotsFor(refs authored.ProofRefs) Rules {
	keys := OlderAppKeys(refs)
	if len(keys) == 0 {
		return r
	}
	out := r
	out.Proofs = append(make([]authored.ProofSlot, 0, len(r.Proofs)+len(keys)), r.Proofs...)
	for _, k := range keys {
		out.Proofs = append(out.Proofs, authored.OlderAppSlot(k))
	}
	return out
}

// OlderAppKeys is the sorted reserved older-app keys a proof map carries.
func OlderAppKeys(refs authored.ProofRefs) []string {
	var keys []string
	for k, v := range refs {
		if authored.IsOlderAppKey(k) && strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func isSeededSlot(stage, key string) bool {
	for _, f := range legacyFieldOrder(stage) {
		if f.Slot == key {
			return true
		}
	}
	return false
}

// NotCapturedByOlderApp names, in card order, every compulsory capture the stored proofs lack and
// every required question that applies and was not answered. A strictly judged submit can leave
// nothing here, so a non-empty list is exactly what an older phone could not send; the verifier
// reads each as "<title> · Not captured (older app)".
func (r Rules) NotCapturedByOlderApp(stored authored.ProofRefs, answers authored.Answers) []string {
	var out []string
	for _, p := range r.Proofs {
		if p.Required && strings.TrimSpace(stored[p.Key]) == "" {
			out = append(out, p.Title)
		}
	}
	applies := map[string]bool{}
	for _, q := range r.Questions {
		cond := q.OnlyIf
		applies[q.ID] = cond == nil || (applies[cond.QuestionID] && answerChoice(answers, cond.QuestionID) == cond.Value)
		if !q.Required || !applies[q.ID] {
			continue
		}
		if raw, ok := answers[q.ID]; !ok || len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
			out = append(out, q.Title)
		}
	}
	return out
}

func answerChoice(a authored.Answers, id string) string {
	raw, ok := a[id]
	if !ok {
		return ""
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// OlderAppCopy is the card an older phone's request is judged by: what it DID send is judged
// (placement, kind, offered answers), what it could not send is not required. Every authored
// capture and question it has never heard of becomes optional. A SEEDED capture stays compulsory
// when its fixed field came in BLANK -- an older phone always sent every seeded field, so a blank
// one is a broken request, not an older app (the legacy "proof_required" rule, unchanged).
func (r Rules) OlderAppCopy(legacy map[string]string) Rules {
	blankSeeded := map[string]bool{}
	for _, f := range legacyFieldOrder(r.Stage) {
		if strings.TrimSpace(legacy[f.Field]) == "" {
			blankSeeded[f.Slot] = true
		}
	}
	out := r
	out.Proofs = make([]authored.ProofSlot, len(r.Proofs))
	for i, p := range r.Proofs {
		p.Required = p.Required && blankSeeded[p.Key]
		out.Proofs[i] = p
	}
	out.Questions = make([]authored.Question, len(r.Questions))
	for i, q := range r.Questions {
		q.Required = false
		out.Questions[i] = q
	}
	return out
}
