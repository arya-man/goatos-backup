// Package postgres implements Counts/Shifting persistence over Postgres.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

const defaultQueryTimeout = 3 * time.Second

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
	var id string
	err := r.pool.QueryRow(ctx, `
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
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("counts: insert base count anchor: %w", err)
	}
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
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id, false, nil
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
  breed_id, breed_key, breed_label, stage_tag, age_class, sex,
  head_count, pregnant_count, lactating_count, warmup_count,
  ration_context_resolution_state, ration_context_ref, blocker_reason, source_row_hash
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5, $6,
  nullif($7::text, '')::uuid, $8, $9, nullif($10, ''), nullif($11, ''), nullif($12, ''),
  $13, $14, $15, $16,
  $17, nullif($18, ''), nullif($19, ''), $20
)`, tenantID, snapshotID, row.ParkID, row.ShedID, dateOnly(row.TargetDate), row.GrainKey,
		ptrValue(row.BreedID), row.BreedKey, row.BreedLabel, ptrValue(row.StageTag), ptrValue(row.AgeClass), ptrValue(row.Sex),
		row.HeadCount, row.PregnantCount, row.LactatingCount, row.WarmupCount,
		defaultResolution(row.RationContextResolutionState), ptrValue(row.RationContextRef), ptrValue(row.BlockerReason), row.SourceRowHash)
	if err != nil {
		return fmt.Errorf("counts: insert projection row: %w", err)
	}
	return nil
}

func insertProjectionException(ctx context.Context, tx pgx.Tx, tenantID, snapshotID string, ex domain.ProjectionException) error {
	_, err := tx.Exec(ctx, `
INSERT INTO count_projection_exceptions (
  tenant_id, count_projection_snapshot_id, exception_type, source_key, grain_key,
  park_id, shed_id, breed_key, stage_tag, severity, owner_ref, blocker_reason, evidence_json
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5,
  nullif($6::text, '')::uuid, nullif($7::text, '')::uuid, nullif($8, ''), nullif($9, ''), $10, nullif($11, ''), $12, $13::jsonb
)
ON CONFLICT (tenant_id, exception_type, source_key, grain_key) WHERE status = 'open'
DO UPDATE SET blocker_reason = EXCLUDED.blocker_reason,
              evidence_json = EXCLUDED.evidence_json,
              updated_at = now()`,
		tenantID, snapshotID, ex.ExceptionType, ex.SourceKey, ex.GrainKey, ptrValue(ex.ParkID), ptrValue(ex.ShedID),
		ptrValue(ex.BreedKey), ptrValue(ex.StageTag), defaultString(ex.Severity, "blocking"), ptrValue(ex.OwnerRef),
		ex.BlockerReason, jsonObject(ex.EvidenceJSON))
	if err != nil {
		return fmt.Errorf("counts: insert projection exception: %w", err)
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
		{"CSG3", "Realized vs one-day projection snapshot not proven."},
		{"CSG4", "Ration-context resolver not proven."},
		{"CSG5", "Idempotency/replay not proven."},
		{"CSG6", "Unreported-shifting and count-mismatch detection not proven."},
		{"CSG7", "Exception workflow not proven."},
		{"CSG8", "Observability not proven."},
		{"CSG9", "Bounded read model/query plan not proven."},
		{"CSG10", "Source parity and seeded E2E not proven."},
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

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
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
