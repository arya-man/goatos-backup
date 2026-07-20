// Package postgres implements Counts/Shifting persistence over Postgres.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

const defaultQueryTimeout = 3 * time.Second

const (
	countBaseAnchorRecordedEventType    = domain.EventBaseCountAnchorRecorded
	shiftingEventRecordedEventType      = domain.EventShiftingEventRecorded
	projectionExceptionOpenedEventType  = domain.EventProjectionExceptionOpened
	projectionExceptionUpdatedEventType = domain.EventProjectionExceptionUpdated
	projectionExceptionClosedEventType  = domain.EventProjectionExceptionClosed
	countsEventSchemaVersion            = "1.0.0"
	countsEventSchemaRef                = "contracts/jsonschema/domain-event-envelope.schema.json"
	countsEventTopic                    = "counts.events"
)

type readinessSubgateUpdate struct {
	ID                string
	Status            string
	Owner             string
	EvidenceRef       string
	BlockerReason     string
	ImplementationRef string
}

type readinessSubgateExec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type readinessSubgateReadWriter interface {
	readinessSubgateExec
	QueryRow(context.Context, string, ...any) pgx.Row
}

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
		if _, err := createBaseCountMismatchException(ctx, tx, in.TenantID, id, in); err != nil {
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
		if err := upsertBaseCountReadiness(ctx, tx, in.TenantID, id); err != nil {
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
	if err := upsertBaseCountReadiness(ctx, r.pool, in.TenantID, id); err != nil {
		return "", false, err
	}
	if err := upsertReplayReadinessPending(ctx, r.pool, in.TenantID, "count_base_anchors:"+id); err != nil {
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
		if err := upsertShiftingReadiness(ctx, r.pool, in.TenantID, id, len(in.Impacts) > 0); err != nil {
			return "", false, err
		}
		if err := upsertReplayReadinessPending(ctx, r.pool, in.TenantID, "shifting_events:"+id); err != nil {
			return "", false, err
		}
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
	if err := upsertShiftingReadiness(ctx, tx, in.TenantID, id, len(in.Impacts) > 0); err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	return id, false, nil
}

func (r *Repository) ScanCountMismatches(ctx context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	result, err := r.startCountMismatchScanRun(ctx, req)
	if err != nil {
		return domain.CountMismatchScanResult{}, err
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return r.finishCountMismatchScanRun(ctx, result, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	anchors, hasMore, err := scanCountMismatchAnchors(ctx, tx, req)
	if err != nil {
		return r.finishCountMismatchScanRun(ctx, result, err)
	}
	for _, anchor := range anchors {
		result.ScannedAnchorCount++
		wrote, err := createBaseCountMismatchException(ctx, tx, req.TenantID, anchor.ID, anchor.BaseCountAnchor)
		if err != nil {
			return r.finishCountMismatchScanRun(ctx, result, err)
		}
		if wrote {
			result.ExceptionWriteCount++
			result.InvestigatingAnchorCount++
		}
	}
	if hasMore && len(anchors) > 0 {
		last := anchors[len(anchors)-1]
		result.NextCursor = &domain.CountMismatchScanCursor{
			CountedAt:         last.CountedAt,
			BaseCountAnchorID: last.ID,
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return r.finishCountMismatchScanRun(ctx, result, err)
	}
	return r.finishCountMismatchScanRun(ctx, result, nil)
}

func (r *Repository) BeginProjectionRecomputeRun(ctx context.Context, req domain.ProjectionRecomputeRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var runID string
	err := r.pool.QueryRow(ctx, `
INSERT INTO count_projection_recompute_runs (
  tenant_id, park_id, horizon, target_date, as_of, status,
  source_contract_version, generated_by, trace_id
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, 'running',
  $6, $7, nullif($8, '')
)
RETURNING count_projection_recompute_run_id::text`,
		req.TenantID, req.ParkID, req.Horizon, dateOnly(req.TargetDate), req.AsOf,
		req.SourceContractVersion, req.GeneratedBy, ptrValue(req.TraceID)).Scan(&runID)
	if err != nil {
		return "", fmt.Errorf("counts: begin projection recompute run: %w", err)
	}
	return runID, nil
}

func (r *Repository) FinishProjectionRecomputeRun(ctx context.Context, runID string, result domain.ProjectionRecomputeResult, recomputeErr error) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	status := "completed"
	projectionStatus := result.ProjectionStatus
	lastError := ""
	if recomputeErr != nil {
		status = "failed"
		projectionStatus = "failed"
		lastError = recomputeErr.Error()
	}
	var tenantID string
	if err := r.pool.QueryRow(ctx, `
UPDATE count_projection_recompute_runs
SET status = $2,
    projection_status = nullif($3, ''),
    snapshot_id = nullif($4, '')::uuid,
    row_count = $5,
    exception_count = $6,
    last_error = nullif($7, ''),
    completed_at = now(),
    updated_at = now()
WHERE count_projection_recompute_run_id = $1::uuid
RETURNING tenant_id::text`,
		runID, status, projectionStatus, result.SnapshotID, result.RowCount,
		result.ExceptionCount, lastError).Scan(&tenantID); err != nil {
		return fmt.Errorf("counts: finish projection recompute run: %w", err)
	}
	csg10Status := "pending"
	csg10Blocker := "Projection recompute run metrics exist; source parity, full observability, query-plan proof, and seeded local E2E evidence remain before CSG10 can turn ready."
	if recomputeErr != nil {
		csg10Status = "blocked"
		csg10Blocker = "Projection recompute failed; repair the worker error before CSG10 can progress: " + recomputeErr.Error()
	}
	if _, err := r.pool.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-projection-recompute;backend/internal/counts/app/service.go;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
  now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`,
		tenantID, csg10Status, "count_projection_recompute_runs:"+runID, csg10Blocker); err != nil {
		return fmt.Errorf("counts: upsert CSG10 projection recompute readiness: %w", err)
	}
	return nil
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
		if err := upsertProjectionSnapshotReadiness(ctx, r.pool, in.TenantID, id); err != nil {
			return "", err
		}
		if err := upsertAliasReadinessFromSnapshot(ctx, r.pool, in.TenantID, id, in.Rows, in.Exceptions); err != nil {
			return "", err
		}
		if err := upsertReplayReadinessPending(ctx, r.pool, in.TenantID, "count_projection_snapshots:"+id); err != nil {
			return "", err
		}
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
	if err := upsertProjectionSnapshotReadiness(ctx, tx, in.TenantID, id); err != nil {
		return "", err
	}
	if err := upsertAliasReadinessFromSnapshot(ctx, tx, in.TenantID, id, in.Rows, in.Exceptions); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func upsertBaseCountReadiness(ctx context.Context, q readinessSubgateExec, tenantID, anchorID string) error {
	if err := upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG1",
		Status:        "ready",
		Owner:         "Counts/Shifting",
		EvidenceRef:   "count_base_anchors:" + anchorID,
		BlockerReason: "No blocker: adopted Base Count anchor is durable at tenant/park/shed/breed grain with source hash provenance.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:RecordBaseCountAnchor;" +
			"backend/internal/counts/app/service.go",
	}); err != nil {
		return err
	}
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG5",
		Status:        "ready",
		Owner:         "Counts/Shifting",
		EvidenceRef:   "count_base_anchors:" + anchorID,
		BlockerReason: "No blocker: Base Count anchor is immediately adopted and unexpected deltas create owner-visible projection exception work.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:RecordBaseCountAnchor;" +
			"backend/internal/counts/adapters/postgres/repository.go:createBaseCountMismatchException",
	})
}

func upsertShiftingReadiness(ctx context.Context, q readinessSubgateExec, tenantID, shiftingEventID string, hasImpacts bool) error {
	if err := upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG2",
		Status:        "ready",
		Owner:         "Counts/Shifting",
		EvidenceRef:   "shifting_events:" + shiftingEventID,
		BlockerReason: "No blocker: append-only ShiftingEvent ledger write is durable with logical-key conflict protection and projection invalidation.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:RecordShiftingEvent;" +
			"backend/migrations/postgres/000001_goatos_clean_slate_baseline.sql",
	}); err != nil {
		return err
	}
	if !hasImpacts {
		return nil
	}
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG3",
		Status:        "ready",
		Owner:         "Counts/Shifting + Feed Direction",
		EvidenceRef:   "shifting_event_impacts:" + shiftingEventID,
		BlockerReason: "No blocker: ShiftingEvent impacts persist structured shed/breed/stage/sex/pregnancy/warm-up counts for projection.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:insertShiftingImpact;" +
			"docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md",
	})
}

func upsertProjectionSnapshotReadiness(ctx context.Context, q readinessSubgateReadWriter, tenantID, snapshotID string) error {
	evidenceRef := "count_projection_snapshots:" + snapshotID
	if err := upsertProjectionHorizonReadiness(ctx, q, tenantID, evidenceRef); err != nil {
		return err
	}
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG9",
		Status:        "ready",
		Owner:         "Counts/Shifting + Feed Direction",
		EvidenceRef:   evidenceRef,
		BlockerReason: "No blocker: bounded immutable projection snapshot rows/exceptions are available through the Counts projection provider.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:CreateProjectionSnapshot;" +
			"backend/internal/counts/adapters/postgres/repository.go:ProjectedCountFor;" +
			"backend/internal/counts/adapters/postgres/repository.go:CountAsOf",
	})
}

func upsertReplayReadinessPending(ctx context.Context, q readinessSubgateExec, tenantID, evidenceRef string) error {
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG8",
		Status:        "pending",
		Owner:         "Counts/Shifting + Feed Direction",
		EvidenceRef:   evidenceRef,
		BlockerReason: "Replay-safe canonical write evidence exists; full typed source replay, projection recompute replay, source parity, and seeded local E2E remain before CSG8 can turn ready.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go;" +
			"backend/cmd/counts-source-import;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md",
	})
}

func upsertAliasReadinessFromSnapshot(ctx context.Context, q readinessSubgateExec, tenantID, snapshotID string, rows []domain.ProjectionRow, exceptions []domain.ProjectionException) error {
	evidenceRef := "count_projection_snapshots:" + snapshotID
	for _, exception := range exceptions {
		if exception.ExceptionType == "alias_conflict" {
			return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
				ID:            "CSG7",
				Status:        "blocked",
				Owner:         "Counts/Shifting + Feed Direction",
				EvidenceRef:   evidenceRef,
				BlockerReason: "Projection snapshot contains alias_conflict work; review and approve breed/stage/tag aliases before Feed consumes this projection.",
				ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:resolveProjectionAliases;" +
					"backend/internal/counts/app/service.go:buildProjectionSnapshot;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md",
			})
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:            "CSG7",
		Status:        "pending",
		Owner:         "Counts/Shifting + Feed Direction",
		EvidenceRef:   evidenceRef,
		BlockerReason: "Latest non-empty projection has no alias_conflict exceptions; full source workbook parity, Sheds DB profile-tag coverage, and owner-approved alias review remain before CSG7 can turn ready.",
		ImplementationRef: "backend/internal/counts/adapters/postgres/repository.go:resolveProjectionAliases;" +
			"backend/internal/counts/app/service.go:buildProjectionSnapshot;context/source-findings/sheds-db-source-findings.md",
	})
}

func upsertProjectionHorizonReadiness(ctx context.Context, q readinessSubgateReadWriter, tenantID, evidenceRef string) error {
	var horizonCount int
	if err := q.QueryRow(ctx, `
SELECT count(DISTINCT horizon)
FROM count_projection_snapshots
WHERE tenant_id = $1::uuid
  AND horizon IN ('count_as_of', 'feed_target_date')`, tenantID).Scan(&horizonCount); err != nil {
		return fmt.Errorf("counts: count projection readiness horizons: %w", err)
	}
	status := "pending"
	blocker := "One projection horizon has durable snapshot evidence; run both count_as_of and feed_target_date horizons before CSG4 can turn ready."
	if horizonCount >= 2 {
		status = "ready"
		blocker = "No blocker: realized count_as_of and feed_target_date projection horizons both have durable snapshot evidence."
	}
	return upsertReadinessSubgate(ctx, q, tenantID, readinessSubgateUpdate{
		ID:                "CSG4",
		Status:            status,
		Owner:             "Counts/Shifting + Feed Direction",
		EvidenceRef:       evidenceRef,
		BlockerReason:     blocker,
		ImplementationRef: "backend/internal/counts/app/service.go:RecomputeProjectionSnapshotWithResult;backend/internal/counts/adapters/postgres/repository.go:CreateProjectionSnapshot",
	})
}

func upsertReadinessSubgate(ctx context.Context, q readinessSubgateExec, tenantID string, update readinessSubgateUpdate) error {
	if _, err := q.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6, nullif($7, ''), now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`,
		tenantID, update.ID, update.Status, update.Owner, update.EvidenceRef,
		update.BlockerReason, update.ImplementationRef); err != nil {
		return fmt.Errorf("counts: upsert %s readiness: %w", update.ID, err)
	}
	return nil
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
		var sourcePark, sourceShed, destinationPark, breedID, stage, age, sex, rationRef, blocker string
		if err := rows.Scan(&movement.ShiftingEventID, &movement.LogicalShiftingEventKey,
			&movement.SourceSystem, &sourcePark, &sourceShed, &destinationPark, &movement.DestinationShedID, &movement.EffectiveAt,
			&movement.GrainKey, &breedID, &movement.BreedKey, &movement.BreedLabel,
			&stage, &age, &sex, &movement.HeadCount, &movement.PregnantCount,
			&movement.LactatingCount, &movement.WarmupCount, &movement.RationContextResolutionState,
			&rationRef, &blocker); err != nil {
			return nil, fmt.Errorf("counts: scan projection movement: %w", err)
		}
		movement.SourceParkID = ptrIfNotEmpty(sourcePark)
		movement.SourceShedID = ptrIfNotEmpty(sourceShed)
		movement.DestinationParkID = destinationPark
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
	if req.Horizon == "count_as_of" {
		return movementWindowUnion(
			`
  AND se.destination_park_id = $2::uuid
  AND se.event_status = 'applied'
  AND se.effective_at <= $3`,
			`
  AND se.source_park_id = $2::uuid
  AND se.source_shed_id IS NOT NULL
  AND se.destination_park_id <> $2::uuid
  AND se.event_status = 'applied'
  AND se.effective_at <= $3`,
		), []any{req.TenantID, req.ParkID, req.AsOf}
	}
	start := dateOnly(req.TargetDate)
	end := start.AddDate(0, 0, 1)
	return movementWindowUnion(
		`
  AND se.destination_park_id = $2::uuid
  AND se.authorization_state = 'authorized'
  AND se.event_status IN ('authorized', 'applied')
  AND se.effective_at >= $3
  AND se.effective_at < $4`,
		`
  AND se.source_park_id = $2::uuid
  AND se.source_shed_id IS NOT NULL
  AND se.destination_park_id <> $2::uuid
  AND se.authorization_state = 'authorized'
  AND se.event_status IN ('authorized', 'applied')
  AND se.effective_at >= $3
  AND se.effective_at < $4`,
	), []any{req.TenantID, req.ParkID, start, end}
}

func movementWindowUnion(destinationFilter, sourceFilter string) string {
	return `
WITH destination_events AS MATERIALIZED (
` + movementEventBranch(destinationFilter) + `
),
source_events AS MATERIALIZED (
` + movementEventBranch(sourceFilter) + `
),
projection_events AS (
  SELECT * FROM destination_events
UNION ALL
  SELECT * FROM source_events
)
SELECT se.shifting_event_id::text, se.logical_shifting_event_key,
       se.source_system,
       COALESCE(se.source_park_id::text, ''), COALESCE(se.source_shed_id::text, ''),
       se.destination_park_id::text, se.destination_shed_id::text,
       se.effective_at, sei.grain_key, COALESCE(sei.breed_id::text, ''),
       sei.breed_key, sei.breed_label, COALESCE(sei.stage_tag, ''),
       COALESCE(sei.age_class, ''), COALESCE(sei.sex, ''),
       sei.head_count, sei.pregnant_count, sei.lactating_count, sei.warmup_count,
       sei.ration_context_resolution_state, COALESCE(sei.ration_context_ref, ''),
       COALESCE(sei.blocker_reason, '')
FROM projection_events se
JOIN LATERAL (
  SELECT shifting_event_impact_id, grain_key, breed_id, breed_key, breed_label,
         stage_tag, age_class, sex, head_count, pregnant_count, lactating_count,
         warmup_count, ration_context_resolution_state, ration_context_ref,
         blocker_reason
  FROM shifting_event_impacts sei
  WHERE sei.tenant_id = se.tenant_id
    AND sei.shifting_event_id = se.shifting_event_id
  ORDER BY sei.grain_key
) sei ON true
ORDER BY se.effective_at, se.shifting_event_id, sei.grain_key`
}

func movementEventBranch(filter string) string {
	return `
SELECT se.tenant_id, se.shifting_event_id, se.logical_shifting_event_key,
       se.source_system, se.source_park_id, se.source_shed_id,
       se.destination_park_id, se.destination_shed_id, se.effective_at
FROM shifting_events se
WHERE se.tenant_id = $1::uuid` + filter
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

func (r *Repository) ListProjectionExceptions(ctx context.Context, req domain.ProjectionExceptionQuery) (domain.ProjectionExceptionList, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	fetchLimit := req.Limit + 1
	cursorUpdated := pgtype.Timestamptz{}
	var cursorID any
	if req.Cursor != nil {
		cursorUpdated = pgtype.Timestamptz{Time: req.Cursor.UpdatedAt, Valid: true}
		cursorID = req.Cursor.ProjectionExceptionID
	}
	rows, err := r.pool.Query(ctx, `
SELECT count_projection_exception_id::text,
       COALESCE(count_projection_snapshot_id::text, ''),
       exception_type,
       source_key,
       grain_key,
       COALESCE(park_id::text, ''),
       COALESCE(shed_id::text, ''),
       COALESCE(breed_key, ''),
       COALESCE(stage_tag, ''),
       severity,
       status,
       COALESCE(owner_ref, ''),
       work_type,
       work_state,
       due_at,
       next_action,
       evidence_link,
       blocker_reason,
       evidence_json,
       COALESCE(resolution_id::text, ''),
       COALESCE(resolved_by_ref, ''),
       COALESCE(resolution_reason, ''),
       COALESCE(resolution_ref, ''),
       resolved_at,
       created_at,
       updated_at
FROM count_projection_exceptions
WHERE tenant_id = $1::uuid
  AND status = $2
  AND (nullif($3::text, '')::uuid IS NULL OR park_id = nullif($3::text, '')::uuid)
  AND (nullif($4::text, '')::uuid IS NULL OR shed_id = nullif($4::text, '')::uuid)
  AND (nullif($5::text, '') IS NULL OR exception_type = nullif($5::text, ''))
  AND (nullif($6::text, '') IS NULL OR severity = nullif($6::text, ''))
  AND (nullif($7::text, '') IS NULL OR owner_ref = nullif($7::text, ''))
  AND (nullif($8::text, '') IS NULL OR work_state = nullif($8::text, ''))
  AND ($9::timestamptz IS NULL OR (updated_at, count_projection_exception_id) < ($9::timestamptz, $10::uuid))
ORDER BY updated_at DESC, count_projection_exception_id DESC
LIMIT $11`,
		req.TenantID, req.Status, ptrValue(req.ParkID), ptrValue(req.ShedID),
		ptrValue(req.ExceptionType), ptrValue(req.Severity), ptrValue(req.OwnerRef),
		ptrValue(req.WorkState), cursorUpdated, cursorID, fetchLimit)
	if err != nil {
		return domain.ProjectionExceptionList{}, fmt.Errorf("counts: list projection exceptions: %w", err)
	}
	defer rows.Close()

	out := []domain.ProjectionException{}
	for rows.Next() {
		ex, err := scanProjectionException(rows)
		if err != nil {
			return domain.ProjectionExceptionList{}, err
		}
		out = append(out, ex)
	}
	if err := rows.Err(); err != nil {
		return domain.ProjectionExceptionList{}, err
	}
	var next *string
	if int32(len(out)) > req.Limit {
		out = out[:req.Limit]
		last := out[len(out)-1]
		cursor, err := domain.EncodeProjectionExceptionCursor(domain.ProjectionExceptionCursor{
			UpdatedAt:             last.UpdatedAt,
			ProjectionExceptionID: last.ProjectionExceptionID,
		})
		if err != nil {
			return domain.ProjectionExceptionList{}, err
		}
		next = &cursor
	}
	return domain.ProjectionExceptionList{Items: out, NextCursor: next}, nil
}

func (r *Repository) ResolveProjectionException(ctx context.Context, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if out, found, err := maybeProjectionExceptionResolutionByIdempotency(ctx, tx, in); err != nil || found {
		return out, err
	}

	var currentStatus, sourceKey string
	if err := tx.QueryRow(ctx, `
SELECT status, source_key
FROM count_projection_exceptions
WHERE tenant_id = $1::uuid
  AND count_projection_exception_id = $2::uuid
FOR UPDATE`, in.TenantID, in.ProjectionExceptionID).Scan(&currentStatus, &sourceKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ProjectionExceptionResolution{}, ports.ErrProjectionExceptionNotFound
		}
		return domain.ProjectionExceptionResolution{}, fmt.Errorf("counts: lock projection exception: %w", err)
	}
	if currentStatus != "open" {
		return domain.ProjectionExceptionResolution{}, ports.ErrProjectionExceptionClosed
	}

	var resolutionID string
	if err := tx.QueryRow(ctx, `
INSERT INTO count_projection_exception_resolutions (
  tenant_id, count_projection_exception_id, action, resolved_by_ref, resolution_reason,
  resolution_ref, idempotency_key, request_fingerprint
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5,
  nullif($6, ''), $7, $8
)
RETURNING count_projection_exception_resolution_id::text`,
		in.TenantID, in.ProjectionExceptionID, in.Action, in.ResolvedByRef, in.ResolutionReason,
		ptrValue(in.ResolutionRef), in.IdempotencyKey, in.RequestFingerprint).Scan(&resolutionID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "count_projection_exception_resolutions_idempotency_unique" {
			if out, found, replayErr := maybeProjectionExceptionResolutionByIdempotency(ctx, tx, in); replayErr != nil || found {
				return out, replayErr
			}
			return domain.ProjectionExceptionResolution{}, ports.ErrIdempotencyConflict
		}
		return domain.ProjectionExceptionResolution{}, fmt.Errorf("counts: insert projection exception resolution: %w", err)
	}

	closedStatus := projectionExceptionClosedStatus(in.Action)
	tag, err := tx.Exec(ctx, `
UPDATE count_projection_exceptions
SET status = $3,
    work_state = $3,
    resolved_at = now(),
    resolution_id = $4::uuid,
    resolved_by_ref = $5,
    resolution_reason = $6,
    resolution_ref = nullif($7, ''),
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND count_projection_exception_id = $2::uuid
  AND status = 'open'`,
		in.TenantID, in.ProjectionExceptionID, closedStatus, resolutionID, in.ResolvedByRef,
		in.ResolutionReason, ptrValue(in.ResolutionRef))
	if err != nil {
		return domain.ProjectionExceptionResolution{}, fmt.Errorf("counts: close projection exception: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ProjectionExceptionResolution{}, ports.ErrProjectionExceptionClosed
	}
	if err := markBaseCountDiscrepancyResolved(ctx, tx, in.TenantID, sourceKey); err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	closedException, err := loadProjectionExceptionForOutbox(ctx, tx, in.TenantID, in.ProjectionExceptionID)
	if err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	if err := insertProjectionExceptionOutbox(ctx, tx, in.TenantID, closedException, projectionExceptionOutboxOptions{
		EventType:        projectionExceptionClosedEventType,
		IdempotencyKey:   projectionExceptionClosedEventType + ":" + resolutionID,
		ActorType:        "user",
		ActorRef:         in.ResolvedByRef,
		ResolutionID:     resolutionID,
		ResolutionAction: in.Action,
	}); err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	out, err := loadProjectionExceptionResolution(ctx, tx, in.TenantID, resolutionID)
	if err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ProjectionExceptionResolution{}, err
	}
	return out, nil
}

func (r *Repository) Readiness(ctx context.Context, tenantID string) (domain.Readiness, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := domain.Readiness{
		TenantID:          tenantID,
		Status:            domain.ReadinessBlocked,
		GenerationAllowed: false,
		// india-date-guard:ignore: owner=ravi issue=GH-india-date scope=readiness-checked-at-absolute-instant expiry=2026-12-31
		Subgates: defaultSubgates(time.Now().UTC()),
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
	if err := r.attachRecentReadinessEvidence(ctx, &out, byID); err != nil {
		return domain.Readiness{}, err
	}
	if err := r.applyCompositeReadinessGuards(ctx, &out, byID); err != nil {
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

func (r *Repository) attachRecentReadinessEvidence(ctx context.Context, readiness *domain.Readiness, byID map[string]int) error {
	const evidenceLimitPerSubgate = 5
	subgateIDs := make([]string, 0, len(readiness.Subgates))
	for _, subgate := range readiness.Subgates {
		if _, ok := byID[subgate.ID]; ok {
			subgateIDs = append(subgateIDs, subgate.ID)
		}
	}
	if len(subgateIDs) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, `
SELECT subgate_id, status, evidence_ref, blocker_reason, COALESCE(implementation_ref, ''), recorded_at
FROM unnest($2::text[]) WITH ORDINALITY AS wanted(subgate_id, subgate_order)
CROSS JOIN LATERAL (
  SELECT status, evidence_ref, blocker_reason, implementation_ref, recorded_at,
         counts_shifting_readiness_evidence_id
  FROM counts_shifting_readiness_evidence
  WHERE tenant_id = $1::uuid AND subgate_id = wanted.subgate_id
  ORDER BY recorded_at DESC, counts_shifting_readiness_evidence_id DESC
  LIMIT $3
) evidence
ORDER BY wanted.subgate_order, evidence.recorded_at DESC, evidence.counts_shifting_readiness_evidence_id DESC`,
		readiness.TenantID, subgateIDs, evidenceLimitPerSubgate)
	if err != nil {
		return fmt.Errorf("counts: readiness evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var subgateID, status, implementation string
		var evidence domain.ReadinessEvidence
		if err := rows.Scan(&subgateID, &status, &evidence.EvidenceRef, &evidence.BlockerReason, &implementation, &evidence.RecordedAt); err != nil {
			return fmt.Errorf("counts: scan readiness evidence: %w", err)
		}
		idx, ok := byID[subgateID]
		if !ok {
			continue
		}
		evidence.Status = domain.ReadinessStatus(status)
		evidence.ImplementationRef = implementation
		readiness.Subgates[idx].RecentEvidence = append(readiness.Subgates[idx].RecentEvidence, evidence)
	}
	return rows.Err()
}

func (r *Repository) applyCompositeReadinessGuards(ctx context.Context, readiness *domain.Readiness, byID map[string]int) error {
	if err := r.applyCSG7ReadinessGuard(ctx, readiness, byID); err != nil {
		return err
	}
	return nil
}

func (r *Repository) applyCSG7ReadinessGuard(ctx context.Context, readiness *domain.Readiness, byID map[string]int) error {
	idx, ok := byID["CSG7"]
	if !ok {
		return nil
	}
	subgate := &readiness.Subgates[idx]
	var openAliasConflicts int
	if err := r.pool.QueryRow(ctx, `
SELECT count(*)
FROM count_projection_exceptions
WHERE tenant_id = $1::uuid
  AND exception_type = 'alias_conflict'
  AND status = 'open'`, readiness.TenantID).Scan(&openAliasConflicts); err != nil {
		return fmt.Errorf("counts: CSG7 alias-conflict readiness guard: %w", err)
	}
	if openAliasConflicts > 0 {
		subgate.Status = domain.ReadinessBlocked
		subgate.EvidenceRef = "count_projection_exceptions:alias_conflict"
		subgate.BlockerReason = fmt.Sprintf("%d open alias_conflict projection exception(s) remain; approve or resolve breed/stage/tag aliases before CSG7 can become pending.", openAliasConflicts)
		subgate.ImplementationRef = "backend/internal/counts/adapters/postgres/repository.go:applyCSG7ReadinessGuard;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md"
		return nil
	}
	missing, err := r.missingCSG7EvidenceFamilies(ctx, readiness.TenantID)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		subgate.Status = domain.ReadinessBlocked
		subgate.EvidenceRef = "counts_shifting_readiness_evidence:CSG7"
		subgate.BlockerReason = "CSG7 is missing required evidence families before it can become pending: " + strings.Join(missing, ", ")
		subgate.ImplementationRef = "backend/cmd/counts-alias-coverage-check;backend/cmd/location-profile-coverage-check;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md"
		return nil
	}
	if subgate.Status != domain.ReadinessReady {
		subgate.Status = domain.ReadinessPending
		subgate.EvidenceRef = "counts_shifting_readiness_evidence:CSG7"
		subgate.BlockerReason = "Required CSG7 alias and Sheds DB profile coverage evidence exists; owner-approved review, full source workbook parity, and seeded local E2E remain before CSG7 can turn ready."
		subgate.ImplementationRef = "backend/cmd/counts-alias-coverage-check;backend/cmd/location-profile-coverage-check;context/source-findings/sheds-db-source-findings.md"
	}
	return nil
}

func (r *Repository) missingCSG7EvidenceFamilies(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `
SELECT requirement.label, COALESCE(latest.status, '') AS latest_status
FROM (
  VALUES
    ('counts-alias-coverage-check:'::text, 'counts alias coverage'::text),
    ('location-profile-coverage-check:'::text, 'Sheds DB location-profile coverage'::text)
) AS requirement(prefix, label)
LEFT JOIN LATERAL (
  SELECT status
  FROM counts_shifting_readiness_evidence
  WHERE tenant_id = $1::uuid
    AND subgate_id = 'CSG7'
    AND evidence_ref LIKE requirement.prefix || '%'
  ORDER BY recorded_at DESC, counts_shifting_readiness_evidence_id DESC
  LIMIT 1
) latest ON true
ORDER BY requirement.label`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("counts: CSG7 evidence family readiness guard: %w", err)
	}
	defer rows.Close()
	missing := []string{}
	for rows.Next() {
		var label, status string
		if err := rows.Scan(&label, &status); err != nil {
			return nil, fmt.Errorf("counts: scan CSG7 evidence family readiness guard: %w", err)
		}
		if status != string(domain.ReadinessPending) && status != string(domain.ReadinessReady) {
			missing = append(missing, label)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("counts: CSG7 evidence family readiness guard rows: %w", err)
	}
	return missing, nil
}

type projectionExceptionResolutionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func maybeProjectionExceptionResolutionByIdempotency(ctx context.Context, tx pgx.Tx, in domain.ProjectionExceptionResolutionRequest) (domain.ProjectionExceptionResolution, bool, error) {
	var resolutionID, existingFP string
	err := tx.QueryRow(ctx, `
SELECT count_projection_exception_resolution_id::text, request_fingerprint
FROM count_projection_exception_resolutions
WHERE tenant_id = $1::uuid AND idempotency_key = $2`, in.TenantID, in.IdempotencyKey).Scan(&resolutionID, &existingFP)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ProjectionExceptionResolution{}, false, nil
	}
	if err != nil {
		return domain.ProjectionExceptionResolution{}, false, fmt.Errorf("counts: load projection exception resolution idempotency: %w", err)
	}
	if existingFP != in.RequestFingerprint {
		return domain.ProjectionExceptionResolution{}, false, ports.ErrIdempotencyConflict
	}
	out, err := loadProjectionExceptionResolution(ctx, tx, in.TenantID, resolutionID)
	if err != nil {
		return domain.ProjectionExceptionResolution{}, false, err
	}
	out.Replayed = true
	return out, true, nil
}

func loadProjectionExceptionResolution(ctx context.Context, q projectionExceptionResolutionQuerier, tenantID, resolutionID string) (domain.ProjectionExceptionResolution, error) {
	var out domain.ProjectionExceptionResolution
	var ref string
	if err := q.QueryRow(ctx, `
SELECT r.count_projection_exception_resolution_id::text,
       r.count_projection_exception_id::text,
       r.action,
       e.status,
       e.work_state,
       r.resolved_by_ref,
       r.resolution_reason,
       COALESCE(r.resolution_ref, ''),
       e.resolved_at
FROM count_projection_exception_resolutions r
JOIN count_projection_exceptions e
  ON e.count_projection_exception_id = r.count_projection_exception_id
WHERE r.tenant_id = $1::uuid
  AND r.count_projection_exception_resolution_id = $2::uuid`, tenantID, resolutionID).Scan(
		&out.ProjectionExceptionResolutionID,
		&out.ProjectionExceptionID,
		&out.Action,
		&out.Status,
		&out.WorkState,
		&out.ResolvedByRef,
		&out.ResolutionReason,
		&ref,
		&out.ResolvedAt,
	); err != nil {
		return domain.ProjectionExceptionResolution{}, fmt.Errorf("counts: load projection exception resolution: %w", err)
	}
	out.ResolutionRef = ptrIfNotEmpty(ref)
	return out, nil
}

func projectionExceptionClosedStatus(action string) string {
	if action == "dismiss" {
		return "dismissed"
	}
	return "resolved"
}

func markBaseCountDiscrepancyResolved(ctx context.Context, tx pgx.Tx, tenantID, sourceKey string) error {
	const prefix = "base_count_anchor:"
	if !strings.HasPrefix(sourceKey, prefix) {
		return nil
	}
	anchorID := strings.TrimSpace(strings.TrimPrefix(sourceKey, prefix))
	if anchorID == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `
UPDATE count_base_anchors
SET discrepancy_state = 'resolved',
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND base_count_anchor_id = $2::uuid
  AND discrepancy_state = 'investigating'`, tenantID, anchorID); err != nil {
		return fmt.Errorf("counts: resolve base count discrepancy: %w", err)
	}
	return nil
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

type countMismatchScanAnchor struct {
	ID string
	domain.BaseCountAnchor
}

func (r *Repository) startCountMismatchScanRun(ctx context.Context, req domain.CountMismatchScanRequest) (domain.CountMismatchScanResult, error) {
	var result domain.CountMismatchScanResult
	err := r.pool.QueryRow(ctx, `
INSERT INTO count_mismatch_scan_runs (
  tenant_id, park_id, shed_id, counted_after, counted_before, cursor_counted_at, cursor_anchor_id
) VALUES (
  $1::uuid, nullif($2::text, '')::uuid, nullif($3::text, '')::uuid,
  $4::timestamptz, $5::timestamptz, $6::timestamptz, nullif($7::text, '')::uuid
)
RETURNING count_mismatch_scan_run_id::text, tenant_id::text, status, started_at`,
		req.TenantID, ptrValue(req.ParkID), ptrValue(req.ShedID), nullableTime(req.CountedAfter),
		req.CountedBefore, nullableTime(req.CursorCountedAt), ptrValue(req.CursorAnchorID)).
		Scan(&result.RunID, &result.TenantID, &result.Status, &result.StartedAt)
	if err != nil {
		return domain.CountMismatchScanResult{}, fmt.Errorf("counts: start mismatch scan run: %w", err)
	}
	return result, nil
}

func (r *Repository) finishCountMismatchScanRun(ctx context.Context, result domain.CountMismatchScanResult, scanErr error) (domain.CountMismatchScanResult, error) {
	status := "completed"
	lastError := ""
	if scanErr != nil {
		status = "failed"
		lastError = scanErr.Error()
	}
	var completedAt pgtype.Timestamptz
	var nextCursorAt pgtype.Timestamptz
	var nextCursorID string
	var errorOut string
	if result.NextCursor != nil {
		nextCursorAt = pgtype.Timestamptz{Time: result.NextCursor.CountedAt, Valid: true}
		nextCursorID = result.NextCursor.BaseCountAnchorID
	}
	err := r.pool.QueryRow(ctx, `
UPDATE count_mismatch_scan_runs
SET status = $3,
    completed_at = now(),
    scanned_anchor_count = $4,
    exception_write_count = $5,
    investigating_anchor_count = $6,
    next_cursor_counted_at = $7::timestamptz,
    next_cursor_anchor_id = nullif($8::text, '')::uuid,
    last_error = nullif($9, ''),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND count_mismatch_scan_run_id = $2::uuid
RETURNING status, completed_at, COALESCE(last_error, '')`,
		result.TenantID, result.RunID, status, result.ScannedAnchorCount, result.ExceptionWriteCount,
		result.InvestigatingAnchorCount, nextCursorAt, nextCursorID, lastError).
		Scan(&result.Status, &completedAt, &errorOut)
	if err != nil {
		if scanErr != nil {
			return result, fmt.Errorf("%w; counts: finish mismatch scan run: %v", scanErr, err)
		}
		return result, fmt.Errorf("counts: finish mismatch scan run: %w", err)
	}
	if completedAt.Valid {
		completed := completedAt.Time
		result.CompletedAt = &completed
	}
	result.LastError = ptrIfNotEmpty(errorOut)
	if err := r.upsertCountMismatchScanReadiness(ctx, result); err != nil {
		if scanErr != nil {
			return result, fmt.Errorf("%w; counts: update mismatch scan readiness: %v", scanErr, err)
		}
		return result, err
	}
	return result, scanErr
}

func (r *Repository) upsertCountMismatchScanReadiness(ctx context.Context, result domain.CountMismatchScanResult) error {
	csg6Status := "ready"
	csg6Blocker := "No blocker: live Base Count mismatch detection and bounded stale/imported scan have durable run evidence."
	if result.Status == "failed" {
		csg6Status = "blocked"
		csg6Blocker = "Count mismatch scan failed: " + ptrValue(result.LastError)
	} else if result.NextCursor != nil {
		csg6Status = "pending"
		csg6Blocker = "Count mismatch scan page completed, but a next cursor remains; continue scanning stale imported/historical anchors."
	}
	csg6Evidence := "count_mismatch_scan_runs:" + result.RunID
	if _, err := r.pool.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG6', $2, 'Counts/Shifting',
  $3, $4, 'backend/cmd/counts-mismatch-scan;backend/internal/counts/adapters/postgres/repository.go',
  now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`, result.TenantID, csg6Status, csg6Evidence, csg6Blocker); err != nil {
		return fmt.Errorf("counts: upsert CSG6 mismatch scan readiness: %w", err)
	}
	csg10Status := "pending"
	csg10Blocker := "Mismatch scan run metrics exist; broader base-count import latency, projection latency, query-plan proof, source parity, and seeded E2E evidence remain."
	if result.Status == "failed" {
		csg10Status = "blocked"
		csg10Blocker = "Mismatch scan observability recorded a failed run; repair the worker error before CSG10 can progress."
	}
	if _, err := r.pool.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-mismatch-scan;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
  now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`, result.TenantID, csg10Status, csg6Evidence, csg10Blocker); err != nil {
		return fmt.Errorf("counts: upsert CSG10 mismatch scan readiness: %w", err)
	}
	return nil
}

func scanCountMismatchAnchors(ctx context.Context, tx pgx.Tx, req domain.CountMismatchScanRequest) ([]countMismatchScanAnchor, bool, error) {
	rows, err := tx.Query(ctx, `
SELECT base_count_anchor_id::text,
       park_id::text,
       shed_id::text,
       COALESCE(breed_id::text, ''),
       breed_key,
       breed_label,
       counted_at,
       head_count,
       source_system,
       source_ref,
       source_hash,
       discrepancy_state,
       idempotency_key,
       request_fingerprint,
       COALESCE(recorded_by::text, '')
FROM count_base_anchors
WHERE tenant_id = $1::uuid
  AND anchor_state = 'adopted'
  AND discrepancy_state <> 'resolved'
  AND counted_at <= $2
  AND (nullif($3::text, '')::uuid IS NULL OR park_id = nullif($3::text, '')::uuid)
  AND (nullif($4::text, '')::uuid IS NULL OR shed_id = nullif($4::text, '')::uuid)
  AND ($5::timestamptz IS NULL OR counted_at > $5::timestamptz)
  AND (
    $6::timestamptz IS NULL
    OR nullif($7::text, '')::uuid IS NULL
    OR (counted_at, base_count_anchor_id) > ($6::timestamptz, nullif($7::text, '')::uuid)
  )
ORDER BY counted_at ASC, base_count_anchor_id ASC
LIMIT $8
FOR UPDATE SKIP LOCKED`,
		req.TenantID, req.CountedBefore, ptrValue(req.ParkID), ptrValue(req.ShedID),
		nullableTime(req.CountedAfter), nullableTime(req.CursorCountedAt), ptrValue(req.CursorAnchorID),
		req.Limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("counts: query count mismatch scan anchors: %w", err)
	}
	defer rows.Close()
	out := []countMismatchScanAnchor{}
	for rows.Next() {
		var anchor countMismatchScanAnchor
		var breedID, recordedBy string
		if err := rows.Scan(&anchor.ID, &anchor.ParkID, &anchor.ShedID, &breedID, &anchor.BreedKey,
			&anchor.BreedLabel, &anchor.CountedAt, &anchor.HeadCount, &anchor.SourceSystem,
			&anchor.SourceRef, &anchor.SourceHash, &anchor.DiscrepancyState, &anchor.IdempotencyKey,
			&anchor.RequestFingerprint, &recordedBy); err != nil {
			return nil, false, fmt.Errorf("counts: scan count mismatch anchor: %w", err)
		}
		anchor.TenantID = req.TenantID
		anchor.BreedID = ptrIfNotEmpty(breedID)
		anchor.RecordedBy = ptrIfNotEmpty(recordedBy)
		out = append(out, anchor)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := int32(len(out)) > req.Limit
	if hasMore {
		out = out[:req.Limit]
	}
	return out, hasMore, nil
}

func createBaseCountMismatchException(ctx context.Context, tx pgx.Tx, tenantID, anchorID string, in domain.BaseCountAnchor) (bool, error) {
	previous, found, err := loadPreviousBaseCountAnchor(ctx, tx, tenantID, anchorID, in)
	if err != nil || !found {
		return false, err
	}
	netShift, err := appliedShiftNetForAnchorWindow(ctx, tx, tenantID, in, previous.CountedAt)
	if err != nil {
		return false, err
	}
	expected := previous.HeadCount + netShift
	if expected == in.HeadCount {
		return false, nil
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
		return false, fmt.Errorf("counts: build count mismatch evidence: %w", err)
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
		WorkState:     "blocked",
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
		return false, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE count_base_anchors
SET discrepancy_state = 'investigating', updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND base_count_anchor_id = $2::uuid`, tenantID, anchorID); err != nil {
		return false, fmt.Errorf("counts: mark base count discrepancy investigating: %w", err)
	}
	return true, nil
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
  AND park_id = $3::uuid
  AND target_date = $4
  AND (nullif($5::text, '')::uuid IS NULL OR shed_id = nullif($5::text, '')::uuid)
  AND (nullif($6::text, '') IS NULL OR lower(breed_key) = lower(nullif($6::text, '')))
  AND (nullif($7::text, '') IS NULL OR ration_context_resolution_state = nullif($7::text, ''))
  AND (nullif($8::text, '')::uuid IS NULL OR count_projection_snapshot_row_id > nullif($8::text, '')::uuid)
ORDER BY count_projection_snapshot_row_id
LIMIT $9`, req.TenantID, out.SnapshotID, req.ParkID, targetDate, ptrValue(req.ShedID), ptrValue(req.BreedKey),
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
	out.ShedBreedTotals = summarizeShedBreedTotals(out.Rows)
	if err := r.loadOpenProjectionExceptions(ctx, out.SnapshotID, &out); err != nil {
		return domain.CountProjection{}, err
	}
	return out, nil
}

func summarizeShedBreedTotals(rows []domain.ProjectionRow) []domain.ProjectionShedBreedTotal {
	byKey := map[string]*domain.ProjectionShedBreedTotal{}
	for _, row := range rows {
		key := row.ShedID + "\x00" + strings.ToLower(strings.TrimSpace(row.BreedKey))
		total, ok := byKey[key]
		if !ok {
			byKey[key] = &domain.ProjectionShedBreedTotal{
				ParkID: row.ParkID, ShedID: row.ShedID, BreedKey: row.BreedKey, BreedLabel: row.BreedLabel,
				RationContextResolutionState: defaultResolution(row.RationContextResolutionState),
			}
			total = byKey[key]
		}
		total.HeadCount += row.HeadCount
		total.PregnantCount += row.PregnantCount
		total.LactatingCount += row.LactatingCount
		total.WarmupCount += row.WarmupCount
		total.RationContextResolutionState = aggregateResolution(total.RationContextResolutionState, row.RationContextResolutionState)
	}
	out := make([]domain.ProjectionShedBreedTotal, 0, len(byKey))
	for _, total := range byKey {
		out = append(out, *total)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ShedID == out[j].ShedID {
			return out[i].BreedKey < out[j].BreedKey
		}
		return out[i].ShedID < out[j].ShedID
	})
	return out
}

func aggregateResolution(current, next string) string {
	current = defaultResolution(current)
	next = defaultResolution(next)
	if current == "blocked" || next == "blocked" {
		return "blocked"
	}
	if current == "unresolved" || next == "unresolved" {
		return "unresolved"
	}
	if current == "resolved" || next == "resolved" {
		return "resolved"
	}
	return "not_required"
}

func (r *Repository) loadOpenProjectionExceptions(ctx context.Context, snapshotID string, out *domain.CountProjection) error {
	rows, err := r.pool.Query(ctx, `
SELECT count_projection_exception_id::text, exception_type, source_key, grain_key,
       COALESCE(park_id::text, ''), COALESCE(shed_id::text, ''), COALESCE(breed_key, ''),
       COALESCE(stage_tag, ''), severity, status, COALESCE(owner_ref, ''),
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
			&park, &shed, &breed, &stage, &ex.Severity, &ex.Status, &owner, &ex.WorkType, &ex.WorkState, &ex.DueAt,
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

type projectionExceptionScanner interface {
	Scan(dest ...any) error
}

func scanProjectionException(scanner projectionExceptionScanner) (domain.ProjectionException, error) {
	var ex domain.ProjectionException
	var snapshotID, park, shed, breed, stage, owner, resolutionID, resolvedBy, reason, ref string
	var resolvedAt pgtype.Timestamptz
	if err := scanner.Scan(&ex.ProjectionExceptionID, &snapshotID, &ex.ExceptionType, &ex.SourceKey, &ex.GrainKey,
		&park, &shed, &breed, &stage, &ex.Severity, &ex.Status, &owner, &ex.WorkType, &ex.WorkState,
		&ex.DueAt, &ex.NextAction, &ex.EvidenceLink, &ex.BlockerReason, &ex.EvidenceJSON,
		&resolutionID, &resolvedBy, &reason, &ref, &resolvedAt, &ex.CreatedAt, &ex.UpdatedAt); err != nil {
		return domain.ProjectionException{}, fmt.Errorf("counts: scan projection exception: %w", err)
	}
	ex.ProjectionSnapshotID = ptrIfNotEmpty(snapshotID)
	ex.ParkID = ptrIfNotEmpty(park)
	ex.ShedID = ptrIfNotEmpty(shed)
	ex.BreedKey = ptrIfNotEmpty(breed)
	ex.StageTag = ptrIfNotEmpty(stage)
	ex.OwnerRef = ptrIfNotEmpty(owner)
	ex.ResolutionID = ptrIfNotEmpty(resolutionID)
	ex.ResolvedByRef = ptrIfNotEmpty(resolvedBy)
	ex.ResolutionReason = ptrIfNotEmpty(reason)
	ex.ResolutionRef = ptrIfNotEmpty(ref)
	if resolvedAt.Valid {
		t := resolvedAt.Time
		ex.ResolvedAt = &t
	}
	return ex, nil
}

func loadProjectionExceptionForOutbox(ctx context.Context, q projectionExceptionResolutionQuerier, tenantID, exceptionID string) (domain.ProjectionException, error) {
	ex, err := scanProjectionException(q.QueryRow(ctx, `
SELECT count_projection_exception_id::text,
       COALESCE(count_projection_snapshot_id::text, ''),
       exception_type,
       source_key,
       grain_key,
       COALESCE(park_id::text, ''),
       COALESCE(shed_id::text, ''),
       COALESCE(breed_key, ''),
       COALESCE(stage_tag, ''),
       severity,
       status,
       COALESCE(owner_ref, ''),
       work_type,
       work_state,
       due_at,
       next_action,
       evidence_link,
       blocker_reason,
       evidence_json,
       COALESCE(resolution_id::text, ''),
       COALESCE(resolved_by_ref, ''),
       COALESCE(resolution_reason, ''),
       COALESCE(resolution_ref, ''),
       resolved_at,
       created_at,
       updated_at
FROM count_projection_exceptions
WHERE tenant_id = $1::uuid
  AND count_projection_exception_id = $2::uuid`, tenantID, exceptionID))
	if err != nil {
		return domain.ProjectionException{}, err
	}
	return ex, nil
}

func insertProjectionException(ctx context.Context, tx pgx.Tx, tenantID, snapshotID string, ex domain.ProjectionException) error {
	var persisted domain.ProjectionException
	var inserted bool
	var snapshotOut, park, shed, breed, stage, owner string
	err := tx.QueryRow(ctx, `
INSERT INTO count_projection_exceptions (
  tenant_id, count_projection_snapshot_id, exception_type, source_key, grain_key,
  park_id, shed_id, breed_key, stage_tag, severity, owner_ref, work_type, work_state,
  due_at, next_action, evidence_link, blocker_reason, evidence_json
) VALUES (
  $1::uuid, nullif($2::text, '')::uuid, $3, $4, $5,
  nullif($6::text, '')::uuid, nullif($7::text, '')::uuid, nullif($8, ''), nullif($9, ''),
  $10, nullif($11, ''),
  COALESCE(NULLIF($12, ''), 'counts_projection_exception'),
  COALESCE(NULLIF($13, ''), 'blocked'),
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
              updated_at = now()
RETURNING count_projection_exception_id::text,
          COALESCE(count_projection_snapshot_id::text, ''),
          exception_type,
          source_key,
          grain_key,
          COALESCE(park_id::text, ''),
          COALESCE(shed_id::text, ''),
          COALESCE(breed_key, ''),
          COALESCE(stage_tag, ''),
          severity,
          status,
          COALESCE(owner_ref, ''),
          work_type,
          work_state,
          due_at,
          next_action,
          evidence_link,
          blocker_reason,
          evidence_json,
          created_at,
          updated_at,
          (xmax = 0)`,
		tenantID, snapshotID, ex.ExceptionType, ex.SourceKey, ex.GrainKey, ptrValue(ex.ParkID), ptrValue(ex.ShedID),
		ptrValue(ex.BreedKey), ptrValue(ex.StageTag), defaultString(ex.Severity, "blocking"), ptrValue(ex.OwnerRef),
		ex.WorkType, ex.WorkState, nullableZeroTime(ex.DueAt), ex.NextAction, ex.EvidenceLink, ex.BlockerReason, jsonObject(ex.EvidenceJSON)).
		Scan(&persisted.ProjectionExceptionID, &snapshotOut, &persisted.ExceptionType, &persisted.SourceKey,
			&persisted.GrainKey, &park, &shed, &breed, &stage, &persisted.Severity, &persisted.Status,
			&owner, &persisted.WorkType, &persisted.WorkState, &persisted.DueAt, &persisted.NextAction,
			&persisted.EvidenceLink, &persisted.BlockerReason, &persisted.EvidenceJSON, &persisted.CreatedAt,
			&persisted.UpdatedAt, &inserted)
	if err != nil {
		return fmt.Errorf("counts: insert projection exception: %w", err)
	}
	persisted.ProjectionSnapshotID = ptrIfNotEmpty(snapshotOut)
	persisted.ParkID = ptrIfNotEmpty(park)
	persisted.ShedID = ptrIfNotEmpty(shed)
	persisted.BreedKey = ptrIfNotEmpty(breed)
	persisted.StageTag = ptrIfNotEmpty(stage)
	persisted.OwnerRef = ptrIfNotEmpty(owner)
	eventType := projectionExceptionUpdatedEventType
	if inserted {
		eventType = projectionExceptionOpenedEventType
	}
	if err := insertProjectionExceptionOutbox(ctx, tx, tenantID, persisted, projectionExceptionOutboxOptions{EventType: eventType}); err != nil {
		return err
	}
	return nil
}

type projectionExceptionOutboxOptions struct {
	EventType        string
	IdempotencyKey   string
	ActorType        string
	ActorRef         string
	ResolutionID     string
	ResolutionAction string
}

func insertProjectionExceptionOutbox(ctx context.Context, tx pgx.Tx, tenantID string, ex domain.ProjectionException, opts projectionExceptionOutboxOptions) error {
	eventType := defaultString(opts.EventType, projectionExceptionUpdatedEventType)
	idempotencyKey := opts.IdempotencyKey
	if idempotencyKey == "" {
		idempotencyKey = eventType + ":" + ex.ProjectionExceptionID + ":" + ex.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	eventID := platformoutbox.DeterministicUUID(idempotencyKey)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	actorType := defaultString(opts.ActorType, "system_rule")
	payload := projectionExceptionOutboxPayload(ex)
	if opts.ResolutionID != "" {
		payload["resolution_id"] = opts.ResolutionID
	}
	if opts.ResolutionAction != "" {
		payload["resolution_action"] = opts.ResolutionAction
	}
	visibility := map[string]any{"tenant_id": tenantID}
	if ex.ParkID != nil && *ex.ParkID != "" {
		visibility["park_id"] = *ex.ParkID
	}
	if ex.ShedID != nil && *ex.ShedID != "" {
		visibility["shed_id"] = *ex.ShedID
	}
	envelope, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": countsEventSchemaVersion,
		"schema_ref":     countsEventSchemaRef,
		"aggregate_type": "count_projection_exception",
		"aggregate_id":   ex.ProjectionExceptionID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "counts",
			"version": nil,
		},
		"idempotency_key": idempotencyKey,
		"actor": map[string]any{
			"actor_type": actorType,
			"actor_id":   nil,
			"actor_ref":  ptrIfNotEmpty(opts.ActorRef),
		},
		"subject_type":     "count_projection_exception",
		"subject_id":       ex.ProjectionExceptionID,
		"visibility_scope": visibility,
		"evidence_refs": []map[string]string{{
			"evidence_type": "count_projection_exception",
			"evidence_id":   ex.ProjectionExceptionID,
		}},
		"payload":  payload,
		"trace_id": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("counts: projection exception envelope: %w", err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "counts.ProjectionException",
		"schema_version":  countsEventSchemaVersion,
		"idempotency_key": idempotencyKey,
		"event_type":      eventType,
	})
	if err != nil {
		return fmt.Errorf("counts: projection exception headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, 'count_projection_exception', $5::uuid,
  $6, $7::jsonb, $8::jsonb, $9, $9, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		tenantID, eventID, eventType, countsEventSchemaVersion, ex.ProjectionExceptionID,
		countsEventTopic, envelope, headers, idempotencyKey)
	if err != nil {
		return fmt.Errorf("counts: projection exception outbox: %w", err)
	}
	return nil
}

func projectionExceptionOutboxPayload(ex domain.ProjectionException) map[string]any {
	payload := map[string]any{
		"projection_exception_id": ex.ProjectionExceptionID,
		"projection_snapshot_id":  ptrValue(ex.ProjectionSnapshotID),
		"exception_type":          ex.ExceptionType,
		"source_key":              ex.SourceKey,
		"grain_key":               ex.GrainKey,
		"park_id":                 ptrValue(ex.ParkID),
		"shed_id":                 ptrValue(ex.ShedID),
		"breed_key":               ptrValue(ex.BreedKey),
		"stage_tag":               ptrValue(ex.StageTag),
		"severity":                ex.Severity,
		"status":                  ex.Status,
		"owner_ref":               ptrValue(ex.OwnerRef),
		"work_type":               ex.WorkType,
		"work_state":              ex.WorkState,
		"due_at":                  ex.DueAt.UTC().Format(time.RFC3339Nano),
		"next_action":             ex.NextAction,
		"evidence_link":           ex.EvidenceLink,
		"blocker_reason":          ex.BlockerReason,
		"evidence_json":           json.RawMessage(jsonObject(ex.EvidenceJSON)),
		"created_at":              ex.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":              ex.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"source_contract_version": domain.SourceContractVersionV1,
	}
	if ex.ResolutionID != nil {
		payload["resolution_id"] = *ex.ResolutionID
	}
	if ex.ResolvedByRef != nil {
		payload["resolved_by_ref"] = *ex.ResolvedByRef
	}
	if ex.ResolutionReason != nil {
		payload["resolution_reason"] = *ex.ResolutionReason
	}
	if ex.ResolutionRef != nil {
		payload["resolution_ref"] = *ex.ResolutionRef
	}
	if ex.ResolvedAt != nil {
		payload["resolved_at"] = ex.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	return payload
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
	return biztime.BusinessDayStart(t)
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

// GetHerdRegisterSummary reads exact summary counts from canonical goats.
// Returns a single scoped summary row for business KPIs.
func (r *Repository) GetHerdRegisterSummary(ctx context.Context, req domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	const query = `
SELECT
  (NULLIF($3, '')::uuid)::text AS park_id,
  NULL::text AS farm_id,
  NULL::text AS current_location_id,
  NULLIF($4, '') AS breed,
  COALESCE(NULLIF($5, ''), 'all') AS sex,
  COALESCE(NULLIF($2, ''), 'all') AS lifecycle_status,
  count(*) AS total_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'alive') AS active_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'alive' AND NOT herd_register_is_kid(g.age_band, g.management_stage)) AS adult_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'alive' AND herd_register_is_kid(g.age_band, g.management_stage)) AS kid_count,
  count(*) FILTER (
    WHERE g.lifecycle_status = 'alive'
      AND herd_register_is_kid(g.age_band, g.management_stage)
      AND NOT EXISTS (
        SELECT 1
        FROM goat_identifiers gi
        WHERE gi.tenant_id = g.tenant_id
          AND gi.goat_id = g.goat_id
          AND gi.identifier_type = 'animal_identifier_1'
          AND gi.status = 'active'
      )
  ) AS untagged_kid_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'dead') AS dead_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'sold') AS sold_count,
  count(*) FILTER (WHERE g.lifecycle_status = 'culled') AS culled_count,
  now() AS projected_at
FROM goats g
WHERE g.tenant_id = $1
  AND g.merged_into_goat_id IS NULL
  AND ($2 = '' OR g.lifecycle_status = $2)
  AND ($3 = '' OR g.park_id = NULLIF($3, '')::uuid)
  AND ($4 = '' OR g.breed = $4)
  AND ($5 = '' OR g.sex = $5)`

	rows, err := r.pool.Query(ctx, query,
		req.TenantID,
		ptrValue(req.LifecycleStatus),
		ptrValue(req.ParkID),
		ptrValue(req.Breed),
		ptrValue(req.Sex),
	)
	if err != nil {
		return domain.HerdRegisterSummary{}, fmt.Errorf("herd register summary: query: %w", err)
	}
	defer rows.Close()

	out := []domain.HerdRegisterSummaryCounts{}
	for rows.Next() {
		var counts domain.HerdRegisterSummaryCounts
		if err := rows.Scan(
			&counts.ParkID,
			&counts.FarmID,
			&counts.CurrentLocationID,
			&counts.Breed,
			&counts.Sex,
			&counts.LifecycleStatus,
			&counts.TotalCount,
			&counts.ActiveCount,
			&counts.AdultCount,
			&counts.KidCount,
			&counts.UntaggedKidCount,
			&counts.DeadCount,
			&counts.SoldCount,
			&counts.CulledCount,
			&counts.ProjectedAt,
		); err != nil {
			return domain.HerdRegisterSummary{}, fmt.Errorf("herd register summary: scan: %w", err)
		}
		out = append(out, counts)
	}

	if err := rows.Err(); err != nil {
		return domain.HerdRegisterSummary{}, fmt.Errorf("herd register summary: iterate: %w", err)
	}

	return domain.HerdRegisterSummary{
		Items: out,
	}, nil
}

// countsBreakdownGroupedCTE is the ONE definition of the census grain, shared verbatim by the
// page query and the chart query. Keep it a single const: if the two copies ever drift, the
// footer total and the chart series silently disagree and nothing fails loudly.
//
// AS MATERIALIZED is load-bearing. PG12+ inlines a CTE referenced once, which would make the
// four-branch UNION in the chart query re-scan goats four times. Materializing computes the
// grain once and rolls it up four ways.
//
// Bind order is fixed for both consumers:
//
//	$1 tenant_id, $2 lifecycle_status, $3 park_id, $4 shed_id,
//	$5 management_stage, $6 breed, $7 sex
const countsBreakdownGroupedCTE = `
WITH grouped AS MATERIALIZED (
  SELECT
    g.park_id,
    g.shed_id,
    COALESCE(g.management_stage, '') AS management_stage,
    COALESCE(g.breed, '')            AS breed,
    g.sex,
    count(*) AS animal_count,
    -- COALESCE is load-bearing: herd_register_is_kid returns NULL when age_band is NULL (NULL='kid'
    -- propagates), and a bare NOT would then drop those animals from BOTH buckets, so kid+adult
    -- would silently stop summing to the total. Defaulting an unknown age to not-a-kid keeps the
    -- two buckets an exact partition of animal_count.
    count(*) FILTER (WHERE COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false)) AS kid_count,
    count(*) FILTER (WHERE NOT COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false)) AS adult_count
  FROM goats g
  WHERE g.tenant_id = $1::uuid
    AND g.merged_into_goat_id IS NULL
    AND ($2 = '' OR g.lifecycle_status = $2)
    AND ($3 = '' OR g.park_id = NULLIF($3, '')::uuid)
    AND ($4 = '' OR g.shed_id = NULLIF($4, '')::uuid)
    AND ($5 = '' OR COALESCE(g.management_stage, '') = $5)
    AND ($6 = '' OR COALESCE(g.breed, '') = $6)
    AND ($7 = '' OR g.sex = $7)
  GROUP BY g.park_id, g.shed_id, COALESCE(g.management_stage, ''), COALESCE(g.breed, ''), g.sex
)`

// scale-guard:ignore: 5k-50k-envelope — canonical indexed read per
// docs/decisions/operational-kernel-5k-50k-scale-envelope.md. This screen is served directly
// from canonical SQL at the current release envelope; it earns its own projection only under
// that ADR's scale-out ladder.
//
// projection-review: membership=canonical goats rows for tenant with merged_into_goat_id IS NULL, filtered before grouping; group_key=(park_id, shed_id, management_stage, breed, sex) read off denormalized goats columns with no COALESCE hierarchy walk; join_cardinality=locations joined twice (park, shed) on the (tenant_id, location_id) primary key, strict 1:{0,1} label lookups after aggregation so no fan-out; pagination=total_rows and total_count are COUNT/SUM window functions over the FULL grouped CTE, invariant to limit/offset; scope=tenant_id plus optional park/shed/stage/breed/sex equality predicates
//
// Expanded rationale:
//
//	membership   = canonical goats rows for the tenant with merged_into_goat_id IS NULL, so a
//	               merged animal is never counted under both its old and new identity, narrowed
//	               by the request predicates BEFORE grouping.
//	group_key    = (park_id, shed_id, management_stage, breed, sex), read straight off the
//	               denormalized columns on goats. No COALESCE hierarchy walk, so the scope matrix
//	               stays flat: park and shed are independent nullable FKs and each NULL is its own
//	               explicit bucket, never folded into a parent.
//
//	               PARK, NOT FARM. The UI labels this column "Farm" because that is the business
//	               word and the name of the source sheet column — but the source sheet's farm
//	               values (CBE/CPT) are loaded as locations of type 'park', so park_id is where
//	               that data actually lives. goats.farm_id exists but is unpopulated in real
//	               seeded data (0 of 321 live animals), so grouping on it would render a column
//	               that is empty for every row. See seed-vaccination-real/main.go, which resolves
//	               the sheet's farm column against location_type='park'.
//	join_card    = locations is joined twice (park, shed) on (tenant_id, location_id), which is
//	               that table's primary key, so each is a strict 1:{0,1} label lookup that cannot
//	               fan out the COUNT. Both joins run AFTER aggregation, against one page of rows
//	               rather than the whole herd. No other table is joined.
//	pagination   = total_rows and total_count are COUNT/SUM window functions over the FULL
//	               grouped CTE and are therefore invariant to limit/offset and to page size.
//	               The chart series roll up the same CTE, never the returned page.
//	scope        = tenant_id plus optional park/shed/stage/breed/sex equality predicates.
const countsBreakdownPageSQL = countsBreakdownGroupedCTE + ` -- scale-guard:ignore: OFFSET walks the PRE-AGGREGATED grain set (distinct park/shed/stage/breed/sex combinations, tens to low thousands at this envelope), never canonical goats rows; handler rejects offset > 5000 outright.
SELECT
  gr.park_id::text,
  COALESCE(NULLIF(park.location_code, ''), park.name, '') AS park_label,
  gr.shed_id::text,
  COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS shed_label,
  gr.management_stage,
  gr.breed,
  gr.sex,
  gr.animal_count,
  count(*)             OVER () AS total_rows,
  sum(gr.animal_count) OVER () AS total_count,
  sum(gr.kid_count)    OVER () AS total_kids,
  sum(gr.adult_count)  OVER () AS total_adults
FROM grouped gr
LEFT JOIN locations park
       ON park.tenant_id = $1::uuid AND park.location_id = gr.park_id
LEFT JOIN locations shed
       ON shed.tenant_id = $1::uuid AND shed.location_id = gr.shed_id
ORDER BY
  gr.animal_count DESC,
  COALESCE(gr.park_id, '00000000-0000-0000-0000-000000000000'::uuid),
  COALESCE(gr.shed_id, '00000000-0000-0000-0000-000000000000'::uuid),
  gr.management_stage,
  gr.breed,
  gr.sex
LIMIT $8 OFFSET $9`

// scale-guard:ignore: 5k-50k-envelope — same canonical read as countsBreakdownPageSQL.
//
// projection-review: membership=identical to countsBreakdownPageSQL by construction (shared CTE const), re-rolled into four one-dimensional series; group_key=one of breed | management_stage | sex | shed_id per UNION branch; join_cardinality=only the shed branch joins locations, on the (tenant_id, location_id) primary key, so 1:{0,1} with no fan-out, the other three branches join nothing; pagination=every series is a whole-result rollup of the full grouped set, independent of the detail table limit/offset; scope=same tenant plus farm/park/shed/stage/breed/sex predicates as the page query
//
// The shed branch is capped at the top 12 by count for chart legibility. That cap is a DISPLAY
// bound on one chart only and is never the source of total_count, which comes from the page
// query's window function over the full grouped set.
const countsBreakdownChartsSQL = countsBreakdownGroupedCTE + `
SELECT 'breed' AS dimension, gr.breed AS series_key, gr.breed AS series_label, sum(gr.animal_count) AS series_count
FROM grouped gr GROUP BY gr.breed
UNION ALL
SELECT 'stage', gr.management_stage, gr.management_stage, sum(gr.animal_count)
FROM grouped gr GROUP BY gr.management_stage
UNION ALL
SELECT 'sex', gr.sex, gr.sex, sum(gr.animal_count)
FROM grouped gr GROUP BY gr.sex
UNION ALL
SELECT * FROM (
  SELECT 'shed' AS dimension,
         COALESCE(gr.shed_id::text, '') AS series_key,
         COALESCE(NULLIF(shed.name, ''), shed.location_code, '') AS series_label,
         sum(gr.animal_count) AS series_count
  FROM grouped gr
  LEFT JOIN locations shed
         ON shed.tenant_id = $1::uuid AND shed.location_id = gr.shed_id
  GROUP BY gr.shed_id, shed.name, shed.location_code
  ORDER BY series_count DESC, series_key
  LIMIT 12
) top_sheds`

// scale-guard:ignore: 5k-50k-envelope — index-only aggregate on goats_tenant_management_idx /
// goats_breed_text_sex_idx, bounded by distinct vocabulary size, not by herd size.
//
// projection-review: membership=same tenant/merge/lifecycle scope as the grain queries but DELIBERATELY without the stage/breed/farm/shed/sex predicates; group_key=the single facet dimension per UNION branch (management_stage, then breed); join_cardinality=no joins at all, so fan-out is structurally impossible; pagination=whole-result rollup, never paged; scope=tenant_id plus lifecycle_status only
//
// Facets must describe the whole selectable vocabulary, not the current selection — filtering
// them by the active filter would collapse each dropdown to the one value already chosen.
const countsBreakdownFacetsSQL = `
SELECT 'stage' AS dimension, COALESCE(g.management_stage, '') AS series_key,
       COALESCE(g.management_stage, '') AS series_label, count(*) AS series_count
FROM goats g
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND ($2 = '' OR g.lifecycle_status = $2)
GROUP BY COALESCE(g.management_stage, '')
UNION ALL
SELECT 'breed', COALESCE(g.breed, ''), COALESCE(g.breed, ''), count(*)
FROM goats g
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND ($2 = '' OR g.lifecycle_status = $2)
GROUP BY COALESCE(g.breed, '')
UNION ALL
SELECT 'park', COALESCE(g.park_id::text, ''),
       COALESCE(NULLIF(park.location_code, ''), park.name, ''),
       count(*)
FROM goats g
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = g.park_id
WHERE g.tenant_id = $1::uuid
  AND g.merged_into_goat_id IS NULL
  AND ($2 = '' OR g.lifecycle_status = $2)
GROUP BY COALESCE(g.park_id::text, ''), park.location_code, park.name
ORDER BY 1, 2`

const (
	countsBreakdownDefaultLimit = 10
	countsBreakdownMaxLimit     = 100
	countsBreakdownMaxOffset    = 5000
)

// GetCountsBreakdown serves the Counts Breakdown census: one page of farm x stage x breed x sex x
// shed grain rows, plus totals, chart series, and filter facets that are all whole-result rollups.
//
// All three statements go out in one SendBatch round trip. None of them sits inside a loop, so
// this is not an N+1 fan-out — draining already-queued batch results is the sanctioned pattern.
func (r *Repository) GetCountsBreakdown(ctx context.Context, req domain.CountsBreakdownQuery) (domain.CountsBreakdown, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit := req.Limit
	if limit <= 0 {
		limit = countsBreakdownDefaultLimit
	}
	if limit > countsBreakdownMaxLimit {
		limit = countsBreakdownMaxLimit
	}
	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > countsBreakdownMaxOffset {
		offset = countsBreakdownMaxOffset
	}

	// Default to the live herd so this screen's population matches Counts -> Herd Register,
	// which pins status=alive. A caller may override explicitly.
	lifecycle := ptrValue(req.LifecycleStatus)
	if lifecycle == "" {
		lifecycle = "alive"
	}

	grainArgs := []any{
		req.TenantID,
		lifecycle,
		ptrValue(req.ParkID),
		ptrValue(req.ShedID),
		ptrValue(req.ManagementStage),
		ptrValue(req.Breed),
		ptrValue(req.Sex),
	}

	batch := &pgx.Batch{}
	batch.Queue(countsBreakdownPageSQL, append(append([]any{}, grainArgs...), limit, offset)...)
	batch.Queue(countsBreakdownChartsSQL, grainArgs...)
	batch.Queue(countsBreakdownFacetsSQL, req.TenantID, lifecycle)

	results := r.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()

	out := domain.CountsBreakdown{
		Items:  []domain.CountsBreakdownRow{},
		Charts: domain.CountsBreakdownCharts{},
		Facets: domain.CountsBreakdownFacets{},
	}

	pageRows, err := results.Query()
	if err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: page query: %w", err)
	}
	for pageRows.Next() {
		var row domain.CountsBreakdownRow
		var totalRows, totalCount, totalKids, totalAdults int64
		if err := pageRows.Scan(
			&row.ParkID,
			&row.ParkLabel,
			&row.ShedID,
			&row.ShedLabel,
			&row.ManagementStage,
			&row.Breed,
			&row.Sex,
			&row.Count,
			&totalRows,
			&totalCount,
			&totalKids,
			&totalAdults,
		); err != nil {
			pageRows.Close()
			return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: page scan: %w", err)
		}
		// Every row carries the same window totals; the last write wins and they agree.
		out.TotalRows = totalRows
		out.TotalCount = totalCount
		out.TotalKids = totalKids
		out.TotalAdults = totalAdults
		out.Items = append(out.Items, row)
	}
	pageRows.Close()
	if err := pageRows.Err(); err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: page iterate: %w", err)
	}

	chartRows, err := results.Query()
	if err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: charts query: %w", err)
	}
	for chartRows.Next() {
		var dimension, key, label string
		var count int64
		if err := chartRows.Scan(&dimension, &key, &label, &count); err != nil {
			chartRows.Close()
			return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: charts scan: %w", err)
		}
		point := domain.CountsBreakdownSeriesPoint{Key: key, Label: label, Count: count}
		switch dimension {
		case "breed":
			out.Charts.Breed = append(out.Charts.Breed, point)
		case "stage":
			out.Charts.Stage = append(out.Charts.Stage, point)
		case "sex":
			out.Charts.Sex = append(out.Charts.Sex, point)
		case "shed":
			out.Charts.Shed = append(out.Charts.Shed, point)
		}
	}
	chartRows.Close()
	if err := chartRows.Err(); err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: charts iterate: %w", err)
	}

	facetRows, err := results.Query()
	if err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: facets query: %w", err)
	}
	for facetRows.Next() {
		var dimension, key, label string
		var count int64
		if err := facetRows.Scan(&dimension, &key, &label, &count); err != nil {
			facetRows.Close()
			return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: facets scan: %w", err)
		}
		point := domain.CountsBreakdownSeriesPoint{Key: key, Label: label, Count: count}
		switch dimension {
		case "stage":
			out.Facets.Stages = append(out.Facets.Stages, point)
		case "breed":
			out.Facets.Breeds = append(out.Facets.Breeds, point)
		case "park":
			out.Facets.Parks = append(out.Facets.Parks, point)
		}
	}
	facetRows.Close()
	if err := facetRows.Err(); err != nil {
		return domain.CountsBreakdown{}, fmt.Errorf("counts breakdown: facets iterate: %w", err)
	}

	if out.Charts.Breed == nil {
		out.Charts.Breed = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Charts.Stage == nil {
		out.Charts.Stage = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Charts.Sex == nil {
		out.Charts.Sex = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Charts.Shed == nil {
		out.Charts.Shed = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Facets.Stages == nil {
		out.Facets.Stages = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Facets.Breeds == nil {
		out.Facets.Breeds = []domain.CountsBreakdownSeriesPoint{}
	}
	if out.Facets.Parks == nil {
		out.Facets.Parks = []domain.CountsBreakdownSeriesPoint{}
	}

	out.ProjectedAt = time.Now().UTC()
	return out, nil
}
