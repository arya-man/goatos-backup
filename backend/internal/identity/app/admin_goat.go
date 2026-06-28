package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	createAdminGoatCommand      = "createAdminGoat"
	previewAdminGoatBulkCommand = "previewAdminGoatBulkImport"
	commitAdminGoatBulkCommand  = "commitAdminGoatBulkImport"
	maxBulkRows                 = 500
)

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
		switch result.Decision {
		case "create":
			response.Summary.CreateReady++
		case "requires_review":
			response.Summary.RequiresReview++
		default:
			response.Summary.Skipped++
		}
		response.Rows = append(response.Rows, result)
	}
	response.Summary.Total = len(response.Rows)
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
	return response, nil
}

func (s *Service) normalizeAdminGoatCreate(_ context.Context, tenantID, actorID, clientKey, traceID string, body *domain.AdminGoatCreateRequest) (*domain.AdminGoatCreateRequest, ports.CreateAdminGoatCommand, []domain.FieldError, error) {
	normalized := *body
	errorsOut := make([]domain.FieldError, 0)
	trimOptionalString(&normalized.RFID)
	trimOptionalString(&normalized.OldTag)
	trimOptionalString(&normalized.TempFieldID)
	trimOptionalString(&normalized.FarmID)
	trimOptionalString(&normalized.FarmCode)
	trimOptionalString(&normalized.ParkID)
	trimOptionalString(&normalized.ParkCode)
	trimOptionalString(&normalized.ShedID)
	trimOptionalString(&normalized.ShedCode)
	trimOptionalString(&normalized.Breed)
	trimOptionalString(&normalized.ManagementStage)
	trimOptionalString(&normalized.HealthStatus)
	trimOptionalString(&normalized.DamID)
	trimOptionalString(&normalized.SireOrLot)
	trimOptionalString(&normalized.PhotoURL)
	trimOptionalString(&normalized.SourceRecordID)
	normalized.Sex = strings.TrimSpace(normalized.Sex)
	normalized.OriginType = strings.TrimSpace(normalized.OriginType)
	normalized.EntryDate = strings.TrimSpace(normalized.EntryDate)
	if normalized.Sex == "" {
		normalized.Sex = "unknown"
	}
	if normalized.OriginType == "" {
		normalized.OriginType = "unknown"
	}
	if normalized.DOBEstimated == nil {
		estimated := true
		normalized.DOBEstimated = &estimated
	}
	if normalized.RFID == nil && normalized.OldTag == nil && normalized.TempFieldID == nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "identifiers", Code: "required", Message: "at least one of rfid, old_tag, or temp_field_id is required"})
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
	if !allowedSex[normalized.Sex] {
		errorsOut = append(errorsOut, domain.FieldError{Field: "sex", Code: "invalid", Message: "sex must be female, male, or unknown"})
	}
	if !allowedOriginType[normalized.OriginType] {
		errorsOut = append(errorsOut, domain.FieldError{Field: "origin_type", Code: "invalid", Message: "origin_type must be birth, procured, imported, or unknown"})
	}
	entryDate, err := parseDateField("entry_date", normalized.EntryDate)
	if err != nil {
		errorsOut = append(errorsOut, domain.FieldError{Field: "entry_date", Code: "invalid", Message: err.Error()})
	}
	var dob *time.Time
	if normalized.DOB != nil {
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
	identifiers := make([]ports.AdminGoatCreateIdentifier, 0, 3)
	if normalized.RFID != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{IdentifierType: "rfid", IdentifierValue: *normalized.RFID, NormalizedValue: normalizeIdentifier("rfid", *normalized.RFID), ScopeKey: "global", IsPrimary: true})
	}
	oldTagScope := ""
	if normalized.ParkID != nil {
		oldTagScope = "park:" + *normalized.ParkID
	} else if normalized.ParkCode != nil {
		oldTagScope = "park_code:" + *normalized.ParkCode
	}
	if normalized.OldTag != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{IdentifierType: "old_tag", IdentifierValue: *normalized.OldTag, NormalizedValue: normalizeIdentifier("old_tag", *normalized.OldTag), ScopeKey: oldTagScope, IsPrimary: true})
	}
	if normalized.TempFieldID != nil {
		identifiers = append(identifiers, ports.AdminGoatCreateIdentifier{IdentifierType: "temp_field_id", IdentifierValue: *normalized.TempFieldID, NormalizedValue: normalizeIdentifier("temp_field_id", *normalized.TempFieldID), ScopeKey: "global", IsPrimary: true})
	}
	cmd := ports.CreateAdminGoatCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		TraceID:              traceID,
		Identifiers:          identifiers,
		FarmID:               normalized.FarmID,
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
	for i := range cmd.Identifiers {
		if cmd.Identifiers[i].IdentifierType == "old_tag" {
			cmd.Identifiers[i].ScopeKey = "park:" + validation.ParkID
		}
	}
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
	if len(body.Rows) == 0 {
		return nil, BadRequest("missing_rows", "rows must contain at least one item")
	}
	return &body, nil
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
			RFID:            optionalCSV(rec, headers, "rfid"),
			OldTag:          optionalCSV(rec, headers, "old_tag"),
			TempFieldID:     optionalCSV(rec, headers, "temp_field_id"),
			FarmCode:        optionalCSV(rec, headers, "farm"),
			ParkID:          optionalCSV(rec, headers, "park_id"),
			ParkCode:        optionalCSV(rec, headers, "park"),
			ShedID:          optionalCSV(rec, headers, "shed_id"),
			ShedCode:        optionalCSV(rec, headers, "shed"),
			Breed:           optionalCSV(rec, headers, "breed"),
			ManagementStage: optionalCSV(rec, headers, "management_stage"),
			Sex:             valueCSV(rec, headers, "sex"),
			DOB:             optionalCSV(rec, headers, "dob"),
			OriginType:      valueCSV(rec, headers, "origin"),
			PhotoURL:        optionalCSV(rec, headers, "photo_url"),
			SourceRecordID:  &sourceID,
			EvidenceRefs: []domain.EvidenceRef{{
				EvidenceType: "source_record",
				EvidenceID:   sourceID,
			}},
		}
		if entry := optionalCSV(rec, headers, "entry_date"); entry != nil {
			req.EntryDate = *entry
		} else {
			req.EntryDate = time.Now().UTC().Format("2006-01-02")
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
	keys := make([]string, 0, 3)
	if row.RFID != nil {
		keys = append(keys, "rfid:"+normalizeIdentifier("rfid", *row.RFID))
	}
	if row.TempFieldID != nil {
		keys = append(keys, "temp_field_id:"+normalizeIdentifier("temp_field_id", *row.TempFieldID))
	}
	if row.OldTag != nil {
		scope := ""
		if row.ParkID != nil {
			scope = "park:" + strings.ToLower(*row.ParkID)
		} else if row.ParkCode != nil {
			scope = "park_code:" + strings.ToLower(*row.ParkCode)
		}
		keys = append(keys, "old_tag:"+scope+":"+normalizeIdentifier("old_tag", *row.OldTag))
	}
	return keys
}

func normalizeHeader(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer(" ", "_", "-", "_", "/", "_", "(", "", ")", "", ".", "")
	v = replacer.Replace(v)
	switch v {
	case "old_tag", "oldtag":
		return "old_tag"
	case "temp_field_id", "temporary_field_id", "tempfieldid":
		return "temp_field_id"
	case "management_stage", "managementstage", "animal_stage", "animalstage", "stage":
		return "management_stage"
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
	"female":  true,
	"male":    true,
	"unknown": true,
}

var allowedOriginType = map[string]bool{
	"birth":    true,
	"procured": true,
	"imported": true,
	"unknown":  true,
}
