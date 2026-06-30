package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

const defaultProjectionEventGeneratedBy = "counts-projection-event-handler"

type ProjectionRecomputer interface {
	RecomputeProjectionSnapshotWithResult(context.Context, domain.ProjectionRecomputeRequest) (domain.ProjectionRecomputeResult, error)
}

type ProjectionInputHandler struct {
	recomputer            ProjectionRecomputer
	now                   func() time.Time
	sourceContractVersion string
	generatedBy           string
}

type ProjectionInputHandlerOption func(*ProjectionInputHandler)

func WithProjectionInputHandlerNow(now func() time.Time) ProjectionInputHandlerOption {
	return func(h *ProjectionInputHandler) {
		if now != nil {
			h.now = now
		}
	}
}

func WithProjectionInputHandlerSourceContractVersion(version string) ProjectionInputHandlerOption {
	return func(h *ProjectionInputHandler) {
		if strings.TrimSpace(version) != "" {
			h.sourceContractVersion = strings.TrimSpace(version)
		}
	}
}

func WithProjectionInputHandlerGeneratedBy(generatedBy string) ProjectionInputHandlerOption {
	return func(h *ProjectionInputHandler) {
		if strings.TrimSpace(generatedBy) != "" {
			h.generatedBy = strings.TrimSpace(generatedBy)
		}
	}
}

func NewProjectionInputHandler(recomputer ProjectionRecomputer, opts ...ProjectionInputHandlerOption) *ProjectionInputHandler {
	h := &ProjectionInputHandler{
		recomputer:            recomputer,
		now:                   func() time.Time { return time.Now().UTC() },
		sourceContractVersion: domain.SourceContractVersionV1,
		generatedBy:           defaultProjectionEventGeneratedBy,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(h)
		}
	}
	return h
}

var _ eventbus.Handler = (*ProjectionInputHandler)(nil)

func (h *ProjectionInputHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(domain.EventBaseCountAnchorRecorded, h)
	bus.Subscribe(domain.EventShiftingEventRecorded, h)
}

type projectionInputPayload struct {
	InputKind             string   `json:"input_kind"`
	ParkID                string   `json:"park_id"`
	SourceParkID          string   `json:"source_park_id"`
	DestinationParkID     string   `json:"destination_park_id"`
	CountedAt             string   `json:"counted_at"`
	EffectiveAt           string   `json:"effective_at"`
	TargetDate            string   `json:"target_date"`
	SourceContractVersion string   `json:"source_contract_version"`
	RecomputeHorizons     []string `json:"recompute_horizons"`
}

func (h *ProjectionInputHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if h == nil || h.recomputer == nil {
		return fmt.Errorf("counts: projection input handler is not configured")
	}
	payload, err := decodeProjectionInputPayload(e.Payload)
	if err != nil {
		return err
	}
	tenantID := strings.TrimSpace(e.TenantID)
	if tenantID == "" {
		return fmt.Errorf("counts: projection input event %s missing tenant_id", e.ID)
	}
	eventTime, err := projectionInputEventTime(payload, e, h.now)
	if err != nil {
		return err
	}
	targetDate, err := projectionInputTargetDate(payload, eventTime)
	if err != nil {
		return err
	}
	horizons, err := projectionInputHorizons(payload.RecomputeHorizons)
	if err != nil {
		return err
	}
	parkIDs := projectionInputParkIDs(payload)
	if len(parkIDs) == 0 {
		return fmt.Errorf("counts: projection input event %s missing park scope", e.ID)
	}
	sourceContractVersion := strings.TrimSpace(payload.SourceContractVersion)
	if sourceContractVersion == "" {
		sourceContractVersion = h.sourceContractVersion
	}
	traceID := strings.TrimSpace(e.ID)
	var trace *string
	if traceID != "" {
		trace = &traceID
	}
	for _, parkID := range parkIDs {
		for _, horizon := range horizons {
			_, err := h.recomputer.RecomputeProjectionSnapshotWithResult(ctx, domain.ProjectionRecomputeRequest{
				TenantID:              tenantID,
				ParkID:                parkID,
				Horizon:               horizon,
				TargetDate:            targetDate,
				AsOf:                  eventTime,
				SourceContractVersion: sourceContractVersion,
				GeneratedBy:           h.generatedBy,
				TraceID:               trace,
			})
			if err != nil {
				return fmt.Errorf("counts: projection recompute from %s for park %s horizon %s: %w", e.Type, parkID, horizon, err)
			}
		}
	}
	return nil
}

func decodeProjectionInputPayload(raw []byte) (projectionInputPayload, error) {
	if len(raw) == 0 {
		return projectionInputPayload{}, nil
	}
	var payload projectionInputPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return projectionInputPayload{}, fmt.Errorf("counts: decode projection input event: %w", err)
	}
	return payload, nil
}

func projectionInputEventTime(payload projectionInputPayload, e eventbus.Event, now func() time.Time) (time.Time, error) {
	raw := firstNonEmptyString(payload.EffectiveAt, payload.CountedAt)
	if raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return time.Time{}, fmt.Errorf("counts: projection input event time must be RFC3339: %w", err)
		}
		return parsed.UTC(), nil
	}
	if !e.OccurredAt.IsZero() {
		return e.OccurredAt.UTC(), nil
	}
	if now == nil {
		now = time.Now
	}
	return now().UTC(), nil
}

func projectionInputTargetDate(payload projectionInputPayload, eventTime time.Time) (time.Time, error) {
	if strings.TrimSpace(payload.TargetDate) == "" {
		return dateOnly(eventTime), nil
	}
	parsed, err := parseProjectionInputDate(strings.TrimSpace(payload.TargetDate))
	if err != nil {
		return time.Time{}, err
	}
	return dateOnly(parsed), nil
}

func parseProjectionInputDate(raw string) (time.Time, error) {
	if parsed, err := time.Parse("2006-01-02", raw); err == nil {
		return parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("counts: projection target_date must be YYYY-MM-DD or RFC3339: %w", err)
	}
	return parsed.UTC(), nil
}

func projectionInputHorizons(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{"count_as_of", "feed_target_date"}, nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, horizon := range raw {
		horizon = strings.TrimSpace(horizon)
		if horizon != "count_as_of" && horizon != "feed_target_date" {
			return nil, ErrInvalidHorizon
		}
		if seen[horizon] {
			continue
		}
		seen[horizon] = true
		out = append(out, horizon)
	}
	if len(out) == 0 {
		return nil, ErrInvalidHorizon
	}
	return out, nil
}

func projectionInputParkIDs(payload projectionInputPayload) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, parkID := range []string{payload.ParkID, payload.SourceParkID, payload.DestinationParkID} {
		parkID = strings.TrimSpace(parkID)
		if parkID == "" || seen[parkID] {
			continue
		}
		seen[parkID] = true
		out = append(out, parkID)
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
