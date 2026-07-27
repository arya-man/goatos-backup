package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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
	c.Progress = progress(c.Sheds, nil, nil)
	return c, nil
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

func (r *Repository) ListCampaigns(ctx context.Context, tenantID string) ([]domain.Campaign, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	rows, err := r.pool.Query(ctx, `SELECT campaign_id::text FROM weighing_campaigns WHERE tenant_id=$1::uuid ORDER BY period_start_date DESC, created_at DESC LIMIT 100`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Campaign
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		c, err := r.getCampaign(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, limit int) ([]domain.ExpectedAnimal, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 250
	}
	if limit > 5000 {
		limit = 5000
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
    row_number() OVER (ORDER BY ea.created_at, ea.animal_id) AS seq
  FROM weighing_expected_animals ea
  JOIN goats g ON g.tenant_id=ea.tenant_id AND g.goat_id=ea.animal_id
  WHERE ea.tenant_id=$1::uuid
    AND ea.campaign_id=$2::uuid
    AND ea.campaign_shed_id=$3::uuid
  ORDER BY ea.created_at, ea.animal_id
  LIMIT $4
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
  scoped.seq::bigint
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
ORDER BY scoped.seq`, tenantID, campaignID, campaignShedID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ExpectedAnimal, 0, limit)
	for rows.Next() {
		var animal domain.ExpectedAnimal
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
		); err != nil {
			return nil, err
		}
		out = append(out, animal)
	}
	return out, rows.Err()
}

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
WITH expected AS (
  SELECT ea.campaign_shed_id, ea.expected_location_id, ea.expected_location_label, g.current_location_id AS actual_location_id, l.name AS actual_location_label
  FROM goats g
  LEFT JOIN weighing_expected_animals ea ON ea.tenant_id=$1::uuid AND ea.campaign_id=$2::uuid AND ea.animal_id=g.goat_id
  LEFT JOIN locations l ON l.tenant_id=g.tenant_id AND l.location_id=g.current_location_id
  WHERE g.tenant_id=$1::uuid AND g.goat_id=$3::uuid
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
		cmd.TenantID, cmd.CampaignID, cmd.AnimalID, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.observationByIdem(ctx, tx, cmd.TenantID, cmd.IdempotencyKey)
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
	var obs domain.Observation
	err = tx.QueryRow(ctx, `
INSERT INTO weighing_shed_observations (tenant_id, campaign_id, campaign_shed_id, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
SELECT $1::uuid, $2::uuid, $3::uuid, $4, $5::uuid, $7::uuid, $6
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid AND cs.campaign_shed_id=$3::uuid AND cs.weighing_category='per_shed_partition'
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
RETURNING shed_observation_id::text, campaign_id::text, campaign_shed_id::text, weight_kg::float8, proof_artifact_id::text, accepted_at`,
		cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.ProofArtifactID, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r.observationByIdem(ctx, tx, cmd.TenantID, cmd.IdempotencyKey)
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
	_, err := r.pool.Exec(ctx, `
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
WHERE ea.tenant_id=$1::uuid AND ea.campaign_id=$2::uuid AND g.tenant_id=ea.tenant_id AND g.goat_id=ea.animal_id`, tenantID, campaignID)
	return err
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
	c.Progress = progress(c.Sheds, nil, nil)
	return c, rows.Err()
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
	var obs domain.Observation
	err := tx.QueryRow(ctx, `SELECT observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,''), animal_id::text, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,''), COALESCE(actual_location_id::text,''), COALESCE(actual_location_label,''), accepted_at FROM weighing_observations WHERE tenant_id=$1::uuid AND idempotency_key=$2`, tenantID, idem).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.AnimalID, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
	if err == nil {
		return obs, tx.Commit(ctx)
	}
	err = tx.QueryRow(ctx, `SELECT shed_observation_id::text, campaign_id::text, campaign_shed_id::text, weight_kg::float8, proof_artifact_id::text, accepted_at FROM weighing_shed_observations WHERE tenant_id=$1::uuid AND idempotency_key=$2`, tenantID, idem).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.WeightKg, &obs.ProofArtifactID, &obs.AcceptedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Observation{}, err
	}
	return obs, tx.Commit(ctx)
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

func progress(sheds []domain.CampaignShed, _ []domain.ExpectedAnimal, _ []domain.Observation) domain.Progress {
	p := domain.Progress{}
	for _, shed := range sheds {
		switch shed.WeighingCategory {
		case domain.CategoryIndividualAnimal:
			p.IndividualExpectedCount += shed.ExpectedAnimalCount
		case domain.CategoryPerShedPartition:
			p.PerScopeExpectedCount++
			if shed.Status == "completed" {
				p.PerScopeCompletedCount++
			}
		}
	}
	p.RemainingCount = (p.IndividualExpectedCount - p.IndividualCompletedCount) + (p.PerScopeExpectedCount - p.PerScopeCompletedCount)
	return p
}

func deterministicUUID(s string) string {
	sum := sha256.Sum256([]byte(s))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[0:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:16]))
}

func nullUUID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
