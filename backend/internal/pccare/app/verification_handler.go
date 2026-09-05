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
	// RemovalPens is present ONLY on a round-grain feed & water removal submit. One entry per
	// pen, each with that pen's own two videos, because a single clip stretched over four pens
	// proves nothing and the verifier cannot tell which pen was actually emptied.
	RemovalPens []struct {
		RemovalPenID  string `json:"removal_pen_id"`
		GatedTaskID   string `json:"gated_task_id"`
		PenLabel      string `json:"pen_label"`
		FeedProofRef  string `json:"feed_proof_ref"`
		WaterProofRef string `json:"water_proof_ref"`
		RowVersion    int32  `json:"row_version"`
	} `json:"removal_pens"`
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
	// A ROUND-grain removal fans out into ONE item PER PEN: the review grain follows the
	// evidence, and the evidence is shot pen by pen. Each item's idempotency key is that pen's
	// own row version, so a re-shot pen mints a fresh item while a retry collapses onto one and
	// the pens that were already approved are never re-queued.
	if len(p.RemovalPens) > 0 {
		for _, pen := range p.RemovalPens {
			penID := strings.TrimSpace(pen.RemovalPenID)
			if penID == "" {
				continue
			}
			penRefs := make([]ports.LabeledRef, 0, 2)
			if ref := strings.TrimSpace(pen.FeedProofRef); ref != "" {
				penRefs = append(penRefs, ports.LabeledRef{ProofRef: ref, Label: "Feed removal video"})
			}
			if ref := strings.TrimSpace(pen.WaterProofRef); ref != "" {
				penRefs = append(penRefs, ports.LabeledRef{ProofRef: ref, Label: "Water removal video"})
			}
			if err := h.enqueuer.EnqueuePCCareVerification(ctx, VerificationEnqueueRequest{
				TenantID:            e.TenantID,
				TaskID:              strings.TrimSpace(p.TaskID),
				Category:            strings.TrimSpace(p.Category),
				ParkID:              strings.TrimSpace(p.ParkID),
				PlannedBusinessDate: strings.TrimSpace(p.PlannedBusinessDate),
				MediaRefs:           penRefs,
				OperatorID:          strings.TrimSpace(p.OperatorID),
				CapturedAt:          capturedAt,
				// The item is the PEN's, so its source ref and its subject are the pen's too.
				RemovalPenID:    penID,
				RemovalPenLabel: strings.TrimSpace(pen.PenLabel),
				IdempotencyKey:  "pc-care-removal-pen:" + penID + ":" + strconv.Itoa(int(pen.RowVersion)),
			}); err != nil {
				return err
			}
		}
		return nil
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
	// removals applies a round-grain removal's PER-PEN verdicts. Nil in a task-only test; a
	// pen verdict then routes nowhere rather than being misapplied to a task.
	removals RemovalVerdictStore
	log      *slog.Logger
}

// RemovalVerdictStore is the narrow slice of the round store this consumer drives.
type RemovalVerdictStore interface {
	ApplyVerifiedRemovalPen(ctx context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error)
	BounceRemovalPenForRework(ctx context.Context, p ports.ApplyRemovalPenVerdictParams) (bool, error)
}

// NewPCCareVerificationHandler constructs the consumer over the verdict store.
func NewPCCareVerificationHandler(store VerdictStore, log *slog.Logger) *PCCareVerificationHandler {
	return &PCCareVerificationHandler{store: store, log: log}
}

// WithRemovalStore wires the per-pen removal verdict writes.
func (h *PCCareVerificationHandler) WithRemovalStore(r RemovalVerdictStore) *PCCareVerificationHandler {
	h.removals = r
	return h
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
	if p.Source.Module != domain.VerificationModulePCCare {
		return nil
	}
	refID := strings.TrimSpace(p.Source.RefID)
	if refID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	// A round-grain removal's verdict lands on ONE PEN's evidence row, and the parent card is
	// rolled up from the pens inside that same write. Routed on ref_type so a task verdict and
	// a pen verdict can never be applied to each other's row.
	if p.Source.RefType == domain.VerificationRefTypeRemovalPen {
		if h.removals == nil {
			return nil
		}
		params := ports.ApplyRemovalPenVerdictParams{
			TenantID:     e.TenantID,
			RemovalPenID: refID,
			VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
			Reason:       strings.TrimSpace(p.Reason),
			TraceID:      e.ID,
		}
		if e.Type == eventVerificationVerdictApproved {
			_, err := h.removals.ApplyVerifiedRemovalPen(ctx, params)
			return err
		}
		_, err := h.removals.BounceRemovalPenForRework(ctx, params)
		return err
	}
	if p.Source.RefType != domain.VerificationRefTypeTask {
		return nil
	}
	taskID := refID

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
