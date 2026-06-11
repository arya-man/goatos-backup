package app

import (
	"context"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const maxImportRunRowsLimit = 500

var reasonCodePattern = regexp.MustCompile(`^[a-z0-9_:-]{1,200}$`)

func (s *Service) GetImportRun(ctx context.Context, tenantID, importRunID, traceID string) (*domain.ImportRunResponse, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, err
	}
	importRunID = strings.TrimSpace(importRunID)
	if !uuidPattern.MatchString(importRunID) {
		return nil, BadRequest("invalid_import_run_id", "import_run_id must be a valid UUID")
	}
	run, err := s.repo.GetImportRun(ctx, tenantID, importRunID)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.ImportRunResponse{ImportRun: *run, TraceID: traceID}, nil
}

func (s *Service) ListImportRunRows(ctx context.Context, params ports.ListImportRunRowsParams, traceID string) (*domain.ImportRunRowsResponse, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	params.ImportRunID = strings.TrimSpace(params.ImportRunID)
	if !uuidPattern.MatchString(params.ImportRunID) {
		return nil, BadRequest("invalid_import_run_id", "import_run_id must be a valid UUID")
	}
	if params.Limit < 1 || params.Limit > maxImportRunRowsLimit {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 500")
	}
	if params.ProcessingState != nil {
		state := strings.TrimSpace(*params.ProcessingState)
		if !validImportRowState(state) {
			return nil, BadRequest("invalid_processing_state", "processing_state is not supported")
		}
		params.ProcessingState = &state
	}
	if params.ReasonCode != nil {
		reason := strings.TrimSpace(*params.ReasonCode)
		if !reasonCodePattern.MatchString(reason) {
			return nil, BadRequest("invalid_reason_code", "reason_code must be a bounded reason code")
		}
		params.ReasonCode = &reason
	}
	items, next, err := s.repo.ListImportRunRows(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if items == nil {
		items = []domain.ImportRunRow{}
	}
	return &domain.ImportRunRowsResponse{Items: items, NextCursor: next, TraceID: traceID}, nil
}

func validImportRowState(state string) bool {
	switch state {
	case "pending", "auto_linked", "created_goat", "needs_review", "rejected", "error":
		return true
	default:
		return false
	}
}
