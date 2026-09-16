package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// FEED SOP CARDS (maintainer decision 2026-09-16). Every feed stage's completion is judged against
// the CARD the sheet (or transport task) was pinned to: which captures, of which kind, compulsory
// or not, and which questions. This file is the one place that resolves a card and judges a
// submit against it; the four completion services call it and store what it returns.

// publishedRules is the card a sheet issued NOW is stamped with.
func (s *Service) publishedRules(ctx context.Context, tenantID, stage string) (domain.Rules, error) {
	if s.sopRules == nil {
		return domain.SeededRules(stage), nil
	}
	return s.sopRules.PublishedRules(ctx, tenantID, stage)
}

// pinnedRules is the card a sheet/task runs under: its pinned version, or the seed for 0.
func (s *Service) pinnedRules(ctx context.Context, tenantID, stage string, version int) (domain.Rules, error) {
	if s.sopRules == nil || version == 0 {
		return domain.SeededRules(stage), nil
	}
	return s.sopRules.RulesVersion(ctx, tenantID, stage, version)
}

// sheetRules is the card a stage of the sheet for (park, feed day, workflow) runs under: the
// version pinned on the issue header (direction for distribution / wastage, packing for packing).
// No sheet yet (a completion racing the first read) reads as the seed, the honest pre-SOP answer.
func (s *Service) sheetRules(ctx context.Context, tenantID, parkID, feedDay, workflow, stage string) (domain.Rules, error) {
	version := 0
	if s.issues != nil {
		headers, err := s.issues.LoadIssueHeaders(ctx, tenantID, parkID, feedDay, workflow)
		if err != nil {
			return domain.Rules{}, err
		}
		if len(headers) > 0 {
			if stage == domain.StagePacking {
				version = headers[0].PackingSOPVersion
			} else {
				version = headers[0].SOPVersion
			}
		}
	}
	return s.pinnedRules(ctx, tenantID, stage, version)
}

// sheetRulesForWrite is sheetRules for a COMPLETION write: the sheet must exist (when the issue
// store is wired -- an unwired service, as in the store-only tests, judges the seeded card as
// before), and a distribution or wastage day must have been reached. Packing is exempt from the
// day check because a bag is packed the day BEFORE its feed day.
func (s *Service) sheetRulesForWrite(ctx context.Context, tenantID, parkID, feedDay, workflow, stage string) (domain.Rules, error) {
	if s.issues != nil {
		headers, err := s.issues.LoadIssueHeaders(ctx, tenantID, parkID, feedDay, workflow)
		if err != nil {
			return domain.Rules{}, err
		}
		if len(headers) == 0 {
			return domain.Rules{}, ports.ErrSheetNotIssued
		}
	}
	if stage != domain.StagePacking {
		day, err := time.ParseInLocation("2006-01-02", feedDay, biztime.DefaultLocation())
		if err == nil && day.After(biztime.BusinessDayStart(s.now())) {
			return domain.Rules{}, ports.ErrFeedDayNotReached
		}
	}
	return s.sheetRules(ctx, tenantID, parkID, feedDay, workflow, stage)
}

// judgedProof is one accepted capture: the slot it proves, its ref and the kind the register says
// it is (an `either` slot's answer), in slot order -- what the store keeps and the verifier item
// is built from.
type judgedProof struct {
	Slot authored.ProofSlot
	Ref  string
	Kind string
}

// judgeCard validates a submit's captures and answers against a card:
//
//  1. legacy fixed fields (an older phone) are mapped onto the seeded slot keys, explicit slot
//     refs winning for the same key;
//  2. every compulsory slot carries a ref, no ref is outside the card, no ref proves two slots
//     (authored.ValidateProofRefs) -- ErrSOPProofSlotInvalid names the slot;
//  3. every ref resolves in the proof register to a completed, tenant-owned upload OF THE SLOT'S
//     KIND (a photo in a video slot is refused); photo slots demand the live camera, as the weight
//     photo always has;
//  4. every applicable required question is answered and every answer is one the card offered
//     (authored.ValidateAnswers) -- ErrSOPAnswerInvalid names the question.
//
// It returns the accepted captures in slot order, the normalized proof map to store and the
// normalized answers to store.
func (s *Service) judgeCard(
	ctx context.Context, tenantID string, rules domain.Rules,
	legacy map[string]string, explicit authored.ProofRefs, answers authored.Answers,
) ([]judgedProof, authored.ProofRefs, authored.Answers, error) {
	// OLDER APP (program decision 7): a request with no card-shaped `proofs` predates the card. It is
	// never forced to update -- its fixed fields are laid onto the card (domain.LegacyCardRefs) and
	// judged for what it DID send; what it could not send is shown to the verifier as
	// "Not captured (older app)" (cardContextRows). A request carrying `proofs` is the new app and
	// is judged strictly.
	judgedBy := rules
	var refs authored.ProofRefs
	if len(authored.NormalizeProofRefs(explicit)) == 0 {
		refs = domain.LegacyCardRefs(rules, legacy)
		if len(refs) == 0 && len(rules.Proofs) > 0 {
			// Nothing the older phone sent fits the card: a submit with no capture cannot become a
			// verifier item (decision 6). Refused by the strict judge, which names the slot.
			if _, err := rules.ValidateProofRefs(refs); err != nil {
				return nil, nil, nil, fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, err)
			}
			return nil, nil, nil, fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, &authored.ProofError{Message: "Record a capture for this card."})
		}
		judgedBy = rules.OlderAppCopy(legacy)
	} else {
		refs = domain.LegacyProofRefs(rules.Stage, legacy, explicit)
	}
	ordered, err := judgedBy.ValidateProofRefs(refs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, err)
	}
	if err := judgedBy.ValidateAnswers(answers); err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %w", ports.ErrSOPAnswerInvalid, err)
	}
	judged := make([]judgedProof, 0, len(ordered))
	for _, o := range ordered {
		judged = append(judged, judgedProof{Slot: o.Slot, Ref: o.Ref, Kind: o.Slot.Kind})
	}
	if s.proofs != nil && len(judged) > 0 {
		expected := make([]ports.ExpectedProofMedia, 0, len(judged))
		for _, j := range judged {
			expected = append(expected, ports.ExpectedProofMedia{
				ProofID:           j.Ref,
				Kind:              ports.MediaKind(j.Slot.Kind),
				RequireLiveCamera: j.Slot.Kind == authored.KindPhoto,
				OnAbsent:          fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, &authored.ProofError{SlotKey: j.Slot.Key, Message: slotKindMessage(j.Slot)}),
			})
		}
		if err := s.proofs.ValidateFeedProofMedia(ctx, tenantID, expected); err != nil {
			return nil, nil, nil, err
		}
		// An `either` slot stores and reports the kind the register judged the capture to be.
		if describer, ok := s.proofs.(ports.ProofMediaDescriber); ok {
			kinds, derr := describer.DescribeFeedProofMedia(ctx, tenantID, proofIDs(judged))
			if derr == nil {
				for i := range judged {
					if k, ok := kinds[judged[i].Ref]; ok && k != "" {
						judged[i].Kind = string(k)
					}
				}
			}
		}
	}
	stored := authored.ProofRefs{}
	for _, j := range judged {
		stored[j.Slot.Key] = j.Ref
	}
	return judged, stored, rules.NormalizeAnswers(answers), nil
}

func proofIDs(judged []judgedProof) []string {
	out := make([]string, 0, len(judged))
	for _, j := range judged {
		out = append(out, j.Ref)
	}
	return out
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

// orderedRefs is the refs in slot order, for the verifier item's media list.
func orderedRefs(judged []judgedProof) []string {
	out := make([]string, 0, len(judged))
	for _, j := range judged {
		out = append(out, j.Ref)
	}
	return out
}

// proofMeta is the per-proof {label, kind} the verifier item carries beside the refs, so the queue
// names each capture by the card's own title and plays it as the kind it is.
//
// A kind nobody judged -- an `either` slot the register could not describe -- is left BLANK, never
// guessed as a video: the verifier queue answers a blank kind from the proof register at read time.
func proofMeta(judged []judgedProof) []ports.ProofMeta {
	out := make([]ports.ProofMeta, 0, len(judged))
	for _, j := range judged {
		out = append(out, ports.ProofMeta{Label: j.Slot.Title, Kind: concreteKind(j.Kind)})
	}
	return out
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

// cardContextRows is the verifier item's context rows for a card: the crew's answers in question
// order, then one "<title> · Not captured (older app)" row per compulsory capture or required
// question the STORED work lacks. Only an older phone's request can store such a gap (a new app's
// is refused), so the rows appear exactly where decision 7 says they must, and a repair retry
// recomputes the same rows from the row it re-enqueues.
func cardContextRows(rules domain.Rules, stored authored.ProofRefs, answers authored.Answers) []authored.AnswerRow {
	rows := authored.AnswerRows(rules.Questions, answers)
	for _, title := range rules.NotCapturedByOlderApp(stored, answers) {
		rows = append(rows, authored.AnswerRow{Title: title, Value: authored.MissingNoteOlderApp})
	}
	return rows
}

// proofsForEnqueue is answersForEnqueue for the captures: the row's stored map when the store
// returned one.
func proofsForEnqueue(fromRow, judged authored.ProofRefs) authored.ProofRefs {
	if len(fromRow) > 0 {
		return fromRow
	}
	return judged
}

// answersForEnqueue prefers the answers the STORE returned for the row over the ones this request
// judged: an already-pending repair retry must queue what the crew stored. A store that returns
// none (a fake, a pre-card caller) keeps the judged answers.
func answersForEnqueue(fromRow, judged authored.Answers) authored.Answers {
	if fromRow != nil {
		return fromRow
	}
	return judged
}

// SOPProofSlotError / SOPAnswerError unwrap the slot key / question id for the HTTP layer.
func SOPProofSlotError(err error) (key, message string, ok bool) {
	var pe *authored.ProofError
	if errors.Is(err, ports.ErrSOPProofSlotInvalid) && errors.As(err, &pe) {
		return pe.SlotKey, pe.Message, true
	}
	return "", "", false
}

func SOPAnswerError(err error) (id, message string, ok bool) {
	var ae *authored.AnswerError
	if errors.Is(err, ports.ErrSOPAnswerInvalid) && errors.As(err, &ae) {
		return ae.QuestionID, ae.Message, true
	}
	return "", "", false
}

func cardContract(r domain.Rules) domain.CardContract { return domain.NewCardContract(r) }

// canonicalOrderedRefs orders the STORED {slot: ref} map by the card, so the verifier item lists
// exactly the row's captures in slot order (a repair retry must never queue request media the row
// does not hold).
func canonicalOrderedRefs(rules domain.Rules, stored authored.ProofRefs, judged []judgedProof) []string {
	if len(stored) == 0 {
		return orderedRefs(judged)
	}
	out := make([]string, 0, len(stored))
	for _, p := range rules.Proofs {
		if ref := strings.TrimSpace(stored[p.Key]); ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

// canonicalProofMeta pairs each stored capture with its slot title and the kind the register judged
// it to be (falling back to the slot's own kind for a ref this request did not judge). An `either`
// slot nobody judged stays blank -- unknown, answered by the proof register at read time -- and is
// never guessed as a video.
func canonicalProofMeta(rules domain.Rules, stored authored.ProofRefs, judged []judgedProof) []ports.ProofMeta {
	if len(stored) == 0 {
		return proofMeta(judged)
	}
	kindOf := map[string]string{}
	for _, j := range judged {
		kindOf[j.Ref] = j.Kind
	}
	out := make([]ports.ProofMeta, 0, len(stored))
	for _, p := range rules.Proofs {
		ref := strings.TrimSpace(stored[p.Key])
		if ref == "" {
			continue
		}
		kind := concreteKind(kindOf[ref])
		if kind == "" {
			kind = concreteKind(p.Kind)
		}
		out = append(out, ports.ProofMeta{Label: p.Title, Kind: kind})
	}
	return out
}
