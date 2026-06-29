package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// EventGoatExited is the event type (death/sale/transfer) that triggers SM-3 cancellation.
const EventGoatExited = "goat.exited"

// GoatExitedHandler runs SM-3: on goat.exited it cancels the goat's open obligations. Idempotent
// (safe under at-least-once delivery). eventbus.Handler → same code for in-process and Pub/Sub.
type GoatExitedHandler struct {
	repo ports.Repository
}

type eventTimeCancelRepository interface {
	CancelOpenForGoatAt(ctx context.Context, tenantID, goatID, reason string, occurredAt time.Time, eventID string) (int, error)
}

// NewGoatExitedHandler constructs the handler.
func NewGoatExitedHandler(repo ports.Repository) *GoatExitedHandler {
	return &GoatExitedHandler{repo: repo}
}

var _ eventbus.Handler = (*GoatExitedHandler)(nil)

// Register subscribes the handler to goat.exited on a bus.
func (h *GoatExitedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventGoatExited, h)
}

// HandleEvent cancels open obligations for the exited goat (event Key = goat_id).
func (h *GoatExitedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if repo, ok := h.repo.(eventTimeCancelRepository); ok {
		occurredAt := e.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = time.Now().UTC()
		}
		_, err := repo.CancelOpenForGoatAt(ctx, e.TenantID, e.Key, "ineligible_after_exit", occurredAt, strings.TrimSpace(e.ID))
		return err
	}
	_, err := h.repo.CancelOpenForGoat(ctx, e.TenantID, e.Key, "ineligible_after_exit")
	return err
}
