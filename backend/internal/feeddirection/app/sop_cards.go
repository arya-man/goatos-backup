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
	refs := domain.LegacyProofRefs(rules.Stage, legacy, explicit)
	ordered, err := rules.ValidateProofRefs(refs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: %w", ports.ErrSOPProofSlotInvalid, err)
	}
	if err := rules.ValidateAnswers(answers); err != nil {
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
func proofMeta(judged []judgedProof) []ports.ProofMeta {
	out := make([]ports.ProofMeta, 0, len(judged))
	for _, j := range judged {
		out = append(out, ports.ProofMeta{Label: j.Slot.Title, Kind: j.Kind})
	}
	return out
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
// it to be (falling back to the slot's own kind for a ref this request did not judge).
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
		kind := kindOf[ref]
		if kind == "" || kind == authored.KindEither {
			kind = p.Kind
			if kind == authored.KindEither {
				kind = authored.KindVideo
			}
		}
		out = append(out, ports.ProofMeta{Label: p.Title, Kind: kind})
	}
	return out
}
