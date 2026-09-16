package domain

import (
	"sort"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// EVIDENCE COMPOSITION (SOP → verifier parity, maintainer decisions 2026-09-16).
//
// The verifier sees EVERYTHING the operator captured: every proof under its SOP step title with
// the kind the register resolved it to, and every answer in farm words grouped by the step that
// asked it. These are the ONE set of pure helpers every producer of a death / birth / reconcile
// item composes from, so the admin-web drawer and the phone's verify detail cannot disagree
// about what a clip is called. Titles are kept raw; WS-A's verificationdomain.ComposeMediaLabels
// numbers titles repeated ACROSS an item at read time, so only a step that itself holds several
// proofs of ONE kind numbers them here ("<title> · photo 1 of 2").

// MediaMetaItem names one proof for the verifier: the step title and the proof's kind.
type MediaMetaItem struct {
	Label string
	Kind  string
}

// EvidenceRow is one answer in farm words, grouped under the step that asked it.
type EvidenceRow struct {
	Label string
	Value string
	Group string
}

// DeathEvidence is the whole bundle one death item carries: refs, positional meta and rows.
type DeathEvidence struct {
	Refs []string
	Meta []MediaMetaItem
	Rows []EvidenceRow
}

// captureGroup is the group every capture-form row and media label rides under on a verifier
// item, so a reviewer can tell "at report" facts from the steps that followed.
const captureGroup = "At report"

// StepMediaMeta names the step's proofs, POSITIONAL against AllProofRefs (videos first, then
// photos). The kind comes from the proof item the register resolved, never from the legacy
// requires_video column; a bare legacy proof_ref is a video.
func StepMediaMeta(a WorkflowAction) []MediaMetaItem {
	title := strings.TrimSpace(a.Title)
	if len(a.ProofRefs) == 0 {
		if a.ProofRef != nil && strings.TrimSpace(*a.ProofRef) != "" {
			return []MediaMetaItem{{Label: title, Kind: ProofKindVideo}}
		}
		return nil
	}
	videos, photos := 0, 0
	for _, p := range a.ProofRefs {
		if p.Kind == ProofKindPhoto {
			photos++
		} else {
			videos++
		}
	}
	out := make([]MediaMetaItem, 0, len(a.ProofRefs))
	nv, np := 0, 0
	// Videos first, then photos: the order AllProofRefs hands the refs to the verifier.
	for pass := 0; pass < 2; pass++ {
		for _, p := range a.ProofRefs {
			isPhoto := p.Kind == ProofKindPhoto
			if (pass == 0) == isPhoto {
				continue
			}
			label := title
			switch {
			case isPhoto && photos > 1:
				np++
				label = title + " · photo " + strconv.Itoa(np) + " of " + strconv.Itoa(photos)
			case !isPhoto && videos > 1:
				nv++
				label = title + " · video " + strconv.Itoa(nv) + " of " + strconv.Itoa(videos)
			}
			kind := ProofKindVideo
			if isPhoto {
				kind = ProofKindPhoto
			}
			out = append(out, MediaMetaItem{Label: label, Kind: kind})
		}
	}
	return out
}

// StepAnswerRow renders the step's answer in farm words. penDisplay is the resolved
// operational location for a Record pen step (the raw answer is "<shed_id>|<label>", which is
// never shown); blank falls back to the raw value rather than dropping the fact. A blank answer
// is no row.
func StepAnswerRow(a WorkflowAction, penDisplay string) (EvidenceRow, bool) {
	if a.AnswerValue == nil {
		return EvidenceRow{}, false
	}
	value := strings.TrimSpace(*a.AnswerValue)
	if value == "" {
		return EvidenceRow{}, false
	}
	title := strings.TrimSpace(a.Title)
	switch {
	case a.HasHook(EngineHookRecordPen):
		if strings.TrimSpace(penDisplay) != "" {
			value = strings.TrimSpace(penDisplay)
		}
	case a.HasHook(EngineHookWeighKg):
		value += " kg"
	case a.AnswerType == AnswerKindMultiSelect:
		parts := strings.Split(value, "|")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		value = strings.Join(parts, ", ")
	case a.AnswerType == AnswerKindYesNo || a.AnswerType == "":
		switch strings.ToLower(value) {
		case "yes":
			value = "Yes"
		case "no":
			value = "No"
		}
	}
	return EvidenceRow{Label: title, Value: value, Group: title}, true
}

// DeathStepsComplete reports whether EVERY operator step of the death workflow is completed with
// its proof satisfied. It replaces DeathVideosComplete as the approval gate (bug 1, 2026-09-16):
// the old gate counted only death_evidence-hooked steps, so an authored question or photo step
// was never required and its answer never reached the verifier.
func DeathStepsComplete(actions []WorkflowAction) bool {
	found := false
	for _, a := range actions {
		if a.ActionType == ActionTypeApproval || a.Status == ActionStatusCanceled {
			continue
		}
		found = true
		if a.Status != ActionStatusCompleted || !a.ProofSatisfied() {
			return false
		}
	}
	return found
}

// DeathEvidenceBundle composes ONE death item's evidence: the capture form's proofs and answers
// first (labelled under "At report"), then every operator step in seq order. Meta is positional
// against Refs. For the seeded two-video document with no capture form this is byte-for-byte the
// legacy DeathProofRefs order, so the idempotency key of an in-flight item does not move.
func DeathEvidenceBundle(capture authored.Evidence, actions []WorkflowAction) DeathEvidence {
	var out DeathEvidence
	for _, m := range capture.Media {
		ref := strings.TrimSpace(m.Ref)
		if ref == "" {
			continue
		}
		label := strings.TrimSpace(m.Label)
		if label != "" {
			label = captureGroup + " · " + label
		}
		out.Refs = append(out.Refs, ref)
		out.Meta = append(out.Meta, MediaMetaItem{Label: label, Kind: strings.TrimSpace(m.Kind)})
	}
	for _, r := range capture.Rows {
		if strings.TrimSpace(r.Value) == "" {
			continue
		}
		group := strings.TrimSpace(r.Group)
		if group == "" {
			group = captureGroup
		}
		out.Rows = append(out.Rows, EvidenceRow{Label: strings.TrimSpace(r.Label), Value: strings.TrimSpace(r.Value), Group: group})
	}
	if note := strings.TrimSpace(capture.MissingNote); note != "" {
		out.Rows = append(out.Rows, EvidenceRow{Label: authored.MissingNoteOlderApp, Value: note, Group: captureGroup})
	}
	sorted := append([]WorkflowAction(nil), actions...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	for _, a := range sorted {
		if a.ActionType == ActionTypeApproval || a.Status == ActionStatusCanceled {
			continue
		}
		out.Refs = append(out.Refs, a.AllProofRefs()...)
		out.Meta = append(out.Meta, StepMediaMeta(a)...)
		if row, ok := StepAnswerRow(a, ""); ok {
			out.Rows = append(out.Rows, row)
		}
	}
	return out
}

// DeathEvidenceKey is the verification idempotency key for one death review round:
// "counts-death-evidence:<workflow_id>:r<round>:<refs…>". Unchanged from the pre-SOP key so an
// item already in the queue is never relabelled or duplicated on redelivery.
func DeathEvidenceKey(workflowID string, round int, refs []string) string {
	key := "counts-death-evidence:" + workflowID + ":r" + strconv.Itoa(round)
	for _, ref := range refs {
		key += ":" + strings.TrimSpace(ref)
	}
	return key
}

// ReopenDeathProofSteps sends every proof-bearing operator step back for a re-shoot after a
// verifier rejection: status rework, proofs and completion cleared, the verifier's words kept
// for the operator. Answer-only steps keep their answers (the operator re-shoots, not re-answers).
// Mutates actions in place and returns the changed rows; a no-op returns none.
func ReopenDeathProofSteps(actions []WorkflowAction, reason string) []WorkflowAction {
	reason = strings.TrimSpace(reason)
	var changed []WorkflowAction
	for i := range actions {
		a := actions[i]
		if a.ActionType == ActionTypeApproval || a.Status == ActionStatusCanceled {
			continue
		}
		if a.ProofMinVideos == 0 && a.ProofMinPhotos == 0 && !a.RequiresVideo {
			continue
		}
		if a.Status != ActionStatusCompleted && a.Status != ActionStatusInReview {
			continue
		}
		actions[i].Status = ActionStatusRework
		actions[i].ProofRef = nil
		actions[i].ProofRefs = nil
		actions[i].CompletedAt = nil
		actions[i].CompletedBy = nil
		actions[i].ReworkReason = optionalPtr(reason)
		actions[i].RowVersion++
		changed = append(changed, actions[i])
	}
	return changed
}
