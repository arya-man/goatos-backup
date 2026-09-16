package app

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): what the VERIFIER reads on
// a weigh item. Mirrors fasting_service.go's mediaMetaFor / answerRowsFor for the removal card.
//
// Two capture kinds, told apart on the item itself (maintainer clarification the same day):
// a leading context row "Weighed as: Per animal" / "Whole pen" in group "Weighing", and the
// operator's answers grouped under "Per-animal answers" / "Whole-pen answers". The subject
// label already differs per kind (domain.CorrectedSubjectLabel), and the source ref_type is
// the queue category. Nothing here merges the two sections.

const (
	captureGroupWeighing         = "Weighing"
	captureKindLabel             = "Weighed as"
	captureKindPerAnimal         = "Per animal"
	captureKindWholePen          = "Whole pen"
	captureGroupPerAnimalAnswers = "Per-animal answers"
	captureGroupWholePenAnswers  = "Whole-pen answers"
	captureGroupNotCaptured      = "Not captured"
)

// captureMetaIndividual names each ordered ref by its slot title; the kind is what the proof
// REGISTER judged the capture to be (an `either` slot's answer), else the slot's own kind.
func captureMetaIndividual(rules domain.Rules, refs domain.IndividualProofRefs, ordered []string, registerKinds map[string]string) []VerificationMediaMeta {
	slotByRef := map[string]domain.RemovalProofSlot{}
	for _, slot := range rules.IndividualProofs() {
		if ref := refs[slot.Key]; ref != "" {
			slotByRef[ref] = slot
		}
	}
	out := make([]VerificationMediaMeta, len(ordered))
	for i, ref := range ordered {
		slot, ok := slotByRef[ref]
		if !ok {
			continue
		}
		kind := registerKinds[ref]
		if kind == "" && slot.Kind != domain.RemovalProofKindEither {
			kind = slot.Kind
		}
		out[i] = VerificationMediaMeta{Label: slot.Title, Kind: kind}
	}
	return out
}

// captureMetaLumpSum names each whole-pen capture "Title k of N" within its slot.
func captureMetaLumpSum(rules domain.Rules, ordered []domain.OrderedCapture, registerKinds map[string]string) []VerificationMediaMeta {
	titles := map[string]domain.CountedProofSlot{}
	for _, slot := range rules.LumpSumProofs() {
		titles[slot.Key] = slot
	}
	out := make([]VerificationMediaMeta, len(ordered))
	for i, c := range ordered {
		slot, ok := titles[c.SlotKey]
		if !ok {
			continue
		}
		kind := registerKinds[c.Ref]
		if kind == "" && slot.Kind != domain.RemovalProofKindEither {
			kind = slot.Kind
		}
		out[i] = VerificationMediaMeta{Label: domain.CaptureMediaLabel(slot.Title, c.Index, c.Count), Kind: kind}
	}
	return out
}

// captureContextRows builds the verifier's rows for ONE weigh: the capture-kind row first, then
// the answers under the kind's own group, then anything an older app could not have sent.
func captureContextRows(kind string, answers []domain.AnswerRow, notCaptured []domain.AnswerRow) []VerificationContextRow {
	group := captureGroupPerAnimalAnswers
	if kind == captureKindWholePen {
		group = captureGroupWholePenAnswers
	}
	out := []VerificationContextRow{{Label: captureKindLabel, Value: kind, Group: captureGroupWeighing}}
	for _, r := range answers {
		out = append(out, VerificationContextRow{Label: r.Label, Value: r.Value, Group: group})
	}
	for _, r := range notCaptured {
		out = append(out, VerificationContextRow{Label: r.Label, Value: r.Value, Group: captureGroupNotCaptured})
	}
	return out
}

// --- Leadership evidence: the pinned rules label what the director sees -----------------------

// decorateLeadershipEvidence resolves each bucket's PINNED rules once per distinct version
// (bounded by the page size) and stamps: MaxShedVideos = the pinned whole-pen Σmax (the "N of
// M" denominator, no longer the fixed proof-policy 5), and each capture's SOP title + kind on
// the Media list, in slot order, so leadership reads "Scale display photo" over an image
// rather than a numbered video. A bucket pinned to a version the farm never published renders
// under the seeded copy, as the removal card does.
func (s *Service) decorateLeadershipEvidence(ctx context.Context, tenantID string, items []*domain.LeadershipShedVideos) error {
	byVersion := map[int]domain.Rules{}
	for _, item := range items {
		if item == nil {
			continue
		}
		rules, ok := byVersion[item.SOPVersion]
		if !ok {
			resolved, err := s.rulesForVersion(ctx, tenantID, item.SOPVersion)
			if errors.Is(err, ports.ErrSOPVersionUnknown) {
				resolved, err = domain.SeededRules(), nil
			}
			if err != nil {
				return err
			}
			rules = resolved
			byVersion[item.SOPVersion] = rules
		}
		item.MaxShedVideos = rules.LumpSumProofsTotalMax()
		for i := range item.Individual {
			item.Individual[i].Media = individualMedia(rules, item.Individual[i])
		}
		if item.LumpSum != nil {
			item.LumpSum.Media = lumpSumMedia(rules, *item.LumpSum)
		}
	}
	return nil
}

func mimeForKind(kind string) string {
	switch kind {
	case domain.RemovalProofKindVideo:
		return "video/mp4"
	case domain.RemovalProofKindPhoto:
		return "image/jpeg"
	}
	return ""
}

// individualMedia lists a per-animal row's captures primary-first with their slot titles. A row
// written before slots existed (no slot map) is the seeded slot's single video.
func individualMedia(rules domain.Rules, obs domain.Observation) []domain.ProofMedia {
	slots := rules.IndividualProofs()
	titleByRef := map[string]string{}
	for _, slot := range slots {
		if ref := obs.Proofs[slot.Key]; ref != "" {
			titleByRef[ref] = slot.Title
		}
	}
	if len(obs.Proofs) == 0 && obs.ProofArtifactID != "" && len(slots) > 0 {
		titleByRef[obs.ProofArtifactID] = slots[0].Title
	}
	ids := obs.ProofArtifactIDs
	if len(ids) == 0 && obs.ProofArtifactID != "" {
		ids = []string{obs.ProofArtifactID}
	}
	out := make([]domain.ProofMedia, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out = append(out, domain.ProofMedia{ProofID: id, Label: titleByRef[id], MimeType: mimeForKind(obs.ProofKinds[id])})
	}
	return out
}

// lumpSumMedia lists a whole-pen row's captures in stored order, titled "Title k of N" within
// each slot. A row written before slots existed is the seeded pen_video slot, numbered.
func lumpSumMedia(rules domain.Rules, obs domain.Observation) []domain.ProofMedia {
	slots := rules.LumpSumProofs()
	titles := map[string]string{}
	for _, slot := range slots {
		titles[slot.Key] = slot.Title
	}
	ids := obs.ProofArtifactIDs
	if len(ids) == 0 && obs.ProofArtifactID != "" {
		ids = []string{obs.ProofArtifactID}
	}
	proofSlots := obs.ProofSlots
	if len(proofSlots) == 0 && len(slots) > 0 {
		proofSlots = domain.LumpSumProofRefs{slots[0].Key: ids}
	}
	labelByRef := map[string]string{}
	for key, refs := range proofSlots {
		title := titles[key]
		if title == "" {
			title = key
		}
		for i, ref := range refs {
			labelByRef[ref] = domain.CaptureMediaLabel(title, i+1, len(refs))
		}
	}
	out := make([]domain.ProofMedia, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out = append(out, domain.ProofMedia{ProofID: id, Label: labelByRef[id], MimeType: mimeForKind(obs.ProofKinds[id])})
	}
	return out
}
