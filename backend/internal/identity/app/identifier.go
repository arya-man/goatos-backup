package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	addGoatIdentifierCommand    = "addGoatIdentifier"
	retireGoatIdentifierCommand = "retireGoatIdentifier"
)

type AddGoatIdentifierInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	RawBody        []byte
}

type RetireGoatIdentifierInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	GoatID         string
	IdentifierID   string
	RawBody        []byte
}

type addGoatIdentifierBody struct {
	IdentifierType   string                `json:"identifier_type"`
	IdentifierValue  string                `json:"identifier_value"`
	ScopeKey         string                `json:"scope_key"`
	IsPrimaryForGoat *bool                 `json:"is_primary_for_goat"`
	EvidenceRefs     *[]domain.EvidenceRef `json:"evidence_refs"`
	RowVersion       *int                  `json:"row_version"`
}

type retireGoatIdentifierBody struct {
	Reason       string                `json:"reason"`
	EvidenceRefs *[]domain.EvidenceRef `json:"evidence_refs"`
	RowVersion   *int                  `json:"row_version"`
}

func (s *Service) AddGoatIdentifier(ctx context.Context, input AddGoatIdentifierInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	body, err := decodeAddGoatIdentifier(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateAddGoatIdentifier(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/goats/%s/identifiers", goatID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, addGoatIdentifierCommand, route, goatID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	isPrimary := false
	if body.IsPrimaryForGoat != nil {
		isPrimary = *body.IsPrimaryForGoat
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s", tenantID, addGoatIdentifierCommand, goatID, clientKey)
	result, err := s.repo.AddGoatIdentifier(ctx, ports.AddGoatIdentifierCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     addGoatIdentifierCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		IdentifierType:       body.IdentifierType,
		IdentifierValue:      body.IdentifierValue,
		NormalizedValue:      normalizeIdentifier(body.IdentifierType, body.IdentifierValue),
		ScopeKey:             body.ScopeKey,
		IsPrimaryForGoat:     isPrimary,
		EvidenceRefs:         *body.EvidenceRefs,
		RowVersion:           *body.RowVersion,
		Reason:               "Admin attached identifier with evidence.",
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) RetireGoatIdentifier(ctx context.Context, input RetireGoatIdentifierInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	goatID := strings.TrimSpace(input.GoatID)
	if !uuidPattern.MatchString(goatID) {
		return nil, BadRequest("invalid_goat_id", "goat_id must be a valid UUID")
	}
	identifierID := strings.TrimSpace(input.IdentifierID)
	if !uuidPattern.MatchString(identifierID) {
		return nil, BadRequest("invalid_identifier_id", "identifier_id must be a valid UUID")
	}
	body, err := decodeRetireGoatIdentifier(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateRetireGoatIdentifier(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/goats/%s/identifiers/%s/retire", goatID, identifierID)
	subjectID := goatID + ":" + identifierID
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, retireGoatIdentifierCommand, route, subjectID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s:%s", tenantID, retireGoatIdentifierCommand, goatID, identifierID, clientKey)
	result, err := s.repo.RetireGoatIdentifier(ctx, ports.RetireGoatIdentifierCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     retireGoatIdentifierCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		GoatID:               goatID,
		IdentifierID:         identifierID,
		Reason:               body.Reason,
		EvidenceRefs:         *body.EvidenceRefs,
		RowVersion:           *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func validateWriteHeaders(rawTenantID, rawActorID, rawClientKey string) (tenantID, actorID, clientKey string, err error) {
	tenantID = strings.TrimSpace(rawTenantID)
	if err := requireTenant(tenantID); err != nil {
		return "", "", "", err
	}
	actorID = strings.TrimSpace(rawActorID)
	if !uuidPattern.MatchString(actorID) {
		return "", "", "", BadRequest("invalid_actor_id", "authenticated actor must be a valid UUID")
	}
	clientKey = strings.TrimSpace(rawClientKey)
	if clientKey == "" {
		return "", "", "", BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	if len(clientKey) < 8 || len(clientKey) > 200 {
		return "", "", "", BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	return tenantID, actorID, clientKey, nil
}

func decodeAddGoatIdentifier(raw []byte) (*addGoatIdentifierBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body addGoatIdentifierBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match AddIdentifierRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func decodeRetireGoatIdentifier(raw []byte) (*retireGoatIdentifierBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body retireGoatIdentifierBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match RetireIdentifierRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateAddGoatIdentifier(body *addGoatIdentifierBody) error {
	body.IdentifierType = strings.TrimSpace(body.IdentifierType)
	if !allowedIdentifierTypes[body.IdentifierType] {
		return BadRequest("invalid_identifier_type", "identifier_type is not supported")
	}
	body.IdentifierValue = strings.TrimSpace(body.IdentifierValue)
	if body.IdentifierValue == "" || len(body.IdentifierValue) > 200 {
		return BadRequest("invalid_identifier_value", "identifier_value must be between 1 and 200 characters")
	}
	body.ScopeKey = strings.TrimSpace(body.ScopeKey)
	if body.ScopeKey == "" || len(body.ScopeKey) > 120 {
		return BadRequest("invalid_scope_key", "scope_key must be between 1 and 120 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	return validateEvidenceRefs(*body.EvidenceRefs, true)
}

func validateRetireGoatIdentifier(body *retireGoatIdentifierBody) error {
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 2000 {
		return BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	return validateEvidenceRefs(*body.EvidenceRefs, true)
}

func adminGoatResponse(result *ports.AdminGoatMutationResult, clientKey string, traceID string) *domain.AdminGoatResponse {
	generationStatus := result.GenerationStatus
	if generationStatus == "" {
		generationStatus = "not_applicable"
	}
	return &domain.AdminGoatResponse{
		Goat:        result.Goat,
		Identifiers: result.Identifiers,
		Decision:    result.Decision,
		Events:      result.Events,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		GenerationStatus: generationStatus,
		TraceID:          traceID,
	}
}
