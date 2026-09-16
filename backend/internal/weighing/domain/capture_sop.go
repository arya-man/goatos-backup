package domain

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16, docs/decisions/weighing-sop.md
// -> "The weigh captures are authored").
//
// Two SEPARATE capture sections, authored independently and never merged:
//
//   - PER ANIMAL (capture.individual): named proof slots (video / photo / either, compulsory or
//     not, at least one compulsory -- the per-animal video stays locked ON through
//     video_required) plus questions the operator answers per animal. RFID scan + weight are
//     fixed and not authorable.
//   - WHOLE PEN (capture.lump_sum): COUNTED proof slots (each with min..max captures) plus
//     questions answered once per pen submit.
//
// The seeded document (sopseed/weighing_session.json, embedded verbatim by migration 000315 and
// therefore NEVER edited) carries no slot lists. The defaults live in the sibling embedded file
// sopseed/weighing_capture_slots.json and are filled in by the accessors below, so a document
// published before slots existed -- and every test fake built from VideoMin/VideoMax alone --
// keeps today's behaviour: one compulsory "Weighing video" per animal, 1..5 "Weighing video"
// per pen.
//
// What stays LOCKED (AGENTS.md "WEIGHING IS SCAN-AND-SUBMIT"): free-flow capture, the one
// business rule (no duplicate scan in a pen before submit -- raw string, untouched here),
// evidence-grain verification (one item per animal / per pen), approve-carries-the-weight, the
// unconditional close gate. The document decides WHAT the operator captures, never what the
// kernel does with it.

// The seeded slot keys. Only the seed's: a version may rename or drop them. The legacy wire
// fields (`proof_artifact_id` alone / `proof_artifact_ids`) map onto them when an older phone
// submits (LegacyIndividualRefs / LegacyLumpSumRefs).
const (
	IndividualProofAnimalVideo = "animal_video"
	LumpSumProofPenVideo       = "pen_video"
)

// Slot ceilings. MaxShedProofArtifacts (5) is now the PER-SLOT ceiling of a whole-pen slot and
// the mirror window older clients read; MaxLumpSumProofsTotal is the row's ceiling (migration
// 000328 widens proof_position to 1..10).
const (
	MaxIndividualProofSlots = 4
	MaxLumpSumProofSlots    = 4
	MaxLumpSumProofsTotal   = 10
	maxCaptureQuestions     = 20
)

// NotCapturedOlderApp is the context row value the verifier and approver read for an authored
// item an OLDER app (installed before slots existed) could not have sent. Never a refusal: an
// older app is never forced to update (program decision 7).
const NotCapturedOlderApp = "Not captured (older app)"

// OLDER APP VIDEO (program decision 7): an older app sends videos only. When the pinned version has
// no slot that can take one, the video is still ACCEPTED and kept under this RESERVED key (and
// "_2", "_3" for further clips), never an authored slot -- ValidateWeighingSOP refuses the key --
// and the verifier reads it as OlderAppVideoLabel.
const (
	OlderAppVideoKey   = "older_app_video"
	OlderAppVideoLabel = "Recorded on an older app"
)

// IsOlderAppVideoKey reports whether a slot key is the reserved older-app key or a numbered form.
func IsOlderAppVideoKey(key string) bool {
	return key == OlderAppVideoKey || strings.HasPrefix(key, OlderAppVideoKey+"_")
}

// OlderAppVideoKeyN is the reserved key of the n-th older-app video (1-based).
func OlderAppVideoKeyN(n int) string {
	if n <= 1 {
		return OlderAppVideoKey
	}
	return OlderAppVideoKey + "_" + strconv.Itoa(n)
}

// OlderAppVideoSlot is the pseudo slot a reserved key reads as: never authored, always a video.
func OlderAppVideoSlot(key string) RemovalProofSlot {
	return RemovalProofSlot{Key: key, Title: OlderAppVideoLabel, Kind: RemovalProofKindVideo}
}

// CountedProofSlot is one whole-pen capture slot carrying Min..Max captures (Min 0 = optional).
type CountedProofSlot struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Hint  string `json:"hint,omitempty"`
	Kind  string `json:"kind"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
}

// Accepts reports whether the slot takes a capture of the given proof type.
func (p CountedProofSlot) Accepts(proofType string) bool {
	return RemovalProofSlot{Kind: p.Kind}.Accepts(proofType)
}

//go:embed sopseed/weighing_capture_slots.json
var seededCaptureSlotsJSON []byte

type seededCaptureSlots struct {
	Individual struct {
		Proofs    []RemovalProofSlot `json:"proofs"`
		Questions []SOPQuestion      `json:"questions"`
	} `json:"individual"`
	LumpSum struct {
		Proofs    []CountedProofSlot `json:"proofs"`
		Questions []SOPQuestion      `json:"questions"`
	} `json:"lump_sum"`
}

func seededCaptureDefaults() seededCaptureSlots {
	var s seededCaptureSlots
	if err := json.Unmarshal(seededCaptureSlotsJSON, &s); err != nil {
		panic("weighing: seeded capture slots do not parse: " + err.Error())
	}
	return s
}

// SeededCaptureSlotsJSON is the embedded default slot document, served to the web editor on the
// page contract so the web model never imports backend JSON.
func SeededCaptureSlotsJSON() []byte { return append([]byte(nil), seededCaptureSlotsJSON...) }

// --- Accessors: never nil, defaults filled from the embedded file ------------------------

// IndividualProofs is the per-animal slot list: the document's own, else the seeded slot.
func (r Rules) IndividualProofs() []RemovalProofSlot {
	if r.Capture.Individual.Proofs != nil {
		return r.Capture.Individual.Proofs
	}
	return append([]RemovalProofSlot(nil), seededCaptureDefaults().Individual.Proofs...)
}

// LumpSumProofs is the whole-pen slot list: the document's own, else the seeded pen_video slot
// carrying the document's OWN video window (a version published with only video_min/video_max
// keeps the window it published).
func (r Rules) LumpSumProofs() []CountedProofSlot {
	if r.Capture.LumpSum.Proofs != nil {
		return r.Capture.LumpSum.Proofs
	}
	out := append([]CountedProofSlot(nil), seededCaptureDefaults().LumpSum.Proofs...)
	if len(out) > 0 {
		lo, hi := r.Capture.LumpSum.VideoMin, r.Capture.LumpSum.VideoMax
		if lo < 1 {
			lo = 1
		}
		if hi < lo {
			hi = lo
		}
		if hi > MaxShedProofArtifacts {
			hi = MaxShedProofArtifacts
		}
		out[0].Min, out[0].Max = lo, hi
	}
	return out
}

// IndividualQuestions / LumpSumQuestions are the two question lists; never nil, never shared.
func (r Rules) IndividualQuestions() []SOPQuestion {
	if r.Capture.Individual.Questions == nil {
		return []SOPQuestion{}
	}
	return r.Capture.Individual.Questions
}

func (r Rules) LumpSumQuestions() []SOPQuestion {
	if r.Capture.LumpSum.Questions == nil {
		return []SOPQuestion{}
	}
	return r.Capture.LumpSum.Questions
}

// IndividualSlotKinds / LumpSumSlotKinds are {slot key: kind} for the store's kind check.
func (r Rules) IndividualSlotKinds() map[string]string {
	out := map[string]string{OlderAppVideoKey: RemovalProofKindVideo}
	for _, s := range r.IndividualProofs() {
		out[s.Key] = s.Kind
	}
	return out
}

func (r Rules) LumpSumSlotKinds() map[string]string {
	out := map[string]string{OlderAppVideoKey: RemovalProofKindVideo}
	for _, s := range r.LumpSumProofs() {
		out[s.Key] = s.Kind
	}
	return out
}

// LumpSumProofsTotalMax is the pinned Σmax over the whole-pen slots -- the leadership
// "N of MaxShedVideos" denominator.
func (r Rules) LumpSumProofsTotalMax() int {
	total := 0
	for _, s := range r.LumpSumProofs() {
		total += s.Max
	}
	if total < 1 {
		total = 1
	}
	return total
}

// ServedRules is the rule set as CLIENTS read it: both capture sections filled explicitly
// (slots + questions) and the legacy video_min / video_max kept as a DERIVED MIRROR (Σ of the
// video / either slots' windows, clamped to 1..5) for phones that predate slots.
func (r Rules) ServedRules() Rules {
	out := r
	out.Capture.Individual.Proofs = r.IndividualProofs()
	out.Capture.Individual.Questions = r.IndividualQuestions()
	out.Capture.LumpSum.Proofs = r.LumpSumProofs()
	out.Capture.LumpSum.Questions = r.LumpSumQuestions()
	out.Capture.LumpSum.VideoMin, out.Capture.LumpSum.VideoMax = legacyVideoWindow(out.Capture.LumpSum.Proofs)
	return out
}

// LegacyVideoWindow is the window an OLDER phone (pre-slots) renders and is judged on: the
// derived mirror over the whole-pen slots. The ONLY sanctioned read of the legacy window
// outside this package (weighing-sop-guard, lump-sum-window-literal).
func (r Rules) LegacyVideoWindow() (int, int) {
	return legacyVideoWindow(r.LumpSumProofs())
}

// legacyVideoWindow derives the older clients' window from the whole-pen slots.
func legacyVideoWindow(slots []CountedProofSlot) (int, int) {
	lo, hi := 0, 0
	for _, s := range slots {
		if s.Kind == RemovalProofKindVideo || s.Kind == RemovalProofKindEither {
			lo += s.Min
			hi += s.Max
		}
	}
	if lo < 1 {
		lo = 1
	}
	if hi > MaxShedProofArtifacts {
		hi = MaxShedProofArtifacts
	}
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// --- Validation of the document's capture sections ------------------------------------

func validateCaptureSections(dsl WeighingSOP, add func(string, ...any)) {
	// PER ANIMAL. The locked rule first: the per-animal video stays ON.
	if !dsl.Capture.Individual.VideoRequired {
		add("weighing.capture.individual.video_required: fixed to true -- at least one compulsory capture per animal is the evidence the verifier reviews")
	}
	if ind := dsl.Capture.Individual.Proofs; ind != nil {
		if len(ind) < 1 || len(ind) > MaxIndividualProofSlots {
			add("weighing.capture.individual.proofs: 1..%d captures per animal", MaxIndividualProofSlots)
		}
		if len(ind) > MaxIndividualProofSlots {
			add("weighing.capture.individual.proofs: at most %d captures per animal", MaxIndividualProofSlots)
		}
		required := 0
		seen := map[string]bool{}
		for i, p := range ind {
			pp := fmt.Sprintf("weighing.capture.individual.proofs.%d", i)
			validateSlotIdentity(pp, p.Key, p.Title, p.Kind, seen, add)
			if p.Required {
				required++
			}
		}
		if required == 0 {
			add("weighing.capture.individual.proofs: at least one compulsory capture per animal")
		}
	}
	if len(dsl.Capture.Individual.Questions) > maxCaptureQuestions {
		add("weighing.capture.individual.questions: at most %d", maxCaptureQuestions)
	}
	validateSOPQuestions("weighing.capture.individual.questions", dsl.Capture.Individual.Questions, add)

	// WHOLE PEN. With explicit slots the legacy window is a mirror and is not judged; without
	// them the window IS the rule (the document a farm published before slots existed).
	if lump := dsl.Capture.LumpSum.Proofs; lump != nil {
		if len(lump) < 1 || len(lump) > MaxLumpSumProofSlots {
			add("weighing.capture.lump_sum.proofs: 1..%d capture slots per pen", MaxLumpSumProofSlots)
		}
		if len(lump) > MaxLumpSumProofSlots {
			add("weighing.capture.lump_sum.proofs: at most %d capture slots per pen", MaxLumpSumProofSlots)
		}
		compulsory := 0
		total := 0
		seen := map[string]bool{}
		for i, p := range lump {
			pp := fmt.Sprintf("weighing.capture.lump_sum.proofs.%d", i)
			validateSlotIdentity(pp, p.Key, p.Title, p.Kind, seen, add)
			if p.Min < 0 {
				add("%s.min: at least 0", pp)
			}
			if p.Max < 1 || p.Max > MaxShedProofArtifacts {
				add("%s.max: 1..%d", pp, MaxShedProofArtifacts)
			}
			if p.Min > p.Max {
				add("%s.min: must not exceed max", pp)
			}
			if p.Min >= 1 {
				compulsory++
			}
			total += p.Max
		}
		if compulsory == 0 {
			add("weighing.capture.lump_sum.proofs: at least one compulsory capture -- a whole-pen weigh must be proven by something the verifier can see")
		}
		if total > MaxLumpSumProofsTotal {
			add("weighing.capture.lump_sum.proofs: at most %d captures per pen in total", MaxLumpSumProofsTotal)
		}
	} else {
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
	}
	if len(dsl.Capture.LumpSum.Questions) > maxCaptureQuestions {
		add("weighing.capture.lump_sum.questions: at most %d", maxCaptureQuestions)
	}
	validateSOPQuestions("weighing.capture.lump_sum.questions", dsl.Capture.LumpSum.Questions, add)
}

func validateSlotIdentity(pp, key, title, kind string, seen map[string]bool, add func(string, ...any)) {
	if !sopIDPattern.MatchString(key) {
		add("%s.key: %q must be a-z, 0-9 and _ (start with a letter)", pp, key)
	}
	if IsOlderAppVideoKey(key) {
		add("%s.key: %q is reserved for a video recorded on an older app", pp, key)
	}
	if seen[key] {
		add("%s.key: %q is listed twice", pp, key)
	}
	seen[key] = true
	if strings.TrimSpace(title) == "" {
		add("%s.title: required", pp)
	}
	switch kind {
	case RemovalProofKindVideo, RemovalProofKindPhoto, RemovalProofKindEither:
	default:
		add("%s.kind: %q is not video / photo / either", pp, kind)
	}
}

// --- The weigh submits, judged slot by slot ------------------------------------------------

// ErrCaptureProofInvalid wraps every ProofError raised on a WEIGH capture (per animal or whole
// pen); distinct from ErrSOPProofInvalid (the removal card) so the two 422 codes never blur.
var ErrCaptureProofInvalid = errors.New("weighing: capture proof invalid")

// ErrCaptureAnswerInvalid wraps every AnswerError raised on a weigh capture's questions.
var ErrCaptureAnswerInvalid = errors.New("weighing: capture answer invalid")

func captureProofInvalid(key, msg string) error {
	return fmt.Errorf("%w: %w", ErrCaptureProofInvalid, &ProofError{SlotKey: key, Message: msg})
}

func captureAnswerInvalid(id, msg string) error {
	return fmt.Errorf("%w: %w", ErrCaptureAnswerInvalid, &AnswerError{QuestionID: id, Message: msg})
}

// IndividualProofRefs is {slot key: proof ref} as a per-animal capture carries it.
type IndividualProofRefs map[string]string

// ValidateIndividualProofRefs judges one animal's captures against THESE rules: every compulsory
// slot carries a ref, no unknown slot, no ref in two slots. Returns the refs in SLOT ORDER --
// the first is the PRIMARY (stored on proof_artifact_id and compared by the verdict applier).
func (r Rules) ValidateIndividualProofRefs(refs IndividualProofRefs) ([]string, error) {
	slots := r.IndividualProofs()
	known := map[string]bool{}
	for _, p := range slots {
		known[p.Key] = true
	}
	for key := range refs {
		if !known[key] {
			return nil, captureProofInvalid(key, "This capture is not part of the weighing.")
		}
	}
	seen := map[string]string{}
	ordered := make([]string, 0, len(refs))
	for _, p := range slots {
		ref := strings.TrimSpace(refs[p.Key])
		if ref == "" {
			if p.Required {
				return nil, captureProofInvalid(p.Key, "Record: "+p.Title)
			}
			continue
		}
		if other, dup := seen[ref]; dup {
			return nil, captureProofInvalid(p.Key, "The same capture cannot prove both "+other+" and "+p.Title+".")
		}
		seen[ref] = p.Title
		ordered = append(ordered, ref)
	}
	if len(ordered) == 0 {
		return nil, captureProofInvalid("", "Record at least one capture.")
	}
	return ordered, nil
}

// NormalizeIndividualProofRefs drops blank entries.
func NormalizeIndividualProofRefs(refs IndividualProofRefs) IndividualProofRefs {
	out := IndividualProofRefs{}
	for k, v := range refs {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
	return out
}

// LumpSumProofRefs is {slot key: proof refs} as a whole-pen submit carries it.
type LumpSumProofRefs map[string][]string

// OrderedCapture is one whole-pen capture in slot order with its position inside its slot.
type OrderedCapture struct {
	Ref     string
	SlotKey string
	Index   int
	Count   int
}

// ValidateLumpSumProofRefs judges one pen's captures: each slot within its min..max, no unknown
// slot, no ref twice, total within MaxLumpSumProofsTotal. Returns the captures in slot order.
func (r Rules) ValidateLumpSumProofRefs(refs LumpSumProofRefs) ([]OrderedCapture, error) {
	slots := r.LumpSumProofs()
	known := map[string]bool{}
	for _, p := range slots {
		known[p.Key] = true
	}
	for key := range refs {
		if !known[key] {
			return nil, captureProofInvalid(key, "This capture is not part of the weighing.")
		}
	}
	seen := map[string]string{}
	var out []OrderedCapture
	for _, p := range slots {
		list := make([]string, 0, len(refs[p.Key]))
		for _, ref := range refs[p.Key] {
			if strings.TrimSpace(ref) != "" {
				list = append(list, strings.TrimSpace(ref))
			}
		}
		if len(list) < p.Min {
			if len(list) == 0 {
				return nil, captureProofInvalid(p.Key, "Record: "+p.Title)
			}
			return nil, captureProofInvalid(p.Key, fmt.Sprintf("%s needs at least %d.", p.Title, p.Min))
		}
		if len(list) > p.Max {
			return nil, captureProofInvalid(p.Key, fmt.Sprintf("%s takes at most %d.", p.Title, p.Max))
		}
		for i, ref := range list {
			if other, dup := seen[ref]; dup {
				return nil, captureProofInvalid(p.Key, "The same capture cannot prove both "+other+" and "+p.Title+".")
			}
			seen[ref] = p.Title
			out = append(out, OrderedCapture{Ref: ref, SlotKey: p.Key, Index: i + 1, Count: len(list)})
		}
	}
	if len(out) == 0 {
		return nil, captureProofInvalid("", "Record at least one capture.")
	}
	if len(out) > MaxLumpSumProofsTotal {
		return nil, captureProofInvalid("", fmt.Sprintf("At most %d captures per pen.", MaxLumpSumProofsTotal))
	}
	return out, nil
}

// NormalizeLumpSumProofRefs drops blank refs and empty slots.
func NormalizeLumpSumProofRefs(refs LumpSumProofRefs) LumpSumProofRefs {
	out := LumpSumProofRefs{}
	for k, list := range refs {
		var kept []string
		for _, v := range list {
			if strings.TrimSpace(v) != "" {
				kept = append(kept, strings.TrimSpace(v))
			}
		}
		if len(kept) > 0 {
			out[k] = kept
		}
	}
	return out
}

// LegacyIndividualRefs maps an OLDER app's single proof onto the pinned document: the seeded
// slot when the document still names it, else the first compulsory slot that takes a video,
// else the first slot that takes a video. Empty when no slot can take it (a photo-only
// document): the caller refuses by name.
func LegacyIndividualRefs(r Rules, primary string) IndividualProofRefs {
	primary = strings.TrimSpace(primary)
	if primary == "" {
		return IndividualProofRefs{}
	}
	slots := r.IndividualProofs()
	if key := legacyIndividualTarget(slots); key != "" {
		return IndividualProofRefs{key: primary}
	}
	// No slot can take a video: still accepted, under the reserved key (never force an update).
	return IndividualProofRefs{OlderAppVideoKey: primary}
}

func legacyIndividualTarget(slots []RemovalProofSlot) string {
	for _, s := range slots {
		if s.Key == IndividualProofAnimalVideo && s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	for _, s := range slots {
		if s.Required && s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	for _, s := range slots {
		if s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	return ""
}

// LegacyLumpSumRefs maps an OLDER app's flat video list onto the pinned document, the same way.
func LegacyLumpSumRefs(r Rules, ids []string) LumpSumProofRefs {
	var list []string
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			list = append(list, strings.TrimSpace(id))
		}
	}
	if len(list) == 0 {
		return LumpSumProofRefs{}
	}
	slots := r.LumpSumProofs()
	if key := legacyLumpSumTarget(slots); key != "" {
		return LumpSumProofRefs{key: list}
	}
	// No slot can take a video: still accepted, under the reserved key (never force an update).
	return LumpSumProofRefs{OlderAppVideoKey: list}
}

func legacyLumpSumTarget(slots []CountedProofSlot) string {
	for _, s := range slots {
		if s.Key == LumpSumProofPenVideo && s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	for _, s := range slots {
		if s.Min >= 1 && s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	for _, s := range slots {
		if s.Accepts(RemovalProofKindVideo) {
			return s.Key
		}
	}
	return ""
}

// LegacyLumpSumOrdered lays an older app's flat list onto its target slot WITHOUT the per-slot
// window judgement (the older phone was shown the derived mirror window and judged on it), so
// the captures still carry a slot key and a "k of N" label for the verifier.
func LegacyLumpSumOrdered(refs LumpSumProofRefs) []OrderedCapture {
	var out []OrderedCapture
	keys := make([]string, 0, len(refs))
	for k := range refs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for i, ref := range refs[k] {
			out = append(out, OrderedCapture{Ref: ref, SlotKey: k, Index: i + 1, Count: len(refs[k])})
		}
	}
	return out
}

// IndividualNotCapturedRows names every compulsory per-animal item an older app did not send:
// each compulsory slot without a ref and each required question without an answer, as context
// rows the verifier and approver read verbatim.
func (r Rules) IndividualNotCapturedRows(refs IndividualProofRefs, answers SOPAnswers) []AnswerRow {
	var out []AnswerRow
	for _, s := range r.IndividualProofs() {
		if s.Required && strings.TrimSpace(refs[s.Key]) == "" {
			out = append(out, AnswerRow{Label: s.Title, Value: NotCapturedOlderApp})
		}
	}
	out = append(out, notAnsweredRows(r.IndividualQuestions(), answers)...)
	return out
}

// LumpSumNotCapturedRows is the whole-pen twin.
func (r Rules) LumpSumNotCapturedRows(refs LumpSumProofRefs, answers SOPAnswers) []AnswerRow {
	var out []AnswerRow
	for _, s := range r.LumpSumProofs() {
		if s.Min >= 1 && len(refs[s.Key]) == 0 {
			out = append(out, AnswerRow{Label: s.Title, Value: NotCapturedOlderApp})
		}
	}
	out = append(out, notAnsweredRows(r.LumpSumQuestions(), answers)...)
	return out
}

func notAnsweredRows(questions []SOPQuestion, answers SOPAnswers) []AnswerRow {
	var out []AnswerRow
	applicable := applicableSOPQuestions(questions, answers)
	for _, q := range questions {
		if !q.Required || !applicable[q.ID] {
			continue
		}
		if _, ok := answers[q.ID]; ok {
			continue
		}
		out = append(out, AnswerRow{Label: q.Title, Value: NotCapturedOlderApp})
	}
	return out
}

// CaptureMediaLabel titles one capture of a slot for the verifier: a slot carrying one capture
// is titled plainly ("Weighing video"); several are numbered ("Weighing video 2 of 3").
func CaptureMediaLabel(title string, index, count int) string {
	if count <= 1 {
		return title
	}
	return title + " " + strconv.Itoa(index) + " of " + strconv.Itoa(count)
}

// --- Answers on the two capture sections (each judged against its OWN question list) --------

func (r Rules) ValidateIndividualAnswers(a SOPAnswers) error {
	return wrapCaptureAnswer(validateSOPAnswers(r.IndividualQuestions(), a, "This question is not part of the per-animal weighing."))
}

func (r Rules) NormalizeIndividualAnswers(a SOPAnswers) SOPAnswers {
	return normalizeSOPAnswers(r.IndividualQuestions(), a)
}

func (r Rules) IndividualAnswerRows(a SOPAnswers) []AnswerRow {
	return sopAnswerRows(r.IndividualQuestions(), a)
}

func (r Rules) ValidateLumpSumAnswers(a SOPAnswers) error {
	return wrapCaptureAnswer(validateSOPAnswers(r.LumpSumQuestions(), a, "This question is not part of the whole-pen weighing."))
}

func (r Rules) NormalizeLumpSumAnswers(a SOPAnswers) SOPAnswers {
	return normalizeSOPAnswers(r.LumpSumQuestions(), a)
}

func (r Rules) LumpSumAnswerRows(a SOPAnswers) []AnswerRow {
	return sopAnswerRows(r.LumpSumQuestions(), a)
}

// wrapCaptureAnswer re-wraps the shared engine's AnswerError under the capture sentinel.
func wrapCaptureAnswer(err error) error {
	if err == nil {
		return nil
	}
	var ae *AnswerError
	if errors.As(err, &ae) {
		return captureAnswerInvalid(ae.QuestionID, ae.Message)
	}
	return err
}
