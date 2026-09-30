// Package ports declares the tasks module's storage seam. The postgres adapter implements it; the
// app service and event consumers depend only on these interfaces.
package ports

import (
	"context"
	"time"

	"github.com/vgoats/goatos/backend/internal/sop/authored"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// OpenWorkflowCommand opens one workflow instance (idempotent on the natural key: a redelivered
// goat.created / goat.exited event, or a twin's shared mother track, lands on ON CONFLICT DO
// NOTHING and inserts nothing).
type OpenWorkflowCommand struct {
	TenantID      string
	TemplateKey   string
	SubjectGoatID string
	DamGoatID     *string
	EventAt       time.Time
	ParkID        *string
	ShedID        *string
	// SubjectRefID keys a workflow on a non-goat subject (a reconcile card, a shifting event).
	// Uniqueness is (tenant, template_key, subject_ref_id) -- migration 000311.
	SubjectRefID *string
	// CaptureEvidence is the capture form's snapshot to stamp on the instance (death: the Add
	// death form's proofs and answers). Ignored on a natural-key conflict: the first open wins and
	// a redelivered event never relabels it.
	CaptureEvidence authored.Evidence
	// SaleHasAnimals decides the sale track's `sale_has_animals` steps (maintainer decision
	// 2026-09-25). Nil = true: every other template, and a sale event written before the fact
	// was carried, compiles every step.
	SaleHasAnimals *bool
	// SaleKinds is the sale lines' product kinds, for the per-kind step conditions (2026-09-28).
	// Nil on every other template and on a sale event written before the key existed.
	SaleKinds []string
	// ClockAnchor is the instant the steps' due times count from, when it is not EventAt: a sale
	// planned for a later day counts from that day (domain.SaleClockAnchor). Zero = EventAt, which
	// is every other workflow. The workflow's event_at stays the recording moment either way.
	ClockAnchor time.Time
}

// GeneralSOP is one startable general work instruction.
type GeneralSOP struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// StepCount is the number of steps the published main track carries.
	StepCount int `json:"step_count"`
}

// GoatWorkflowFacts is the canonical goat-row slice the consumers read (one indexed PK lookup).
type GoatWorkflowFacts struct {
	GoatID          string
	DisplayID       string
	Species         string
	Sex             string
	Breed           string
	DOB             *time.Time
	TimeOfBirth     *string // HH:MM (goats.time_of_birth), nil = unknown -> 07:00 IST fallback
	LifecycleStatus string
	ParkID          *string
	ShedID          *string
	// PartitionLabel is the animal's pen inside ShedID ("Part 3", "1"): the human label from
	// goat_shed_partitions, blank for an undivided shed ('whole' never leaves the adapter).
	PartitionLabel string
}

// DeathVerdictCommand applies a verifier's approve/rework verdict to a death workflow's sign-off.
type DeathVerdictCommand struct {
	// RecordingKey is the verdict's item key; a rework appends capture re-shoot steps keyed on it
	// so a redelivered verdict inserts nothing.
	RecordingKey string
	TenantID     string
	WorkflowID   string
	VerifiedBy   string
	Reason       string
	VerdictAt    time.Time
}

// DeathEvidenceReview is the approved death workflow ready for ONE generic verification item:
// the instance (with its capture snapshot and review round = row_version) and every step, so the
// app service composes the bundle through the SAME builder the completion path uses. It is read
// only after the approval transaction has opened the verification gate; before that, operator
// uploads remain staged and invisible to Verify.
type DeathEvidenceReview struct {
	Workflow   domain.WorkflowInstance
	Actions    []domain.WorkflowAction
	OperatorID string
}

// BirthStepVerdictCommand applies a verifier's verdict to ONE recorded step of a birth workflow
// (maintainer decision 2026-09-16: birth evidence is reviewed per step). ActionID is the
// verification item's ref_id; the workflow is resolved from it.
type BirthStepVerdictCommand struct {
	RecordingKey string
	TenantID     string
	ActionID     string
	Approved     bool
	VerifiedBy   string
	Reason       string
	VerdictAt    time.Time
}

// Repository is the tasks module's storage port. All reads/writes are tenant-scoped; action writes
// maintain the workflow_instances card fields in the same transaction (compute-on-write).
type Repository interface {
	// OpenWorkflow inserts the instance plus its template's action rows atomically. Returns
	// created=false when the natural key already exists (nothing is touched).
	OpenWorkflow(ctx context.Context, cmd OpenWorkflowCommand) (created bool, err error)

	// GoatWorkflowFacts reads the canonical goat row by PK.
	GoatWorkflowFacts(ctx context.Context, tenantID, goatID string) (GoatWorkflowFacts, error)

	// ResolveDamGoat resolves a free-text dam reference (uuid or identifier value) to a goat_id.
	// Returns domain.ErrNotFound when it resolves to no canonical animal.
	ResolveDamGoat(ctx context.Context, tenantID, damRef string) (string, error)

	// ListWorkflows serves one keyset page of cards plus the day's chip counts.
	ListWorkflows(ctx context.Context, q domain.WorkflowListQuery) (domain.WorkflowListPage, error)

	// ListColostrumDay serves the Colostrum lens: one keyset page of cards for the kids with
	// colostrum feeds due on ONE business date, counted at that day's grain. It is a different read
	// from ListWorkflows rather than a filter on it because the birth list keys on the BIRTH date
	// and counts every operator action, neither of which answers "what colostrum is due today"
	// (docs/decisions/colostrum-milk-module.md).
	ListColostrumDay(ctx context.Context, q domain.ColostrumDayQuery) (domain.WorkflowListPage, error)

	// GetWorkflow serves the detail: card header + facts + all action rows.
	GetWorkflow(ctx context.Context, tenantID, workflowID string, now time.Time) (domain.WorkflowDetail, error)

	// AnswerAction / CompleteAction run the domain state machine under the request-level idempotency
	// contract and maintain the card fields in the same transaction.
	AnswerAction(ctx context.Context, cmd domain.AnswerActionCommand) (domain.ActionWriteResult, error)
	CompleteAction(ctx context.Context, cmd domain.CompleteActionCommand) (domain.ActionWriteResult, error)

	// CompleteTagActionForGoat records the permanent-RFID prerequisite without completing the
	// mandatory tagging-video task. No-op when there is no such open step.
	CompleteTagActionForGoat(ctx context.Context, tenantID, goatID string, completedAt time.Time) error

	// CompleteSaleTagStep completes the sale workflow's sale_tag_animals step for the deal when
	// its allocation confirm lands. No-op when there is no such open step.
	CompleteSaleTagStep(ctx context.Context, tenantID, dealID string, completedAt time.Time) error

	// KID STAGE SHIFT TASKS (2026-09-30). LitterOfChild resolves the birth event a kid was born in
	// (domain.ErrNotFound when none); LitterKids lists that litter's kids and current stages; and
	// ReconcileLitterShiftSteps applies the herd register to the litter workflow's shift steps
	// (a no-op when the litter has no workflow).
	LitterOfChild(ctx context.Context, tenantID, goatID string) (string, error)
	LitterKids(ctx context.Context, tenantID, birthEventID string) ([]domain.LitterKid, error)
	ReconcileLitterShiftSteps(ctx context.Context, tenantID, birthEventID string, at time.Time) error
	// CancelSaleWorkflow cancels the sale workflow of a deal marked failed: every UNFINISHED step
	// is cancelled and the card closes; finished steps keep their record. No-op when the deal has
	// no workflow or it is no longer open.
	CancelSaleWorkflow(ctx context.Context, tenantID, dealID string) error
	// ReanchorSaleWorkflow moves a PLANNED sale's unfinished step clocks to the day it actually
	// closed (saleDate, the restamped business date). A workflow anchored on its recording is left
	// alone; a redelivered close finds the anchor already moved and changes nothing.
	ReanchorSaleWorkflow(ctx context.Context, tenantID, dealID, saleDate string) error
	// ReconcileAnimalPurchaseDecisionStep follows the newest load counts, reopening
	// a completed decision when later candidates sync. Zero counts reconcile a
	// newly opened workflow with an earlier event receipt without overwriting it.
	ReconcileAnimalPurchaseDecisionStep(ctx context.Context, tenantID, loadID string, pending, decided int, completedAt time.Time) error
	// CompleteFeedPurchaseReachedStep completes the feed-purchase workflow's arrival step when the
	// ledger marks the load delivered.
	CompleteFeedPurchaseReachedStep(ctx context.Context, tenantID, purchaseID string, completedAt time.Time) error
	// CompleteToxinTestStep completes the feed-purchase workflow's aflatoxin step when a round on
	// the load is accepted.
	CompleteToxinTestStep(ctx context.Context, tenantID, purchaseID string, completedAt time.Time) error

	// DeathEvidenceForVerification loads an admin-approved death workflow (every step) by subject
	// goat. Returns domain.ErrNotFound when no in-review death workflow exists.
	DeathEvidenceForVerification(ctx context.Context, tenantID, goatID string) (DeathEvidenceReview, error)

	// CancelDeathWorkflowForGoat closes the staged workflow after an admin rejects the death.
	// Idempotent and a no-op when no workflow exists.
	CancelDeathWorkflowForGoat(ctx context.Context, tenantID, goatID string, canceledAt time.Time) error
	// CancelBirthWorkflowsForRejectedBirth cancels a REJECTED birth's kid workflows and its shared
	// mother track (maintainer decision 2026-09-25) and returns every action id of those
	// workflows, so the caller can withdraw their still-pending verifier items. Idempotent: a
	// second call changes nothing and returns the same ids.
	CancelBirthWorkflowsForRejectedBirth(ctx context.Context, tenantID, birthEventID, motherGoatID string, childGoatIDs []string) ([]string, error)

	// ApplyDeathSignoffApproved completes the sign-off action and the workflow after a verifier
	// approves the death evidence. Idempotent.
	ApplyDeathSignoffApproved(ctx context.Context, cmd DeathVerdictCommand) error

	// BounceDeathVideosForRework resets every proof-bearing step to 'rework' (answers kept) and
	// closes the verification gate after a verifier rejects the evidence
	// (domain.ReopenDeathProofSteps). Idempotent.
	BounceDeathVideosForRework(ctx context.Context, cmd DeathVerdictCommand) error

	// ListGeneralSOPs reads the tenant's PUBLISHED general SOPs (kind = general) -- the work
	// instructions an operator may start by hand.
	ListGeneralSOPs(ctx context.Context, tenantID string) ([]GeneralSOP, error)

	// WorkflowIDBySubjectRef returns the workflow keyed on (template_key, subject_ref_id), or
	// domain.ErrNotFound.
	WorkflowIDBySubjectRef(ctx context.Context, tenantID, templateKey, subjectRefID string) (string, error)

	// ReopenProofStepsForRework sends every completed proof-bearing operator step of a SOP-driven
	// workflow (reconcile, shifting) back to 'rework' with its proofs cleared, after a verifier
	// rejects the evidence. Idempotent.
	ReopenProofStepsForRework(ctx context.Context, tenantID, workflowID, reason string) error
	// ApplyBirthStepVerdict applies one verdict to one in_review birth step (domain.ApplyStepVerdict)
	// and recomputes the card in the same transaction. A verdict for a step that is not awaiting
	// one is a benign redelivery and a no-op; a step that does not exist is domain.ErrNotFound.
	ApplyBirthStepVerdict(ctx context.Context, cmd BirthStepVerdictCommand) error

	// ApplyBirthSignoffApproved / BounceBirthVideoForRework serve the retired WHOLE-WORKFLOW birth
	// bundle (ref_type workflow_birth_signoff) so an item enqueued before the per-step cutover still
	// lands its verdict. Nothing enqueues that shape any more.
	ApplyBirthSignoffApproved(ctx context.Context, cmd DeathVerdictCommand) error
	BounceBirthVideoForRework(ctx context.Context, cmd DeathVerdictCommand) error

	// AppendCaptureReshootSteps appends one "Re-shoot report proof" step per capture proof to the
	// workflow (domain.CaptureReshootSteps), idempotent on the workflow_actions natural key, and
	// recomputes the card in the same transaction.
	AppendCaptureReshootSteps(ctx context.Context, tenantID, workflowID string, capture authored.Evidence, indexes []int, recordingKey, reason string) error

	// BirthWorkflowIDForEvent resolves the track a litter's report re-shoot is appended to: the
	// mother track of the birth event, else its first kid track. domain.ErrNotFound when none.
	BirthWorkflowIDForEvent(ctx context.Context, tenantID, birthEventID string) (string, error)

	// FetchShedDetails fetches the shed name and partition label for operational location composition.
	// Returns empty strings if the shed is not found or has no partition.
	FetchShedDetails(ctx context.Context, tenantID, shedID string) (shedName, partitionLabel string, err error)
}
