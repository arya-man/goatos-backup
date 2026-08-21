package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

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

// VerdictStore is the narrow slice of the task store this consumer drives — kept small so the
// shared eventwiring registration can hand it exactly the verdict half and nothing else.
type VerdictStore interface {
	ApplyVerifiedTask(ctx context.Context, p ports.ApplyVerifiedTaskParams) (bool, error)
	BounceTaskForRework(ctx context.Context, p ports.BounceTaskParams) (bool, error)
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
