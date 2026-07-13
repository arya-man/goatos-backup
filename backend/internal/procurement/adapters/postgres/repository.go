// Package postgres implements procurement/source-entry persistence.
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

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
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

func (r *Repository) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.pool.Ping(ctx)
}

func (r *Repository) ListLoads(ctx context.Context, q domain.LoadQuery) (domain.LoadListResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	fetchLimit := limit + 1
	cursorUpdated := pgtype.Timestamptz{}
	var cursorLoadID any
	if q.Cursor != nil {
		cursorUpdated = pgtype.Timestamptz{Time: q.Cursor.UpdatedAt, Valid: true}
		cursorLoadID = q.Cursor.LoadID
	}
	rows, err := r.pool.Query(ctx, `
SELECT pl.load_id::text, pl.tenant_id::text, pl.source_party_id::text, p.display_name,
       pl.source_location_id::text, loc.location_code, loc.name,
       pl.expected_count, pl.purchase_date, pl.planned_dispatch_at, pl.status, pl.notes, pl.context,
       pl.created_at, pl.updated_at, pl.row_version
FROM procurement_loads pl
JOIN parties p ON p.party_id = pl.source_party_id
LEFT JOIN locations loc ON loc.tenant_id = pl.tenant_id AND loc.location_id = pl.source_location_id
WHERE pl.tenant_id = $1::uuid
  AND ($2::text = '' OR pl.status = $2::text)
  AND (
    $4::timestamptz IS NULL
    OR (pl.updated_at, pl.load_id) < ($4::timestamptz, $5::uuid)
  )
ORDER BY pl.updated_at DESC, pl.load_id DESC
LIMIT $3`, q.TenantID, q.Status, fetchLimit, cursorUpdated, cursorLoadID)
	if err != nil {
		return domain.LoadListResult{}, fmt.Errorf("procurement: list loads: %w", err)
	}
	defer rows.Close()
	out := []domain.Load{}
	for rows.Next() {
		load, err := scanLoad(rows)
		if err != nil {
			return domain.LoadListResult{}, err
		}
		out = append(out, load)
	}
	if err := rows.Err(); err != nil {
		return domain.LoadListResult{}, err
	}
	var next *string
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		cursor, err := domain.EncodeLoadCursor(domain.LoadCursor{UpdatedAt: last.UpdatedAt, LoadID: last.LoadID})
		if err != nil {
			return domain.LoadListResult{}, err
		}
		next = &cursor
	}
	return domain.LoadListResult{Items: out, NextCursor: next}, nil
}

func (r *Repository) CreateLoad(ctx context.Context, in ports.CreateLoad) (domain.Load, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Load{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.load"
	fingerprint := requestFingerprint(
		in.TenantID, in.SourcePartyID, stringPtrValue(in.SourceLocationID),
		fmt.Sprintf("%d", in.ExpectedCount), fpTime(in.PurchaseDate), fpTime(in.PlannedDispatch),
		in.Notes, canonicalJSON(in.Context),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.Load{}, rerr
		}
		if !res.proceed {
			// Replay of an already-created load: return the original result, run NO side effects.
			_ = tx.Rollback(ctx)
			return r.getLoadByID(ctx, in.TenantID, res.resultID)
		}
	}

	load, err := scanLoad(tx.QueryRow(ctx, `
INSERT INTO procurement_loads (
  tenant_id, source_party_id, source_location_id, expected_count, purchase_date,
  planned_dispatch_at, notes, context, idempotency_key, created_by
) VALUES (
  $1::uuid, $2::uuid, nullif($3::text, '')::uuid, $4, $5::date,
  $6::timestamptz, $7, $8::jsonb, $9, nullif($10::text, '')::uuid
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING load_id::text, tenant_id::text, source_party_id::text,
          (SELECT display_name FROM parties WHERE party_id = procurement_loads.source_party_id),
          source_location_id::text,
          (SELECT location_code FROM locations WHERE locations.tenant_id = procurement_loads.tenant_id AND locations.location_id = procurement_loads.source_location_id),
          (SELECT name FROM locations WHERE locations.tenant_id = procurement_loads.tenant_id AND locations.location_id = procurement_loads.source_location_id),
          expected_count, purchase_date, planned_dispatch_at, status, notes, context,
          created_at, updated_at, row_version`,
		in.TenantID, in.SourcePartyID, stringPtrValue(in.SourceLocationID), in.ExpectedCount,
		dateArg(in.PurchaseDate), timeArg(in.PlannedDispatch), in.Notes, jsonObjectArg(in.Context),
		in.IdempotencyKey, stringPtrValue(in.ActorID)))
	if err != nil {
		return domain.Load{}, fmt.Errorf("procurement: create load: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.ActorID),
		ActorType:    "human",
		Action:       "procurement.source_load.created",
		ResourceType: "procurement_load",
		ResourceID:   load.LoadID,
		ScopeType:    "load",
		ScopeID:      load.LoadID,
		AfterState:   load,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "source_entry",
			"category":        "source_load",
			"result":          load.Status,
			"status":          load.Status,
			"idempotency_key": in.IdempotencyKey,
			"operation_id":    in.IdempotencyKey,
			"source_party_id": in.SourcePartyID,
		},
	}); err != nil {
		return domain.Load{}, fmt.Errorf("procurement: audit source load create: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_load", load.LoadID); err != nil {
			return domain.Load{}, fmt.Errorf("procurement: complete create load idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Load{}, err
	}
	return load, nil
}

func (r *Repository) GetLoadDetail(ctx context.Context, tenantID, loadID string) (domain.LoadDetail, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	load, err := scanLoad(r.pool.QueryRow(ctx, `
SELECT pl.load_id::text, pl.tenant_id::text, pl.source_party_id::text, p.display_name,
       pl.source_location_id::text, loc.location_code, loc.name,
       pl.expected_count, pl.purchase_date, pl.planned_dispatch_at, pl.status, pl.notes, pl.context,
       pl.created_at, pl.updated_at, pl.row_version
FROM procurement_loads pl
JOIN parties p ON p.party_id = pl.source_party_id
LEFT JOIN locations loc ON loc.tenant_id = pl.tenant_id AND loc.location_id = pl.source_location_id
WHERE pl.tenant_id = $1::uuid AND pl.load_id = $2::uuid`, tenantID, loadID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LoadDetail{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.LoadDetail{}, fmt.Errorf("procurement: get load: %w", err)
	}

	detail := domain.LoadDetail{Load: load}
	if detail.Goats, err = r.listLoadGoats(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.HoldingStays, err = r.listHoldingStays(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.HFVaccinations, err = r.listHFVaccinationEvidence(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.HealthChecks, err = r.listHealthChecks(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.Decisions, err = r.listDecisions(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.Transit, err = r.listTransit(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.ArrivalReviews, err = r.listArrivalReviews(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	if detail.PCHandoffs, err = r.listPCHandoffs(ctx, tenantID, loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	detail.Timeline = buildTimeline(detail)
	return detail, nil
}

func (r *Repository) AddGoatToLoad(ctx context.Context, in ports.AddGoatToLoad) (domain.LoadGoat, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	if strings.TrimSpace(in.Purpose) == "" {
		in.Purpose = domain.PurposeUnspecified
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.LoadGoat{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.load_goat"
	// Fingerprint the ORIGINAL request before any in-flight mutation of in.* below.
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, stringPtrValue(in.GoatID), stringPtrValue(in.AnimalIdentifier1),
		stringPtrValue(in.AnimalIdentifier2), in.Species, in.Sex, in.SelectionState,
		in.SelectionReason, in.Purpose, in.CurrentState, in.SourceEntryState, in.OwnershipState, in.HealthState,
		stringPtrValue(in.HoldingLocationID), fpTime(in.WarmupStartedAt), fpTime(in.WarmupEndedAt),
		intPtrFingerprint(in.WarmupDays), canonicalJSON(in.ProofRefs), canonicalJSON(in.Metadata),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.LoadGoat{}, rerr
		}
		if !res.proceed {
			// Replay of an already-added goat: return the original row, run NO side effects (no new goat).
			_ = tx.Rollback(ctx)
			return r.getLoadGoatByID(ctx, in.TenantID, res.resultID)
		}
	}

	var sourcePartyID string
	var sourceLocation pgtype.Text
	if err = tx.QueryRow(ctx, `
SELECT source_party_id::text, source_location_id::text
FROM procurement_loads
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`, in.TenantID, in.LoadID).Scan(&sourcePartyID, &sourceLocation); err != nil {
		return domain.LoadGoat{}, mapNotFound(err)
	}

	goatID := stringPtrValue(in.GoatID)
	if ownerID, lookupErr := lookupAnimalIdentifierOwner(ctx, tx, in.TenantID, in.AnimalIdentifier1); lookupErr != nil {
		return domain.LoadGoat{}, lookupErr
	} else if ownerID != nil && (goatID == "" || goatID != *ownerID) {
		return domain.LoadGoat{}, fmt.Errorf("%w: animal identifier 1 already belongs to animal %s", ports.ErrInvalidTransition, *ownerID)
	}
	if ownerID, lookupErr := lookupAnimalIdentifierOwner(ctx, tx, in.TenantID, in.AnimalIdentifier2); lookupErr != nil {
		return domain.LoadGoat{}, lookupErr
	} else if ownerID != nil && (goatID == "" || goatID != *ownerID) {
		return domain.LoadGoat{}, fmt.Errorf("%w: animal identifier 2 already belongs to animal %s", ports.ErrInvalidTransition, *ownerID)
	}
	if goatID == "" {
		if err = tx.QueryRow(ctx, `
	INSERT INTO goats (
	  tenant_id, lifecycle_status, custodian_party_id, species, sex,
	  origin_type, created_by
	) VALUES (
	  $1::uuid, 'inactive', $2::uuid, $3, $4, 'procured', nullif($5::text, '')::uuid
	)
	RETURNING goat_id::text`, in.TenantID, sourcePartyID, in.Species, in.Sex, stringPtrValue(in.ActorID)).Scan(&goatID); err != nil {
			return domain.LoadGoat{}, fmt.Errorf("procurement: create source animal: %w", err)
		}
	} else if err = ensureExistingGoatSpeciesAndSex(ctx, tx, in.TenantID, goatID, in.Species, in.Sex); err != nil {
		return domain.LoadGoat{}, err
	}
	if err = insertAnimalIdentifier(ctx, tx, in.TenantID, goatID, "animal_identifier_1", in.AnimalIdentifier1, true); err != nil {
		return domain.LoadGoat{}, err
	}
	if err = insertAnimalIdentifier(ctx, tx, in.TenantID, goatID, "animal_identifier_2", in.AnimalIdentifier2, false); err != nil {
		return domain.LoadGoat{}, err
	}

	row := tx.QueryRow(ctx, `
	INSERT INTO procurement_load_goats (
	  tenant_id, load_id, goat_id, animal_identifier_1, animal_identifier_2,
	  selection_state, selection_reason, purpose, current_state, source_entry_state,
	  source_entry_ref, ownership_state, health_state, warmup_started_at,
	  warmup_ended_at, warmup_days, holding_location_id, proof_refs, metadata
	) VALUES (
	  $1::uuid, $2::uuid, $3::uuid, nullif($4::text, ''), nullif($5::text, ''),
	  $6, $7, $8, $9, $10, nullif($11::text, ''), $12, $13, $14::timestamptz,
	  $15::timestamptz, $16, nullif($17::text, '')::uuid, $18::jsonb, $19::jsonb
	)
	ON CONFLICT (tenant_id, load_id, goat_id) DO UPDATE
	SET animal_identifier_1 = COALESCE(EXCLUDED.animal_identifier_1, procurement_load_goats.animal_identifier_1),
	    animal_identifier_2 = COALESCE(EXCLUDED.animal_identifier_2, procurement_load_goats.animal_identifier_2),
	    selection_state = EXCLUDED.selection_state,
	    selection_reason = EXCLUDED.selection_reason,
    purpose = EXCLUDED.purpose,
    current_state = EXCLUDED.current_state,
    source_entry_state = EXCLUDED.source_entry_state,
    source_entry_ref = COALESCE(EXCLUDED.source_entry_ref, procurement_load_goats.source_entry_ref),
    ownership_state = EXCLUDED.ownership_state,
    health_state = EXCLUDED.health_state,
    warmup_started_at = COALESCE(EXCLUDED.warmup_started_at, procurement_load_goats.warmup_started_at),
    warmup_ended_at = COALESCE(EXCLUDED.warmup_ended_at, procurement_load_goats.warmup_ended_at),
    warmup_days = COALESCE(EXCLUDED.warmup_days, procurement_load_goats.warmup_days),
    holding_location_id = COALESCE(EXCLUDED.holding_location_id, procurement_load_goats.holding_location_id),
    proof_refs = EXCLUDED.proof_refs,
    metadata = EXCLUDED.metadata,
	    updated_at = now(),
	    row_version = procurement_load_goats.row_version + 1
		RETURNING load_goat_id::text, tenant_id::text, load_id::text, goat_id::text,
	          animal_identifier_1, animal_identifier_2, selection_state, selection_reason, purpose,
	          current_state, source_entry_state, source_entry_ref, ownership_state,
	          health_state, warmup_started_at, warmup_ended_at, warmup_days,
	          holding_location_id::text, loaded_at, arrived_at, intake_accepted_at,
	          exit_reason, proof_refs, metadata, created_at, updated_at, row_version`,
		in.TenantID, in.LoadID, goatID, stringPtrValue(in.AnimalIdentifier1), stringPtrValue(in.AnimalIdentifier2),
		in.SelectionState, in.SelectionReason, in.Purpose,
		in.CurrentState, in.SourceEntryState, stringPtrValue(in.SourceEntryRef), in.OwnershipState, in.HealthState,
		timeArg(in.WarmupStartedAt), timeArg(in.WarmupEndedAt), intPtrArg(in.WarmupDays),
		stringPtrValue(in.HoldingLocationID), jsonArrayArg(in.ProofRefs), jsonObjectArg(in.Metadata))
	loadGoat, err := scanLoadGoat(row)
	if err != nil {
		return domain.LoadGoat{}, fmt.Errorf("procurement: add animal to load: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.ActorID),
		ActorType:    "human",
		Action:       "procurement.source_animal.added",
		ResourceType: "procurement_load_goat",
		ResourceID:   loadGoat.LoadGoatID,
		ScopeType:    "load",
		ScopeID:      in.LoadID,
		AfterState:   loadGoat,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "source_entry",
			"category":        "source_animal",
			"result":          loadGoat.CurrentState,
			"status":          loadGoat.CurrentState,
			"idempotency_key": in.IdempotencyKey,
			"operation_id":    in.IdempotencyKey,
			"goat_id":         loadGoat.GoatID,
			"load_id":         in.LoadID,
			"purpose":         loadGoat.Purpose,
		},
	}); err != nil {
		return domain.LoadGoat{}, fmt.Errorf("procurement: audit source animal add: %w", err)
	}
	if in.HoldingLocationID != nil && in.WarmupStartedAt != nil {
		_, err = tx.Exec(ctx, `
INSERT INTO source_holding_stays (
  tenant_id, goat_id, load_id, holding_location_id, started_at, ended_at,
  warmup_state, warmup_days, purpose, health_state, ownership_state, proof_refs
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::timestamptz, $6::timestamptz,
  $7, $8, $9, $10, $11, $12::jsonb
)
ON CONFLICT (tenant_id, load_id, goat_id, holding_location_id, started_at) DO UPDATE
SET ended_at = COALESCE(EXCLUDED.ended_at, source_holding_stays.ended_at),
    warmup_state = EXCLUDED.warmup_state,
    warmup_days = COALESCE(EXCLUDED.warmup_days, source_holding_stays.warmup_days),
    purpose = EXCLUDED.purpose,
    health_state = EXCLUDED.health_state,
    ownership_state = EXCLUDED.ownership_state,
    proof_refs = EXCLUDED.proof_refs,
    updated_at = now(),
    row_version = source_holding_stays.row_version + 1`,
			in.TenantID, goatID, in.LoadID, *in.HoldingLocationID, timeArg(in.WarmupStartedAt),
			timeArg(in.WarmupEndedAt), warmupState(in.WarmupDays, in.Purpose), intPtrArg(in.WarmupDays),
			in.Purpose, in.HealthState, in.OwnershipState, jsonArrayArg(in.ProofRefs))
		if err != nil {
			return domain.LoadGoat{}, fmt.Errorf("procurement: upsert holding stay: %w", err)
		}
	}
	if sourceLocation.Valid && in.HoldingLocationID == nil {
		_ = sourceLocation
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_load_goat", loadGoat.LoadGoatID); err != nil {
			return domain.LoadGoat{}, fmt.Errorf("procurement: complete add goat idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.LoadGoat{}, err
	}
	return loadGoat, nil
}

// GoatOnLoad reports whether the goat is currently a member of the procurement load (tenant-scoped).
// Used by the app layer to validate HF vaccination evidence references before the write.
func (r *Repository) GoatOnLoad(ctx context.Context, tenantID, loadID, goatID string) (bool, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM procurement_load_goats
  WHERE tenant_id = $1::uuid AND load_id = $2::uuid AND goat_id = $3::uuid
)`, tenantID, loadID, goatID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("procurement: check goat on load: %w", err)
	}
	return exists, nil
}

// mapHFReferenceError translates a Postgres foreign-key violation (SQLSTATE 23503) on the HF evidence
// insert into ports.ErrInvalidReference, so a missing protocol version / rule / goat / load / proof
// surfaces as a 400 instead of a raw 500. Returns nil for any other error.
func mapHFReferenceError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("%w (%s)", ports.ErrInvalidReference, pgErr.ConstraintName)
	}
	return nil
}

func (r *Repository) RecordHFVaccinationEvidence(ctx context.Context, in ports.HFVaccinationEvidence) (domain.HFVaccinationEvidence, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.hf_vaccination_evidence.import"
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, in.GoatID, in.ProtocolVersionID, in.RuleID, in.DoseCode,
		in.AdministeredAt.UTC().Format(time.RFC3339Nano), in.VaccineName, in.LotNumber,
		stringPtrValue(in.ProofRefID), in.SourceRef, canonicalJSON(in.Metadata),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.HFVaccinationEvidence{}, rerr
		}
		if !res.proceed {
			_ = tx.Rollback(ctx)
			return r.getHFVaccinationEvidenceByID(ctx, in.TenantID, res.resultID)
		}
	}

	evidence, err := scanHFVaccinationEvidence(tx.QueryRow(ctx, `
INSERT INTO procurement_hf_vaccination_evidence (
  tenant_id, load_id, goat_id, protocol_version_id, rule_id, dose_code,
  administered_at, vaccine_name, lot_number, proof_ref_id, source_ref,
  idempotency_key, imported_by, metadata
)
SELECT
  $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6,
  $7::timestamptz, $8, $9, nullif($10::text, '')::uuid, $11,
  $12, nullif($13::text, '')::uuid, $14::jsonb
WHERE EXISTS (
  SELECT 1
  FROM procurement_load_goats plg
  WHERE plg.tenant_id = $1::uuid
    AND plg.load_id = $2::uuid
    AND plg.goat_id = $3::uuid
)
RETURNING evidence_id::text, tenant_id::text, load_id::text, goat_id::text,
          protocol_version_id::text, rule_id::text, dose_code, administered_at,
          vaccine_name, lot_number, proof_ref_id::text, source_ref, review_status,
          reviewed_by::text, reviewed_at, review_reason, imported_by::text,
          imported_at, metadata, created_at, updated_at, row_version`,
		in.TenantID, in.LoadID, in.GoatID, in.ProtocolVersionID, in.RuleID, in.DoseCode,
		in.AdministeredAt, in.VaccineName, in.LotNumber, stringPtrValue(in.ProofRefID),
		in.SourceRef, in.IdempotencyKey, stringPtrValue(in.ImportedBy), jsonObjectArg(in.Metadata)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HFVaccinationEvidence{}, ports.ErrInvalidTransition
	}
	if err != nil {
		if ref := mapHFReferenceError(err); ref != nil {
			return domain.HFVaccinationEvidence{}, ref
		}
		return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: record HF vaccination evidence: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.ImportedBy),
		ActorType:    "human",
		Action:       "procurement.hf_vaccination_evidence.imported",
		ResourceType: "hf_vaccination_evidence",
		ResourceID:   evidence.EvidenceID,
		ScopeType:    "load",
		ScopeID:      in.LoadID,
		AfterState:   evidence,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "source_entry",
			"category":        "hf_vaccination_evidence",
			"result":          evidence.ReviewStatus,
			"status":          evidence.ReviewStatus,
			"idempotency_key": in.IdempotencyKey,
			"operation_id":    in.IdempotencyKey,
			"goat_id":         in.GoatID,
			"load_id":         in.LoadID,
			"rule_id":         in.RuleID,
		},
	}); err != nil {
		return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: audit HF vaccination evidence import: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_hf_vaccination_evidence", evidence.EvidenceID); err != nil {
			return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: complete HF evidence import idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	return evidence, nil
}

func (r *Repository) ReviewHFVaccinationEvidence(ctx context.Context, in ports.ReviewHFVaccinationEvidence) (domain.HFVaccinationEvidence, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.hf_vaccination_evidence.review"
	fingerprint := requestFingerprint(
		in.TenantID, in.EvidenceID, fmt.Sprintf("%d", in.ExpectedRowVersion), in.ReviewStatus, in.ReviewReason,
		stringPtrValue(in.ReviewedBy), fpTimeIf(in.ReviewedAtSet, in.ReviewedAt),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.HFVaccinationEvidence{}, rerr
		}
		if !res.proceed {
			_ = tx.Rollback(ctx)
			return r.getHFVaccinationEvidenceByID(ctx, in.TenantID, res.resultID)
		}
	}

	before, err := r.getHFVaccinationEvidenceByIDTx(ctx, tx, in.TenantID, in.EvidenceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HFVaccinationEvidence{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if before.RowVersion != in.ExpectedRowVersion {
		return domain.HFVaccinationEvidence{}, ports.ErrStaleWrite
	}
	// Trusted evidence can suppress a real post-arrival dose. It is only valid when the dose was
	// administered inside our governed procurement holding stay, not from an outside/vendor claim.
	if in.ReviewStatus == domain.HFVaccinationReviewTrusted &&
		(before.ProofRefID == nil || strings.TrimSpace(*before.ProofRefID) == "") {
		return domain.HFVaccinationEvidence{}, ports.ErrProofRequired
	}
	if in.ReviewStatus == domain.HFVaccinationReviewTrusted {
		trustOK, err := trustedProcurementHoldingEvidenceContext(ctx, tx, in.TenantID, in.EvidenceID)
		if err != nil {
			return domain.HFVaccinationEvidence{}, err
		}
		if !trustOK {
			return domain.HFVaccinationEvidence{}, ports.ErrInvalidTrustContext
		}
	}
	if before.ReviewStatus == domain.HFVaccinationReviewTrusted && in.ReviewStatus != domain.HFVaccinationReviewTrusted {
		return domain.HFVaccinationEvidence{}, fmt.Errorf("%w: trusted HF vaccination evidence requires correction workflow", ports.ErrInvalidTransition)
	}
	evidence, err := scanHFVaccinationEvidence(tx.QueryRow(ctx, `
UPDATE procurement_hf_vaccination_evidence
SET review_status = $3,
    review_reason = $4,
    reviewed_by = nullif($5::text, '')::uuid,
    reviewed_at = $6::timestamptz,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND evidence_id = $2::uuid
  AND row_version = $7
RETURNING evidence_id::text, tenant_id::text, load_id::text, goat_id::text,
          protocol_version_id::text, rule_id::text, dose_code, administered_at,
          vaccine_name, lot_number, proof_ref_id::text, source_ref, review_status,
          reviewed_by::text, reviewed_at, review_reason, imported_by::text,
          imported_at, metadata, created_at, updated_at, row_version`,
		in.TenantID, in.EvidenceID, in.ReviewStatus, in.ReviewReason, stringPtrValue(in.ReviewedBy), in.ReviewedAt, in.ExpectedRowVersion))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.HFVaccinationEvidence{}, ports.ErrStaleWrite
	}
	if err != nil {
		return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: review HF vaccination evidence: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.ReviewedBy),
		ActorType:    "human",
		Action:       "procurement.hf_vaccination_evidence.reviewed",
		ResourceType: "hf_vaccination_evidence",
		ResourceID:   evidence.EvidenceID,
		ScopeType:    "load",
		ScopeID:      evidence.LoadID,
		BeforeState:  before,
		AfterState:   evidence,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "source_entry",
			"category":        "hf_vaccination_evidence",
			"result":          evidence.ReviewStatus,
			"status":          evidence.ReviewStatus,
			"idempotency_key": in.IdempotencyKey,
			"operation_id":    in.IdempotencyKey,
			"goat_id":         evidence.GoatID,
			"load_id":         evidence.LoadID,
			"rule_id":         evidence.RuleID,
		},
	}); err != nil {
		return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: audit HF vaccination evidence review: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_hf_vaccination_evidence", evidence.EvidenceID); err != nil {
			return domain.HFVaccinationEvidence{}, fmt.Errorf("procurement: complete HF evidence review idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	return evidence, nil
}

func (r *Repository) RecordSourceHealth(ctx context.Context, in ports.SourceHealth) (domain.SourceHealthCheck, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.SourceHealthCheck{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.source_health"
	// CheckedAt enters the fingerprint only when the client supplied it; an omitted value is server-defaulted
	// to now() (service.go) and excluded, so a later exact replay is not misread as a different payload — while
	// a genuinely different client-supplied checked_at still conflicts.
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, in.GoatID, in.HealthState, in.Reason,
		stringPtrValue(in.CheckedBy), fpTimeIf(in.CheckedAtSet, in.CheckedAt),
		stringPtrValue(in.ProofRefID), stringPtrValue(in.SOPTaskID),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.SourceHealthCheck{}, rerr
		}
		if !res.proceed {
			// Replay of an already-recorded request: return the original result, run NO side effects. Flag it
			// so the service skips replay-unsafe side effects (re-cancelling vaccination obligations).
			_ = tx.Rollback(ctx)
			out, rerr := r.getSourceHealthByID(ctx, in.TenantID, res.resultID)
			out.Replayed = true
			return out, rerr
		}
	}

	check, err := scanHealthCheck(tx.QueryRow(ctx, `
INSERT INTO procurement_source_health_checks (
  tenant_id, goat_id, load_id, health_state, reason, checked_by, checked_at,
  proof_ref_id, sop_task_id, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5, nullif($6::text, '')::uuid, $7::timestamptz,
  nullif($8::text, '')::uuid, nullif($9::text, '')::uuid, $10
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING health_check_id::text, tenant_id::text, goat_id::text, load_id::text,
          health_state, reason, checked_by::text, checked_at, proof_ref_id::text,
          sop_task_id::text, created_at`,
		in.TenantID, in.GoatID, in.LoadID, in.HealthState, in.Reason,
		stringPtrValue(in.CheckedBy), in.CheckedAt, stringPtrValue(in.ProofRefID),
		stringPtrValue(in.SOPTaskID), in.IdempotencyKey))
	if err != nil {
		return domain.SourceHealthCheck{}, fmt.Errorf("procurement: record source health: %w", err)
	}
	state := mapHealthStateToProcurementState(in.HealthState)
	loadStatus := domain.LoadStatusPreDispatchPending
	if in.HealthState == domain.HealthFailed {
		loadStatus = domain.LoadStatusBlocked
	}
	if in.HealthState == domain.HealthDeferred {
		loadStatus = domain.LoadStatusDeferred
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_load_goats
SET health_state = $4,
    current_state = $5,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid AND goat_id = $3::uuid`,
		in.TenantID, in.LoadID, in.GoatID, in.HealthState, state); err != nil {
		return domain.SourceHealthCheck{}, fmt.Errorf("procurement: update health state: %w", err)
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_loads
SET status = $3, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`,
		in.TenantID, in.LoadID, loadStatus); err != nil {
		return domain.SourceHealthCheck{}, fmt.Errorf("procurement: update load health status: %w", err)
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     in.TenantID,
		ActorID:      stringPtrValue(in.CheckedBy),
		ActorType:    "human",
		Action:       "procurement.source_health.recorded",
		ResourceType: "procurement_source_health_check",
		ResourceID:   check.HealthCheckID,
		ScopeType:    "load",
		ScopeID:      in.LoadID,
		AfterState:   check,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "source_entry",
			"category":        "source_health",
			"result":          check.HealthState,
			"status":          check.HealthState,
			"idempotency_key": in.IdempotencyKey,
			"operation_id":    in.IdempotencyKey,
			"goat_id":         in.GoatID,
			"load_id":         in.LoadID,
		},
	}); err != nil {
		return domain.SourceHealthCheck{}, fmt.Errorf("procurement: audit source health: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_source_health_check", check.HealthCheckID); err != nil {
			return domain.SourceHealthCheck{}, fmt.Errorf("procurement: complete source health idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	return check, nil
}

func (r *Repository) RecordDecision(ctx context.Context, in ports.Decision) (domain.Decision, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Decision{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.decision"
	// DecidedAt enters the fingerprint only when client-supplied (omitted = server-defaulted to now(),
	// excluded), so a later exact replay is not misread as a different payload.
	fingerprint := requestFingerprint(
		in.TenantID, in.GoatID, in.LoadID, in.DecisionStage, in.DecisionType, in.Reason,
		stringPtrValue(in.DecidedBy), fpTimeIf(in.DecidedAtSet, in.DecidedAt),
		stringPtrValue(in.ProofRefID), stringPtrValue(in.SOPTaskID),
		stringPtrValue(in.OwnerID), stringPtrValue(in.ResumeCondition), canonicalJSON(in.Metadata),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.Decision{}, rerr
		}
		if !res.proceed {
			_ = tx.Rollback(ctx)
			out, rerr := r.getDecisionByID(ctx, in.TenantID, res.resultID)
			out.Replayed = true
			return out, rerr
		}
	}

	decision, err := scanDecision(tx.QueryRow(ctx, `
INSERT INTO source_entry_decisions (
  tenant_id, goat_id, load_id, decision_stage, decision_type, reason, decided_by,
  decided_at, proof_ref_id, sop_task_id, owner_id, resume_condition, idempotency_key, metadata
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5, $6, nullif($7::text, '')::uuid,
  $8::timestamptz, nullif($9::text, '')::uuid, nullif($10::text, '')::uuid,
  nullif($11::text, '')::uuid, nullif($12::text, ''), $13, $14::jsonb
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING decision_id::text, tenant_id::text, goat_id::text, load_id::text,
          decision_stage, decision_type, reason, decided_by::text, decided_at,
          proof_ref_id::text, sop_task_id::text, owner_id::text, resume_condition,
          metadata, created_at`,
		in.TenantID, in.GoatID, in.LoadID, in.DecisionStage, in.DecisionType,
		in.Reason, stringPtrValue(in.DecidedBy), in.DecidedAt, stringPtrValue(in.ProofRefID),
		stringPtrValue(in.SOPTaskID), stringPtrValue(in.OwnerID), stringPtrValue(in.ResumeCondition),
		in.IdempotencyKey, jsonObjectArg(in.Metadata)))
	if err != nil {
		return domain.Decision{}, fmt.Errorf("procurement: record decision: %w", err)
	}
	goatState, selectionState, loadStatus := decisionState(in.DecisionType)
	sourceEntryState := ""
	ownershipState := ""
	reason := strings.ToLower(in.Reason)
	if in.DecisionType == domain.DecisionBlocked && (strings.Contains(reason, "source") || strings.Contains(reason, "identifier")) {
		sourceEntryState = "blocked"
	}
	if in.DecisionType == domain.DecisionBlocked && strings.Contains(reason, "ownership") {
		ownershipState = "blocked"
	}
	tag, err := tx.Exec(ctx, `
UPDATE procurement_load_goats
SET current_state = $4,
    selection_state = $5,
    source_entry_state = CASE WHEN $6::text = '' THEN source_entry_state ELSE $6 END,
    ownership_state = CASE WHEN $7::text = '' THEN ownership_state ELSE $7 END,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND goat_id = $3::uuid
  AND (
    $4::text <> 'pre_dispatch_accepted'
    OR (
      current_state IN ('source_health_passed', 'pre_dispatch_pending')
      AND health_state = 'passed'
      AND source_entry_state = 'accepted'
      AND ownership_state IN ('mesha_owned', 'settled')
    )
  )`,
		in.TenantID, in.LoadID, in.GoatID, goatState, selectionState, sourceEntryState, ownershipState)
	if err != nil {
		return domain.Decision{}, fmt.Errorf("procurement: update decision state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.Decision{}, fmt.Errorf("%w: goat is not eligible for pre-dispatch acceptance", ports.ErrInvalidTransition)
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_loads
SET status = $3, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`,
		in.TenantID, in.LoadID, loadStatus); err != nil {
		return domain.Decision{}, fmt.Errorf("procurement: update load decision status: %w", err)
	}
	if currentState, exitReason, ok := exitStateFromReason(in.Reason); ok {
		lifecycle := currentState
		if currentState == domain.GoatStateDead {
			lifecycle = "dead"
		}
		if _, err = tx.Exec(ctx, `
UPDATE procurement_load_goats
SET current_state = $4,
    selection_state = $4,
    exit_reason = $5,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid AND goat_id = $3::uuid`,
			in.TenantID, in.LoadID, in.GoatID, currentState, exitReason); err != nil {
			return domain.Decision{}, fmt.Errorf("procurement: update exit state: %w", err)
		}
		if _, err = tx.Exec(ctx, `
UPDATE goats
SET lifecycle_status = $3,
    exited_at = $4::timestamptz,
    exit_reason = $5,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
			in.TenantID, in.GoatID, lifecycle, in.DecidedAt, exitReason); err != nil {
			return domain.Decision{}, fmt.Errorf("procurement: update goat exit lifecycle: %w", err)
		}
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "source_entry_decision", decision.DecisionID); err != nil {
			return domain.Decision{}, fmt.Errorf("procurement: complete decision idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.Decision{}, err
	}
	return decision, nil
}

func (r *Repository) DispatchLoad(ctx context.Context, in ports.DispatchLoad) (domain.TransitHandoff, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.TransitHandoff{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.dispatch"
	// DispatchedAt enters the fingerprint only when client-supplied (omitted = server-defaulted to now(),
	// excluded); the goat list is order-normalized so the same set in any order is the same request.
	dispatchGoats := append([]string(nil), in.GoatIDs...)
	sort.Strings(dispatchGoats)
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, stringPtrValue(in.FromLocationID), in.ToLocationID,
		stringPtrValue(in.ProofRefID), fpTimeIf(in.DispatchedAtSet, in.DispatchedAt), fpTime(in.ArrivedAt),
		strings.Join(dispatchGoats, ","),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.TransitHandoff{}, rerr
		}
		if !res.proceed {
			// Replay of an already-recorded dispatch: return the original handoff, run NO side effects.
			_ = tx.Rollback(ctx)
			return r.getTransitHandoffByID(ctx, in.TenantID, res.resultID)
		}
	}

	loadedCount := 0
	discrepancy := "none"
	status := "in_transit"
	loadStatus := domain.LoadStatusInTransit
	if in.ProofRefID == nil || strings.TrimSpace(*in.ProofRefID) == "" {
		discrepancy = "blocked"
		status = "planned"
		loadStatus = domain.LoadStatusBlocked
	} else {
		acceptedCount, countErr := countPreDispatchAccepted(ctx, tx, in.TenantID, in.LoadID)
		if countErr != nil {
			return domain.TransitHandoff{}, countErr
		}
		if acceptedCount == 0 {
			return domain.TransitHandoff{}, fmt.Errorf("%w: dispatch requires at least one eligible pre-dispatch accepted goat", ports.ErrInvalidTransition)
		}
		if len(in.GoatIDs) == 0 {
			tag, execErr := tx.Exec(ctx, `
	UPDATE procurement_load_goats
SET current_state = 'in_transit',
    selection_state = 'loaded',
    loaded_at = $3::timestamptz,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND current_state = 'pre_dispatch_accepted'
  AND health_state = 'passed'
  AND source_entry_state = 'accepted'
  AND ownership_state IN ('mesha_owned', 'settled')`,
				in.TenantID, in.LoadID, in.DispatchedAt)
			if execErr != nil {
				return domain.TransitHandoff{}, fmt.Errorf("procurement: load accepted goats: %w", execErr)
			}
			loadedCount = int(tag.RowsAffected())
		} else {
			// Set-based load of the explicit goat list: one round trip via ANY(), then verify every requested
			// goat was eligible by diffing the RETURNING set against the request.
			loaded := make(map[string]bool, len(in.GoatIDs))
			rows, execErr := tx.Query(ctx, `
UPDATE procurement_load_goats
SET current_state = 'in_transit',
    selection_state = 'loaded',
    loaded_at = $4::timestamptz,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND goat_id = ANY($3::uuid[])
  AND current_state = 'pre_dispatch_accepted'
  AND health_state = 'passed'
  AND source_entry_state = 'accepted'
  AND ownership_state IN ('mesha_owned', 'settled')
RETURNING goat_id::text`,
				in.TenantID, in.LoadID, in.GoatIDs, in.DispatchedAt)
			if execErr != nil {
				return domain.TransitHandoff{}, fmt.Errorf("procurement: load goats: %w", execErr)
			}
			for rows.Next() {
				var goatID string
				if scanErr := rows.Scan(&goatID); scanErr != nil {
					rows.Close()
					return domain.TransitHandoff{}, scanErr
				}
				loaded[goatID] = true
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				return domain.TransitHandoff{}, rowsErr
			}
			for _, goatID := range in.GoatIDs {
				if !loaded[goatID] {
					return domain.TransitHandoff{}, fmt.Errorf("%w: goat %s is not eligible for dispatch", ports.ErrInvalidTransition, goatID)
				}
			}
			loadedCount = len(loaded)
		}
		if loadedCount < acceptedCount {
			discrepancy = "partial_load"
		}
	}
	handoff, err := scanTransit(tx.QueryRow(ctx, `
INSERT INTO transit_handoffs (
  tenant_id, load_id, from_location_id, to_location_id, loaded_count,
  dispatched_at, arrived_at, proof_ref_id, discrepancy_state, status,
  idempotency_key, created_by
) VALUES (
  $1::uuid, $2::uuid, nullif($3::text, '')::uuid, $4::uuid, $5,
  $6::timestamptz, $7::timestamptz, nullif($8::text, '')::uuid, $9, $10,
  $11, nullif($12::text, '')::uuid
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
	RETURNING handoff_id::text, tenant_id::text, load_id::text, from_location_id::text,
	          COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.from_location_id), from_location_id::text),
	          to_location_id::text,
	          COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.to_location_id), to_location_id::text),
	          loaded_count, dispatched_at, arrived_at,
	          proof_ref_id::text, discrepancy_state, status, created_at, updated_at, row_version`,
		in.TenantID, in.LoadID, stringPtrValue(in.FromLocationID), in.ToLocationID,
		loadedCount, in.DispatchedAt, timeArg(in.ArrivedAt), stringPtrValue(in.ProofRefID),
		discrepancy, status, in.IdempotencyKey, stringPtrValue(in.ActorID)))
	if err != nil {
		return domain.TransitHandoff{}, fmt.Errorf("procurement: create transit handoff: %w", err)
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_loads
SET status = $3, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`,
		in.TenantID, in.LoadID, loadStatus); err != nil {
		return domain.TransitHandoff{}, fmt.Errorf("procurement: update dispatch status: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_transit_handoff", handoff.HandoffID); err != nil {
			return domain.TransitHandoff{}, fmt.Errorf("procurement: complete dispatch idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.TransitHandoff{}, err
	}
	return handoff, nil
}

func (r *Repository) RecordArrivalReview(ctx context.Context, in ports.ArrivalReview) (domain.ArrivalReview, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.ArrivalReview{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.arrival_review"
	itemParts := make([]string, 0, len(in.Goats))
	for _, item := range in.Goats {
		itemParts = append(itemParts, strings.Join([]string{
			arrivalItemKey(item), stringPtrValue(item.GoatID), item.ArrivalState,
			stringPtrValue(item.HealthFlag), stringPtrValue(item.WeightFlag),
			stringPtrValue(item.ProofRefID), item.Notes,
		}, "\x1e"))
	}
	sort.Strings(itemParts)
	// ReviewedAt enters the fingerprint only when client-supplied (omitted = server-defaulted to now(),
	// excluded), so a later exact replay is not misread as a different payload.
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, in.ParkLocationID,
		fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d", in.ExpectedCount, in.LoadedCount, in.ArrivedCount,
			in.MatchedCount, in.MissingCount, in.ExtraCount, in.RejectedCount),
		canonicalJSON(in.HealthFlags), canonicalJSON(in.WeightFlags), stringPtrValue(in.MediaProofID),
		in.Status, stringPtrValue(in.ReviewedBy), fpTimeIf(in.ReviewedAtSet, in.ReviewedAt),
		strings.Join(itemParts, "\x1d"),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return domain.ArrivalReview{}, rerr
		}
		if !res.proceed {
			_ = tx.Rollback(ctx)
			out, rerr := r.getArrivalReviewByID(ctx, in.TenantID, res.resultID)
			out.Replayed = true
			return out, rerr
		}
	}

	review, err := scanArrivalReview(tx.QueryRow(ctx, `
INSERT INTO arrival_intake_reviews (
  tenant_id, load_id, park_location_id, expected_count, loaded_count,
  arrived_count, matched_count, missing_count, extra_count, rejected_count,
  health_flags, weight_flags, media_proof_id, status, reviewed_by,
  reviewed_at, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5,
  $6, $7, $8, $9, $10, $11::jsonb, $12::jsonb, nullif($13::text, '')::uuid,
  $14, nullif($15::text, '')::uuid, $16::timestamptz, $17
)
ON CONFLICT (tenant_id, idempotency_key) DO UPDATE
SET idempotency_key = EXCLUDED.idempotency_key
RETURNING review_id::text, tenant_id::text, load_id::text, park_location_id::text,
          COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = arrival_intake_reviews.tenant_id AND location_id = arrival_intake_reviews.park_location_id), park_location_id::text),
          expected_count, loaded_count, arrived_count, matched_count, missing_count,
          extra_count, rejected_count, health_flags, weight_flags, media_proof_id::text,
          status, reviewed_by::text, reviewed_at, created_at, updated_at, row_version`,
		in.TenantID, in.LoadID, in.ParkLocationID, in.ExpectedCount, in.LoadedCount,
		in.ArrivedCount, in.MatchedCount, in.MissingCount, in.ExtraCount, in.RejectedCount,
		jsonArrayArg(in.HealthFlags), jsonArrayArg(in.WeightFlags), stringPtrValue(in.MediaProofID),
		in.Status, stringPtrValue(in.ReviewedBy), in.ReviewedAt, in.IdempotencyKey))
	if err != nil {
		return domain.ArrivalReview{}, fmt.Errorf("procurement: record arrival review: %w", err)
	}
	// Batch insert arrival_intake_review_goats via UNNEST
	type arrivalGoatInput struct {
		goatID          *string
		animalID2       *string
		animalID1       *string
		itemKey         string
		arrivalState    string
		healthFlag      *string
		weightFlag      *string
		proofRefID      *string
		notes           string
	}
	goatInputs := make([]arrivalGoatInput, 0, len(in.Goats))
	goatIDsForUpdate := make([]string, 0, len(in.Goats))
	for _, item := range in.Goats {
		goatInputs = append(goatInputs, arrivalGoatInput{
			goatID:       item.GoatID,
			animalID2:    item.AnimalIdentifier2,
			animalID1:    item.AnimalIdentifier1,
			itemKey:      arrivalItemKey(item),
			arrivalState: item.ArrivalState,
			healthFlag:   item.HealthFlag,
			weightFlag:   item.WeightFlag,
			proofRefID:   item.ProofRefID,
			notes:        item.Notes,
		})
		if item.GoatID != nil {
			goatIDsForUpdate = append(goatIDsForUpdate, *item.GoatID)
		}
	}

	// Build UNNEST arrays for batch insert
	goatIDs := make([]string, len(goatInputs))
	animalID2s := make([]string, len(goatInputs))
	animalID1s := make([]string, len(goatInputs))
	itemKeys := make([]string, len(goatInputs))
	arrivalStates := make([]string, len(goatInputs))
	healthFlags := make([]string, len(goatInputs))
	weightFlags := make([]string, len(goatInputs))
	proofRefIDs := make([]string, len(goatInputs))
	notesList := make([]string, len(goatInputs))

	for i, input := range goatInputs {
		goatIDs[i] = stringPtrValue(input.goatID)
		animalID2s[i] = stringPtrValue(input.animalID2)
		animalID1s[i] = stringPtrValue(input.animalID1)
		itemKeys[i] = input.itemKey
		arrivalStates[i] = input.arrivalState
		healthFlags[i] = stringPtrValue(input.healthFlag)
		weightFlags[i] = stringPtrValue(input.weightFlag)
		proofRefIDs[i] = stringPtrValue(input.proofRefID)
		notesList[i] = input.notes
	}

	// Batch insert all arrival_intake_review_goats
	arrivalGoatRows, err := tx.Query(ctx, `
INSERT INTO arrival_intake_review_goats (
  tenant_id, review_id, load_id, goat_id, animal_identifier_2, animal_identifier_1,
  item_key, arrival_state, health_flag, weight_flag, proof_ref_id, notes
)
SELECT $1::uuid, $2::uuid, $3::uuid,
       nullif(v.goat_id::text, '')::uuid,
       nullif(v.animal_id_2, ''),
       nullif(v.animal_id_1, ''),
       v.item_key, v.arrival_state,
       nullif(v.health_flag, ''),
       nullif(v.weight_flag, ''),
       nullif(v.proof_ref_id::text, '')::uuid,
       v.notes
FROM UNNEST($4::text[], $5::text[], $6::text[], $7::text[], $8::text[], $9::text[], $10::text[], $11::text[], $12::text[])
  AS v(goat_id, animal_id_2, animal_id_1, item_key, arrival_state, health_flag, weight_flag, proof_ref_id, notes)
ON CONFLICT (tenant_id, review_id, item_key) DO UPDATE
SET arrival_state = EXCLUDED.arrival_state,
    health_flag = EXCLUDED.health_flag,
    weight_flag = EXCLUDED.weight_flag,
    proof_ref_id = EXCLUDED.proof_ref_id,
    notes = EXCLUDED.notes
RETURNING review_goat_id::text, tenant_id::text, review_id::text, load_id::text,
          goat_id::text, animal_identifier_2, animal_identifier_1, arrival_state, health_flag,
          weight_flag, proof_ref_id::text, notes, created_at`,
		in.TenantID, review.ReviewID, in.LoadID, goatIDs, animalID2s, animalID1s, itemKeys, arrivalStates,
		healthFlags, weightFlags, proofRefIDs, notesList)
	if err != nil {
		return domain.ArrivalReview{}, fmt.Errorf("procurement: batch insert arrival review goats: %w", err)
	}
	defer arrivalGoatRows.Close()

	items := []domain.ArrivalGoat{}
	for arrivalGoatRows.Next() {
		item, itemErr := scanArrivalGoat(arrivalGoatRows)
		if itemErr != nil {
			return domain.ArrivalReview{}, fmt.Errorf("procurement: scan arrival review goat: %w", itemErr)
		}
		items = append(items, item)
	}
	if err := arrivalGoatRows.Err(); err != nil {
		return domain.ArrivalReview{}, err
	}

	// Batch update procurement_load_goats for eligible goats (those with goat_id)
	if len(goatIDsForUpdate) > 0 {
		// Build arrays for CASE updates
		nextStates := make([]string, len(goatIDsForUpdate))
		for i, goatID := range goatIDsForUpdate {
			// Find the corresponding ArrivalState from goatInputs
			for _, input := range goatInputs {
				if input.goatID != nil && *input.goatID == goatID {
					nextStates[i] = mapArrivalState(input.arrivalState)
					break
				}
			}
		}

		tag, err := tx.Exec(ctx, `
UPDATE procurement_load_goats plg
SET current_state = CASE
      WHEN plg.goat_id = ANY($4::uuid[]) THEN
        ($5::text[])[array_position($4::uuid[], plg.goat_id)]
      ELSE plg.current_state
    END,
    selection_state = CASE
      WHEN plg.goat_id = ANY($4::uuid[]) AND
           ($5::text[])[array_position($4::uuid[], plg.goat_id)] = 'arrival_rejected'
      THEN 'arrival_rejected'
      ELSE plg.selection_state
    END,
    arrived_at = CASE
      WHEN plg.goat_id = ANY($4::uuid[]) THEN $3::timestamptz
      ELSE plg.arrived_at
    END,
    updated_at = now(),
    row_version = plg.row_version + 1
WHERE plg.tenant_id = $1::uuid
  AND plg.load_id = $2::uuid
  AND plg.goat_id = ANY($4::uuid[])
  AND (
    ($5::text[])[array_position($4::uuid[], plg.goat_id)] <> 'arrival_accepted'
    OR (
      plg.current_state IN ('loaded', 'in_transit', 'arrival_review_pending')
      AND plg.loaded_at IS NOT NULL
      AND plg.health_state = 'passed'
      AND plg.source_entry_state = 'accepted'
      AND plg.ownership_state IN ('mesha_owned', 'settled')
    )
  )`,
			in.TenantID, in.LoadID, in.ReviewedAt,
			goatIDsForUpdate, nextStates)
		if err != nil {
			return domain.ArrivalReview{}, fmt.Errorf("procurement: batch update arrival goat states: %w", err)
		}
		if tag.RowsAffected() != int64(len(goatIDsForUpdate)) {
			return domain.ArrivalReview{}, fmt.Errorf("%w: not all goats were eligible for arrival state update", ports.ErrInvalidTransition)
		}
	}
	if in.Status == domain.DecisionRejected && len(in.Goats) == 0 {
		if _, err = tx.Exec(ctx, `
UPDATE procurement_load_goats
SET current_state = 'arrival_rejected',
    selection_state = 'arrival_rejected',
    arrived_at = $3::timestamptz,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND current_state IN ('loaded', 'in_transit', 'arrival_review_pending', 'arrival_accepted')`,
			in.TenantID, in.LoadID, in.ReviewedAt); err != nil {
			return domain.ArrivalReview{}, fmt.Errorf("procurement: reject arrived load goats: %w", err)
		}
	}
	loadStatus := domain.LoadStatusArrivalReview
	if in.Status == domain.DecisionRejected {
		loadStatus = domain.LoadStatusRejected
	}
	if in.Status == domain.DecisionBlocked {
		loadStatus = domain.LoadStatusBlocked
	}
	if in.Status == domain.DecisionDeferred {
		loadStatus = domain.LoadStatusDeferred
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_loads
SET status = $3, updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`,
		in.TenantID, in.LoadID, loadStatus); err != nil {
		return domain.ArrivalReview{}, fmt.Errorf("procurement: update arrival load status: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "arrival_intake_review", review.ReviewID); err != nil {
			return domain.ArrivalReview{}, fmt.Errorf("procurement: complete arrival review idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.ArrivalReview{}, err
	}
	review.Goats = items
	return review, nil
}

func (r *Repository) AcceptIntake(ctx context.Context, in ports.AcceptIntake) ([]domain.PCHandoff, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const scope = "procurement.accept_intake"
	fpGoats := append([]string(nil), in.GoatIDs...)
	sort.Strings(fpGoats)
	// AcceptedAt enters the fingerprint only when client-supplied (omitted = server-defaulted to now(),
	// excluded). EntryDate always stays: it is a client-meaningful date (and only date-granular), part of the
	// semantic identity.
	fingerprint := requestFingerprint(
		in.TenantID, in.LoadID, in.ParkLocationID, in.ShedLocationID,
		biztime.BusinessDate(in.EntryDate), fpTimeIf(in.AcceptedAtSet, in.AcceptedAt),
		stringPtrValue(in.IntakeHealthSignal), canonicalJSON(in.TrustedVaccinationHistory),
		strings.Join(fpGoats, ","),
	)
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		res, rerr := reserveIdempotency(ctx, tx, in.TenantID, scope, key, fingerprint)
		if rerr != nil {
			return nil, rerr
		}
		if !res.proceed {
			// Replay: the PC handoffs for this load were already produced; return them, run NO side effects.
			_ = tx.Rollback(ctx)
			return r.listPCHandoffs(ctx, in.TenantID, in.LoadID)
		}
	}

	goatIDs := append([]string(nil), in.GoatIDs...)
	if len(goatIDs) == 0 {
		rows, queryErr := tx.Query(ctx, `
SELECT goat_id::text
FROM procurement_load_goats
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND current_state = 'arrival_accepted'
  AND loaded_at IS NOT NULL
  AND arrived_at IS NOT NULL
  AND health_state = 'passed'
  AND source_entry_state = 'accepted'
  AND ownership_state IN ('mesha_owned', 'settled')
  AND EXISTS (
    SELECT 1
    FROM transit_handoffs th
    WHERE th.tenant_id = procurement_load_goats.tenant_id
      AND th.load_id = procurement_load_goats.load_id
      AND th.proof_ref_id IS NOT NULL
      AND th.status IN ('in_transit', 'arrived')
  )
ORDER BY goat_id`, in.TenantID, in.LoadID)
		if queryErr != nil {
			return nil, fmt.Errorf("procurement: list intake goats: %w", queryErr)
		}
		for rows.Next() {
			var goatID string
			if scanErr := rows.Scan(&goatID); scanErr != nil {
				rows.Close()
				return nil, scanErr
			}
			goatIDs = append(goatIDs, goatID)
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			rows.Close()
			return nil, rowsErr
		}
		rows.Close()
	}
	if len(goatIDs) == 0 {
		return nil, fmt.Errorf("%w: accepted intake requires at least one arrival-accepted goat", ports.ErrInvalidTransition)
	}

	// Batch update procurement_load_goats for all eligible goats
	updateGoatTag, err := tx.Exec(ctx, `
UPDATE procurement_load_goats
SET current_state = 'accepted_herd_intake',
    selection_state = 'accepted_herd_intake',
    intake_accepted_at = $4::timestamptz,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND goat_id = ANY($3::uuid[])
  AND current_state = 'arrival_accepted'
  AND loaded_at IS NOT NULL
  AND arrived_at IS NOT NULL
  AND health_state = 'passed'
  AND source_entry_state = 'accepted'
  AND ownership_state IN ('mesha_owned', 'settled')
  AND EXISTS (
    SELECT 1
    FROM transit_handoffs th
    WHERE th.tenant_id = procurement_load_goats.tenant_id
      AND th.load_id = procurement_load_goats.load_id
      AND th.proof_ref_id IS NOT NULL
      AND th.status IN ('in_transit', 'arrived')
  )`,
		in.TenantID, in.LoadID, goatIDs, in.AcceptedAt)
	if err != nil {
		return nil, fmt.Errorf("procurement: batch update procurement_load_goats for accepted intake: %w", err)
	}
	if updateGoatTag.RowsAffected() != int64(len(goatIDs)) {
		return nil, fmt.Errorf("%w: not all goats were eligible for accepted intake", ports.ErrInvalidTransition)
	}

	// Batch update goats table for all accepted goats
	_, err = tx.Exec(ctx, `
UPDATE goats
SET lifecycle_status = 'alive',
    origin_type = 'procured',
    entry_date = $5::date,
    current_location_id = $4::uuid,
    park_id = $3::uuid,
    shed_id = $4::uuid,
    sex = COALESCE(NULLIF(plg.metadata ->> 'sex', ''), goats.sex),
    dob = COALESCE(
      CASE
        WHEN COALESCE(plg.metadata ->> 'dob', '') ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'
        THEN (plg.metadata ->> 'dob')::date
        ELSE NULL
      END,
      goats.dob
    ),
    dob_estimated = COALESCE(
      CASE
        WHEN lower(COALESCE(plg.metadata ->> 'dob_estimated', '')) IN ('true', 'false')
        THEN (plg.metadata ->> 'dob_estimated')::boolean
        ELSE NULL
      END,
      goats.dob_estimated
    ),
    management_stage = COALESCE(NULLIF(plg.metadata ->> 'management_stage', ''), goats.management_stage),
    health_status = CASE
      WHEN plg.health_state = 'passed' THEN 'healthy'
      ELSE COALESCE(NULLIF($6, ''), goats.health_status)
    END,
    updated_at = now(),
    row_version = goats.row_version + 1
FROM procurement_load_goats plg
WHERE goats.tenant_id = $1::uuid
  AND goats.goat_id = ANY($2::uuid[])
  AND plg.tenant_id = goats.tenant_id
  AND plg.load_id = $7::uuid
  AND plg.goat_id = goats.goat_id`,
		in.TenantID, goatIDs, in.ParkLocationID, in.ShedLocationID, in.EntryDate, stringPtrValue(in.IntakeHealthSignal), in.LoadID)
	if err != nil {
		return nil, fmt.Errorf("procurement: batch update goats for accepted intake: %w", err)
	}

	// Batch insert into goat_location_history for all accepted goats
	_, err = tx.Exec(ctx, `
INSERT INTO goat_location_history (
  tenant_id, goat_id, to_location_id, reason, occurred_at, actor_id, source_record_id
)
SELECT $1::uuid, v.goat_id::uuid, $2::uuid, 'procurement_accepted_intake', $3::timestamptz,
       nullif($4::text, '')::uuid, concat('procurement_load:', $5::text)
FROM UNNEST($6::text[]) v(goat_id)`,
		in.TenantID, in.ShedLocationID, in.AcceptedAt, stringPtrValue(in.ActorID), in.LoadID, goatIDs)
	if err != nil {
		return nil, fmt.Errorf("procurement: batch insert goat_location_history: %w", err)
	}

	// Batch insert procurement_pc_handoffs for all accepted goats
	out := make([]domain.PCHandoff, 0, len(goatIDs))
	handoffRows, err := tx.Query(ctx, `
INSERT INTO procurement_pc_handoffs (
  tenant_id, load_id, goat_id, accepted_at, park_location_id, shed_location_id,
  entry_date, trusted_vaccination_history, intake_health_signal, idempotency_key
)
SELECT $1::uuid, $2::uuid, v.goat_id::uuid, $3::timestamptz, $4::uuid, $5::uuid,
       $6::date, $7::jsonb, nullif($8::text, ''), concat($9::text, ':', v.goat_id)
FROM UNNEST($10::text[]) v(goat_id)
ON CONFLICT (tenant_id, load_id, goat_id) DO UPDATE
SET accepted_at = EXCLUDED.accepted_at,
    park_location_id = EXCLUDED.park_location_id,
    shed_location_id = EXCLUDED.shed_location_id,
    entry_date = EXCLUDED.entry_date,
    trusted_vaccination_history = EXCLUDED.trusted_vaccination_history,
    intake_health_signal = EXCLUDED.intake_health_signal,
    updated_at = now()
RETURNING handoff_id::text, tenant_id::text, load_id::text, goat_id::text,
          accepted_at, park_location_id::text,
          COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = procurement_pc_handoffs.tenant_id AND location_id = procurement_pc_handoffs.park_location_id), park_location_id::text),
          shed_location_id::text,
          COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = procurement_pc_handoffs.tenant_id AND location_id = procurement_pc_handoffs.shed_location_id), shed_location_id::text),
          entry_date,
          trusted_vaccination_history, intake_health_signal, event_status, created_at, updated_at`,
		in.TenantID, in.LoadID, in.AcceptedAt, in.ParkLocationID, in.ShedLocationID,
		in.EntryDate, jsonArrayArg(in.TrustedVaccinationHistory), stringPtrValue(in.IntakeHealthSignal),
		in.IdempotencyKey, goatIDs)
	if err != nil {
		return nil, fmt.Errorf("procurement: batch insert procurement_pc_handoffs: %w", err)
	}
	defer handoffRows.Close()

	for handoffRows.Next() {
		handoff, handoffErr := scanPCHandoff(handoffRows)
		if handoffErr != nil {
			return nil, fmt.Errorf("procurement: scan PC handoff: %w", handoffErr)
		}
		out = append(out, handoff)
	}
	if err := handoffRows.Err(); err != nil {
		return nil, err
	}
	// Close the RETURNING cursor before issuing further statements on tx: pgx cannot
	// run a query while another result set is still open on the same connection.
	handoffRows.Close()
	// Emit goat.created per accepted handoff now that the cursor is closed.
	for i := range out {
		if err := r.emitAcceptedIntakeGoatCreated(ctx, tx, in, out[i]); err != nil {
			return nil, err
		}
		out[i].EventStatus = "emitted"
	}
	if _, err = tx.Exec(ctx, `
UPDATE procurement_loads
SET status = 'accepted_intake', updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1::uuid AND load_id = $2::uuid`,
		in.TenantID, in.LoadID); err != nil {
		return nil, fmt.Errorf("procurement: update accepted intake load: %w", err)
	}
	if key := strings.TrimSpace(in.IdempotencyKey); key != "" {
		if err = completeIdempotency(ctx, tx, in.TenantID, scope, key, "procurement_pc_handoffs_load", in.LoadID); err != nil {
			return nil, fmt.Errorf("procurement: complete accept intake idempotency: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repository) ListWorkRows(ctx context.Context, q domain.WorkQuery) (domain.WorkListResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	fetchLimit := limit + 1
	cursorUpdated := pgtype.Timestamptz{}
	cursorRowID := ""
	if q.Cursor != nil {
		cursorUpdated = pgtype.Timestamptz{Time: q.Cursor.UpdatedAt, Valid: true}
		cursorRowID = q.Cursor.RowID
	}
	rowID := stringPtrValue(q.RowID)
	workState := stringPtrValue(q.WorkState)
	severity := stringPtrValue(q.Severity)

	rows, err := r.pool.Query(ctx, procurementWorkRowsSQL, q.TenantID, workState, severity, rowID, cursorUpdated, cursorRowID, fetchLimit, q.ExceptionOnly)
	if err != nil {
		return domain.WorkListResult{}, fmt.Errorf("procurement: list work rows: %w", err)
	}
	defer rows.Close()
	out := []domain.WorkRow{}
	for rows.Next() {
		row, scanErr := scanWorkRow(rows)
		if scanErr != nil {
			return domain.WorkListResult{}, scanErr
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return domain.WorkListResult{}, err
	}

	countRows, err := r.pool.Query(ctx, procurementWorkCountsSQL, q.TenantID, workState, severity, rowID, q.ExceptionOnly)
	if err != nil {
		return domain.WorkListResult{}, fmt.Errorf("procurement: count work rows: %w", err)
	}
	defer countRows.Close()
	counts := []domain.CountByWorkState{}
	for countRows.Next() {
		var state string
		var count int64
		if err := countRows.Scan(&state, &count); err != nil {
			return domain.WorkListResult{}, err
		}
		counts = append(counts, domain.CountByWorkState{WorkState: state, Count: count})
	}
	if err := countRows.Err(); err != nil {
		return domain.WorkListResult{}, err
	}

	var next *string
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		cursor, err := domain.EncodeWorkCursor(domain.WorkCursor{UpdatedAt: last.UpdatedAt, RowID: last.RowID})
		if err != nil {
			return domain.WorkListResult{}, err
		}
		next = &cursor
	}
	return domain.WorkListResult{Rows: out, CountsByWorkState: counts, NextCursor: next}, nil
}

func (r *Repository) GetWorkRow(ctx context.Context, q domain.WorkQuery, rowID string) (domain.WorkRow, bool, error) {
	q.RowID = &rowID
	q.Limit = 1
	result, err := r.ListWorkRows(ctx, q)
	if err != nil {
		return domain.WorkRow{}, false, err
	}
	if len(result.Rows) == 0 {
		return domain.WorkRow{}, false, nil
	}
	return result.Rows[0], true, nil
}

func (r *Repository) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, r.timeout)
}

func lookupAnimalIdentifierOwner(ctx context.Context, tx pgx.Tx, tenantID string, value *string) (*string, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	var goatID string
	err := tx.QueryRow(ctx, `
SELECT goat_id::text
FROM goat_identifiers
WHERE tenant_id = $1::uuid
  AND normalized_value = $2
LIMIT 1`, tenantID, normalizeIdentifier(*value)).Scan(&goatID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("procurement: lookup animal identifier: %w", err)
	}
	return &goatID, nil
}

func ensureExistingGoatSpeciesAndSex(ctx context.Context, tx pgx.Tx, tenantID, goatID, requestedSpecies, requestedSex string) error {
	var canonicalSpecies, canonicalSex string
	err := tx.QueryRow(ctx, `
SELECT species, sex
FROM goats
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid`, tenantID, goatID).Scan(&canonicalSpecies, &canonicalSex)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrInvalidReference
	}
	if err != nil {
		return fmt.Errorf("procurement: lookup existing goat species/sex: %w", err)
	}
	if canonicalSpecies != requestedSpecies {
		return fmt.Errorf("%w: goat %s species is %s, request was %s", ports.ErrInvalidReference, goatID, canonicalSpecies, requestedSpecies)
	}
	if canonicalSex != requestedSex {
		return fmt.Errorf("%w: goat %s is %s, request was %s", ports.ErrSexMismatch, goatID, canonicalSex, requestedSex)
	}
	return nil
}

func insertAnimalIdentifier(ctx context.Context, tx pgx.Tx, tenantID, goatID, identifierType string, value *string, primary bool) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	if ownerID, err := lookupAnimalIdentifierOwner(ctx, tx, tenantID, value); err != nil {
		return err
	} else if ownerID != nil {
		if *ownerID == goatID {
			return nil
		}
		return fmt.Errorf("%w: animal identifier already belongs to animal %s", ports.ErrInvalidTransition, *ownerID)
	}
	_, err := tx.Exec(ctx, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, source_system,
  source_record_id, normalizer_version
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, 'global', $6, 'active', now(),
  'procurement_source_entry', $7, 'procurement-v1'
)`, tenantID, goatID, identifierType, strings.TrimSpace(*value), normalizeIdentifier(*value), primary, "animal:"+goatID)
	if err != nil {
		if isUniqueViolation(err) {
			return ports.ErrWriteConflict
		}
		return fmt.Errorf("procurement: insert animal identifier: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func countPreDispatchAccepted(ctx context.Context, tx pgx.Tx, tenantID, loadID string) (int, error) {
	var count int
	if err := tx.QueryRow(ctx, `
SELECT count(*)::int
FROM procurement_load_goats
WHERE tenant_id = $1::uuid
  AND load_id = $2::uuid
  AND current_state = 'pre_dispatch_accepted'
  AND health_state = 'passed'
  AND source_entry_state = 'accepted'
  AND ownership_state IN ('mesha_owned', 'settled')`,
		tenantID, loadID).Scan(&count); err != nil {
		return 0, fmt.Errorf("procurement: count pre-dispatch accepted goats: %w", err)
	}
	return count, nil
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLoad(row scanner) (domain.Load, error) {
	var out domain.Load
	var sourcePartyName, sourceLocation, sourceLocationCode, sourceLocationName pgtype.Text
	var purchaseDate pgtype.Date
	var planned pgtype.Timestamptz
	var contextBytes []byte
	if err := row.Scan(&out.LoadID, &out.TenantID, &out.SourcePartyID, &sourcePartyName,
		&sourceLocation, &sourceLocationCode, &sourceLocationName,
		&out.ExpectedCount, &purchaseDate, &planned, &out.Status, &out.Notes, &contextBytes,
		&out.CreatedAt, &out.UpdatedAt, &out.RowVersion); err != nil {
		return domain.Load{}, err
	}
	out.SourcePartyName = textValue(sourcePartyName)
	out.SourceLocationID = textPtr(sourceLocation)
	out.SourceLocationCode = textPtr(sourceLocationCode)
	out.SourceLocationName = textPtr(sourceLocationName)
	out.PurchaseDate = datePtr(purchaseDate)
	out.PlannedDispatch = timePtr(planned)
	out.Context = rawJSON(contextBytes, `{}`)
	return out, nil
}

func scanLoadGoat(row scanner) (domain.LoadGoat, error) {
	var out domain.LoadGoat
	var animalIdentifier1, animalIdentifier2, sourceEntryRef, holdingID, exitReason pgtype.Text
	var warmupStart, warmupEnd, loadedAt, arrivedAt, intakeAt pgtype.Timestamptz
	var warmupDays pgtype.Int4
	var proofRefs, metadata []byte
	if err := row.Scan(&out.LoadGoatID, &out.TenantID, &out.LoadID, &out.GoatID,
		&animalIdentifier1, &animalIdentifier2, &out.SelectionState, &out.SelectionReason,
		&out.Purpose, &out.CurrentState, &out.SourceEntryState, &sourceEntryRef, &out.OwnershipState,
		&out.HealthState, &warmupStart, &warmupEnd, &warmupDays, &holdingID, &loadedAt,
		&arrivedAt, &intakeAt, &exitReason, &proofRefs, &metadata, &out.CreatedAt,
		&out.UpdatedAt, &out.RowVersion); err != nil {
		return domain.LoadGoat{}, err
	}
	out.AnimalIdentifier1 = textPtr(animalIdentifier1)
	out.AnimalIdentifier2 = textPtr(animalIdentifier2)
	out.SourceEntryRef = textPtr(sourceEntryRef)
	out.WarmupStartedAt = timePtr(warmupStart)
	out.WarmupEndedAt = timePtr(warmupEnd)
	out.WarmupDays = intPtr(warmupDays)
	out.HoldingLocationID = textPtr(holdingID)
	out.LoadedAt = timePtr(loadedAt)
	out.ArrivedAt = timePtr(arrivedAt)
	out.IntakeAcceptedAt = timePtr(intakeAt)
	out.ExitReason = textPtr(exitReason)
	out.ProofRefs = rawJSON(proofRefs, `[]`)
	out.Metadata = rawJSON(metadata, `{}`)
	return out, nil
}

func scanHoldingStay(row scanner) (domain.HoldingStay, error) {
	var out domain.HoldingStay
	var ended pgtype.Timestamptz
	var days pgtype.Int4
	var proofRefs []byte
	if err := row.Scan(&out.StayID, &out.TenantID, &out.GoatID, &out.LoadID,
		&out.HoldingLocationID, &out.StartedAt, &ended, &out.WarmupState, &days,
		&out.Purpose, &out.HealthState, &out.OwnershipState, &out.Status, &proofRefs, &out.CreatedAt,
		&out.UpdatedAt, &out.RowVersion); err != nil {
		return domain.HoldingStay{}, err
	}
	out.EndedAt = timePtr(ended)
	out.WarmupDays = intPtr(days)
	out.ProofRefs = rawJSON(proofRefs, `[]`)
	return out, nil
}

func scanHFVaccinationEvidence(row scanner) (domain.HFVaccinationEvidence, error) {
	var out domain.HFVaccinationEvidence
	var proofRef, reviewedBy, importedBy pgtype.Text
	var reviewedAt pgtype.Timestamptz
	var metadata []byte
	if err := row.Scan(
		&out.EvidenceID,
		&out.TenantID,
		&out.LoadID,
		&out.GoatID,
		&out.ProtocolVersionID,
		&out.RuleID,
		&out.DoseCode,
		&out.AdministeredAt,
		&out.VaccineName,
		&out.LotNumber,
		&proofRef,
		&out.SourceRef,
		&out.ReviewStatus,
		&reviewedBy,
		&reviewedAt,
		&out.ReviewReason,
		&importedBy,
		&out.ImportedAt,
		&metadata,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.RowVersion,
	); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	out.ProofRefID = textPtr(proofRef)
	out.ReviewedBy = textPtr(reviewedBy)
	out.ReviewedAt = timePtr(reviewedAt)
	out.ImportedBy = textPtr(importedBy)
	out.Metadata = rawJSON(metadata, `{}`)
	return out, nil
}

func trustedProcurementHoldingEvidenceContext(ctx context.Context, tx pgx.Tx, tenantID, evidenceID string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM procurement_hf_vaccination_evidence ev
  JOIN procurement_load_goats plg
    ON plg.tenant_id = ev.tenant_id
   AND plg.load_id = ev.load_id
   AND plg.goat_id = ev.goat_id
  JOIN proof_artifacts proof
    ON proof.proof_id = ev.proof_ref_id
   AND proof.tenant_id = ev.tenant_id
   AND proof.upload_state = 'completed'
  WHERE ev.tenant_id = $1::uuid
    AND ev.evidence_id = $2::uuid
    AND ev.proof_ref_id IS NOT NULL
    AND plg.holding_location_id IS NOT NULL
    AND plg.warmup_started_at IS NOT NULL
    AND COALESCE(
      plg.warmup_days,
      floor(extract(epoch FROM (COALESCE(plg.warmup_ended_at, ev.administered_at) - plg.warmup_started_at)) / 86400)::int
    ) BETWEEN 28 AND 35
    AND ev.administered_at >= plg.warmup_started_at
    AND (plg.warmup_ended_at IS NULL OR ev.administered_at <= plg.warmup_ended_at)
)`, tenantID, evidenceID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("procurement: validate trusted holding vaccination evidence: %w", err)
	}
	return ok, nil
}

func scanHealthCheck(row scanner) (domain.SourceHealthCheck, error) {
	var out domain.SourceHealthCheck
	var checkedBy, proofRefID, taskID pgtype.Text
	if err := row.Scan(&out.HealthCheckID, &out.TenantID, &out.GoatID, &out.LoadID,
		&out.HealthState, &out.Reason, &checkedBy, &out.CheckedAt, &proofRefID,
		&taskID, &out.CreatedAt); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	out.CheckedBy = textPtr(checkedBy)
	out.ProofRefID = textPtr(proofRefID)
	out.SOPTaskID = textPtr(taskID)
	return out, nil
}

func scanDecision(row scanner) (domain.Decision, error) {
	var out domain.Decision
	var decidedBy, proofRefID, taskID, ownerID, resume pgtype.Text
	var metadata []byte
	if err := row.Scan(&out.DecisionID, &out.TenantID, &out.GoatID, &out.LoadID,
		&out.DecisionStage, &out.DecisionType, &out.Reason, &decidedBy, &out.DecidedAt,
		&proofRefID, &taskID, &ownerID, &resume, &metadata, &out.CreatedAt); err != nil {
		return domain.Decision{}, err
	}
	out.DecidedBy = textPtr(decidedBy)
	out.ProofRefID = textPtr(proofRefID)
	out.SOPTaskID = textPtr(taskID)
	out.OwnerID = textPtr(ownerID)
	out.ResumeCondition = textPtr(resume)
	out.Metadata = rawJSON(metadata, `{}`)
	return out, nil
}

func scanTransit(row scanner) (domain.TransitHandoff, error) {
	var out domain.TransitHandoff
	var from, fromLabel, toLabel, proof pgtype.Text
	var arrivedAt pgtype.Timestamptz
	if err := row.Scan(&out.HandoffID, &out.TenantID, &out.LoadID, &from, &fromLabel, &out.ToLocationID, &toLabel,
		&out.LoadedCount, &out.DispatchedAt, &arrivedAt, &proof, &out.DiscrepancyState,
		&out.Status, &out.CreatedAt, &out.UpdatedAt, &out.RowVersion); err != nil {
		return domain.TransitHandoff{}, err
	}
	out.FromLocationID = textPtr(from)
	out.FromLocationLabel = textValue(fromLabel)
	out.ToLocationLabel = textValue(toLabel)
	out.ArrivedAt = timePtr(arrivedAt)
	out.ProofRefID = textPtr(proof)
	return out, nil
}

// getTransitHandoffByID re-reads a previously created transit handoff for an idempotent DispatchLoad replay.
func (r *Repository) getTransitHandoffByID(ctx context.Context, tenantID, handoffID string) (domain.TransitHandoff, error) {
	return scanTransit(r.pool.QueryRow(ctx, `
	SELECT handoff_id::text, tenant_id::text, load_id::text, from_location_id::text,
	       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.from_location_id), from_location_id::text),
	       to_location_id::text,
	       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.to_location_id), to_location_id::text),
	       loaded_count, dispatched_at, arrived_at, proof_ref_id::text,
	       discrepancy_state, status, created_at, updated_at, row_version
	FROM transit_handoffs
	WHERE tenant_id = $1::uuid AND handoff_id = nullif($2::text, '')::uuid`, tenantID, handoffID))
}

func scanArrivalReview(row scanner) (domain.ArrivalReview, error) {
	var out domain.ArrivalReview
	var parkLabel, mediaProof, reviewedBy pgtype.Text
	var healthFlags, weightFlags []byte
	if err := row.Scan(&out.ReviewID, &out.TenantID, &out.LoadID, &out.ParkLocationID, &parkLabel,
		&out.ExpectedCount, &out.LoadedCount, &out.ArrivedCount, &out.MatchedCount,
		&out.MissingCount, &out.ExtraCount, &out.RejectedCount, &healthFlags, &weightFlags,
		&mediaProof, &out.Status, &reviewedBy, &out.ReviewedAt, &out.CreatedAt,
		&out.UpdatedAt, &out.RowVersion); err != nil {
		return domain.ArrivalReview{}, err
	}
	out.HealthFlags = rawJSON(healthFlags, `[]`)
	out.WeightFlags = rawJSON(weightFlags, `[]`)
	out.ParkLocationLabel = textValue(parkLabel)
	out.MediaProofID = textPtr(mediaProof)
	out.ReviewedBy = textPtr(reviewedBy)
	return out, nil
}

func scanArrivalGoat(row scanner) (domain.ArrivalGoat, error) {
	var out domain.ArrivalGoat
	var goatID, tempID, sourceTag, healthFlag, weightFlag, proofRef pgtype.Text
	if err := row.Scan(&out.ReviewGoatID, &out.TenantID, &out.ReviewID, &out.LoadID,
		&goatID, &tempID, &sourceTag, &out.ArrivalState, &healthFlag, &weightFlag,
		&proofRef, &out.Notes, &out.CreatedAt); err != nil {
		return domain.ArrivalGoat{}, err
	}
	out.GoatID = textPtr(goatID)
	out.AnimalIdentifier1 = textPtr(sourceTag)
	out.AnimalIdentifier2 = textPtr(tempID)
	out.HealthFlag = textPtr(healthFlag)
	out.WeightFlag = textPtr(weightFlag)
	out.ProofRefID = textPtr(proofRef)
	return out, nil
}

func scanPCHandoff(row scanner) (domain.PCHandoff, error) {
	var out domain.PCHandoff
	var history []byte
	var parkLabel, shedLabel, signal pgtype.Text
	var entryDate pgtype.Date
	if err := row.Scan(&out.HandoffID, &out.TenantID, &out.LoadID, &out.GoatID,
		&out.AcceptedAt, &out.ParkLocationID, &parkLabel, &out.ShedLocationID, &shedLabel, &entryDate,
		&history, &signal, &out.EventStatus, &out.CreatedAt, &out.UpdatedAt); err != nil {
		return domain.PCHandoff{}, err
	}
	if entryDate.Valid {
		out.EntryDate = entryDate.Time
	}
	out.ParkLocationLabel = textValue(parkLabel)
	out.ShedLocationLabel = textValue(shedLabel)
	out.TrustedVaccinationHistory = rawJSON(history, `[]`)
	out.IntakeHealthSignal = textPtr(signal)
	return out, nil
}

func scanWorkRow(row scanner) (domain.WorkRow, error) {
	var out domain.WorkRow
	var loadGoatID, goatID, ownerID, blocker pgtype.Text
	var dueAt pgtype.Timestamptz
	var warmupDays pgtype.Int4
	if err := row.Scan(
		&out.RowID,
		&out.LoadID,
		&out.LoadStatus,
		&loadGoatID,
		&goatID,
		&out.SourcePartyID,
		&out.WorkType,
		&out.WorkState,
		&out.Severity,
		&out.Title,
		&out.Detail,
		&out.NextAction,
		&blocker,
		&ownerID,
		&dueAt,
		&out.UpdatedAt,
		&warmupDays,
		&out.ExpectedCount,
		&out.CompletedCount,
	); err != nil {
		return domain.WorkRow{}, fmt.Errorf("procurement: scan work row: %w", err)
	}
	out.LoadGoatID = textPtr(loadGoatID)
	out.GoatID = textPtr(goatID)
	out.OwnerID = textPtr(ownerID)
	out.BlockerReason = textPtr(blocker)
	out.DueAt = timePtr(dueAt)
	out.WarmupDays = intPtr(warmupDays)
	return out, nil
}

const procurementWorkBaseSQL = `
WITH goat_rows AS (
  SELECT
    'load_goat:' || plg.load_goat_id::text AS row_id,
    pl.load_id::text AS load_id,
    pl.status AS load_status,
    plg.load_goat_id::text AS load_goat_id,
    plg.goat_id::text AS goat_id,
    pl.source_party_id::text AS source_party_id,
    plg.updated_at,
    plg.warmup_started_at,
    plg.warmup_days,
    1 AS expected_count,
    CASE
      WHEN plg.current_state = 'accepted_herd_intake' THEN 'accepted_intake'
      WHEN plg.source_entry_state = 'blocked' THEN 'source_entry'
      WHEN plg.ownership_state IN ('pending', 'shared_pending', 'blocked', 'not_owned') THEN 'ownership'
      WHEN plg.health_state IN ('pending', 'failed', 'deferred') THEN 'source_health'
      WHEN plg.current_state IN ('pre_dispatch_rejected', 'arrival_rejected', 'source_rejected', 'dead', 'sold', 'lost') THEN 'rejected_review'
      WHEN plg.current_state IN ('pre_dispatch_deferred') THEN 'pre_dispatch'
      WHEN plg.current_state IN ('pre_dispatch_blocked') THEN 'pre_dispatch'
      WHEN plg.current_state = 'pre_dispatch_accepted' THEN 'dispatch_proof'
      WHEN plg.current_state IN ('loaded', 'in_transit') THEN 'arrival_gate'
      WHEN plg.current_state = 'arrival_review_pending' THEN 'arrival_mismatch'
      WHEN plg.current_state = 'arrival_accepted' THEN 'accepted_intake'
      ELSE 'source_entry'
    END AS work_type,
    CASE
      WHEN plg.current_state = 'accepted_herd_intake' THEN 'completed'
      WHEN plg.current_state IN ('pre_dispatch_rejected', 'arrival_rejected', 'source_rejected', 'dead', 'sold', 'lost') OR plg.health_state = 'failed' THEN 'rejected'
      WHEN plg.source_entry_state = 'blocked' OR plg.ownership_state IN ('pending', 'shared_pending', 'blocked', 'not_owned') OR plg.current_state IN ('pre_dispatch_blocked', 'arrival_review_pending') THEN 'blocked'
      WHEN plg.current_state IN ('pre_dispatch_deferred') OR plg.health_state = 'deferred' THEN 'deferred'
      WHEN plg.current_state = 'pre_dispatch_accepted' THEN 'proof_pending'
	      WHEN plg.warmup_started_at IS NOT NULL
	       AND (
	         COALESCE(plg.warmup_days, floor(extract(epoch FROM (now() - plg.warmup_started_at)) / 86400)::int) > 35
	         OR (plg.warmup_ended_at IS NOT NULL AND COALESCE(plg.warmup_days, floor(extract(epoch FROM (plg.warmup_ended_at - plg.warmup_started_at)) / 86400)::int) < 28)
	       ) THEN 'overdue'
      ELSE 'due'
    END AS work_state,
    CASE
      WHEN plg.source_entry_state = 'blocked' OR plg.ownership_state IN ('blocked', 'not_owned') THEN 'critical'
      WHEN plg.current_state IN ('pre_dispatch_rejected', 'arrival_rejected', 'source_rejected', 'dead', 'sold', 'lost') OR plg.health_state = 'failed' THEN 'critical'
      WHEN plg.current_state = 'arrival_review_pending' THEN 'at_risk'
	      WHEN plg.warmup_started_at IS NOT NULL
	       AND (
	         COALESCE(plg.warmup_days, floor(extract(epoch FROM (now() - plg.warmup_started_at)) / 86400)::int) > 35
	         OR (plg.warmup_ended_at IS NOT NULL AND COALESCE(plg.warmup_days, floor(extract(epoch FROM (plg.warmup_ended_at - plg.warmup_started_at)) / 86400)::int) < 28)
	       ) THEN 'at_risk'
      WHEN plg.current_state = 'accepted_herd_intake' THEN 'ok'
      ELSE 'watch'
    END AS severity,
    CASE
      WHEN plg.current_state = 'accepted_herd_intake' THEN 'Accepted intake complete'
      WHEN plg.source_entry_state = 'blocked' THEN 'Fix source-entry block'
      WHEN plg.ownership_state IN ('pending', 'shared_pending', 'blocked', 'not_owned') THEN 'Resolve source ownership'
      WHEN plg.health_state IN ('pending', 'failed', 'deferred') THEN 'Complete source health SOP'
      WHEN plg.current_state = 'pre_dispatch_accepted' THEN 'Capture truck loading proof'
      WHEN plg.current_state IN ('loaded', 'in_transit') THEN 'Complete arrival gate'
      WHEN plg.current_state = 'arrival_review_pending' THEN 'Resolve arrival mismatch'
      WHEN plg.current_state = 'arrival_accepted' THEN 'Accept herd intake'
      ELSE 'Review source-entry state'
    END AS title,
    concat_ws(' / ', plg.current_state, 'purpose=' || plg.purpose, 'health=' || plg.health_state, 'source_entry=' || plg.source_entry_state, 'ownership=' || plg.ownership_state) AS detail,
    CASE
      WHEN plg.current_state = 'accepted_herd_intake' THEN 'No action'
      WHEN plg.source_entry_state = 'blocked' THEN 'Fix source-entry block before dispatch'
      WHEN plg.ownership_state IN ('pending', 'shared_pending', 'blocked', 'not_owned') THEN 'Resolve ownership before dispatch'
      WHEN plg.health_state IN ('pending', 'deferred') THEN 'Run or review source health SOP'
      WHEN plg.health_state = 'failed' THEN 'Reject or quarantine from procurement'
      WHEN plg.current_state = 'pre_dispatch_accepted' THEN 'Attach truck loading proof'
      WHEN plg.current_state IN ('loaded', 'in_transit') THEN 'Record park arrival gate'
      WHEN plg.current_state = 'arrival_review_pending' THEN 'Reconcile missing, extra, health, or weight discrepancy'
      WHEN plg.current_state = 'arrival_accepted' THEN 'Create accepted intake handoff'
      ELSE 'Review procurement source-entry row'
    END AS next_action,
    CASE
      WHEN plg.source_entry_state <> 'accepted' THEN plg.source_entry_state
      WHEN plg.ownership_state NOT IN ('mesha_owned', 'settled') THEN plg.ownership_state
      WHEN plg.health_state <> 'passed' THEN plg.health_state
      WHEN plg.current_state = 'arrival_review_pending' THEN 'arrival_mismatch'
      ELSE NULL
    END AS blocker_reason,
    NULL::text AS owner_id,
    COALESCE(plg.warmup_ended_at, pl.planned_dispatch_at, plg.updated_at + interval '1 day') AS due_at,
    CASE WHEN plg.current_state = 'accepted_herd_intake' THEN 1 ELSE 0 END AS completed_count
  FROM procurement_load_goats plg
  JOIN procurement_loads pl
    ON pl.tenant_id = plg.tenant_id
   AND pl.load_id = plg.load_id
  WHERE plg.tenant_id = $1::uuid
), load_rows AS (
  SELECT
    'load:' || pl.load_id::text AS row_id,
    pl.load_id::text AS load_id,
    pl.status AS load_status,
    NULL::text AS load_goat_id,
    NULL::text AS goat_id,
    pl.source_party_id::text AS source_party_id,
    pl.updated_at,
    NULL::timestamptz AS warmup_started_at,
    NULL::int AS warmup_days,
    pl.expected_count,
    'load_setup' AS work_type,
    CASE WHEN pl.expected_count = 0 THEN 'due' ELSE 'blocked' END AS work_state,
    CASE WHEN pl.expected_count = 0 THEN 'watch' ELSE 'at_risk' END AS severity,
    'Add source animals to load' AS title,
    'load has no animal rows' AS detail,
    'Tag source-only or candidate animals for this procurement load' AS next_action,
    'missing_source_animals' AS blocker_reason,
    NULL::text AS owner_id,
    COALESCE(pl.planned_dispatch_at, pl.updated_at + interval '1 day') AS due_at,
    0 AS completed_count
  FROM procurement_loads pl
  WHERE pl.tenant_id = $1::uuid
    AND NOT EXISTS (
      SELECT 1
      FROM procurement_load_goats plg
      WHERE plg.tenant_id = pl.tenant_id
        AND plg.load_id = pl.load_id
    )
), work_rows AS (
  SELECT * FROM goat_rows
  UNION ALL
  SELECT * FROM load_rows
)`

const procurementWorkRowsSQL = procurementWorkBaseSQL + `
SELECT row_id, load_id, load_status, load_goat_id, goat_id, source_party_id,
       work_type, work_state, severity, title, detail, next_action,
       blocker_reason, owner_id, due_at, updated_at, warmup_days,
       expected_count, completed_count
FROM work_rows
WHERE ($2::text = '' OR work_state = $2::text)
  AND ($3::text = '' OR severity = $3::text)
  AND ($4::text = '' OR row_id = $4::text)
  AND (
    NOT $8::boolean
    OR work_state IN ('blocked', 'overdue')
    OR (work_state = 'proof_pending' AND work_type = 'dispatch_proof')
    OR (work_state IN ('rejected', 'deferred') AND severity IN ('at_risk', 'critical', 'broken'))
  )
  AND (
    $5::timestamptz IS NULL
    OR (updated_at, row_id) < ($5::timestamptz, $6::text)
  )
ORDER BY updated_at DESC, row_id DESC
LIMIT $7`

const procurementWorkCountsSQL = procurementWorkBaseSQL + `
SELECT work_state, count(*)::bigint
FROM work_rows
WHERE ($2::text = '' OR work_state = $2::text)
  AND ($3::text = '' OR severity = $3::text)
  AND ($4::text = '' OR row_id = $4::text)
  AND (
    NOT $5::boolean
    OR work_state IN ('blocked', 'overdue')
    OR (work_state = 'proof_pending' AND work_type = 'dispatch_proof')
    OR (work_state IN ('rejected', 'deferred') AND severity IN ('at_risk', 'critical', 'broken'))
  )
GROUP BY work_state
ORDER BY work_state`

func (r *Repository) listLoadGoats(ctx context.Context, tenantID, loadID string) ([]domain.LoadGoat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT load_goat_id::text, tenant_id::text, load_id::text, goat_id::text,
       animal_identifier_1, animal_identifier_2, selection_state, selection_reason, purpose,
       current_state, source_entry_state, source_entry_ref, ownership_state,
       health_state, warmup_started_at, warmup_ended_at, warmup_days,
       holding_location_id::text, loaded_at, arrived_at, intake_accepted_at,
       exit_reason, proof_refs, metadata, created_at, updated_at, row_version
FROM procurement_load_goats
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY created_at ASC, goat_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LoadGoat
	for rows.Next() {
		row, scanErr := scanLoadGoat(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listHoldingStays(ctx context.Context, tenantID, loadID string) ([]domain.HoldingStay, error) {
	rows, err := r.pool.Query(ctx, `
SELECT stay_id::text, tenant_id::text, goat_id::text, load_id::text, holding_location_id::text,
       started_at, ended_at, warmup_state, warmup_days, purpose, health_state, ownership_state,
       status, proof_refs, created_at, updated_at, row_version
FROM source_holding_stays
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY started_at ASC, stay_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HoldingStay
	for rows.Next() {
		row, scanErr := scanHoldingStay(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listHFVaccinationEvidence(ctx context.Context, tenantID, loadID string) ([]domain.HFVaccinationEvidence, error) {
	rows, err := r.pool.Query(ctx, `
SELECT evidence_id::text, tenant_id::text, load_id::text, goat_id::text,
       protocol_version_id::text, rule_id::text, dose_code, administered_at,
       vaccine_name, lot_number, proof_ref_id::text, source_ref, review_status,
       reviewed_by::text, reviewed_at, review_reason, imported_by::text,
       imported_at, metadata, created_at, updated_at, row_version
FROM procurement_hf_vaccination_evidence
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY administered_at DESC, evidence_id DESC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.HFVaccinationEvidence{}
	for rows.Next() {
		row, scanErr := scanHFVaccinationEvidence(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listHealthChecks(ctx context.Context, tenantID, loadID string) ([]domain.SourceHealthCheck, error) {
	rows, err := r.pool.Query(ctx, `
SELECT health_check_id::text, tenant_id::text, goat_id::text, load_id::text,
       health_state, reason, checked_by::text, checked_at, proof_ref_id::text,
       sop_task_id::text, created_at
FROM procurement_source_health_checks
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY checked_at ASC, health_check_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SourceHealthCheck
	for rows.Next() {
		row, scanErr := scanHealthCheck(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listDecisions(ctx context.Context, tenantID, loadID string) ([]domain.Decision, error) {
	rows, err := r.pool.Query(ctx, `
SELECT decision_id::text, tenant_id::text, goat_id::text, load_id::text,
       decision_stage, decision_type, reason, decided_by::text, decided_at,
       proof_ref_id::text, sop_task_id::text, owner_id::text, resume_condition,
       metadata, created_at
FROM source_entry_decisions
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY decided_at ASC, decision_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Decision
	for rows.Next() {
		row, scanErr := scanDecision(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listTransit(ctx context.Context, tenantID, loadID string) ([]domain.TransitHandoff, error) {
	rows, err := r.pool.Query(ctx, `
	SELECT handoff_id::text, tenant_id::text, load_id::text, from_location_id::text,
	       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.from_location_id), from_location_id::text),
	       to_location_id::text,
	       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = transit_handoffs.tenant_id AND location_id = transit_handoffs.to_location_id), to_location_id::text),
	       loaded_count, dispatched_at, arrived_at, proof_ref_id::text,
	       discrepancy_state, status, created_at, updated_at, row_version
	FROM transit_handoffs
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY dispatched_at ASC, handoff_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TransitHandoff
	for rows.Next() {
		row, scanErr := scanTransit(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// getSourceHealthByID re-reads a previously recorded source-health check for an idempotent replay.
// getLoadByID re-reads a previously created load for an idempotent CreateLoad replay.
func (r *Repository) getLoadByID(ctx context.Context, tenantID, loadID string) (domain.Load, error) {
	return scanLoad(r.pool.QueryRow(ctx, `
SELECT pl.load_id::text, pl.tenant_id::text, pl.source_party_id::text, p.display_name,
       pl.source_location_id::text, loc.location_code, loc.name,
       pl.expected_count, pl.purchase_date, pl.planned_dispatch_at, pl.status, pl.notes, pl.context,
       pl.created_at, pl.updated_at, pl.row_version
FROM procurement_loads pl
JOIN parties p ON p.party_id = pl.source_party_id
LEFT JOIN locations loc ON loc.tenant_id = pl.tenant_id AND loc.location_id = pl.source_location_id
WHERE pl.tenant_id = $1::uuid AND pl.load_id = nullif($2::text, '')::uuid`, tenantID, loadID))
}

// getLoadGoatByID re-reads a previously added load goat for an idempotent AddGoatToLoad replay.
func (r *Repository) getLoadGoatByID(ctx context.Context, tenantID, loadGoatID string) (domain.LoadGoat, error) {
	return scanLoadGoat(r.pool.QueryRow(ctx, `
SELECT load_goat_id::text, tenant_id::text, load_id::text, goat_id::text,
       animal_identifier_1, animal_identifier_2, selection_state, selection_reason, purpose,
       current_state, source_entry_state, source_entry_ref, ownership_state,
       health_state, warmup_started_at, warmup_ended_at, warmup_days,
       holding_location_id::text, loaded_at, arrived_at, intake_accepted_at,
       exit_reason, proof_refs, metadata, created_at, updated_at, row_version
FROM procurement_load_goats
WHERE tenant_id = $1::uuid AND load_goat_id = nullif($2::text, '')::uuid`, tenantID, loadGoatID))
}

func (r *Repository) getHFVaccinationEvidenceByID(ctx context.Context, tenantID, evidenceID string) (domain.HFVaccinationEvidence, error) {
	return r.getHFVaccinationEvidenceByIDTx(ctx, r.pool, tenantID, evidenceID)
}

func (r *Repository) getHFVaccinationEvidenceByIDTx(ctx context.Context, rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenantID, evidenceID string) (domain.HFVaccinationEvidence, error) {
	return scanHFVaccinationEvidence(rowQuerier.QueryRow(ctx, `
SELECT evidence_id::text, tenant_id::text, load_id::text, goat_id::text,
       protocol_version_id::text, rule_id::text, dose_code, administered_at,
       vaccine_name, lot_number, proof_ref_id::text, source_ref, review_status,
       reviewed_by::text, reviewed_at, review_reason, imported_by::text,
       imported_at, metadata, created_at, updated_at, row_version
FROM procurement_hf_vaccination_evidence
WHERE tenant_id = $1::uuid AND evidence_id = nullif($2::text, '')::uuid`, tenantID, evidenceID))
}

func (r *Repository) getSourceHealthByID(ctx context.Context, tenantID, healthCheckID string) (domain.SourceHealthCheck, error) {
	return scanHealthCheck(r.pool.QueryRow(ctx, `
SELECT health_check_id::text, tenant_id::text, goat_id::text, load_id::text,
       health_state, reason, checked_by::text, checked_at, proof_ref_id::text,
       sop_task_id::text, created_at
FROM procurement_source_health_checks
WHERE tenant_id = $1::uuid AND health_check_id = nullif($2::text, '')::uuid`, tenantID, healthCheckID))
}

// getDecisionByID re-reads a previously recorded source-entry decision for an idempotent replay.
func (r *Repository) getDecisionByID(ctx context.Context, tenantID, decisionID string) (domain.Decision, error) {
	return scanDecision(r.pool.QueryRow(ctx, `
SELECT decision_id::text, tenant_id::text, goat_id::text, load_id::text,
       decision_stage, decision_type, reason, decided_by::text, decided_at,
       proof_ref_id::text, sop_task_id::text, owner_id::text, resume_condition,
       metadata, created_at
FROM source_entry_decisions
WHERE tenant_id = $1::uuid AND decision_id = nullif($2::text, '')::uuid`, tenantID, decisionID))
}

// getArrivalReviewByID re-reads a previously recorded arrival review (with its goats) for an idempotent replay.
func (r *Repository) getArrivalReviewByID(ctx context.Context, tenantID, reviewID string) (domain.ArrivalReview, error) {
	review, err := scanArrivalReview(r.pool.QueryRow(ctx, `
SELECT review_id::text, tenant_id::text, load_id::text, park_location_id::text,
       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = arrival_intake_reviews.tenant_id AND location_id = arrival_intake_reviews.park_location_id), park_location_id::text),
       expected_count, loaded_count, arrived_count, matched_count, missing_count,
       extra_count, rejected_count, health_flags, weight_flags, media_proof_id::text,
       status, reviewed_by::text, reviewed_at, created_at, updated_at, row_version
FROM arrival_intake_reviews
WHERE tenant_id = $1::uuid AND review_id = nullif($2::text, '')::uuid`, tenantID, reviewID))
	if err != nil {
		return domain.ArrivalReview{}, err
	}
	items, err := r.listArrivalGoats(ctx, tenantID, review.ReviewID)
	if err != nil {
		return domain.ArrivalReview{}, err
	}
	review.Goats = items
	return review, nil
}

func (r *Repository) listArrivalReviews(ctx context.Context, tenantID, loadID string) ([]domain.ArrivalReview, error) {
	rows, err := r.pool.Query(ctx, `
SELECT review_id::text, tenant_id::text, load_id::text, park_location_id::text,
       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = arrival_intake_reviews.tenant_id AND location_id = arrival_intake_reviews.park_location_id), park_location_id::text),
       expected_count, loaded_count, arrived_count, matched_count, missing_count,
       extra_count, rejected_count, health_flags, weight_flags, media_proof_id::text,
       status, reviewed_by::text, reviewed_at, created_at, updated_at, row_version
FROM arrival_intake_reviews
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY reviewed_at ASC, review_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArrivalReview
	for rows.Next() {
		row, scanErr := scanArrivalReview(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		items, itemErr := r.listArrivalGoats(ctx, tenantID, out[i].ReviewID)
		if itemErr != nil {
			return nil, itemErr
		}
		out[i].Goats = items
	}
	return out, nil
}

func (r *Repository) listArrivalGoats(ctx context.Context, tenantID, reviewID string) ([]domain.ArrivalGoat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT review_goat_id::text, tenant_id::text, review_id::text, load_id::text,
       goat_id::text, animal_identifier_2, animal_identifier_1, arrival_state, health_flag,
       weight_flag, proof_ref_id::text, notes, created_at
FROM arrival_intake_review_goats
WHERE tenant_id = $1::uuid AND review_id = $2::uuid
ORDER BY arrival_state ASC, review_goat_id ASC`, tenantID, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArrivalGoat
	for rows.Next() {
		row, scanErr := scanArrivalGoat(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *Repository) listPCHandoffs(ctx context.Context, tenantID, loadID string) ([]domain.PCHandoff, error) {
	rows, err := r.pool.Query(ctx, `
SELECT handoff_id::text, tenant_id::text, load_id::text, goat_id::text,
       accepted_at, park_location_id::text,
       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = procurement_pc_handoffs.tenant_id AND location_id = procurement_pc_handoffs.park_location_id), park_location_id::text),
       shed_location_id::text,
       COALESCE((SELECT COALESCE(NULLIF(location_code, ''), name) FROM locations WHERE tenant_id = procurement_pc_handoffs.tenant_id AND location_id = procurement_pc_handoffs.shed_location_id), shed_location_id::text),
       entry_date,
       trusted_vaccination_history, intake_health_signal, event_status, created_at, updated_at
FROM procurement_pc_handoffs
WHERE tenant_id = $1::uuid AND load_id = $2::uuid
ORDER BY accepted_at ASC, handoff_id ASC`, tenantID, loadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PCHandoff
	for rows.Next() {
		row, scanErr := scanPCHandoff(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func buildTimeline(detail domain.LoadDetail) []domain.TimelineEvent {
	events := []domain.TimelineEvent{{
		EventType:  "load_created",
		OccurredAt: detail.Load.CreatedAt,
		RefID:      detail.Load.LoadID,
		State:      detail.Load.Status,
		Summary:    "procurement load created",
	}}
	for _, goat := range detail.Goats {
		goatID := goat.GoatID
		events = append(events, domain.TimelineEvent{EventType: "goat_added", OccurredAt: goat.CreatedAt, GoatID: &goatID, RefID: goat.LoadGoatID, State: goat.CurrentState, Summary: "goat added to source-entry load"})
	}
	for _, stay := range detail.HoldingStays {
		goatID := stay.GoatID
		events = append(events, domain.TimelineEvent{EventType: "holding_stay", OccurredAt: stay.StartedAt, GoatID: &goatID, RefID: stay.StayID, State: stay.WarmupState, Summary: "source holding stay opened"})
	}
	for _, health := range detail.HealthChecks {
		goatID := health.GoatID
		events = append(events, domain.TimelineEvent{EventType: "source_health", OccurredAt: health.CheckedAt, GoatID: &goatID, RefID: health.HealthCheckID, State: health.HealthState, Summary: "source health SOP recorded"})
	}
	for _, decision := range detail.Decisions {
		goatID := decision.GoatID
		events = append(events, domain.TimelineEvent{EventType: decision.DecisionStage, OccurredAt: decision.DecidedAt, GoatID: &goatID, RefID: decision.DecisionID, State: decision.DecisionType, Summary: decision.Reason})
	}
	for _, transit := range detail.Transit {
		events = append(events, domain.TimelineEvent{EventType: "transit_handoff", OccurredAt: transit.DispatchedAt, RefID: transit.HandoffID, State: transit.Status, Summary: transit.DiscrepancyState})
	}
	for _, review := range detail.ArrivalReviews {
		events = append(events, domain.TimelineEvent{EventType: "arrival_review", OccurredAt: review.ReviewedAt, RefID: review.ReviewID, State: review.Status, Summary: "arrival gate reviewed"})
	}
	for _, handoff := range detail.PCHandoffs {
		goatID := handoff.GoatID
		events = append(events, domain.TimelineEvent{EventType: "accepted_intake_pc_handoff", OccurredAt: handoff.AcceptedAt, GoatID: &goatID, RefID: handoff.HandoffID, State: handoff.EventStatus, Summary: "accepted intake ready for PC vaccination handoff"})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OccurredAt.Before(events[j].OccurredAt)
	})
	return events
}

func mapHealthStateToProcurementState(state string) string {
	switch state {
	case domain.HealthPassed:
		return domain.GoatStateSourceHealthPassed
	case domain.HealthFailed:
		return domain.GoatStateSourceHealthFailed
	default:
		return domain.GoatStateSourceHealthPending
	}
}

func decisionState(decisionType string) (goatState, selectionState, loadStatus string) {
	switch decisionType {
	case domain.DecisionAccepted:
		return domain.GoatStatePreDispatchAccepted, "accepted", domain.LoadStatusDispatchReady
	case domain.DecisionRejected:
		return domain.GoatStatePreDispatchRejected, "rejected", domain.LoadStatusPreDispatchPending
	case domain.DecisionDeferred:
		return domain.GoatStatePreDispatchDeferred, "deferred", domain.LoadStatusDeferred
	default:
		return domain.GoatStatePreDispatchBlocked, "blocked", domain.LoadStatusBlocked
	}
}

func exitStateFromReason(reason string) (currentState, exitReason string, ok bool) {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case strings.Contains(normalized, "died") || strings.Contains(normalized, "dead"):
		return domain.GoatStateDead, "died", true
	case strings.Contains(normalized, "sold"):
		return domain.GoatStateSold, "sold", true
	case strings.Contains(normalized, "lost") ||
		strings.Contains(normalized, "missing goat") ||
		strings.Contains(normalized, "goat missing"):
		return domain.GoatStateLost, "lost", true
	default:
		return "", "", false
	}
}

func mapArrivalState(state string) string {
	switch state {
	case "accepted", "matched":
		return domain.GoatStateArrivalAccepted
	case "rejected":
		return domain.GoatStateArrivalRejected
	case "missing", "health_flag", "weight_flag", "deferred", "blocked":
		return domain.GoatStateArrivalReviewPending
	default:
		return domain.GoatStateArrivalReviewPending
	}
}

func arrivalItemKey(item ports.ArrivalGoat) string {
	return ports.ArrivalItemKey(item)
}

func warmupState(days *int, purpose string) string {
	if days == nil {
		return "in_progress"
	}
	if *days < 28 || *days > 35 {
		return "outside_normal_window"
	}
	return "completed"
}

func textPtr(v pgtype.Text) *string {
	if !v.Valid || v.String == "" {
		return nil
	}
	s := v.String
	return &s
}

func textValue(v pgtype.Text) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func timePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func datePtr(v pgtype.Date) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func intPtr(v pgtype.Int4) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int32)
	return &i
}

func stringPtrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func stringPtr(v string) *string {
	return &v
}

func timeArg(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return *v
}

func dateArg(v *time.Time) any {
	if v == nil || v.IsZero() {
		return nil
	}
	return biztime.BusinessDate(*v)
}

func intPtrArg(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func intPtrFingerprint(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func jsonObjectArg(v json.RawMessage) []byte {
	if len(v) == 0 {
		return []byte("{}")
	}
	return []byte(v)
}

func jsonArrayArg(v json.RawMessage) []byte {
	if len(v) == 0 {
		return []byte("[]")
	}
	return []byte(v)
}

func rawJSON(v []byte, fallback string) json.RawMessage {
	if len(v) == 0 || !json.Valid(v) {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(v)
}

func normalizeIdentifier(v string) string {
	return ports.NormalizeIdentifier(v)
}
