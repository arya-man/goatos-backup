package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// This file wires the writes that must dirty a shed for the bounded incremental vaccination-shed
// projector (P0-B, context/execution/api-projection-performance-handoff-2026-07-13.md) into domain
// events, so cmd/domain-event-consumer enqueues the affected shed(s) without the request path (or
// any obligation write path) taking a direct dependency on vaccinationexecution. Event type
// constants are mirrored from their producing modules (identity's goat.location.changed/
// goat.shifted/goat.exited, vaccination's vaccination.completed) rather than imported, to avoid a
// cross-module import cycle -- vaccinationexecution is a downstream read-model consumer of these
// events, never their owner.
//
// Wired here: GoatShifted (old + new shed), GoatExited (current shed), VaccinationCompleted (the
// completed obligation's goat's shed). NOT wired (documented, not silently skipped): obligation
// status-event writes that are not goat-move/exit/completion (e.g. a direct SM-2 reschedule/defer
// that does not change scope, or a manual capacity/protocol-config change) do not yet enqueue a
// dirty shed here -- until then, those sheds are only kept fresh by the time-driven
// EnqueueDueTransitions pass, which bounds staleness to the TTL rather than reacting instantly.
const (
	eventGoatLocationChanged  = "goat.location.changed"
	eventGoatShifted          = "goat.shifted"
	eventGoatExited           = "goat.exited"
	eventVaccinationCompleted = "vaccination.completed"
)

// DirtyShedEnqueuer enqueues sheds into the durable dirty-scope queue (dirty_scopes.go). Satisfied
// by vaccinationexecution/adapters/postgres.Repository.
type DirtyShedEnqueuer interface {
	EnqueueDirtySheds(ctx context.Context, tenantID string, shedIDs []string, reason string) error
}

// ObligationShedResolver resolves the shed of a goat-target obligation's target goat. Satisfied by
// vaccinationexecution/adapters/postgres.Repository.ShedIDForObligationGoat.
type ObligationShedResolver interface {
	ShedIDForObligationGoat(ctx context.Context, tenantID, obligationID string) (string, bool, error)
}

// GoatShiftedDirtyShedHandler enqueues BOTH the OLD and NEW shed of a moved goat, so the shed-wise
// vaccination projection recomputes the shed the goat left (its counts drop) and the shed it
// entered (its counts rise) -- P0-B invalidation coverage requires both scopes on a move, not just
// the destination.
type GoatShiftedDirtyShedHandler struct {
	enqueuer DirtyShedEnqueuer
}

// NewGoatShiftedDirtyShedHandler constructs the handler.
func NewGoatShiftedDirtyShedHandler(enqueuer DirtyShedEnqueuer) *GoatShiftedDirtyShedHandler {
	return &GoatShiftedDirtyShedHandler{enqueuer: enqueuer}
}

var _ eventbus.Handler = (*GoatShiftedDirtyShedHandler)(nil)

// Register subscribes to both the canonical goat.location.changed event and the legacy
// goat.shifted alias, mirroring obligation's GoatShiftedHandler.
func (h *GoatShiftedDirtyShedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventGoatLocationChanged, h)
	bus.Subscribe(eventGoatShifted, h)
}

type goatShiftedShedPayload struct {
	FromShedID string `json:"from_shed_id"`
	ToShedID   string `json:"to_shed_id"`
}

// HandleEvent enqueues the old + new shed ids carried in the goat move payload
// (backend/internal/identity/adapters/postgres/goat_lifecycle.go MoveGoat payload). Idempotent:
// re-enqueuing an already-queued shed is a coalescing no-op (dirty_scopes.go).
func (h *GoatShiftedDirtyShedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.enqueuer == nil {
		return nil
	}
	var p goatShiftedShedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	sheds := make([]string, 0, 2)
	if id := strings.TrimSpace(p.FromShedID); id != "" {
		sheds = append(sheds, id)
	}
	if id := strings.TrimSpace(p.ToShedID); id != "" {
		sheds = append(sheds, id)
	}
	if len(sheds) == 0 {
		return nil
	}
	return h.enqueuer.EnqueueDirtySheds(ctx, e.TenantID, sheds, "goat_shifted")
}

// GoatExitedDirtyShedHandler enqueues the exited goat's shed (its animal count drops).
type GoatExitedDirtyShedHandler struct {
	enqueuer DirtyShedEnqueuer
}

// NewGoatExitedDirtyShedHandler constructs the handler.
func NewGoatExitedDirtyShedHandler(enqueuer DirtyShedEnqueuer) *GoatExitedDirtyShedHandler {
	return &GoatExitedDirtyShedHandler{enqueuer: enqueuer}
}

var _ eventbus.Handler = (*GoatExitedDirtyShedHandler)(nil)

// Register subscribes to goat.exited.
func (h *GoatExitedDirtyShedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventGoatExited, h)
}

type goatExitedShedPayload struct {
	CurrentShedID string `json:"current_shed_id"`
}

// HandleEvent enqueues the exited goat's shed from the exit payload
// (backend/internal/identity/adapters/postgres/goat_lifecycle.go ExitGoat payload).
func (h *GoatExitedDirtyShedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.enqueuer == nil {
		return nil
	}
	var p goatExitedShedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	shedID := strings.TrimSpace(p.CurrentShedID)
	if shedID == "" {
		return nil
	}
	return h.enqueuer.EnqueueDirtySheds(ctx, e.TenantID, []string{shedID}, "goat_exited")
}

// VaccinationCompletedDirtyShedHandler enqueues the shed of a completed obligation's target goat,
// so an accepted vaccination completion recomputes that shed's due/done counts promptly instead of
// waiting for the bounded time-driven pass to catch up.
type VaccinationCompletedDirtyShedHandler struct {
	enqueuer DirtyShedEnqueuer
	resolver ObligationShedResolver
}

// NewVaccinationCompletedDirtyShedHandler constructs the handler.
func NewVaccinationCompletedDirtyShedHandler(enqueuer DirtyShedEnqueuer, resolver ObligationShedResolver) *VaccinationCompletedDirtyShedHandler {
	return &VaccinationCompletedDirtyShedHandler{enqueuer: enqueuer, resolver: resolver}
}

var _ eventbus.Handler = (*VaccinationCompletedDirtyShedHandler)(nil)

// Register subscribes to vaccination.completed.
func (h *VaccinationCompletedDirtyShedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVaccinationCompleted, h)
}

type vaccinationCompletedShedPayload struct {
	TenantID     string `json:"tenant_id,omitempty"`
	ObligationID string `json:"obligation_id"`
	Status       string `json:"status,omitempty"`
}

// HandleEvent resolves the completed obligation's target goat's CURRENT shed (a single indexed
// lookup, not a fanout loop -- one per completion event) and enqueues it. Payload shape mirrors
// vaccinationapp.VaccinationCompletedEvent (this package cannot import that one without an import
// cycle, so the fields are duplicated here).
func (h *VaccinationCompletedDirtyShedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.enqueuer == nil || h.resolver == nil {
		return nil
	}
	var p vaccinationCompletedShedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	tenantID := e.TenantID
	if tenantID == "" {
		tenantID = p.TenantID
	}
	obligationID := p.ObligationID
	if obligationID == "" {
		obligationID = e.Key
	}
	if tenantID == "" || obligationID == "" {
		return nil
	}
	if p.Status != "" && p.Status != "completed" {
		return nil
	}
	shedID, found, err := h.resolver.ShedIDForObligationGoat(ctx, tenantID, obligationID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	return h.enqueuer.EnqueueDirtySheds(ctx, tenantID, []string{shedID}, "vaccination_completed")
}
