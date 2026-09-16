package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// A routine reviewed by the verifier hands ONE item per submit -- every capture plus the
// answers as context rows -- to the verifier queue. Two consumers, the pen-visit shape:
//
//	pen_routine.submitted                     -> EnqueuePenRoutineVerification  (ONE item)
//	verification.verdict.approved (our item)  -> ApplyVerified   (emits pen_routine.verified in-tx)
//	verification.verdict.rework   (our item)  -> BounceForRework (the assignee does it again)
//
// Both filter STRICTLY on the module / ref_type this module owns, so a vaccination, feed or
// pen-visit verdict is ignored here exactly as ours is ignored there. A routine with review
// 'none' completes on submit and never reaches this file: its submitted event carries
// status 'completed' and the enqueue handler skips it.
const (
	eventRoutineSubmitted            = "pen_routine.submitted"
	eventVerificationVerdictApproved = "verification.verdict.approved"
	eventVerificationVerdictRework   = "verification.verdict.rework"

	// VerificationVertical / VerificationModule / VerificationCategory / VerificationRefType are
	// the task's identity in the verifier queue. Category pen_routine sits under its OWN
	// navigation module (Routines); the ref type is what the verdict consumer keys on.
	VerificationVertical = "operations"
	VerificationModule   = "pen_routines"
	VerificationCategory = "pen_routine"
	VerificationRefType  = "pen_routine_task"
)

// VerificationEnqueueRequest is what the bridge needs to mint the verifier item.
type VerificationEnqueueRequest struct {
	TenantID       string
	TaskID         string
	RoutineName    string
	CadenceLine    string
	ParkID         string
	ParkName       string
	ShedID         string
	PartitionLabel string
	PenLabel       string
	TriggerKinds   []string
	SourceDate     string
	Proofs         []domain.ProofItem
	AnswerRows     []domain.AnswerRow
	SubmittedBy    string
	CapturedAt     time.Time
	IdempotencyKey string
}

// VerificationEnqueuer is the seam into the verification module (adapters/verificationbridge).
type VerificationEnqueuer interface {
	EnqueuePenRoutineVerification(ctx context.Context, in VerificationEnqueueRequest) error
}

// submittedPayload is the subset of the pen_routine.submitted payload the enqueuer reads.
type submittedPayload struct {
	TaskID         string             `json:"task_id"`
	RoutineName    string             `json:"routine_name"`
	ReviewKind     string             `json:"review_kind"`
	ParkID         string             `json:"park_id"`
	ParkName       string             `json:"park_name"`
	ShedID         string             `json:"shed_id"`
	PartitionLabel string             `json:"partition_label"`
	PenLabel       string             `json:"pen_label"`
	TriggerKinds   []string           `json:"trigger_kinds"`
	SourceDate     string             `json:"source_business_date"`
	Status         string             `json:"status"`
	Proofs         []domain.ProofItem `json:"proof_refs"`
	AnswerRows     []domain.AnswerRow `json:"answer_rows"`
	RowVersion     int                `json:"row_version"`
	ChangedBy      string             `json:"changed_by_user_id"`
}

// TaskReader is the narrow read the enqueue handler uses to resolve the cadence line, which
// the payload does not carry. Optional: without it the item's context has no cadence row.
type TaskReader interface {
	GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error)
}

// PendingVerificationHandler turns the module-owned transactional submit event into the generic
// verifier item. If verification is temporarily down, the outbox/domain consumer retries this
// event; the idempotency key is stable at (task, row version) grain, so a redo after a rework
// mints a fresh item while a retry collapses onto one.
type PendingVerificationHandler struct {
	enqueuer VerificationEnqueuer
	tasks    TaskReader
	log      *slog.Logger
}

// NewPendingVerificationHandler constructs the enqueue consumer.
func NewPendingVerificationHandler(enqueuer VerificationEnqueuer, log *slog.Logger) *PendingVerificationHandler {
	return &PendingVerificationHandler{enqueuer: enqueuer, log: log}
}

// WithTaskReader wires the cadence-line lookup.
func (h *PendingVerificationHandler) WithTaskReader(r TaskReader) *PendingVerificationHandler {
	h.tasks = r
	return h
}

var _ eventbus.Handler = (*PendingVerificationHandler)(nil)

// Register subscribes to the submit event.
func (h *PendingVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventRoutineSubmitted, h)
}

// HandleEvent enqueues one verifier item per submit that awaits review.
func (h *PendingVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventRoutineSubmitted || h.enqueuer == nil {
		return nil
	}
	var p submittedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	taskID := strings.TrimSpace(p.TaskID)
	if taskID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	// A review-'none' routine completed on submit: nothing to verify.
	if p.Status != domain.StatusPendingVerification {
		return nil
	}
	capturedAt := time.Now().UTC()
	if !e.OccurredAt.IsZero() {
		capturedAt = e.OccurredAt.UTC()
	}
	cadence := ""
	if h.tasks != nil {
		if task, err := h.tasks.GetTask(ctx, e.TenantID, taskID); err == nil {
			cadence = task.CadenceLine
		}
	}
	return h.enqueuer.EnqueuePenRoutineVerification(ctx, VerificationEnqueueRequest{
		TenantID:       e.TenantID,
		TaskID:         taskID,
		RoutineName:    strings.TrimSpace(p.RoutineName),
		CadenceLine:    cadence,
		ParkID:         strings.TrimSpace(p.ParkID),
		ParkName:       strings.TrimSpace(p.ParkName),
		ShedID:         strings.TrimSpace(p.ShedID),
		PartitionLabel: strings.TrimSpace(p.PartitionLabel),
		PenLabel:       strings.TrimSpace(p.PenLabel),
		TriggerKinds:   p.TriggerKinds,
		SourceDate:     strings.TrimSpace(p.SourceDate),
		Proofs:         p.Proofs,
		AnswerRows:     p.AnswerRows,
		SubmittedBy:    strings.TrimSpace(p.ChangedBy),
		CapturedAt:     capturedAt,
		IdempotencyKey: "pen-routine-verification:" + taskID + ":" + strconv.Itoa(p.RowVersion),
	})
}

// verdictPayload is the subset of the verdict payload the applier reads.
type verdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// VerdictStore is the narrow slice of the repository the applier drives.
type VerdictStore interface {
	ApplyVerified(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error)
	BounceForRework(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error)
}

// PenRoutineVerificationHandler applies a verifier's verdict to the task it judged.
type PenRoutineVerificationHandler struct {
	store VerdictStore
	log   *slog.Logger
}

// NewPenRoutineVerificationHandler constructs the applier over the verdict store.
func NewPenRoutineVerificationHandler(store VerdictStore, log *slog.Logger) *PenRoutineVerificationHandler {
	return &PenRoutineVerificationHandler{store: store, log: log}
}

var _ eventbus.Handler = (*PenRoutineVerificationHandler)(nil)

// Register subscribes to both verdict event types.
func (h *PenRoutineVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a pen_routines item to the matching write.
func (h *PenRoutineVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVerificationVerdictApproved && e.Type != eventVerificationVerdictRework {
		return nil
	}
	var p verdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != VerificationModule || p.Source.RefType != VerificationRefType {
		return nil
	}
	taskID := strings.TrimSpace(p.Source.RefID)
	if taskID == "" || strings.TrimSpace(e.TenantID) == "" || h.store == nil {
		return nil
	}
	params := ports.VerdictParams{
		TenantID:   e.TenantID,
		TaskID:     taskID,
		VerifiedBy: strings.TrimSpace(p.VerifiedBy),
		Reason:     strings.TrimSpace(p.Reason),
		TraceID:    e.ID,
	}
	if e.Type == eventVerificationVerdictRework {
		_, err := h.store.BounceForRework(ctx, params)
		return err
	}
	_, err := h.store.ApplyVerified(ctx, params)
	return err
}
