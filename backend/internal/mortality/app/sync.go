package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/mortality/domain"
	"github.com/vgoats/goatos/backend/internal/mortality/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	mortalitySyncCommand       = "createMortalitySyncRun"
	mortalitySourceContext     = "legacy_bq_mortality"
	mortalitySourceUnavailable = "mortality_source_rows"
)

type SyncMortalityInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

type mortalitySyncBody struct {
	DryRun bool `json:"dry_run"`
}

type preparedMortalityRows struct {
	Events                   []ports.EventInputRow
	ProjectionRows           []ports.ProjectionInputRow
	RowsSkipped              int
	DedupCandidateEvents     int
	UnresolvedLocationLabels int
	SourceWatermark          *string
}

func (s *Service) Sync(ctx context.Context, input SyncMortalityInput) (*domain.MortalitySyncRunResponse, error) {
	tenantID, actorID, clientKey, err := validateMortalitySyncHeaders(input.TenantID, input.ActorID, input.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	body, err := decodeMortalitySyncBody(input.RawBody)
	if err != nil {
		return nil, err
	}
	sourceRows, err := s.repo.ListSourceRows(ctx, ports.SourceRowsParams{TenantID: tenantID})
	if err != nil {
		return nil, Internal("mortality source row read failed")
	}
	if body.DryRun {
		return mortalityDryRunResponse(clientKey, input.TraceID, len(sourceRows)), nil
	}

	cmd := ports.SyncCommand{
		TenantID:             tenantID,
		ActorID:              actorID,
		ClientIdempotencyKey: clientKey,
		StoredIdempotencyKey: mortalityStoredSyncKey(tenantID, mortalitySyncCommand, clientKey),
		IdempotencyScope:     mortalitySyncCommand,
		RequestHash:          mortalityRequestHash(input.RawBody),
		TraceID:              input.TraceID,
		Mode:                 "execute",
		SourceRowsRead:       len(sourceRows),
	}
	if len(sourceRows) == 0 {
		cmd.SourceUnavailable = true
		cmd.UnavailableSources = []string{mortalitySourceUnavailable}
		result, err := s.repo.RunSync(ctx, cmd)
		if err != nil {
			return nil, mapMortalitySyncRepoErr(err)
		}
		return &result.Response, nil
	}
	prepared, err := s.prepareMortalityRows(ctx, tenantID, actorID, input.TraceID, sourceRows)
	if err != nil {
		return nil, err
	}
	cmd.Events = prepared.Events
	cmd.ProjectionRows = prepared.ProjectionRows
	cmd.RowsSkipped = prepared.RowsSkipped
	cmd.DedupCandidateEvents = prepared.DedupCandidateEvents
	cmd.UnresolvedLocationLabels = prepared.UnresolvedLocationLabels
	cmd.SourceWatermark = prepared.SourceWatermark
	result, err := s.repo.RunSync(ctx, cmd)
	if err != nil {
		return nil, mapMortalitySyncRepoErr(err)
	}
	return &result.Response, nil
}

func (s *Service) GetSyncRun(ctx context.Context, tenantID, syncRunID, traceID string) (*domain.MortalitySyncRunResponse, error) {
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
		return nil, mapMortalitySyncRepoErr(err)
	}
	return result, nil
}

func (s *Service) prepareMortalityRows(ctx context.Context, tenantID, actorID, traceID string, sourceRows []ports.SourceRow) (preparedMortalityRows, error) {
	out := preparedMortalityRows{Events: []ports.EventInputRow{}, ProjectionRows: []ports.ProjectionInputRow{}}
	sort.SliceStable(sourceRows, func(i, j int) bool {
		if sourceRows[i].SourceWatermark != nil && sourceRows[j].SourceWatermark != nil && *sourceRows[i].SourceWatermark != *sourceRows[j].SourceWatermark {
			return *sourceRows[i].SourceWatermark < *sourceRows[j].SourceWatermark
		}
		return sourceRows[i].SourceRowID < sourceRows[j].SourceRowID
	})
	candidateOrdinals := map[string]int{}
	for _, row := range sourceRows {
		event, ok := parseMortalityPayload(row)
		if !ok {
			out.RowsSkipped++
			continue
		}
		farmID, farmUnresolved, err := s.resolveMortalityLocation(ctx, tenantID, actorID, traceID, event.FarmLabel, row)
		if err != nil {
			return out, err
		}
		shedID, shedUnresolved, err := s.resolveMortalityLocation(ctx, tenantID, actorID, traceID, event.HousingLabel, row)
		if err != nil {
			return out, err
		}
		if farmUnresolved {
			out.UnresolvedLocationLabels++
		}
		if shedUnresolved {
			out.UnresolvedLocationLabels++
		}
		event.CanonicalFarmLocationID = farmID
		event.CanonicalShedLocationID = shedID
		if event.DedupCandidateKey != nil {
			candidateOrdinals[*event.DedupCandidateKey]++
			ordinal := candidateOrdinals[*event.DedupCandidateKey]
			event.UnresolvedEventOrdinal = &ordinal
			out.DedupCandidateEvents++
		}
		if farmUnresolved || shedUnresolved || event.DedupConfidence == "candidate_review" {
			event.ReviewStatus = "needs_review"
		}
		out.SourceWatermark = mortalityMaxTextPtr(out.SourceWatermark, row.SourceWatermark)
		out.Events = append(out.Events, event)
	}
	out.ProjectionRows = buildMortalityProjectionRows(out.Events)
	return out, nil
}

func (s *Service) resolveMortalityLocation(ctx context.Context, tenantID, actorID, traceID string, label *string, row ports.SourceRow) (locationID *string, unresolved bool, err error) {
	if label == nil || strings.TrimSpace(*label) == "" {
		return nil, false, nil
	}
	if s.resolver == nil {
		return nil, true, nil
	}
	evidence, _ := json.Marshal(map[string]any{
		"module":         "mortality",
		"source_row_id":  row.SourceRowID,
		"source_system":  row.SourceSystem,
		"source_table":   row.SourceTable,
		"source_row_key": row.SourceRowKey,
		"source_label":   *label,
		"source_hash":    row.PayloadHash,
	})
	resolution, err := s.resolver.ResolveSourceLabel(ctx, locationsapp.ResolveSourceLabelInput{
		TenantID:      tenantID,
		ActorID:       actorID,
		TraceID:       traceID,
		SourceContext: mortalitySourceContext,
		SourceLabel:   *label,
		EvidenceJSON:  evidence,
		EvidenceHash:  mortalityStableHash("mortality-source-label", tenantID, mortalitySourceContext, locationsapp.NormalizeSourceLabel(*label)),
	})
	if err != nil {
		return nil, false, err
	}
	if resolution.Location == nil {
		return nil, true, nil
	}
	return &resolution.Location.LocationID, false, nil
}

func parseMortalityPayload(row ports.SourceRow) (ports.EventInputRow, bool) {
	var payload map[string]any
	if err := json.Unmarshal(row.PayloadJSON, &payload); err != nil {
		return ports.EventInputRow{}, false
	}
	eventDate := mortalityFirstNonEmpty(mortalityStringField(payload, "event_date", "death_date", "date"), mortalityDateFromText(row.SourceWatermark))
	if _, err := time.Parse("2006-01-02", eventDate); err != nil {
		return ports.EventInputRow{}, false
	}
	eventType := strings.ToLower(mortalityFirstNonEmpty(mortalityStringField(payload, "event_type"), "death"))
	if eventType != "death" && eventType != "abortion" {
		return ports.EventInputRow{}, false
	}
	goatID := mortalityUUIDPtr(mortalityStringField(payload, "goat_id"))
	sourceGoatIdentifier := mortalityStringPtrField(payload, "source_goat_identifier", "old_tag", "rfid", "tag")
	sourceIdentifierKind := mortalityStringPtrField(payload, "source_identifier_kind", "identifier_kind")
	if sourceIdentifierKind == nil && sourceGoatIdentifier != nil {
		kind := "legacy_tag"
		sourceIdentifierKind = &kind
	}
	dedupConfidence := "candidate_review"
	var logicalEventKey *string
	if goatID != nil {
		value := mortalityStableHash("mortality-logical-goat", *goatID, eventType, eventDate)
		logicalEventKey = &value
		dedupConfidence = "resolved_identity"
	} else if sourceGoatIdentifier != nil {
		value := mortalityStableHash("mortality-logical-source", mortalityValueOrEmpty(sourceIdentifierKind), *sourceGoatIdentifier, eventType, eventDate)
		logicalEventKey = &value
		dedupConfidence = "stable_source_identifier"
	}
	if explicit := mortalityStringField(payload, "logical_event_key"); explicit != "" {
		logicalEventKey = &explicit
		if dedupConfidence == "candidate_review" {
			dedupConfidence = "stable_source_identifier"
		}
	}
	var dedupCandidateKey *string
	if logicalEventKey == nil {
		value := mortalityStableHash(
			"mortality-candidate",
			eventType,
			eventDate,
			mortalityStringField(payload, "farm_label", "farm"),
			mortalityStringField(payload, "shed_label", "shed", "housing_label"),
			mortalityStringField(payload, "breed_key", "breed"),
			mortalityStringField(payload, "age_class"),
			mortalityStringField(payload, "sex", "gender"),
		)
		dedupCandidateKey = &value
	}
	idempotencyKey := mortalityStringField(payload, "idempotency_key")
	if logicalEventKey != nil {
		idempotencyKey = mortalityStableHash("mortality-idempotency-logical", *logicalEventKey)
	}
	if idempotencyKey == "" {
		idempotencyKey = mortalityStableHash("mortality-idempotency-source", row.PayloadHash, row.SourceRowID)
	}
	reviewStatus := "accepted"
	if dedupConfidence == "candidate_review" {
		reviewStatus = "needs_review"
	}
	ageClass := strings.ToLower(mortalityFirstNonEmpty(mortalityStringField(payload, "age_class"), "unknown"))
	if ageClass != "kid" && ageClass != "adult" {
		ageClass = "unknown"
	}
	sex := mortalityNormalizeEnumPtr(mortalityStringPtrField(payload, "sex", "gender"), map[string]bool{"female": true, "male": true, "unknown": true})
	eventHash := mortalityStableHash("mortality-event", idempotencyKey, eventType, eventDate, string(row.PayloadJSON))
	return ports.EventInputRow{
		LogicalEventKey:      logicalEventKey,
		DedupCandidateKey:    dedupCandidateKey,
		DedupConfidence:      dedupConfidence,
		EventType:            eventType,
		EventDate:            eventDate,
		GoatID:               goatID,
		SourceGoatIdentifier: sourceGoatIdentifier,
		SourceIdentifierKind: sourceIdentifierKind,
		AgeClass:             ageClass,
		BreedKey:             mortalityStringPtrField(payload, "breed_key"),
		BreedLabel:           mortalityStringPtrField(payload, "breed_label", "breed"),
		FarmKey:              mortalityStringPtrField(payload, "farm_key"),
		FarmLabel:            mortalityStringPtrField(payload, "farm_label", "farm"),
		HousingKey:           mortalityStringPtrField(payload, "housing_key", "shed_key"),
		HousingLabel:         mortalityStringPtrField(payload, "housing_label", "shed_label", "shed"),
		LoadKey:              mortalityStringPtrField(payload, "load_key"),
		LoadLabel:            mortalityStringPtrField(payload, "load_label", "load"),
		DeliveryKey:          mortalityStringPtrField(payload, "delivery_key"),
		DeliveryLabel:        mortalityStringPtrField(payload, "delivery_label", "delivery"),
		Sex:                  sex,
		SourceRowID:          row.SourceRowID,
		EventHash:            eventHash,
		ReviewStatus:         reviewStatus,
		IdempotencyKey:       idempotencyKey,
	}, true
}

func buildMortalityProjectionRows(events []ports.EventInputRow) []ports.ProjectionInputRow {
	if len(events) == 0 {
		return []ports.ProjectionInputRow{}
	}
	maxDate := ""
	for _, event := range events {
		if event.EventDate > maxDate {
			maxDate = event.EventDate
		}
	}
	thisMonth := ""
	if len(maxDate) >= 7 {
		thisMonth = maxDate[:7]
	}
	rows := []ports.ProjectionInputRow{}
	rows = append(rows, mortalityRowsForPeriod("overall", nil, nil, events)...)
	thisMonthEvents := []ports.EventInputRow{}
	for _, event := range events {
		if strings.HasPrefix(event.EventDate, thisMonth) {
			thisMonthEvents = append(thisMonthEvents, event)
		}
	}
	if len(thisMonthEvents) > 0 {
		start, end := monthBounds(thisMonth)
		rows = append(rows, mortalityRowsForPeriod("this-month", &start, &end, thisMonthEvents)...)
	}
	byMonth := map[string][]ports.EventInputRow{}
	for _, event := range events {
		if len(event.EventDate) >= 7 {
			byMonth[event.EventDate[:7]] = append(byMonth[event.EventDate[:7]], event)
		}
	}
	months := make([]string, 0, len(byMonth))
	for month := range byMonth {
		months = append(months, month)
	}
	sort.Strings(months)
	for _, month := range months {
		start, end := monthBounds(month)
		rows = append(rows, mortalityRowsForPeriod("month-wise", &start, &end, byMonth[month])...)
	}
	return rows
}

func mortalityRowsForPeriod(period string, start, end *string, events []ports.EventInputRow) []ports.ProjectionInputRow {
	rows := []ports.ProjectionInputRow{}
	deaths := countEvents(events, "death")
	abortions := countEvents(events, "abortion")
	rows = append(rows,
		mortalityProjectionRow(period, start, end, "summary", "total", "death_count", "Deaths", "death_count", deaths, 0),
		mortalityProjectionRow(period, start, end, "summary", "total", "abortion_count", "Abortions", "abortion_count", abortions, 1),
	)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "breed", "breed", "mortality_event_count", 10, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.BreedKey, event.BreedLabel, "unknown_breed", "Unknown breed")
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "farm", "farm", "mortality_event_count", 100, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.FarmKey, event.FarmLabel, "unknown_farm", "Unknown farm")
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "load", "load", "mortality_event_count", 200, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.LoadKey, event.LoadLabel, "unknown_load", "Unknown load")
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "delivery", "delivery", "mortality_event_count", 300, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.DeliveryKey, event.DeliveryLabel, "unknown_delivery", "Unknown delivery")
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "gender", "sex", "mortality_event_count", 400, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.Sex, event.Sex, "unknown", "Unknown")
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "status", "status", "mortality_event_count", 500, func(event ports.EventInputRow) (string, string) {
		return event.EventType, strings.Title(event.EventType)
	})...)
	rows = append(rows, mortalityGroupRows(period, start, end, events, "housing", "housing", "mortality_event_count", 600, func(event ports.EventInputRow) (string, string) {
		return mortalityDimension(event.HousingKey, event.HousingLabel, "unknown_housing", "Unknown housing")
	})...)
	return rows
}

func mortalityGroupRows(period string, start, end *string, events []ports.EventInputRow, section, grain, metric string, offset int, keyLabel func(ports.EventInputRow) (string, string)) []ports.ProjectionInputRow {
	type bucket struct {
		label string
		count float64
	}
	buckets := map[string]bucket{}
	for _, event := range events {
		key, label := keyLabel(event)
		item := buckets[key]
		item.label = label
		item.count++
		buckets[key] = item
	}
	keys := make([]string, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := []ports.ProjectionInputRow{}
	for index, key := range keys {
		item := buckets[key]
		rows = append(rows, mortalityProjectionRow(period, start, end, section, grain, key, item.label, metric, item.count, offset+index))
	}
	return rows
}

func mortalityProjectionRow(period string, start, end *string, section, grain, dimensionKey, dimensionLabel, metricKey string, value float64, sortOrder int) ports.ProjectionInputRow {
	numerator := value
	composition := "legacy_only"
	return ports.ProjectionInputRow{
		Period:                     period,
		PeriodStart:                start,
		PeriodEnd:                  end,
		Section:                    section,
		Grain:                      grain,
		DimensionKey:               dimensionKey,
		DimensionLabel:             dimensionLabel,
		MetricKey:                  metricKey,
		Numerator:                  &numerator,
		NumeratorSourceComposition: &composition,
		Value:                      value,
		Unit:                       "count",
		SortOrder:                  sortOrder,
		SourceHash:                 mortalityStableHash("mortality-projection", period, section, grain, dimensionKey, metricKey, strconv.FormatFloat(value, 'f', -1, 64)),
		SourceComposition:          composition,
	}
}

func countEvents(events []ports.EventInputRow, eventType string) float64 {
	var count float64
	for _, event := range events {
		if event.EventType == eventType {
			count++
		}
	}
	return count
}

func monthBounds(month string) (string, string) {
	start := month + "-01"
	parsed, err := time.Parse("2006-01-02", start)
	if err != nil {
		return start, start
	}
	end := parsed.AddDate(0, 1, -1).Format("2006-01-02")
	return start, end
}

func mortalityDimension(key, label *string, fallbackKey, fallbackLabel string) (string, string) {
	if key != nil && strings.TrimSpace(*key) != "" {
		outLabel := strings.TrimSpace(*key)
		if label != nil && strings.TrimSpace(*label) != "" {
			outLabel = strings.TrimSpace(*label)
		}
		return strings.TrimSpace(*key), outLabel
	}
	if label != nil && strings.TrimSpace(*label) != "" {
		value := strings.ToLower(strings.Join(strings.Fields(*label), "_"))
		return value, strings.TrimSpace(*label)
	}
	return fallbackKey, fallbackLabel
}

func decodeMortalitySyncBody(raw []byte) (mortalitySyncBody, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return mortalitySyncBody{}, nil
	}
	var body mortalitySyncBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return body, BadRequest("invalid_json", "request body must be valid JSON")
	}
	return body, nil
}

func validateMortalitySyncHeaders(rawTenantID, rawActorID, rawClientKey string) (tenantID, actorID, clientKey string, err error) {
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

func mortalityDryRunResponse(clientKey, traceID string, rowsRead int) *domain.MortalitySyncRunResponse {
	return &domain.MortalitySyncRunResponse{
		SyncRunID:      "dry-run",
		Mode:           "dry_run",
		Status:         "completed",
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

func mapMortalitySyncRepoErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ports.ErrIdempotencyConflict):
		return BadRequest("idempotency_conflict", "Idempotency-Key was reused with a different request")
	case errors.Is(err, ports.ErrIdempotencyPending):
		return BadRequest("idempotency_pending", "Idempotency-Key is already in progress")
	default:
		return Internal("mortality sync failed")
	}
}

func mortalityStoredSyncKey(tenantID, command, clientKey string) string {
	return strings.Join([]string{tenantID, command, clientKey}, ":")
}

func mortalityRequestHash(raw []byte) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}
	sum := sha256.Sum256(trimmed)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mortalityStableHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func mortalityStringField(payload map[string]any, keys ...string) string {
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

func mortalityStringPtrField(payload map[string]any, keys ...string) *string {
	value := mortalityStringField(payload, keys...)
	if value == "" {
		return nil
	}
	return &value
}

func mortalityUUIDPtr(value string) *string {
	value = strings.TrimSpace(value)
	if !uuidutil.IsUUIDString(value) {
		return nil
	}
	return &value
}

func mortalityNormalizeEnumPtr(value *string, allowed map[string]bool) *string {
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

func mortalityFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mortalityValueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func mortalityMaxTextPtr(current, candidate *string) *string {
	if candidate == nil || strings.TrimSpace(*candidate) == "" {
		return current
	}
	value := strings.TrimSpace(*candidate)
	if current == nil || value > *current {
		return &value
	}
	return current
}

func mortalityDateFromText(value *string) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(*value)
	if len(text) >= 10 {
		text = text[:10]
	}
	if _, err := time.Parse("2006-01-02", text); err != nil {
		return ""
	}
	return text
}
