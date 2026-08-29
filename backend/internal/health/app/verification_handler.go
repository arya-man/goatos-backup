package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Health treatment-evidence consumer. Health verification is post-task review, never a completion
// gate (the treatment was already given), so the verdicts map to:
//
//	verification.verdict.approved -> stamp verified_by/verified_at on the completed session
//	verification.verdict.rework   -> flip the session to 'rework' so the operator re-does and
//	                                 re-films it; medicine administrations stay as history
//
// It filters strictly on source.module + source.ref_type so a vaccination/feed/counts verdict is
// ignored. Registered ONLY through eventwiring.RegisterVerificationAppliers, the one shared list
// the API bus, cmd/outbox-relay, and cmd/domain-event-consumer all call.
const (
	eventHealthVerdictApproved = "verification.verdict.approved"
	eventHealthVerdictRework   = "verification.verdict.rework"
)

// healthVerdictPayload is the subset of the verification verdict payload this consumer reads.
type healthVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// TreatmentVerdictStore is the health repository slice this handler drives.
type TreatmentVerdictStore interface {
	ApplyVerifiedTreatment(ctx context.Context, tenantID, sessionID, verifiedBy string, verifiedAt time.Time) error
	BounceTreatmentForRework(ctx context.Context, tenantID, sessionID, verifiedBy, reason string) error
}

// HealthVerificationHandler applies a verifier's verdict to the treatment session it reviewed.
type HealthVerificationHandler struct {
	store TreatmentVerdictStore
	now   func() time.Time
	log   *slog.Logger
}

// NewHealthVerificationHandler constructs the consumer over the health repository.
func NewHealthVerificationHandler(store TreatmentVerdictStore, log *slog.Logger) *HealthVerificationHandler {
	if log == nil {
		log = slog.Default()
	}
	return &HealthVerificationHandler{store: store, now: time.Now, log: log}
}

var _ eventbus.Handler = (*HealthVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *HealthVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventHealthVerdictApproved, h)
	bus.Subscribe(eventHealthVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a treatment-session item to the matching write.
func (h *HealthVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventHealthVerdictApproved && e.Type != eventHealthVerdictRework {
		return nil
	}
	var p healthVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != domain.VerificationModuleHealth || p.Source.RefType != domain.VerificationRefTypeTreatmentSession {
		return nil
	}
	sessionID := strings.TrimSpace(p.Source.RefID)
	if sessionID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	switch e.Type {
	case eventHealthVerdictApproved:
		occurredAt := e.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = h.now().UTC()
		}
		return h.store.ApplyVerifiedTreatment(ctx, e.TenantID, sessionID, strings.TrimSpace(p.VerifiedBy), occurredAt)
	case eventHealthVerdictRework:
		return h.store.BounceTreatmentForRework(ctx, e.TenantID, sessionID, strings.TrimSpace(p.VerifiedBy), strings.TrimSpace(p.Reason))
	}
	return nil
}
