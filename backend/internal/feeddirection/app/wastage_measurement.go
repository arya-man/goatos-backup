package app

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"

	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// FEED WASTAGE MEASUREMENT — the verifier's service (maintainer decision 2026-08-18).
//
// Its own small service rather than a method on Service, on the same terms as weighing's
// WeightCorrectionService: recording the measured leftover is the VERIFIER'S act on one completion,
// and the thing that serves it must not be handed the generation and completion writes to reach.
// Service belongs to the operator's surfaces; this belongs to the verifier's.
type WastageMeasurementService struct {
	store     ports.WastageCompletionStore
	relabeler WastageVerificationRelabeler
	log       *slog.Logger
}

// WastageVerificationRelabeler pushes the recomposed subject label back onto the verification item
// raised for this completion, so the queue row shows the value she just recorded.
type WastageVerificationRelabeler interface {
	RelabelFeedWastageVerification(ctx context.Context, tenantID, completionID, subjectLabel string) error
}

func NewWastageMeasurementService(store ports.WastageCompletionStore, log *slog.Logger) *WastageMeasurementService {
	return &WastageMeasurementService{store: store, log: log}
}

// WithVerificationRelabeler wires the seam that keeps the verifier's queue row in step with the
// recorded value. Optional on purpose: without it the measurement still lands on the completion and
// every read shows it; only the queue row's label lags — a display defect on one surface, never a
// reason to refuse a measurement the verifier is entitled to record.
func (s *WastageMeasurementService) WithVerificationRelabeler(relabeler WastageVerificationRelabeler) *WastageMeasurementService {
	s.relabeler = relabeler
	return s
}

// WastageMeasurementCommand is one verifier measurement of one completion.
type WastageMeasurementCommand struct {
	TenantID     string
	CompletionID string
	// WastageKg is the leftover weight she read off the video. ZERO IS VALID — an empty trough is
	// a real, good measurement.
	WastageKg      float64
	RecordedBy     string
	IdempotencyKey string
	TraceID        string
}

// InvalidWastageMeasurement carries a stable refusal code so the HTTP adapter can answer with the
// specific reason instead of a flat "request is invalid". errors.Is-comparable to
// ports.ErrWastageValueOutOfRange for existing handler branches.
type InvalidWastageMeasurement struct {
	Code string
}

func (e *InvalidWastageMeasurement) Error() string {
	return "feeddirection: invalid wastage measurement: " + e.Code
}

func (e *InvalidWastageMeasurement) Unwrap() error { return ports.ErrWastageValueOutOfRange }

// maxWastageMeasurementKg mirrors the store's typo ceiling so the two layers cannot disagree about
// what a valid measurement is. See wastage_completions.go maxRecordableWastageKg.
const maxWastageMeasurementKg = 10000

// RecordWastageMeasurement stores the verifier's measured leftover weight and restates the queue
// row's subject label with it.
func (s *WastageMeasurementService) RecordWastageMeasurement(ctx context.Context, cmd WastageMeasurementCommand) (ports.RecordWastageMeasurementResult, error) {
	if s == nil || s.store == nil {
		return ports.RecordWastageMeasurementResult{}, ports.ErrWastageCompletionNotFound
	}
	cmd.TenantID = strings.TrimSpace(cmd.TenantID)
	cmd.CompletionID = strings.TrimSpace(cmd.CompletionID)
	cmd.RecordedBy = strings.TrimSpace(cmd.RecordedBy)
	cmd.IdempotencyKey = strings.TrimSpace(cmd.IdempotencyKey)
	switch {
	case cmd.TenantID == "" || cmd.CompletionID == "":
		return ports.RecordWastageMeasurementResult{}, &InvalidWastageMeasurement{Code: "missing_completion"}
	case cmd.RecordedBy == "":
		return ports.RecordWastageMeasurementResult{}, &InvalidWastageMeasurement{Code: "missing_verifier"}
	case cmd.IdempotencyKey == "":
		return ports.RecordWastageMeasurementResult{}, &InvalidWastageMeasurement{Code: "missing_idempotency_key"}
	case math.IsNaN(cmd.WastageKg) || math.IsInf(cmd.WastageKg, 0) || cmd.WastageKg < 0 || cmd.WastageKg > maxWastageMeasurementKg:
		return ports.RecordWastageMeasurementResult{}, &InvalidWastageMeasurement{Code: "wastage_out_of_range"}
	}

	result, err := s.store.RecordWastageMeasurement(ctx, ports.RecordWastageMeasurementParams{
		TenantID:       cmd.TenantID,
		CompletionID:   cmd.CompletionID,
		WastageKg:      cmd.WastageKg,
		RecordedBy:     cmd.RecordedBy,
		IdempotencyKey: cmd.IdempotencyKey,
		TraceID:        cmd.TraceID,
	})
	if err != nil {
		return ports.RecordWastageMeasurementResult{}, err
	}

	// Relabel AFTER the measurement transaction committed, and NOT fatally: the write already
	// succeeded, and failing the request here would tell the verifier her measurement did not land
	// while the farm's records say it did. The honest failure is a stale LABEL on one queue row.
	if s.relabeler != nil && result.SubjectLabel != "" {
		if relErr := s.relabeler.RelabelFeedWastageVerification(ctx, cmd.TenantID, cmd.CompletionID, result.SubjectLabel); relErr != nil && s.log != nil {
			s.log.WarnContext(ctx, "feed_wastage_measurement_relabel_failed",
				"tenant_id", cmd.TenantID,
				"completion_id", cmd.CompletionID,
				"error", relErr,
			)
		}
	}
	return result, nil
}

// WastageMeasurementCode extracts the field-level refusal code from an error returned by
// RecordWastageMeasurement, for the HTTP adapter to render. "" for any error that is not a
// validation refusal.
func WastageMeasurementCode(err error) string {
	var invalid *InvalidWastageMeasurement
	if errors.As(err, &invalid) {
		return invalid.Code
	}
	return ""
}
