package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const maxBulkResolveConflicts = 200

var allowedBulkDecisionTypes = map[string]bool{
	"keep_passport_value":        true,
	"use_legacy_value":           true,
	"acknowledge_lifecycle_flag": true,
}

// BulkResolveConflictsInput carries the request for the bulk-resolve endpoint.
type BulkResolveConflictsInput struct {
	TenantID string
	ActorID  string
	TraceID  string
	RawBody  []byte
}

type bulkResolveBody struct {
	DecisionType string                    `json:"decision_type"`
	Conflicts    []bulkResolveConflictItem `json:"conflicts"`
	Reason       string                    `json:"reason"`
}

type bulkResolveConflictItem struct {
	ConflictID string `json:"conflict_id"`
	RowVersion *int   `json:"row_version"`
}

// BulkResolveConflicts applies one human decision to many conflicts. Reason and
// per-conflict row_version are required; the batch is bounded; the repository
// aborts the whole batch if any conflict is missing, stale, or invalid for the
// decision.
func (s *Service) BulkResolveConflicts(ctx context.Context, input BulkResolveConflictsInput) (*domain.BulkResolveConflictsResult, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	actorID := strings.TrimSpace(input.ActorID)
	if !uuidPattern.MatchString(actorID) {
		return nil, BadRequest("invalid_actor", "actor identity is required for bulk resolve")
	}

	body, err := decodeBulkResolve(input.RawBody)
	if err != nil {
		return nil, err
	}
	if !allowedBulkDecisionTypes[body.DecisionType] {
		return nil, BadRequest("invalid_decision_type", "decision_type must be keep_passport_value, use_legacy_value, or acknowledge_lifecycle_flag")
	}
	if len(body.Conflicts) == 0 {
		return nil, BadRequest("missing_conflicts", "conflicts must contain at least one item")
	}
	if len(body.Conflicts) > maxBulkResolveConflicts {
		return nil, BadRequest("too_many_conflicts", "conflicts must not exceed the bulk limit")
	}
	reason := strings.TrimSpace(body.Reason)
	if reason == "" || len(reason) > 2000 {
		return nil, BadRequest("invalid_reason", "reason must be between 1 and 2000 characters")
	}

	ids := make([]string, 0, len(body.Conflicts))
	rowVersions := make(map[string]int, len(body.Conflicts))
	for _, item := range body.Conflicts {
		id := strings.TrimSpace(item.ConflictID)
		if !uuidPattern.MatchString(id) {
			return nil, BadRequest("invalid_conflict_id", "each conflict_id must be a valid UUID")
		}
		if _, dup := rowVersions[id]; dup {
			return nil, BadRequest("duplicate_conflict_id", "conflicts must not contain duplicate conflict_id")
		}
		if item.RowVersion == nil || *item.RowVersion < 1 {
			return nil, BadRequest("invalid_row_version", "each conflict requires row_version >= 1")
		}
		ids = append(ids, id)
		rowVersions[id] = *item.RowVersion
	}

	result, err := s.repo.BulkResolveConflicts(ctx, ports.BulkResolveConflictsCommand{
		TenantID:      tenantID,
		ActorID:       actorID,
		TraceID:       input.TraceID,
		BulkRequestID: input.TraceID,
		DecisionType:  body.DecisionType,
		ConflictIDs:   ids,
		RowVersions:   rowVersions,
		Reason:        reason,
	})
	if err != nil {
		return nil, mapRepoErr(err)
	}
	result.TraceID = input.TraceID
	return result, nil
}

func decodeBulkResolve(raw []byte) (*bulkResolveBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body bulkResolveBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match BulkResolveConflictsRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	body.DecisionType = strings.TrimSpace(body.DecisionType)
	return &body, nil
}
