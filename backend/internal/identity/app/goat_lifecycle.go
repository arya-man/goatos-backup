package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	moveGoatCommand = "moveGoat"
	exitGoatCommand = "exitGoat"
)

type MoveGoatInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	RawBody        []byte
}

type ExitGoatInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	RawBody        []byte
}

func (s *Service) MoveGoat(ctx context.Context, input MoveGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeMoveGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateMoveGoat(body); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("move goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/move"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, moveGoatCommand, route, goatID, raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	occurredAt := time.Now().UTC()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	result, err := s.repo.MoveGoat(ctx, ports.MoveGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, moveGoatCommand, goatID, clientKey),
		IdempotencyScope:     moveGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		ToParkID:             body.ParkID,
		ToShedID:             body.ShedID,
		Reason:               strings.TrimSpace(body.Reason),
		OccurredAt:           occurredAt,
		EvidenceRefs:         body.EvidenceRefs,
		RowVersion:           body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) ExitGoat(ctx context.Context, input ExitGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeExitGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateExitGoat(body); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("exit goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/exit"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, exitGoatCommand, route, goatID, raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	occurredAt := time.Now().UTC()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	result, err := s.repo.ExitGoat(ctx, ports.ExitGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, exitGoatCommand, goatID, clientKey),
		IdempotencyScope:     exitGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		LifecycleStatus:      strings.TrimSpace(body.LifecycleStatus),
		ExitReason:           strings.TrimSpace(body.ExitReason),
		Reason:               strings.TrimSpace(body.Reason),
		OccurredAt:           occurredAt,
		EvidenceRefs:         body.EvidenceRefs,
		RowVersion:           body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func decodeMoveGoat(raw []byte) (*domain.MoveGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.MoveGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match MoveGoatRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func decodeExitGoat(raw []byte) (*domain.ExitGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.ExitGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ExitGoatRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateMoveGoat(body *domain.MoveGoatRequest) error {
	body.ParkID = strings.TrimSpace(body.ParkID)
	body.ShedID = strings.TrimSpace(body.ShedID)
	body.Reason = strings.TrimSpace(body.Reason)
	if !uuidPattern.MatchString(body.ParkID) {
		return BadRequest("invalid_park_id", "park_id must be a valid UUID")
	}
	if !uuidPattern.MatchString(body.ShedID) {
		return BadRequest("invalid_shed_id", "shed_id must be a valid UUID")
	}
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be positive")
	}
	return validateEvidenceRefs(body.EvidenceRefs, true)
}

func validateExitGoat(body *domain.ExitGoatRequest) error {
	body.LifecycleStatus = strings.TrimSpace(body.LifecycleStatus)
	body.ExitReason = strings.TrimSpace(body.ExitReason)
	body.Reason = strings.TrimSpace(body.Reason)
	expectedReason, ok := exitReasonByLifecycle[body.LifecycleStatus]
	if !ok {
		return BadRequest("invalid_lifecycle_status", "lifecycle_status must be dead, sold, culled, transferred, or lost")
	}
	if !allowedExitReasons[body.ExitReason] {
		return BadRequest("invalid_exit_reason", "exit_reason must be sold, died, culled, transferred, or lost")
	}
	if body.ExitReason != expectedReason {
		return BadRequest("invalid_exit_reason", "exit_reason must match lifecycle_status")
	}
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be positive")
	}
	return validateEvidenceRefs(body.EvidenceRefs, true)
}

var allowedExitReasons = map[string]bool{
	"sold":        true,
	"died":        true,
	"culled":      true,
	"transferred": true,
	"lost":        true,
}

var exitReasonByLifecycle = map[string]string{
	"dead":        "died",
	"sold":        "sold",
	"culled":      "culled",
	"transferred": "transferred",
	"lost":        "lost",
}
