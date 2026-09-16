package app

import (
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
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
