// Package ports declares the tasks module's storage seam. The postgres adapter implements it; the
// app service and event consumers depend only on these interfaces.
package ports

import (
	"context"
	"time"

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
}

// DeathVerdictCommand applies a verifier's approve/rework verdict to a death workflow's sign-off.
type DeathVerdictCommand struct {
	TenantID   string
	WorkflowID string
	VerifiedBy string
	Reason     string
	VerdictAt  time.Time
}

// DeathEvidenceReview is the approved evidence bundle ready for ONE generic verification item.
// It is read only after the approval transaction has moved the internal review action to
// in_review; before that, operator uploads remain staged and invisible to Verify.
type DeathEvidenceReview struct {
	WorkflowID string
	OperatorID string
	ParkID     string
	ShedID     string
	EventDate  string
	ProofRefs  []string
	Round      int
}

type BirthEvidenceReview struct {
	WorkflowID  string
	SubjectRole string
	OperatorID  string
	ParkID      string
	ShedID      string
	EventDate   string
	ProofRefs   []string
	Round       int
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

	// DeathEvidenceForVerification loads an admin-approved death evidence pair by subject goat.
	// Returns domain.ErrNotFound when no in-review death workflow exists.
	DeathEvidenceForVerification(ctx context.Context, tenantID, goatID string) (DeathEvidenceReview, error)
	// BirthWorkflowEvidenceForVerification opens one review gate for exactly one completed mother
	// or child workflow. Sibling workflows never participate in its readiness or verdict.
	BirthWorkflowEvidenceForVerification(ctx context.Context, tenantID, workflowID string) (BirthEvidenceReview, error)

	// CancelDeathWorkflowForGoat closes the staged workflow after an admin rejects the death.
	// Idempotent and a no-op when no workflow exists.
	CancelDeathWorkflowForGoat(ctx context.Context, tenantID, goatID string, canceledAt time.Time) error

	// ApplyDeathSignoffApproved completes the sign-off action and the workflow after a verifier
	// approves the death evidence. Idempotent.
	ApplyDeathSignoffApproved(ctx context.Context, cmd DeathVerdictCommand) error

	// BounceDeathVideosForRework resets both video actions to 'rework' and the sign-off to pending
	// after a verifier rejects the evidence. Idempotent.
	BounceDeathVideosForRework(ctx context.Context, cmd DeathVerdictCommand) error
	ApplyBirthSignoffApproved(ctx context.Context, cmd DeathVerdictCommand) error
	BounceBirthVideoForRework(ctx context.Context, cmd DeathVerdictCommand) error
}
