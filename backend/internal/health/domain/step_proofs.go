package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidStepProof is a malformed step-proof write.
	ErrInvalidStepProof = errors.New("health: that step proof is not valid")
	// ErrStepNotInSession is a step id that does not belong to the session being written to.
	// It is refused rather than ignored: a client that could attach a video to another session's
	// step could put evidence of one animal's treatment onto another animal's card.
	ErrStepNotInSession = errors.New("health: that step does not belong to this session")
	// ErrStepProofsIncomplete is a submit attempted while a step still owes its video.
	ErrStepProofsIncomplete = errors.New("health: every step needs its video before this session can be submitted")
)

// ONE VIDEO PER TREATMENT STEP (maintainer decision 2026-09-23).
//
// A session used to carry one proof for the whole card. The Fever card's day-1 morning holds
// twelve steps and six separate injections, so one clip could not show the work and a verifier
// could not tell from it whether every injection was given.
//
// The REVIEW grain is unchanged -- one item per session, tag and disease, the verifier steps
// through the clips inside it. Only the evidence got finer.

// StepProof is one step's recorded video.
//
// CapturedBy is per step because different people may do different steps of one session, which a
// single proof_ref on the session could never record.
type StepProof struct {
	StepID     string    `json:"step_id"`
	ProofRef   string    `json:"proof_ref"`
	CapturedBy string    `json:"captured_by"`
	CapturedAt time.Time `json:"captured_at"`
}

// RecordStepProofInput attaches ONE video to ONE step.
//
// It is its own write, separate from completing the session, for the reason the proof business-ack
// contract gives: the blob reaching storage is not the business fact. This write is the business
// fact, it retries on its own, and the step does not read as recorded until it lands.
type RecordStepProofInput struct {
	TenantID           string
	ActorID            string
	SessionID          string
	StepID             string
	ProofRef           string
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}

// Validate normalises and checks a step-proof write.
func (in *RecordStepProofInput) Validate() error {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.SessionID = strings.TrimSpace(in.SessionID)
	in.StepID = strings.TrimSpace(in.StepID)
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if in.TenantID == "" || in.ActorID == "" || in.SessionID == "" || in.StepID == "" ||
		in.ProofRef == "" || in.IdempotencyKey == "" {
		return ErrInvalidStepProof
	}
	return nil
}

// MissingStepProofs names the steps of this session that still owe a video, in the order the
// operator works them.
//
// EVERY step owes one (maintainer, 2026-09-23). The check lives here rather than in SQL so the
// phone and the server refuse on the same rule, and so the refusal can NAME what is missing
// instead of reporting a bare count -- an operator told "3 steps missing" has to hunt for them.
func MissingStepProofs(steps []ProtocolStep, proofs []StepProof) []ProtocolStep {
	have := make(map[string]struct{}, len(proofs))
	for _, p := range proofs {
		if strings.TrimSpace(p.ProofRef) != "" {
			have[p.StepID] = struct{}{}
		}
	}
	missing := make([]ProtocolStep, 0, len(steps))
	for _, s := range steps {
		if s.StepID == "" {
			// A step with no id cannot be proved against; it belongs to a pre-step-proof session
			// and is handled by the session's own single proof.
			continue
		}
		if _, ok := have[s.StepID]; !ok {
			missing = append(missing, s)
		}
	}
	return missing
}

// StepLabel is what a step is CALLED, to the operator filming it and to the verifier watching the
// clip back. The verifier sees a list of videos; without this she cannot tell which injection each
// one is.
func StepLabel(s ProtocolStep) string {
	if s.MedicineName != nil && strings.TrimSpace(*s.MedicineName) != "" {
		label := strings.TrimSpace(*s.MedicineName)
		if s.DosageText != nil && strings.TrimSpace(*s.DosageText) != "" {
			label += " " + strings.TrimSpace(*s.DosageText)
			if s.DosageDenominator != nil && strings.TrimSpace(*s.DosageDenominator) != "" {
				label += "/" + strings.TrimSpace(*s.DosageDenominator)
			}
		}
		if s.MedicineRoute != nil && strings.TrimSpace(*s.MedicineRoute) != "" {
			label += " " + strings.TrimSpace(*s.MedicineRoute)
		}
		return label
	}
	if s.Instruction != nil && strings.TrimSpace(*s.Instruction) != "" {
		return strings.TrimSpace(*s.Instruction)
	}
	return strings.TrimSpace(s.RecordType)
}
