package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Domain event types that drive the vaccination operator-config auto-cascade. Registered in
// context/architecture/domain-event-registry.json.
const (
	EventVaccinationCapacityChanged = "vaccination.capacity.changed"
	EventVaccinationRosterChanged   = "vaccination.roster.changed"
	EventVaccinationLeaveChanged    = "vaccination.leave.changed"
)

// OperatorConfigChangePayload is the shared body for all three operator-config-cascade events: the
// park whose future planned vaccination drives must be recomputed against the new config.
// EffectiveFrom, when present, is a YYYY-MM-DD business date; an empty value means "today" at
// HandleEvent time (Asia/Kolkata).
type OperatorConfigChangePayload struct {
	ParkID        string `json:"park_id"`
	ScopeType     string `json:"scope_type,omitempty"`
	ScopeID       string `json:"scope_id,omitempty"`
	EffectiveFrom string `json:"effective_from,omitempty"`
}

// operatorConfigReplanRepository is the narrow seam this handler needs: the existing, tested release
// path (RecomputeFutureVaccinationDrives) plus the watermark claim that makes replay a no-op. Reusing
// RecomputeFutureVaccinationDrives means this handler never duplicates its release SQL or its
// per-tenant advisory lock (same granularity as the sweeper -- see operator_recompute.go).
type operatorConfigReplanRepository interface {
	ClaimOperatorConfigReplanWatermark(ctx context.Context, tenantID, parkID, eventType, eventID string) (bool, error)
	RecomputeFutureVaccinationDrives(ctx context.Context, tenantID, parkID string, effectiveFrom time.Time) (int, error)
}

// shedParkResolver resolves a shed location id to its parent park location id. Only
// vaccination.leave.changed carries a shed-scoped payload today (workforce leave scope is
// tenant/center/shed, never park directly); the other two event types already carry park_id.
type shedParkResolver interface {
	ParkIDForShed(ctx context.Context, tenantID, shedID string) (string, error)
}

// OperatorConfigReplanHandler re-plans a park's future vaccination drives whenever its operator
// capacity/N, default-operator/roster, or operator leave changes -- WITHOUT a manual CLI run. It only
// releases future planned batches back to unbatched (via the reused RecomputeFutureVaccinationDrives);
// the next sweeper tick re-plans the released obligations under the current config through the
// existing clinical-guarded path. Idempotent: a replayed event with the same event id is a no-op after
// the first successful claim (at-least-once delivery safe).
type OperatorConfigReplanHandler struct {
	repo operatorConfigReplanRepository
}

// NewOperatorConfigReplanHandler constructs the handler. repo must implement
// operatorConfigReplanRepository (the concrete obligation postgres.Repository does).
func NewOperatorConfigReplanHandler(repo ports.Repository) *OperatorConfigReplanHandler {
	ordered, _ := repo.(operatorConfigReplanRepository)
	return &OperatorConfigReplanHandler{repo: ordered}
}

var _ eventbus.Handler = (*OperatorConfigReplanHandler)(nil)

// Register subscribes the handler to the three operator-config-cascade event types.
func (h *OperatorConfigReplanHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVaccinationCapacityChanged, h)
	bus.Subscribe(EventVaccinationRosterChanged, h)
	bus.Subscribe(EventVaccinationLeaveChanged, h)
}

// HandleEvent claims the (tenant, event_id) watermark, then re-plans the affected park's future
// vaccination drives from effectiveFrom forward. Key = park_id (the aggregate this event describes).
func (h *OperatorConfigReplanHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h.repo == nil {
		return fmt.Errorf("obligation: operator config replan handler: repository does not support the required methods")
	}

	var p OperatorConfigChangePayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return fmt.Errorf("obligation: decode operator config change payload: %w", err)
		}
	}
	parkID := strings.TrimSpace(p.ParkID)
	shedScoped := strings.TrimSpace(p.ScopeType) == "shed" && strings.TrimSpace(p.ScopeID) != ""
	if parkID == "" && shedScoped {
		resolver, ok := h.repo.(shedParkResolver)
		if !ok {
			return fmt.Errorf("obligation: operator config change event %s: repository cannot resolve shed to park", e.Type)
		}
		resolved, err := resolver.ParkIDForShed(ctx, e.TenantID, strings.TrimSpace(p.ScopeID))
		if err != nil {
			return fmt.Errorf("obligation: resolve shed %s to park: %w", p.ScopeID, err)
		}
		parkID = resolved
	}
	if parkID == "" && !shedScoped {
		// e.Key is only a park id for the park-scoped event types (capacity/roster); a shed-scoped
		// leave event's Key is the shed id and must never be used as a park id fallback.
		parkID = strings.TrimSpace(e.Key)
	}
	if parkID == "" {
		return fmt.Errorf("obligation: operator config change event %s missing park id", e.Type)
	}

	eventID := strings.TrimSpace(e.ID)
	if eventID == "" {
		return fmt.Errorf("obligation: operator config change event missing stable event id (idempotency requires one)")
	}

	applied, err := h.repo.ClaimOperatorConfigReplanWatermark(ctx, e.TenantID, parkID, e.Type, eventID)
	if err != nil {
		return err
	}
	if !applied {
		// Exact replay of an already-processed event: no-op, matching the watermark-claim contract.
		return nil
	}

	effectiveFrom, err := resolveEffectiveFrom(p.EffectiveFrom, e.OccurredAt)
	if err != nil {
		return err
	}

	_, err = h.repo.RecomputeFutureVaccinationDrives(ctx, e.TenantID, parkID, effectiveFrom)
	return err
}

// resolveEffectiveFrom parses an authored YYYY-MM-DD business date, or falls back to "today" (Asia/
// Kolkata business day) derived from the event's occurred-at instant (or now, if that's zero).
func resolveEffectiveFrom(effectiveFrom string, occurredAt time.Time) (time.Time, error) {
	effectiveFrom = strings.TrimSpace(effectiveFrom)
	if effectiveFrom == "" {
		anchor := occurredAt
		if anchor.IsZero() {
			anchor = time.Now().UTC()
		}
		return biztime.BusinessDayStart(anchor), nil
	}
	parsed, err := time.Parse("2006-01-02", effectiveFrom)
	if err != nil {
		return time.Time{}, fmt.Errorf("obligation: operator config change effective_from: %w", err)
	}
	return biztime.BusinessDayStart(parsed), nil
}
