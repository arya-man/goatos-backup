package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	moveGoatCommand          = "moveGoat"
	exitGoatCommand          = "exitGoat"
	criticalDeathGoatCommand = "criticalDeathGoat"
	stageGoatCommand         = "stageGoat"
	healthGoatCommand        = "healthGoat"
	reproductiveGoatCommand  = "reproductiveGoat"
	identityGoatCommand      = "identityGoat"
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

type IdentityGoatInput struct {
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
		slog.ErrorContext(ctx, "move_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", moveGoatCommand), slog.Any("error", err))
		return nil, Internal("move goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/move"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, moveGoatCommand, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "move_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", moveGoatCommand), slog.Any("error", err))
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

// Two-route split for goat exits. Both land in exitGoat; they differ only in which
// exit vocabulary each one ACCEPTS, and the split is the guardrail:
//
//	ExitGoat           sold | culled | transferred | lost   — rejects dead/died (409)
//	CriticalDeathExit  dead + died ONLY                      — rejects everything else (400)
//
// Neither validator can be satisfied by the other's payload, so a death exit cannot
// reach the repository without GuardrailApproved=true (re-checked in the postgres
// adapter, not trusted from here). That is what buys the death-specific effects —
// obligation cancellation and the goat.exited emission — instead of a silent status flip.
//
// The split is about the WRITE, not the caller. It is not an admin-web-only or
// leadership-only path: death recording is field work (a maintainer-approved
// operator records a death from the mobile Counts module), and the guardrail applies
// identically to every principal that holds the route permission. Who may call it is
// decided by permissions.RoutePermissions; what the call must prove is decided here.
// Widening access therefore never means widening this validator — see
// docs/features/critical-animal-action-guardrails.md.
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
		slog.ErrorContext(ctx, "exit_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", commandName), slog.Any("error", err))
		return nil, Internal("exit goat request normalization failed")
	}
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, commandName, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "exit_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", commandName), slog.Any("error", err))
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
		slog.ErrorContext(ctx, "stage_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", stageGoatCommand), slog.Any("error", err))
		return nil, Internal("stage goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/stage"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, stageGoatCommand, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "stage_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", stageGoatCommand), slog.Any("error", err))
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
		slog.ErrorContext(ctx, "health_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", healthGoatCommand), slog.Any("error", err))
		return nil, Internal("health goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/health"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, healthGoatCommand, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "health_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", healthGoatCommand), slog.Any("error", err))
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
		slog.ErrorContext(ctx, "reproductive_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", reproductiveGoatCommand), slog.Any("error", err))
		return nil, Internal("reproductive goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/reproductive"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, reproductiveGoatCommand, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "reproductive_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", reproductiveGoatCommand), slog.Any("error", err))
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

// IdentityGoat corrects a goat's DOB and/or entry_date. It emits goat.identity.changed so the
// vaccination recheck consumer recomputes obligations (the anchor of age-routing + birth_age/
// post_arrival due dates).
func (s *Service) IdentityGoat(ctx context.Context, input IdentityGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeIdentityGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	dob, entryDate, err := validateIdentityGoat(body)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		slog.ErrorContext(ctx, "identity_goat_request_marshal_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", identityGoatCommand), slog.Any("error", err))
		return nil, Internal("identity goat request normalization failed")
	}
	route := "/admin/goats/{goat_id}/identity"
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, identityGoatCommand, route, goatID, raw)
	if err != nil {
		slog.ErrorContext(ctx, "identity_goat_request_hash_failed",
			slog.String("tenant_id", tenantID), slog.String("goat_id", goatID),
			slog.String("op", identityGoatCommand), slog.Any("error", err))
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	// VACC-REV-07: recomputation + persistence use the SERVER processing instant, never a
	// client-supplied occurred_at. A backdated occurred_at must not become the generation as_of (it
	// would sort before an accepted completion's verified_at and let already-anchored work be
	// regenerated); a future occurred_at is rejected (it would evaluate schedules ahead of time and
	// stamp a future updated_at). Any client-supplied value is retained only as the business
	// effective date for audit.
	serverNow := time.Now().UTC()
	var effectiveAt *time.Time
	if body.OccurredAt != nil {
		// VACC-REV-07: reject a strictly-future occurred_at (no skew allowance) — a correction cannot
		// have "happened" after the server processed it.
		if body.OccurredAt.After(serverNow) {
			return nil, BadRequest("invalid_occurred_at", "occurred_at cannot be in the future")
		}
		ea := body.OccurredAt.UTC()
		effectiveAt = &ea
	}
	// VACC-REV-12: a DOB or arrival date cannot be in the future — a future anchor would push the
	// birth_age/post_arrival vaccination schedule into the future. Reject the correction. Use India
	// business-calendar dates to avoid boundary issues at IST midnight (UTC 18:30): comparing
	// "today" as a date in IST, not as a UTC instant.
	nowBusinessDay := biztime.BusinessDayStart(serverNow)
	if dob != nil {
		dobBusinessDay := biztime.BusinessDayStart(*dob)
		if dobBusinessDay.After(nowBusinessDay) {
			return nil, BadRequest("invalid_dob", "dob cannot be in the future")
		}
	}
	if entryDate != nil {
		entryBusinessDay := biztime.BusinessDayStart(*entryDate)
		if entryBusinessDay.After(nowBusinessDay) {
			return nil, BadRequest("invalid_entry_date", "entry_date cannot be in the future")
		}
	}
	result, err := s.repo.IdentityGoat(ctx, ports.IdentityGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: fmt.Sprintf("%s:%s:%s:%s", tenantID, identityGoatCommand, goatID, clientKey),
		IdempotencyScope:     identityGoatCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		DOB:                  dob,
		EntryDate:            entryDate,
		Reason:               strings.TrimSpace(body.Reason),
		OccurredAt:           serverNow,
		EffectiveAt:          effectiveAt,
		EvidenceRefs:         body.EvidenceRefs,
		RowVersion:           body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func decodeIdentityGoat(raw []byte) (*domain.IdentityGoatRequest, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body domain.IdentityGoatRequest
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match IdentityGoatRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateIdentityGoat(body *domain.IdentityGoatRequest) (*time.Time, *time.Time, error) {
	body.Reason = strings.TrimSpace(body.Reason)
	if len(body.Reason) < 3 || len(body.Reason) > 500 {
		return nil, nil, BadRequest("invalid_reason", "reason must be between 3 and 500 characters")
	}
	if body.RowVersion < 1 {
		return nil, nil, BadRequest("invalid_row_version", "row_version must be positive")
	}
	dob, err := optionalDateField("dob", body.DOB)
	if err != nil {
		return nil, nil, err
	}
	entryDate, err := optionalDateField("entry_date", body.EntryDate)
	if err != nil {
		return nil, nil, err
	}
	if dob == nil && entryDate == nil {
		return nil, nil, BadRequest("missing_correction", "at least one of dob or entry_date must be provided")
	}
	if err := validateEvidenceRefs(body.EvidenceRefs, true); err != nil {
		return nil, nil, err
	}
	return dob, entryDate, nil
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
		slog.Warn("goat_lifecycle_date_field_invalid",
			slog.String("field", field), slog.String("raw_value", *raw), slog.Any("error", err))
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
	return GuardrailRequired("critical_action_guardrail_required", "quarantine and ICU health transitions must use the critical-action guardrail path")
}

// criticalDeathExit detects a death exit from EITHER half of the dead+died pairing
// (OR, not AND) so a caller cannot slip past by sending only one of them. The regular
// exit primitive uses it to reject; the guardrail primitive uses it to require. Mismatched
// pairs (e.g. dead + sold) are then caught by validateExitGoatCommon's exitReasonByLifecycle
// check, so the guardrail route accepts exactly dead+died.
func criticalDeathExit(lifecycleStatus, exitReason string) bool {
	return strings.TrimSpace(lifecycleStatus) == "dead" || strings.TrimSpace(exitReason) == "died"
}

// criticalDeathTransitionError is a 409 guardrail-required, not a 403 authorization
// failure, and the distinction is deliberate: it says "this write must take the guarded
// route", never "you are not allowed to record a death". An operator recording a death
// from the mobile Counts module gets this same 409 if it aims at the regular exit
// primitive — the fix is the route, not a role escalation.
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
