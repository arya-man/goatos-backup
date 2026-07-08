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
	moveGoatCommand          = "moveGoat"
	exitGoatCommand          = "exitGoat"
	criticalDeathGoatCommand = "criticalDeathGoat"
	stageGoatCommand         = "stageGoat"
	healthGoatCommand        = "healthGoat"
	reproductiveGoatCommand  = "reproductiveGoat"
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

type StageGoatInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	RawBody        []byte
}

type HealthGoatInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	RawBody        []byte
}

type ReproductiveGoatInput struct {
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
	return s.exitGoat(ctx, input, exitGoatCommand, "/admin/goats/{goat_id}/exit", validateExitGoat, false)
}

func (s *Service) CriticalDeathExit(ctx context.Context, input ExitGoatInput) (*domain.AdminGoatResponse, error) {
	return s.exitGoat(ctx, input, criticalDeathGoatCommand, "/admin/goats/{goat_id}/critical-death-exit", validateCriticalDeathExit, true)
}

func (s *Service) exitGoat(ctx context.Context, input ExitGoatInput, commandName, route string, validate func(*domain.ExitGoatRequest) error, guardrailApproved bool) (*domain.AdminGoatResponse, error) {
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
	if err := validate(body); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("exit goat request normalization failed")
	}
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, commandName, route, goatID, raw)
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
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, commandName, goatID, clientKey),
		IdempotencyScope:     commandName,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		LifecycleStatus:      strings.TrimSpace(body.LifecycleStatus),
		ExitReason:           strings.TrimSpace(body.ExitReason),
		Reason:               strings.TrimSpace(body.Reason),
		OccurredAt:           occurredAt,
		EvidenceRefs:         body.EvidenceRefs,
		RowVersion:           body.RowVersion,
		GuardrailApproved:    guardrailApproved,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) StageGoat(ctx context.Context, input StageGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeStageGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateStageGoat(body); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("stage goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/stage"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, stageGoatCommand, route, goatID, raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	occurredAt := time.Now().UTC()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	result, err := s.repo.StageGoat(ctx, ports.StageGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, stageGoatCommand, goatID, clientKey),
		IdempotencyScope:     stageGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		ManagementStage:      strings.TrimSpace(body.ManagementStage),
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

func (s *Service) HealthGoat(ctx context.Context, input HealthGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeHealthGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateHealthGoat(body); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("health goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/health"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, healthGoatCommand, route, goatID, raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	occurredAt := time.Now().UTC()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	result, err := s.repo.HealthGoat(ctx, ports.HealthGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, healthGoatCommand, goatID, clientKey),
		IdempotencyScope:     healthGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		HealthStatus:         strings.TrimSpace(body.HealthStatus),
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

func (s *Service) ReproductiveGoat(ctx context.Context, input ReproductiveGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeReproductiveGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	breedingDate, lastDeliveryDate, err := validateReproductiveGoat(body)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, Internal("reproductive goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/reproductive"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, reproductiveGoatCommand, route, goatID, raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	occurredAt := time.Now().UTC()
	if body.OccurredAt != nil {
		occurredAt = body.OccurredAt.UTC()
	}
	result, err := s.repo.ReproductiveGoat(ctx, ports.ReproductiveGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, reproductiveGoatCommand, goatID, clientKey),
		IdempotencyScope:     reproductiveGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		ReproductiveStatus:   strings.TrimSpace(body.ReproductiveStatus),
		BreedingDate:         breedingDate,
		LastDeliveryDate:     lastDeliveryDate,
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

func decodeStageGoat(raw []byte) (*domain.StageGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.StageGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match StageGoatRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func decodeHealthGoat(raw []byte) (*domain.HealthGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.HealthGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match HealthGoatRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func decodeReproductiveGoat(raw []byte) (*domain.ReproductiveGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.ReproductiveGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ReproductiveGoatRequest")
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
	if err := validateExitGoatCommon(body); err != nil {
		return err
	}
	if criticalDeathExit(body.LifecycleStatus, body.ExitReason) {
		return criticalDeathTransitionError()
	}
	return nil
}

func validateCriticalDeathExit(body *domain.ExitGoatRequest) error {
	if err := validateExitGoatCommon(body); err != nil {
		return err
	}
	if !criticalDeathExit(body.LifecycleStatus, body.ExitReason) {
		return BadRequest("invalid_death_exit", "critical death exit requires lifecycle_status dead and exit_reason died")
	}
	return nil
}

func validateExitGoatCommon(body *domain.ExitGoatRequest) error {
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

func validateStageGoat(body *domain.StageGoatRequest) error {
	body.ManagementStage = strings.TrimSpace(body.ManagementStage)
	body.Reason = strings.TrimSpace(body.Reason)
	if len(body.ManagementStage) < 1 || len(body.ManagementStage) > 80 {
		return BadRequest("invalid_management_stage", "management_stage must be between 1 and 80 characters")
	}
	if strings.ContainsAny(body.ManagementStage, "\n\r\t") {
		return BadRequest("invalid_management_stage", "management_stage must be a single line value")
	}
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be positive")
	}
	return validateEvidenceRefs(body.EvidenceRefs, true)
}

func validateHealthGoat(body *domain.HealthGoatRequest) error {
	body.HealthStatus = strings.TrimSpace(body.HealthStatus)
	body.Reason = strings.TrimSpace(body.Reason)
	if !allowedHealthStatuses[body.HealthStatus] {
		return BadRequest("invalid_health_status", "health_status must be healthy, sick, under_treatment, recovering, quarantine, or icu")
	}
	if criticalHealthStatus(body.HealthStatus) {
		return criticalHealthTransitionError()
	}
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be positive")
	}
	return validateEvidenceRefs(body.EvidenceRefs, true)
}

// validateReproductiveGoat does structural validation only. The reproductive value itself is
// validated against the source-backed vocabulary (active status_definitions on the reproductive
// axis) inside the repository transaction, so no divergent status list is hardcoded here.
func validateReproductiveGoat(body *domain.ReproductiveGoatRequest) (*time.Time, *time.Time, error) {
	body.ReproductiveStatus = strings.TrimSpace(body.ReproductiveStatus)
	body.Reason = strings.TrimSpace(body.Reason)
	if len(body.ReproductiveStatus) < 1 || len(body.ReproductiveStatus) > 80 {
		return nil, nil, BadRequest("invalid_reproductive_status", "reproductive_status must be between 1 and 80 characters")
	}
	if strings.ContainsAny(body.ReproductiveStatus, "\n\r\t") {
		return nil, nil, BadRequest("invalid_reproductive_status", "reproductive_status must be a single line value")
	}
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return nil, nil, BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return nil, nil, BadRequest("invalid_row_version", "row_version must be positive")
	}
	breedingDate, err := optionalDateField("breeding_date", body.BreedingDate)
	if err != nil {
		return nil, nil, err
	}
	lastDeliveryDate, err := optionalDateField("last_delivery_date", body.LastDeliveryDate)
	if err != nil {
		return nil, nil, err
	}
	if err := validateEvidenceRefs(body.EvidenceRefs, true); err != nil {
		return nil, nil, err
	}
	return breedingDate, lastDeliveryDate, nil
}

// optionalDateField parses an optional YYYY-MM-DD field. Absent or blank returns (nil, nil) so the
// stored value is left untouched; a present-but-malformed value is rejected.
func optionalDateField(field string, raw *string) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	if strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := parseDateField(field, *raw)
	if err != nil {
		return nil, BadRequest("invalid_"+field, field+" must be YYYY-MM-DD")
	}
	utc := parsed.UTC()
	return &utc, nil
}

var allowedHealthStatuses = map[string]bool{
	"healthy":         true,
	"sick":            true,
	"under_treatment": true,
	"recovering":      true,
	"quarantine":      true,
	"icu":             true,
}

func criticalHealthStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "quarantine", "icu":
		return true
	default:
		return false
	}
}

func criticalHealthTransitionError() *Error {
	return GuardrailRequired("critical_health_transition_requires_guardrail", "quarantine and ICU health transitions must use the critical-action guardrail path")
}

func criticalDeathExit(lifecycleStatus, exitReason string) bool {
	return strings.TrimSpace(lifecycleStatus) == "dead" || strings.TrimSpace(exitReason) == "died"
}

func criticalDeathTransitionError() *Error {
	return GuardrailRequired("critical_death_transition_requires_guardrail", "death exits must use the critical-action guardrail path")
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
