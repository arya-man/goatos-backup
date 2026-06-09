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

func (s *Service) ApproveCandidate(context.Context, ReviewCandidateInput) (*domain.CandidateDecisionResponse, error) {
	return nil, NotImplemented("candidate_approve_not_implemented", "candidate approval needs contract-defined canonical mutation semantics before implementation")
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
