package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

const correctCensusSliceCommand = "correctCensusSlice"

const (
	previewCorrectCensusSliceRoute = "/admin/goats/census-slice/preview"
	commitCorrectCensusSliceRoute  = "/admin/goats/census-slice/commit"
)

// CorrectCensusSliceInput carries the transport envelope. The body is decoded here, not in the
// handler, so unknown fields are rejected against the same struct the request hash is taken of.
type CorrectCensusSliceInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

// PreviewCorrectCensusSlice reports how many animals the correction would touch. It writes nothing
// and takes no idempotency key: a preview is safe to repeat, and requiring a key would make the
// dialog's own reopen burn keys the commit could not then reuse.
func (s *Service) PreviewCorrectCensusSlice(ctx context.Context, input CorrectCensusSliceInput) (*domain.CensusSliceCorrectionPreviewResponse, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return nil, BadRequest("missing_tenant", "tenant context is required")
	}
	body, err := decodeCorrectCensusSlice(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateCorrectCensusSlice(body, false); err != nil {
		return nil, err
	}

	preview, err := s.repo.PreviewCorrectCensusSlice(ctx, ports.CorrectCensusSliceCommand{
		TenantID:        tenantID,
		TraceID:         input.TraceID,
		ShedID:          body.ShedID,
		PartitionLabel:  partitionParam(body.PartitionLabel),
		ManagementStage: body.ManagementStage,
		Breed:           body.Breed,
		Sex:             body.Sex,
		Field:           body.Field,
		Value:           body.Value,
	})
	if err != nil {
		return nil, mapCensusCorrectionErr(err)
	}

	return &domain.CensusSliceCorrectionPreviewResponse{
		ShedID:                     preview.ShedID,
		ShedName:                   preview.ShedName,
		PartitionLabel:             preview.PartitionLabel,
		OperationalLocationDisplay: preview.OperationalLocationDisplay,
		Field:                      preview.Field,
		CurrentValue:               preview.CurrentValue,
		Value:                      preview.Value,
		TotalLive:                  preview.TotalLive,
		TraceID:                    input.TraceID,
	}, nil
}

// CommitCorrectCensusSlice applies the correction. The idempotency subject is the SLICE, so the
// same key used against a different row or a different target value is a hash conflict rather than
// a silent replay of the wrong write.
func (s *Service) CommitCorrectCensusSlice(ctx context.Context, input CorrectCensusSliceInput) (*domain.CensusSliceCorrectionResponse, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	actorID := strings.TrimSpace(input.ActorID)
	clientKey := strings.TrimSpace(input.IdempotencyKey)
	if tenantID == "" || actorID == "" {
		return nil, BadRequest("missing_tenant", "tenant and actor context are required")
	}
	if clientKey == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	raw := bytes.TrimSpace(input.RawBody)
	body, err := decodeCorrectCensusSlice(raw)
	if err != nil {
		return nil, err
	}
	if err := validateCorrectCensusSlice(body, true); err != nil {
		return nil, err
	}

	subject := fmt.Sprintf("%s:%s:%s:%s:%s:%s",
		body.ShedID, oploc.NormalizePartition(body.PartitionLabel),
		body.ManagementStage, body.Breed, body.Sex, body.Field)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, correctCensusSliceCommand, commitCorrectCensusSliceRoute, subject, raw)
	if err != nil {
		return nil, fmt.Errorf("correct census slice request hash failed: %w", err)
	}

	result, err := s.repo.CorrectCensusSlice(ctx, ports.CorrectCensusSliceCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, correctCensusSliceCommand, subject, clientKey),
		IdempotencyScope:     correctCensusSliceCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		ShedID:               body.ShedID,
		PartitionLabel:       partitionParam(body.PartitionLabel),
		ManagementStage:      body.ManagementStage,
		Breed:                body.Breed,
		Sex:                  body.Sex,
		Field:                body.Field,
		Value:                body.Value,
		Reason:               body.Reason,
		OccurredAt:           s.now().UTC(),
	})
	if err != nil {
		return nil, mapCensusCorrectionErr(err)
	}

	return &domain.CensusSliceCorrectionResponse{
		ShedID:                     result.ShedID,
		ShedName:                   result.ShedName,
		PartitionLabel:             result.PartitionLabel,
		OperationalLocationDisplay: result.OperationalLocationDisplay,
		Field:                      result.Field,
		CurrentValue:               result.CurrentValue,
		Value:                      result.Value,
		TotalLive:                  result.TotalLive,
		Corrected:                  result.Corrected,
		IdempotencyKey:             clientKey,
		TraceID:                    input.TraceID,
	}, nil
}

func decodeCorrectCensusSlice(raw []byte) (*domain.CorrectCensusSliceRequest, error) {
	var body domain.CorrectCensusSliceRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	return &body, nil
}

// validateCorrectCensusSlice normalizes and checks the request.
//
// The FIELD is checked against a closed set here rather than at the database, because it selects
// which statement runs: an unknown field must never reach the repository. The VALUE's vocabulary is
// checked at the repository, where the tenant's breeds live.
func validateCorrectCensusSlice(body *domain.CorrectCensusSliceRequest, requireReason bool) error {
	body.ShedID = strings.TrimSpace(body.ShedID)
	body.PartitionLabel = strings.TrimSpace(body.PartitionLabel)
	body.ManagementStage = strings.TrimSpace(body.ManagementStage)
	body.Breed = strings.TrimSpace(body.Breed)
	body.Sex = strings.TrimSpace(body.Sex)
	body.Field = strings.ToLower(strings.TrimSpace(body.Field))
	body.Value = strings.TrimSpace(body.Value)
	body.Reason = strings.TrimSpace(body.Reason)

	if body.ShedID == "" {
		return BadRequest("invalid_shed", "shed_id is required")
	}
	if len(body.PartitionLabel) > 80 || strings.ContainsAny(body.PartitionLabel, "\n\r\t") {
		return BadRequest("invalid_partition", "partition_label is not valid")
	}
	if body.ManagementStage == "" || len(body.ManagementStage) > 80 {
		return BadRequest("invalid_stage", "management_stage is required")
	}
	if len(body.Breed) > 120 {
		return BadRequest("invalid_breed", "breed is not valid")
	}
	// Sex identifies the ROW, so it must be one of the two the column allows -- a row the census
	// can render is a row with a real sex.
	if body.Sex != "female" && body.Sex != "male" {
		return BadRequest("invalid_sex", "sex must be female or male")
	}
	if body.Field != "breed" && body.Field != "sex" {
		return BadRequest("invalid_field", "field must be breed or sex")
	}
	if body.Value == "" || len(body.Value) > 120 {
		return BadRequest("invalid_value", "value is required")
	}
	if body.Field == "sex" && body.Value != "female" && body.Value != "male" {
		return BadRequest("invalid_value", "sex must be female or male")
	}
	// A correction that changes nothing is rejected rather than written: an audit row claiming a
	// correction that moved no animal is noise in the one place that has to stay readable.
	current := body.Breed
	if body.Field == "sex" {
		current = body.Sex
	}
	if strings.EqualFold(current, body.Value) {
		return BadRequest("no_change", "that is already this row's value")
	}
	if requireReason && (len(body.Reason) < 3 || len(body.Reason) > 500) {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	return nil
}

func mapCensusCorrectionErr(err error) error {
	switch {
	case errors.Is(err, ports.ErrCensusCorrectionEmptyScope):
		return Conflict("census_slice_empty", "no live animals match that row any more")
	case errors.Is(err, ports.ErrCensusCorrectionScopeTooLarge):
		return Conflict("census_slice_too_large", "that row holds more animals than one correction may change")
	case errors.Is(err, ports.ErrCensusCorrectionValue):
		return BadRequest("invalid_value", "that value is not in this tenant's vocabulary")
	case errors.Is(err, ports.ErrCensusCorrectionField):
		return BadRequest("invalid_field", "field must be breed or sex")
	case errors.Is(err, ports.ErrCensusCorrectionNoChange):
		return BadRequest("no_change", "that is already this row's value")
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return Conflict("idempotency_conflict", "that key was used for a different correction")
	case errors.Is(err, ports.ErrIdempotencyPending):
		return Conflict("idempotency_pending", "an identical correction is still in progress")
	default:
		return err
	}
}
