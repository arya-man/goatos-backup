package app

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	createAdminGoatCommand                = "createAdminGoat"
	previewAdminGoatBulkCommand           = "previewAdminGoatBulkImport"
	commitAdminGoatBulkCommand            = "commitAdminGoatBulkImport"
	maxBulkRows                           = 500
	adminGoatBulkPreviewTokenV1           = "v1"
	adminGoatBulkPreviewTokenMaxAge       = 30 * time.Minute
	adminGoatBulkPreviewTokenClockSkew    = 2 * time.Minute
	adminGoatBulkPreviewTokenNonceBytes   = 16
	defaultAdminGoatBulkPreviewSigningKey = "goatos-admin-goat-bulk-preview-dev-v1"
)

// DevBulkPreviewSigningKey is the local/test-only fallback injected by bootstrap when
// GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY is intentionally unset in a non-shared environment.
func DevBulkPreviewSigningKey() string {
	return defaultAdminGoatBulkPreviewSigningKey
}

type CreateAdminGoatInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type PreviewAdminGoatBulkInput struct {
	TenantID string
	TraceID  string
	RawBody  []byte
}

type CommitAdminGoatBulkInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type adminGoatRepository interface {
	ValidateAdminGoatCreate(ctx context.Context, cmd ports.ValidateAdminGoatCreateCommand) (ports.AdminGoatCreateValidation, error)
	CreateAdminGoat(ctx context.Context, cmd ports.CreateAdminGoatCommand) (*ports.AdminGoatMutationResult, error)
}

func (s *Service) adminGoatRepository() (adminGoatRepository, error) {
	repo, ok := s.repo.(adminGoatRepository)
	if !ok {
		return nil, NotImplemented("admin_goat_create_unavailable", "admin goat create repository is not configured")
	}
	return repo, nil
}

// reproductiveBulkMatcher is an optional capability: when the repository
// implements it, bulk-import rows carrying reproductive_status that match an
// existing goat are applied as a reproductive transition instead of being
// rejected as a create conflict.
type reproductiveBulkMatcher interface {
	ResolveGoatForReproductiveBulkUpdate(ctx context.Context, cmd ports.ResolveReproductiveMatchCommand) (ports.ReproductiveMatchResult, error)
}

func (s *Service) reproductiveBulkMatcher() (reproductiveBulkMatcher, bool) {
	matcher, ok := s.repo.(reproductiveBulkMatcher)
	return matcher, ok
}

const bulkImportUpdateReproductiveDecision = "update_reproductive"

// reproductiveUpdateCandidate reports whether a normalized row explicitly
// carries a reproductive_status (the only trigger for the matched-existing
// reproductive update path; rows without it keep their original create-conflict
// behavior).
func reproductiveUpdateCandidate(normalized *domain.AdminGoatCreateRequest) bool {
	return normalized != nil && normalized.ReproductiveStatus != nil && strings.TrimSpace(*normalized.ReproductiveStatus) != ""
}

// onlyIdentifierOwnershipConflicts reports whether every conflict is an
// identifier-already-owned conflict (i.e. the row is a clean match against an
// existing goat rather than a bad reference).
func onlyIdentifierOwnershipConflicts(conflicts []domain.FieldError) bool {
	if len(conflicts) == 0 {
		return false
	}
	for _, conflict := range conflicts {
		if conflict.Code != "identifier_already_owned" {
			return false
		}
	}
	return true
}

func (s *Service) CreateAdminGoat(ctx context.Context, input CreateAdminGoatInput) (*domain.AdminGoatResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeCreateAdminGoat(input.RawBody)
	if err != nil {
		return nil, err
	}
	repo, err := s.adminGoatRepository()
	if err != nil {
		return nil, err
	}
	normalized, cmd, fieldErrors, err := s.normalizeAdminGoatCreate(ctx, tenantID, actorID, clientKey, input.TraceID, body)
	if err != nil {
		return nil, err
	}
	if len(fieldErrors) > 0 {
		return nil, BadRequest("invalid_goat_create", fieldErrors[0].Message)
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil, Internal("goat create request normalization failed")
	}
	requestHash, err := CanonicalRequestHashWithSubject(tenantID, createAdminGoatCommand, "/admin/goats", "", raw)
	if err != nil {
		return nil, BadRequest("invalid_json", "request body must be valid JSON")
	}
	cmd.RequestHash = requestHash
	cmd.StoredIdempotencyKey = fmt.Sprintf("%s:%s:%s", tenantID, createAdminGoatCommand, clientKey)
	cmd.IdempotencyScope = createAdminGoatCommand
	fieldErrors, _, err = validateAdminGoatCreate(ctx, repo, normalized, &cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	if len(fieldErrors) > 0 {
		return nil, BadRequest("invalid_goat_create", fieldErrors[0].Message)
	}
	result, err := repo.CreateAdminGoat(ctx, cmd)
	if err != nil {
		return nil, mapRepoErr(err)
	}
	return adminGoatResponse(result, clientKey, input.TraceID), nil
}

func (s *Service) PreviewAdminGoatBulkImport(ctx context.Context, input PreviewAdminGoatBulkInput) (*domain.AdminGoatBulkResponse, error) {
	if err := requireTenant(input.TenantID); err != nil {
		return nil, err
	}
	repo, err := s.adminGoatRepository()
	if err != nil {
		return nil, err
	}
	body, err := decodeBulkPreview(input.RawBody)
	if err != nil {
		return nil, err
	}
	rows, err := parseAdminGoatCSV(body.CSV)
	if err != nil {
		return nil, err
	}
	if len(rows) > maxBulkRows {
		return nil, BadRequest("bulk_too_large", fmt.Sprintf("bulk preview supports at most %d rows", maxBulkRows))
	}
	response := &domain.AdminGoatBulkResponse{Rows: make([]domain.AdminGoatBulkRowResult, 0, len(rows)), TraceID: input.TraceID}
	seenIdentifiers := map[string]int{}
	for i := range rows {
		rowNumber := rows[i].RowNumber
		normalized, cmd, fieldErrors, err := s.normalizeAdminGoatCreate(ctx, input.TenantID, "", "", input.TraceID, &rows[i].Request)
		result := domain.AdminGoatBulkRowResult{
			RowNumber:        rowNumber,
			Decision:         "create",
			Errors:           []domain.FieldError{},
			Warnings:         []domain.Warning{},
			Normalized:       normalized,
			GenerationStatus: "queued",
		}
		if err != nil {
			result.Decision = "requires_review"
			result.Errors = []domain.FieldError{{Field: "row", Code: "invalid", Message: err.Error()}}
		}
		if len(fieldErrors) > 0 {
			result.Decision = "requires_review"
			result.Errors = fieldErrors
		}
		if len(result.Errors) == 0 {
			if duplicateErrors := duplicateAdminGoatImportErrors(normalized, rowNumber, seenIdentifiers); len(duplicateErrors) > 0 {
				result.Decision = "requires_review"
				result.Errors = duplicateErrors
				result.GenerationStatus = "skipped_needs_review"
			}
		}
		if len(result.Errors) == 0 {
			conflicts, warnings, vErr := validateAdminGoatCreate(ctx, repo, normalized, &cmd)
			if vErr != nil {
				return nil, mapRepoErr(vErr)
			}
			if len(conflicts) > 0 {
				result.Decision = "requires_review"
				result.Errors = conflicts
				result.GenerationStatus = "skipped_needs_review"
			}
			result.Warnings = append(result.Warnings, warnings...)
		}
		if result.Decision == "requires_review" && reproductiveUpdateCandidate(normalized) && onlyIdentifierOwnershipConflicts(result.Errors) {
			if err := s.previewReproductiveBulkUpdate(ctx, input.TenantID, normalized, &result); err != nil {
				return nil, mapRepoErr(err)
			}
		}
		switch result.Decision {
		case "create":
			response.Summary.CreateReady++
		case bulkImportUpdateReproductiveDecision:
			response.Summary.UpdateReady++
		case "requires_review":
			response.Summary.RequiresReview++
		default:
			response.Summary.Skipped++
		}
		response.Rows = append(response.Rows, result)
	}
	response.Summary.Total = len(response.Rows)
	response.PreviewToken, err = s.signAdminGoatBulkPreview(input.TenantID, body.FileHash, previewCommitRows(response.Rows))
	if err != nil {
		return nil, Internal("bulk preview token generation failed")
	}
	return response, nil
}

func (s *Service) CommitAdminGoatBulkImport(ctx context.Context, input CommitAdminGoatBulkInput) (*domain.AdminGoatBulkResponse, error) {
	tenantID, actorID, clientKey, err := validateWriteHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	repo, err := s.adminGoatRepository()
	if err != nil {
		return nil, err
	}
	body, err := decodeBulkCommit(input.RawBody)
	if err != nil {
		return nil, err
	}
	if len(body.Rows) > maxBulkRows {
		return nil, BadRequest("bulk_too_large", fmt.Sprintf("bulk commit supports at most %d rows", maxBulkRows))
	}
	previewRows, err := s.normalizeBulkCommitRowsForPreviewToken(ctx, tenantID, input.TraceID, body.Rows)
	if err != nil {
		return nil, err
	}
	if err := s.verifyAdminGoatBulkPreviewToken(tenantID, body.FileHash, body.PreviewToken, previewRows); err != nil {
		return nil, err
	}
	body.Rows = previewRows
	response := &domain.AdminGoatBulkResponse{Rows: make([]domain.AdminGoatBulkRowResult, 0, len(body.Rows)), TraceID: input.TraceID}
	for i := range body.Rows {
		rowNumber, request, rowErrors := adminGoatBulkCommitRowRequest(body.Rows[i], i)
		rowResult := domain.AdminGoatBulkRowResult{
			RowNumber:        rowNumber,
			Decision:         "create",
			Errors:           []domain.FieldError{},
			Warnings:         []domain.Warning{},
			GenerationStatus: "queued",
		}
		if len(rowErrors) > 0 {
			rowResult.Decision = "requires_review"
			rowResult.Errors = rowErrors
			rowResult.GenerationStatus = "skipped_needs_review"
			response.Summary.RequiresReview++
			response.Rows = append(response.Rows, rowResult)
			continue
		}
		normalized, cmd, fieldErrors, err := s.normalizeAdminGoatCreate(ctx, tenantID, actorID, clientKey, input.TraceID, request)
		rowResult.Normalized = normalized
		if err != nil {
			rowResult.Decision = "requires_review"
			rowResult.Errors = []domain.FieldError{{Field: "row", Code: "invalid", Message: err.Error()}}
		}
		if len(fieldErrors) > 0 {
			rowResult.Decision = "requires_review"
			rowResult.Errors = fieldErrors
		}
		if len(rowResult.Errors) > 0 {
			rowResult.GenerationStatus = "skipped_needs_review"
			response.Summary.RequiresReview++
			response.Rows = append(response.Rows, rowResult)
			continue
		}
		raw, err := json.Marshal(normalized)
		if err != nil {
			return nil, Internal("bulk row normalization failed")
		}
		rowKey := rowIdempotencyKey(clientKey, rowNumber)
		requestHash, err := CanonicalRequestHashWithSubject(tenantID, createAdminGoatCommand, "/admin/goats/bulk-commit", fmt.Sprintf("row:%d", rowNumber), raw)
		if err != nil {
			return nil, BadRequest("invalid_json", "bulk row must be valid JSON")
		}
		cmd.ClientIdempotencyKey = rowKey
		cmd.StoredIdempotencyKey = fmt.Sprintf("%s:%s:%s:%s", tenantID, commitAdminGoatBulkCommand, clientKey, rowKey)
		cmd.IdempotencyScope = commitAdminGoatBulkCommand
		cmd.RequestHash = requestHash
		conflicts, warnings, err := validateAdminGoatCreate(ctx, repo, normalized, &cmd)
		rowResult.Warnings = append(rowResult.Warnings, warnings...)
		if err != nil {
			rowResult.Decision = "requires_review"
			rowResult.GenerationStatus = "skipped_needs_review"
			rowResult.Errors = []domain.FieldError{{Field: "row", Code: "commit_failed", Message: mapRepoErr(err).Error()}}
			response.Summary.Failed++
			response.Rows = append(response.Rows, rowResult)
			continue
		}
		if len(conflicts) > 0 && reproductiveUpdateCandidate(normalized) && onlyIdentifierOwnershipConflicts(conflicts) {
			handled, err := s.commitReproductiveBulkUpdate(ctx, tenantID, actorID, clientKey, input.TraceID, rowNumber, normalized, &rowResult, response)
			if err != nil {
				return nil, err
			}
			if handled {
				response.Rows = append(response.Rows, rowResult)
				continue
			}
		}
		if len(conflicts) > 0 {
			rowResult.Decision = "requires_review"
			rowResult.GenerationStatus = "skipped_needs_review"
			rowResult.Errors = conflicts
			response.Summary.RequiresReview++
			response.Rows = append(response.Rows, rowResult)
			continue
		}
		result, err := repo.CreateAdminGoat(ctx, cmd)
		if err != nil {
			rowResult.Decision = "requires_review"
			rowResult.GenerationStatus = "skipped_needs_review"
			rowResult.Errors = []domain.FieldError{{Field: "row", Code: "commit_failed", Message: mapRepoErr(err).Error()}}
			response.Summary.Failed++
			response.Rows = append(response.Rows, rowResult)
			continue
		}
		rowResult.Result = adminGoatResponse(result, rowKey, input.TraceID)
		rowResult.GenerationStatus = result.GenerationStatus
		response.Summary.Created++
		response.Rows = append(response.Rows, rowResult)
	}
	response.Summary.Total = len(response.Rows)
	response.PreviewToken = body.PreviewToken
	return response, nil
}

func (s *Service) normalizeAdminGoatCreate(_ context.Context, tenantID, actorID, clientKey, traceID string, body *domain.AdminGoatCreateRequest) (*domain.AdminGoatCreateRequest, ports.CreateAdminGoatCommand, []domain.FieldError, error) {
	normalized := *body
	errorsOut := make([]domain.FieldError, 0)
	trimOptionalString(&normalized.AnimalIdentifier1)
	trimOptionalString(&normalized.AnimalIdentifier2)
	trimOptionalString(&normalized.FarmID)
	trimOptionalString(&normalized.FarmCode)
	trimOptionalString(&normalized.ParkID)
	trimOptionalString(&normalized.ParkCode)
	trimOptionalString(&normalized.ShedID)
	trimOptionalString(&normalized.ShedCode)
	trimOptionalString(&normalized.Breed)
	trimOptionalString(&normalized.ManagementStage)
	trimOptionalString(&normalized.HealthStatus)
	trimOptionalString(&normalized.ReproductiveStatus)
	trimOptionalString(&normalized.DamID)
	trimOptionalString(&normalized.SireOrLot)
	trimOptionalString(&normalized.PhotoURL)
	trimOptionalString(&normalized.SourceRecordID)
	normalized.Species = strings.TrimSpace(normalized.Species)
	normalized.Sex = strings.TrimSpace(normalized.Sex)
	normalized.OriginType = strings.TrimSpace(normalized.OriginType)
	normalized.EntryDate = strings.TrimSpace(normalized.EntryDate)
	if normalized.DOBEstimated == nil {
		estimated := false
		normalized.DOBEstimated = &estimated
	}
	if normalized.AnimalIdentifier1 == nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "animal_identifier_1", Code: "required", Message: "Animal ID 1 is required"})
	}
	// Animal ID 2 stays optional until the double RFID tagging rollout is live.
	// That rollout must make it mandatory in app validation and with a DB invariant.
	if normalized.AnimalIdentifier1 != nil && normalized.AnimalIdentifier2 != nil &&
		normalizeIdentifier("animal_identifier_1", *normalized.AnimalIdentifier1) == normalizeIdentifier("animal_identifier_2", *normalized.AnimalIdentifier2) {
		errorsOut = append(errorsOut, domain.FieldError{Field: "animal_identifier_2", Code: "duplicate", Message: "Animal ID 1 and Animal ID 2 must be different"})
	}
	if normalized.ParkID == nil && normalized.ParkCode == nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "park_id", Code: "required", Message: "park_id or park_code is required"})
	}
	if normalized.ParkID != nil && !uuidPattern.MatchString(*normalized.ParkID) {
		errorsOut = append(errorsOut, domain.FieldError{Field: "park_id", Code: "invalid", Message: "park_id must be a UUID"})
	}
	if normalized.ShedID == nil && normalized.ShedCode == nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "shed_id", Code: "required", Message: "shed_id or shed_code is required"})
	}
	if normalized.ShedID != nil && !uuidPattern.MatchString(*normalized.ShedID) {
		errorsOut = append(errorsOut, domain.FieldError{Field: "shed_id", Code: "invalid", Message: "shed_id must be a UUID"})
	}
	if normalized.FarmID != nil && !uuidPattern.MatchString(*normalized.FarmID) {
		errorsOut = append(errorsOut, domain.FieldError{Field: "farm_id", Code: "invalid", Message: "farm_id must be a UUID"})
	}
	if normalized.Sex == "" {
		errorsOut = append(errorsOut, domain.FieldError{Field: "sex", Code: "required", Message: "sex is required and must be female or male"})
	} else if !allowedSex[normalized.Sex] {
		errorsOut = append(errorsOut, domain.FieldError{Field: "sex", Code: "invalid", Message: "sex must be female or male"})
	}
	if normalized.Species == "" {
		errorsOut = append(errorsOut, domain.FieldError{Field: "species", Code: "required", Message: "species is required and must be goat or sheep"})
	} else if !allowedSpecies[normalized.Species] {
		errorsOut = append(errorsOut, domain.FieldError{Field: "species", Code: "invalid", Message: "species must be goat or sheep"})
	}
	if !allowedOriginType[normalized.OriginType] {
		errorsOut = append(errorsOut, domain.FieldError{Field: "origin_type", Code: "invalid", Message: "origin_type must be birth, procured, or imported"})
	}
	entryDate, err := parseDateField("entry_date", normalized.EntryDate)
	if err != nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "entry_date", Code: "invalid", Message: err.Error()})
	}
	var dob *time.Time
	if normalized.DOB == nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "dob", Code: "required", Message: "dob is required"})
	} else {
		parsed, err := parseDateField("dob", *normalized.DOB)
		if err != nil {
			errorsOut = append(errorsOut, domain.FieldError{Field: "dob", Code: "invalid", Message: err.Error()})
		} else {
			dob = &parsed
		}
	}
	if dob != nil && !entryDate.IsZero() && dob.After(entryDate) {
		errorsOut = append(errorsOut, domain.FieldError{Field: "dob", Code: "invalid", Message: "dob cannot be after entry_date"})
	}
	if normalized.WeightKg != nil && *normalized.WeightKg < 0 {
		errorsOut = append(errorsOut, domain.FieldError{Field: "weight_kg", Code: "invalid", Message: "weight_kg must be non-negative"})
	}
	if err := validateEvidenceRefs(normalized.EvidenceRefs, true); err != nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "evidence_refs", Code: "invalid", Message: err.Error()})
	}
	identifiers := make([]ports.AdminGoatCreateIdentifier, 0, 2)
	if normalized.AnimalIdentifier1 != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{IdentifierType: "animal_identifier_1", IdentifierValue: *normalized.AnimalIdentifier1, NormalizedValue: normalizeIdentifier("animal_identifier_1", *normalized.AnimalIdentifier1), ScopeKey: "global", IsPrimary: true})
	}
	if normalized.AnimalIdentifier2 != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{IdentifierType: "animal_identifier_2", IdentifierValue: *normalized.AnimalIdentifier2, NormalizedValue: normalizeIdentifier("animal_identifier_2", *normalized.AnimalIdentifier2), ScopeKey: "global", IsPrimary: false})
	}
	cmd := ports.CreateAdminGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		TraceID:              traceID,
		Identifiers:          identifiers,
		FarmID:               normalized.FarmID,
		Species:              normalized.Species,
		Breed:                normalized.Breed,
		Sex:                  normalized.Sex,
		DOB:                  dob,
		DOBEstimated:         *normalized.DOBEstimated,
		OriginType:           normalized.OriginType,
		EntryDate:            entryDate,
		ManagementStage:      normalized.ManagementStage,
		HealthStatus:         normalized.HealthStatus,
		WeightKg:             normalized.WeightKg,
		DamID:                normalized.DamID,
		SireOrLot:            normalized.SireOrLot,
		PhotoURL:             normalized.PhotoURL,
		SourceRecordID:       normalized.SourceRecordID,
		EvidenceRefs:         normalized.EvidenceRefs,
	}
	if len(errorsOut) > 0 {
		return &normalized, cmd, errorsOut, nil
	}
	return &normalized, cmd, nil, nil
}

func validateAdminGoatCreate(ctx context.Context, repo adminGoatRepository, normalized *domain.AdminGoatCreateRequest, cmd *ports.CreateAdminGoatCommand) ([]domain.FieldError, []domain.Warning, error) {
	validation, err := repo.ValidateAdminGoatCreate(ctx, ports.ValidateAdminGoatCreateCommand{
		TenantID:             cmd.TenantID,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		RequestHash:          cmd.RequestHash,
		Identifiers:          cmd.Identifiers,
		FarmID:               normalized.FarmID,
		FarmCode:             normalized.FarmCode,
		ParkID:               normalized.ParkID,
		ParkCode:             normalized.ParkCode,
		ShedID:               normalized.ShedID,
		ShedCode:             normalized.ShedCode,
		ManagementStage:      normalized.ManagementStage,
	})
	if err != nil {
		return nil, nil, err
	}
	if len(validation.Conflicts) > 0 {
		return validation.Conflicts, validation.Warnings, nil
	}
	cmd.CustodianPartyID = validation.CustodianPartyID
	cmd.FarmID = validation.FarmID
	cmd.ParkID = validation.ParkID
	cmd.ShedID = validation.ShedID
	return nil, validation.Warnings, nil
}

func decodeCreateAdminGoat(raw []byte) (*domain.AdminGoatCreateRequest, error) {
	var body domain.AdminGoatCreateRequest
	if err := decodeStrict(raw, &body, "CreateAdminGoatRequest"); err != nil {
		return nil, err
	}
	return &body, nil
}

func decodeBulkPreview(raw []byte) (*domain.AdminGoatBulkPreviewRequest, error) {
	var body domain.AdminGoatBulkPreviewRequest
	if err := decodeStrict(raw, &body, "AdminGoatBulkPreviewRequest"); err != nil {
		return nil, err
	}
	fileHash, err := normalizeBulkFileHash(body.FileHash)
	if err != nil {
		return nil, err
	}
	body.FileHash = fileHash
	if strings.TrimSpace(body.CSV) == "" {
		return nil, BadRequest("missing_csv", "csv is required")
	}
	return &body, nil
}

func decodeBulkCommit(raw []byte) (*domain.AdminGoatBulkCommitRequest, error) {
	var body domain.AdminGoatBulkCommitRequest
	if err := decodeStrict(raw, &body, "AdminGoatBulkCommitRequest"); err != nil {
		return nil, err
	}
	fileHash, err := normalizeBulkFileHash(body.FileHash)
	if err != nil {
		return nil, err
	}
	body.FileHash = fileHash
	previewToken, err := normalizeBulkPreviewToken(body.PreviewToken)
	if err != nil {
		return nil, err
	}
	body.PreviewToken = previewToken
	if len(body.Rows) == 0 {
		return nil, BadRequest("missing_rows", "rows must contain at least one item")
	}
	return &body, nil
}

func normalizeBulkFileHash(raw string) (string, error) {
	hash := strings.ToLower(strings.TrimSpace(raw))
	if len(hash) != 64 {
		return "", BadRequest("invalid_file_hash", "file_hash must be the SHA-256 hash returned by bulk preview")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", BadRequest("invalid_file_hash", "file_hash must be the SHA-256 hash returned by bulk preview")
	}
	return hash, nil
}

func normalizeBulkPreviewToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	prefix := adminGoatBulkPreviewTokenV1 + ":"
	if !strings.HasPrefix(token, prefix) {
		return "", BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	parts := strings.Split(token, ":")
	if len(parts) != 4 || parts[0] != adminGoatBulkPreviewTokenV1 {
		return "", BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
		return "", BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	if decoded, err := hex.DecodeString(parts[2]); err != nil || len(decoded) != adminGoatBulkPreviewTokenNonceBytes {
		return "", BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	if decoded, err := hex.DecodeString(parts[3]); err != nil || len(decoded) != sha256.Size {
		return "", BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	return token, nil
}

func decodeStrict(raw []byte, dest any, schemaName string) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return BadRequest("invalid_json", "request body is required")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dest); err != nil {
		return BadRequest("invalid_json", "request body must match "+schemaName)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return BadRequest("invalid_json", "request body must contain a single JSON object")
	}
	return nil
}

type parsedAdminGoatCSVRow struct {
	RowNumber int
	Request   domain.AdminGoatCreateRequest
}

func parseAdminGoatCSV(raw string) ([]parsedAdminGoatCSVRow, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil && err != io.EOF {
		return nil, BadRequest("invalid_csv", "csv could not be parsed")
	}
	if err == io.EOF {
		return nil, BadRequest("invalid_csv", "csv must include a header and at least one row")
	}
	headers := map[string]int{}
	for i, h := range header {
		headers[normalizeHeader(h)] = i
	}
	out := make([]parsedAdminGoatCSVRow, 0)
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, BadRequest("invalid_csv", "csv could not be parsed")
		}
		if csvRecordBlank(rec) {
			continue
		}
		rowNumber, _ := reader.FieldPos(0)
		sourceID := fmt.Sprintf("bulk-csv-row:%d", rowNumber)
		req := domain.AdminGoatCreateRequest{
			AnimalIdentifier1:  optionalCSV(rec, headers, "animal_identifier_1"),
			AnimalIdentifier2:  optionalCSV(rec, headers, "animal_identifier_2"),
			FarmCode:           optionalCSV(rec, headers, "farm"),
			Species:            valueCSV(rec, headers, "species"),
			ParkID:             optionalCSV(rec, headers, "park_id"),
			ParkCode:           optionalCSV(rec, headers, "park"),
			ShedID:             optionalCSV(rec, headers, "shed_id"),
			ShedCode:           optionalCSV(rec, headers, "shed"),
			Breed:              optionalCSV(rec, headers, "breed"),
			ManagementStage:    optionalCSV(rec, headers, "management_stage"),
			ReproductiveStatus: optionalCSV(rec, headers, "reproductive_status"),
			Sex:                valueCSV(rec, headers, "sex"),
			DOB:                optionalCSV(rec, headers, "dob"),
			OriginType:         valueCSV(rec, headers, "origin"),
			PhotoURL:           optionalCSV(rec, headers, "photo_url"),
			SourceRecordID:     &sourceID,
			EvidenceRefs: []domain.EvidenceRef{{
				EvidenceType: "source_record",
				EvidenceID:   sourceID,
			}},
		}
		if entry := optionalCSV(rec, headers, "entry_date"); entry != nil {
			req.EntryDate = *entry
		} else {
			req.EntryDate = biztime.BusinessDate(time.Now())
		}
		if weight := optionalCSV(rec, headers, "weight_kg"); weight != nil {
			var parsed float64
			if _, err := fmt.Sscanf(*weight, "%f", &parsed); err == nil {
				req.WeightKg = &parsed
			}
		}
		req.DamID = optionalCSV(rec, headers, "dam_id")
		req.SireOrLot = optionalCSV(rec, headers, "sire_lot")
		out = append(out, parsedAdminGoatCSVRow{RowNumber: rowNumber, Request: req})
	}
	if len(out) == 0 {
		return nil, BadRequest("invalid_csv", "csv must include a header and at least one row")
	}
	return out, nil
}

func adminGoatBulkCommitRowRequest(row domain.AdminGoatBulkCommitRow, index int) (int, *domain.AdminGoatCreateRequest, []domain.FieldError) {
	rowNumber := row.RowNumber
	if rowNumber == 0 {
		rowNumber = index + 1
	}
	if rowNumber < 1 {
		return index + 1, nil, []domain.FieldError{{
			Field:   "row_number",
			Code:    "invalid",
			Message: "row_number must identify a CSV data row",
		}}
	}
	if row.Normalized != nil {
		return rowNumber, row.Normalized, nil
	}
	request := row.AdminGoatCreateRequest
	return rowNumber, &request, nil
}

func (s *Service) normalizeBulkCommitRowsForPreviewToken(ctx context.Context, tenantID, traceID string, rows []domain.AdminGoatBulkCommitRow) ([]domain.AdminGoatBulkCommitRow, error) {
	out := make([]domain.AdminGoatBulkCommitRow, 0, len(rows))
	seenIdentifiers := map[string]int{}
	for i := range rows {
		rowNumber, request, rowErrors := adminGoatBulkCommitRowRequest(rows[i], i)
		if len(rowErrors) > 0 {
			return nil, BadRequest("invalid_preview_rows", rowErrors[0].Message)
		}
		normalized, _, fieldErrors, err := s.normalizeAdminGoatCreate(ctx, tenantID, "", "", traceID, request)
		if err != nil {
			return nil, err
		}
		if len(fieldErrors) > 0 {
			return nil, BadRequest("invalid_preview_rows", fieldErrors[0].Message)
		}
		if duplicateErrors := duplicateAdminGoatImportErrors(normalized, rowNumber, seenIdentifiers); len(duplicateErrors) > 0 {
			return nil, BadRequest("invalid_preview_rows", duplicateErrors[0].Message)
		}
		out = append(out, domain.AdminGoatBulkCommitRow{RowNumber: rowNumber, Normalized: normalized})
	}
	return out, nil
}

func previewCommitRows(rows []domain.AdminGoatBulkRowResult) []domain.AdminGoatBulkCommitRow {
	out := make([]domain.AdminGoatBulkCommitRow, 0, len(rows))
	for _, row := range rows {
		committable := row.Decision == "create" || row.Decision == bulkImportUpdateReproductiveDecision
		if committable && row.Normalized != nil {
			out = append(out, domain.AdminGoatBulkCommitRow{RowNumber: row.RowNumber, Normalized: row.Normalized})
		}
	}
	return out
}

// adminGoatIdentifiersForMatch builds the normalized identifier lookup keys used
// to resolve a matched-existing goat, mirroring normalizeAdminGoatCreate.
func adminGoatIdentifiersForMatch(normalized *domain.AdminGoatCreateRequest) []ports.AdminGoatCreateIdentifier {
	identifiers := make([]ports.AdminGoatCreateIdentifier, 0, 2)
	if normalized.AnimalIdentifier1 != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{
			IdentifierType:  "animal_identifier_1",
			NormalizedValue: normalizeIdentifier("animal_identifier_1", *normalized.AnimalIdentifier1),
		})
	}
	if normalized.AnimalIdentifier2 != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{
			IdentifierType:  "animal_identifier_2",
			NormalizedValue: normalizeIdentifier("animal_identifier_2", *normalized.AnimalIdentifier2),
		})
	}
	return identifiers
}

func reproductiveBulkSourceRef(normalized *domain.AdminGoatCreateRequest, rowNumber int) string {
	if normalized.SourceRecordID != nil && strings.TrimSpace(*normalized.SourceRecordID) != "" {
		return strings.TrimSpace(*normalized.SourceRecordID)
	}
	return fmt.Sprintf("bulk-import-row:%d", rowNumber)
}

// previewReproductiveBulkUpdate reclassifies a matched-existing row (identifier
// already owned) that carries a differing reproductive_status from a create
// conflict into an actionable reproductive update, capturing match_confidence
// and source_ref. It does not mutate anything.
func (s *Service) previewReproductiveBulkUpdate(ctx context.Context, tenantID string, normalized *domain.AdminGoatCreateRequest, result *domain.AdminGoatBulkRowResult) error {
	matcher, ok := s.reproductiveBulkMatcher()
	if !ok {
		return nil
	}
	match, err := matcher.ResolveGoatForReproductiveBulkUpdate(ctx, ports.ResolveReproductiveMatchCommand{
		TenantID:    tenantID,
		Identifiers: adminGoatIdentifiersForMatch(normalized),
	})
	if err != nil {
		return err
	}
	if !match.Matched {
		return nil
	}
	target := strings.TrimSpace(*normalized.ReproductiveStatus)
	if match.CurrentReproductiveStatus == target {
		// Already at target: nothing to change; leave as requires_review no-op.
		return nil
	}
	goatID := match.GoatID
	confidence := match.MatchConfidence
	result.Decision = bulkImportUpdateReproductiveDecision
	result.Errors = []domain.FieldError{}
	result.GenerationStatus = "queued"
	result.MatchedGoatID = &goatID
	result.MatchConfidence = &confidence
	result.SourceRef = normalized.SourceRecordID
	return nil
}

// commitReproductiveBulkUpdate applies a matched-existing reproductive update via
// the event-emitting ReproductiveGoat transition (expected_row_version no-clobber
// captured from the matched goat's current row_version). It returns handled=true
// when it took ownership of the row (applied or definitively failed); handled=
// false lets the caller fall back to the normal create-conflict handling
// (unmatched / blocked / no-op / capability absent).
func (s *Service) commitReproductiveBulkUpdate(ctx context.Context, tenantID, actorID, clientKey, traceID string, rowNumber int, normalized *domain.AdminGoatCreateRequest, rowResult *domain.AdminGoatBulkRowResult, response *domain.AdminGoatBulkResponse) (bool, error) {
	matcher, ok := s.reproductiveBulkMatcher()
	if !ok {
		return false, nil
	}
	match, err := matcher.ResolveGoatForReproductiveBulkUpdate(ctx, ports.ResolveReproductiveMatchCommand{
		TenantID:    tenantID,
		Identifiers: adminGoatIdentifiersForMatch(normalized),
	})
	if err != nil {
		return false, mapRepoErr(err)
	}
	if !match.Matched {
		return false, nil
	}
	target := strings.TrimSpace(*normalized.ReproductiveStatus)
	if match.CurrentReproductiveStatus == target {
		return false, nil
	}
	goatID := match.GoatID
	confidence := match.MatchConfidence
	rowResult.MatchedGoatID = &goatID
	rowResult.MatchConfidence = &confidence
	rowResult.SourceRef = normalized.SourceRecordID

	evidence := normalized.EvidenceRefs
	if len(evidence) == 0 {
		sourceSystem := "bulk_import"
		evidence = []domain.EvidenceRef{{
			EvidenceType: "source_record",
			EvidenceID:   reproductiveBulkSourceRef(normalized, rowNumber),
			SourceSystem: &sourceSystem,
		}}
	}
	body, err := json.Marshal(domain.ReproductiveGoatRequest{
		ReproductiveStatus: target,
		Reason:             fmt.Sprintf("Bulk import reproductive update (row %d)", rowNumber),
		EvidenceRefs:       evidence,
		RowVersion:         match.RowVersion,
	})
	if err != nil {
		return false, Internal("bulk reproductive update normalization failed")
	}
	resp, applyErr := s.ReproductiveGoat(ctx, ReproductiveGoatInput{
		TenantID:       tenantID,
		ActorID:        actorID,
		IdempotencyKey: rowIdempotencyKey(clientKey, rowNumber),
		TraceID:        traceID,
		GoatID:         goatID,
		RawBody:        body,
	})
	if applyErr != nil {
		rowResult.Decision = "requires_review"
		rowResult.GenerationStatus = "skipped_needs_review"
		rowResult.Errors = []domain.FieldError{{Field: "reproductive_status", Code: "update_failed", Message: applyErr.Error()}}
		response.Summary.Failed++
		return true, nil
	}
	rowResult.Decision = bulkImportUpdateReproductiveDecision
	rowResult.Errors = []domain.FieldError{}
	rowResult.GenerationStatus = "updated"
	rowResult.Result = resp
	response.Summary.Updated++
	return true, nil
}

type bulkPreviewTokenRow struct {
	RowNumber  int                            `json:"row_number"`
	Normalized *domain.AdminGoatCreateRequest `json:"normalized"`
}

func (s *Service) signAdminGoatBulkPreview(tenantID, fileHash string, rows []domain.AdminGoatBulkCommitRow) (string, error) {
	nonce, err := randomAdminGoatBulkPreviewNonce()
	if err != nil {
		return "", err
	}
	return signAdminGoatBulkPreviewWithKeyAt(tenantID, fileHash, rows, s.bulkPreviewSigningKey, time.Now().UTC(), nonce)
}

func signAdminGoatBulkPreviewWithKey(tenantID, fileHash string, rows []domain.AdminGoatBulkCommitRow, signingKey string) (string, error) {
	nonce, err := randomAdminGoatBulkPreviewNonce()
	if err != nil {
		return "", err
	}
	return signAdminGoatBulkPreviewWithKeyAt(tenantID, fileHash, rows, signingKey, time.Now().UTC(), nonce)
}

func signAdminGoatBulkPreviewWithKeyAt(tenantID, fileHash string, rows []domain.AdminGoatBulkCommitRow, signingKey string, issuedAt time.Time, nonce string) (string, error) {
	payloadRows := make([]bulkPreviewTokenRow, 0, len(rows))
	for _, row := range rows {
		payloadRows = append(payloadRows, bulkPreviewTokenRow{RowNumber: row.RowNumber, Normalized: row.Normalized})
	}
	issuedUnix := issuedAt.UTC().Unix()
	payload, err := json.Marshal(struct {
		Version  string                `json:"version"`
		TenantID string                `json:"tenant_id"`
		FileHash string                `json:"file_hash"`
		IssuedAt int64                 `json:"issued_at"`
		Nonce    string                `json:"nonce"`
		Rows     []bulkPreviewTokenRow `json:"rows"`
	}{
		Version:  adminGoatBulkPreviewTokenV1,
		TenantID: strings.TrimSpace(tenantID),
		FileHash: fileHash,
		IssuedAt: issuedUnix,
		Nonce:    nonce,
		Rows:     payloadRows,
	})
	if err != nil {
		return "", err
	}
	key := strings.TrimSpace(signingKey)
	if key == "" {
		return "", fmt.Errorf("bulk preview signing key is required")
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(payload)
	return fmt.Sprintf("%s:%d:%s:%s", adminGoatBulkPreviewTokenV1, issuedUnix, nonce, hex.EncodeToString(mac.Sum(nil))), nil
}

func (s *Service) verifyAdminGoatBulkPreviewToken(tenantID, fileHash, token string, rows []domain.AdminGoatBulkCommitRow) error {
	parts := strings.Split(token, ":")
	if len(parts) != 4 || parts[0] != adminGoatBulkPreviewTokenV1 {
		return BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	issuedUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return BadRequest("invalid_preview_token", "bulk commit must include the preview_token returned by bulk preview")
	}
	issuedAt := time.Unix(issuedUnix, 0).UTC()
	now := time.Now().UTC()
	if now.Sub(issuedAt) > adminGoatBulkPreviewTokenMaxAge || issuedAt.After(now.Add(adminGoatBulkPreviewTokenClockSkew)) {
		return BadRequest("invalid_preview_token", "bulk preview token expired; preview the CSV again")
	}
	expected, err := signAdminGoatBulkPreviewWithKeyAt(tenantID, fileHash, rows, s.bulkPreviewSigningKey, issuedAt, parts[2])
	if err != nil {
		return Internal("bulk preview token verification failed")
	}
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return BadRequest("invalid_preview_token", "bulk commit rows do not match the latest preview; preview the CSV again")
	}
	return nil
}

func randomAdminGoatBulkPreviewNonce() (string, error) {
	var nonce [adminGoatBulkPreviewTokenNonceBytes]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func duplicateAdminGoatImportErrors(normalized *domain.AdminGoatCreateRequest, rowNumber int, seen map[string]int) []domain.FieldError {
	if normalized == nil {
		return nil
	}
	keys := adminGoatImportDuplicateKeys(normalized)
	for _, key := range keys {
		if firstRow, ok := seen[key]; ok {
			return []domain.FieldError{{
				Field:   "identifiers",
				Code:    "duplicate_in_file",
				Message: fmt.Sprintf("duplicate identifier in uploaded sheet; first seen on row %d", firstRow),
			}}
		}
	}
	for _, key := range keys {
		seen[key] = rowNumber
	}
	return nil
}

func adminGoatImportDuplicateKeys(row *domain.AdminGoatCreateRequest) []string {
	keys := make([]string, 0, 2)
	if row.AnimalIdentifier1 != nil {
		keys = append(keys, "animal_identifier_1:"+normalizeIdentifier("animal_identifier_1", *row.AnimalIdentifier1))
	}
	if row.AnimalIdentifier2 != nil {
		keys = append(keys, "animal_identifier_2:"+normalizeIdentifier("animal_identifier_2", *row.AnimalIdentifier2))
	}
	return keys
}

func normalizeHeader(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_", "(", "", ")", "", ".", "")
	v = replacer.Replace(v)
	switch v {
	case "animal_id_1", "animalid1", "animal_identifier_1", "animalidentifier1", "id1":
		return "animal_identifier_1"
	case "animal_id_2", "animalid2", "animal_identifier_2", "animalidentifier2", "id2":
		return "animal_identifier_2"
	case "management_stage", "managementstage", "animal_stage", "animalstage", "stage":
		return "management_stage"
	case "reproductive_status", "reproductivestatus", "repro_status", "reprostatus":
		return "reproductive_status"
	case "weightkg", "weight_kg":
		return "weight_kg"
	case "sire_lot", "sire_or_lot":
		return "sire_lot"
	default:
		return v
	}
}

func valueCSV(record []string, headers map[string]int, key string) string {
	if idx, ok := headers[key]; ok && idx >= 0 && idx < len(record) {
		return strings.TrimSpace(record[idx])
	}
	return ""
}

func optionalCSV(record []string, headers map[string]int, key string) *string {
	value := valueCSV(record, headers, key)
	if value == "" {
		return nil
	}
	return &value
}

func csvRecordBlank(record []string) bool {
	for _, value := range record {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func parseDateField(field, value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("%s is required", field)
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be YYYY-MM-DD", field)
	}
	return parsed, nil
}

func trimOptionalString(value **string) {
	if value == nil || *value == nil {
		return
	}
	trimmed := strings.TrimSpace(**value)
	if trimmed == "" {
		*value = nil
		return
	}
	*value = &trimmed
}

func rowIdempotencyKey(clientKey string, rowNumber int) string {
	return fmt.Sprintf("%s:row:%d", clientKey, rowNumber)
}

var allowedSex = map[string]bool{
	"female": true,
	"male":   true,
}

var allowedSpecies = map[string]bool{
	"goat":  true,
	"sheep": true,
}

var allowedOriginType = map[string]bool{
	"birth":    true,
	"procured": true,
	"imported": true,
}
