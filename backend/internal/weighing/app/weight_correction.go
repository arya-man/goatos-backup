package app

import (
	"context"
	"errors"
	"log/slog"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// VERIFIER WEIGHT CORRECTION -- the service.
//
// It is its own small service rather than a method on Service for the same reason
// ports.WeightCorrectionStore is its own interface: the correction is the
// VERIFIER'S act on one observation, and the thing that serves it must not be
// handed the planner and execution writes to reach. Service belongs to the
// operator's and leadership's surfaces; this belongs to the verifier's.
type WeightCorrectionService struct {
	store     ports.WeightCorrectionStore
	relabeler VerificationRelabeler
	log       *slog.Logger
}

// VerificationRelabeler pushes the recomposed subject label back onto the
// verification item raised for this observation.
//
// It is a separate seam from the enqueue/withdraw/ack trio above for the same
// reason those are separate from each other: an existing fake that only raises or
// retires items keeps satisfying its own interface unchanged.
type VerificationRelabeler interface {
	RelabelWeighingVerification(ctx context.Context, tenantID, refType, observationID, subjectLabel string) error
}

func NewWeightCorrectionService(store ports.WeightCorrectionStore, log *slog.Logger) *WeightCorrectionService {
	return &WeightCorrectionService{store: store, log: log}
}

// WithVerificationRelabeler wires the seam that keeps the verifier's queue row in
// step with the corrected weight.
//
// Optional on purpose, exactly like the verdict handler's apply-acker: without it
// the correction still lands on the observation and every weighing read model shows
// the new number. Only the verification item's own label lags, which is a display
// defect on one surface -- never a reason to refuse a correction the verifier is
// entitled to make.
func (s *WeightCorrectionService) WithVerificationRelabeler(relabeler VerificationRelabeler) *WeightCorrectionService {
	s.relabeler = relabeler
	return s
}

// InvalidCorrection carries the domain validator's stable refusal code so the HTTP
// adapter can answer with the specific reason ("weight_out_of_range") instead of a
// flat "request is invalid" that tells the verifier nothing about which field to
// fix. It stays errors.Is-comparable to ports.ErrInvalidArgument so every existing
// handler branch keeps matching.
type InvalidCorrection struct {
	Code string
}

func (e *InvalidCorrection) Error() string {
	return "weighing: invalid weight correction: " + e.Code
}

func (e *InvalidCorrection) Unwrap() error { return ports.ErrInvalidArgument }

// CorrectObservationWeight replaces the weight the verifier judged wrong.
//
// Validation happens HERE as well as in the store, deliberately: the store is the
// last line and refuses with a bare ErrInvalidArgument, while this layer is the one
// that can hand the client the field-level code it needs to render a useful message.
// The two share domain.ValidateWeightCorrection, so they cannot disagree about what
// a valid correction is.
func (s *WeightCorrectionService) CorrectObservationWeight(ctx context.Context, cmd domain.WeightCorrectionCommand) (domain.WeightCorrectionResult, error) {
	if s == nil || s.store == nil {
		return domain.WeightCorrectionResult{}, ports.ErrInvalidArgument
	}
	cmd, code := domain.ValidateWeightCorrection(cmd)
	if code != "" {
		return domain.WeightCorrectionResult{}, &InvalidCorrection{Code: code}
	}

	result, err := s.store.CorrectObservationWeight(ctx, cmd)
	if err != nil {
		return domain.WeightCorrectionResult{}, err
	}

	s.relabel(ctx, cmd, result)
	return result, nil
}

// relabel restates the verification item's subject label with the corrected weight.
//
// Deliberately AFTER the correction transaction committed and deliberately NOT
// fatal, for the same reason the verdict handler's ack is: the correction is a write
// that already succeeded, and failing the request here would tell the verifier her
// correction did not land while the farm's records say it did. The honest failure is
// a stale LABEL on one queue row -- visibly behind rather than invisibly wrong -- and
// the next correction of the same row restates it.
func (s *WeightCorrectionService) relabel(ctx context.Context, cmd domain.WeightCorrectionCommand, result domain.WeightCorrectionResult) {
	if s.relabeler == nil || result.SubjectLabel == "" {
		return
	}
	if err := s.relabeler.RelabelWeighingVerification(ctx, cmd.TenantID, cmd.RefType, cmd.ObservationID, result.SubjectLabel); err != nil && s.log != nil {
		s.log.WarnContext(ctx, "weighing_weight_correction_relabel_failed",
			"tenant_id", cmd.TenantID,
			"observation_id", cmd.ObservationID,
			"ref_type", cmd.RefType,
			"error", err,
		)
	}
}

// CorrectionCode extracts the field-level refusal code from an error returned by
// CorrectObservationWeight, for the HTTP adapter to render. It returns "" for any
// error that is not a validation refusal, so a caller can branch on it without
// type-asserting at the call site.
func CorrectionCode(err error) string {
	var invalid *InvalidCorrection
	if errors.As(err, &invalid) {
		return invalid.Code
	}
	return ""
}
