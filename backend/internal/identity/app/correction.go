package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	correctionCreateCommand  = "createCorrectionRequest"
	correctionCreateRoute    = "/identity/correction-requests"
	correctionResolveCommand = "resolveCorrectionRequest"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type CreateCorrectionRequestInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type ResolveCorrectionRequestInput struct {
	TenantID            string
	ActorID             string
	IdempotencyKey      string
	TraceID             string
	CorrectionRequestID string
	RawBody             []byte
}

type createCorrectionRequestBody struct {
	RequestType     string                `json:"request_type"`
	GoatID          *string               `json:"goat_id"`
	IdentifierType  *string               `json:"identifier_type"`
	IdentifierValue *string               `json:"identifier_value"`
	LocationScope   *domain.LocationScope `json:"location_scope"`
	Description     string                `json:"description"`
	EvidenceRefs    *[]domain.EvidenceRef `json:"evidence_refs"`
}

type resolveCorrectionRequestBody struct {
	State        string                `json:"state"`
	Reason       string                `json:"reason"`
	EvidenceRefs *[]domain.EvidenceRef `json:"evidence_refs"`
	RowVersion   *int                  `json:"row_version"`
}

func (s *Service) CreateCorrectionRequest(ctx context.Context, input CreateCorrectionRequestInput) (*domain.CorrectionRequestResponse, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	actorID := strings.TrimSpace(input.ActorID)
	if !uuidPattern.MatchString(actorID) {
		return nil, BadRequest("invalid_actor_id", "authenticated actor must be a valid UUID")
	}
	clientKey := strings.TrimSpace(input.IdempotencyKey)
	if clientKey == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	if len(clientKey) < 8 || len(clientKey) > 200 {
		return nil, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}

	body, err := decodeCreateCorrectionRequest(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateCreateCorrectionRequest(body); err != nil {
		return nil, err
	}
	requestHash, err := CanonicalRequestHash(tenantID, correctionCreateCommand, correctionCreateRoute, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}

	storedKey := fmt.Sprintf("%s:%s:%s", tenantID, correctionCreateCommand, clientKey)
	result, err := s.repo.CreateCorrectionRequest(ctx, ports.CreateCorrectionRequestCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     correctionCreateCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		RequestType:          body.RequestType,
		GoatID:               body.GoatID,
		IdentifierType:       body.IdentifierType,
		IdentifierValue:      body.IdentifierValue,
		LocationScope:        *body.LocationScope,
		Description:          body.Description,
		EvidenceRefs:         *body.EvidenceRefs,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}

	return &domain.CorrectionRequestResponse{
		CorrectionRequest: result.CorrectionRequest,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

func (s *Service) ResolveCorrectionRequest(ctx context.Context, input ResolveCorrectionRequestInput) (*domain.CorrectionRequestResponse, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	actorID := strings.TrimSpace(input.ActorID)
	if !uuidPattern.MatchString(actorID) {
		return nil, BadRequest("invalid_actor_id", "authenticated actor must be a valid UUID")
	}
	clientKey := strings.TrimSpace(input.IdempotencyKey)
	if clientKey == "" {
		return nil, BadRequest("missing_idempotency_key", "Idempotency-Key header is required")
	}
	if len(clientKey) < 8 || len(clientKey) > 200 {
		return nil, BadRequest("invalid_idempotency_key", "Idempotency-Key must be between 8 and 200 characters")
	}
	correctionID := strings.TrimSpace(input.CorrectionRequestID)
	if !uuidPattern.MatchString(correctionID) {
		return nil, BadRequest("invalid_correction_request_id", "correction_request_id must be a valid UUID")
	}

	body, err := decodeResolveCorrectionRequest(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateResolveCorrectionRequest(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/identity/correction-requests/%s/resolve", correctionID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, correctionResolveCommand, route, correctionID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}

	storedKey := fmt.Sprintf("%s:%s:%s", tenantID, correctionResolveCommand, clientKey)
	result, err := s.repo.ResolveCorrectionRequest(ctx, ports.ResolveCorrectionRequestCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     correctionResolveCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		CorrectionRequestID:  correctionID,
		TargetState:          body.State,
		Reason:               body.Reason,
		EvidenceRefs:         *body.EvidenceRefs,
		RowVersion:           *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}

	return &domain.CorrectionRequestResponse{
		CorrectionRequest: result.CorrectionRequest,
		Decision:          &result.Decision,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

func CanonicalRequestHash(tenantID, command, route string, body []byte) (string, error) {
	return canonicalRequestHash(tenantID, command, route, "", body)
}

func CanonicalRequestHashWithSubject(tenantID, command, route, subjectID string, body []byte) (string, error) {
	return canonicalRequestHash(tenantID, command, route, subjectID, body)
}

func canonicalRequestHash(tenantID, command, route, subjectID string, body []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	canonical := struct {
		TenantID  string `json:"tenant_id"`
		Command   string `json:"command"`
		Route     string `json:"route"`
		SubjectID string `json:"subject_id,omitempty"`
		Body      any    `json:"body"`
	}{
		TenantID:  tenantID,
		Command:   command,
		Route:     route,
		SubjectID: subjectID,
		Body:      decoded,
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func decodeCreateCorrectionRequest(raw []byte) (*createCorrectionRequestBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body createCorrectionRequestBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match CreateCorrectionRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func decodeResolveCorrectionRequest(raw []byte) (*resolveCorrectionRequestBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body resolveCorrectionRequestBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ResolveCorrectionRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateCreateCorrectionRequest(body *createCorrectionRequestBody) error {
	body.RequestType = strings.TrimSpace(body.RequestType)
	if !allowedCorrectionRequestTypes[body.RequestType] {
		return BadRequest("invalid_request_type", "request_type is not supported")
	}
	if body.LocationScope == nil {
		return BadRequest("missing_location_scope", "location_scope is required")
	}
	if err := validateOptionalUUID("goat_id", body.GoatID); err != nil {
		return err
	}
	if err := validateLocationScope(body.LocationScope); err != nil {
		return err
	}
	if body.IdentifierType != nil {
		trimmed := strings.TrimSpace(*body.IdentifierType)
		if trimmed == "" {
			return BadRequest("invalid_identifier_type", "identifier_type cannot be empty")
		}
		if !allowedIdentifierTypes[trimmed] {
			return BadRequest("invalid_identifier_type", "identifier_type is not supported")
		}
		*body.IdentifierType = trimmed
	}
	if body.IdentifierValue != nil {
		trimmed := strings.TrimSpace(*body.IdentifierValue)
		if trimmed == "" || len(trimmed) > 200 {
			return BadRequest("invalid_identifier_value", "identifier_value must be between 1 and 200 characters")
		}
		*body.IdentifierValue = trimmed
	}
	body.Description = strings.TrimSpace(body.Description)
	if body.Description == "" || len(body.Description) > 2000 {
		return BadRequest("invalid_description", "description must be between 1 and 2000 characters")
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	return validateEvidenceRefs(*body.EvidenceRefs, false)
}

func validateResolveCorrectionRequest(body *resolveCorrectionRequestBody) error {
	body.State = strings.TrimSpace(body.State)
	if !allowedResolveCorrectionStates[body.State] {
		return BadRequest("invalid_state", "state is not supported for correction resolution")
	}
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

func validateEvidenceRefs(refs []domain.EvidenceRef, requireNonEmpty bool) error {
	if requireNonEmpty && len(refs) == 0 {
		return BadRequest("missing_evidence_refs", "evidence_refs must contain at least one item")
	}
	for i := range refs {
		ref := &refs[i]
		ref.EvidenceType = strings.TrimSpace(ref.EvidenceType)
		ref.EvidenceID = strings.TrimSpace(ref.EvidenceID)
		if !allowedEvidenceTypes[ref.EvidenceType] {
			return BadRequest("invalid_evidence_ref", "evidence_type is not supported")
		}
		if ref.EvidenceID == "" || len(ref.EvidenceID) > 200 {
			return BadRequest("invalid_evidence_ref", "evidence_id must be between 1 and 200 characters")
		}
		if err := trimAndValidateOptionalString("source_system", ref.SourceSystem, 120, true); err != nil {
			return err
		}
		if err := trimAndValidateOptionalString("description", ref.Description, 500, false); err != nil {
			return err
		}
	}
	return nil
}

func validateLocationScope(scope *domain.LocationScope) error {
	if err := validateOptionalUUID("farm_id", scope.FarmID); err != nil {
		return err
	}
	if err := validateOptionalUUID("park_id", scope.ParkID); err != nil {
		return err
	}
	if err := validateOptionalUUID("shed_id", scope.ShedID); err != nil {
		return err
	}
	if err := validateOptionalUUID("cohort_id", scope.CohortID); err != nil {
		return err
	}
	return nil
}

func validateOptionalUUID(field string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || !uuidPattern.MatchString(trimmed) {
		return BadRequest("invalid_"+field, field+" must be a valid UUID")
	}
	*value = trimmed
	return nil
}

func trimAndValidateOptionalString(field string, value *string, max int, requireNonEmpty bool) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if requireNonEmpty && trimmed == "" {
		return BadRequest("invalid_"+field, field+" cannot be empty")
	}
	if len(trimmed) > max {
		return BadRequest("invalid_"+field, fmt.Sprintf("%s must be %d characters or fewer", field, max))
	}
	*value = trimmed
	return nil
}

var allowedCorrectionRequestTypes = map[string]bool{
	"missing_tag":                      true,
	"tag_reused":                       true,
	"rfid_conflict":                    true,
	"possible_duplicate":               true,
	"wrong_location":                   true,
	"wrong_status":                     true,
	"field_verification_result":        true,
	"identifier_seen_but_not_attached": true,
}

var allowedResolveCorrectionStates = map[string]bool{
	"approved":          true,
	"rejected":          true,
	"needs_field_check": true,
	"closed":            true,
}

var allowedIdentifierTypes = map[string]bool{
	"old_tag":            true,
	"rfid":               true,
	"visual_tag":         true,
	"sheet_row_id":       true,
	"purchase_load_id":   true,
	"temp_field_id":      true,
	"external_system_id": true,
}

var allowedEvidenceTypes = map[string]bool{
	"source_record": true,
	"identifier":    true,
	"goat":          true,
	"event":         true,
	"media":         true,
	"decision":      true,
	"import_run":    true,
	"conflict":      true,
	"location":      true,
	"actor":         true,
}
