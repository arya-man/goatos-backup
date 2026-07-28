package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repository) CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Campaign{}, err
	}
	defer tx.Rollback(ctx)
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_created", cmd.IdempotencyKey); err != nil || ok {
		return existing, err
	}
	var c domain.Campaign
	err = tx.QueryRow(ctx, `
INSERT INTO weighing_campaigns (tenant_id, park_id, period_start_date, period_end_date, start_business_date, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid,$2::uuid,$3::date,$4::date,$5::date,$6,$7::uuid,$8::uuid)
RETURNING campaign_id::text, tenant_id::text, park_id::text, period_start_date::text, period_end_date::text, start_business_date::text, status, planned_cap_per_day, operator_user_id::text, created_by::text, created_at, updated_at, row_version`,
		cmd.TenantID, cmd.ParkID, cmd.PeriodStartDate, cmd.PeriodEndDate, cmd.StartBusinessDate, cmd.PlannedCapPerDay, cmd.OperatorUserID, cmd.CreatedBy).
		Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "weighing_campaigns_one_active_week_per_park_idx" {
			return domain.Campaign{}, ports.ErrImmutable
		}
		return domain.Campaign{}, err
	}
	for _, shed := range cmd.Sheds {
		var cs domain.CampaignShed
		err = tx.QueryRow(ctx, `
WITH inserted AS (
  INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, expected_animal_count)
  SELECT $1::uuid, $2::uuid, $3::uuid, $4, $5, $6,
    CASE WHEN $6 = 'individual_animal' THEN (
      SELECT count(*)::int FROM goats g WHERE g.tenant_id = $2::uuid AND g.current_location_id = $3::uuid AND g.lifecycle_status = 'alive' AND herd_register_is_kid(g.age_band, g.management_stage)
    ) ELSE 1 END
  RETURNING campaign_shed_id, expected_animal_count
)
INSERT INTO weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id)
SELECT $1::uuid, $2::uuid, g.goat_id, $3::uuid, $5, inserted.campaign_shed_id
FROM inserted
JOIN goats g ON g.tenant_id = $2::uuid AND g.current_location_id = $3::uuid AND g.lifecycle_status = 'alive' AND herd_register_is_kid(g.age_band, g.management_stage)
WHERE $6 = 'individual_animal'
ON CONFLICT DO NOTHING
RETURNING (SELECT campaign_shed_id::text FROM inserted), $1::text, $3::text, $4, $5, (SELECT expected_animal_count FROM inserted), $6, 'pending'`, c.CampaignID, cmd.TenantID, shed.LocationID, shed.LocationType, shed.DisplayName, shed.WeighingCategory).
			Scan(&cs.CampaignShedID, &cs.CampaignID, &cs.LocationID, &cs.LocationType, &cs.DisplayName, &cs.ExpectedAnimalCount, &cs.WeighingCategory, &cs.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT campaign_shed_id::text, campaign_id::text, location_id::text, location_type, display_name, expected_animal_count, weighing_category, status FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND location_id=$3::uuid`, cmd.TenantID, c.CampaignID, shed.LocationID).
				Scan(&cs.CampaignShedID, &cs.CampaignID, &cs.LocationID, &cs.LocationType, &cs.DisplayName, &cs.ExpectedAnimalCount, &cs.WeighingCategory, &cs.Status)
		}
		if err != nil {
			return domain.Campaign{}, err
		}
		c.Sheds = append(c.Sheds, cs)
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.campaign_created", c.CampaignID, cmd.IdempotencyKey, c); err != nil {
		return domain.Campaign{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Campaign{}, err
	}
	c.Progress = progress(c.Sheds, 0, 0, 0, 0)
	return c, nil
}

func (r *Repository) UpdateCampaign(ctx context.Context, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Campaign{}, err
	}
	defer tx.Rollback(ctx)
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_updated", cmd.IdempotencyKey); err != nil || ok {
		return existing, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE weighing_campaigns
SET park_id=$3::uuid,
    period_start_date=$4::date,
    period_end_date=$5::date,
    start_business_date=$6::date,
    planned_cap_per_day=$7,
    operator_user_id=$8::uuid,
    updated_at=now(),
    row_version=row_version+1
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status <> 'completed'
  AND status <> 'canceled'`,
		cmd.TenantID, campaignID, cmd.ParkID, cmd.PeriodStartDate, cmd.PeriodEndDate, cmd.StartBusinessDate, cmd.PlannedCapPerDay, cmd.OperatorUserID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Campaign{}, ports.ErrImmutable
	}
	for _, shed := range cmd.Sheds {
		var campaignShedID string
		err = tx.QueryRow(ctx, `
WITH upserted AS (
  INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, expected_animal_count)
  SELECT $1::uuid, $2::uuid, $3::uuid, $4, $5, $6,
    CASE WHEN $6 = 'individual_animal' THEN (
      SELECT count(*)::int FROM goats g WHERE g.tenant_id = $2::uuid AND g.current_location_id = $3::uuid AND g.lifecycle_status = 'alive' AND herd_register_is_kid(g.age_band, g.management_stage)
    ) ELSE 1 END
  ON CONFLICT (tenant_id, campaign_id, location_id)
  DO UPDATE SET
    location_type=EXCLUDED.location_type,
    display_name=EXCLUDED.display_name,
    weighing_category=EXCLUDED.weighing_category,
    expected_animal_count=EXCLUDED.expected_animal_count,
    updated_at=now()
  WHERE weighing_campaign_sheds.status <> 'completed'
  RETURNING campaign_shed_id
)
INSERT INTO weighing_expected_animals (campaign_id, tenant_id, animal_id, expected_location_id, expected_location_label, campaign_shed_id)
SELECT $1::uuid, $2::uuid, g.goat_id, $3::uuid, $5, upserted.campaign_shed_id
FROM upserted
JOIN goats g ON g.tenant_id = $2::uuid AND g.current_location_id = $3::uuid AND g.lifecycle_status = 'alive' AND herd_register_is_kid(g.age_band, g.management_stage)
WHERE $6 = 'individual_animal'
ON CONFLICT (campaign_id, animal_id)
DO UPDATE SET
  expected_location_id=EXCLUDED.expected_location_id,
  expected_location_label=EXCLUDED.expected_location_label,
  campaign_shed_id=EXCLUDED.campaign_shed_id,
  status=CASE WHEN weighing_expected_animals.status='weighed' THEN weighing_expected_animals.status ELSE 'pending' END,
  availability_status=CASE WHEN weighing_expected_animals.status='weighed' THEN weighing_expected_animals.availability_status ELSE 'expected_shed' END,
  updated_at=now()
RETURNING (SELECT campaign_shed_id::text FROM upserted)`, campaignID, cmd.TenantID, shed.LocationID, shed.LocationType, shed.DisplayName, shed.WeighingCategory).
			Scan(&campaignShedID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `SELECT campaign_shed_id::text FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND location_id=$3::uuid`, cmd.TenantID, campaignID, shed.LocationID).Scan(&campaignShedID)
		}
		if err != nil {
			return domain.Campaign{}, err
		}
		if shed.WeighingCategory == domain.CategoryPerShedPartition {
			if _, err := tx.Exec(ctx, `UPDATE weighing_expected_animals SET status='canceled', updated_at=now() WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid AND status <> 'weighed'`, cmd.TenantID, campaignID, campaignShedID); err != nil {
				return domain.Campaign{}, err
			}
		}
	}
	c, err := r.getCampaignTx(ctx, tx, cmd.TenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.campaign_updated", campaignID, cmd.IdempotencyKey, c); err != nil {
		return domain.Campaign{}, err
	}
	return c, tx.Commit(ctx)
}

func (r *Repository) PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Campaign{}, err
	}
	defer tx.Rollback(ctx)
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, tenantID, "weighing.campaign_published", idempotencyKey); err != nil || ok {
		return existing, err
	}
	tag, err := tx.Exec(ctx, `UPDATE weighing_campaigns SET status='published', published_at=now(), updated_at=now(), row_version=row_version+1 WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND status='draft'`, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Campaign{}, ports.ErrImmutable
	}
	c, err := r.getCampaignTx(ctx, tx, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if err := r.enqueue(ctx, tx, tenantID, "weighing.campaign_published", campaignID, idempotencyKey, map[string]string{"campaign_id": campaignID, "published_by": actorID}); err != nil {
		return domain.Campaign{}, err
	}
	return c, tx.Commit(ctx)
}

func (r *Repository) ListCampaigns(ctx context.Context, tenantID string, cursor string, limit int) (domain.CampaignPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	cur, err := decodeCampaignCursor(cursor)
	if err != nil {
		return domain.CampaignPage{}, ports.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, `
SELECT campaign_id::text, tenant_id::text, park_id::text, period_start_date::text, period_end_date::text,
  start_business_date::text, status, planned_cap_per_day, operator_user_id::text, created_by::text,
  created_at, updated_at, row_version
FROM weighing_campaigns
WHERE tenant_id=$1::uuid
  AND (
    $2::date IS NULL
    OR (period_start_date, created_at, campaign_id) < ($2::date, $3::timestamptz, $4::uuid)
  )
ORDER BY period_start_date DESC, created_at DESC, campaign_id DESC
LIMIT $5`, tenantID, nullableString(cur.PeriodStartDate), nullableTime(cur.CreatedAt), nullableString(cur.CampaignID), limit+1)
	if err != nil {
		return domain.CampaignPage{}, err
	}
	defer rows.Close()
	out := make([]domain.Campaign, 0, limit)
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var c domain.Campaign
		if err := rows.Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion); err != nil {
			return domain.CampaignPage{}, err
		}
		out = append(out, c)
		ids = append(ids, c.CampaignID)
	}
	if err := rows.Err(); err != nil {
		return domain.CampaignPage{}, err
	}
	nextCursor := ""
	if len(out) > limit {
		last := out[limit-1]
		nextCursor = encodeCampaignCursor(campaignCursor{PeriodStartDate: last.PeriodStartDate, CreatedAt: last.CreatedAt, CampaignID: last.CampaignID})
		out = out[:limit]
		ids = ids[:limit]
	}
	if err := r.hydrateCampaigns(ctx, tenantID, ids, out); err != nil {
		return domain.CampaignPage{}, err
	}
	return domain.CampaignPage{Items: out, NextCursor: nextCursor}, nil
}

// PlannerCatalog returns the bounded Android planner vocabulary: active parks, their active kid
// sheds with current kid counts, active field operators, and existing park/week campaigns.
//
// mobile-guard:ignore: bounded park/shed/operator catalog cached on-device, not a paginated feed
// scale-guard:ignore: bounded physical-location catalog; kid counts are grouped server-side
func (r *Repository) PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string) (domain.PlannerCatalog, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
WITH shed_counts AS (
  SELECT
    g.current_location_id AS shed_id,
    count(*)::int AS kid_count
  FROM goats g
  WHERE g.tenant_id=$1::uuid
    AND g.lifecycle_status='alive'
    AND COALESCE(herd_register_is_kid(g.age_band, g.management_stage), false)
    AND g.current_location_id IS NOT NULL
  GROUP BY g.current_location_id
),
existing AS (
  SELECT
    wc.park_id,
    wc.campaign_id::text,
    wc.status,
    wc.period_start_date::text,
    wc.period_end_date::text,
    wc.start_business_date::text,
    wc.operator_user_id::text,
    count(wcs.campaign_shed_id)::int AS shed_count
  FROM weighing_campaigns wc
  LEFT JOIN weighing_campaign_sheds wcs
    ON wcs.tenant_id=wc.tenant_id
   AND wcs.campaign_id=wc.campaign_id
  WHERE wc.tenant_id=$1::uuid
    AND wc.period_start_date=$2::date
    AND wc.status <> 'canceled'
  GROUP BY wc.park_id, wc.campaign_id
)
SELECT
  park.location_id::text,
  park.name,
  shed.location_id::text,
  shed.name,
  COALESCE(sc.kid_count, 0)::int,
  existing.campaign_id,
  existing.status,
  existing.period_start_date,
  existing.period_end_date,
  existing.start_business_date,
  existing.operator_user_id,
  COALESCE(existing.shed_count, 0)::int
FROM locations park
LEFT JOIN locations shed
       ON shed.tenant_id=park.tenant_id
      AND shed.parent_location_id=park.location_id
      AND shed.location_type='shed'
      AND shed.status='active'
      AND shed.retired_at IS NULL
LEFT JOIN shed_counts sc ON sc.shed_id=shed.location_id
LEFT JOIN existing ON existing.park_id=park.location_id
WHERE park.tenant_id=$1::uuid
  AND park.location_type='park'
  AND park.status='active'
  AND park.retired_at IS NULL
ORDER BY park.display_order, park.name, park.location_id, shed.display_order, shed.name, shed.location_id`, tenantID, periodStartDate)
	if err != nil {
		return domain.PlannerCatalog{}, err
	}
	defer rows.Close()
	out := domain.PlannerCatalog{Parks: []domain.PlannerPark{}, Operators: []domain.PlannerOperator{}}
	parkIndex := map[string]int{}
	for rows.Next() {
		var parkID, parkName string
		var shedID, shedName *string
		var kidCount int
		var existingID, existingStatus, existingStart, existingEnd, existingBusinessDate, existingOperator *string
		var existingShedCount int
		if err := rows.Scan(&parkID, &parkName, &shedID, &shedName, &kidCount, &existingID, &existingStatus, &existingStart, &existingEnd, &existingBusinessDate, &existingOperator, &existingShedCount); err != nil {
			return domain.PlannerCatalog{}, err
		}
		idx, ok := parkIndex[parkID]
		if !ok {
			park := domain.PlannerPark{ParkID: parkID, Name: parkName, Sheds: []domain.PlannerShed{}}
			if existingID != nil {
				park.ExistingCampaign = &domain.CampaignSummary{
					CampaignID:        *existingID,
					Status:            deref(existingStatus),
					PeriodStartDate:   deref(existingStart),
					PeriodEndDate:     deref(existingEnd),
					StartBusinessDate: deref(existingBusinessDate),
					OperatorUserID:    deref(existingOperator),
					ShedCount:         existingShedCount,
				}
			}
			out.Parks = append(out.Parks, park)
			idx = len(out.Parks) - 1
			parkIndex[parkID] = idx
		}
		if shedID == nil || shedName == nil {
			continue
		}
		out.Parks[idx].Sheds = append(out.Parks[idx].Sheds, domain.PlannerShed{
			LocationID: *shedID,
			Name:       *shedName,
			KidCount:   kidCount,
		})
		out.Parks[idx].KidCount += kidCount
	}
	if err := rows.Err(); err != nil {
		return domain.PlannerCatalog{}, err
	}
	operatorRows, err := r.pool.Query(ctx, `
SELECT user_id::text, display_name, display_code
FROM workforce_members
WHERE tenant_id=$1::uuid
  AND status='active'
  AND user_id IS NOT NULL
  AND primary_role_hint='operator'
ORDER BY display_name, display_code, user_id`, tenantID)
	if err != nil {
		return domain.PlannerCatalog{}, err
	}
	defer operatorRows.Close()
	for operatorRows.Next() {
		var operator domain.PlannerOperator
		if err := operatorRows.Scan(&operator.UserID, &operator.DisplayName, &operator.DisplayCode); err != nil {
			return domain.PlannerCatalog{}, err
		}
		out.Operators = append(out.Operators, operator)
	}
	if err := operatorRows.Err(); err != nil {
		return domain.PlannerCatalog{}, err
	}
	return out, nil
}

func (r *Repository) ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, cursor string, limit int) (domain.RosterPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cur, err := decodeRosterCursor(cursor)
	if err != nil {
		return domain.RosterPage{}, ports.ErrInvalidArgument
	}
	rows, err := r.pool.Query(ctx, `
WITH scoped AS (
  SELECT
    ea.campaign_id,
    ea.campaign_shed_id,
    ea.animal_id,
    g.display_id,
    ea.expected_location_id,
    ea.expected_location_label,
    ea.status,
    ea.availability_status,
    ea.current_location_id,
    ea.current_location_label,
    ea.current_lifecycle_status,
    ea.created_at,
    row_number() OVER (ORDER BY ea.created_at, ea.animal_id) AS seq
  FROM weighing_expected_animals ea
  JOIN goats g ON g.tenant_id=ea.tenant_id AND g.goat_id=ea.animal_id
  WHERE ea.tenant_id=$1::uuid
    AND ea.campaign_id=$2::uuid
    AND ea.campaign_shed_id=$3::uuid
    AND (
      $4::timestamptz IS NULL
      OR (ea.created_at, ea.animal_id) > ($4::timestamptz, $5::uuid)
    )
  ORDER BY ea.created_at, ea.animal_id
  LIMIT $6
)
SELECT
  scoped.campaign_id::text,
  scoped.campaign_shed_id::text,
  scoped.animal_id::text,
  scoped.display_id,
  COALESCE(primary_id.identifier_value, '') AS primary_identifier,
  COALESCE(secondary_id.identifier_value, '') AS secondary_identifier,
  scoped.expected_location_id::text,
  scoped.expected_location_label,
  scoped.status,
  scoped.availability_status,
  COALESCE(scoped.current_location_id::text, '') AS current_location_id,
  COALESCE(scoped.current_location_label, '') AS current_location_label,
  COALESCE(scoped.current_lifecycle_status, '') AS current_lifecycle_status,
  scoped.seq::bigint,
  scoped.created_at
FROM scoped
LEFT JOIN LATERAL (
  SELECT identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id=$1::uuid
    AND gi.goat_id=scoped.animal_id
    AND gi.identifier_type='animal_identifier_1'
    AND gi.status='active'
  ORDER BY gi.is_primary_for_goat DESC, gi.created_at DESC, gi.identifier_id
  LIMIT 1
) primary_id ON true
LEFT JOIN LATERAL (
  SELECT identifier_value
  FROM goat_identifiers gi
  WHERE gi.tenant_id=$1::uuid
    AND gi.goat_id=scoped.animal_id
    AND gi.identifier_type='animal_identifier_2'
    AND gi.status='active'
  ORDER BY gi.is_primary_for_goat DESC, gi.created_at DESC, gi.identifier_id
  LIMIT 1
) secondary_id ON true
ORDER BY scoped.created_at, scoped.animal_id`, tenantID, campaignID, campaignShedID, nullableTime(cur.CreatedAt), nullableString(cur.AnimalID), limit+1)
	if err != nil {
		return domain.RosterPage{}, err
	}
	defer rows.Close()
	out := make([]domain.ExpectedAnimal, 0, limit)
	created := make([]time.Time, 0, limit+1)
	for rows.Next() {
		var animal domain.ExpectedAnimal
		var createdAt time.Time
		if err := rows.Scan(
			&animal.CampaignID,
			&animal.CampaignShedID,
			&animal.AnimalID,
			&animal.DisplayAnimalID,
			&animal.PrimaryIdentifier,
			&animal.SecondaryIdentifier,
			&animal.ExpectedLocationID,
			&animal.ExpectedLocationLabel,
			&animal.Status,
			&animal.AvailabilityStatus,
			&animal.CurrentLocationID,
			&animal.CurrentLocationLabel,
			&animal.CurrentLifecycleStatus,
			&animal.Seq,
			&createdAt,
		); err != nil {
			return domain.RosterPage{}, err
		}
		out = append(out, animal)
		created = append(created, createdAt)
	}
	if err := rows.Err(); err != nil {
		return domain.RosterPage{}, err
	}
	nextCursor := ""
	if len(out) > limit {
		last := out[limit-1]
		nextCursor = encodeRosterCursor(rosterCursor{CreatedAt: created[limit-1], AnimalID: last.AnimalID})
		out = out[:limit]
	}
	return domain.RosterPage{Items: out, NextCursor: nextCursor}, nil
}

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	if existing, ok, err := r.observationByIdemTx(ctx, tx, cmd.TenantID, cmd.IdempotencyKey); err != nil || ok {
		if err != nil {
			return domain.Observation{}, err
		}
		return existing, tx.Commit(ctx)
	}
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
	WITH campaign AS (
	  SELECT campaign_id
	  FROM weighing_campaigns
	  WHERE tenant_id=$1::uuid
	    AND campaign_id=$2::uuid
	    AND status IN ('published','in_progress')
	    AND operator_user_id=$7::uuid
	), expected AS (
	  SELECT
	    ea.campaign_shed_id,
	    ea.expected_location_id,
	    ea.expected_location_label,
	    COALESCE(NULLIF($8, '')::uuid, g.current_location_id) AS actual_location_id,
	    actual_location.name AS actual_location_label
	  FROM campaign c
	  JOIN goats g ON g.tenant_id=$1::uuid AND g.goat_id=$3::uuid
	  LEFT JOIN weighing_expected_animals ea ON ea.tenant_id=$1::uuid AND ea.campaign_id=$2::uuid AND ea.animal_id=g.goat_id
	  LEFT JOIN locations actual_location
	    ON actual_location.tenant_id=g.tenant_id
	   AND actual_location.location_id=COALESCE(NULLIF($8, '')::uuid, g.current_location_id)
	  JOIN proof_artifacts proof
	    ON proof.tenant_id=$1::uuid
	   AND proof.proof_id=$5::uuid
	   AND proof.upload_state='completed'
	   AND proof.proof_type='video'
	   AND proof.subject_type='goat'
	   AND proof.subject_id=$3::uuid
	   AND (
	     (proof.scope_type='goat' AND proof.scope_id=$3::uuid)
	     OR (
	       proof.scope_type='shed'
	       AND proof.scope_id IN (ea.expected_location_id, COALESCE(NULLIF($8, '')::uuid, g.current_location_id))
	     )
	   )
	), inserted AS (
	  INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, animal_id, weight_kg, proof_artifact_id, expected_location_id, expected_location_label, actual_location_id, actual_location_label, mismatch_status, recorded_by, idempotency_key)
	  SELECT $1::uuid, $2::uuid, campaign_shed_id, $3::uuid, $4, $5::uuid, expected_location_id, expected_location_label, actual_location_id, actual_location_label,
	    CASE WHEN campaign_shed_id IS NULL THEN 'extra_scan' WHEN expected_location_id IS DISTINCT FROM actual_location_id THEN 'wrong_shed' ELSE 'expected_shed' END,
    $7::uuid, $6
  FROM expected
  ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
  RETURNING observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,'') AS campaign_shed_id_text, animal_id::text, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,'') AS expected_location_id_text, COALESCE(actual_location_id::text,'') AS actual_location_id_text, COALESCE(actual_location_label,'') AS actual_location_label_text, accepted_at
	)
	SELECT observation_id, campaign_id, campaign_shed_id_text, animal_id, weight_kg, proof_artifact_id, expected_location_id_text, actual_location_id_text, actual_location_label_text, accepted_at FROM inserted`,
		cmd.TenantID, cmd.CampaignID, cmd.AnimalID, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy, cmd.ActualLocationID).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, r.classifyAnimalObservationRejection(ctx, tx, cmd)
	}
	if err != nil {
		return domain.Observation{}, err
	}
	if obs.CampaignShedID != "" {
		if _, err := tx.Exec(ctx, `
UPDATE weighing_expected_animals
SET status='weighed',
  availability_status=CASE WHEN $4::uuid = expected_location_id THEN 'expected_shed' ELSE 'moved_other_shed' END,
  current_location_id=$4::uuid,
  current_location_label=$5,
  availability_checked_at=now(),
  updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND animal_id=$3::uuid`, cmd.TenantID, cmd.CampaignID, cmd.AnimalID, nullUUID(obs.ActualLocationID), obs.ActualLocationLabel); err != nil {
			return domain.Observation{}, err
		}
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.observation_accepted", obs.ObservationID, cmd.IdempotencyKey, obs); err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	if existing, ok, err := r.observationByIdemTx(ctx, tx, cmd.TenantID, cmd.IdempotencyKey); err != nil || ok {
		if err != nil {
			return domain.Observation{}, err
		}
		return existing, tx.Commit(ctx)
	}
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
	WITH campaign AS (
	  SELECT campaign_id
	  FROM weighing_campaigns
	  WHERE tenant_id=$1::uuid
	    AND campaign_id=$2::uuid
	    AND status IN ('published','in_progress')
	    AND operator_user_id=$7::uuid
	), scope AS (
	  SELECT cs.campaign_shed_id, cs.location_id
	  FROM campaign c
	  JOIN weighing_campaign_sheds cs
	    ON cs.tenant_id=$1::uuid
	   AND cs.campaign_id=c.campaign_id
	   AND cs.campaign_shed_id=$3::uuid
	   AND cs.weighing_category='per_shed_partition'
	  JOIN proof_artifacts proof
	    ON proof.tenant_id=$1::uuid
	   AND proof.proof_id=$5::uuid
	   AND proof.upload_state='completed'
	   AND proof.proof_type='video'
	   AND proof.scope_type='shed'
	   AND proof.scope_id=cs.location_id
	   AND proof.subject_type='shed'
	   AND proof.subject_id=cs.location_id
	)
	INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
	SELECT $1::uuid, $2::uuid, campaign_shed_id, $4, $5::uuid, $7::uuid, $6
	FROM scope
	ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	RETURNING shed_observation_id::text, campaign_id::text, campaign_shed_id::text, weight_kg::float8, proof_artifact_id::text, accepted_at`,
		cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.ProofArtifactID, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, r.classifyShedObservationRejection(ctx, tx, cmd)
	}
	if err != nil {
		return domain.Observation{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE weighing_campaign_sheds SET status='completed', completed_at=now(), updated_at=now() WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, cmd.TenantID, cmd.CampaignShedID); err != nil {
		return domain.Observation{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.shed_observation_accepted", obs.ObservationID, cmd.IdempotencyKey, obs); err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) RefreshAvailability(ctx context.Context, tenantID, campaignID string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	const chunkLimit = 500
	var lastCreated time.Time
	var lastAnimalID string
	for {
		rows, err := r.pool.Query(ctx, `
SELECT ea.animal_id::text, ea.created_at
FROM weighing_expected_animals ea
WHERE ea.tenant_id=$1::uuid
  AND ea.campaign_id=$2::uuid
  AND (
    $3::timestamptz IS NULL
    OR (ea.created_at, ea.animal_id) > ($3::timestamptz, $4::uuid)
  )
ORDER BY ea.created_at, ea.animal_id
LIMIT $5`, tenantID, campaignID, nullableTime(lastCreated), nullableString(lastAnimalID), chunkLimit)
		if err != nil {
			return err
		}
		animalIDs := make([]string, 0, chunkLimit)
		for rows.Next() {
			var animalID string
			var created time.Time
			if err := rows.Scan(&animalID, &created); err != nil {
				rows.Close()
				return err
			}
			animalIDs = append(animalIDs, animalID)
			lastAnimalID = animalID
			lastCreated = created
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(animalIDs) == 0 {
			return nil
		}
		if _, err := r.pool.Exec(ctx, `
UPDATE weighing_expected_animals ea
SET availability_status = CASE
    WHEN g.lifecycle_status IN ('dead') THEN 'dead'
    WHEN g.lifecycle_status IN ('sold','transferred') THEN 'sold_transferred'
    WHEN g.current_location_id IS DISTINCT FROM ea.expected_location_id THEN 'moved_other_shed'
    ELSE 'expected_shed'
  END,
  current_location_id=g.current_location_id,
  current_location_label=l.name,
  current_lifecycle_status=g.lifecycle_status,
  availability_checked_at=now(),
  status=CASE WHEN g.lifecycle_status IN ('dead','sold','transferred') THEN 'unavailable' ELSE ea.status END,
  updated_at=now()
FROM goats g
LEFT JOIN locations l ON l.tenant_id=g.tenant_id AND l.location_id=g.current_location_id
WHERE ea.tenant_id=$1::uuid
  AND ea.campaign_id=$2::uuid
  AND ea.animal_id = ANY($3::uuid[])
  AND g.tenant_id=ea.tenant_id
  AND g.goat_id=ea.animal_id`, tenantID, campaignID, animalIDs); err != nil {
			return err
		}
		if len(animalIDs) < chunkLimit {
			return nil
		}
	}
}

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

func (r *Repository) getCampaign(ctx context.Context, tenantID, campaignID string) (domain.Campaign, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Campaign{}, err
	}
	defer tx.Rollback(ctx)
	return r.getCampaignTx(ctx, tx, tenantID, campaignID)
}

func (r *Repository) getCampaignTx(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) (domain.Campaign, error) {
	var c domain.Campaign
	err := tx.QueryRow(ctx, `SELECT campaign_id::text, tenant_id::text, park_id::text, period_start_date::text, period_end_date::text, start_business_date::text, status, planned_cap_per_day, operator_user_id::text, created_by::text, created_at, updated_at, row_version FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, tenantID, campaignID).
		Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, err
	}
	rows, err := tx.Query(ctx, `SELECT campaign_shed_id::text, campaign_id::text, location_id::text, location_type, display_name, expected_animal_count, weighing_category, status FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid ORDER BY display_name`, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var shed domain.CampaignShed
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName, &shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.Status); err != nil {
			return domain.Campaign{}, err
		}
		c.Sheds = append(c.Sheds, shed)
	}
	completedAnimals, completedScopes, wrongShed, missing, err := r.progressStats(ctx, tx, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	c.Progress = progress(c.Sheds, completedAnimals, completedScopes, wrongShed, missing)
	return c, rows.Err()
}

func (r *Repository) hydrateCampaigns(ctx context.Context, tenantID string, ids []string, campaigns []domain.Campaign) error {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[string]int, len(campaigns))
	for i := range campaigns {
		byID[campaigns[i].CampaignID] = i
	}
	rows, err := r.pool.Query(ctx, `
SELECT campaign_shed_id::text, campaign_id::text, location_id::text, location_type, display_name, expected_animal_count, weighing_category, status
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id = ANY($2::uuid[])
ORDER BY campaign_id, display_name`, tenantID, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var shed domain.CampaignShed
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName, &shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.Status); err != nil {
			rows.Close()
			return err
		}
		if idx, ok := byID[shed.CampaignID]; ok {
			campaigns[idx].Sheds = append(campaigns[idx].Sheds, shed)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	stats := make(map[string]struct {
		completedAnimals int
		completedScopes  int
		wrongShed        int
		missing          int
	}, len(campaigns))
	rows, err = r.pool.Query(ctx, `
SELECT campaign_id::text,
  count(*) FILTER (WHERE status = 'weighed')::int AS completed_animals,
  count(*) FILTER (WHERE availability_status = 'moved_other_shed')::int AS wrong_shed,
  count(*) FILTER (
    WHERE availability_status IN ('dead', 'sold_transferred')
      OR current_lifecycle_status IN ('dead', 'sold', 'transferred')
  )::int AS missing
FROM weighing_expected_animals
WHERE tenant_id=$1::uuid AND campaign_id = ANY($2::uuid[])
GROUP BY campaign_id`, tenantID, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var row struct {
			completedAnimals int
			completedScopes  int
			wrongShed        int
			missing          int
		}
		if err := rows.Scan(&id, &row.completedAnimals, &row.wrongShed, &row.missing); err != nil {
			rows.Close()
			return err
		}
		stats[id] = row
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	rows, err = r.pool.Query(ctx, `
SELECT wso.campaign_id::text, count(DISTINCT wso.campaign_shed_id)::int
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=wso.tenant_id
 AND cs.campaign_id=wso.campaign_id
 AND cs.campaign_shed_id=wso.campaign_shed_id
 AND cs.weighing_category='per_shed_partition'
WHERE wso.tenant_id=$1::uuid AND wso.campaign_id = ANY($2::uuid[])
GROUP BY wso.campaign_id`, tenantID, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var completedScopes int
		if err := rows.Scan(&id, &completedScopes); err != nil {
			rows.Close()
			return err
		}
		row := stats[id]
		row.completedScopes = completedScopes
		stats[id] = row
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for i := range campaigns {
		row := stats[campaigns[i].CampaignID]
		campaigns[i].Progress = progress(campaigns[i].Sheds, row.completedAnimals, row.completedScopes, row.wrongShed, row.missing)
	}
	return nil
}

func (r *Repository) progressStats(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) (completedAnimals int, completedScopes int, wrongShed int, missing int, err error) {
	err = tx.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE status = 'weighed')::int AS completed_animals,
  count(*) FILTER (WHERE availability_status = 'moved_other_shed')::int AS wrong_shed,
  count(*) FILTER (
    WHERE availability_status IN ('dead', 'sold_transferred')
      OR current_lifecycle_status IN ('dead', 'sold', 'transferred')
  )::int AS missing
FROM weighing_expected_animals
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, tenantID, campaignID).
		Scan(&completedAnimals, &wrongShed, &missing)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	err = tx.QueryRow(ctx, `
SELECT count(DISTINCT wso.campaign_shed_id)::int
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=wso.tenant_id
 AND cs.campaign_id=wso.campaign_id
 AND cs.campaign_shed_id=wso.campaign_shed_id
 AND cs.weighing_category='per_shed_partition'
WHERE wso.tenant_id=$1::uuid AND wso.campaign_id=$2::uuid`, tenantID, campaignID).
		Scan(&completedScopes)
	return completedAnimals, completedScopes, wrongShed, missing, err
}

func (r *Repository) campaignByIdempotency(ctx context.Context, tx pgx.Tx, tenantID, eventType, idem string) (domain.Campaign, bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT aggregate_id::text FROM outbox_messages WHERE tenant_id=$1::uuid AND event_type=$2 AND idempotency_key=$3`, tenantID, eventType, idem).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, false, nil
	}
	if err != nil {
		return domain.Campaign{}, false, err
	}
	c, err := r.getCampaignTx(ctx, tx, tenantID, id)
	return c, true, err
}

func (r *Repository) observationByIdem(ctx context.Context, tx pgx.Tx, tenantID, idem string) (domain.Observation, error) {
	obs, ok, err := r.observationByIdemTx(ctx, tx, tenantID, idem)
	if err != nil {
		return domain.Observation{}, err
	}
	if !ok {
		return domain.Observation{}, ports.ErrNotFound
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) observationByIdemTx(ctx context.Context, tx pgx.Tx, tenantID, idem string) (domain.Observation, bool, error) {
	var obs domain.Observation
	err := tx.QueryRow(ctx, `SELECT observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,''), animal_id::text, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,''), COALESCE(actual_location_id::text,''), COALESCE(actual_location_label,''), accepted_at FROM weighing_observations WHERE tenant_id=$1::uuid AND idempotency_key=$2`, tenantID, idem).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	if err == nil {
		return obs, true, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, false, err
	}
	err = tx.QueryRow(ctx, `SELECT shed_observation_id::text, campaign_id::text, campaign_shed_id::text, weight_kg::float8, proof_artifact_id::text, accepted_at FROM weighing_shed_observations WHERE tenant_id=$1::uuid AND idempotency_key=$2`, tenantID, idem).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.ProofArtifactID, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, false, nil
	}
	if err != nil {
		return domain.Observation{}, false, err
	}
	return obs, true, nil
}

func (r *Repository) classifyAnimalObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) error {
	var status, operatorID string
	err := tx.QueryRow(ctx, `SELECT status, operator_user_id::text FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, cmd.TenantID, cmd.CampaignID).
		Scan(&status, &operatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != domain.StatusPublished && status != domain.StatusInProgress {
		return ports.ErrImmutable
	}
	if operatorID != cmd.RecordedBy {
		return ports.ErrForbidden
	}
	var animalExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid)`, cmd.TenantID, cmd.AnimalID).Scan(&animalExists); err != nil {
		return err
	}
	if !animalExists {
		return ports.ErrNotFound
	}
	var proofOK bool
	err = tx.QueryRow(ctx, `
	SELECT EXISTS (
	  SELECT 1
	  FROM goats g
	  LEFT JOIN weighing_expected_animals ea
	    ON ea.tenant_id=$1::uuid
	   AND ea.campaign_id=$2::uuid
	   AND ea.animal_id=g.goat_id
	  JOIN proof_artifacts proof
	    ON proof.tenant_id=$1::uuid
	   AND proof.proof_id=$3::uuid
	   AND proof.upload_state='completed'
	   AND proof.proof_type='video'
	   AND proof.subject_type='goat'
	   AND proof.subject_id=g.goat_id
	   AND (
	     (proof.scope_type='goat' AND proof.scope_id=g.goat_id)
	     OR (
	       proof.scope_type='shed'
	       AND proof.scope_id IN (ea.expected_location_id, COALESCE(NULLIF($4, '')::uuid, g.current_location_id))
	     )
	   )
	  WHERE g.tenant_id=$1::uuid AND g.goat_id=$5::uuid
	)`, cmd.TenantID, cmd.CampaignID, cmd.ProofArtifactID, cmd.ActualLocationID, cmd.AnimalID).Scan(&proofOK)
	if err != nil {
		return err
	}
	if !proofOK {
		return ports.ErrInvalidArgument
	}
	return ports.ErrNotFound
}

func (r *Repository) classifyShedObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordShedObservation) error {
	var status, operatorID string
	err := tx.QueryRow(ctx, `SELECT status, operator_user_id::text FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, cmd.TenantID, cmd.CampaignID).
		Scan(&status, &operatorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != domain.StatusPublished && status != domain.StatusInProgress {
		return ports.ErrImmutable
	}
	if operatorID != cmd.RecordedBy {
		return ports.ErrForbidden
	}
	var category string
	var proofOK bool
	err = tx.QueryRow(ctx, `
	SELECT cs.weighing_category,
	  EXISTS (
	    SELECT 1
	    FROM proof_artifacts proof
	    WHERE proof.tenant_id=$1::uuid
	      AND proof.proof_id=$4::uuid
	      AND proof.upload_state='completed'
	      AND proof.proof_type='video'
	      AND proof.scope_type='shed'
	      AND proof.scope_id=cs.location_id
	      AND proof.subject_type='shed'
	      AND proof.subject_id=cs.location_id
	  ) AS proof_ok
	FROM weighing_campaign_sheds cs
	WHERE cs.tenant_id=$1::uuid
	  AND cs.campaign_id=$2::uuid
	  AND cs.campaign_shed_id=$3::uuid`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.ProofArtifactID).
		Scan(&category, &proofOK)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if category != domain.CategoryPerShedPartition {
		return ports.ErrNotFound
	}
	if !proofOK {
		return ports.ErrInvalidArgument
	}
	return ports.ErrNotFound
}

func (r *Repository) enqueue(ctx context.Context, tx pgx.Tx, tenantID, eventType, aggregateID, idem string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := deterministicUUID(eventType + ":" + tenantID + ":" + idem)
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id, topic, payload, headers, idempotency_key, status)
VALUES ($1::uuid, $2::uuid, $3, 'v1', 'weighing', $4::uuid, 'domain-events', $5::jsonb, '{}'::jsonb, $6, 'pending')
ON CONFLICT DO NOTHING`, tenantID, eventID, eventType, aggregateID, string(raw), idem)
	return err
}

func progress(sheds []domain.CampaignShed, completedAnimals, completedScopes, wrongShed, missing int) domain.Progress {
	p := domain.Progress{}
	for _, shed := range sheds {
		switch shed.WeighingCategory {
		case domain.CategoryIndividualAnimal:
			p.IndividualExpectedCount += shed.ExpectedAnimalCount
		case domain.CategoryPerShedPartition:
			p.PerScopeExpectedCount++
		}
	}
	p.IndividualCompletedCount = completedAnimals
	p.PerScopeCompletedCount = completedScopes
	p.WrongShedCount = wrongShed
	p.MissingCount = missing
	p.RemainingCount = (p.IndividualExpectedCount - p.IndividualCompletedCount) + (p.PerScopeExpectedCount - p.PerScopeCompletedCount)
	if p.RemainingCount < 0 {
		p.RemainingCount = 0
	}
	return p
}

func deterministicUUID(s string) string {
	sum := sha256.Sum256([]byte(s))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

type campaignCursor struct {
	PeriodStartDate string    `json:"period_start_date"`
	CreatedAt       time.Time `json:"created_at"`
	CampaignID      string    `json:"campaign_id"`
}

type rosterCursor struct {
	CreatedAt time.Time `json:"created_at"`
	AnimalID  string    `json:"animal_id"`
}

func encodeCampaignCursor(cursor campaignCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCampaignCursor(value string) (campaignCursor, error) {
	if strings.TrimSpace(value) == "" {
		return campaignCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return campaignCursor{}, err
	}
	var cursor campaignCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return campaignCursor{}, err
	}
	if cursor.PeriodStartDate == "" || cursor.CreatedAt.IsZero() || cursor.CampaignID == "" {
		return campaignCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

func encodeRosterCursor(cursor rosterCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeRosterCursor(value string) (rosterCursor, error) {
	if strings.TrimSpace(value) == "" {
		return rosterCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return rosterCursor{}, err
	}
	var cursor rosterCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return rosterCursor{}, err
	}
	if cursor.CreatedAt.IsZero() || cursor.AnimalID == "" {
		return rosterCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func nullUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
