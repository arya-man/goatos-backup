// Package postgres implements Counts/Shifting persistence over Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

const defaultQueryTimeout = 3 * time.Second

const (
	countBaseAnchorRecordedEventType = domain.EventBaseCountAnchorRecorded
	shiftingEventRecordedEventType   = domain.EventShiftingEventRecorded
	countsEventSchemaVersion         = "1.0.0"
	countsEventSchemaRef             = "contracts/jsonschema/domain-event-envelope.schema.json"
	countsEventTopic                 = "counts.events"
)

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) RecordBaseCountAnchor(ctx context.Context, in domain.BaseCountAnchor) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
INSERT INTO count_base_anchors (
  tenant_id, park_id, shed_id, breed_id, breed_key, breed_label, counted_at, head_count,
  source_system, source_ref, source_hash, discrepancy_state, idempotency_key, request_fingerprint, recorded_by
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, '')::uuid, $5, $6, $7, $8,
  $9, $10, $11, $12, $13, $14, nullif($15::text, '')::uuid
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING base_count_anchor_id::text`,
		in.TenantID, in.ParkID, in.ShedID, ptrValue(in.BreedID), in.BreedKey, in.BreedLabel,
		in.CountedAt, in.HeadCount, in.SourceSystem, in.SourceRef, in.SourceHash, in.DiscrepancyState,
		in.IdempotencyKey, in.RequestFingerprint, ptrValue(in.RecordedBy)).Scan(&id)
	if err == nil {
		if err := createBaseCountMismatchException(ctx, tx, in.TenantID, id, in); err != nil {
			return "", false, err
		}
		if err := insertCountsProjectionInputOutbox(ctx, tx, countsProjectionInputEvent{
			EventType: countBaseAnchorRecordedEventType,
			TenantID:  in.TenantID, AggregateType: "count_base_anchor", AggregateID: id,
			SubjectType: "count_base_anchor", SubjectID: id,
			ParkID: in.ParkID, ShedID: in.ShedID,
			Payload: map[string]any{
				"input_kind":              "base_count_anchor",
				"base_count_anchor_id":    id,
				"park_id":                 in.ParkID,
				"shed_id":                 in.ShedID,
				"breed_key":               in.BreedKey,
				"counted_at":              in.CountedAt.UTC().Format(time.RFC3339Nano),
				"source_hash":             in.SourceHash,
				"source_contract_version": domain.SourceContractVersionV1,
				"recompute_horizons":      []string{"count_as_of", "feed_target_date"},
			},
			EvidenceType: "count_base_anchor", EvidenceID: id,
		}); err != nil {
			return "", false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", false, err
		}
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("counts: insert base count anchor: %w", err)
	}
	_ = tx.Rollback(ctx)
	id, err = r.idempotentAnchor(ctx, in.TenantID, in.IdempotencyKey, in.RequestFingerprint)
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (r *Repository) RecordShiftingEvent(ctx context.Context, in domain.ShiftingEvent) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	id, replay, err := insertShiftingEvent(ctx, tx, in)
	if err != nil {
		return "", false, err
	}
	if replay {
		_ = tx.Rollback(ctx)
		return id, true, nil
	}
	for _, impact := range in.Impacts {
		if err := insertShiftingImpact(ctx, tx, in.TenantID, id, impact); err != nil {
			return "", false, err
		}
	}
	if err := insertCountsProjectionInputOutbox(ctx, tx, countsProjectionInputEvent{
		EventType: shiftingEventRecordedEventType,
		TenantID:  in.TenantID, AggregateType: "shifting_event", AggregateID: id,
		SubjectType: "shifting_event", SubjectID: id,
		ParkID: in.DestinationParkID, ShedID: in.DestinationShedID,
		Payload: map[string]any{
			"input_kind":                 "shifting_event",
			"shifting_event_id":          id,
			"logical_shifting_event_key": in.LogicalShiftingEventKey,
			"source_park_id":             ptrValue(in.SourceParkID),
			"source_shed_id":             ptrValue(in.SourceShedID),
			"destination_park_id":        in.DestinationParkID,
			"destination_shed_id":        in.DestinationShedID,
			"effective_at":               in.EffectiveAt.UTC().Format(time.RFC3339Nano),
			"authorization_state":        in.AuthorizationState,
			"event_status":               in.EventStatus,
			"payload_hash":               in.PayloadHash,
			"source_contract_version":    domain.SourceContractVersionV1,
			"recompute_horizons":         []string{"count_as_of", "feed_target_date"},
		},
		EvidenceType: "shifting_event", EvidenceID: id,
	}); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (r *Repository) ProjectionInputs(ctx context.Context, req domain.ProjectionRecomputeRequest) (domain.ProjectionInputs, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	anchors, err := r.projectionAnchors(ctx, req)
	if err != nil {
		return domain.ProjectionInputs{}, err
	}
	movements, err := r.projectionMovements(ctx, req)
	if err != nil {
		return domain.ProjectionInputs{}, err
	}
	inputs := domain.ProjectionInputs{Anchors: anchors, Movements: movements}
	if err := r.resolveProjectionAliases(ctx, req, &inputs); err != nil {
		return domain.ProjectionInputs{}, err
	}
	return inputs, nil
}

func (r *Repository) CreateProjectionSnapshot(ctx context.Context, in domain.ProjectionSnapshot) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	err = tx.QueryRow(ctx, `
INSERT INTO count_projection_snapshots (
  tenant_id, horizon, park_id, target_date, as_of, projection_status,
  source_contract_version, source_hash, base_anchor_ids_hash, shifting_event_ids_hash,
  row_count, exception_count, generated_by, trace_id
) VALUES (
  $1::uuid, $2, $3::uuid, $4, $5, $6,
  $7, $8, $9, $10,
  $11, $12, $13, nullif($14, '')
)
ON CONFLICT (tenant_id, horizon, park_id, target_date, source_hash) DO NOTHING
RETURNING count_projection_snapshot_id::text`,
		in.TenantID, in.Horizon, in.ParkID, dateOnly(in.TargetDate), in.AsOf, in.ProjectionStatus,
		in.SourceContractVersion, in.SourceHash, in.BaseAnchorIDsHash, in.ShiftingEventIDsHash,
		len(in.Rows), len(in.Exceptions), in.GeneratedBy, ptrValue(in.TraceID)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `
SELECT count_projection_snapshot_id::text
FROM count_projection_snapshots
WHERE tenant_id = $1::uuid AND horizon = $2 AND park_id = $3::uuid AND target_date = $4 AND source_hash = $5`,
			in.TenantID, in.Horizon, in.ParkID, dateOnly(in.TargetDate), in.SourceHash).Scan(&id); err != nil {
			return "", fmt.Errorf("counts: load existing projection snapshot: %w", err)
		}
		_ = tx.Rollback(ctx)
		return id, nil
	}
	if err != nil {
		return "", fmt.Errorf("counts: insert projection snapshot: %w", err)
	}
	for _, row := range in.Rows {
		if err := insertProjectionRow(ctx, tx, in.TenantID, id, row); err != nil {
			return "", err
		}
	}
	for _, exception := range in.Exceptions {
		if err := insertProjectionException(ctx, tx, in.TenantID, id, exception); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func (r *Repository) projectionAnchors(ctx context.Context, req domain.ProjectionRecomputeRequest) ([]domain.ProjectionBaseAnchor, error) {
	rows, err := r.pool.Query(ctx, `
SELECT DISTINCT ON (shed_id, lower(breed_key))
       base_count_anchor_id::text, park_id::text, shed_id::text,
       COALESCE(breed_id::text, ''), breed_key, breed_label,
       source_system, counted_at, head_count, source_hash
FROM count_base_anchors
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND counted_at <= $3
  AND anchor_state = 'adopted'
ORDER BY shed_id, lower(breed_key), counted_at DESC, base_count_anchor_id DESC`,
		req.TenantID, req.ParkID, req.AsOf)
	if err != nil {
		return nil, fmt.Errorf("counts: query projection anchors: %w", err)
	}
	defer rows.Close()
	out := []domain.ProjectionBaseAnchor{}
	for rows.Next() {
		var anchor domain.ProjectionBaseAnchor
		var breedID string
		if err := rows.Scan(&anchor.BaseCountAnchorID, &anchor.ParkID, &anchor.ShedID,
			&breedID, &anchor.BreedKey, &anchor.BreedLabel, &anchor.SourceSystem, &anchor.CountedAt,
			&anchor.HeadCount, &anchor.SourceHash); err != nil {
			return nil, fmt.Errorf("counts: scan projection anchor: %w", err)
		}
		anchor.BreedID = ptrIfNotEmpty(breedID)
		anchor.SourceBreedKey = anchor.BreedKey
		out = append(out, anchor)
	}
	return out, rows.Err()
}

func (r *Repository) projectionMovements(ctx context.Context, req domain.ProjectionRecomputeRequest) ([]domain.ProjectionMovementImpact, error) {
	query, args := movementWindowQuery(req)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("counts: query projection movements: %w", err)
	}
	defer rows.Close()
	out := []domain.ProjectionMovementImpact{}
	for rows.Next() {
		var movement domain.ProjectionMovementImpact
		var sourceShed, breedID, stage, age, sex, rationRef, blocker string
		if err := rows.Scan(&movement.ShiftingEventID, &movement.LogicalShiftingEventKey,
			&movement.SourceSystem, &sourceShed, &movement.DestinationShedID, &movement.EffectiveAt,
			&movement.GrainKey, &breedID, &movement.BreedKey, &movement.BreedLabel,
			&stage, &age, &sex, &movement.HeadCount, &movement.PregnantCount,
			&movement.LactatingCount, &movement.WarmupCount, &movement.RationContextResolutionState,
			&rationRef, &blocker); err != nil {
			return nil, fmt.Errorf("counts: scan projection movement: %w", err)
		}
		movement.SourceShedID = ptrIfNotEmpty(sourceShed)
		movement.BreedID = ptrIfNotEmpty(breedID)
		movement.StageTag = ptrIfNotEmpty(stage)
		movement.SourceBreedKey = movement.BreedKey
		movement.SourceStageTag = movement.StageTag
		movement.AgeClass = ptrIfNotEmpty(age)
		movement.Sex = ptrIfNotEmpty(sex)
		movement.RationContextRef = ptrIfNotEmpty(rationRef)
		movement.BlockerReason = ptrIfNotEmpty(blocker)
		out = append(out, movement)
	}
	return out, rows.Err()
}

func movementWindowQuery(req domain.ProjectionRecomputeRequest) (string, []any) {
	base := `
SELECT se.shifting_event_id::text, se.logical_shifting_event_key,
       se.source_system,
       COALESCE(se.source_shed_id::text, ''), se.destination_shed_id::text,
       se.effective_at, sei.grain_key, COALESCE(sei.breed_id::text, ''),
       sei.breed_key, sei.breed_label, COALESCE(sei.stage_tag, ''),
       COALESCE(sei.age_class, ''), COALESCE(sei.sex, ''),
       sei.head_count, sei.pregnant_count, sei.lactating_count, sei.warmup_count,
       sei.ration_context_resolution_state, COALESCE(sei.ration_context_ref, ''),
       COALESCE(sei.blocker_reason, '')
FROM shifting_events se
JOIN shifting_event_impacts sei
  ON sei.tenant_id = se.tenant_id
 AND sei.shifting_event_id = se.shifting_event_id
WHERE se.tenant_id = $1::uuid
  AND (se.source_park_id = $2::uuid OR se.destination_park_id = $2::uuid)
  AND se.event_status NOT IN ('rejected', 'canceled', 'unresolved')
`
	if req.Horizon == "count_as_of" {
		return base + `
  AND se.event_status = 'applied'
  AND se.effective_at <= $3
ORDER BY se.effective_at, se.shifting_event_id, sei.grain_key`, []any{req.TenantID, req.ParkID, req.AsOf}
	}
	start := dateOnly(req.TargetDate)
	end := start.AddDate(0, 0, 1)
	return base + `
  AND se.authorization_state = 'authorized'
  AND se.event_status IN ('authorized', 'applied')
  AND se.effective_at >= $3
  AND se.effective_at < $4
ORDER BY se.effective_at, se.shifting_event_id, sei.grain_key`, []any{req.TenantID, req.ParkID, start, end}
}

type countAliasMapping struct {
	CanonicalValue string
	CanonicalLabel string
}

type breedAliasMapping struct {
	BreedID       string
	CanonicalName string
}

func (r *Repository) resolveProjectionAliases(ctx context.Context, req domain.ProjectionRecomputeRequest, inputs *domain.ProjectionInputs) error {
	approved, err := r.approvedCountAliases(ctx, req.TenantID, req.AsOf)
	if err != nil {
		return err
	}
	breedAliases, err := r.activeBreedAliases(ctx)
	if err != nil {
		return err
	}
	for i := range inputs.Anchors {
		anchor := &inputs.Anchors[i]
		resolved, blocker := resolveBreedAlias(anchor.BreedKey, anchor.SourceSystem, approved, breedAliases)
		if resolved.CanonicalValue != "" {
			anchor.BreedKey = countAliasNorm(resolved.CanonicalValue)
			anchor.BreedLabel = defaultString(resolved.CanonicalLabel, resolved.CanonicalValue)
			if breed, ok := breedAliases[countAliasNorm(resolved.CanonicalValue)]; ok {
				anchor.BreedID = &breed.BreedID
				anchor.BreedLabel = breed.CanonicalName
			}
		}
		if blocker != "" {
			anchor.AliasBlockerReason = appendAliasBlocker(anchor.AliasBlockerReason, blocker)
		}
	}
	for i := range inputs.Movements {
		movement := &inputs.Movements[i]
		resolved, blocker := resolveBreedAlias(movement.BreedKey, movement.SourceSystem, approved, breedAliases)
		if resolved.CanonicalValue != "" {
			movement.BreedKey = countAliasNorm(resolved.CanonicalValue)
			movement.BreedLabel = defaultString(resolved.CanonicalLabel, resolved.CanonicalValue)
			if breed, ok := breedAliases[countAliasNorm(resolved.CanonicalValue)]; ok {
				movement.BreedID = &breed.BreedID
				movement.BreedLabel = breed.CanonicalName
			}
		}
		if blocker != "" {
			movement.AliasBlockerReason = appendAliasBlocker(movement.AliasBlockerReason, blocker)
		}
		if movement.StageTag != nil {
			stage, stageBlocker := resolveDimensionAlias("stage_tag", *movement.StageTag, movement.SourceSystem, approved)
			if stage.CanonicalValue != "" {
				canonical := countAliasNorm(stage.CanonicalValue)
				movement.StageTag = &canonical
			}
			if stageBlocker != "" {
				movement.AliasBlockerReason = appendAliasBlocker(movement.AliasBlockerReason, stageBlocker)
			}
		}
	}
	return nil
}

func (r *Repository) approvedCountAliases(ctx context.Context, tenantID string, asOf time.Time) (map[string]countAliasMapping, error) {
	rows, err := r.pool.Query(ctx, `
SELECT dimension, source_system, source_value_norm, canonical_value, COALESCE(canonical_label, '')
FROM count_dimension_aliases
WHERE tenant_id = $1::uuid
  AND review_status = 'approved'
  AND effective_from <= $2::date
  AND (effective_to IS NULL OR effective_to > $2::date)`, tenantID, dateOnly(asOf))
	if err != nil {
		return nil, fmt.Errorf("counts: query approved aliases: %w", err)
	}
	defer rows.Close()
	out := map[string]countAliasMapping{}
	for rows.Next() {
		var dimension, sourceSystem, sourceValueNorm string
		var mapping countAliasMapping
		if err := rows.Scan(&dimension, &sourceSystem, &sourceValueNorm, &mapping.CanonicalValue, &mapping.CanonicalLabel); err != nil {
			return nil, fmt.Errorf("counts: scan approved alias: %w", err)
		}
		out[countAliasKey(dimension, sourceSystem, sourceValueNorm)] = mapping
	}
	return out, rows.Err()
}

func (r *Repository) activeBreedAliases(ctx context.Context) (map[string]breedAliasMapping, error) {
	rows, err := r.pool.Query(ctx, `
SELECT ba.normalized_alias, b.breed_id::text, b.canonical_name
FROM breed_aliases ba
JOIN breeds b ON b.breed_id = ba.breed_id
WHERE b.status = 'active'
ORDER BY ba.source_system NULLS LAST, ba.alias_id`)
	if err != nil {
		return nil, fmt.Errorf("counts: query breed aliases: %w", err)
	}
	defer rows.Close()
	out := map[string]breedAliasMapping{}
	for rows.Next() {
		var norm string
		var mapping breedAliasMapping
		if err := rows.Scan(&norm, &mapping.BreedID, &mapping.CanonicalName); err != nil {
			return nil, fmt.Errorf("counts: scan breed alias: %w", err)
		}
		if _, exists := out[norm]; !exists {
			out[norm] = mapping
		}
	}
	return out, rows.Err()
}

func resolveBreedAlias(raw, sourceSystem string, approved map[string]countAliasMapping, breedAliases map[string]breedAliasMapping) (countAliasMapping, string) {
	norm := countAliasNorm(raw)
	if norm == "" {
		return countAliasMapping{}, ""
	}
	if mapping, ok := approved[countAliasKey("breed", sourceSystem, norm)]; ok {
		return mapping, ""
	}
	if mapping, ok := approved[countAliasKey("breed", "*", norm)]; ok {
		return mapping, ""
	}
	if breed, ok := breedAliases[norm]; ok {
		return countAliasMapping{CanonicalValue: breed.CanonicalName, CanonicalLabel: breed.CanonicalName}, ""
	}
	return countAliasMapping{}, fmt.Sprintf("unreviewed breed alias %q from source_system %q", raw, sourceSystem)
}

func resolveDimensionAlias(dimension, raw, sourceSystem string, approved map[string]countAliasMapping) (countAliasMapping, string) {
	norm := countAliasNorm(raw)
	if norm == "" {
		return countAliasMapping{}, ""
	}
	if mapping, ok := approved[countAliasKey(dimension, sourceSystem, norm)]; ok {
		return mapping, ""
	}
	if mapping, ok := approved[countAliasKey(dimension, "*", norm)]; ok {
		return mapping, ""
	}
	return countAliasMapping{}, fmt.Sprintf("unreviewed %s alias %q from source_system %q", dimension, raw, sourceSystem)
}

func countAliasKey(dimension, sourceSystem, norm string) string {
	return strings.TrimSpace(dimension) + "\x00" + strings.TrimSpace(sourceSystem) + "\x00" + strings.TrimSpace(norm)
}

func countAliasNorm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "_"))
}

func countProjectionGrainKey(shedID, breedKey string) string {
	return strings.ToLower(strings.TrimSpace(shedID)) + ":" + strings.ToLower(strings.TrimSpace(breedKey))
}

func appendAliasBlocker(existing *string, reason string) *string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return existing
	}
	if existing == nil || strings.TrimSpace(*existing) == "" {
		return &reason
	}
	combined := strings.TrimSpace(*existing) + "; " + reason
	return &combined
}

func (r *Repository) CountAsOf(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	req.TargetDate = req.AsOf
	return r.projection(ctx, "count_as_of", req)
}

func (r *Repository) ProjectedCountFor(ctx context.Context, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	return r.projection(ctx, "feed_target_date", req)
}

func (r *Repository) Readiness(ctx context.Context, tenantID string) (domain.Readiness, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := domain.Readiness{
		TenantID:          tenantID,
		Status:            domain.ReadinessBlocked,
		GenerationAllowed: false,
		Subgates:          defaultSubgates(time.Now().UTC()),
	}
	rows, err := r.pool.Query(ctx, `
SELECT subgate_id, status, owner, evidence_ref, blocker_reason, COALESCE(implementation_ref, ''), last_checked_at
FROM counts_shifting_readiness_subgates
WHERE tenant_id = $1::uuid`, tenantID)
	if err != nil {
		return domain.Readiness{}, fmt.Errorf("counts: readiness subgates: %w", err)
	}
	defer rows.Close()
	byID := map[string]int{}
	for i, subgate := range out.Subgates {
		byID[subgate.ID] = i
	}
	for rows.Next() {
		var sg domain.ReadinessSubgate
		var status, implementation string
		if err := rows.Scan(&sg.ID, &status, &sg.Owner, &sg.EvidenceRef, &sg.BlockerReason, &implementation, &sg.LastCheckedAt); err != nil {
			return domain.Readiness{}, fmt.Errorf("counts: scan readiness subgate: %w", err)
		}
		sg.Status = domain.ReadinessStatus(status)
		sg.ImplementationRef = implementation
		if idx, ok := byID[sg.ID]; ok {
			out.Subgates[idx] = sg
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Readiness{}, err
	}
	if err := r.pool.QueryRow(ctx, `
SELECT count(*) FROM count_projection_exceptions
WHERE tenant_id = $1::uuid AND status = 'open'`, tenantID).Scan(&out.OpenExceptionCount); err != nil {
		return domain.Readiness{}, fmt.Errorf("counts: readiness exception count: %w", err)
	}
	var target pgtype.Date
	if err := r.pool.QueryRow(ctx, `
SELECT projection_status, target_date, row_count
FROM count_projection_snapshots
WHERE tenant_id = $1::uuid
ORDER BY created_at DESC, count_projection_snapshot_id DESC
LIMIT 1`, tenantID).Scan(&out.LatestProjectionStatus, &target, &out.LatestProjectionRowCount); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Readiness{}, fmt.Errorf("counts: latest projection: %w", err)
	}
	if target.Valid {
		t := target.Time
		out.LatestProjectionTarget = &t
	}
	allReady := out.OpenExceptionCount == 0 && out.LatestProjectionStatus == "ready"
	for _, subgate := range out.Subgates {
		if subgate.Status != domain.ReadinessReady {
			allReady = false
			break
		}
	}
	if allReady {
		out.Status = domain.ReadinessReady
		out.GenerationAllowed = true
	}
	return out, nil
}

func (r *Repository) idempotentAnchor(ctx context.Context, tenantID, key, fingerprint string) (string, error) {
	var id, existingFP string
	if err := r.pool.QueryRow(ctx, `
SELECT base_count_anchor_id::text, request_fingerprint
FROM count_base_anchors
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, tenantID, key).Scan(&id, &existingFP); err != nil {
		return "", fmt.Errorf("counts: load base count idempotency: %w", err)
	}
	if existingFP != fingerprint {
		return "", ports.ErrIdempotencyConflict
	}
	return id, nil
}

type previousBaseCountAnchor struct {
	ID        string
	CountedAt time.Time
	HeadCount int32
}

func createBaseCountMismatchException(ctx context.Context, tx pgx.Tx, tenantID, anchorID string, in domain.BaseCountAnchor) error {
	previous, found, err := loadPreviousBaseCountAnchor(ctx, tx, tenantID, anchorID, in)
	if err != nil || !found {
		return err
	}
	netShift, err := appliedShiftNetForAnchorWindow(ctx, tx, tenantID, in, previous.CountedAt)
	if err != nil {
		return err
	}
	expected := previous.HeadCount + netShift
	if expected == in.HeadCount {
		return nil
	}
	actualDelta := in.HeadCount - previous.HeadCount
	unexplainedDelta := in.HeadCount - expected
	exceptionType := "count_mismatch"
	if netShift == 0 {
		exceptionType = "unreported_shifting"
	}
	reason := fmt.Sprintf(
		"physical Base Count differs from prior anchor plus applied shiftings for shed/breed grain: previous=%d, applied_shift_net=%d, expected=%d, actual=%d, unexplained_delta=%d",
		previous.HeadCount, netShift, expected, in.HeadCount, unexplainedDelta,
	)
	evidence, err := json.Marshal(map[string]any{
		"source":                         "base_count_anchor_reconciliation",
		"base_count_anchor_id":           anchorID,
		"previous_base_count_anchor_id":  previous.ID,
		"park_id":                        in.ParkID,
		"shed_id":                        in.ShedID,
		"breed_key":                      in.BreedKey,
		"previous_counted_at":            previous.CountedAt.UTC().Format(time.RFC3339Nano),
		"current_counted_at":             in.CountedAt.UTC().Format(time.RFC3339Nano),
		"previous_head_count":            previous.HeadCount,
		"actual_head_count":              in.HeadCount,
		"actual_delta":                   actualDelta,
		"applied_shifting_net":           netShift,
		"expected_head_count":            expected,
		"unexplained_delta":              unexplainedDelta,
		"source_contract_version":        domain.SourceContractVersionV1,
		"discrepancy_state_after_record": "investigating",
	})
	if err != nil {
		return fmt.Errorf("counts: build count mismatch evidence: %w", err)
	}
	grainKey := countProjectionGrainKey(in.ShedID, in.BreedKey)
	exception := domain.ProjectionException{
		ExceptionType: exceptionType,
		SourceKey:     "base_count_anchor:" + anchorID,
		GrainKey:      grainKey,
		ParkID:        &in.ParkID,
		ShedID:        &in.ShedID,
		BreedKey:      &in.BreedKey,
		Severity:      "blocking",
		WorkType:      "counts_projection_exception",
		WorkState:     "owner_missing",
		DueAt:         in.CountedAt.UTC().Add(2 * time.Hour),
		NextAction:    "Investigate count mismatch before Feed generation",
		EvidenceLink:  "/feed-direction/counts-projection/exceptions/base_count_anchor:" + anchorID,
		BlockerReason: reason,
		EvidenceJSON:  evidence,
	}
	if exceptionType == "unreported_shifting" {
		exception.NextAction = "Review mismatch and create or confirm the missing ShiftingEvent"
	}
	if err := insertProjectionException(ctx, tx, tenantID, "", exception); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE count_base_anchors
SET discrepancy_state = 'investigating', updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND base_count_anchor_id = $2::uuid`, tenantID, anchorID); err != nil {
		return fmt.Errorf("counts: mark base count discrepancy investigating: %w", err)
	}
	return nil
}

func loadPreviousBaseCountAnchor(ctx context.Context, tx pgx.Tx, tenantID, anchorID string, in domain.BaseCountAnchor) (previousBaseCountAnchor, bool, error) {
	var out previousBaseCountAnchor
	err := tx.QueryRow(ctx, `
SELECT base_count_anchor_id::text, counted_at, head_count
FROM count_base_anchors
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND shed_id = $3::uuid
  AND lower(breed_key) = lower($4)
  AND counted_at < $5
  AND anchor_state = 'adopted'
  AND base_count_anchor_id <> $6::uuid
ORDER BY counted_at DESC, base_count_anchor_id DESC
LIMIT 1`, tenantID, in.ParkID, in.ShedID, in.BreedKey, in.CountedAt, anchorID).Scan(&out.ID, &out.CountedAt, &out.HeadCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return previousBaseCountAnchor{}, false, nil
	}
	if err != nil {
		return previousBaseCountAnchor{}, false, fmt.Errorf("counts: load previous base count anchor: %w", err)
	}
	return out, true, nil
}

func appliedShiftNetForAnchorWindow(ctx context.Context, tx pgx.Tx, tenantID string, in domain.BaseCountAnchor, previousCountedAt time.Time) (int32, error) {
	var net int32
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(
  CASE
    WHEN se.destination_park_id = $2::uuid AND se.destination_shed_id = $3::uuid THEN sei.head_count
    WHEN se.source_park_id = $2::uuid AND se.source_shed_id = $3::uuid THEN -sei.head_count
    ELSE 0
  END
), 0)::integer
FROM shifting_events se
JOIN shifting_event_impacts sei
  ON sei.tenant_id = se.tenant_id
 AND sei.shifting_event_id = se.shifting_event_id
WHERE se.tenant_id = $1::uuid
  AND se.event_status = 'applied'
  AND se.effective_at > $5
  AND se.effective_at <= $6
  AND lower(sei.breed_key) = lower($4)
  AND (
    (se.destination_park_id = $2::uuid AND se.destination_shed_id = $3::uuid)
    OR (se.source_park_id = $2::uuid AND se.source_shed_id = $3::uuid)
  )`, tenantID, in.ParkID, in.ShedID, in.BreedKey, previousCountedAt, in.CountedAt).Scan(&net); err != nil {
		return 0, fmt.Errorf("counts: compute applied shifting net: %w", err)
	}
	return net, nil
}

func insertShiftingEvent(ctx context.Context, tx pgx.Tx, in domain.ShiftingEvent) (string, bool, error) {
	if id, found, err := maybeExistingShiftingByIdempotency(ctx, tx, in); err != nil || found {
		return id, found, err
	}
	if id, found, err := maybeExistingShiftingByLogicalKey(ctx, tx, in); err != nil || found {
		return id, found, err
	}
	var id string
	err := tx.QueryRow(ctx, `
INSERT INTO shifting_events (
  tenant_id, logical_shifting_event_key, priority, category, source_park_id, source_shed_id,
  destination_park_id, destination_shed_id, raised_at, effective_at, authorized_at, authorized_by,
  authorization_state, verification_state, event_status, source_system, source_ref, proof_ref,
  payload_hash, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2, $3, $4, nullif($5::text, '')::uuid, nullif($6::text, '')::uuid,
  $7::uuid, $8::uuid, $9, $10, $11, nullif($12::text, '')::uuid,
  $13, $14, $15, $16, $17, nullif($18, ''),
  $19, $20, $21
)
RETURNING shifting_event_id::text`,
		in.TenantID, in.LogicalShiftingEventKey, in.Priority, in.Category, ptrValue(in.SourceParkID), ptrValue(in.SourceShedID),
		in.DestinationParkID, in.DestinationShedID, in.RaisedAt, in.EffectiveAt, nullableTime(in.AuthorizedAt), ptrValue(in.AuthorizedBy),
		in.AuthorizationState, in.VerificationState, in.EventStatus, in.SourceSystem, in.SourceRef, ptrValue(in.ProofRef),
		in.PayloadHash, in.IdempotencyKey, in.RequestFingerprint).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "shifting_events_logical_key_unique" {
		return "", false, ports.ErrLogicalKeyConflict
	}
	if errors.As(err, &pgErr) && pgErr.ConstraintName == "shifting_events_idempotency_unique" {
		return "", false, ports.ErrIdempotencyConflict
	}
	return "", false, fmt.Errorf("counts: insert shifting event: %w", err)
}

func maybeExistingShiftingByIdempotency(ctx context.Context, tx pgx.Tx, in domain.ShiftingEvent) (string, bool, error) {
	var id, existingFP string
	err := tx.QueryRow(ctx, `
SELECT shifting_event_id::text, request_fingerprint
FROM shifting_events
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, in.TenantID, in.IdempotencyKey).Scan(&id, &existingFP)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("counts: load shifting idempotency: %w", err)
	}
	if existingFP != in.RequestFingerprint {
		return "", false, ports.ErrIdempotencyConflict
	}
	return id, true, nil
}

func maybeExistingShiftingByLogicalKey(ctx context.Context, tx pgx.Tx, in domain.ShiftingEvent) (string, bool, error) {
	var id, existingHash string
	err := tx.QueryRow(ctx, `
SELECT shifting_event_id::text, payload_hash
FROM shifting_events
WHERE tenant_id = $1::uuid AND logical_shifting_event_key = $2`, in.TenantID, in.LogicalShiftingEventKey).Scan(&id, &existingHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("counts: load shifting logical key: %w", err)
	}
	if existingHash != in.PayloadHash {
		return "", false, ports.ErrLogicalKeyConflict
	}
	return id, true, nil
}

func insertShiftingImpact(ctx context.Context, tx pgx.Tx, tenantID, eventID string, impact domain.ShiftingEventImpact) error {
	_, err := tx.Exec(ctx, `
INSERT INTO shifting_event_impacts (
  tenant_id, shifting_event_id, grain_key, breed_id, breed_key, breed_label, stage_tag, age_class, sex,
  head_count, pregnant_count, lactating_count, warmup_count, risk_flags,
  ration_context_resolution_state, ration_context_ref, blocker_reason
) VALUES (
  $1::uuid, $2::uuid, $3, nullif($4::text, '')::uuid, $5, $6, nullif($7, ''), nullif($8, ''), nullif($9, ''),
  $10, $11, $12, $13, $14::jsonb,
  $15, nullif($16, ''), nullif($17, '')
)`, tenantID, eventID, impact.GrainKey, ptrValue(impact.BreedID), impact.BreedKey, impact.BreedLabel,
		ptrValue(impact.StageTag), ptrValue(impact.AgeClass), ptrValue(impact.Sex), impact.HeadCount,
		impact.PregnantCount, impact.LactatingCount, impact.WarmupCount, jsonObject(impact.RiskFlagsJSON),
		defaultResolution(impact.RationContextResolutionState), ptrValue(impact.RationContextRef), ptrValue(impact.BlockerReason))
	if err != nil {
		return fmt.Errorf("counts: insert shifting impact: %w", err)
	}
	return nil
}

func insertProjectionRow(ctx context.Context, tx pgx.Tx, tenantID, snapshotID string, row domain.ProjectionRow) error {
	_, err := tx.Exec(ctx, `
INSERT INTO count_projection_snapshot_rows (
  tenant_id, count_projection_snapshot_id, park_id, shed_id, target_date, grain_key,
  base_count_anchor_id, included_shifting_event_ids_hash,
  breed_id, breed_key, breed_label, stage_tag, age_class, sex,
  head_count, pregnant_count, lactating_count, warmup_count,
  ration_context_resolution_state, ration_context_ref, blocker_reason, source_row_hash
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6,
  $7::uuid, $8,
  nullif($9::text, '')::uuid, $10, $11, nullif($12, ''), nullif($13, ''), nullif($14, ''),
  $15, $16, $17, $18,
  $19, nullif($20, ''), nullif($21, ''), $22
)`, tenantID, snapshotID, row.ParkID, row.ShedID, dateOnly(row.TargetDate), row.GrainKey,
		row.BaseCountAnchorID, row.IncludedShiftingEventIDsHash,
		ptrValue(row.BreedID), row.BreedKey, row.BreedLabel, ptrValue(row.StageTag), ptrValue(row.AgeClass), ptrValue(row.Sex),
		row.HeadCount, row.PregnantCount, row.LactatingCount, row.WarmupCount,
		defaultResolution(row.RationContextResolutionState), ptrValue(row.RationContextRef), ptrValue(row.BlockerReason), row.SourceRowHash)
	if err != nil {
		return fmt.Errorf("counts: insert projection row: %w", err)
	}
	return nil
}

func (r *Repository) projection(ctx context.Context, horizon string, req domain.CountProjectionRequest) (domain.CountProjection, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	targetDate := dateOnly(req.TargetDate)
	out := domain.CountProjection{
		TenantID: req.TenantID, Horizon: horizon, ParkID: req.ParkID, TargetDate: targetDate,
		ProjectionStatus: "blocked",
	}
	var target pgtype.Date
	err := r.pool.QueryRow(ctx, `
SELECT count_projection_snapshot_id::text, projection_status, source_contract_version,
       source_hash, base_anchor_ids_hash, shifting_event_ids_hash,
       exception_count, row_count, target_date
FROM count_projection_snapshots
WHERE tenant_id = $1::uuid
  AND horizon = $2
  AND park_id = $3::uuid
  AND target_date = $4
ORDER BY created_at DESC, count_projection_snapshot_id DESC
LIMIT 1`, req.TenantID, horizon, req.ParkID, targetDate).Scan(
		&out.SnapshotID, &out.ProjectionStatus, &out.SourceContractVersion,
		&out.SourceHash, &out.BaseAnchorIDsHash, &out.ShiftingEventIDsHash,
		&out.ExceptionCount, &out.TotalRowCount, &target,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Blockers = append(out.Blockers, domain.ProjectionBlocker{
			ExceptionType: "missing_projection_snapshot",
			SourceKey:     horizon + ":" + targetDate.Format("2006-01-02"),
			GrainKey:      "tenant:park:date",
			Severity:      "blocking",
			BlockerReason: "No Counts/Shifting projection snapshot exists for the requested tenant, park, horizon, and target date.",
		})
		return out, nil
	}
	if err != nil {
		return domain.CountProjection{}, fmt.Errorf("counts: load projection snapshot: %w", err)
	}
	if target.Valid {
		out.TargetDate = target.Time
	}
	rows, err := r.pool.Query(ctx, `
SELECT count_projection_snapshot_row_id::text, park_id::text, shed_id::text, target_date,
       grain_key, base_count_anchor_id::text, included_shifting_event_ids_hash,
       COALESCE(breed_id::text, ''), breed_key, breed_label,
       COALESCE(stage_tag, ''), COALESCE(age_class, ''), COALESCE(sex, ''),
       head_count, pregnant_count, lactating_count, warmup_count,
       ration_context_resolution_state, COALESCE(ration_context_ref, ''),
       COALESCE(blocker_reason, ''), source_row_hash
FROM count_projection_snapshot_rows
WHERE tenant_id = $1::uuid
  AND count_projection_snapshot_id = $2::uuid
  AND (nullif($3::text, '')::uuid IS NULL OR shed_id = nullif($3::text, '')::uuid)
  AND (nullif($4::text, '') IS NULL OR lower(breed_key) = lower(nullif($4::text, '')))
  AND (nullif($5::text, '') IS NULL OR ration_context_resolution_state = nullif($5::text, ''))
  AND (nullif($6::text, '')::uuid IS NULL OR count_projection_snapshot_row_id > nullif($6::text, '')::uuid)
ORDER BY count_projection_snapshot_row_id
LIMIT $7`, req.TenantID, out.SnapshotID, ptrValue(req.ShedID), ptrValue(req.BreedKey),
		ptrValue(req.RationContextResolutionState), ptrValue(req.Cursor), req.Limit+1)
	if err != nil {
		return domain.CountProjection{}, fmt.Errorf("counts: query projection rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row domain.ProjectionRow
		var target pgtype.Date
		var breedID, stage, age, sex, rationRef, blocker string
		if err := rows.Scan(&row.ProjectionRowID, &row.ParkID, &row.ShedID, &target,
			&row.GrainKey, &row.BaseCountAnchorID, &row.IncludedShiftingEventIDsHash,
			&breedID, &row.BreedKey, &row.BreedLabel,
			&stage, &age, &sex,
			&row.HeadCount, &row.PregnantCount, &row.LactatingCount, &row.WarmupCount,
			&row.RationContextResolutionState, &rationRef, &blocker, &row.SourceRowHash); err != nil {
			return domain.CountProjection{}, fmt.Errorf("counts: scan projection row: %w", err)
		}
		if target.Valid {
			row.TargetDate = target.Time
		}
		row.BreedID = ptrIfNotEmpty(breedID)
		row.StageTag = ptrIfNotEmpty(stage)
		row.AgeClass = ptrIfNotEmpty(age)
		row.Sex = ptrIfNotEmpty(sex)
		row.RationContextRef = ptrIfNotEmpty(rationRef)
		row.BlockerReason = ptrIfNotEmpty(blocker)
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return domain.CountProjection{}, err
	}
	if int32(len(out.Rows)) > req.Limit {
		limit := int(req.Limit)
		next := out.Rows[limit].ProjectionRowID
		out.NextCursor = &next
		out.Rows = out.Rows[:limit]
	}
	if err := r.loadOpenProjectionExceptions(ctx, out.SnapshotID, &out); err != nil {
		return domain.CountProjection{}, err
	}
	return out, nil
}

func (r *Repository) loadOpenProjectionExceptions(ctx context.Context, snapshotID string, out *domain.CountProjection) error {
	rows, err := r.pool.Query(ctx, `
SELECT count_projection_exception_id::text, exception_type, source_key, grain_key,
       COALESCE(park_id::text, ''), COALESCE(shed_id::text, ''), COALESCE(breed_key, ''),
       COALESCE(stage_tag, ''), severity, COALESCE(owner_ref, ''),
       work_type, work_state, due_at, next_action, evidence_link, blocker_reason, evidence_json
FROM count_projection_exceptions
WHERE (
    count_projection_snapshot_id = $1::uuid
    OR (
      count_projection_snapshot_id IS NULL
      AND tenant_id = $2::uuid
      AND (park_id IS NULL OR park_id = $3::uuid)
    )
  )
  AND status = 'open'
ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'blocking' THEN 1 ELSE 2 END,
         updated_at DESC, count_projection_exception_id DESC
LIMIT 50`, snapshotID, out.TenantID, out.ParkID)
	if err != nil {
		return fmt.Errorf("counts: query projection exceptions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var ex domain.ProjectionException
		var park, shed, breed, stage, owner string
		if err := rows.Scan(&ex.ProjectionExceptionID, &ex.ExceptionType, &ex.SourceKey, &ex.GrainKey,
			&park, &shed, &breed, &stage, &ex.Severity, &owner, &ex.WorkType, &ex.WorkState, &ex.DueAt,
			&ex.NextAction, &ex.EvidenceLink, &ex.BlockerReason, &ex.EvidenceJSON); err != nil {
			return fmt.Errorf("counts: scan projection exception: %w", err)
		}
		ex.ParkID = ptrIfNotEmpty(park)
		ex.ShedID = ptrIfNotEmpty(shed)
		ex.BreedKey = ptrIfNotEmpty(breed)
		ex.StageTag = ptrIfNotEmpty(stage)
		ex.OwnerRef = ptrIfNotEmpty(owner)
		out.Exceptions = append(out.Exceptions, ex)
	}
	return rows.Err()
}

func insertProjectionException(ctx context.Context, tx pgx.Tx, tenantID, snapshotID string, ex domain.ProjectionException) error {
	_, err := tx.Exec(ctx, `
INSERT INTO count_projection_exceptions (
  tenant_id, count_projection_snapshot_id, exception_type, source_key, grain_key,
  park_id, shed_id, breed_key, stage_tag, severity, owner_ref, work_type, work_state,
  due_at, next_action, evidence_link, blocker_reason, evidence_json
) VALUES (
  $1::uuid, nullif($2::text, '')::uuid, $3, $4, $5,
  nullif($6::text, '')::uuid, nullif($7::text, '')::uuid, nullif($8, ''), nullif($9, ''),
  $10, nullif($11, ''),
  COALESCE(NULLIF($12, ''), 'counts_projection_exception'),
  COALESCE(NULLIF($13, ''), CASE WHEN nullif($11, '') IS NULL THEN 'owner_missing' ELSE 'blocked' END),
  COALESCE($14::timestamptz, CASE $10 WHEN 'critical' THEN now() WHEN 'warning' THEN now() + interval '24 hours' ELSE now() + interval '2 hours' END),
  COALESCE(NULLIF($15, ''), 'Review Counts/Shifting projection exception'),
  COALESCE(NULLIF($16, ''), '/feed-direction/counts-projection/exceptions/' || $4),
  $17,
  $18::jsonb
)
ON CONFLICT (tenant_id, exception_type, source_key, grain_key) WHERE status = 'open'
DO UPDATE SET count_projection_snapshot_id = EXCLUDED.count_projection_snapshot_id,
              park_id = EXCLUDED.park_id,
              shed_id = EXCLUDED.shed_id,
              breed_key = EXCLUDED.breed_key,
              stage_tag = EXCLUDED.stage_tag,
              severity = EXCLUDED.severity,
              owner_ref = EXCLUDED.owner_ref,
              work_type = EXCLUDED.work_type,
              work_state = EXCLUDED.work_state,
              due_at = LEAST(count_projection_exceptions.due_at, EXCLUDED.due_at),
              next_action = EXCLUDED.next_action,
              evidence_link = EXCLUDED.evidence_link,
              blocker_reason = EXCLUDED.blocker_reason,
              evidence_json = EXCLUDED.evidence_json,
              updated_at = now()`,
		tenantID, snapshotID, ex.ExceptionType, ex.SourceKey, ex.GrainKey, ptrValue(ex.ParkID), ptrValue(ex.ShedID),
		ptrValue(ex.BreedKey), ptrValue(ex.StageTag), defaultString(ex.Severity, "blocking"), ptrValue(ex.OwnerRef),
		ex.WorkType, ex.WorkState, nullableZeroTime(ex.DueAt), ex.NextAction, ex.EvidenceLink, ex.BlockerReason, jsonObject(ex.EvidenceJSON))
	if err != nil {
		return fmt.Errorf("counts: insert projection exception: %w", err)
	}
	return nil
}

type countsProjectionInputEvent struct {
	EventType     string
	TenantID      string
	AggregateType string
	AggregateID   string
	SubjectType   string
	SubjectID     string
	ParkID        string
	ShedID        string
	Payload       map[string]any
	EvidenceType  string
	EvidenceID    string
}

func insertCountsProjectionInputOutbox(ctx context.Context, tx pgx.Tx, event countsProjectionInputEvent) error {
	eventID := platformoutbox.DeterministicUUID(event.EventType + ":" + event.TenantID + ":" + event.AggregateID)
	idempotencyKey := event.EventType + ":" + event.AggregateID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	visibility := map[string]any{"tenant_id": event.TenantID}
	if event.ParkID != "" {
		visibility["park_id"] = event.ParkID
	}
	if event.ShedID != "" {
		visibility["shed_id"] = event.ShedID
	}
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     event.EventType,
		"schema_version": countsEventSchemaVersion,
		"schema_ref":     countsEventSchemaRef,
		"aggregate_type": event.AggregateType,
		"aggregate_id":   event.AggregateID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "counts",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_id":   nil,
			"actor_ref":  nil,
		},
		"subject_type":     event.SubjectType,
		"subject_id":       event.SubjectID,
		"visibility_scope": visibility,
		"evidence_refs": []map[string]string{{
			"evidence_type": event.EvidenceType,
			"evidence_id":   event.EvidenceID,
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("counts: projection input envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "counts.RecordProjectionInput",
		"schema_version":  countsEventSchemaVersion,
		"idempotency_key": idempotencyKey,
		"event_type":      event.EventType,
	})
	if err != nil {
		return fmt.Errorf("counts: projection input headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $10, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		event.TenantID, eventID, event.EventType, countsEventSchemaVersion,
		event.AggregateType, event.AggregateID, countsEventTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return fmt.Errorf("counts: projection input outbox: %w", err)
	}
	return nil
}

func defaultSubgates(checkedAt time.Time) []domain.ReadinessSubgate {
	items := []struct {
		id      string
		blocker string
	}{
		{"CSG1", "Base Count anchor not proven."},
		{"CSG2", "Append-only ShiftingEvent ledger not proven."},
		{"CSG3", "Structured cohort/stage impacts not proven."},
		{"CSG4", "Realized count_as_of and one-day projected_count_for horizon split not proven."},
		{"CSG5", "Immediate physical Base Count adoption plus discrepancy investigation not proven."},
		{"CSG6", "Unreported-shifting and count-mismatch detection not proven."},
		{"CSG7", "Owner-approved breed/stage alias mapping coverage is not complete."},
		{"CSG8", "Idempotency/replay across ingestion, projection, and source replay not proven."},
		{"CSG9", "Feed projection API over bounded immutable rows not proven."},
		{"CSG10", "Scale, observability, source parity, and seeded E2E not proven."},
	}
	out := make([]domain.ReadinessSubgate, 0, len(items))
	for _, item := range items {
		out = append(out, domain.ReadinessSubgate{
			ID: item.id, Status: domain.ReadinessBlocked, Owner: "Counts/Shifting + Feed Direction",
			EvidenceRef:   "docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md",
			BlockerReason: item.blocker, LastCheckedAt: checkedAt,
		})
	}
	return out
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func ptrIfNotEmpty(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func nullableZeroTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func jsonObject(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func defaultResolution(value string) string {
	return defaultString(value, "unresolved")
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
