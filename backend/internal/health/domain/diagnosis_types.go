package domain

import (
	"encoding/json"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// Diagnosis-run statuses.
//
// A run is PROPOSED until the Health Director confirms it. That gate is what
// keeps the engine advisory: nothing it names becomes a course an operator
// administers until a human agrees. Emergencies and field actions are the
// documented exceptions and do not wait -- they are carried on the proposal and
// acted on immediately.
const (
	DiagnosisStatusProposed   = "proposed"
	DiagnosisStatusConfirmed  = "confirmed"
	DiagnosisStatusSuperseded = "superseded"
)

// SubmitObservationInput is one completed observation form for one animal.
//
// The animal facts are NOT taken from the request: species, sex, status, age
// band and lifecycle are read from GoatOS inside the transaction. A client that
// could assert "this goat is an adult male" could steer the diagnosis by lying
// about the animal, which is a far easier mistake to make than a wrong tick.
type SubmitObservationInput struct {
	TenantID string
	ActorID  string
	GoatID   string

	Findings diagnosis.Findings
	Context  diagnosis.Context

	BusinessDate       time.Time
	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}

// SubmitObservationResult carries the whole proposal back to the manager. The
// emergencies in it are already actionable; the problems are not, until the
// Director confirms.
type SubmitObservationResult struct {
	DiagnosisRunID string               `json:"health_diagnosis_run_id"`
	Status         string               `json:"status"`
	Proposal       diagnosis.Proposal   `json:"proposal"`
	Confirmable    []ConfirmableProblem `json:"confirmable"`

	// MayConfirm is whether the SUBMITTER may also decide. Normally false -- the
	// health manager records, the Director confirms -- but the Director may record
	// an observation themselves, and then the two acts collapse into one visit.
	MayConfirm bool `json:"may_confirm"`

	IdempotentReplay bool `json:"idempotent_replay"`
}

// MarshalJSON emits `confirmable` as a JSON ARRAY, never null -- same contract rule, and
// same defect, as diagnosis.Proposal's own marshaller. A REJECTED run has nothing to
// confirm, so the slice is nil, so Go emitted `null` into a field app-api.yaml declares as
// an array and the client types as a non-optional list. The response then failed to decode
// on the phone, the queued write was retried forever, and the screen sat on "Recorded."
// for a form the engine had already judged. Fixing Proposal alone was not enough: this
// wrapper is a second nil slice one layer up, and it broke exactly the same way.
func (r SubmitObservationResult) MarshalJSON() ([]byte, error) {
	type alias SubmitObservationResult
	a := alias(r)
	if a.Confirmable == nil {
		a.Confirmable = []ConfirmableProblem{}
	}
	return json.Marshal(a)
}

// ConfirmableProblem is one proposed diagnosis together with everything the
// Director needs to decide, and everything that would happen if they confirm.
//
// SOPAvailable is the honest half. Nine of the register's thirty diagnoses point
// at a treatment card nobody has authored yet; confirming one of those cannot
// open a course, and the Director must see that BEFORE deciding rather than
// discovering it as a failure afterwards.
type ConfirmableProblem struct {
	ID       string `json:"id"`
	Tier     string `json:"tier"`
	SOPRef   string `json:"sop_ref"`
	ExitType string `json:"exit_type"`

	DiseaseKey   string `json:"disease_key"`
	SOPAvailable bool   `json:"sop_available"`
	// BlockedReason is operator-facing and names the missing card. Empty when
	// SOPAvailable is true.
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// ConfirmDiagnosisInput is the Director's decision.
//
// ConfirmedProblems is a SUBSET of what the engine proposed -- the Director may
// drop any of them (an override), but may not add one here. Diagnosing off the
// register is a real power the Director holds, and it is deliberately not this
// endpoint: it would let a client open a course for a disease no rule proposed
// and no evidence supports.
type ConfirmDiagnosisInput struct {
	TenantID       string
	ActorID        string
	DiagnosisRunID string

	ConfirmedProblems []string

	IdempotencyKey     string
	RequestFingerprint string
	TraceID            string
}

// ConfirmDiagnosisResult reports what the confirmation actually opened.
type ConfirmDiagnosisResult struct {
	DiagnosisRunID string       `json:"health_diagnosis_run_id"`
	Status         string       `json:"status"`
	OpenedCases    []OpenedCase `json:"opened_cases"`
	// Declined is what the Director chose not to confirm. Kept on the response so
	// the manager sees the override rather than silently losing the proposal.
	Declined []string `json:"declined"`

	IdempotentReplay bool `json:"idempotent_replay"`
}

// ConfirmableFromProposal rebuilds the Director's decision list from a stored
// proposal.
//
// The proposal is the durable record; this list is derived from it every time it
// is needed, so a run read back tomorrow offers exactly the choices it offered
// when it was made. Deriving it in more than one place is how the confirm path
// and the read path drift apart, which is why this lives in the domain rather
// than beside each caller.
//
// SOPAvailable is deliberately NOT set here -- it needs a published-card lookup
// the domain cannot do. Callers pass the result through annotateSOPAvailability.
func ConfirmableFromProposal(proposal diagnosis.Proposal) []ConfirmableProblem {
	out := make([]ConfirmableProblem, 0, len(proposal.Problems))
	for _, id := range proposal.Problems {
		sopRef := proposal.SOP[id]
		out = append(out, ConfirmableProblem{
			ID:         id,
			Tier:       string(proposal.Tiers[id]),
			SOPRef:     sopRef,
			ExitType:   proposal.CourseType[id],
			DiseaseKey: SOPRefToDiseaseKey(sopRef),
		})
	}
	return out
}

// OpenedCase is one course this confirmation started.
type OpenedCase struct {
	CaseID       string `json:"case_id"`
	DiseaseKey   string `json:"disease_key"`
	ExitType     string `json:"exit_type"`
	DurationDays *int   `json:"duration_days"`
	SessionCount int    `json:"session_count"`
}

// DiagnosisRun is the durable record of one observation and what the engine made
// of it.
type DiagnosisRun struct {
	DiagnosisRunID string `json:"health_diagnosis_run_id"`
	GoatID         string `json:"goat_id"`

	// GoatDisplayID is the animal as a PERSON recognises it, and it is carried on
	// the read for the same reason the queue row carries it: the screen must never
	// have to name the animal from a uuid, and it cannot compose the name itself.
	//
	// A device opening an assessment has often never seen the submit response --
	// the manager submitted from their phone, the Director opens it on theirs --
	// so a client-side cache is not a source for this. Without it the assessment
	// header is simply blank, which is the defect this field exists to close.
	GoatDisplayID string `json:"goat_display_id"`

	// RegisterVersion is the DIAGNOSIS pin. A case separately pins its treatment
	// protocol version; the two move independently.
	RegisterVersion string `json:"register_version"`

	ObservedBy   string    `json:"observed_by"`
	ObservedAt   time.Time `json:"observed_at"`
	BusinessDate string    `json:"business_date"`

	Proposal diagnosis.Proposal `json:"proposal"`
	Status   string             `json:"status"`

	// Confirmable is what the Director may still act on, carried on the READ so
	// the queue is self-sufficient. Without it a phone that has never seen the
	// submit response -- a fresh install, a second device, the Director's own --
	// could render the proposal but offer no decision.
	//
	// Empty once the run is decided: a confirmed or declined run has nothing left
	// to choose.
	Confirmable []ConfirmableProblem `json:"confirmable"`

	// MayConfirm is whether THIS CALLER may cast the decision, resolved from their
	// own grants.
	//
	// It is a separate fact from Confirmable, and conflating the two is the trap:
	// the manager who submitted the observation receives the confirmable list on
	// their own submit response, and they are precisely the person who must NOT be
	// able to confirm it. A list of choices is not authority over them.
	//
	// The write is gated independently at the route. This exists so the app does
	// not offer a button that is guaranteed to fail -- never as the gate itself.
	MayConfirm bool `json:"may_confirm"`

	ConfirmedBy *string    `json:"confirmed_by"`
	ConfirmedAt *time.Time `json:"confirmed_at"`
}

// MarshalJSON emits `confirmable` as a JSON ARRAY, never null -- same contract rule, and
// same defect, as diagnosis.Proposal's own marshaller. A REJECTED run has nothing to
// confirm, so the slice is nil, so Go emitted `null` into a field app-api.yaml declares as
// an array and the client types as a non-optional list. The response then failed to decode
// on the phone, the queued write was retried forever, and the screen sat on "Recorded."
// for a form the engine had already judged. Fixing Proposal alone was not enough: this
// wrapper is a second nil slice one layer up, and it broke exactly the same way.
func (r DiagnosisRun) MarshalJSON() ([]byte, error) {
	type alias DiagnosisRun
	a := alias(r)
	if a.Confirmable == nil {
		a.Confirmable = []ConfirmableProblem{}
	}
	return json.Marshal(a)
}

// DiagnosisQueueFilter selects one page of the Director's queue.
//
// Status defaults to `proposed` at the service, because the queue's whole purpose
// is work still awaiting a decision. It is a filter rather than a hardcode so the
// same endpoint can show what was already decided without a second read model.
type DiagnosisQueueFilter struct {
	TenantID string
	Status   string
	// GoatID narrows to one animal's diagnosis history. Blank means every animal.
	GoatID string
	Cursor string
	Limit  int
}

// DiagnosisQueueItem is one row of the Director's queue.
//
// It carries enough to DECIDE WHAT TO OPEN FIRST and nothing more: who the animal
// is, where it is, when it was seen, and how bad the assessment looks. The full
// proposal is a separate read, because a queue that ships every proposal in full
// is a page fetch that grows with the register.
type DiagnosisQueueItem struct {
	DiagnosisRunID string `json:"health_diagnosis_run_id"`
	GoatID         string `json:"goat_id"`
	GoatDisplayID  string `json:"goat_display_id"`

	// Where the animal is, composed by the backend. Clients render it verbatim.
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`

	ObservedAt   time.Time `json:"observed_at"`
	BusinessDate string    `json:"business_date"`
	Status       string    `json:"status"`

	// Problems is the ranked diagnosis list, severity first. Rendered as the row's
	// headline, so its order is the backend's and must not be re-sorted by a client.
	Problems []string `json:"problems"`
	// EmergencyCount is how many things need doing NOW. It does not wait for the
	// Director, so it is on the ROW rather than only inside the proposal: a queue
	// that hides an emergency behind a tap is worse than no queue.
	EmergencyCount int `json:"emergency_count"`
	// UnexplainedCount is how many abnormal findings no diagnosis accounts for.
	UnexplainedCount int `json:"unexplained_count"`
}

// DiagnosisQueuePage is one keyset page of the queue.
type DiagnosisQueuePage struct {
	Items []DiagnosisQueueItem `json:"items"`
	// NextCursor is absent on the last page. Never an offset: the queue is ordered
	// newest-first and rows are inserted at the head, so an offset would re-show or
	// skip rows as new observations land mid-scroll.
	NextCursor *string `json:"next_cursor"`
	// MayConfirm is whether THIS caller may decide any of it, resolved from their
	// own grants. A manager may read the queue -- seeing what they sent is still
	// waiting is useful -- without being offered the decision on any of it.
	MayConfirm bool `json:"may_confirm"`
}
