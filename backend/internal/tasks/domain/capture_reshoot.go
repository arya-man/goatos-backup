package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// CAPTURE RE-SHOOT (maintainer decision 5, 2026-09-16). A verifier REJECT of a capture-form proof
// (the Add birth / Add death form's own photos and clips) sends a "Re-shoot report proof" step
// back to the operator with the verifier's words, one step per capture proof, appended to the
// event's workflow. Recording it replaces exactly that proof in the capture snapshot and the
// report goes back to Verify -- the new proof is what the approver and the verifier then see.

// EngineHookReshootReport marks an appended capture re-shoot step.
const EngineHookReshootReport = "reshoot_report"

const reshootKeyPrefix = "reshoot_report_"

// CaptureReshootSteps builds the appended steps for one rejection: one per capture proof named in
// indexes (none named = every proof: death's bundle), in capture order, after every existing
// step, status rework carrying the reason. Birth is verified per slot, so a birth rejection names
// exactly one index. Keys are deterministic per (verdict recording key, proof index), so a
// redelivered verdict lands on the workflow_actions natural key and inserts nothing, while a later
// rejection mints fresh steps.
func CaptureReshootSteps(capture authored.Evidence, existing []WorkflowAction, recordingKey, reason string, indexes ...int) []WorkflowAction {
	if len(capture.Media) == 0 {
		return nil
	}
	sum := sha256.Sum256([]byte(recordingKey))
	round := hex.EncodeToString(sum[:])[:8]
	seq := 0
	for _, a := range existing {
		if a.Seq > seq {
			seq = a.Seq
		}
	}
	out := make([]WorkflowAction, 0, len(capture.Media))
	wanted := map[int]bool{}
	for _, i := range indexes {
		wanted[i] = true
	}
	for i, m := range capture.Media {
		if len(wanted) > 0 && !wanted[i] {
			continue
		}
		seq++
		a := WorkflowAction{
			ActionKey:    reshootKeyPrefix + strconv.Itoa(i) + "_" + round,
			Seq:          seq,
			Section:      SectionMain,
			ActionType:   ActionTypeAction,
			Title:        "Re-shoot report proof · " + strings.TrimSpace(m.Label),
			Detail:       "The verifier sent this back. Record it again with the in-app camera.",
			Status:       ActionStatusRework,
			TaskType:     EngineHookReshootReport,
			AnswerType:   AnswerKindNone,
			EngineHook:   EngineHookReshootReport,
			ReworkReason: optionalPtr(strings.TrimSpace(reason)),
			RowVersion:   1,
		}
		switch strings.ToLower(strings.TrimSpace(m.Kind)) {
		case ProofKindVideo:
			a.ProofMinVideos, a.RequiresVideo = 1, true
		case ProofKindPhoto:
			a.ProofMinPhotos = 1
		}
		out = append(out, a)
	}
	return out
}

// ReshootMediaIndex reads which capture proof a re-shoot step replaces.
func ReshootMediaIndex(a WorkflowAction) (int, bool) {
	if !strings.HasPrefix(a.ActionKey, reshootKeyPrefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(a.ActionKey, reshootKeyPrefix)
	idx, _, found := strings.Cut(rest, "_")
	if !found {
		return 0, false
	}
	n, err := strconv.Atoi(idx)
	return n, err == nil && n >= 0
}

// ReplaceCaptureMedia returns a copy of the snapshot with proof i replaced by the re-shot one
// (its label kept; the kind taken from the new proof when it carries one).
func ReplaceCaptureMedia(capture authored.Evidence, i int, proof ProofItem) authored.Evidence {
	if i < 0 || i >= len(capture.Media) || strings.TrimSpace(proof.Ref) == "" {
		return capture
	}
	out := capture
	out.Media = append([]authored.EvidenceMedia(nil), capture.Media...)
	out.Media[i].Ref = strings.TrimSpace(proof.Ref)
	if k := strings.TrimSpace(proof.Kind); k != "" {
		out.Media[i].Kind = k
	}
	return out
}
