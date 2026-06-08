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

const resolveConflictCommand = "resolveIdentityConflict"

type ResolveConflictInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	ConflictID     string
	RawBody        []byte
}

type resolveConflictBody struct {
	DecisionType      string                    `json:"decision_type"`
	DecisionResult    string                    `json:"decision_result"`
	SurvivorGoatID    *string                   `json:"survivor_goat_id"`
	AffectedGoatIDs   []string                  `json:"affected_goat_ids"`
	IdentifierActions []domain.IdentifierAction `json:"identifier_actions"`
	EvidenceRefs      *[]domain.EvidenceRef     `json:"evidence_refs"`
	Reason            string                    `json:"reason"`
	RowVersion        *int                      `json:"row_version"`
}

func (s *Service) ResolveConflict(ctx context.Context, input ResolveConflictInput) (*domain.ResolveConflictResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	conflictID := strings.TrimSpace(input.ConflictID)
	if !uuidPattern.MatchString(conflictID) {
		return nil, BadRequest("invalid_conflict_id", "conflict_id must be a valid UUID")
	}
	body, err := decodeResolveConflict(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateResolveConflict(body); err != nil {
		return nil, err
	}
	if body.DecisionType == "create_goat" {
		return nil, NotImplemented("unsupported_conflict_decision", "create_goat conflict resolution needs contract-defined goat creation fields before implementation")
	}

	route := fmt.Sprintf("/admin/identity/conflicts/%s/resolve", conflictID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, resolveConflictCommand, route, conflictID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s", tenantID, resolveConflictCommand, conflictID, clientKey)
	result, err := s.repo.ResolveConflict(ctx, ports.ResolveConflictCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     resolveConflictCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		ConflictID:           conflictID,
		DecisionType:         body.DecisionType,
		DecisionResult:       body.DecisionResult,
		SurvivorGoatID:       stringValue(body.SurvivorGoatID),
		AffectedGoatIDs:      body.AffectedGoatIDs,
		IdentifierActions:    body.IdentifierActions,
		EvidenceRefs:         *body.EvidenceRefs,
		Reason:               body.Reason,
		RowVersion:           *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.ResolveConflictResponse{
		ConflictID: result.ConflictID,
		State:      result.State,
		Decision:   result.Decision,
		Merge:      result.Merge,
		Events:     result.Events,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

func decodeResolveConflict(raw []byte) (*resolveConflictBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body resolveConflictBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ResolveConflictRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateResolveConflict(body *resolveConflictBody) error {
	body.DecisionType = strings.TrimSpace(body.DecisionType)
	if !allowedResolveConflictDecisionTypes[body.DecisionType] {
		return BadRequest("invalid_decision_type", "decision_type is not supported")
	}
	body.DecisionResult = strings.TrimSpace(body.DecisionResult)
	if !allowedResolveConflictDecisionResults[body.DecisionResult] {
		return BadRequest("invalid_decision_result", "decision_result is not supported")
	}
	if expected := resolveConflictDecisionPairs[body.DecisionType]; expected != body.DecisionResult {
		return BadRequest("invalid_decision_pair", "decision_type and decision_result are not a supported pair")
	}
	if body.SurvivorGoatID != nil {
		trimmed := strings.TrimSpace(*body.SurvivorGoatID)
		body.SurvivorGoatID = &trimmed
	}
	if body.DecisionType == "merge_goats" && (body.SurvivorGoatID == nil || strings.TrimSpace(*body.SurvivorGoatID) == "") {
		return BadRequest("missing_survivor_goat_id", "survivor_goat_id is required for merge_goats")
	}
	if body.SurvivorGoatID != nil && strings.TrimSpace(*body.SurvivorGoatID) != "" {
		if err := validateOptionalUUID("survivor_goat_id", body.SurvivorGoatID); err != nil {
			return err
		}
	}
	if body.DecisionType != "merge_goats" && body.SurvivorGoatID != nil && strings.TrimSpace(*body.SurvivorGoatID) != "" {
		return BadRequest("unexpected_survivor_goat_id", "survivor_goat_id is only supported for merge_goats")
	}
	if body.DecisionType == "mark_identifier_disputed" && len(body.IdentifierActions) == 0 {
		return BadRequest("missing_identifier_actions", "mark_identifier_disputed requires explicit dispute identifier_actions")
	}
	if err := validateResolveConflictAffectedGoats(body); err != nil {
		return err
	}
	for i := range body.IdentifierActions {
		if err := validateIdentifierAction(&body.IdentifierActions[i]); err != nil {
			return err
		}
		if body.DecisionType == "mark_identifier_disputed" {
			if body.IdentifierActions[i].Action != "dispute" {
				return BadRequest("invalid_identifier_action", "mark_identifier_disputed only accepts dispute identifier_actions")
			}
			if body.IdentifierActions[i].IdentifierID == nil {
				return BadRequest("missing_identifier_id", "dispute identifier_actions require identifier_id")
			}
		}
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	if err := validateEvidenceRefs(*body.EvidenceRefs, true); err != nil {
		return err
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 2000 {
		return BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	return nil
}

func validateResolveConflictAffectedGoats(body *resolveConflictBody) error {
	if len(body.AffectedGoatIDs) == 0 {
		return BadRequest("missing_affected_goat_ids", "affected_goat_ids must contain at least one goat")
	}
	seen := map[string]struct{}{}
	hasLoser := false
	survivorID := ""
	if body.SurvivorGoatID != nil {
		survivorID = *body.SurvivorGoatID
	}
	for i := range body.AffectedGoatIDs {
		goatID := strings.TrimSpace(body.AffectedGoatIDs[i])
		if !uuidPattern.MatchString(goatID) {
			return BadRequest("invalid_affected_goat_id", "affected_goat_ids must contain valid UUIDs")
		}
		body.AffectedGoatIDs[i] = goatID
		if _, ok := seen[goatID]; ok {
			return BadRequest("duplicate_affected_goat_id", "affected_goat_ids must not contain duplicates")
		}
		seen[goatID] = struct{}{}
		if goatID != survivorID {
			hasLoser = true
		}
	}
	if body.DecisionType == "merge_goats" && !hasLoser {
		return BadRequest("missing_merged_goat", "affected_goat_ids must include at least one goat distinct from survivor_goat_id")
	}
	return nil
}

func validateIdentifierAction(action *domain.IdentifierAction) error {
	action.Action = strings.TrimSpace(action.Action)
	if !allowedIdentifierActions[action.Action] {
		return BadRequest("invalid_identifier_action", "identifier action is not supported")
	}
	if err := validateOptionalUUID("identifier_id", action.IdentifierID); err != nil {
		return err
	}
	if action.IdentifierType != nil {
		trimmed := strings.TrimSpace(*action.IdentifierType)
		if trimmed == "" || !allowedIdentifierTypes[trimmed] {
			return BadRequest("invalid_identifier_type", "identifier_type is not supported")
		}
		*action.IdentifierType = trimmed
	}
	if action.IdentifierValue != nil {
		trimmed := strings.TrimSpace(*action.IdentifierValue)
		if trimmed == "" || len(trimmed) > 200 {
			return BadRequest("invalid_identifier_value", "identifier_value must be between 1 and 200 characters")
		}
		*action.IdentifierValue = trimmed
	}
	return nil
}

var allowedResolveConflictDecisionTypes = map[string]bool{
	"merge_goats":                true,
	"mark_identifier_disputed":   true,
	"create_goat":                true,
	"reject_match":               true,
	"request_field_verification": true,
}

var allowedResolveConflictDecisionResults = map[string]bool{
	"same_goat_merge":                     true,
	"different_goats_identifier_disputed": true,
	"new_goat_required":                   true,
	"candidate_rejected":                  true,
	"field_verification_required":         true,
}

var resolveConflictDecisionPairs = map[string]string{
	"merge_goats":                "same_goat_merge",
	"mark_identifier_disputed":   "different_goats_identifier_disputed",
	"create_goat":                "new_goat_required",
	"reject_match":               "candidate_rejected",
	"request_field_verification": "field_verification_required",
}

var allowedIdentifierActions = map[string]bool{
	"attach":   true,
	"retire":   true,
	"dispute":  true,
	"transfer": true,
	"preserve": true,
	"reject":   true,
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
