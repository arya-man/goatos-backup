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

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
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
	fingerprint := idempotencyFingerprint(cmd)
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_created", cmd.IdempotencyKey, fingerprint); err != nil || ok {
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
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_created", cmd.IdempotencyKey, fingerprint, "weighing_campaign", c.CampaignID, c); err != nil {
		return domain.Campaign{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.campaign_created", c.CampaignID, cmd.IdempotencyKey, fingerprint, c); err != nil {
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
	fingerprint := idempotencyFingerprint(struct {
		CampaignID string
		Command    domain.UpdateCampaign
	}{CampaignID: campaignID, Command: cmd})
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_updated", cmd.IdempotencyKey, fingerprint); err != nil || ok {
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
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.campaign_updated", cmd.IdempotencyKey, fingerprint, "weighing_campaign", campaignID, c); err != nil {
		return domain.Campaign{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.campaign_updated", campaignID, cmd.IdempotencyKey, fingerprint, c); err != nil {
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
	fingerprint := idempotencyFingerprint(map[string]string{"campaign_id": campaignID, "published_by": actorID})
	if existing, ok, err := r.campaignByIdempotency(ctx, tx, tenantID, "weighing.campaign_published", idempotencyKey, fingerprint); err != nil || ok {
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
	if err := r.recordIdempotency(ctx, tx, tenantID, "weighing.campaign_published", idempotencyKey, fingerprint, "weighing_campaign", campaignID, c); err != nil {
		return domain.Campaign{}, err
	}
	if err := r.enqueue(ctx, tx, tenantID, "weighing.campaign_published", campaignID, idempotencyKey, fingerprint, map[string]string{"campaign_id": campaignID, "published_by": actorID}); err != nil {
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
	observations := make([]domain.Observation, 0)
	observationRows, err := r.pool.Query(ctx, `
SELECT observation_id::text, campaign_id::text, campaign_shed_id::text,
       COALESCE(NULLIF(scanned_identifier, ''), animal_id::text),
       weight_kg::float8, proof_artifact_id::text,
       COALESCE(expected_location_id::text, ''), accepted_at
FROM weighing_observations
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND (
    animal_id IS NULL
    OR animal_id = ANY($4::uuid[])
  )
ORDER BY accepted_at, observation_id`, tenantID, campaignID, campaignShedID, rosterAnimalIDs(out))
	if err != nil {
		return domain.RosterPage{}, err
	}
	defer observationRows.Close()
	for observationRows.Next() {
		var observation domain.Observation
		if err := observationRows.Scan(
			&observation.ObservationID,
			&observation.CampaignID,
			&observation.CampaignShedID,
			&observation.AnimalID,
			&observation.WeightKg,
			&observation.ProofArtifactID,
			&observation.ExpectedLocationID,
			&observation.AcceptedAt,
		); err != nil {
			return domain.RosterPage{}, err
		}
		observations = append(observations, observation)
	}
	if err := observationRows.Err(); err != nil {
		return domain.RosterPage{}, err
	}
	return domain.RosterPage{Items: out, Observations: observations, NextCursor: nextCursor}, nil
}

func (r *Repository) GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID string) (domain.LeadershipShedVideos, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()

	var result domain.LeadershipShedVideos
	err := r.pool.QueryRow(ctx, `
SELECT campaign_id::text, campaign_shed_id::text, display_name, weighing_category, status
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid`,
		tenantID, campaignID, campaignShedID,
	).Scan(&result.CampaignID, &result.CampaignShedID, &result.ShedName, &result.WeighingCategory, &result.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LeadershipShedVideos{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	result.Individual = []domain.Observation{}

	if result.WeighingCategory == "per_shed_partition" {
		var lump domain.Observation
		err = r.pool.QueryRow(ctx, `
SELECT
  wso.shed_observation_id::text,
  wso.campaign_id::text,
  wso.campaign_shed_id::text,
  wso.weight_kg::float8,
  wso.average_weight_kg::float8,
  wso.animal_count,
  wso.proof_artifact_id::text,
  COALESCE(
    (SELECT array_agg(p.proof_artifact_id::text ORDER BY p.proof_position)
       FROM weighing_shed_observation_proofs p
      WHERE p.tenant_id=wso.tenant_id AND p.shed_observation_id=wso.shed_observation_id),
    ARRAY[wso.proof_artifact_id::text]
  ),
  wso.accepted_at
FROM weighing_shed_observations wso
WHERE wso.tenant_id=$1::uuid AND wso.campaign_id=$2::uuid AND wso.campaign_shed_id=$3::uuid
ORDER BY wso.accepted_at DESC
LIMIT 1`, tenantID, campaignID, campaignShedID).Scan(
			&lump.ObservationID,
			&lump.CampaignID,
			&lump.CampaignShedID,
			&lump.WeightKg,
			&lump.AverageWeightKg,
			&lump.AnimalCount,
			&lump.ProofArtifactID,
			&lump.ProofArtifactIDs,
			&lump.AcceptedAt,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		if err != nil {
			return domain.LeadershipShedVideos{}, err
		}
		result.LumpSum = &lump
		return result, nil
	}

	rows, err := r.pool.Query(ctx, `
SELECT
  observation_id::text,
  campaign_id::text,
  campaign_shed_id::text,
  COALESCE(NULLIF(scanned_identifier, ''), animal_id::text),
  weight_kg::float8,
  proof_artifact_id::text,
  accepted_at
FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
ORDER BY accepted_at, observation_id`, tenantID, campaignID, campaignShedID)
	if err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var observation domain.Observation
		if err := rows.Scan(
			&observation.ObservationID,
			&observation.CampaignID,
			&observation.CampaignShedID,
			&observation.AnimalID,
			&observation.WeightKg,
			&observation.ProofArtifactID,
			&observation.AcceptedAt,
		); err != nil {
			return domain.LeadershipShedVideos{}, err
		}
		observation.ProofArtifactIDs = []string{observation.ProofArtifactID}
		result.Individual = append(result.Individual, observation)
	}
	if err := rows.Err(); err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	return result, nil
}

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	fingerprint := idempotencyFingerprint(cmd)
	if existing, ok, err := r.observationByIdemTx(ctx, tx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint); err != nil || ok {
		if err != nil {
			return domain.Observation{}, err
		}
		return existing, tx.Commit(ctx)
	}
	if !uuidutil.IsUUIDString(cmd.AnimalID) {
		return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
	}
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
	WITH campaign AS (
	  SELECT campaign_id
	  FROM weighing_campaigns
	  WHERE tenant_id=$1::uuid
	    AND campaign_id=$2::uuid
	    AND status IN ('published','in_progress','delayed')
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
		  JOIN weighing_expected_animals ea
		    ON ea.tenant_id=$1::uuid
		   AND ea.campaign_id=$2::uuid
		   AND ea.campaign_shed_id=$9::uuid
		   AND ea.animal_id=g.goat_id
		   AND ea.status <> 'unavailable'
		   AND ea.availability_status NOT IN ('icu','quarantine','dead','culled','sold_transferred','exited')
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
	   AND proof.scope_type='goat'
	   AND proof.scope_id=$3::uuid
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
		cmd.TenantID, cmd.CampaignID, cmd.AnimalID, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy, cmd.ActualLocationID, cmd.CampaignShedID).
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
		if err := r.completeIndividualScopeIfDone(ctx, tx, cmd.TenantID, cmd.CampaignID, obs.CampaignShedID); err != nil {
			return domain.Observation{}, err
		}
	}
	if err := r.completeCampaignIfDone(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
		return domain.Observation{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint, "weighing_observation", obs.ObservationID, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.observation_accepted", obs.ObservationID, cmd.IdempotencyKey, fingerprint, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := r.auditAnimalObservation(ctx, tx, cmd, nil, obs); err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) recordUnknownAnimalObservationTx(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	tag := strings.TrimSpace(cmd.ScannedIdentifier)
	before, err := r.unknownAnimalObservationBefore(ctx, tx, cmd, tag)
	if err != nil {
		return domain.Observation{}, err
	}
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
WITH campaign AS (
  SELECT campaign_id
  FROM weighing_campaigns
  WHERE tenant_id=$1::uuid
    AND campaign_id=$2::uuid
    AND status IN ('published','in_progress','delayed')
    AND operator_user_id=$7::uuid
), assigned_shed AS (
  SELECT campaign_shed_id, location_id, display_name
  FROM weighing_campaign_sheds
  WHERE tenant_id=$1::uuid
    AND campaign_id=$2::uuid
    AND campaign_shed_id=$8::uuid
    AND status <> 'canceled'
  ORDER BY created_at, campaign_shed_id
  LIMIT 1
), proof_ok AS (
  SELECT proof_id
  FROM proof_artifacts proof
  WHERE proof.tenant_id=$1::uuid
    AND proof.proof_id=$5::uuid
    AND proof.upload_state='completed'
    AND proof.proof_type='video'
	), updated AS (
	  UPDATE weighing_observations observation
	  SET weight_kg=$4,
	      proof_artifact_id=p.proof_id,
	      recorded_by=$7::uuid,
	      accepted_at=now()
	  FROM campaign c
	  JOIN assigned_shed s ON true
	  JOIN proof_ok p ON true
	  WHERE observation.tenant_id=$1::uuid
    AND observation.campaign_id=$2::uuid
	    AND observation.campaign_shed_id=s.campaign_shed_id
	    AND observation.animal_id IS NULL
	    AND lower(observation.scanned_identifier)=lower($3)
  RETURNING observation.observation_id::text, observation.campaign_id::text,
    COALESCE(observation.campaign_shed_id::text,'') AS campaign_shed_id_text,
    observation.scanned_identifier AS animal_id_text, observation.weight_kg::float8,
    observation.proof_artifact_id::text,
    COALESCE(observation.expected_location_id::text,'') AS expected_location_id_text,
    '' AS actual_location_id_text, '' AS actual_location_label_text, observation.accepted_at
), inserted AS (
  INSERT INTO weighing_observations (
    tenant_id, campaign_id, campaign_shed_id, animal_id, scanned_identifier,
    weight_kg, proof_artifact_id, expected_location_id, expected_location_label,
    mismatch_status, recorded_by, idempotency_key
  )
  SELECT $1::uuid, $2::uuid, s.campaign_shed_id, NULL, $3,
    $4, p.proof_id, s.location_id, s.display_name,
    'extra_scan', $7::uuid, $6
  FROM campaign c
  JOIN assigned_shed s ON true
  JOIN proof_ok p ON true
  WHERE NOT EXISTS (SELECT 1 FROM updated)
  ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
  RETURNING observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,'') AS campaign_shed_id_text, COALESCE(animal_id::text, scanned_identifier) AS animal_id_text, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,'') AS expected_location_id_text, '' AS actual_location_id_text, '' AS actual_location_label_text, accepted_at
)
SELECT observation_id, campaign_id, campaign_shed_id_text, animal_id_text, weight_kg, proof_artifact_id, expected_location_id_text, actual_location_id_text, actual_location_label_text, accepted_at FROM updated
UNION ALL
SELECT observation_id, campaign_id, campaign_shed_id_text, animal_id_text, weight_kg, proof_artifact_id::text, expected_location_id_text, actual_location_id_text, actual_location_label_text, accepted_at FROM inserted`,
		cmd.TenantID, cmd.CampaignID, tag, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy, cmd.CampaignShedID).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Observation{}, err
	}
	if err := r.completeCampaignIfDone(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
		return domain.Observation{}, err
	}
	fingerprint := idempotencyFingerprint(cmd)
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint, "weighing_observation", obs.ObservationID, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.observation_accepted", obs.ObservationID, cmd.IdempotencyKey, fingerprint, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := r.auditAnimalObservation(ctx, tx, cmd, before, obs); err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	if cmd.AverageWeightKg <= 0 {
		cmd.AverageWeightKg = cmd.WeightKg
	}
	if len(cmd.ProofArtifactIDs) == 0 && cmd.ProofArtifactID != "" {
		cmd.ProofArtifactIDs = []string{cmd.ProofArtifactID}
	}
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	fingerprint := idempotencyFingerprint(cmd)
	if existing, ok, err := r.observationByIdemTx(ctx, tx, cmd.TenantID, "weighing.shed_observation_accepted", cmd.IdempotencyKey, fingerprint); err != nil || ok {
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
	    AND status IN ('published','in_progress','delayed')
	  AND operator_user_id=$7::uuid
	), scope AS (
	  SELECT cs.campaign_shed_id, cs.location_id
	  FROM campaign c
	  JOIN weighing_campaign_sheds cs
	    ON cs.tenant_id=$1::uuid
	   AND cs.campaign_id=c.campaign_id
	   AND cs.campaign_shed_id=$3::uuid
	   AND cs.weighing_category='per_shed_partition'
	), proof_bundle AS (
	  SELECT array_agg(proof.proof_id ORDER BY requested.proof_position) AS proof_ids
	  FROM scope
	  CROSS JOIN unnest($5::uuid[]) WITH ORDINALITY AS requested(proof_id, proof_position)
	  JOIN proof_artifacts proof
	    ON proof.tenant_id=$1::uuid
	   AND proof.proof_id=requested.proof_id
	   AND proof.upload_state='completed'
	   AND proof.proof_type='video'
	   AND proof.scope_type='shed'
	   AND proof.scope_id=scope.location_id
	   AND proof.subject_type='shed'
	   AND proof.subject_id=scope.location_id
	  HAVING count(*)=cardinality($5::uuid[])
	     AND count(*) BETWEEN 1 AND 5
	)
	INSERT INTO weighing_shed_observations (
	  tenant_id, campaign_id, campaign_shed_id, weight_kg, average_weight_kg, animal_count,
	  proof_artifact_id, recorded_by, idempotency_key
	)
	SELECT $1::uuid, $2::uuid, scope.campaign_shed_id, $4, $8,
	  $9, proof_bundle.proof_ids[1], $7::uuid, $6
	FROM scope
	JOIN proof_bundle ON true
	ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
	RETURNING shed_observation_id::text, campaign_id::text, campaign_shed_id::text,
	  weight_kg::float8, average_weight_kg::float8, animal_count, proof_artifact_id::text, accepted_at`,
		cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.WeightKg, cmd.ProofArtifactIDs, cmd.IdempotencyKey, cmd.RecordedBy, cmd.AverageWeightKg, cmd.AnimalCount).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.AverageWeightKg, &obs.AnimalCount, &obs.ProofArtifactID, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, r.classifyShedObservationRejection(ctx, tx, cmd)
	}
	if err != nil {
		return domain.Observation{}, err
	}
	obs.ProofArtifactIDs = append([]string(nil), cmd.ProofArtifactIDs...)
	if _, err := tx.Exec(ctx, `
INSERT INTO weighing_shed_observation_proofs (
  shed_observation_id, tenant_id, proof_artifact_id, proof_position
)
SELECT $1::uuid, $2::uuid, requested.proof_id, requested.proof_position
FROM unnest($3::uuid[]) WITH ORDINALITY AS requested(proof_id, proof_position)
ON CONFLICT (shed_observation_id, proof_position) DO NOTHING`,
		obs.ObservationID, cmd.TenantID, cmd.ProofArtifactIDs); err != nil {
		return domain.Observation{}, err
	}
	completed, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='completed', completed_at=COALESCE(completed_at, now()), updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status <> 'completed'`, cmd.TenantID, cmd.CampaignShedID)
	if err != nil {
		return domain.Observation{}, err
	}
	if completed.RowsAffected() > 0 {
		if err := r.enqueueShedSubmissionCompleted(ctx, tx, cmd.TenantID, cmd.CampaignShedID); err != nil {
			return domain.Observation{}, err
		}
	}
	if err := r.completeCampaignIfDone(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
		return domain.Observation{}, err
	}
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.shed_observation_accepted", cmd.IdempotencyKey, fingerprint, "weighing_shed_observation", obs.ObservationID, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.shed_observation_accepted", obs.ObservationID, cmd.IdempotencyKey, fingerprint, obs); err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) completeIndividualScopeIfDone(ctx context.Context, tx pgx.Tx, tenantID, campaignID, campaignShedID string) error {
	completed, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds cs
SET status='completed',
  completed_at=COALESCE(cs.completed_at, now()),
  updated_at=now()
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
  AND cs.weighing_category='individual_animal'
  AND cs.status <> 'completed'
  AND NOT EXISTS (
    SELECT 1
    FROM weighing_expected_animals ea
    WHERE ea.tenant_id=cs.tenant_id
      AND ea.campaign_id=cs.campaign_id
      AND ea.campaign_shed_id=cs.campaign_shed_id
      AND ea.status NOT IN ('weighed', 'unavailable', 'canceled', 'closed_by_override')
  )`, tenantID, campaignID, campaignShedID)
	if err != nil || completed.RowsAffected() == 0 {
		return err
	}
	return r.enqueueShedSubmissionCompleted(ctx, tx, tenantID, campaignShedID)
}

func (r *Repository) SubmitIndividualScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey string, scannedIdentifiers []string) error {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	fingerprint := idempotencyFingerprint(map[string]any{
		"campaign_id":         campaignID,
		"campaign_shed_id":    campaignShedID,
		"submitted_by":        actorID,
		"scanned_identifiers": scannedIdentifiers,
	})
	if _, resourceType, _, ok, err := r.idempotencyResource(ctx, tx, tenantID, "weighing.individual_scope_submitted", idempotencyKey, fingerprint); err != nil || ok {
		if err != nil {
			return err
		}
		if resourceType != "weighing_campaign_shed" {
			return ports.ErrIdempotencyConflict
		}
		return tx.Commit(ctx)
	}
	result, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds cs
SET status='completed', completed_at=COALESCE(cs.completed_at, now()), updated_at=now()
FROM weighing_campaigns campaign
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
  AND cs.weighing_category='individual_animal'
  AND campaign.tenant_id=cs.tenant_id
  AND campaign.campaign_id=cs.campaign_id
  AND campaign.operator_user_id=$4::uuid
  AND cardinality($5::text[]) > 0
  AND NOT EXISTS (
    SELECT 1
    FROM unnest($5::text[]) AS captured(scanned_identifier)
    WHERE NOT EXISTS (
      SELECT 1 FROM weighing_observations observation
      JOIN proof_artifacts proof
        ON proof.tenant_id=observation.tenant_id
       AND proof.proof_id=observation.proof_artifact_id
       AND proof.upload_state='completed'
       AND proof.proof_type='video'
    WHERE observation.tenant_id=cs.tenant_id
      AND observation.campaign_id=cs.campaign_id
      AND observation.campaign_shed_id=cs.campaign_shed_id
      AND observation.scanned_identifier=captured.scanned_identifier
      AND observation.weight_kg > 0
    )
  )
  AND NOT EXISTS (
    SELECT 1
    FROM weighing_expected_animals expected
    WHERE expected.tenant_id=cs.tenant_id
      AND expected.campaign_id=cs.campaign_id
      AND expected.campaign_shed_id=cs.campaign_shed_id
      AND expected.status NOT IN ('weighed', 'unavailable', 'canceled')
      AND NOT EXISTS (
        SELECT 1
        FROM weighing_observations observation
        JOIN proof_artifacts proof
          ON proof.tenant_id=observation.tenant_id
         AND proof.proof_id=observation.proof_artifact_id
         AND proof.upload_state='completed'
         AND proof.proof_type='video'
        WHERE observation.tenant_id=expected.tenant_id
          AND observation.campaign_id=expected.campaign_id
          AND observation.campaign_shed_id=expected.campaign_shed_id
          AND observation.animal_id=expected.animal_id
          AND observation.weight_kg > 0
      )
  )`, tenantID, campaignID, campaignShedID, actorID, scannedIdentifiers)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ports.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `
UPDATE weighing_expected_animals
SET status='closed_by_override', updated_at=now()
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND status NOT IN ('weighed', 'unavailable', 'canceled')`, tenantID, campaignID, campaignShedID); err != nil {
		return err
	}
	if err := r.enqueueShedSubmissionCompleted(ctx, tx, tenantID, campaignShedID); err != nil {
		return err
	}
	if err := r.completeCampaignIfDone(ctx, tx, tenantID, campaignID); err != nil {
		return err
	}
	if err := r.recordIdempotency(ctx, tx, tenantID, "weighing.individual_scope_submitted", idempotencyKey, fingerprint, "weighing_campaign_shed", campaignShedID, map[string]any{
		"campaign_id":         campaignID,
		"campaign_shed_id":    campaignShedID,
		"submitted_by":        actorID,
		"scanned_identifiers": scannedIdentifiers,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rosterAnimalIDs(rows []domain.ExpectedAnimal) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.AnimalID != "" {
			ids = append(ids, row.AnimalID)
		}
	}
	return ids
}

func (r *Repository) completeCampaignIfDone(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) error {
	_, err := tx.Exec(ctx, `
UPDATE weighing_campaigns wc
SET status='completed',
  completed_at=COALESCE(wc.completed_at, now()),
  updated_at=now(),
  row_version=row_version+1
WHERE wc.tenant_id=$1::uuid
  AND wc.campaign_id=$2::uuid
  AND wc.status IN ('published', 'in_progress', 'delayed')
  AND EXISTS (
    SELECT 1
    FROM weighing_campaign_sheds cs
    WHERE cs.tenant_id=wc.tenant_id
      AND cs.campaign_id=wc.campaign_id
      AND cs.status <> 'canceled'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM weighing_campaign_sheds cs
    WHERE cs.tenant_id=wc.tenant_id
      AND cs.campaign_id=wc.campaign_id
      AND cs.status NOT IN ('completed', 'canceled')
  )`, tenantID, campaignID)
	return err
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
			return r.completeResolvedIndividualScopes(ctx, tenantID, campaignID)
		}
		if _, err := r.pool.Exec(ctx, `
UPDATE weighing_expected_animals ea
SET availability_status = CASE
    WHEN g.lifecycle_status IN ('dead') THEN 'dead'
    WHEN g.lifecycle_status IN ('sold','transferred') THEN 'sold_transferred'
    WHEN g.health_status = 'icu' THEN 'icu'
    WHEN g.health_status = 'quarantine' THEN 'quarantine'
    WHEN g.current_location_id IS DISTINCT FROM ea.expected_location_id THEN 'moved_other_shed'
    ELSE 'expected_shed'
  END,
  current_location_id=g.current_location_id,
  current_location_label=l.name,
  current_lifecycle_status=g.lifecycle_status,
  availability_checked_at=now(),
  status=CASE
    WHEN g.lifecycle_status IN ('dead','sold','transferred')
      OR g.health_status IN ('icu','quarantine')
    THEN 'unavailable'
    ELSE ea.status
  END,
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
			return r.completeResolvedIndividualScopes(ctx, tenantID, campaignID)
		}
	}
}

func (r *Repository) completeResolvedIndividualScopes(ctx context.Context, tenantID, campaignID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
SELECT campaign_shed_id::text
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND weighing_category='individual_animal'
  AND status <> 'completed'`, tenantID, campaignID)
	if err != nil {
		return err
	}
	scopeIDs := []string{}
	for rows.Next() {
		var scopeID string
		if err := rows.Scan(&scopeID); err != nil {
			rows.Close()
			return err
		}
		scopeIDs = append(scopeIDs, scopeID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, scopeID := range scopeIDs {
		if err := r.completeIndividualScopeIfDone(ctx, tx, tenantID, campaignID, scopeID); err != nil {
			return err
		}
	}
	if err := r.completeCampaignIfDone(ctx, tx, tenantID, campaignID); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

func (r *Repository) campaignByIdempotency(ctx context.Context, tx pgx.Tx, tenantID, eventType, idem, fingerprint string) (domain.Campaign, bool, error) {
	id, resourceType, _, ok, err := r.idempotencyResource(ctx, tx, tenantID, eventType, idem, fingerprint)
	if err != nil || !ok {
		return domain.Campaign{}, ok, err
	}
	if resourceType != "weighing_campaign" {
		return domain.Campaign{}, true, ports.ErrIdempotencyConflict
	}
	c, err := r.getCampaignTx(ctx, tx, tenantID, id)
	return c, true, err
}

func (r *Repository) observationByIdem(ctx context.Context, tx pgx.Tx, tenantID, idem string) (domain.Observation, error) {
	obs, ok, err := r.observationByIdemTx(ctx, tx, tenantID, "weighing.observation_accepted", idem, "")
	if err != nil {
		return domain.Observation{}, err
	}
	if !ok {
		return domain.Observation{}, ports.ErrNotFound
	}
	return obs, tx.Commit(ctx)
}

func (r *Repository) observationByIdemTx(ctx context.Context, tx pgx.Tx, tenantID, eventType, idem, fingerprint string) (domain.Observation, bool, error) {
	id, resourceType, snapshot, ok, err := r.idempotencyResource(ctx, tx, tenantID, eventType, idem, fingerprint)
	if err != nil || !ok {
		return domain.Observation{}, ok, err
	}
	if len(snapshot) > 0 {
		var obs domain.Observation
		if err := json.Unmarshal(snapshot, &obs); err != nil {
			return domain.Observation{}, true, err
		}
		return obs, true, nil
	}
	var obs domain.Observation
	var queryErr error
	switch resourceType {
	case "weighing_observation":
		queryErr = tx.QueryRow(ctx, `SELECT observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,''), COALESCE(animal_id::text, scanned_identifier), weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,''), COALESCE(actual_location_id::text,''), COALESCE(actual_location_label,''), accepted_at FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, tenantID, id).
			Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	case "weighing_shed_observation":
		queryErr = tx.QueryRow(ctx, `
SELECT
  wso.shed_observation_id::text,
  wso.campaign_id::text,
  wso.campaign_shed_id::text,
  wso.weight_kg::float8,
  wso.average_weight_kg::float8,
  wso.proof_artifact_id::text,
  COALESCE(
    (
      SELECT array_agg(wsop.proof_artifact_id::text ORDER BY wsop.proof_position)
      FROM weighing_shed_observation_proofs wsop
      WHERE wsop.tenant_id=wso.tenant_id
        AND wsop.shed_observation_id=wso.shed_observation_id
    ),
    ARRAY[wso.proof_artifact_id::text]
  ),
  wso.accepted_at
FROM weighing_shed_observations wso
WHERE wso.tenant_id=$1::uuid AND wso.shed_observation_id=$2::uuid`, tenantID, id).
			Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.AverageWeightKg, &obs.ProofArtifactID, &obs.ProofArtifactIDs, &obs.AcceptedAt)
	default:
		return domain.Observation{}, true, ports.ErrIdempotencyConflict
	}
	if queryErr != nil {
		return domain.Observation{}, true, queryErr
	}
	return obs, true, nil
}

func (r *Repository) unknownAnimalObservationBefore(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation, tag string) (*domain.Observation, error) {
	var before domain.Observation
	err := tx.QueryRow(ctx, `
SELECT observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,''), scanned_identifier,
  weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,''),
  COALESCE(actual_location_id::text,''), COALESCE(actual_location_label,''), accepted_at
FROM weighing_observations
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND animal_id IS NULL
  AND lower(scanned_identifier)=lower($4)
ORDER BY accepted_at DESC, observation_id
LIMIT 1`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, tag).
		Scan(&before.ObservationID, &before.CampaignID, &before.CampaignShedID, &before.AnimalID, &before.WeightKg, &before.ProofArtifactID, &before.ExpectedLocationID, &before.ActualLocationID, &before.ActualLocationLabel, &before.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &before, nil
}

func (r *Repository) auditAnimalObservation(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation, before *domain.Observation, after domain.Observation) error {
	action := "weighing.observation_accepted"
	if before != nil {
		weightChanged := before.WeightKg != after.WeightKg
		proofChanged := before.ProofArtifactID != after.ProofArtifactID
		switch {
		case weightChanged && proofChanged:
			action = "weighing.observation_updated"
		case weightChanged:
			action = "weighing.observation_weight_updated"
		case proofChanged:
			action = "weighing.observation_proof_replaced"
		default:
			action = "weighing.observation_reaccepted"
		}
	}
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     cmd.TenantID,
		ActorID:      cmd.RecordedBy,
		ActorType:    "operator",
		Action:       action,
		ResourceType: "weighing_observation",
		ResourceID:   after.ObservationID,
		ScopeType:    "weighing.campaign_shed",
		ScopeID:      after.CampaignShedID,
		BeforeState:  before,
		AfterState:   after,
		Metadata: map[string]any{
			"campaign_id":            cmd.CampaignID,
			"campaign_shed_id":       after.CampaignShedID,
			"animal_id":              cmd.AnimalID,
			"scanned_identifier":     strings.TrimSpace(cmd.ScannedIdentifier),
			"weight_kg":              after.WeightKg,
			"proof_artifact_id":      after.ProofArtifactID,
			"previous_weight_kg":     previousWeight(before),
			"previous_proof_id":      previousProof(before),
			"client_idempotency_key": cmd.IdempotencyKey,
		},
	})
}

func previousWeight(before *domain.Observation) any {
	if before == nil {
		return nil
	}
	return before.WeightKg
}

func previousProof(before *domain.Observation) any {
	if before == nil {
		return nil
	}
	return before.ProofArtifactID
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
	if status != domain.StatusPublished && status != domain.StatusInProgress && status != domain.StatusDelayed {
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
	   AND proof.scope_type='goat'
	   AND proof.scope_id=g.goat_id
	  WHERE g.tenant_id=$1::uuid AND g.goat_id=$4::uuid
	)`, cmd.TenantID, cmd.CampaignID, cmd.ProofArtifactID, cmd.AnimalID).Scan(&proofOK)
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
	if status != domain.StatusPublished && status != domain.StatusInProgress && status != domain.StatusDelayed {
		return ports.ErrImmutable
	}
	if operatorID != cmd.RecordedBy {
		return ports.ErrForbidden
	}
	var category string
	var proofOK bool
	err = tx.QueryRow(ctx, `
	SELECT cs.weighing_category,
	  (
	    SELECT count(*)=cardinality($4::uuid[])
	      AND count(*) BETWEEN 1 AND 5
	    FROM unnest($4::uuid[]) AS requested(proof_id)
	    JOIN proof_artifacts proof
	      ON proof.tenant_id=$1::uuid
	     AND proof.proof_id=requested.proof_id
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
	  AND cs.campaign_shed_id=$3::uuid`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.ProofArtifactIDs).
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

func (r *Repository) idempotencyResource(ctx context.Context, tx pgx.Tx, tenantID, eventType, idem, fingerprint string) (id string, resourceType string, snapshot []byte, ok bool, err error) {
	var storedFingerprint string
	var snapshotText string
	err = tx.QueryRow(ctx, `
SELECT request_fingerprint, resource_type, resource_id::text, COALESCE(result_snapshot::text, '')
FROM weighing_idempotency_records
WHERE tenant_id=$1::uuid AND event_type=$2 AND idempotency_key=$3`, tenantID, eventType, idem).
		Scan(&storedFingerprint, &resourceType, &id, &snapshotText)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", nil, false, nil
	}
	if err != nil {
		return "", "", nil, false, err
	}
	if fingerprint != "" && storedFingerprint != fingerprint {
		return "", "", nil, true, ports.ErrIdempotencyConflict
	}
	if snapshotText != "" {
		snapshot = []byte(snapshotText)
	}
	return id, resourceType, snapshot, true, nil
}

func (r *Repository) recordIdempotency(ctx context.Context, tx pgx.Tx, tenantID, eventType, idem, fingerprint, resourceType, resourceID string, snapshot any) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
INSERT INTO weighing_idempotency_records (
  tenant_id, event_type, idempotency_key, request_fingerprint, resource_type, resource_id, result_snapshot
)
VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7::jsonb)
ON CONFLICT (tenant_id, event_type, idempotency_key) DO NOTHING`,
		tenantID, eventType, idem, fingerprint, resourceType, resourceID, string(raw))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, _, _, _, err := r.idempotencyResource(ctx, tx, tenantID, eventType, idem, fingerprint)
		return err
	}
	return nil
}

func idempotencyFingerprint(payload any) string {
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (r *Repository) enqueue(ctx context.Context, tx pgx.Tx, tenantID, eventType, aggregateID, idem, fingerprint string, payload any) error {
	eventID := deterministicUUID(eventType + ":" + tenantID + ":" + idem)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(map[string]any{
		"event_id":       eventID,
		"event_type":     eventType,
		"schema_version": "1.0.0",
		"schema_ref":     "contracts/jsonschema/domain-event-envelope.schema.json#" + eventType,
		"aggregate_type": "weighing",
		"aggregate_id":   aggregateID,
		"occurred_at":    now,
		"recorded_at":    now,
		"producer": map[string]any{
			"service": "goatos-api",
			"module":  "weighing",
		},
		"idempotency_key": idem,
		"actor": map[string]any{
			"actor_type": "system_rule",
			"actor_ref":  "weighing",
		},
		"subject_type":     weighingSubjectType(eventType),
		"subject_id":       aggregateID,
		"visibility_scope": map[string]any{"tenant_id": tenantID},
		"evidence_refs":    []any{},
		"payload":          payload,
		"trace_id":         idem,
	})
	if err != nil {
		return err
	}
	headers, err := json.Marshal(map[string]string{
		"request_fingerprint": fingerprint,
		"schema_version":      "1.0.0",
		"idempotency_key":     idem,
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id, topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at)
VALUES ($1::uuid, $2::uuid, $3, '1.0.0', 'weighing', $4::uuid, 'domain-events', $5::jsonb, $6::jsonb, $7, $7, 'pending', now())
ON CONFLICT DO NOTHING`, tenantID, eventID, eventType, aggregateID, string(raw), string(headers), idem)
	return err
}

func weighingSubjectType(eventType string) string {
	switch eventType {
	case "weighing.campaign_created", "weighing.campaign_updated", "weighing.campaign_published":
		return "weighing_campaign"
	case "weighing.shed_submission.completed":
		return "weighing_campaign_shed"
	default:
		return "weighing_observation"
	}
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
