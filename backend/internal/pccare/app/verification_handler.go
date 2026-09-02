package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// PC Care verification consumer. A PC Care task is completed only when an independent verifier
// approves the operators' video set. The verification module emits a generic verdict event on
// every approve/reject; this handler is the pc_care-side consumer:
//
//	verification.verdict.approved (our pc_care item) -> ApplyVerifiedTask     (task completed NOW)
//	verification.verdict.rework   (our pc_care item) -> BounceTaskForRework   (back to the operators)
//
// It filters STRICTLY on source.module + source.ref_type so a vaccination/feed/weighing verdict
// is ignored (feed packing handler clone).
const (
	eventVerificationVerdictApproved = "verification.verdict.approved"
	eventVerificationVerdictRework   = "verification.verdict.rework"
	eventPCCarePendingVerification   = "pc_care.task.pending_verification"
)

// pcCareVerdictPayload is the subset of the verdict payload this consumer reads.
type pcCareVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

type pcCarePendingVerificationPayload struct {
	TaskID              string `json:"task_id"`
	Category            string `json:"category"`
	ParkID              string `json:"park_id"`
	ShedID              string `json:"shed_id"`
	ShedName            string `json:"shed_name"`
	PartitionLabel      string `json:"partition_label"`
	VaccineLabel        string `json:"vaccine_label"`
	PlannedBusinessDate string `json:"planned_business_date"`
	MediaRefs           []struct {
		ProofRef string `json:"proof_ref"`
		Label    string `json:"label"`
	} `json:"media_refs"`
	AnimalCount int32  `json:"animal_count"`
	OperatorID  string `json:"operator_id"`
	RowVersion  int32  `json:"row_version"`
}

// VerdictStore is the narrow slice of the task store this consumer drives — kept small so the
// shared eventwiring registration can hand it exactly the verdict half and nothing else.
type VerdictStore interface {
	ApplyVerifiedTask(ctx context.Context, p ports.ApplyVerifiedTaskParams) (bool, error)
	BounceTaskForRework(ctx context.Context, p ports.BounceTaskParams) (bool, error)
}

// PCCarePendingVerificationHandler turns the module-owned transactional submit event into the
// generic verifier item. If verification is temporarily down, the outbox/domain consumer retries
// this event; the idempotency key is stable at task row-version grain.
type PCCarePendingVerificationHandler struct {
	enqueuer VerificationEnqueuer
	log      *slog.Logger
}

func NewPCCarePendingVerificationHandler(enqueuer VerificationEnqueuer, log *slog.Logger) *PCCarePendingVerificationHandler {
	return &PCCarePendingVerificationHandler{enqueuer: enqueuer, log: log}
}

var _ eventbus.Handler = (*PCCarePendingVerificationHandler)(nil)

func (h *PCCarePendingVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventPCCarePendingVerification, h)
}

func (h *PCCarePendingVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventPCCarePendingVerification || h.enqueuer == nil {
		return nil
	}
	var p pcCarePendingVerificationPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	// A submitted vaccine-stock task is judged by the PC DIRECTOR on the module's own
	// stock-verdict route, never by the tenant verifier (maintainer decision 2026-09-02) — so
	// no verification item is enqueued for it. The event itself still fires: it is the durable
	// record that the task entered pending_verification.
	if domain.IsDirectorApprovedCategory(strings.TrimSpace(p.Category)) {
		return nil
	}
	refs := make([]ports.LabeledRef, 0, len(p.MediaRefs))
	for _, ref := range p.MediaRefs {
		if strings.TrimSpace(ref.ProofRef) == "" {
			continue
		}
		refs = append(refs, ports.LabeledRef{ProofRef: ref.ProofRef, Label: ref.Label})
	}
	capturedAt := time.Now().UTC()
	if !e.OccurredAt.IsZero() {
		capturedAt = e.OccurredAt.UTC()
	}
	return h.enqueuer.EnqueuePCCareVerification(ctx, VerificationEnqueueRequest{
		TenantID:            e.TenantID,
		TaskID:              strings.TrimSpace(p.TaskID),
		Category:            strings.TrimSpace(p.Category),
		ParkID:              strings.TrimSpace(p.ParkID),
		ShedID:              strings.TrimSpace(p.ShedID),
		ShedName:            strings.TrimSpace(p.ShedName),
		PartitionLabel:      strings.TrimSpace(p.PartitionLabel),
		VaccineLabel:        strings.TrimSpace(p.VaccineLabel),
		PlannedBusinessDate: strings.TrimSpace(p.PlannedBusinessDate),
		MediaRefs:           refs,
		AnimalCount:         p.AnimalCount,
		OperatorID:          strings.TrimSpace(p.OperatorID),
		CapturedAt:          capturedAt,
		IdempotencyKey:      "pc-care-verification:" + strings.TrimSpace(p.TaskID) + ":" + strconv.Itoa(int(p.RowVersion)),
	})
}

// PCCareVerificationHandler applies a verifier's verdict to the task it verified.
type PCCareVerificationHandler struct {
	store VerdictStore
	log   *slog.Logger
}

// NewPCCareVerificationHandler constructs the consumer over the verdict store.
func NewPCCareVerificationHandler(store VerdictStore, log *slog.Logger) *PCCareVerificationHandler {
	return &PCCareVerificationHandler{store: store, log: log}
}

var _ eventbus.Handler = (*PCCareVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *PCCareVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a pc_care item to the matching write.
func (h *PCCareVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVerificationVerdictApproved && e.Type != eventVerificationVerdictRework {
		return nil
	}
	var p pcCareVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != domain.VerificationModulePCCare || p.Source.RefType != domain.VerificationRefTypeTask {
		return nil
	}
	taskID := strings.TrimSpace(p.Source.RefID)
	if taskID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}

	switch e.Type {
	case eventVerificationVerdictApproved:
		_, err := h.store.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{
			TenantID:   e.TenantID,
			TaskID:     taskID,
			VerifiedBy: strings.TrimSpace(p.VerifiedBy),
			// Thread the verification event id as the trace id so the pc_care.task.completed
			// outbox envelope carries a non-empty trace_id.
			TraceID: e.ID,
		})
		return err
	case eventVerificationVerdictRework:
		_, err := h.store.BounceTaskForRework(ctx, ports.BounceTaskParams{
			TenantID: e.TenantID,
			TaskID:   taskID,
			Reason:   strings.TrimSpace(p.Reason),
			TraceID:  e.ID,
		})
		return err
	}
	return nil
}
