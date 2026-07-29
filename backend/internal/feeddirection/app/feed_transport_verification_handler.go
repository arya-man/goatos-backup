package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

type FeedTransportVerificationHandler struct {
	store ports.TransportStore
	log   *slog.Logger
}

func NewFeedTransportVerificationHandler(store ports.TransportStore, log *slog.Logger) *FeedTransportVerificationHandler {
	return &FeedTransportVerificationHandler{store: store, log: log}
}
func (h *FeedTransportVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}
func (h *FeedTransportVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p feedDistributionVerdictPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return err
	}
	if p.Source.Module != domain.VerificationModuleFeed || p.Source.RefType != domain.VerificationRefTypeTransport {
		return nil
	}
	id := strings.TrimSpace(p.Source.RefID)
	if id == "" {
		return nil
	}
	if e.Type == eventVerificationVerdictApproved {
		_, err := h.store.ApplyVerifiedTransport(ctx, ports.ApplyTransportParams{TenantID: e.TenantID, AttemptID: id, VerifiedBy: strings.TrimSpace(p.VerifiedBy), TraceID: e.ID})
		return err
	}
	if e.Type == eventVerificationVerdictRework {
		_, err := h.store.BounceTransportForRework(ctx, ports.BounceTransportParams{TenantID: e.TenantID, AttemptID: id, Reason: strings.TrimSpace(p.Reason), VerifiedBy: strings.TrimSpace(p.VerifiedBy), TraceID: e.ID})
		return err
	}
	return nil
}
