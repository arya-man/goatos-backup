package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// SHIFTING SOP CARDS (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md). A raise's
// extras and a completion's captures + answers are judged against the document the movement is
// PINNED to. This file is the one place that resolves a card and judges a submit against it; the
// raise handler and Complete call it and store what it returns. ports.ErrShiftingProofSlotInvalid
// is minted HERE and nowhere else.

// NotCapturedOlderApp is the value of a context / capture row naming an authored item an OLDER
// app (one predating the SOP fields) could not send. Backend-owned farm copy, rendered verbatim.
const NotCapturedOlderApp = "Not captured (older app)"

// Context-row groups the verifier and approver read the answers under.
const (
	GroupAtRaise      = "At raise"
	GroupCompletion   = "Completion"
	GroupHighPriority = "High priority"
)

// WithSOPRules wires the rules source and the pin store. Either may be nil: an unwired source
// runs the seeded document; an unwired store skips the pre-lock pin read (the repository still
// gates completion under the row lock).
func (s *ShiftingExecutionService) WithSOPRules(rules ports.ShiftingSOPRulesSource, store ports.ShiftingSOPStore) *ShiftingExecutionService {
	s.sopRules = rules
	s.sopStore = store
	return s
}

// WithProofMedia wires the proof-register check for slot kinds. nil skips it (unit tests).
func (s *ShiftingExecutionService) WithProofMedia(media ports.ShiftingProofMedia) *ShiftingExecutionService {
	s.proofMedia = media
	return s
}

func (s *ShiftingExecutionService) publishedRules(ctx context.Context, tenantID string) (domain.ShiftingRules, error) {
	if s.sopRules == nil {
		return domain.SeededShiftingRules(), nil
	}
	return s.sopRules.PublishedRules(ctx, tenantID)
}

func (s *ShiftingExecutionService) pinnedRules(ctx context.Context, tenantID string, version *int) (domain.ShiftingRules, error) {
	if s.sopRules == nil || version == nil || *version == 0 {
		return domain.SeededShiftingRules(), nil
	}
	return s.sopRules.RulesVersion(ctx, tenantID, *version)
}

// PublishedRaiseCard is the raise form's extras a phone renders now (served on the destinations
// read), with the version a raise from that form will pin.
func (s *ShiftingExecutionService) PublishedRaiseCard(ctx context.Context, tenantID string) (domain.ShiftingCardRules, error) {
	rules, err := s.publishedRules(ctx, tenantID)
	if err != nil {
		return domain.ShiftingCardRules{}, err
	}
	return rules.RaiseCard(), nil
}

// judgedShiftingProof is one accepted capture: the slot, its ref and the kind the register says it
// is, in slot order.
type judgedShiftingProof struct {
	Slot authored.ProofSlot
	Ref  string
	Kind string
}

// shiftingJudgement is what a judged submit stores and queues.
type shiftingJudgement struct {
	Proofs  []judgedShiftingProof
	Stored  authored.ProofRefs
	Answers authored.Answers
	// Missing names, in card order, every compulsory slot / required question an OLDER app did
	// not send. Always empty for a strictly judged (new-shaped) request.
	Missing []string
}

// judgeShiftingCard validates a submit's captures and answers against a card.
//
//  1. every compulsory slot carries a ref, no ref is outside the card, no ref proves two slots
//     (authored.ValidateProofRefs) -- ErrShiftingProofSlotInvalid names the slot;
//  2. every ref resolves in the proof register to a completed, tenant-owned upload OF THE SLOT'S
//     KIND; photo slots demand the live camera;
//  3. every applicable required question is answered with an offered answer
//     (authored.ValidateAnswers) -- ErrShiftingAnswerInvalid names the question.
//
// OLDER APP (legacyShape): a request that predates the SOP fields is ACCEPTED -- what it sent is
// judged for kind and placement, and every compulsory item it could not send is returned in
// Missing rather than refused. Forcing an update is not an option (program decision 7).
func (s *ShiftingExecutionService) judgeShiftingCard(
	ctx context.Context, tenantID string, card domain.ShiftingCardRules,
	refs authored.ProofRefs, answers authored.Answers, legacyShape bool,
) (shiftingJudgement, error) {
	slots, questions := card.Proofs, card.Questions
	if legacyShape {
		slots, questions = optionalCopy(card.Proofs), optionalQuestions(card.Questions)
	}
	ordered, err := authored.ValidateProofRefs(slots, refs)
	if err != nil {
		return shiftingJudgement{}, fmt.Errorf("%w: %w", ports.ErrShiftingProofSlotInvalid, err)
	}
	if err := authored.ValidateAnswers(questions, answers); err != nil {
		return shiftingJudgement{}, fmt.Errorf("%w: %w", ports.ErrShiftingAnswerInvalid, err)
	}
	judged := make([]judgedShiftingProof, 0, len(ordered))
	for _, o := range ordered {
		judged = append(judged, judgedShiftingProof{Slot: o.Slot, Ref: o.Ref, Kind: o.Slot.Kind})
	}
	if s.proofMedia != nil && len(judged) > 0 {
		expected := make([]ports.ExpectedShiftingProofMedia, 0, len(judged))
		ids := make([]string, 0, len(judged))
		for _, j := range judged {
			ids = append(ids, j.Ref)
			expected = append(expected, ports.ExpectedShiftingProofMedia{
				ProofID:           j.Ref,
				Kind:              j.Slot.Kind,
				RequireLiveCamera: j.Slot.Kind == authored.KindPhoto,
				OnAbsent:          fmt.Errorf("%w: %w", ports.ErrShiftingProofSlotInvalid, &authored.ProofError{SlotKey: j.Slot.Key, Message: slotKindMessage(j.Slot)}),
			})
		}
		if err := s.proofMedia.ValidateShiftingProofMedia(ctx, tenantID, expected); err != nil {
			return shiftingJudgement{}, err
		}
		if kinds, derr := s.proofMedia.DescribeShiftingProofMedia(ctx, tenantID, ids); derr == nil {
			for i := range judged {
				if k, ok := kinds[judged[i].Ref]; ok && k != "" {
					judged[i].Kind = k
				}
			}
		}
	}
	stored := authored.ProofRefs{}
	for _, j := range judged {
		stored[j.Slot.Key] = j.Ref
	}
	normalized := authored.NormalizeAnswers(questions, answers)
	return shiftingJudgement{Proofs: judged, Stored: stored, Answers: normalized,
		Missing: notCaptured(card, stored, normalized)}, nil
}

func optionalCopy(slots []authored.ProofSlot) []authored.ProofSlot {
	out := make([]authored.ProofSlot, len(slots))
	for i, p := range slots {
		p.Required = false
		out[i] = p
	}
	return out
}

func optionalQuestions(qs []authored.Question) []authored.Question {
	out := make([]authored.Question, len(qs))
	for i, q := range qs {
		q.Required = false
		out[i] = q
	}
	return out
}

// notCaptured names every compulsory slot and required, applicable question of the card that the
// stored proofs / answers do not carry -- in card order.
func notCaptured(card domain.ShiftingCardRules, stored authored.ProofRefs, answers authored.Answers) []string {
	var out []string
	for _, p := range card.Proofs {
		if p.Required && strings.TrimSpace(stored[p.Key]) == "" {
			out = append(out, p.Title)
		}
	}
	for _, q := range card.Questions {
		if !q.Required || !questionApplies(q, answers) {
			continue
		}
		if _, ok := answers[q.ID]; !ok {
			out = append(out, q.Title)
		}
	}
	return out
}

func questionApplies(q authored.Question, a authored.Answers) bool {
	if q.OnlyIf == nil {
		return true
	}
	raw, ok := a[q.OnlyIf.QuestionID]
	if !ok {
		return false
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return strings.TrimSpace(v) == q.OnlyIf.Value
}

func slotKindMessage(slot authored.ProofSlot) string {
	switch slot.Kind {
	case authored.KindPhoto:
		return "Take a live-camera photo for: " + slot.Title
	case authored.KindVideo:
		return "Record a video for: " + slot.Title
	default:
		return "Record a video or take a photo for: " + slot.Title
	}
}

// legacyCompletionRefs maps an older app's fixed fields onto the pinned card. The seeded keys are
// tried first (domain.LegacyShiftingProofRefs); a seeded key the farm authored AWAY is re-targeted
// onto the first free video-accepting slot of the same section, so an older phone's clip still
// lands on the card rather than being refused as "not part of this card".
func legacyCompletionRefs(rules domain.ShiftingRules, priority string, legacy map[string]string, explicit authored.ProofRefs) authored.ProofRefs {
	refs := domain.LegacyShiftingProofRefs(priority, legacy, explicit)
	retarget := func(seededKey string, section []authored.ProofSlot) {
		ref, has := refs[seededKey]
		if !has {
			return
		}
		for _, p := range section {
			if p.Key == seededKey {
				return
			}
		}
		delete(refs, seededKey)
		for _, p := range section {
			if _, taken := refs[p.Key]; taken || !p.Accepts(authored.KindVideo) {
				continue
			}
			refs[p.Key] = ref
			return
		}
	}
	retarget(domain.SlotShiftingVideo, rules.Completion.Proofs)
	if strings.EqualFold(strings.TrimSpace(priority), domain.ShiftingPriorityHigh) {
		retarget(domain.SlotShiftingPackingVideo, rules.HighPriority.Proofs)
		retarget(domain.SlotShiftingFeedingVideo, rules.HighPriority.Proofs)
	}
	return refs
}

// --- Raise ----------------------------------------------------------------------------------

// RaiseJudgeInput is a raise's SOP half: the version the phone echoed (nil = whatever is
// published now), whether the request predates the SOP fields, and its captures and answers.
type RaiseJudgeInput struct {
	TenantID      string
	EchoedVersion *int
	LegacyShape   bool
	Proofs        authored.ProofRefs
	Answers       authored.Answers
}

// RaiseJudgement is what a judged raise pins and stores.
type RaiseJudgement struct {
	Version int
	Proofs  authored.ProofRefs
	Answers authored.Answers
	// Capture is the approver's snapshot, in the shared CountsApprovalCapture shape.
	Capture domain.CountsApprovalCapture
}

// JudgeRaise resolves the raise card (the echoed known version, else the published one) and
// judges the raise's captures and answers against it. Nothing is written here.
func (s *ShiftingExecutionService) JudgeRaise(ctx context.Context, in RaiseJudgeInput) (RaiseJudgement, error) {
	var rules domain.ShiftingRules
	var err error
	if in.EchoedVersion != nil {
		rules, err = s.pinnedRules(ctx, in.TenantID, in.EchoedVersion)
	} else {
		rules, err = s.publishedRules(ctx, in.TenantID)
	}
	if err != nil {
		return RaiseJudgement{}, err
	}
	card := rules.RaiseCard()
	judged, err := s.judgeShiftingCard(ctx, in.TenantID, card, authored.NormalizeProofRefs(in.Proofs), in.Answers, in.LegacyShape)
	if err != nil {
		return RaiseJudgement{}, err
	}
	capture := domain.CountsApprovalCapture{
		VersionLabel: versionLabel(rules.Version),
		Rows:         []domain.CountsApprovalCaptureRow{},
		Media:        []domain.CountsApprovalCaptureMedia{},
	}
	for _, row := range authored.AnswerRows(card.Questions, judged.Answers) {
		capture.Rows = append(capture.Rows, domain.CountsApprovalCaptureRow{Label: row.Title, Value: row.Value, Group: GroupAtRaise})
	}
	for _, m := range judged.Missing {
		capture.Rows = append(capture.Rows, domain.CountsApprovalCaptureRow{Label: m, Value: NotCapturedOlderApp, Group: GroupAtRaise})
	}
	for _, j := range judged.Proofs {
		capture.Media = append(capture.Media, domain.CountsApprovalCaptureMedia{ProofID: j.Ref, Label: j.Slot.Title, Kind: concreteKind(j.Kind)})
	}
	if len(judged.Missing) > 0 {
		capture.MissingNote = NotCapturedOlderAppNote(judged.Missing)
	}
	return RaiseJudgement{Version: rules.Version, Proofs: judged.Stored, Answers: judged.Answers, Capture: capture}, nil
}

// NotCapturedOlderAppNote composes the approver's one-line note for what an older app did not send.
func NotCapturedOlderAppNote(missing []string) string {
	return NotCapturedOlderApp + ": " + strings.Join(missing, ", ")
}

func versionLabel(version int) string {
	if version == 0 {
		return "Standard SOP"
	}
	return "SOP v" + strconv.Itoa(version)
}

// concreteKind is a capture kind the verifier can play without asking: video or photo, else "".
func concreteKind(kind string) string {
	switch kind {
	case authored.KindVideo, authored.KindPhoto:
		return kind
	default:
		return ""
	}
}

// --- Enqueue composition ---------------------------------------------------------------------

// completionMedia orders the row's STORED completion captures by the pinned card, pairing each with
// its slot title and the kind this request judged (falling back to the slot's own kind; an `either`
// nobody judged stays blank and the queue answers it from the register).
func completionMedia(card domain.ShiftingCardRules, stored authored.ProofRefs, judged []judgedShiftingProof) ([]string, []ports.ProofMeta) {
	kindOf := map[string]string{}
	for _, j := range judged {
		kindOf[j.Ref] = j.Kind
	}
	refs := make([]string, 0, len(stored))
	meta := make([]ports.ProofMeta, 0, len(stored))
	for _, p := range card.Proofs {
		ref := strings.TrimSpace(stored[p.Key])
		if ref == "" {
			continue
		}
		kind := concreteKind(kindOf[ref])
		if kind == "" {
			kind = concreteKind(p.Kind)
		}
		refs = append(refs, ref)
		meta = append(meta, ports.ProofMeta{Label: p.Title, Kind: kind})
	}
	return refs, meta
}

// raiseMedia appends the raise card's captures, labelled "At raise · <title>", with the kind the
// raise snapshot recorded.
func raiseMedia(card domain.ShiftingCardRules, stored authored.ProofRefs, snapshot json.RawMessage) ([]string, []ports.ProofMeta) {
	kindOf := map[string]string{}
	if len(snapshot) > 0 {
		var capture domain.CountsApprovalCapture
		if err := json.Unmarshal(snapshot, &capture); err == nil {
			for _, m := range capture.Media {
				kindOf[m.ProofID] = m.Kind
			}
		}
	}
	refs := make([]string, 0, len(stored))
	meta := make([]ports.ProofMeta, 0, len(stored))
	for _, p := range card.Proofs {
		ref := strings.TrimSpace(stored[p.Key])
		if ref == "" {
			continue
		}
		kind := concreteKind(kindOf[ref])
		if kind == "" {
			kind = concreteKind(p.Kind)
		}
		refs = append(refs, ref)
		meta = append(meta, ports.ProofMeta{Label: GroupAtRaise + " · " + p.Title, Kind: kind})
	}
	return refs, meta
}

// answerRows renders a card's stored answers plus its not-captured items under one group.
func answerRows(card domain.ShiftingCardRules, answers authored.Answers, missing []string, group string) []VerificationContextRow {
	var out []VerificationContextRow
	for _, row := range authored.AnswerRows(card.Questions, answers) {
		out = append(out, VerificationContextRow{Label: row.Title, Value: row.Value, Group: group})
	}
	for _, m := range missing {
		out = append(out, VerificationContextRow{Label: m, Value: NotCapturedOlderApp, Group: group})
	}
	return out
}

// SOPProofSlotError / SOPAnswerError unwrap the slot key / question id for the HTTP layer.
func SOPProofSlotError(err error) (key, message string, ok bool) {
	var pe *authored.ProofError
	if errors.Is(err, ports.ErrShiftingProofSlotInvalid) && errors.As(err, &pe) {
		return pe.SlotKey, pe.Message, true
	}
	return "", "", false
}

func SOPAnswerError(err error) (id, message string, ok bool) {
	var ae *authored.AnswerError
	if errors.Is(err, ports.ErrShiftingAnswerInvalid) && errors.As(err, &ae) {
		return ae.QuestionID, ae.Message, true
	}
	return "", "", false
}
