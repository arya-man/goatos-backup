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

const reclassifyShedStageCommand = "reclassifyShedStage"

const (
	previewReclassifyShedStageRoute = "/admin/goats/shed-stage/preview"
	commitReclassifyShedStageRoute  = "/admin/goats/shed-stage/commit"
)

// ReclassifyShedStageInput carries the transport-level envelope. RawBody is decoded here rather
// than in the handler so unknown fields are rejected against the same struct the hash is taken of.
type ReclassifyShedStageInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

// PreviewReclassifyShedStage reports what the reclassification WOULD do. It writes nothing and
// takes no idempotency key -- a preview is safe to repeat, and requiring a key would make the
// dialog's own refresh burn keys the commit then could not reuse.
func (s *Service) PreviewReclassifyShedStage(ctx context.Context, input ReclassifyShedStageInput) (*domain.ReclassifyShedStagePreviewResponse, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return nil, BadRequest("missing_tenant", "tenant context is required")
	}
	body, err := decodeReclassifyShedStage(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateReclassifyShedStage(body, false); err != nil {
		return nil, err
	}

	preview, err := s.repo.PreviewReclassifyShedStage(ctx, ports.ReclassifyShedStageCommand{
		TenantID:        tenantID,
		TraceID:         input.TraceID,
		ShedID:          body.ShedID,
		PartitionLabel:  partitionParam(body.PartitionLabel),
		ManagementStage: body.ManagementStage,
		ConfigureEmpty:  body.ConfigureEmpty,
	})
	if err != nil {
		return nil, mapReclassifyErr(err)
	}

	response := &domain.ReclassifyShedStagePreviewResponse{
		ShedID:                     preview.ShedID,
		ShedName:                   preview.ShedName,
		PartitionLabel:             optionalLabel(preview.PartitionLabel),
		OperationalLocationDisplay: preview.OperationalLocationDisplay,
		ManagementStage:            preview.ManagementStage,
		AgeBand:                    preview.AgeBand,
		TotalLive:                  preview.TotalLive,
		Changing:                   preview.Changing,
		Unchanged:                  preview.Unchanged,
		CurrentStages:              make([]domain.ReclassifyShedStageBucket, 0, len(preview.CurrentStages)),
		TraceID:                    input.TraceID,
	}
	for _, bucket := range preview.CurrentStages {
		response.CurrentStages = append(response.CurrentStages, domain.ReclassifyShedStageBucket{
			ManagementStage: bucket.ManagementStage,
			AgeBand:         bucket.AgeBand,
			Count:           bucket.Count,
		})
	}
	return response, nil
}

// CommitReclassifyShedStage applies the cohort tag to the pen. Immediate and unstaged: there is no
// approval step, so the Idempotency-Key is the only thing standing between a double-clicked button
// and a second round of stage-change events.
func (s *Service) CommitReclassifyShedStage(ctx context.Context, input ReclassifyShedStageInput) (*domain.ReclassifyShedStageResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeReclassifyShedStage(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateReclassifyShedStage(body, true); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("reclassify shed stage request normalization failed: %w", err)
	}

	// The subject is the PEN, so the same key used against a different pen or a different target
	// cohort is a hash conflict rather than a silent replay of the wrong write.
	subject := fmt.Sprintf("%s:%s", body.ShedID, oploc.NormalizePartition(body.PartitionLabel))
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, reclassifyShedStageCommand, commitReclassifyShedStageRoute, subject, raw)
	if err != nil {
		return nil, fmt.Errorf("reclassify shed stage request hash failed: %w", err)
	}

	result, err := s.repo.ReclassifyShedStage(ctx, ports.ReclassifyShedStageCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, reclassifyShedStageCommand, subject, clientKey),
		IdempotencyScope:     reclassifyShedStageCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		ShedID:               body.ShedID,
		PartitionLabel:       partitionParam(body.PartitionLabel),
		ManagementStage:      body.ManagementStage,
		Reason:               body.Reason,
		ConfigureEmpty:       body.ConfigureEmpty,
		OccurredAt:           s.now().UTC(),
	})
	if err != nil {
		return nil, mapReclassifyErr(err)
	}

	return &domain.ReclassifyShedStageResponse{
		ShedID:                     result.ShedID,
		ShedName:                   result.ShedName,
		PartitionLabel:             optionalLabel(result.PartitionLabel),
		OperationalLocationDisplay: result.OperationalLocationDisplay,
		ManagementStage:            result.ManagementStage,
		AgeBand:                    result.AgeBand,
		TotalLive:                  result.TotalLive,
		Reclassified:               result.Reclassified,
		Unchanged:                  result.Unchanged,
		IdempotencyKey:             clientKey,
		TraceID:                    input.TraceID,
	}, nil
}

func decodeReclassifyShedStage(raw []byte) (*domain.ReclassifyShedStageRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.ReclassifyShedStageRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ReclassifyShedStageRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

// validateReclassifyShedStage normalizes and checks the request. requireReason is false for the
// preview, which is a read: forcing a reason before the operator has seen what they are about to
// change would make them write one for a pen they then decide not to touch.
func validateReclassifyShedStage(body *domain.ReclassifyShedStageRequest, requireReason bool) error {
	body.ShedID = strings.TrimSpace(body.ShedID)
	body.PartitionLabel = strings.TrimSpace(body.PartitionLabel)
	body.ManagementStage = strings.TrimSpace(body.ManagementStage)
	body.Reason = strings.TrimSpace(body.Reason)

	if !uuidPattern.MatchString(body.ShedID) {
		return BadRequest("invalid_shed_id", "shed_id must be a valid UUID")
	}
	if len(body.PartitionLabel) > 80 || strings.ContainsAny(body.PartitionLabel, "\n\r\t") {
		return BadRequest("invalid_partition_label", "partition_label must be a single line value of at most 80 characters")
	}
	if len(body.ManagementStage) < 1 || len(body.ManagementStage) > 80 {
		return BadRequest("invalid_management_stage", "management_stage must be between 1 and 80 characters")
	}
	if strings.ContainsAny(body.ManagementStage, "\n\r\t") {
		return BadRequest("invalid_management_stage", "management_stage must be a single line value")
	}
	if requireReason && (len(body.Reason) < 3 || len(body.Reason) > 500) {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if len(body.Reason) > 500 {
		return BadRequest("invalid_reason", "reason must be at most 500 characters")
	}
	return nil
}

// partitionParam maps the wire's empty/'whole' forms to the port's nil, so an undivided shed is
// expressed once, as absence, rather than three interchangeable ways.
func partitionParam(label string) *string {
	if !oploc.IsPartitioned(label) {
		return nil
	}
	trimmed := strings.TrimSpace(label)
	return &trimmed
}

// optionalLabel keeps the 'whole' sentinel out of the wire. It is a matching key, never user copy.
func optionalLabel(label string) *string {
	if !oploc.IsPartitioned(label) {
		return nil
	}
	value := label
	return &value
}

// mapReclassifyErr turns the port's domain errors into the operator-facing failures. The clinical
// rejection is called out by name because "you cannot mark a whole pen sick from here" is a rule
// the operator needs to understand, not a generic invalid-value.
func mapReclassifyErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ports.ErrClinicalDestinationTag):
		return BadRequest("clinical_stage_not_allowed",
			"a clinical state is set by the animal's health workflow and cannot be applied to a whole pen from here")
	case errors.Is(err, ports.ErrDestinationTagConflict):
		return BadRequest("invalid_management_stage", "management_stage is not an active stage for this tenant")
	case errors.Is(err, ports.ErrReclassifyEmptyScope):
		return BadRequest("empty_scope", "the selected shed and partition holds no live animals")
	case errors.Is(err, ports.ErrReclassifyScopeTooLarge):
		return BadRequest("scope_too_large",
			fmt.Sprintf("the selected shed and partition holds more than %d live animals", ports.MaxReclassifyGoatsPerCommand))
	default:
		return mapRepoErr(err)
	}
}
