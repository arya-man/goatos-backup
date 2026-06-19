package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	countsSyncCommand       = "createCountsSyncRun"
	countsSourceContext     = "legacy_bq_counts"
	countsSourceUnavailable = "counts_source_rows"
)

type SyncCountsInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type countsSyncBody struct {
	DryRun       bool    `json:"dry_run"`
	SnapshotDate *string `json:"snapshot_date"`
}

type preparedCountsRows struct {
	SnapshotRows             []ports.SnapshotInputRow
	ProjectionRows           []ports.ProjectionInputRow
	RowsSkipped              int
	UnresolvedLocationLabels int
	SnapshotDate             *string
	SummarySourceDate        *string
	SourceWatermark          *string
}

func (s *Service) Sync(ctx context.Context, input SyncCountsInput) (*domain.CountsSyncRunResponse, error) {
	tenantID, actorID, clientKey, err := validateSyncHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeCountsSyncBody(input.RawBody)
	if err != nil {
		return nil, err
	}
	snapshotDate, err := normalizeOptionalDate(body.SnapshotDate, "snapshot_date")
	if err != nil {
		return nil, err
	}
	sourceRows, err := s.repo.ListSourceRows(ctx, ports.SourceRowsParams{
		TenantID:     tenantID,
		SnapshotDate: valueOrEmpty(snapshotDate),
	})
	if err != nil {
		return nil, Internal("counts source row read failed")
	}
	if body.DryRun {
		return countsDryRunResponse(clientKey, input.TraceID, snapshotDate, len(sourceRows)), nil
	}

	cmd := ports.SyncCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: storedSyncKey(tenantID, countsSyncCommand, clientKey),
		IdempotencyScope:     countsSyncCommand,
		RequestHash:          requestHash(input.RawBody),
		TraceID:              input.TraceID,
		Mode:                 "execute",
		SnapshotDate:         snapshotDate,
		SourceRowsRead:       len(sourceRows),
	}
	if len(sourceRows) == 0 {
		cmd.SourceUnavailable = true
		cmd.UnavailableSources = []string{countsSourceUnavailable}
		result, err := s.repo.RunSync(ctx, cmd)
		if err != nil {
			return nil, mapSyncRepoErr(err)
		}
		return &result.Response, nil
	}

	prepared, err := s.prepareCountsRows(ctx, tenantID, actorID, input.TraceID, snapshotDate, sourceRows)
	if err != nil {
		return nil, err
	}
	cmd.SnapshotDate = prepared.SnapshotDate
	cmd.SummarySourceDate = prepared.SummarySourceDate
	cmd.SourceWatermark = prepared.SourceWatermark
	cmd.SnapshotRows = prepared.SnapshotRows
	cmd.ProjectionRows = prepared.ProjectionRows
	cmd.RowsSkipped = prepared.RowsSkipped
	cmd.UnresolvedLocationLabels = prepared.UnresolvedLocationLabels
	result, err := s.repo.RunSync(ctx, cmd)
	if err != nil {
		return nil, mapSyncRepoErr(err)
	}
	return &result.Response, nil
}

func (s *Service) GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.CountsSyncRunResponse, error) {
	tenantID = strings.TrimSpace(tenantID)
	if !uuidutil.IsUUIDString(tenantID) {
		return nil, Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	syncRunID = strings.TrimSpace(syncRunID)
	if !uuidutil.IsUUIDString(syncRunID) {
		return nil, BadRequest("invalid_sync_run_id", "sync_run_id must be a UUID")
	}
	result, err := s.repo.GetSyncRun(ctx, tenantID, syncRunID, traceID)
	if err != nil {
		return nil, mapSyncRepoErr(err)
	}
	return result, nil
}

func (s *Service) prepareCountsRows(ctx context.Context, tenantID, actorID, traceID string, requestedSnapshotDate *string, sourceRows []ports.SourceRow) (preparedCountsRows, error) {
	out := preparedCountsRows{
		SnapshotRows:   []ports.SnapshotInputRow{},
		ProjectionRows: []ports.ProjectionInputRow{},
	}
	sort.SliceStable(sourceRows, func(i, j int) bool {
		if sourceRows[i].SourceWatermarkDate != nil && sourceRows[j].SourceWatermarkDate != nil && *sourceRows[i].SourceWatermarkDate != *sourceRows[j].SourceWatermarkDate {
			return *sourceRows[i].SourceWatermarkDate < *sourceRows[j].SourceWatermarkDate
		}
		return sourceRows[i].SourceRowID < sourceRows[j].SourceRowID
	})
	for index, row := range sourceRows {
		parsed, ok := parseCountsPayload(row, requestedSnapshotDate)
		if !ok {
			out.RowsSkipped++
			continue
		}
		farmID, farmType, farmUnresolved, err := s.resolveCountsLocation(ctx, tenantID, actorID, traceID, parsed.FarmLabel, row)
		if err != nil {
			return out, err
		}
		shedID, shedType, shedUnresolved, err := s.resolveCountsLocation(ctx, tenantID, actorID, traceID, parsed.ShedLabel, row)
		if err != nil {
			return out, err
		}
		if farmUnresolved {
			out.UnresolvedLocationLabels++
		}
		if shedUnresolved {
			out.UnresolvedLocationLabels++
		}
		resolvedID := firstStringPtr(shedID, farmID)
		resolvedType := firstStringPtr(shedType, farmType)
		logicalFactKey := parsed.LogicalFactKey
		if logicalFactKey == "" {
			logicalFactKey = stableHash("counts-fact", parsed.ViewID, parsed.SnapshotDate, parsed.Section, parsed.Grain, parsed.DimensionKey, parsed.MetricKey, row.SourceRowID)
		}
		sourceHash := row.PayloadHash
		if sourceHash == "" {
			sourceHash = stableHash("counts-source", string(row.PayloadJSON))
		}
		out.SnapshotRows = append(out.SnapshotRows, ports.SnapshotInputRow{
			SnapshotDate:         parsed.SnapshotDate,
			SourceMode:           parsed.SourceMode,
			RowKind:              parsed.RowKind,
			TabScope:             parsed.TabScope,
			FarmKey:              parsed.FarmKey,
			FarmLabel:            parsed.FarmLabel,
			FarmID:               farmID,
			ShedKey:              parsed.ShedKey,
			ShedLabel:            parsed.ShedLabel,
			ShedID:               shedID,
			ResolvedLocationID:   resolvedID,
			ResolvedLocationType: resolvedType,
			StatusKey:            parsed.StatusKey,
			StatusLabel:          parsed.StatusLabel,
			BreedKey:             parsed.BreedKey,
			BreedLabel:           parsed.BreedLabel,
			AgeClass:             parsed.AgeClass,
			Sex:                  parsed.Sex,
			MetricName:           parsed.MetricKey,
			CountValue:           parsed.CountValue,
			SourceRowID:          row.SourceRowID,
			LogicalFactKey:       logicalFactKey,
			ProjectionInputHash:  sourceHash,
		})
		out.ProjectionRows = append(out.ProjectionRows, ports.ProjectionInputRow{
			ViewID:                  parsed.ViewID,
			SnapshotDate:            parsed.SnapshotDate,
			SummarySourceDate:       parsed.SummarySourceDate,
			Section:                 parsed.Section,
			Grain:                   parsed.Grain,
			DimensionKey:            parsed.DimensionKey,
			DimensionLabel:          parsed.DimensionLabel,
			SecondaryDimensionKey:   parsed.SecondaryDimensionKey,
			SecondaryDimensionLabel: parsed.SecondaryDimensionLabel,
			MetricKey:               parsed.MetricKey,
			CountValue:              parsed.CountValue,
			NumericValue:            parsed.NumericValue,
			Unit:                    parsed.Unit,
			Denominator:             parsed.Denominator,
			SortOrder:               parsed.SortOrder + index,
			SourceHash:              sourceHash,
			SourceComposition:       sourceComposition(parsed.SourceMode),
		})
		out.SnapshotDate = maxDatePtr(out.SnapshotDate, &parsed.SnapshotDate)
		out.SummarySourceDate = maxDatePtr(out.SummarySourceDate, parsed.SummarySourceDate)
		out.SourceWatermark = maxDatePtr(out.SourceWatermark, row.SourceWatermarkDate)
	}
	if out.SnapshotDate == nil {
		out.SnapshotDate = requestedSnapshotDate
	}
	if out.SummarySourceDate == nil {
		out.SummarySourceDate = out.SnapshotDate
	}
	if out.SourceWatermark == nil {
		out.SourceWatermark = out.SnapshotDate
	}
	return out, nil
}

func (s *Service) resolveCountsLocation(ctx context.Context, tenantID, actorID, traceID string, label *string, row ports.SourceRow) (locationID *string, locationType *string, unresolved bool, err error) {
	if label == nil || strings.TrimSpace(*label) == "" {
		return nil, nil, false, nil
	}
	if s.resolver == nil {
		return nil, nil, true, nil
	}
	evidence, _ := json.Marshal(map[string]any{
		"module":         "counts",
		"source_row_id":  row.SourceRowID,
		"source_system":  row.SourceSystem,
		"source_id":      row.SourceID,
		"source_table":   row.SourceTable,
		"source_row_key": row.SourceRowKey,
		"source_label":   *label,
		"source_hash":    row.PayloadHash,
	})
	resolution, err := s.resolver.ResolveSourceLabel(ctx, locationsapp.ResolveSourceLabelInput{
		TenantID:      tenantID,
		ActorID:       actorID,
		TraceID:       traceID,
		SourceContext: countsSourceContext,
		SourceLabel:   *label,
		EvidenceJSON:  evidence,
		EvidenceHash:  stableHash("counts-source-label", tenantID, countsSourceContext, locationsapp.NormalizeSourceLabel(*label)),
	})
	if err != nil {
		return nil, nil, false, err
	}
	if resolution.Location == nil {
		return nil, nil, true, nil
	}
	return &resolution.Location.LocationID, &resolution.Location.LocationType, false, nil
}

type parsedCountsPayload struct {
	ViewID                  string
	SnapshotDate            string
	SummarySourceDate       *string
	SourceMode              string
	Section                 string
	Grain                   string
	DimensionKey            string
	DimensionLabel          string
	SecondaryDimensionKey   *string
	SecondaryDimensionLabel *string
	MetricKey               string
	CountValue              *int64
	NumericValue            *float64
	Unit                    string
	Denominator             *float64
	SortOrder               int
	RowKind                 string
	TabScope                *string
	FarmKey                 *string
	FarmLabel               *string
	ShedKey                 *string
	ShedLabel               *string
	StatusKey               *string
	StatusLabel             *string
	BreedKey                *string
	BreedLabel              *string
	AgeClass                *string
	Sex                     *string
	LogicalFactKey          string
}

func parseCountsPayload(row ports.SourceRow, requestedSnapshotDate *string) (parsedCountsPayload, bool) {
	var payload map[string]any
	if err := json.Unmarshal(row.PayloadJSON, &payload); err != nil {
		return parsedCountsPayload{}, false
	}
	snapshotDate := firstNonEmpty(
		stringField(payload, "snapshot_date"),
		valueOrEmpty(row.SourceWatermarkDate),
		valueOrEmpty(requestedSnapshotDate),
		time.Now().UTC().Format("2006-01-02"),
	)
	if _, err := time.Parse("2006-01-02", snapshotDate); err != nil {
		return parsedCountsPayload{}, false
	}
	viewID := normalizeView(stringField(payload, "view_id", "view"))
	if !validView(viewID) {
		return parsedCountsPayload{}, false
	}
	section := firstNonEmpty(stringField(payload, "section"), "summary")
	if !validCountsSection(section) {
		return parsedCountsPayload{}, false
	}
	grain := firstNonEmpty(stringField(payload, "grain"), "metric")
	if !validCountsGrain(grain) {
		return parsedCountsPayload{}, false
	}
	metricKey := firstNonEmpty(stringField(payload, "metric_key", "metric_name"), "goat_count")
	dimensionKey := firstNonEmpty(stringField(payload, "dimension_key"), metricKey)
	dimensionLabel := firstNonEmpty(stringField(payload, "dimension_label"), dimensionKey)
	unit := firstNonEmpty(stringField(payload, "unit"), "count")
	if !validCountsUnit(unit) {
		return parsedCountsPayload{}, false
	}
	countValue := int64Field(payload, "count_value", "count")
	numericValue := float64Field(payload, "numeric_value", "value")
	if countValue == nil && numericValue == nil {
		return parsedCountsPayload{}, false
	}
	sourceMode := firstNonEmpty(stringField(payload, "source_mode"), "legacy_bq")
	if sourceMode != "legacy_bq" && sourceMode != "goatos_canonical" {
		sourceMode = "legacy_bq"
	}
	summaryDate := stringPtrIfDate(stringField(payload, "summary_source_date"))
	if summaryDate == nil {
		summaryDate = &snapshotDate
	}
	sortOrder := intField(payload, "sort_order")
	return parsedCountsPayload{
		ViewID:                  viewID,
		SnapshotDate:            snapshotDate,
		SummarySourceDate:       summaryDate,
		SourceMode:              sourceMode,
		Section:                 section,
		Grain:                   grain,
		DimensionKey:            dimensionKey,
		DimensionLabel:          dimensionLabel,
		SecondaryDimensionKey:   stringPtrField(payload, "secondary_dimension_key"),
		SecondaryDimensionLabel: stringPtrField(payload, "secondary_dimension_label"),
		MetricKey:               metricKey,
		CountValue:              countValue,
		NumericValue:            numericValue,
		Unit:                    unit,
		Denominator:             float64Field(payload, "denominator"),
		SortOrder:               sortOrder,
		RowKind:                 countsRowKind(section),
		TabScope:                stringPtrField(payload, "tab_scope"),
		FarmKey:                 stringPtrField(payload, "farm_key"),
		FarmLabel:               stringPtrField(payload, "farm_label", "farm"),
		ShedKey:                 stringPtrField(payload, "shed_key"),
		ShedLabel:               stringPtrField(payload, "shed_label", "shed"),
		StatusKey:               stringPtrField(payload, "status_key"),
		StatusLabel:             stringPtrField(payload, "status_label", "status"),
		BreedKey:                stringPtrField(payload, "breed_key"),
		BreedLabel:              stringPtrField(payload, "breed_label", "breed"),
		AgeClass:                normalizeNullableEnum(stringPtrField(payload, "age_class"), map[string]bool{"adult": true, "kid": true, "unknown": true}),
		Sex:                     normalizeNullableEnum(stringPtrField(payload, "sex", "gender"), map[string]bool{"female": true, "male": true, "unknown": true}),
		LogicalFactKey:          stringField(payload, "logical_fact_key"),
	}, true
}

func decodeCountsSyncBody(raw []byte) (countsSyncBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return countsSyncBody{}, nil
	}
	var body countsSyncBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, BadRequest("invalid_json", "request body must be valid JSON")
	}
	return body, nil
}

func validateSyncHeaders(rawTenantID, rawActorID, rawClientKey string) (tenantID, actorID, clientKey string, err error) {
	tenantID = strings.TrimSpace(rawTenantID)
	if !uuidutil.IsUUIDString(tenantID) {
		return "", "", "", Unauthorized("missing_tenant_scope", "tenant scope is required")
	}
	actorID = strings.TrimSpace(rawActorID)
	if !uuidutil.IsUUIDString(actorID) {
		return "", "", "", Unauthorized("missing_actor_scope", "actor scope is required")
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

func normalizeOptionalDate(raw *string, field string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" || value == "latest" {
		return nil, nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return nil, BadRequest("invalid_"+field, field+" must be YYYY-MM-DD")
	}
	return &value, nil
}

func countsDryRunResponse(clientKey, traceID string, snapshotDate *string, rowsRead int) *domain.CountsSyncRunResponse {
	return &domain.CountsSyncRunResponse{
		SyncRunID:      "dry-run",
		Mode:           "dry_run",
		Status:         "completed",
		SnapshotDate:   snapshotDate,
		SourceRowsRead: rowsRead,
		Freshness: domain.FreshnessEnvelope{
			FreshnessStatus:    "unknown",
			ServingState:       "never_synced",
			Stale:              true,
			UnavailableSources: []string{},
			SourceComposition:  "legacy_only",
		},
		Idempotency: domain.IdempotencyMeta{IdempotencyKey: clientKey},
		TraceID:     traceID,
	}
}

func mapSyncRepoErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return BadRequest("idempotency_conflict", "Idempotency-Key was reused with a different request")
	case errors.Is(err, ports.ErrIdempotencyPending):
		return BadRequest("idempotency_pending", "Idempotency-Key is already in progress")
	case errors.Is(err, ports.ErrSyncRunNotFound):
		return NotFound("sync_run_not_found", "Counts sync run was not found")
	default:
		return Internal("counts sync failed")
	}
}

func storedSyncKey(tenantID, command, clientKey string) string {
	return strings.Join([]string{tenantID, command, clientKey}, ":")
}

func requestHash(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	sum := sha256.Sum256(trimmed)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stableHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func maxDatePtr(current, candidate *string) *string {
	if candidate == nil || strings.TrimSpace(*candidate) == "" {
		return current
	}
	value := strings.TrimSpace(*candidate)
	if current == nil || value > *current {
		return &value
	}
	return current
}

func firstStringPtr(values ...*string) *string {
	for _, value := range values {
		if value != nil && strings.TrimSpace(*value) != "" {
			return value
		}
	}
	return nil
}

func stringField(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				return trimmed
			}
		case float64:
			return strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(typed)
		}
	}
	return ""
}

func stringPtrField(payload map[string]any, keys ...string) *string {
	value := stringField(payload, keys...)
	if value == "" {
		return nil
	}
	return &value
}

func int64Field(payload map[string]any, keys ...string) *int64 {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			out := int64(typed)
			return &out
		case string:
			if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func float64Field(payload map[string]any, keys ...string) *float64 {
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return &typed
		case string:
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64); err == nil {
				return &parsed
			}
		}
	}
	return nil
}

func intField(payload map[string]any, key string) int {
	if value := int64Field(payload, key); value != nil {
		return int(*value)
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringPtrIfDate(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return nil
	}
	return &value
}

func normalizeNullableEnum(value *string, allowed map[string]bool) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "" {
		return nil
	}
	if !allowed[normalized] {
		normalized = "unknown"
	}
	return &normalized
}

func validCountsSection(section string) bool {
	switch section {
	case "summary", "status", "breed", "status_breed", "farm", "farm_distribution", "age", "adults_gender", "kids_gender", "kids_stage_gender", "fattening_gender", "core_farm_gender_breed":
		return true
	default:
		return false
	}
}

func validCountsGrain(grain string) bool {
	switch grain {
	case "metric", "status", "breed", "status_breed", "farm", "age_class", "sex", "breed_sex":
		return true
	default:
		return false
	}
}

func validCountsUnit(unit string) bool {
	switch unit {
	case "count", "kg", "inr", "kg_per_goat", "percent":
		return true
	default:
		return false
	}
}

func countsRowKind(section string) string {
	switch section {
	case "summary":
		return "summary_kpi"
	case "age", "adults_gender", "kids_gender", "kids_stage_gender", "fattening_gender":
		return "age_gender_kpi"
	case "core_farm_gender_breed":
		return "core_gender_breed"
	default:
		return "detail_count"
	}
}

func sourceComposition(sourceMode string) string {
	if sourceMode == "goatos_canonical" {
		return "canonical_only"
	}
	return "legacy_only"
}

func formatCountsError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("counts sync: %w", err)
}
