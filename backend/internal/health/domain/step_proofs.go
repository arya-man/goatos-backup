package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	// ErrStepProofClosed is a step-proof write attempted after the session is no longer editable.
	ErrStepProofClosed = errors.New("health: this treatment session has already been submitted")
	// ErrSessionNotDue is a visit closed before the hour it is meant to happen.
	ErrSessionNotDue = errors.New("health: this visit has not started yet")
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

// DoseLabel is the dose as a person must read it, and it is the BACKEND's words because a dose
// is not presentation -- misreading one is a medicine given wrong.
//
// Three ways the phone got this wrong by joining the raw columns with a separator:
//
//	dosage 5,     denominator NULL   ->  "5 · Oral"        five WHAT
//	dosage 3,     denominator "none" ->  "3 · none · Oral" the word none printed as a unit
//	dosage 0.033, denominator "kg"   ->  "0.033 · kg"      reads as a weight of medicine,
//	                                                       when it means 0.033 PER kg
//
// A denominator is a PER-unit, so it is rendered as one. "none" and a blank both mean the dose
// has no per-unit, and neither is a word an operator should ever see.
func DoseLabel(dosage, denominator, route *string) string {
	amount := ptrTrim(dosage)
	per := ptrTrim(denominator)
	if strings.EqualFold(per, "none") {
		per = ""
	}
	parts := make([]string, 0, 2)
	switch {
	case amount != "" && per != "":
		parts = append(parts, amount+" per "+per)
	case amount != "":
		parts = append(parts, amount)
	}
	if r := ptrTrim(route); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, " · ")
}

func ptrTrim(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

// StepLabel is what a step is CALLED, to the operator filming it and to the verifier watching the
// clip back. The verifier sees a list of videos; without this she cannot tell which injection each
// one is.
func StepLabel(s ProtocolStep) string {
	if name := ptrTrim(s.MedicineName); name != "" {
		// The SAME words the operator read on the card, so the verifier is judging the clip
		// against the dose the operator was told to give.
		if dose := DoseLabel(s.DosageText, s.DosageDenominator, s.MedicineRoute); dose != "" {
			return name + " · " + dose
		}
		return name
	}
	if s.Instruction != nil && strings.TrimSpace(*s.Instruction) != "" {
		return strings.TrimSpace(*s.Instruction)
	}
	return strings.TrimSpace(s.RecordType)
}

// StepMedia is one clip paired with the NAME of the step it proves.
//
// The verifier opens ONE item holding the whole session's set and steps through it. Without a
// name per clip she cannot tell which of six injections each video is, which is the only thing
// that makes a twelve-clip item reviewable rather than a wall of footage.
type StepMedia struct {
	StepID   string `json:"step_id"`
	Label    string `json:"label"`
	ProofRef string `json:"proof_ref"`
}

// StepProofsMissingError refuses a submit and NAMES the steps that still owe a video.
//
// It carries the steps rather than a count because an operator told "3 steps missing" on a
// twelve-step card has to hunt for them; the phone renders these titles straight back onto the
// rows that are still empty.
type StepProofsMissingError struct {
	Missing []ProtocolStep
}

func (e StepProofsMissingError) Error() string {
	names := make([]string, 0, len(e.Missing))
	for _, s := range e.Missing {
		names = append(names, StepLabel(s))
	}
	return ErrStepProofsIncomplete.Error() + ": " + strings.Join(names, ", ")
}

// Is lets callers match the sentinel while still reading the named steps off the concrete type.
func (e StepProofsMissingError) Is(target error) bool { return target == ErrStepProofsIncomplete }

// StepLabels names the missing steps, for a client that renders the refusal.
func (e StepProofsMissingError) StepLabels() []string {
	out := make([]string, 0, len(e.Missing))
	for _, s := range e.Missing {
		out = append(out, StepLabel(s))
	}
	return out
}

// SessionNotDueError refuses a visit closed before its time, and says WHEN it opens.
//
// A card now earns a morning, an afternoon and an evening visit, and all three are visible from
// first light. Nothing stopped an operator closing the evening one at 07:00 -- a dose recorded as
// given hours before anyone gives it, with a video proving only that the animal was filmed in the
// morning.
type SessionNotDueError struct {
	Session string
	DueAt   time.Time
}

func (e SessionNotDueError) Error() string {
	return ErrSessionNotDue.Error() + ": " + e.SessionLabel() + " work opens at " + e.DueLabel()
}

// Is lets callers match the sentinel while still reading the hour off the concrete type.
func (e SessionNotDueError) Is(target error) bool { return target == ErrSessionNotDue }

// SessionLabel is the visit in farm words.
func (e SessionNotDueError) SessionLabel() string {
	switch strings.ToLower(strings.TrimSpace(e.Session)) {
	case SessionAfternoon:
		return "Afternoon"
	case SessionEvening:
		return "Evening"
	case SessionMorning:
		return "Morning"
	default:
		return "This"
	}
}

// DueLabel is the hour the work opens, on the FARM's clock. A UTC time here would tell an
// operator in Coimbatore to come back five and a half hours early.
func (e SessionNotDueError) DueLabel() string {
	return e.DueAt.In(biztime.DefaultLocation()).Format("15:04")
}
