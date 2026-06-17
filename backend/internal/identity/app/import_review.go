package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const maxImportRunRowsLimit = 500
const maxImportRunsLimit = 50
const reviewImportRowCommand = "reviewImportRow"

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

func (s *Service) ListImportRuns(ctx context.Context, params ports.ListImportRunsParams, traceID string) (*domain.ImportRunListResponse, error) {
	if err := requireTenant(params.TenantID); err != nil {
		return nil, err
	}
	if params.Limit < 1 || params.Limit > maxImportRunsLimit {
		return nil, BadRequest("invalid_limit", "limit must be between 1 and 50")
	}
	items, err := s.repo.ListImportRuns(ctx, params)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if items == nil {
		items = []domain.ImportRun{}
	}
	return &domain.ImportRunListResponse{Items: items, TraceID: traceID}, nil
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

type ReviewImportRowInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	ImportRunID    string
	ImportRowID    string
	RawBody        []byte
}

type reviewImportRowBody struct {
	Action       string                `json:"action"`
	RowVersion   *int                  `json:"row_version"`
	Reason       string                `json:"reason"`
	EvidenceRefs *[]domain.EvidenceRef `json:"evidence_refs"`
	Sex          *string               `json:"sex"`
	Breed        *string               `json:"breed"`
}

// ReviewImportRow applies a safe import-row review action: reject (terminal),
// fix (patch whitelisted normalized sex/breed), or reapply (requeue an eligible
// needs_review row to pending for the approved RFID apply path). It never mints a
// goat from the request.
func (s *Service) ReviewImportRow(ctx context.Context, input ReviewImportRowInput) (*domain.ReviewImportRowResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	importRunID := strings.TrimSpace(input.ImportRunID)
	if !uuidPattern.MatchString(importRunID) {
		return nil, BadRequest("invalid_import_run_id", "import_run_id must be a valid UUID")
	}
	importRowID := strings.TrimSpace(input.ImportRowID)
	if !uuidPattern.MatchString(importRowID) {
		return nil, BadRequest("invalid_import_row_id", "import_row_id must be a valid UUID")
	}
	body, err := decodeReviewImportRow(input.RawBody)
	if err != nil {
		return nil, err
	}
	if err := validateReviewImportRow(body); err != nil {
		return nil, err
	}
	route := fmt.Sprintf("/admin/import-runs/%s/rows/%s/review", importRunID, importRowID)
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, reviewImportRowCommand, route, importRowID, input.RawBody)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	storedKey := fmt.Sprintf("%s:%s:%s:%s", tenantID, reviewImportRowCommand, importRowID, clientKey)

	cmd := ports.ReviewImportRowCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedKey,
		IdempotencyScope:     reviewImportRowCommand,
		RequestHash:          requestHash,
		TraceID:              input.TraceID,
		ImportRunID:          importRunID,
		ImportRowID:          importRowID,
		Action:               body.Action,
		RowVersion:           *body.RowVersion,
		Reason:               body.Reason,
		EvidenceRefs:         *body.EvidenceRefs,
	}
	if body.Action == "fix" {
		cmd.FixSex = body.Sex
		cmd.FixBreed = body.Breed
	}
	result, err := s.repo.ReviewImportRow(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return &domain.ReviewImportRowResponse{
		Row: result.Row,
		Idempotency: domain.IdempotencyMeta{
			IdempotencyKey: clientKey,
			Replayed:       result.Replayed,
			FirstResultID:  result.FirstResultID,
		},
		TraceID: input.TraceID,
	}, nil
}

func decodeReviewImportRow(raw []byte) (*reviewImportRowBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var body reviewImportRowBody
	if err := decoder.Decode(&body); err != nil {
		return nil, BadRequest("invalid_json", "request body must match ReviewImportRowRequest")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return &body, nil
}

func validateReviewImportRow(body *reviewImportRowBody) error {
	body.Action = strings.TrimSpace(body.Action)
	switch body.Action {
	case "reject", "fix", "reapply":
	default:
		return BadRequest("invalid_action", "action must be reject, fix, or reapply")
	}
	if body.RowVersion == nil || *body.RowVersion < 1 {
		return BadRequest("invalid_row_version", "row_version must be at least 1")
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
	if body.Action == "fix" {
		if err := normalizeFixSex(body.Sex); err != nil {
			return err
		}
		if err := normalizeFixField("breed", body.Breed); err != nil {
			return err
		}
		if body.Sex == nil && body.Breed == nil {
			return BadRequest("missing_fix_fields", "fix requires at least one of sex or breed")
		}
	} else if body.Sex != nil || body.Breed != nil {
		return BadRequest("unexpected_fix_fields", "sex and breed are only allowed for the fix action")
	}
	return nil
}

func normalizeFixSex(value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.ToLower(strings.TrimSpace(*value))
	switch trimmed {
	case "male", "female":
		*value = trimmed
		return nil
	default:
		return BadRequest("invalid_sex", "sex must be male or female")
	}
}

func normalizeFixField(name string, value *string) error {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" || len(trimmed) > 200 {
		return BadRequest("invalid_"+name, name+" must be between 1 and 200 characters")
	}
	*value = trimmed
	return nil
}
