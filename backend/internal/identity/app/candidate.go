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
	listCandidatesCommand   = "listIdentityCandidates"
	rejectCandidateCommand  = "rejectIdentityCandidate"
	approveCandidateCommand = "approveIdentityCandidate"
)

type ReviewCandidateInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	CandidateID    string
	RawBody        []byte
}

type reviewCandidateBody struct {
	Reason       string                `json:"reason"`
	EvidenceRefs *[]domain.EvidenceRef `json:"evidence_refs"`
	RowVersion   *int                  `json:"row_version"`
}

func (s *Service) ListCandidates(ctx context.Context, params ports.ListCandidatesParams, traceID string) (*domain.CandidateListResponse, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	if params.Limit < 1 || params.Limit > 100 {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 100")
	}
	items, next, err := s.repo.ListCandidates(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CandidateListResponse{Items: items, NextCursor: next, TraceID: traceID}, nil
}

func (s *Service) RejectCandidate(ctx context.Context, input ReviewCandidateInput) (*domain.CandidateDecisionResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	candidateID := strings.TrimSpace(input.CandidateID)
	if !uuidPattern.MatchString(candidateID) {
		return nil, BadRequest("invalid_candidate_id", "candidate_id must be a valid UUID")
	}
	body, err := decodeReviewCandidate(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateReviewCandidate(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/identity/candidates/%s/reject", candidateID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, rejectCandidateCommand, route, candidateID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s", tenantID, rejectCandidateCommand, candidateID, clientKey)
	result, err := s.repo.RejectCandidate(ctx, ports.RejectCandidateCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     rejectCandidateCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		CandidateID:          candidateID,
		Reason:               body.Reason,
		EvidenceRefs:         *body.EvidenceRefs,
		RowVersion:           *body.RowVersion,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CandidateDecisionResponse{
		CandidateID: result.Candidate.CandidateID,
		State:       result.Candidate.State,
		Decision:    result.Decision,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

type approveCandidateBody struct {
	DecisionType string                `json:"decision_type"`
	Reason       string                `json:"reason"`
	EvidenceRefs *[]domain.EvidenceRef `json:"evidence_refs"`
	RowVersion   *int                  `json:"row_version"`
	// attach_identifier fields
	TargetGoatID         *string `json:"target_goat_id"`
	GoatRowVersion       *int    `json:"goat_row_version"`
	IdentifierType       *string `json:"identifier_type"`
	IdentifierValue      *string `json:"identifier_value"`
	ScopeKey             *string `json:"scope_key"`
	IsPrimaryForGoat     bool    `json:"is_primary_for_goat"`
	ExtractFromLegacyRow bool    `json:"extract_from_legacy_row"`
	// merge_goats fields
	SurvivorGoatID    *string                   `json:"survivor_goat_id"`
	AffectedGoatIDs   []string                  `json:"affected_goat_ids"`
	IdentifierActions []domain.IdentifierAction `json:"identifier_actions"`
}

// ApproveCandidate applies a safe, non-create approve outcome for an identity
// match candidate: attach an explicit (or deterministically extracted) identifier
// to an existing goat, or merge the candidate's two goats using the existing
// merge invariants. Approve-to-create is intentionally NOT a decision_type here;
// minting a new goat stays blocked on the conflict create_goat contract.
func (s *Service) ApproveCandidate(ctx context.Context, input ReviewCandidateInput) (*domain.CandidateDecisionResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	candidateID := strings.TrimSpace(input.CandidateID)
	if !uuidPattern.MatchString(candidateID) {
		return nil, BadRequest("invalid_candidate_id", "candidate_id must be a valid UUID")
	}
	body, err := decodeApproveCandidate(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateApproveCandidate(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/identity/candidates/%s/approve", candidateID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, approveCandidateCommand, route, candidateID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s", tenantID, approveCandidateCommand, candidateID, clientKey)

	cmd := ports.ApproveCandidateCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     approveCandidateCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		CandidateID:          candidateID,
		DecisionType:         body.DecisionType,
		Reason:               body.Reason,
		EvidenceRefs:         *body.EvidenceRefs,
		RowVersion:           *body.RowVersion,
	}
	switch body.DecisionType {
	case "attach_identifier":
		cmd.GoatID = strings.TrimSpace(*body.TargetGoatID)
		cmd.GoatRowVersion = *body.GoatRowVersion
		cmd.IsPrimaryForGoat = body.IsPrimaryForGoat
		cmd.ExtractFromLegacyRow = body.ExtractFromLegacyRow
		if !body.ExtractFromLegacyRow {
			cmd.IdentifierType = strings.TrimSpace(*body.IdentifierType)
			cmd.IdentifierValue = strings.TrimSpace(*body.IdentifierValue)
			cmd.NormalizedValue = normalizeIdentifier(cmd.IdentifierType, cmd.IdentifierValue)
			if body.ScopeKey != nil {
				cmd.ScopeKey = strings.TrimSpace(*body.ScopeKey)
			}
		}
	case "merge_goats":
		cmd.SurvivorGoatID = strings.TrimSpace(*body.SurvivorGoatID)
		cmd.AffectedGoatIDs = body.AffectedGoatIDs
		cmd.IdentifierActions = body.IdentifierActions
	}

	result, err := s.repo.ApproveCandidate(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.CandidateDecisionResponse{
		CandidateID: result.Candidate.CandidateID,
		State:       result.Candidate.State,
		Decision:    result.Decision,
		Merge:       result.Merge,
		Events:      result.Events,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

func decodeApproveCandidate(raw []byte) (*approveCandidateBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body approveCandidateBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ApproveCandidateRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateApproveCandidate(body *approveCandidateBody) error {
	body.DecisionType = strings.TrimSpace(body.DecisionType)
	if body.DecisionType != "attach_identifier" && body.DecisionType != "merge_goats" {
		// create_goat / approve-to-create stays blocked: minting a new goat needs
		// the conflict create_goat field set.
		return BadRequest("invalid_decision_type", "decision_type must be attach_identifier or merge_goats; approve-to-create is not supported")
	}
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 2000 {
		return BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	if err := validateEvidenceRefs(*body.EvidenceRefs, true); err != nil {
		return err
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}

	switch body.DecisionType {
	case "attach_identifier":
		if len(body.AffectedGoatIDs) > 0 || body.SurvivorGoatID != nil || len(body.IdentifierActions) > 0 {
			return BadRequest("unexpected_merge_fields", "merge fields are not allowed for attach_identifier")
		}
		if err := validateOptionalUUID("target_goat_id", body.TargetGoatID); err != nil {
			return err
		}
		if body.TargetGoatID == nil || strings.TrimSpace(*body.TargetGoatID) == "" {
			return BadRequest("missing_target_goat_id", "target_goat_id is required for attach_identifier")
		}
		if body.GoatRowVersion == nil || *body.GoatRowVersion < 1 {
			return BadRequest("invalid_goat_row_version", "goat_row_version must be at least 1 for attach_identifier")
		}
		hasExplicit := body.IdentifierType != nil || body.IdentifierValue != nil
		if body.ExtractFromLegacyRow == hasExplicit {
			return BadRequest("invalid_identifier_source", "provide exactly one of an explicit identifier (identifier_type+identifier_value) or extract_from_legacy_row")
		}
		if hasExplicit {
			if body.IdentifierType == nil || !allowedIdentifierTypes[strings.TrimSpace(*body.IdentifierType)] {
				return BadRequest("invalid_identifier_type", "identifier_type is required and must be supported for an explicit attach")
			}
			if body.IdentifierValue == nil || strings.TrimSpace(*body.IdentifierValue) == "" || len(strings.TrimSpace(*body.IdentifierValue)) > 200 {
				return BadRequest("invalid_identifier_value", "identifier_value must be between 1 and 200 characters")
			}
			if body.ScopeKey != nil && len(strings.TrimSpace(*body.ScopeKey)) > 200 {
				return BadRequest("invalid_scope_key", "scope_key must be at most 200 characters")
			}
		}
	case "merge_goats":
		if body.TargetGoatID != nil || body.IdentifierType != nil || body.IdentifierValue != nil || body.ScopeKey != nil || body.GoatRowVersion != nil || body.ExtractFromLegacyRow || body.IsPrimaryForGoat {
			return BadRequest("unexpected_attach_fields", "attach fields are not allowed for merge_goats")
		}
		if err := validateOptionalUUID("survivor_goat_id", body.SurvivorGoatID); err != nil {
			return err
		}
		if body.SurvivorGoatID == nil || strings.TrimSpace(*body.SurvivorGoatID) == "" {
			return BadRequest("missing_survivor_goat_id", "survivor_goat_id is required for merge_goats")
		}
		survivorID := strings.TrimSpace(*body.SurvivorGoatID)
		if len(body.AffectedGoatIDs) == 0 {
			return BadRequest("missing_affected_goat_ids", "affected_goat_ids must contain at least one goat")
		}
		seen := map[string]struct{}{}
		hasLoser := false
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
		if !hasLoser {
			return BadRequest("missing_merged_goat", "affected_goat_ids must include at least one goat distinct from survivor_goat_id")
		}
		for i := range body.IdentifierActions {
			if err := validateIdentifierAction(&body.IdentifierActions[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeReviewCandidate(raw []byte) (*reviewCandidateBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body reviewCandidateBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ReviewCandidateRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateReviewCandidate(body *reviewCandidateBody) error {
	body.Reason = strings.TrimSpace(body.Reason)
	if body.Reason == "" || len(body.Reason) > 2000 {
		return BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}
	if body.EvidenceRefs == nil {
		return BadRequest("missing_evidence_refs", "evidence_refs is required")
	}
	if err := validateEvidenceRefs(*body.EvidenceRefs, true); err != nil {
		return err
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
	}
	return nil
}
