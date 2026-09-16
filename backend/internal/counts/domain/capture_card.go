package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// CAPTURE CARD (maintainer decision 4, 2026-09-16, docs/decisions/sop-driven-herd-operations.md
// → "Phase 2: capture forms"). The Add birth / Add death forms take SOP-authored EXTRAS beside
// their fixed fields: proof slots (photo / video / either, compulsory or not) and questions,
// authored on /counts/sops as the `capture_card` section of the counts.birth / counts.death
// version. ABSENT = the empty card = today's form. The card is pinned at capture; the snapshot
// (authored.Evidence) reaches the approver's row and rides the verifier item.
//
// Built on backend/internal/sop/authored so a slot or a question means the same thing on every
// card the farm authors; this file adds only the section's own shape, its bounds and the
// OLDER-APP rule (decision 7): a submission that does not send the new fields is accepted, maps
// nothing, and notes every compulsory item it did not carry; a submission that DOES send them is
// judged strictly.

// CaptureSchemaVersion is the schema tag every capture_card section must carry.
const CaptureSchemaVersion = "goatos.sop-capture.v1"

// CaptureEvidenceGroup is the group every capture row rides under on a reviewer's screen.
const CaptureEvidenceGroup = "At report"

// CaptureCard is the typed form of `form_dsl.capture_card`.
type CaptureCard struct {
	SchemaVersion string               `json:"schema_version"`
	Instruction   string               `json:"instruction,omitempty"`
	Proofs        []authored.ProofSlot `json:"proofs,omitempty"`
	Questions     []authored.Question  `json:"questions,omitempty"`
}

// IsEmpty reports a card that asks nothing (the day-one form).
func (c CaptureCard) IsEmpty() bool { return len(c.Proofs) == 0 && len(c.Questions) == 0 }

// ErrCaptureCardInvalid wraps every parse problem.
var ErrCaptureCardInvalid = errors.New("counts: capture_card is invalid")

// ParseCaptureCard extracts the section. An ABSENT section is the empty card, never an error:
// every version published before the section existed keeps today's form.
func ParseCaptureCard(formDSL map[string]any) (CaptureCard, error) {
	raw, ok := formDSL["capture_card"]
	if !ok || raw == nil {
		return CaptureCard{SchemaVersion: CaptureSchemaVersion}, nil
	}
	if _, isObject := raw.(map[string]any); !isObject {
		return CaptureCard{}, fmt.Errorf("%w: capture_card must be an object", ErrCaptureCardInvalid)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return CaptureCard{}, fmt.Errorf("%w: %v", ErrCaptureCardInvalid, err)
	}
	var out CaptureCard
	if err := json.Unmarshal(encoded, &out); err != nil {
		return CaptureCard{}, fmt.Errorf("%w: %v", ErrCaptureCardInvalid, err)
	}
	if out.SchemaVersion != CaptureSchemaVersion {
		return CaptureCard{}, fmt.Errorf("%w: schema_version %q", ErrCaptureCardInvalid, out.SchemaVersion)
	}
	return out, nil
}

// ValidateCaptureCard names every problem by path (capture_card.proofs.N.key, ...). A card with
// only questions, or nothing at all, is valid: a form MAY ask questions and no media
// (maintainer decision 6) and the empty card is today's form.
func ValidateCaptureCard(card CaptureCard) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if len(card.Instruction) > authored.MaxTextLength {
		add("capture_card.instruction: too long")
	}
	authored.ValidateProofSlots("capture_card.proofs", card.Proofs, false, add)
	authored.ValidateQuestions("capture_card.questions", card.Questions, add)
	return problems
}

// CaptureSubmission is what a NEW app sends beside the form's fixed fields (`sop_capture`).
type CaptureSubmission struct {
	SOPVersionID string
	Proofs       authored.ProofRefs
	Answers      authored.Answers
}

// CaptureJudgement is the accepted capture: the slot map and answers to store, and the titles
// of every compulsory item an older app did not send (empty for a new app, which is judged
// strictly instead).
type CaptureJudgement struct {
	Proofs  authored.ProofRefs
	Answers authored.Answers
	Ordered []authored.OrderedProof
	Missing []string
}

// JudgeCapture applies the older-app rule. sub == nil is a submission from an app older than
// the card: accepted, nothing mapped, every compulsory slot and (unconditional) compulsory
// question noted as missing. A present submission is judged strictly through the shared
// authored rules: a missing compulsory slot is a *authored.ProofError naming it, a bad answer a
// *authored.AnswerError naming the question; blank slots and inapplicable answers are dropped.
func JudgeCapture(card CaptureCard, sub *CaptureSubmission) (CaptureJudgement, error) {
	if sub == nil {
		var missing []string
		for _, p := range card.Proofs {
			if p.Required {
				missing = append(missing, strings.TrimSpace(p.Title))
			}
		}
		for _, q := range card.Questions {
			if q.Required && q.OnlyIf == nil {
				missing = append(missing, strings.TrimSpace(q.Title))
			}
		}
		return CaptureJudgement{Proofs: authored.ProofRefs{}, Answers: authored.Answers{}, Missing: missing}, nil
	}
	proofs := authored.NormalizeProofRefs(sub.Proofs)
	ordered, err := authored.ValidateProofRefs(card.Proofs, proofs)
	if err != nil {
		return CaptureJudgement{}, err
	}
	answers := sub.Answers
	if answers == nil {
		answers = authored.Answers{}
	}
	if err := authored.ValidateAnswers(card.Questions, answers); err != nil {
		return CaptureJudgement{}, err
	}
	return CaptureJudgement{Proofs: proofs, Answers: authored.NormalizeAnswers(card.Questions, answers), Ordered: ordered}, nil
}

// ComposeCaptureEvidence takes the snapshot: proofs in SLOT order under their titles with the
// register's kind (a typed slot falls back to its own kind; an `either` slot the register could
// not judge stays blank -- never a guessed video), answers in farm words, and the missing note.
func ComposeCaptureEvidence(versionLabel string, card CaptureCard, proofs authored.ProofRefs, kinds map[string]string, answers authored.Answers, missing []string) authored.Evidence {
	out := authored.Evidence{VersionLabel: strings.TrimSpace(versionLabel)}
	for _, slot := range card.Proofs {
		ref := strings.TrimSpace(proofs[slot.Key])
		if ref == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(kinds[ref]))
		switch kind {
		case "image":
			kind = authored.KindPhoto
		case authored.KindVideo, authored.KindPhoto:
		default:
			kind = ""
		}
		if kind == "" && slot.Kind != authored.KindEither {
			kind = slot.Kind
		}
		out.Media = append(out.Media, authored.EvidenceMedia{Ref: ref, Kind: kind, Label: strings.TrimSpace(slot.Title)})
	}
	for _, row := range authored.AnswerRows(card.Questions, answers) {
		out.Rows = append(out.Rows, authored.EvidenceRow{Label: row.Title, Value: row.Value, Group: CaptureEvidenceGroup})
	}
	var notes []string
	for _, m := range missing {
		if m = strings.TrimSpace(m); m != "" {
			notes = append(notes, m)
		}
	}
	out.MissingNote = strings.Join(notes, "; ")
	return out
}
