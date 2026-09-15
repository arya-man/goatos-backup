package domain

import (
	"fmt"
	"strings"
	"time"

	fwrdomain "github.com/vgoats/goatos/backend/internal/feedwaterremoval/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// WEIGHING FASTING PRECONDITION (maintainer decision 2026-09-03).
//
// Animals must have feed and water removed the evening BEFORE they are weighed.
// Every campaign created from this decision on carries exactly ONE fasting
// task: a second operator (same park, assigned at create) removes feed and
// water the night before the weigh date and proves it with TWO live-camera
// videos — one feed, one water — submitted before MIDNIGHT IST.
//
// The three clocks, all Asia/Kolkata BUSINESS-DAY anchored (never now±N hours):
//
//	create cutoff — a weigh date D is plannable only strictly BEFORE the farm's
//	                removal cutoff on D-1. At or after it the earliest offerable
//	                date is D+1, because tonight's removal window is already
//	                open and the removal operator cannot be assigned into it.
//	visibility    — the fasting card is served to its operator from the cutoff
//	                on (weigh date - 1). Before that instant the card is
//	                withheld from the list; the record itself stays readable.
//
// THE CUTOFF IS CONFIG, NOT CODE (maintainer decision 2026-09-07): it is read
// per tenant from feed_water_removal_config through feedwaterremoval/ports
// .CutoffReader and threaded into every rule below as a value. Weighing never
// names that table (it is isolated from every non-weighing table) and carries
// no literal hour of its own; a missing config row refuses the plan
// (ports.ErrCutoffNotConfigured) rather than inventing an evening.
//
//	deadline      — 00:00 IST of the weigh date. The kernel's midnight gate
//	                rolls an UNSUBMITTED fasting task and its campaign's work
//	                items forward one day together.
//
// The gate reads submitted_at, never status: operator SUBMISSION is what
// unblocks the next day's weighing. Verification is post-hoc review — a later
// rework verdict never re-blocks weighing that already happened.
const (
	FastingStatusOpen                = "open"
	FastingStatusPendingVerification = "pending_verification"
	FastingStatusCompleted           = "completed"
	FastingStatusRework              = "rework"

	// VerificationCategoryFasting is the fasting proof's own verifier queue
	// category, beside the weigh-capture category VerificationCategoryWeighing.
	VerificationCategoryFasting = "weighing_fasting"
	// VerificationRefTypeFasting is the source ref type carried on the
	// verification item and matched by the verdict consumer. Since the
	// per-shed correction (maintainer, 2026-09-03) the ref is ONE SHED's
	// evidence row (weighing_fasting_shed_proofs.fasting_shed_id): the review
	// grain follows the evidence, one item per shed.
	VerificationRefTypeFasting = "weighing_fasting_shed"
)

// FastingShedProof is ONE SHED's removal evidence on a fasting card
// (maintainer correction 2026-09-03: one feed video + one water video PER
// SHED, never one clip stretched over the whole round).
type FastingShedProof struct {
	FastingShedID  string `json:"fasting_shed_id,omitempty"`
	CampaignShedID string `json:"campaign_shed_id"`
	// ShedLabel names the shed the operator must empty; backend-owned, shown
	// verbatim on the slot header and the verifier item.
	ShedLabel     string `json:"shed_label"`
	FeedProofRef  string `json:"feed_proof_ref,omitempty"`
	WaterProofRef string `json:"water_proof_ref,omitempty"`
	Status        string `json:"status"`
	// ReworkReason is the verifier's rejection for THIS shed, verbatim.
	ReworkReason string `json:"rework_reason,omitempty"`
	RowVersion   int    `json:"row_version,omitempty"`
	// Answers are the removal operator's answers to the SOP's authored questions, as
	// submitted with this shed's captures (WEIGHING SOP, domain/sop.go).
	Answers SOPAnswers `json:"answers,omitempty"`
	// Proofs is {slot key: proof ref}: every capture recorded on this shed, keyed by the SOP's
	// slot. FeedProofRef / WaterProofRef above mirror the seeded feed_video / water_video slots
	// for older readers and are blank when the document has no such slot.
	Proofs RemovalProofRefs `json:"proofs,omitempty"`
	// ProofKinds is {proof ref: video | photo} as the proof register judged each capture --
	// an `either` slot's answer. Read from the register on EVERY path (submit, replay, card
	// list), so a retried verification enqueue names the same kind the first one did.
	ProofKinds map[string]string `json:"-"`
	// ShedLocationID routes the verifier item's shed filter; internal, not wire.
	ShedLocationID string `json:"-"`
}

// FastingTask is the wire-ready fasting row.
type FastingTask struct {
	FastingTaskID  string `json:"fasting_task_id"`
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	ParkID         string `json:"park_id"`
	ParkName       string `json:"park_name,omitempty"`
	OperatorUserID string `json:"operator_user_id"`
	// OperatorName is resolved for planner/monitor surfaces; blank when the
	// caller is the operator themself.
	OperatorName string `json:"operator_name,omitempty"`
	// PlannedWeighDate is the immutable audit anchor; WeighBusinessDate is the
	// current weigh date and rolls forward with the campaign's work items.
	PlannedWeighDate  string `json:"planned_weigh_date"`
	WeighBusinessDate string `json:"weigh_business_date"`
	// RemovalBusinessDate is the evening the work happens on: weigh date - 1.
	// Composed server-side so no client re-derives date arithmetic.
	RemovalBusinessDate string     `json:"removal_business_date"`
	Status              string     `json:"status"`
	FeedProofRef        string     `json:"feed_proof_ref,omitempty"`
	WaterProofRef       string     `json:"water_proof_ref,omitempty"`
	SubmittedBy         string     `json:"submitted_by,omitempty"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	VerifiedAt          *time.Time `json:"verified_at,omitempty"`
	// ReworkReason is the verifier's rejection reason, rendered verbatim.
	ReworkReason       string `json:"rework_reason,omitempty"`
	RolledForwardCount int    `json:"rolled_forward_count"`
	RowVersion         int    `json:"row_version"`
	// ShedCount and SubjectLabel give the card its farm-worded identity
	// ("Remove feed & water · Coimbatore · 4 sheds") without the client
	// composing copy.
	ShedCount    int    `json:"shed_count"`
	SubjectLabel string `json:"subject_label"`
	// Sheds is the per-shed evidence list: every non-canceled bucket of the
	// campaign, each owed its own feed + water videos.
	Sheds     []FastingShedProof `json:"sheds,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// FastingTaskPage is one keyset page of an operator's fasting cards.
type FastingTaskPage struct {
	Items      []FastingTask `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

// FastingShedCard is ONE SHED's removal card as the operator's list serves it
// (maintainer correction #2, 2026-09-03: one CARD per shed — the operator is
// never made to open an umbrella card to find the sheds). The parent round
// still owns the operator, the dates, and the midnight gate; the card carries
// what this one shed needs.
type FastingShedCard struct {
	FastingTaskID  string `json:"fasting_task_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	// FastingShedID is set once evidence exists for the shed; blank before.
	FastingShedID string `json:"fasting_shed_id,omitempty"`
	// ShedLabel and SubjectLabel are backend-owned copy, rendered verbatim:
	// "Castro 1" / "Remove feed & water · Castro 1".
	ShedLabel    string `json:"shed_label"`
	SubjectLabel string `json:"subject_label"`
	ParkName     string `json:"park_name,omitempty"`
	// Status is THIS shed's state: open | pending_verification | completed | rework.
	Status string `json:"status"`
	// ReworkReason is the verifier's rejection for THIS shed, verbatim.
	ReworkReason        string     `json:"rework_reason,omitempty"`
	FeedProofRef        string     `json:"feed_proof_ref,omitempty"`
	WaterProofRef       string     `json:"water_proof_ref,omitempty"`
	PlannedWeighDate    string     `json:"planned_weigh_date"`
	WeighBusinessDate   string     `json:"weigh_business_date"`
	RemovalBusinessDate string     `json:"removal_business_date"`
	SubmittedAt         *time.Time `json:"submitted_at,omitempty"`
	RowVersion          int        `json:"row_version"`
	// SOPVersion is the parent task's pinned weighing.session version; the service resolves
	// it into the card copy below. Internal, not wire.
	SOPVersion int `json:"-"`
	// Instruction, Proofs and Questions are the SOP's authored card copy (WEIGHING SOP,
	// domain/sop.go): the operator sentence, the capture slots (title, hint, video / photo /
	// either, compulsory), and the questions answered with them. Rendered verbatim; the phone
	// composes none of it.
	Instruction string             `json:"instruction,omitempty"`
	Proofs      []RemovalProofSlot `json:"proofs"`
	Questions   []SOPQuestion      `json:"questions"`
	// Answers are the answers already recorded on this shed (a submitted or rework card).
	Answers SOPAnswers `json:"answers,omitempty"`
	// ProofKinds is {slot key: video | photo} for every recorded capture -- the kind the
	// register judged it to be, which an `either` slot cannot say on its own. The phone picks
	// the player from it when it reopens the card without its own local state.
	ProofKinds map[string]string `json:"proof_kinds,omitempty"`
	// ProofRefs is {slot key: proof ref} already recorded on this shed, so a read-only card
	// renders every capture, not only the two seeded ones.
	ProofRefs RemovalProofRefs `json:"proof_refs,omitempty"`
}

// FastingShedCardPage is one keyset page of per-shed cards.
type FastingShedCardPage struct {
	Items      []FastingShedCard `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

// FastingVerdict is a verifier approve/rework applied to ONE SHED's evidence
// row by the verdict consumer. EventID keys replay; a redelivery applies once.
type FastingVerdict struct {
	TenantID string
	// FastingShedID is the shed evidence row the verdict lands on
	// (weighing_fasting_shed_proofs.fasting_shed_id).
	FastingShedID string
	// Status is VerificationStatusVerified or VerificationStatusRework.
	Status     string
	VerifiedBy string
	Reason     string
	EventID    string
}

// SubmitFastingShed is the operator's act (maintainer correction #2): ONE
// shed's own feed and water videos. The round's midnight gate is satisfied
// only when EVERY shed of the round has been submitted this way.
// FastingShedSubmitResult is one shed submit's outcome: the card the phone
// re-renders, the evidence row the verification enqueue names, and the parent
// round as it stands after this submit (whose submitted_at, when the LAST shed
// just went in, is the midnight gate's fact).
type FastingShedSubmitResult struct {
	Card     FastingShedCard
	Evidence FastingShedProof
	Task     FastingTask
	// Replayed marks an exact idempotent retry: no side effects ran and no
	// verification round may be re-raised.
	Replayed bool
}

type SubmitFastingShed struct {
	TenantID       string
	FastingTaskID  string
	CampaignShedID string
	// FeedProofRef / WaterProofRef are the LEGACY wire pair (an older phone): the service maps
	// them onto the feed_video / water_video slots when Proofs is empty.
	FeedProofRef  string
	WaterProofRef string
	// Proofs is {slot key: proof ref}, validated against the task's pinned rules by the service
	// (every compulsory slot present, no unknown slot, no ref twice) and by the store (each ref a
	// completed in-app capture of the slot's kind, unused by a sibling shed, not a rejected clip).
	Proofs RemovalProofRefs
	// SlotKinds is {slot key: video | photo | either}, resolved by the service from the pinned
	// rules for the store's type check. OrderedRefs is the same captures in slot order, what the
	// verifier item is built from.
	SlotKinds   map[string]string
	OrderedRefs []string
	// Answers to the SOP's authored removal questions, validated against the task's pinned
	// rules by the service and stored on the shed's evidence row.
	Answers        SOPAnswers
	IdempotencyKey string
	SubmittedBy    string
}

// EarliestPlannableWeighDate answers the create cutoff: the first weigh date a
// planner may still choose at instant now under the farm's configured cutoff.
// Strictly before the cutoff that is TOMORROW (tonight's removal window has
// not opened, so tonight's operator can still be assigned); at or after it,
// the DAY AFTER TOMORROW.
//
// now is the caller's clock, passed in so tests are deterministic; cutoff is
// the tenant's configured value, never a literal. The arithmetic lives in
// feedwaterremoval/domain so PC Care answers the identical question.
func EarliestPlannableWeighDate(now time.Time, cutoff fwrdomain.Cutoff) string {
	return fwrdomain.EarliestPlannableBusinessDate(now, cutoff)
}

// WeighDateAllowsFastingCreate reports whether weighDate (a validated
// YYYY-MM-DD business date) is still plannable at instant now.
func WeighDateAllowsFastingCreate(weighDate string, now time.Time, cutoff fwrdomain.Cutoff) bool {
	return fwrdomain.DateAllowsPlanning(weighDate, now, cutoff)
}

// FastingCardVisibleFrom is the instant the operator's card appears: the
// configured cutoff on the evening before the (current) weigh date.
func FastingCardVisibleFrom(weighBusinessDate string, cutoff fwrdomain.Cutoff) (time.Time, error) {
	return fwrdomain.VisibleFrom(weighBusinessDate, cutoff)
}

// FastingDeadline is the submit deadline: 00:00 IST of the weigh date. A
// fasting task with submitted_at unset at or after this instant rolls forward.
func FastingDeadline(weighBusinessDate string) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", weighBusinessDate, biztime.DefaultLocation())
	if err != nil {
		return time.Time{}, err
	}
	return day, nil
}

// RemovalBusinessDate is the evening the removal happens on: weigh date - 1.
func RemovalBusinessDate(weighBusinessDate string) string {
	day, err := time.ParseInLocation("2006-01-02", weighBusinessDate, biztime.DefaultLocation())
	if err != nil {
		return ""
	}
	return day.AddDate(0, 0, -1).Format("2006-01-02")
}

// FastingSubjectLabel composes the sentence the verifier (and the operator's
// card) reads. Farm wording only; the park is the place the round happens and
// the shed count is its size. A blank park degrades to the bare work name
// rather than printing an id or a dangling separator.
// FastingShedSubjectLabel names ONE shed's verifier item: the work, then the
// shed the clip must show. A blank label degrades to the bare work name.
func FastingShedSubjectLabel(shedLabel string) string {
	label := "Remove feed & water"
	if trimmed := strings.TrimSpace(shedLabel); trimmed != "" {
		label += " · " + trimmed
	}
	return label
}

func FastingSubjectLabel(parkName string, shedCount int) string {
	label := "Remove feed & water"
	if trimmed := strings.TrimSpace(parkName); trimmed != "" {
		label += " · " + trimmed
	}
	if shedCount == 1 {
		label += " · 1 shed"
	} else if shedCount > 1 {
		label += fmt.Sprintf(" · %d sheds", shedCount)
	}
	return label
}

// SlotKinds re-keys the register's {ref: kind} onto the card's {slot key: kind}; a ref the
// register does not know is left out rather than guessed.
func SlotKinds(refs RemovalProofRefs, kindsByRef map[string]string) map[string]string {
	if len(refs) == 0 || len(kindsByRef) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, ref := range refs {
		if kind := kindsByRef[ref]; kind != "" {
			out[key] = kind
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
