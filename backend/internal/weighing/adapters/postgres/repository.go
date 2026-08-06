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
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Repository struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
	proofURLs    ProofURLResolver
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	return &Repository{pool: pool, queryTimeout: queryTimeout}
}

// WithProofURLResolver wires the CSV export's proof-video URL resolution. Without it,
// ExportCampaignCSV still runs -- it just emits the raw storage reference plus a note saying no
// resolver was configured, rather than a clickable URL.
func (r *Repository) WithProofURLResolver(resolver ProofURLResolver) *Repository {
	r.proofURLs = resolver
	return r
}

// assertOperatorsScopedToPark refuses a bucket assigned to someone whose scope does not reach the
// task's park.
//
// Operators are park-scoped by invariant -- an operator belongs to exactly ONE park -- while
// leadership scoped to the tenant (the growth director) legitimately spans parks. Nothing
// downstream re-checks this: the work list filters on operator_user_id alone and the write only
// requires the caller to be the assignee, so a CPT operator handed a CBE bucket would both SEE and
// be able to weigh another park's shed. Blocked here, at the write, rather than trusted to the
// planner UI.
func (r *Repository) assertOperatorsScopedToPark(ctx context.Context, tx pgx.Tx, tenantID, parkID string, sheds []domain.CreateCampaignShed) error {
	ids := make([]string, 0, len(sheds))
	seen := make(map[string]struct{}, len(sheds))
	for _, shed := range sheds {
		id := strings.TrimSpace(shed.OperatorUserID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	// One set-based read, never a query per shed. A person passes when ANY active grant is
	// tenant-wide or names this park.
	rows, err := tx.Query(ctx, `
SELECT u.user_id::text
FROM unnest($3::uuid[]) AS u(user_id)
WHERE NOT EXISTS (
  SELECT 1 FROM user_scope_grants g
  WHERE g.tenant_id=$1::uuid
    AND g.user_id=u.user_id
    AND g.status='active'
    AND (g.scope_type='tenant' OR (g.scope_type='park' AND g.scope_id=$2::uuid))
)`, tenantID, parkID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var offending string
		if err := rows.Scan(&offending); err != nil {
			return err
		}
		return ports.ErrOperatorOutsidePark
	}
	return rows.Err()
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
	if err := r.assertOperatorsScopedToPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.Sheds); err != nil {
		return domain.Campaign{}, err
	}
	var c domain.Campaign
	err = tx.QueryRow(ctx, `
INSERT INTO weighing_campaigns (tenant_id, park_id, period_start_date, period_end_date, start_business_date, planned_cap_per_day, operator_user_id, created_by)
VALUES ($1::uuid,$2::uuid,$3::date,$4::date,$5::date,$6,$7::uuid,$8::uuid)
RETURNING campaign_id::text, tenant_id::text, park_id::text, period_start_date::text, period_end_date::text, start_business_date::text, status, planned_cap_per_day, operator_user_id::text, created_by::text, created_at, updated_at, row_version`,
		cmd.TenantID, cmd.ParkID, cmd.PeriodStartDate, cmd.PeriodEndDate, cmd.StartBusinessDate, cmd.PlannedCapPerDay, cmd.OperatorUserID, cmd.CreatedBy).
		Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion)
	if err != nil {
		// NO park-week uniqueness mapping here on purpose. A park-week may hold
		// SEVERAL tasks: the capture category is a per-BUCKET property, so
		// leadership plans some sheds lump-sum and the park's LEFTOVER sheds as a
		// second task in the same week. The campaign-grain unique index that used
		// to reject that was dropped in migration 000081; the real invariant
		// (one shed is at most one person's open work on one date) is enforced at
		// bucket grain by uq_weighing_open_shed_per_park_date and surfaces below
		// as a typed ShedScheduleConflict that NAMES the blocked sheds -- which is
		// the error the planner can actually act on.
		return domain.Campaign{}, err
	}
	// DUPLICATE WORK BLOCK: one open weighing row per (park, weigh date, shed).
	// Checked before any bucket is written so the whole create fails as one unit
	// with the conflicting bucket names, rather than half-writing a task.
	if conflicts, err := r.shedScheduleConflicts(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.StartBusinessDate, c.CampaignID, createCampaignLocationIDs(cmd.Sheds)); err != nil {
		return domain.Campaign{}, err
	} else if len(conflicts) > 0 {
		return domain.Campaign{}, shedScheduleConflictError(cmd.StartBusinessDate, conflicts)
	}
	// Partition labels come from the shed_partitions CATALOG, resolved ONCE for the whole campaign
	// (never inside the loop -- that would be an N+1 on the campaign write path). A name alone
	// cannot prove a partition: "Castro 2" and "Mandela 1" are the same shape, and only the
	// catalog knows which is a real partition.
	resolvedPartitions, err := resolvePartitionLabels(ctx, tx, cmd.TenantID, createCampaignLocationIDs(cmd.Sheds))
	if err != nil {
		return domain.Campaign{}, err
	}
	for _, shed := range cmd.Sheds {
		var cs domain.CampaignShed
		operatorID := weighingShedOperatorID(cmd.OperatorUserID, shed)
		// FREE-FLOW: weighing has no expected set. This bucket write is the ONLY
		// membership fact -- the selected shed/location becomes a task bucket, full
		// stop. No herd read, no per-goat roster row, no expected count derived from
		// goats/herd_register_is_kid. expected_animal_count is written as 0 -- the
		// column's own default, meaning NO EXPECTATION. It used to be written as a
		// literal 1, which every CEO-side progress figure then read as "this shed
		// expects one animal": a shed where five animals were weighed reported an
		// expectation of one. Nothing derives business truth from this column any
		// more (see progress() and kernel.go); 0 is the only value free-flow can
		// honestly store.
		partitionLabel := partitionLabelFor(resolvedPartitions, shed.LocationID, shed.DisplayName)
		err = tx.QueryRow(ctx, // scale-guard:ignore: bounded planner shed list; each selected bucket is written once during campaign setup
			`
INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, 0,
    -- TASK IDENTITY, denormalized from the campaign so the one-open-row-per
    -- (park, weigh date, shed) unique index can exist at all. Every write path
    -- must set these; migration 000062 fails loudly if one forgets.
    $8::uuid, $9::date, $10)
RETURNING campaign_shed_id::text, $1::text, $3::text, $4, $5, expected_animal_count, $6, $7::text, 'pending'`, c.CampaignID, cmd.TenantID, shed.LocationID, shed.LocationType, shed.DisplayName, shed.WeighingCategory, operatorID, cmd.ParkID, cmd.StartBusinessDate, nullableString(partitionLabel)).
			Scan(&cs.CampaignShedID, &cs.CampaignID, &cs.LocationID, &cs.LocationType, &cs.DisplayName, &cs.ExpectedAnimalCount, &cs.WeighingCategory, &cs.OperatorUserID, &cs.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx /* scale-guard:ignore: bounded planner shed fallback lookup after idempotent insert race */, `SELECT campaign_shed_id::text, campaign_id::text, location_id::text, location_type, display_name, expected_animal_count, weighing_category, operator_user_id::text, status FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND location_id=$3::uuid`, cmd.TenantID, c.CampaignID, shed.LocationID).
				Scan(&cs.CampaignShedID, &cs.CampaignID, &cs.LocationID, &cs.LocationType, &cs.DisplayName, &cs.ExpectedAnimalCount, &cs.WeighingCategory, &cs.OperatorUserID, &cs.Status)
		}
		if err != nil {
			return domain.Campaign{}, mapShedUniqueViolation(err, cmd.StartBusinessDate, shed.DisplayName)
		}
		stampCampaignShedLocation(&cs, partitionLabel)
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

// assertNoFinishedShedBlocksMove refuses an edit that would MOVE a task (its weigh
// date or its park) while any of its buckets is already 'completed' or 'closed'.
//
// It reads the task's CURRENT identity and compares it with the requested one, so
// an ordinary edit -- adding or removing sheds, changing the operator or the cap,
// re-saving the same date -- is untouched. Only a move is refused, and only when
// there is finished work that would be left behind by it.
//
// scale-guard:ignore: single-row campaign lookup plus a bounded read of THIS task's
// finished buckets, one per edit
func (r *Repository) assertNoFinishedShedBlocksMove(ctx context.Context, tx pgx.Tx, tenantID, campaignID, parkID, startBusinessDate string) error {
	var currentPark, currentDate string
	if err := tx.QueryRow(ctx, `
SELECT park_id::text, start_business_date::text
FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, tenantID, campaignID).Scan(&currentPark, &currentDate); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // the update itself reports a missing task
		}
		return err
	}
	movesDate := strings.TrimSpace(startBusinessDate) != "" && startBusinessDate != currentDate
	movesPark := strings.TrimSpace(parkID) != "" && parkID != currentPark
	if !movesDate && !movesPark {
		return nil
	}
	rows, err := tx.Query(ctx, `
SELECT DISTINCT display_name
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status IN ('completed','closed')
ORDER BY display_name`, tenantID, campaignID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var finished []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		finished = append(finished, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(finished) == 0 {
		return nil
	}
	return &ports.FinishedShedConflict{WeighDate: currentDate, Sheds: finished}
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
	// Editing a task must not smuggle in a cross-park assignee either.
	if err := r.assertOperatorsScopedToPark(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.Sheds); err != nil {
		return domain.Campaign{}, err
	}
	// A task that already holds FINISHED work cannot be moved to another day or
	// park. The bucket upsert below deliberately refuses to touch a 'completed' or
	// 'closed' bucket -- a weighed shed records the day it was actually weighed and
	// its proof hangs off that day, so dragging it to a new date would falsify when
	// the work happened. But letting the MOVE succeed anyway is the silent half of
	// the same bug: the campaign lands on Tuesday while the finished bucket still
	// says Monday, and the shed shows up on neither day's task while the screen
	// reports success. Refuse the move and name the sheds; the planner then either
	// drops the finished shed from this task or leaves the task where it is and
	// plans the new date as its own.
	if err := r.assertNoFinishedShedBlocksMove(ctx, tx, cmd.TenantID, campaignID, cmd.ParkID, cmd.StartBusinessDate); err != nil {
		return domain.Campaign{}, err
	}
	// A bucket's weighing category may only change while it holds nothing: the two categories
	// write to different tables and every count sums both, so flipping over captured work double
	// counts the same animals rather than migrating them.
	// Take the SAME campaign row lock every capture writer takes before reading the bucket
	// counts. Without it this is check-then-act: the guard reads "no captures", a concurrent
	// RecordAnimalObservation commits into that bucket, and the flip proceeds anyway --
	// reproducing exactly the double-count this guard exists to prevent. Lock ordering matches
	// lockCampaignRowForNoKeyUpdate's convention (campaign first), so it cannot deadlock against
	// the capture path.
	if err := r.lockCampaignRowForNoKeyUpdate(ctx, tx, cmd.TenantID, campaignID); err != nil {
		return domain.Campaign{}, err
	}
	if err := r.assertNoCategoryFlipOverCapturedWork(ctx, tx, cmd.TenantID, campaignID, cmd.Sheds); err != nil {
		return domain.Campaign{}, err
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
WHERE weighing_campaigns.tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('completed','closed','canceled')`,
		cmd.TenantID, campaignID, cmd.ParkID, cmd.PeriodStartDate, cmd.PeriodEndDate, cmd.StartBusinessDate, cmd.PlannedCapPerDay, cmd.OperatorUserID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Campaign{}, ports.ErrImmutable
	}
	selectedLocationIDs := make([]string, 0, len(cmd.Sheds))
	for _, shed := range cmd.Sheds {
		selectedLocationIDs = append(selectedLocationIDs, shed.LocationID)
	}
	if _, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='canceled', updated_at=now()
-- The predicate must qualify with the table being UPDATEd. Qualifying with
-- weighing_campaigns (which is not in this statement's FROM) made Postgres
-- reject the statement at parse time, so EVERY campaign edit failed.
WHERE weighing_campaign_sheds.tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status NOT IN ('completed', 'closed', 'canceled')
  AND NOT (location_id = ANY($3::uuid[]))`, cmd.TenantID, campaignID, selectedLocationIDs); err != nil {
		return domain.Campaign{}, err
	}
	// DUPLICATE WORK BLOCK, same rule as create. This campaign is excluded from the
	// check: re-saving a shed the task already owns is not a duplicate.
	if conflicts, err := r.shedScheduleConflicts(ctx, tx, cmd.TenantID, cmd.ParkID, cmd.StartBusinessDate, campaignID, createCampaignLocationIDs(cmd.Sheds)); err != nil {
		return domain.Campaign{}, err
	} else if len(conflicts) > 0 {
		return domain.Campaign{}, shedScheduleConflictError(cmd.StartBusinessDate, conflicts)
	}
	// Partition labels come from the shed_partitions CATALOG, resolved ONCE for the whole campaign
	// (never inside the loop -- that would be an N+1 on the campaign write path). A name alone
	// cannot prove a partition: "Castro 2" and "Mandela 1" are the same shape, and only the
	// catalog knows which is a real partition.
	resolvedPartitions, err := resolvePartitionLabels(ctx, tx, cmd.TenantID, createCampaignLocationIDs(cmd.Sheds))
	if err != nil {
		return domain.Campaign{}, err
	}
	for _, shed := range cmd.Sheds {
		partitionLabel := partitionLabelFor(resolvedPartitions, shed.LocationID, shed.DisplayName)
		var campaignShedID string
		// The bucket's status BEFORE this edit. Read separately because an
		// ON CONFLICT DO UPDATE cannot report the old row in RETURNING, and a CTE is
		// not visible from that RETURNING either. Reviving a canceled bucket carries a
		// second obligation (B09) that must fire ONLY when a revive actually happened.
		var priorStatus string
		// scale-guard:ignore: bounded planner shed list; one single-row unique-key lookup per selected bucket, same grain as the upsert it precedes
		if err := tx.QueryRow(ctx, `
SELECT status FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND location_id=$3::uuid`,
			cmd.TenantID, campaignID, shed.LocationID).Scan(&priorStatus); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domain.Campaign{}, err
		}
		operatorID := weighingShedOperatorID(cmd.OperatorUserID, shed)
		// FREE-FLOW: an edit re-states the SAME bucket membership fact create does --
		// no herd read, no per-goat roster row, no expected count of any kind
		// (expected_animal_count is written 0 = no expectation, same as create).
		err = tx.QueryRow(ctx, // scale-guard:ignore: bounded planner shed list; each selected bucket is upserted once during campaign edit
			`
INSERT INTO weighing_campaign_sheds (campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, operator_user_id, expected_animal_count, park_id, start_business_date, partition_label)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7::uuid, 0,
    -- TASK IDENTITY, re-stated on every edit: an edit that moved the task's park
    -- or weigh date must move its buckets with it, or the duplicate guard would
    -- keep defending the OLD slot and stop defending the new one.
    $8::uuid, $9::date)
ON CONFLICT (tenant_id, campaign_id, location_id)
DO UPDATE SET
  park_id=EXCLUDED.park_id,
  start_business_date=EXCLUDED.start_business_date,
  location_type=EXCLUDED.location_type,
  display_name=EXCLUDED.display_name,
  weighing_category=EXCLUDED.weighing_category,
  operator_user_id=EXCLUDED.operator_user_id,
  expected_animal_count=EXCLUDED.expected_animal_count,
  partition_label=EXCLUDED.partition_label,
  status=CASE WHEN weighing_campaign_sheds.status='canceled' THEN 'pending' ELSE weighing_campaign_sheds.status END,
  updated_at=now()
-- 'canceled' is deliberately NOT in this list: re-selecting a shed the planner had
-- deselected must revive it, which is exactly what the CASE above does. Excluding
-- canceled here made that CASE unreachable, so a re-added shed stayed canceled.
WHERE weighing_campaign_sheds.status NOT IN ('completed','closed')
RETURNING campaign_shed_id::text`, campaignID, cmd.TenantID, shed.LocationID, shed.LocationType, shed.DisplayName, shed.WeighingCategory, operatorID, cmd.ParkID, cmd.StartBusinessDate, nullableString(partitionLabel)).
			Scan(&campaignShedID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx /* scale-guard:ignore: bounded planner shed fallback lookup after idempotent upsert race */, `SELECT campaign_shed_id::text FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND location_id=$3::uuid`, cmd.TenantID, campaignID, shed.LocationID).Scan(&campaignShedID)
		}
		if err != nil {
			return domain.Campaign{}, mapShedUniqueViolation(err, cmd.StartBusinessDate, shed.DisplayName)
		}
		// B09: a bucket that LEAVES a terminal status must take its kernel work item
		// with it, in the same transaction. Re-adding a deselected shed revives the
		// bucket from 'canceled', but the sweeper had already terminalized its
		// weighing_work_items row -- and every open-work read filters on
		// work_state IN ('scheduled','delayed'). Without this the shed reads 'pending'
		// on the task while Calendar, Control Tower and the operator's day-start show
		// no work for it: two surfaces, two different answers for one bucket.
		//
		// Guarded on the PRIOR status so an ordinary edit of a live bucket cannot
		// resurrect a work item that was terminalized for a legitimate reason.
		if priorStatus == "canceled" {
			if _, err := r.ReactivateWorkItemsForBucket(ctx, tx, cmd.TenantID, campaignShedID); err != nil {
				return domain.Campaign{}, err
			}
		}

		// Sync the work item when a bucket's properties change. Cadence passes (day-start, roll-forward, delayed escalation) read operator_user_id
		// from weighing_work_items.operator_user_id. If an edit reassigns the shed to a
		// different operator, the old operator keeps getting nudged while the new one sees
		// nothing. Similarly, park_id and shed_label (display_name) may have changed.
		// Only sync non-terminal items (scheduled/delayed); completed/closed items stay frozen.
		// scale-guard:ignore: bounded by unique campaign_shed_id and filtered on work_state; per-bucket sync during campaign edit
		if _, err := tx.Exec(ctx, `
	UPDATE weighing_work_items
	SET operator_user_id=$3::uuid,
	    park_id=$4::uuid,
	    shed_label=$5,
	    weighing_category=$6,
	    updated_at=now()
	WHERE tenant_id=$1::uuid
	  AND campaign_shed_id=$2::uuid
	  AND work_state IN ('scheduled','delayed')`, cmd.TenantID, campaignShedID, operatorID, cmd.ParkID, shed.DisplayName, shed.WeighingCategory); err != nil {
			return domain.Campaign{}, err
		}
	}

	// A bucket ADDED to an already-published campaign has no work item.
	// createWorkItemsForPublishTx is only called from PublishCampaign, so an edit never
	// backfills work items for new buckets. Since the bucket rows exist and the campaign
	// is already published, the kernel's day-start pass will never surface them.
	// Re-call createWorkItemsForPublishTx for the campaign: the ON CONFLICT ... DO NOTHING
	// makes it idempotent, so existing buckets' work items are untouched and only new
	// buckets get backfilled.
	var campaignStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, cmd.TenantID, campaignID).Scan(&campaignStatus); err != nil {
		return domain.Campaign{}, err
	}
	if campaignStatus == "published" {
		if _, err := r.createWorkItemsForPublishTx(ctx, tx, cmd.TenantID, campaignID); err != nil {
			return domain.Campaign{}, err
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
	// PUBLISH-TIME RE-CHECK. Creation already blocked duplicates, but publishing is
	// when the work becomes an operator's, and a sibling task could have claimed one
	// of these sheds for the same weigh date in between.
	if conflicts, err := r.campaignScheduleConflicts(ctx, tx, tenantID, campaignID); err != nil {
		return domain.Campaign{}, err
	} else if len(conflicts) > 0 {
		weighDate := ""
		if err := tx.QueryRow(ctx, `SELECT start_business_date::text FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, tenantID, campaignID).Scan(&weighDate); err != nil {
			return domain.Campaign{}, err
		}
		return domain.Campaign{}, shedScheduleConflictError(weighDate, conflicts)
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
	// PHASE 2 KERNEL: publishing a campaign materializes its durable work items in
	// THIS transaction. A required recorder, not a side effect: if the work items
	// cannot be written the publish fails and the campaign stays draft, so a
	// published campaign can never exist without the work the kernel sweeps.
	// Idempotent by the unique (tenant_id, campaign_shed_id) bucket index, so
	// republish and an exact replay create no duplicates.
	if _, err := r.createWorkItemsForPublishTx(ctx, tx, tenantID, campaignID); err != nil {
		return domain.Campaign{}, err
	}
	if err := r.recordIdempotency(ctx, tx, tenantID, "weighing.campaign_published", idempotencyKey, fingerprint, "weighing_campaign", campaignID, c); err != nil {
		return domain.Campaign{}, err
	}
	// The published payload carries the per-bucket operator assignment already read
	// above (c.Sheds), so the notification consumer can push DOWNWARD to each
	// assigned operator about ONLY their own buckets without a callback into
	// weighing and without a per-shed query. One bucket has exactly ONE operator.
	publishedBuckets := make([]publishedBucket, 0, len(c.Sheds))
	for _, shed := range c.Sheds {
		publishedBuckets = append(publishedBuckets, publishedBucket{
			CampaignShedID:      shed.CampaignShedID,
			ShedID:              shed.LocationID,
			ShedLabel:           shed.DisplayName,
			OperatorID:          shed.OperatorUserID,
			WeighingCategory:    shed.WeighingCategory,
			ExpectedAnimalCount: shed.ExpectedAnimalCount,
		})
	}
	if err := r.enqueue(ctx, tx, tenantID, "weighing.campaign_published", campaignID, idempotencyKey, fingerprint, weighingCampaignPublishedPayload{
		TenantID:          tenantID,
		CampaignID:        campaignID,
		ParkID:            c.ParkID,
		PublishedBy:       actorID,
		PeriodStartDate:   c.PeriodStartDate,
		PeriodEndDate:     c.PeriodEndDate,
		StartBusinessDate: c.StartBusinessDate,
		Buckets:           publishedBuckets,
	}); err != nil {
		return domain.Campaign{}, err
	}
	return c, tx.Commit(ctx)
}

func (r *Repository) ListCampaigns(ctx context.Context, tenantID, parkID string, cursor string, limit int) (domain.CampaignPage, error) {
	return r.listCampaigns(ctx, tenantID, "", parkID, "", cursor, limit, nil)
}

func (r *Repository) ListCampaignsForOperator(ctx context.Context, tenantID, operatorUserID, parkID string, cursor string, limit int) (domain.CampaignPage, error) {
	return r.listCampaigns(ctx, tenantID, operatorUserID, parkID, "", cursor, limit, nil)
}

// CampaignByID resolves ONE task by id, through the SAME query the list uses rather than a
// second hand-written campaign read. That is the point of routing it here: the list query
// already carries the operator predicate, the park-grant defence-in-depth inside it, the park
// name join and the bucket/progress hydration, and a parallel single-row copy would have to
// re-derive all four and would drift from them on the next change.
// The caller's authority travels INSIDE that same query as `access`. It used to be checked in
// the app layer by a preceding CampaignParkID call, which read park_id in one statement and
// returned the row in another: a task that moved park between the two was authorized as its old
// park and returned as its new one. There is no re-check after the read here, because a re-check
// would be a THIRD read of the same mutable column and would inherit the same gap -- the row that
// is returned has to be the row that was authorized, and one query is the only way to say that.
func (r *Repository) CampaignByID(ctx context.Context, tenantID, campaignID string, access ports.CampaignAccess) (domain.Campaign, error) {
	page, err := r.listCampaigns(ctx, tenantID, "", "", campaignID, "", 1, &access)
	if err != nil {
		return domain.Campaign{}, err
	}
	if len(page.Items) == 0 {
		return domain.Campaign{}, ports.ErrNotFound
	}
	return page.Items[0], nil
}

// campaignID is an EXACT-id filter, not a search: when it is set the read answers one task and
// the whole-filter tab counts are skipped, because a single task has no tabs to number.
//
// access is non-nil ONLY on the single-task read. It replaces the AND-shaped operator predicate
// with an OR over the caller's alternative authorities (park set, tenant-wide, own assignment),
// which is what lets authorization happen in the same statement as retrieval instead of in a
// preceding one. The list paths pass nil and are untouched: their authority is already applied
// as a park filter by the app layer before they get here.
func (r *Repository) listCampaigns(ctx context.Context, tenantID, operatorUserID, parkID, campaignID string, cursor string, limit int, access *ports.CampaignAccess) (domain.CampaignPage, error) {
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
	operatorFilter := strings.TrimSpace(operatorUserID)
	parkFilter := strings.TrimSpace(parkID)
	campaignFilter := strings.TrimSpace(campaignID)
	// The access arms are bound as scalars even when unused, so the list paths and the
	// single-task path run the SAME statement text and cannot drift apart. accessApplies=false
	// short-circuits the whole clause to true for the list paths.
	accessApplies := access != nil
	accessUnrestricted := accessApplies && access.Unrestricted
	// An EMPTY (never nil) array: `= ANY('{}')` is FALSE, whereas `= ANY(NULL)` is NULL, and a
	// NULL arm inside this OR would make an otherwise-admitted row evaluate to NULL and vanish.
	accessParkIDs := []string{}
	accessAssignee := ""
	if accessApplies {
		accessParkIDs = append(accessParkIDs, access.AuthorizedParkIDs...)
		accessAssignee = strings.TrimSpace(access.AssigneeUserID)
	}
	// projection-review: membership=weighing_campaigns rows for one tenant, with per-campaign bucket detail and progress rollups attached afterwards by hydrateCampaigns keyed on campaign_id; group_key=(tenant_id,campaign_id) with keyset order key (period_start_date,created_at,campaign_id); join_cardinality=locations park is at most one row per campaign (locations PK location_id, matched on tenant_id+location_id) and weighing_campaign_sheds is 1:N but only reached through an EXISTS semijoin so it cannot multiply campaign rows; pagination=keyset on (period_start_date,created_at,campaign_id) DESC with tenant and operator predicates inside WHERE so filtering happens before LIMIT, and LIMIT $5 is limit+1 purely for cursor lookahead; scope=tenant_id=$1 always, plus the operator predicate $6 restricting the tenant to campaigns holding a non-canceled weighing_campaign_sheds bucket assigned to that operator, plus the optional exact-id predicate $8 which serves the single-task read (CampaignByID) off the primary key without changing the grain -- one campaign_id can match at most one row, so it narrows and never multiplies -- plus the single-task access predicate ($9 applies, $10 tenant-wide, $11 authorized park array, $12 assignee), which is a disjunction of row-local tests (park_id = ANY(array), plus an EXISTS semijoin on weighing_campaign_sheds that contributes no extra rows) evaluated against the SAME row snapshot the query returns, so authorization and retrieval share one statement and one park_id value.
	//
	// Grain proof (a) producer unique columns vs consumer match/group columns:
	//   producer weighing_campaigns   unique: (campaign_id) PK, tenant-scoped (tenant_id, campaign_id)
	//   consumer list row             match:  (weighing_campaigns.tenant_id, weighing_campaigns.campaign_id)
	//   producer weighing_campaign_sheds unique: (campaign_shed_id) PK; tenant/campaign-scoped (tenant_id, campaign_id, campaign_shed_id)
	//   consumer hydrateCampaigns     match:  (tenant_id, campaign_id, campaign_shed_id), grouped in Go by campaign_id
	//   producer locations            unique: (location_id) PK
	//   consumer park label           match:  (park.tenant_id, park.location_id) = (weighing_campaigns.tenant_id, weighing_campaigns.park_id)
	//
	// Grain proof (b) row multiplicity of every joined side:
	//   locations park                 0..1 rows per campaign (LEFT JOIN on the locations primary key; COALESCE covers the 0 case).
	//                                  It is the only table joined into the row path, and it is exactly why every column here is
	//                                  schema-qualified: locations also owns status/name/tenant_id/created_at/updated_at, so the
	//                                  previously bare status/tenant_id/period_start_date/campaign_id references were ambiguous
	//                                  the moment this join was added (Postgres 42702). Qualification is the fix, not a rename.
	//   weighing_campaign_sheds scope  1..N rows per campaign, consumed ONLY inside EXISTS (semijoin) => contributes 0 extra rows.
	//   weighing_expected_animals      1..N rows per campaign, never joined here; pre-aggregated in hydrateCampaigns with
	//                                  GROUP BY campaign_id before it reaches a campaign row.
	//   weighing_shed_observations     1..N rows per campaign, never joined here; pre-aggregated in hydrateCampaigns with
	//                                  count(DISTINCT campaign_shed_id) GROUP BY campaign_id.
	//
	// Grain proof (c) ratio key sets (Progress remaining/completed vs expected):
	//   numerator   (completed animals, completed per-shed scopes, wrong-shed, missing) ranges over
	//               (tenant_id, campaign_id, campaign_shed_id IN the operator-visible bucket set)
	//   denominator (individual expected count, per-scope expected count) ranges over
	//               (tenant_id, campaign_id, campaign_shed_id IN the operator-visible bucket set)
	//   Identical key sets on both sides: hydrateCampaigns applies the SAME operator bucket predicate to the
	//   rollups that it applies to the bucket rows, so an operator's remaining count can never be computed from
	//   a sibling operator's completions against their own smaller expected set.
	rows, err := r.pool.Query(ctx, `
SELECT weighing_campaigns.campaign_id::text, weighing_campaigns.tenant_id::text, weighing_campaigns.park_id::text, COALESCE(park.name, '') AS park_name,
  weighing_campaigns.period_start_date::text, weighing_campaigns.period_end_date::text,
  weighing_campaigns.start_business_date::text, weighing_campaigns.status, weighing_campaigns.planned_cap_per_day,
  weighing_campaigns.operator_user_id::text, weighing_campaigns.created_by::text,
  weighing_campaigns.created_at, weighing_campaigns.updated_at, weighing_campaigns.row_version,
  COALESCE(weighing_campaigns.close_reason, ''),
  COALESCE(weighing_campaigns.closure_kind, '')
FROM weighing_campaigns
LEFT JOIN locations park
       ON park.tenant_id=weighing_campaigns.tenant_id
      AND park.location_id=weighing_campaigns.park_id
WHERE weighing_campaigns.tenant_id=$1::uuid
  AND (
    $6::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM weighing_campaign_sheds scope
      WHERE scope.tenant_id=weighing_campaigns.tenant_id
        AND scope.campaign_id=weighing_campaigns.campaign_id
        AND scope.operator_user_id=$6::uuid
        AND scope.status <> 'canceled'
        -- Defence in depth: a park-scoped operator never SEES another park's work, even if a row
        -- somehow carries a cross-park assignment. The write refuses it and a trigger refuses it,
        -- and this read refuses to surface it.
        AND EXISTS (
          SELECT 1 FROM user_scope_grants g
          WHERE g.tenant_id=weighing_campaigns.tenant_id
            AND g.user_id=$6::uuid
            AND g.status='active'
            AND (g.scope_type='tenant' OR (g.scope_type='park' AND g.scope_id=weighing_campaigns.park_id))
        )
    )
  )
  AND ($7::uuid IS NULL OR weighing_campaigns.park_id=$7::uuid)
  AND ($8::uuid IS NULL OR weighing_campaigns.campaign_id=$8::uuid)
  -- Single-task authorization, evaluated against THIS row's park_id in the same statement that
  -- returns it. Splitting it into "read the park, authorize it, then read the row" is what let a
  -- task that changed park between the two statements be authorized as one park and served as
  -- another. The arms are alternatives on purpose: an actor assigned a bucket here is authorized
  -- by the assignment even in a park they do not monitor.
  AND (
    NOT $9::boolean
    OR $10::boolean
    OR weighing_campaigns.park_id = ANY($11::uuid[])
    OR (
      $12::uuid IS NOT NULL
      AND EXISTS (
        SELECT 1
        FROM weighing_campaign_sheds assigned
        WHERE assigned.tenant_id=weighing_campaigns.tenant_id
          AND assigned.campaign_id=weighing_campaigns.campaign_id
          AND assigned.operator_user_id=$12::uuid
          AND assigned.status <> 'canceled'
          AND EXISTS (
            SELECT 1 FROM user_scope_grants g
            WHERE g.tenant_id=weighing_campaigns.tenant_id
              AND g.user_id=$12::uuid
              AND g.status='active'
              AND (g.scope_type='tenant' OR (g.scope_type='park' AND g.scope_id=weighing_campaigns.park_id))
          )
      )
    )
  )
  AND (
    $2::date IS NULL
    OR (weighing_campaigns.period_start_date, weighing_campaigns.created_at, weighing_campaigns.campaign_id) < ($2::date, $3::timestamptz, $4::uuid)
  )
ORDER BY weighing_campaigns.period_start_date DESC, weighing_campaigns.created_at DESC, weighing_campaigns.campaign_id DESC
LIMIT $5`, tenantID, nullableString(cur.PeriodStartDate), nullableTime(cur.CreatedAt), nullableString(cur.CampaignID), limit+1, nullableString(operatorFilter), nullableString(parkFilter), nullableString(campaignFilter),
		accessApplies, accessUnrestricted, accessParkIDs, nullableString(accessAssignee))
	if err != nil {
		return domain.CampaignPage{}, err
	}
	defer rows.Close()
	out := make([]domain.Campaign, 0, limit)
	ids := make([]string, 0, limit+1)
	for rows.Next() {
		var c domain.Campaign
		if err := rows.Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.ParkName, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion, &c.CloseReason, &c.ClosureKind); err != nil {
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
	hydrationOperator := operatorFilter
	if access != nil && len(out) == 1 && !access.AdmitsPark(out[0].ParkID) {
		// Admitted by the ASSIGNMENT arm rather than by park authority, so the caller sees only
		// their own bucket and only their own progress -- the same narrowing the assignee's task
		// list applies. The park it is decided against is the one this very query returned, so
		// this cannot be a second, disagreeing read of park_id.
		hydrationOperator = strings.TrimSpace(access.AssigneeUserID)
	}
	if err := r.hydrateCampaigns(ctx, tenantID, ids, out, hydrationOperator); err != nil {
		return domain.CampaignPage{}, err
	}
	if campaignFilter != "" {
		// One named task carries no Active/Completed tabs, so the whole-filter count query is
		// pure cost on this path.
		return domain.CampaignPage{Items: out, NextCursor: nextCursor}, nil
	}
	counts, err := r.campaignCounts(ctx, tenantID, operatorFilter, parkID)
	if err != nil {
		return domain.CampaignPage{}, err
	}
	// Operator-grain roll-up for the oversight surface. Whole-filter and park-aware,
	// so the phone never has to group a keyset page and call the result a person's total.
	summaries, err := r.operatorSummaries(ctx, tenantID, operatorFilter, parkFilter)
	if err != nil {
		return domain.CampaignPage{}, err
	}
	return domain.CampaignPage{Items: out, NextCursor: nextCursor, Counts: counts, OperatorSummaries: summaries}, nil
}

// campaignCounts is the WHOLE-FILTER task tally behind the Active / Completed tabs.
//
// GRAIN: one weighing_campaigns row = one task = one park on one weigh date. The
// count is therefore a plain count of campaign rows, never a count of buckets,
// animals, or observations, and never derived from the rows of a page.
//
// It is a SEPARATE, count-only query rather than a window function bolted onto the
// list query on purpose: a count computed inside the paged query would have to drop
// the LIMIT to be whole-filter, turning a bounded keyset page into a full scan of
// the tenant's campaigns on every page request.
//
// Scope: tenant, plus the SAME operator predicate the row query uses (so an
// operator's tabs count only their own tasks). It is deliberately NOT narrowed by
// park_id — the park chip filters rows, and the tab numbers must stay still while
// the user flicks between parks.
//
// Grain proof:
//
//	producer weighing_campaigns unique: (campaign_id) PK, tenant-scoped (tenant_id, campaign_id)
//	consumer                    group:  none — one scalar pair over the scope
//	weighing_campaign_sheds is 1..N per campaign and is read ONLY inside an EXISTS
//	semijoin, so it contributes zero extra rows and cannot inflate either count.
//
// Buckets are disjoint and exhaustive over the live statuses: completed/closed on one
// side, draft/published/in_progress/delayed on the other. 'canceled' is retracted work
// and belongs to neither tab, so it is counted in neither.
func (r *Repository) campaignCounts(ctx context.Context, tenantID, operatorUserID, parkID string) (domain.CampaignCounts, error) {
	var counts domain.CampaignCounts
	err := r.pool.QueryRow(ctx, `
SELECT
  count(*) FILTER (WHERE wc.status IN ('draft','published','in_progress','delayed'))::int AS active,
  count(*) FILTER (WHERE wc.status IN ('completed','closed'))::int AS completed
FROM weighing_campaigns wc
WHERE wc.tenant_id=$1::uuid
  AND (
    $2::uuid IS NULL
    OR EXISTS (
      SELECT 1
      FROM weighing_campaign_sheds scope
      WHERE scope.tenant_id=wc.tenant_id
        AND scope.campaign_id=wc.campaign_id
        AND scope.operator_user_id=$2::uuid
        AND scope.status <> 'canceled'
    )
  )
  AND ($3::uuid IS NULL OR wc.park_id = $3::uuid)`,
		tenantID, nullableString(strings.TrimSpace(operatorUserID)), nullableString(strings.TrimSpace(parkID))).
		Scan(&counts.Active, &counts.Completed)
	if err != nil {
		return domain.CampaignCounts{}, err
	}
	return counts, nil
}

// PlannerCatalog returns the PARK-GRAIN planner vocabulary for ONE weigh date: EVERY active park
// the planner may use, each with its identity, a park-grain kid count, a park-grain shed COUNT,
// and the park's existing task on that date, plus the operator picker.
//
// It deliberately returns NO shed rows. Parks and sheds are two different grains, and they used to
// share ONE flattened keyset page: because a real park holds 76+ sheds and the page was ~20 rows,
// page one was entirely one park and the wizard's park step offered a single park. The park list is
// a handful of rows and is served whole (capped by domain.MaxPlannerParks); the sheds of the CHOSEN
// park are paged by PlannerParkBuckets.
func (r *Repository) PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string) (domain.PlannerCatalog, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	// projection-review: membership=active parks of one tenant, decorated with (a) a park-grain count of that park's active sheds and (b) the park's existing task on the requested date; group_key=park location_id; join_cardinality=the existing side is pre-aggregated with DISTINCT ON (park_id) to EXACTLY ONE row per park before the join, so it cannot multiply park rows; pagination=NONE by design, parks are few and the picker must offer all of them, the read is bounded by a hard LIMIT instead; scope=tenant_id
	//
	// Grain proof (shed_count):
	//   producer locations (shed rows)  unique per shed: location_id (PK)
	//   consumer park row               match: shed.parent_location_id = park.location_id
	//   The count is a CORRELATED SCALAR AGGREGATE over that park's own children, evaluated per
	//   park row. It is therefore the park's TOTAL active shed count -- NOT a sum of shed rows
	//   that happened to land on some page, which is the grain error this split removes. No shed
	//   row reaches the result set at all.
	//
	// Grain proof (existing):
	//   producer weighing_campaigns     0..N rows per (tenant_id, park_id, period_start_date) --
	//     nothing forbids two non-canceled tasks for one park on one date, so the many side is
	//     pre-aggregated by DISTINCT ON rather than joined raw. Since migration 000081 dropped
	//     weighing_campaigns_one_active_week_per_park_idx this is the NORMAL case, not a
	//     theoretical one: leadership plans a park's leftover sheds as a second task.
	//   consumer park row               match: existing.park_id = park.location_id
	//   => 0..1 existing rows per park row. LEFT JOIN, never a fan-out.
	//   Its shed_count is a scalar aggregate over that campaign's OWN buckets, at campaign grain --
	//   NEVER the park-week total across tasks, which would be a merge across two grains.
	//   task_count is a WINDOW count over the same pre-DISTINCT partition, so it reports how many
	//   tasks the park really holds that week while the summary columns still describe exactly one
	//   of them. Without it a caller cannot tell "one task" from "the newest of three".
	//
	// Served by locations_tenant_type_status_order_idx
	// (tenant_id, location_type, status, display_order, name, location_id): the three equality
	// columns first, then exactly this ORDER BY, so the LIMIT stops the index walk.
	rows, err := r.pool.Query(ctx, `
WITH existing AS (
  SELECT DISTINCT ON (wc.park_id)
    wc.park_id,
    wc.campaign_id::text AS campaign_id,
    wc.status,
    wc.period_start_date::text AS period_start_date,
    wc.period_end_date::text AS period_end_date,
    wc.start_business_date::text AS start_business_date,
    wc.operator_user_id::text AS operator_user_id,
    (
      SELECT count(*)::int
      FROM weighing_campaign_sheds wcs
      WHERE wcs.tenant_id=wc.tenant_id AND wcs.campaign_id=wc.campaign_id
    ) AS shed_count,
    -- Evaluated BEFORE DISTINCT ON collapses the partition, so it counts every
    -- task the park holds that week, not the one row that survives.
    count(*) OVER (PARTITION BY wc.park_id)::int AS task_count
  FROM weighing_campaigns wc
  WHERE wc.tenant_id=$1::uuid
    AND wc.period_start_date=$2::date
    AND wc.status <> 'canceled'
  ORDER BY wc.park_id, wc.created_at DESC, wc.campaign_id DESC
)
SELECT
  park.location_id::text,
  park.name,
  (
    SELECT count(*)::int
    FROM locations shed
    WHERE shed.tenant_id=park.tenant_id
      AND shed.parent_location_id=park.location_id
      AND shed.location_type='shed'
      AND shed.status='active'
      AND shed.retired_at IS NULL
  ) AS shed_count,
  existing.campaign_id,
  existing.status,
  existing.period_start_date,
  existing.period_end_date,
  existing.start_business_date,
  existing.operator_user_id,
  COALESCE(existing.shed_count, 0)::int,
  COALESCE(existing.task_count, 0)::int
FROM locations park
LEFT JOIN existing ON existing.park_id=park.location_id
WHERE park.tenant_id=$1::uuid
  AND park.location_type='park'
  AND park.status='active'
  AND park.retired_at IS NULL
ORDER BY park.display_order, park.name, park.location_id
LIMIT $3`, tenantID, periodStartDate, domain.MaxPlannerParks)
	if err != nil {
		return domain.PlannerCatalog{}, err
	}
	defer rows.Close()
	out := domain.PlannerCatalog{Parks: []domain.PlannerPark{}, Operators: []domain.PlannerOperator{}}
	parkIndex := map[string]int{}
	for rows.Next() {
		var park domain.PlannerPark
		var existingID, existingStatus, existingStart, existingEnd, existingBusinessDate, existingOperator *string
		var existingShedCount, existingTaskCount int
		if err := rows.Scan(
			&park.ParkID, &park.Name, &park.ShedCount,
			&existingID, &existingStatus, &existingStart, &existingEnd,
			&existingBusinessDate, &existingOperator, &existingShedCount, &existingTaskCount,
		); err != nil {
			return domain.PlannerCatalog{}, err
		}
		park.ExistingCampaignCount = existingTaskCount
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
		parkIndex[park.ParkID] = len(out.Parks) - 1
	}
	if err := rows.Err(); err != nil {
		return domain.PlannerCatalog{}, err
	}
	// FREE-FLOW: no herd-derived park kid count. The park picker carries identity and
	// shed/task membership only; parkIndex is retained for clarity of the remaining
	// park-row lookups below.
	_ = parkIndex
	// The operator picker rides THIS read only. The wizard holds it while it pages the chosen
	// park's buckets, so it is never re-sent per shed page.
	//
	// WHO belongs here is "may be assigned weighing work", NOT "carries the operator role hint".
	// The growth director weighs his own sheds -- in the standing fixture he holds two per park --
	// and filtering on primary_role_hint='operator' left him out of the picker entirely, so a
	// director bucket could not be assigned through the wizard at all and his name could not be
	// resolved for a bucket he already owned (the chip fell back to the word "Operator").
	// Membership follows the weighing.execute grant, so a new role that can weigh appears here
	// without another edit.
	operatorRows, err := r.pool.Query(ctx, `
SELECT m.user_id::text, m.display_name, m.display_code,
       COALESCE(
         (SELECT array_agg(DISTINCT g2.scope_id::text)
          FROM user_scope_grants g2
          WHERE g2.tenant_id = m.tenant_id
            AND g2.user_id = m.user_id
            AND g2.status = 'active'
            AND g2.scope_type = 'park'
            AND NOT EXISTS (
              SELECT 1 FROM user_scope_grants g3
              WHERE g3.tenant_id = m.tenant_id AND g3.user_id = m.user_id
                AND g3.status = 'active' AND g3.scope_type = 'tenant'
            )),
         ARRAY[]::text[]
       ) AS park_ids
FROM workforce_members m
WHERE m.tenant_id=$1::uuid
  AND m.status='active'
  AND m.user_id IS NOT NULL
  AND (
    m.primary_role_hint = ANY($3::text[])
    OR EXISTS (
      SELECT 1 FROM user_scope_grants g
      WHERE g.tenant_id = m.tenant_id
        AND g.user_id = m.user_id
        AND g.status = 'active'
        AND g.role = ANY($3::text[])
    )
  )
ORDER BY m.display_name, m.display_code, m.user_id
-- Hard cap. The field-operator roster is small (single digits in the real data),
-- but "small today" is not a bound.
LIMIT $2`, tenantID, domain.PlannerOperatorLimit, domain.WeighingAssignableRoles)
	if err != nil {
		return domain.PlannerCatalog{}, err
	}
	defer operatorRows.Close()
	for operatorRows.Next() {
		var operator domain.PlannerOperator
		if err := operatorRows.Scan(&operator.UserID, &operator.DisplayName, &operator.DisplayCode, &operator.ParkIDs); err != nil {
			return domain.PlannerCatalog{}, err
		}
		out.Operators = append(out.Operators, operator)
	}
	if err := operatorRows.Err(); err != nil {
		return domain.PlannerCatalog{}, err
	}
	return out, nil
}

// PlannerParkBuckets returns ONE keyset page of the active sheds of ONE park, each with its current
// kid count and whether it is ALREADY CLAIMED by an open weighing task on the requested weigh date
// and by whom -- the availability behind "show available / show all buckets" and behind the disabled
// reason on a taken bucket.
//
// excludeCampaignID is the task being EDITED: its own buckets must not read back as taken, or an
// edit could never re-save the sheds it already owns.
func (r *Repository) PlannerParkBuckets(ctx context.Context, tenantID, parkID, periodStartDate, excludeCampaignID, cursor string, limit int) (domain.PlannerParkBuckets, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = domain.PlannerBucketPageSize
	}
	if limit > domain.MaxPlannerBucketPageSize {
		limit = domain.MaxPlannerBucketPageSize
	}
	cur, err := decodePlannerBucketCursor(cursor)
	if err != nil {
		return domain.PlannerParkBuckets{}, ports.ErrInvalidArgument
	}
	// projection-review: membership=the active sheds of ONE park, decorated with (a) that shed's
	// alive-kid count and (b) the open weighing claim on ONE weigh date; group_key=shed
	// location_id; join_cardinality=taken is pre-aggregated to EXACTLY ONE row per (tenant_id,
	// location_id) before the join and the kid count is a per-row LATERAL aggregate, so neither
	// can multiply shed rows; pagination=KEYSET on (display_order, name, location_id) within the
	// park with a ~20 default page; scope=tenant_id, one park_id, and the single requested
	// business date.
	//
	// Grain proof (taken):
	//   producer weighing_campaign_sheds unique per open claim: guaranteed 1 row per
	//     (tenant_id, park_id, start_business_date, location_id) by uq_weighing_open_shed_per_park_date
	//     (migration 000062). The DISTINCT ON is belt-and-braces for the excluded-campaign case and
	//     for the window before that index exists in an old database.
	//   consumer shed row              match: (taken.tenant_id, taken.location_id) = (shed.tenant_id, shed.location_id)
	//   => 0..1 taken rows per shed row. LEFT JOIN, never a fan-out.
	//   The taken set is narrowed to THIS park, so it is an index scan over just this park's open
	//   buckets for that day, evaluated ONCE for the page.
	//
	// Grain proof (kid_count):
	//   SHED-grain count of alive kid goats currently standing in that shed. One correlated
	//   aggregate per returned shed row (at most `limit`+1 of them), served by
	//   goats_current_location_lifecycle_idx (current_location_id, lifecycle_status). It replaces
	//   a GROUP BY over every goat of the tenant, which was computed in full to decorate ~20 rows.
	//
	// Ordering is served by locations_tenant_parent_status_idx
	// (tenant_id, parent_location_id, status, display_order, name, location_id): the three
	// equality columns first, then exactly this keyset tuple in this order, so the LIMIT stops the
	// index walk and no Sort node is needed.
	rows, err := r.pool.Query(ctx, `
WITH taken AS (
  SELECT DISTINCT ON (cs.tenant_id, cs.location_id)
    cs.tenant_id,
    cs.location_id,
    cs.campaign_id::text AS campaign_id,
    cs.status,
    cs.operator_user_id::text AS operator_user_id,
    cs.weighing_category,
    COALESCE(op.display_name, '') AS operator_display_name
  FROM weighing_campaign_sheds cs
  -- op.status='active' is REQUIRED, not decoration: the only (tenant_id, user_id)
  -- uniqueness on workforce_members is workforce_members_active_user_unique_idx,
  -- which is PARTIAL on status='active'. Without the predicate a user who has a
  -- prior non-active member row matches twice, the DISTINCT ON tiebreak says
  -- nothing about op, and the "already scheduled by <name>" reason renders a
  -- stale name that can flip between refreshes.
  LEFT JOIN workforce_members op
    ON op.tenant_id=cs.tenant_id
   AND op.user_id=cs.operator_user_id
   AND op.status='active'
  WHERE cs.tenant_id=$1::uuid
    AND cs.park_id=$2::uuid
    AND cs.start_business_date=$3::date
    AND cs.status NOT IN ('canceled', 'closed', 'completed')
    AND ($4::uuid IS NULL OR cs.campaign_id <> $4::uuid)
  ORDER BY cs.tenant_id, cs.location_id, cs.created_at, cs.campaign_shed_id
)
SELECT
  shed.display_order,
  shed.name,
  shed.location_id::text,
  taken.campaign_id,
  taken.status,
  taken.operator_user_id,
  taken.operator_display_name,
  taken.weighing_category
FROM locations shed
LEFT JOIN taken ON taken.tenant_id=shed.tenant_id AND taken.location_id=shed.location_id
WHERE shed.tenant_id=$1::uuid
  AND shed.parent_location_id=$2::uuid
  AND shed.location_type='shed'
  AND shed.status='active'
  AND shed.retired_at IS NULL
  AND (
    $5::int IS NULL
    OR (shed.display_order, shed.name, shed.location_id) > ($5::int, $6::text, $7::uuid)
  )
ORDER BY shed.display_order, shed.name, shed.location_id
LIMIT $8`,
		tenantID, parkID, periodStartDate, nullableString(strings.TrimSpace(excludeCampaignID)),
		cur.orderArg(), cur.nameArg(), cur.idArg(),
		limit+1)
	if err != nil {
		return domain.PlannerParkBuckets{}, err
	}
	defer rows.Close()
	out := domain.PlannerParkBuckets{ParkID: parkID, Sheds: []domain.PlannerShed{}}
	sorts := make([]plannerBucketCursor, 0, limit+1)
	for rows.Next() {
		var shed domain.PlannerShed
		var sort plannerBucketCursor
		var takenCampaignID, takenStatus, takenOperatorID, takenOperatorName, takenCategory *string
		if err := rows.Scan(
			&sort.ShedOrder, &sort.ShedName, &shed.LocationID,
			&takenCampaignID, &takenStatus, &takenOperatorID, &takenOperatorName, &takenCategory,
		); err != nil {
			return domain.PlannerParkBuckets{}, err
		}
		sort.ShedID = shed.LocationID
		sort.Set = true
		shed.Name = sort.ShedName
		shed.Scheduled = takenCampaignID != nil
		shed.ScheduledCampaignID = deref(takenCampaignID)
		shed.ScheduledStatus = deref(takenStatus)
		shed.ScheduledOperatorUserID = deref(takenOperatorID)
		shed.ScheduledOperatorDisplayName = deref(takenOperatorName)
		shed.ScheduledWeighingCategory = deref(takenCategory)
		applyPlannerShedPartitionDisplay(&shed)
		out.Sheds = append(out.Sheds, shed)
		sorts = append(sorts, sort)
	}
	if err := rows.Err(); err != nil {
		return domain.PlannerParkBuckets{}, err
	}
	if len(out.Sheds) > limit {
		out.NextCursor = encodePlannerBucketCursor(sorts[limit-1])
		out.Sheds = out.Sheds[:limit]
	}
	return out, nil
}

func (r *Repository) ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, observationsCursor string, limit int) (domain.RosterPage, error) {
	return r.listScopeRoster(ctx, tenantID, campaignID, campaignShedID, "", observationsCursor, limit)
}

func (r *Repository) ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, observationsCursor string, limit int) (domain.RosterPage, error) {
	return r.listScopeRoster(ctx, tenantID, campaignID, campaignShedID, operatorUserID, observationsCursor, limit)
}

// listScopeRoster is the free-flow scan/observation history read for one bucket.
// There is no expected roster to read here -- identity is scanned_identifier
// only. Items/NextCursor on the returned domain.RosterPage are always empty:
// they are kept ONLY for wire compatibility with clients still reading those
// keys, and nothing populates them.
func (r *Repository) listScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, observationsCursorValue string, limit int) (domain.RosterPage, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	operatorFilter := strings.TrimSpace(operatorUserID)
	out := make([]domain.ExpectedAnimal, 0)
	nextCursor := ""
	obsCur, err := decodeObservationsCursor(observationsCursorValue)
	if err != nil {
		return domain.RosterPage{}, ports.ErrInvalidArgument
	}
	observations := make([]domain.Observation, 0, limit)
	observationAcceptedAt := make([]time.Time, 0, limit+1)
	observationRows, err := r.pool.Query(ctx, `
-- Every selected column is qualified: weighing_campaign_sheds carries campaign_id and
-- campaign_shed_id too, so an unqualified reference here is ambiguous (SQLSTATE 42702) and
-- 500s the whole scan roster.
--
-- Free-flow: this reads scanned_identifier directly (weighing_observations has
-- no animal_id column -- dropped by
-- 000078_weighing_observations_drop_animal_id.sql). The old query filtered on
-- animal_id IS NULL OR animal_id = ANY(roster-animal-ids), which was already
-- vacuously true for every row (animal_id has been NULL on every capture since
-- free-flow shipped), so removing the dead predicate changes no behaviour.
SELECT weighing_observations.observation_id::text,
       weighing_observations.campaign_id::text,
       weighing_observations.campaign_shed_id::text,
       weighing_observations.scanned_identifier,
       weighing_observations.weight_kg::float8,
       weighing_observations.proof_artifact_id::text,
       COALESCE(weighing_observations.expected_location_id::text, ''),
       weighing_observations.accepted_at,
       -- WHICH tag came back. Both columns already exist on domain.Observation and were
       -- documented as the operator's only way to tell a sent-back animal from an accepted
       -- one, but this roster never selected them: every card rendered identically, so the
       -- operator saw a green tick on the animal a verifier had rejected.
       COALESCE(weighing_observations.verification_status, ''),
       COALESCE(weighing_observations.rework_reason, '')
FROM weighing_observations
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=weighing_observations.tenant_id
 AND cs.campaign_id=weighing_observations.campaign_id
 AND cs.campaign_shed_id=weighing_observations.campaign_shed_id
 AND ($4::uuid IS NULL OR cs.operator_user_id=$4::uuid)
 AND cs.status <> 'canceled'
WHERE weighing_observations.tenant_id=$1::uuid
  AND weighing_observations.campaign_id=$2::uuid
  AND weighing_observations.campaign_shed_id=$3::uuid
  AND (
    $5::timestamptz IS NULL
    OR (weighing_observations.accepted_at, weighing_observations.observation_id) > ($5::timestamptz, $6::uuid)
  )
ORDER BY weighing_observations.accepted_at, weighing_observations.observation_id
LIMIT $7`, tenantID, campaignID, campaignShedID, nullableString(operatorFilter),
		nullableTime(obsCur.AcceptedAt), nullableString(obsCur.ObservationID), limit+1)
	if err != nil {
		return domain.RosterPage{}, err
	}
	defer observationRows.Close()
	for observationRows.Next() {
		var observation domain.Observation
		var acceptedAt time.Time
		if err := observationRows.Scan(
			&observation.ObservationID,
			&observation.CampaignID,
			&observation.CampaignShedID,
			&observation.ScannedIdentifier,
			&observation.WeightKg,
			&observation.ProofArtifactID,
			&observation.ExpectedLocationID,
			&observation.AcceptedAt,
			&observation.VerificationStatus,
			&observation.ReworkReason,
		); err != nil {
			return domain.RosterPage{}, err
		}
		acceptedAt = observation.AcceptedAt
		observations = append(observations, observation)
		observationAcceptedAt = append(observationAcceptedAt, acceptedAt)
	}
	if err := observationRows.Err(); err != nil {
		return domain.RosterPage{}, err
	}
	nextObservationsCursor := ""
	if len(observations) > limit {
		last := observations[limit-1]
		nextObservationsCursor = encodeObservationsCursor(observationsCursor{AcceptedAt: observationAcceptedAt[limit-1], ObservationID: last.ObservationID})
		observations = observations[:limit]
	}
	return domain.RosterPage{Items: out, Observations: observations, NextCursor: nextCursor, NextObservationsCursor: nextObservationsCursor}, nil
}

// GetLeadershipShedVideos is the shed-grain leadership evidence read.
//
// GRAIN: one weighing_observations row = one captured animal weight in this
// bucket. Individual is a KEYSET PAGE of that grain — it is not a rollup and
// carries no denominator. LumpSum is the bucket's ONE accepted
// weighing_shed_observations row (0..1 OPEN, unique on (tenant_id, campaign_shed_id)
// WHERE withdrawn_at IS NULL; ReopenScope stamps withdrawn_at and this read filters
// on it, so a reopened-but-not-yet-resubmitted bucket has none)
// and is not paged — there is no "latest of several" to tie-break.
//
// The head row carries the bucket's own context (park name, weigh business date,
// assignee display name) so a client deep-linking straight to this surface does
// not have to be handed them as route args by a screen it never passed through.
// access is the caller's PARK authority and is evaluated INSIDE the head query, against the
// weighing_campaigns row that query already joins for the park name. It used to be checked in
// the app layer against a park fetched by a separate CampaignParkID statement; park_id is
// mutable (UpdateCampaign moves a task between parks), so a bucket whose task moved between the
// two reads was authorized as its old park and had its evidence video served from its new one.
// Re-checking after the read would be a third read of the same moving value -- the row that is
// returned has to be the row the predicate admitted, which is one statement or nothing.
//
// The evidence reads that follow are keyed on (campaign_id, campaign_shed_id) -- the identity
// the head row already admitted -- and never re-resolve the park.
func (r *Repository) GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID, cursor string, limit int, access ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = domain.LeadershipShedVideosPageSize
	}
	if limit > domain.MaxLeadershipShedVideosPageSize {
		limit = domain.MaxLeadershipShedVideosPageSize
	}
	cur, err := decodeObservationsCursor(cursor)
	if err != nil {
		return domain.LeadershipShedVideos{}, ports.ErrInvalidArgument
	}
	// EMPTY, never nil: `= ANY('{}')` is FALSE while `= ANY(NULL)` is NULL, and a NULL arm inside
	// the OR would make an otherwise-admitted row evaluate to NULL and vanish. A zero
	// CampaignAccess therefore admits nothing, which is the right answer for an actor who passed
	// the park-blind role gate and holds no monitored park here.
	accessParkIDs := append([]string{}, access.AuthorizedParkIDs...)

	var result domain.LeadershipShedVideos
	var periodStart, periodEnd string
	// Grain proof: weighing_campaign_sheds is the row source (unique on
	// campaign_shed_id). weighing_campaigns is 1:1 with campaign_id, locations
	// park is 0..1 on its primary key, and workforce_members is filtered to
	// status='active' — the ONLY (tenant_id, user_id) uniqueness on that table is
	// the partial workforce_members_active_user_unique_idx, so without the
	// predicate a user with a prior non-active row matches twice and fans the
	// head row out. None of the three can multiply the shed row.
	err = r.pool.QueryRow(ctx, `
SELECT cs.campaign_id::text, cs.campaign_shed_id::text, cs.display_name, COALESCE(cs.operator_user_id::text, ''),
       cs.weighing_category, cs.status, COALESCE(cs.expected_animal_count, 0),
       COALESCE(park.name, ''),
       COALESCE(cs.start_business_date::text, wc.start_business_date::text, ''),
       COALESCE(op.display_name, ''),
       wc.period_start_date::text, COALESCE(wc.period_end_date::text, '')
FROM weighing_campaign_sheds cs
JOIN weighing_campaigns wc
  ON wc.tenant_id=cs.tenant_id AND wc.campaign_id=cs.campaign_id
LEFT JOIN locations park
  ON park.tenant_id=wc.tenant_id AND park.location_id=wc.park_id
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid AND cs.campaign_shed_id=$3::uuid
  -- Authorization over THIS row's park, in the statement that returns it. There is no assignee
  -- arm: leadership evidence review is WeighingMonitor-only, and an assignee reads their own
  -- captures through the roster instead. A caller admitted by neither arm gets no row, which
  -- surfaces as ErrNotFound and so cannot be told apart from a bucket that does not exist.
  AND (
    $4::boolean
    OR wc.park_id = ANY($5::uuid[])
  )`,
		tenantID, campaignID, campaignShedID, access.Unrestricted, accessParkIDs,
	).Scan(&result.CampaignID, &result.CampaignShedID, &result.ShedName, &result.OperatorUserID,
		&result.WeighingCategory, &result.Status, &result.EstimatedAnimalCount,
		&result.ParkName, &result.WeighDate, &result.OperatorDisplayName,
		&periodStart, &periodEnd)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LeadershipShedVideos{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	result.PeriodLabel = periodLabel(periodStart, periodEnd)
	result.Individual = []domain.Observation{}
	result.MaxShedVideos = domain.MaxShedProofArtifacts

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
  -- Superseded by a reopen: history, not the bucket's current submission.
  AND wso.withdrawn_at IS NULL`,
			tenantID, campaignID, campaignShedID).Scan(
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

	// Keyset page, ASC on (accepted_at, observation_id) — the SAME cursor shape the
	// roster's observations page already uses, so one decoder serves both and a
	// cursor means the same thing on either surface.
	//
	// Index-backed: weighing_observations_shed_keyset_idx
	// (tenant_id, campaign_id, campaign_shed_id, accepted_at, observation_id),
	// migration 000060. The three equality columns are the index prefix and the
	// two ordering columns follow in ASC keyset order, so Postgres walks the index
	// in output order and LIMIT stops the scan early — no sort node, no read of
	// the rows beyond the page. A row inserted mid-page lands after the cursor
	// (accepted_at only moves forward), so it appears on a later page instead of
	// shifting or duplicating rows already returned.
	rows, err := r.pool.Query(ctx, `
SELECT
  observation_id::text,
  campaign_id::text,
  campaign_shed_id::text,
  scanned_identifier,
  weight_kg::float8,
  proof_artifact_id::text,
  accepted_at
FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND (
    $4::timestamptz IS NULL
    OR (accepted_at, observation_id) > ($4::timestamptz, $5::uuid)
  )
ORDER BY accepted_at, observation_id
LIMIT $6`, tenantID, campaignID, campaignShedID,
		nullableTime(cur.AcceptedAt), nullableString(cur.ObservationID), limit+1)
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
			&observation.ScannedIdentifier,
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
	if len(result.Individual) > limit {
		last := result.Individual[limit-1]
		result.NextIndividualCursor = encodeObservationsCursor(observationsCursor{AcceptedAt: last.AcceptedAt, ObservationID: last.ObservationID})
		result.Individual = result.Individual[:limit]
	}
	return result, nil
}

// recordAnimalObservationMaxSerializationRetries bounds the internal retry of
// a losing SERIALIZABLE transaction in RecordAnimalObservation. Every retry
// only ever fires on ports.ErrWriteConflict (SQLSTATE 40001) -- a transaction
// that PostgreSQL itself says lost a race, not a transaction that hit a real
// duplicate (that returns ports.ErrDuplicateScan immediately, no retry). Five
// is generous for this shape of conflict: the loser only ever needed to wait
// for ONE bucket-lock holder to commit, so a retry either wins immediately
// against now-quiescent state or the bucket is under contention heavy enough
// that surfacing ErrWriteConflict to the caller (who is expected to resubmit)
// is the honest answer.
const recordAnimalObservationMaxSerializationRetries = 5

func (r *Repository) RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var lastErr error
	for attempt := 0; attempt < recordAnimalObservationMaxSerializationRetries; attempt++ {
		obs, err := r.recordAnimalObservationAttempt(ctx, cmd)
		if err == nil {
			return obs, nil
		}
		if !errors.Is(err, ports.ErrWriteConflict) {
			// A genuine outcome (success, ErrDuplicateScan, ErrForbidden, any
			// other domain/plumbing error) is never retried -- only a
			// SERIALIZABLE loser is, and only because losing tells us nothing
			// by itself about whether this request was actually valid.
			return domain.Observation{}, err
		}
		// A blind retry here is WRONG for the same-tag shape: by the time this
		// attempt aborted, the winner it conflicted with has already committed,
		// so a naive retry is a brand-new, non-overlapping transaction that
		// would see the winner's row via a plain read and walk straight into
		// the "updated" CTE's UPDATE-in-place branch -- silently overwriting
		// the winner exactly the way the original update-vs-update bug did,
		// just one retry later. SERIALIZABLE only guarantees THIS transaction
		// didn't corrupt anything; it says nothing about whether the NEXT one
		// should exist at all.
		//
		// So before retrying, ask the one question that discriminates the two
		// real-world shapes a 40001 loser can be: is there now an open row for
		// THIS SAME tag in THIS SAME bucket? If yes, some other capture
		// genuinely landed on the animal we were trying to capture first --
		// that is a real duplicate, not a retry-safe contention artifact, and
		// retrying would only reproduce the clobber. If no, the conflict came
		// from an UNRELATED transaction merely sharing this bucket's row lock
		// (a different animal captured at an overlapping instant, the ordinary
		// field case) and it is safe -- correct, even -- to retry.
		// Before concluding this is a real duplicate, check whether the row
		// that now occupies the tag was written by THIS SAME idempotency
		// key -- i.e. this very request's own earlier attempt actually won
		// the race and committed, and this attempt only saw 40001 because
		// PostgreSQL's SSI conflict detection can fire at COMMIT time on a
		// transaction whose write already landed. That is not a duplicate
		// scan at all: it is the idempotent-replay case, and the caller
		// must get back the winner's row, not ErrDuplicateScan.
		fingerprint := idempotencyFingerprint(cmd)
		if existing, idemErr := r.observationByIdemPool(ctx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint); idemErr == nil {
			return existing, nil
		} else if !errors.Is(idemErr, ports.ErrNotFound) {
			return domain.Observation{}, idemErr
		}
		dup, checkErr := r.observationTagHasOpenRow(ctx, cmd.TenantID, cmd.CampaignShedID, cmd.ScannedIdentifier)
		if checkErr != nil {
			return domain.Observation{}, checkErr
		}
		if dup {
			return domain.Observation{}, ports.ErrDuplicateScan
		}
		lastErr = err
	}
	return domain.Observation{}, lastErr
}

// observationByIdemPool looks up an already-recorded idempotent result in a
// FRESH transaction (not the aborted SERIALIZABLE attempt's tx, which is
// rolled back and unusable). Used only by the retry loop above to
// distinguish "this request's own earlier attempt already won" from "a
// different request holds this tag" after a 40001 loss -- see the comment
// at the call site. Returns ports.ErrNotFound (not an error) when no
// idempotency record exists yet, which the caller treats as "keep
// evaluating for a genuine duplicate", not as a failure.
func (r *Repository) observationByIdemPool(ctx context.Context, tenantID, eventType, idem, fingerprint string) (domain.Observation, error) {
	if idem == "" {
		return domain.Observation{}, ports.ErrNotFound
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	obs, ok, err := r.observationByIdemTx(ctx, tx, tenantID, eventType, idem, fingerprint)
	if err != nil {
		return domain.Observation{}, err
	}
	if !ok {
		return domain.Observation{}, ports.ErrNotFound
	}
	return obs, tx.Commit(ctx)
}

// observationTagHasOpenRow reports whether ANY row -- regardless of which
// caller wrote it -- is currently the open (submitted_at IS NULL) capture for
// this tag in this bucket. Used only to disambiguate a losing SERIALIZABLE
// transaction (ports.ErrWriteConflict) in the retry loop above: see the
// comment there for why this check, not a blind retry, is what makes the
// retry safe.
func (r *Repository) observationTagHasOpenRow(ctx context.Context, tenantID, campaignShedID, tag string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_observations
  WHERE tenant_id=$1::uuid
    AND campaign_shed_id=$2::uuid
    AND lower(btrim(scanned_identifier))=lower(btrim($3))
    AND submitted_at IS NULL
)`, tenantID, campaignShedID, tag).Scan(&exists)
	return exists, err
}

// recordAnimalObservationAttempt is ONE SERIALIZABLE transaction attempt at
// RecordAnimalObservation. SERIALIZABLE, not the pool default READ COMMITTED,
// is load-bearing here.
//
// recordUnknownAnimalObservationTx's "assigned_shed" CTE takes a
// FOR NO KEY UPDATE row lock on the bucket (F8, close-race fix) BEFORE the
// "updated"/"inserted" CTE pair decides whether this tag gets a fresh
// INSERT or an in-place UPDATE. That lock serialises two concurrent
// captures of the SAME bucket -- but under READ COMMITTED, serialising
// does NOT mean disambiguating: the loser, once unblocked, re-evaluates
// against the winner's now-committed row and silently takes the
// UPDATE-in-place branch, overwriting the winner's weight/proof with its
// own. Both callers get a 200: the winner's response already carries the
// weight it wrote, now stale, and the winner is never told a second
// capture clobbered it. That is the SAME data-integrity race the
// weighing_observations_one_open_tag_uidx partial index (000073) closes
// for the pure insert-vs-insert shape, but the index cannot see an
// UPDATE-vs-UPDATE race at all -- no INSERT means no 23505.
//
// A legitimate SEQUENTIAL rescan (the same client, tag still open,
// deliberately submitting a fresh idempotency key to replace a bad video
// before submit -- see TestRescanOfUnsubmittedTagUpdatesSameRowNoDuplicateRow)
// must keep updating in place: the two calls do not overlap in time, so
// there is nothing to disambiguate. SERIALIZABLE preserves exactly that
// case (no read-write conflict when the transactions do not overlap) while
// making PostgreSQL itself detect the TRUE overlap: the loser's implicit
// read of weighing_observations (via the "updated"/"submitted_duplicate"
// CTEs) is invalidated by the winner's commit, and PostgreSQL aborts the
// loser with serialization_failure (40001) instead of silently completing
// it against stale-then-refreshed state.
//
// BUT the same bucket-row lock also catches TWO DIFFERENT animals captured
// at overlapping instants in the SAME bucket -- the ordinary field case, not
// a race -- as SSI-conflict candidates. mapObservationUniqueViolation maps
// 40001 to ports.ErrWriteConflict, never to ports.ErrDuplicateScan, and
// RecordAnimalObservation retries that outcome (see the loop above) so a
// cross-animal SSI loser keeps retrying until it wins rather than being told
// it duplicated a scan it never made.
func (r *Repository) recordAnimalObservationAttempt(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return domain.Observation{}, err
	}
	defer tx.Rollback(ctx)
	fingerprint := idempotencyFingerprint(cmd)
	if existing, ok, err := r.observationByIdemTx(ctx, tx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint); err != nil || ok {
		if err != nil {
			return domain.Observation{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Observation{}, mapObservationUniqueViolation(err)
		}
		return existing, nil
	}
	// FREE-FLOW ONLY (maintainer decision 2026-07-31, column removed 2026-08-03
	// by 000078_weighing_observations_drop_animal_id.sql).
	//
	// There is no longer a "known animal" write path. Weighing stores what the
	// scanner read: the raw tag goes to scanned_identifier. weighing_observations
	// has no animal_id column at all — a request that happens to carry a goat
	// UUID must not cause a goats lookup, must not demand goat-scoped proof, and
	// must not be rejected because the herd does not know that id. Linking a
	// weight to a canonical goat is future enrichment/backfill work, not
	// something the operator write path waits on.
	//
	// The gates that remain are all WEIGHING-owned: campaign is live, the bucket
	// belongs to this campaign and this operator and is not canceled, the bucket is
	// an individual_animal bucket, the proof is a completed video scoped to that
	// bucket's shed, the weight is positive, and the idempotency key is honoured.
	return r.recordUnknownAnimalObservationTx(ctx, tx, cmd)
}

// lockCampaignRowForNoKeyUpdate is the SOLE entry point every capture-side
// writer (RecordAnimalObservation, RecordShedObservation) must call before
// locking a bucket row, and it must be called BEFORE that bucket lock, not
// after.
//
// LOCK-ORDERING CONVENTION (do not violate): campaign row, then bucket
// row(s), always in that order, tenant-wide. CloseCampaign (close.go)
// already locks the campaign row first and its buckets second (ORDER BY
// campaign_shed_id). Before this fix, the capture path took the OPPOSITE
// order: it locked the bucket first (assigned_shed / scope CTE's FOR NO KEY
// UPDATE) and only reached the campaign row later, via
// completeCampaignIfDone's UPDATE -- campaign-row lock acquired SECOND. Two
// transactions taking locks in opposite orders on the same two rows is a
// textbook AB-BA deadlock: a capture holding the bucket lock and waiting on
// the campaign row can deadlock against a close holding the campaign lock
// and waiting on the same bucket row. PostgreSQL's deadlock detector kills
// one side, but under load that surfaces as sporadic, hard-to-reproduce
// 40P01 errors on ordinary capture traffic.
//
// Taking this lock FIRST, unconditionally, before the bucket lock, makes
// every writer agree on one global order (campaign, then bucket) so no
// cycle can ever form. FOR NO KEY UPDATE (not FOR UPDATE) matches
// CloseCampaign's own lock mode for the same reason documented there: a
// plain FOR UPDATE on the campaign row conflicts with the FOR KEY SHARE a
// concurrent child-row FK check takes, which would manufacture a spurious
// deadlock of its own.
func (r *Repository) lockCampaignRowForNoKeyUpdate(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) error {
	var discard string
	err := tx.QueryRow(ctx, `
SELECT campaign_id::text
FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid
FOR NO KEY UPDATE`, tenantID, campaignID).Scan(&discard)
	if errors.Is(err, pgx.ErrNoRows) {
		// No campaign row to lock is not this function's problem to report --
		// the caller's own campaign-status CTE/gate will reject the write with
		// the correct domain error. Returning nil here (not the raw
		// ErrNoRows) keeps that classification in one place.
		return nil
	}
	return err
}

func (r *Repository) recordUnknownAnimalObservationTx(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	tag := strings.TrimSpace(cmd.ScannedIdentifier)
	before, err := r.unknownAnimalObservationBefore(ctx, tx, cmd, tag)
	if err != nil {
		return domain.Observation{}, err
	}
	// Lock-ordering convention (see lockCampaignRowForNoKeyUpdate): campaign
	// row before bucket row, always. Must happen before the query below,
	// whose assigned_shed CTE takes the bucket's FOR NO KEY UPDATE lock.
	if err := r.lockCampaignRowForNoKeyUpdate(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
		return domain.Observation{}, err
	}
	businessDayStart := biztime.BusinessDayStart(time.Now())
	businessDayEnd := businessDayStart.Add(24 * time.Hour)
	var obs domain.Observation
	// rowExisted distinguishes the `updated` CTE branch from `inserted`; obs.Superseded says
	// whether that update opened a NEW evidence round. Both false = brand-new capture;
	// rowExisted && !Superseded = a content-identical re-post that changed nothing.
	var rowExisted bool
	err = tx.QueryRow(ctx, `
WITH campaign AS (
  SELECT campaign_id
  FROM weighing_campaigns
  WHERE tenant_id=$1::uuid
    AND campaign_id=$2::uuid
    AND status IN ('published','in_progress','delayed')
), assigned_shed AS (
  -- F8: lock the bucket row BEFORE evaluating its status, closing the race
  -- where a plain SELECT (no row lock) reads 'pending/in_progress' just
  -- ahead of a concurrent CloseScope/CloseCampaign commit, then this
  -- transaction's INSERT into weighing_observations lands anyway because it
  -- is a different table the close's row lock never covered. FOR NO KEY
  -- UPDATE (not FOR UPDATE) matches CloseCampaign's own lock mode on this
  -- same table (see close.go) for the identical reason: a plain FOR UPDATE
  -- here would conflict with the FOR KEY SHARE a concurrent FK check takes,
  -- so NO KEY UPDATE avoids a spurious deadlock while still serialising
  -- against any closer. Lock ordering: this only ever locks its OWN single
  -- bucket, never more than one row, so it cannot participate in a
  -- lock-ordering cycle against CloseCampaign's campaign-then-buckets-by-id
  -- ordering or against ReopenScope (which also locks only its own bucket).
  SELECT campaign_shed_id, location_id, display_name
  FROM weighing_campaign_sheds AS cs
  WHERE tenant_id=$1::uuid
    AND campaign_id=$2::uuid
    AND campaign_shed_id=$8::uuid
    AND operator_user_id=$7::uuid
    -- Terminal-state gate. A bucket that is completed (operator already
    -- submitted), closed, or canceled must never accept a new or updated
    -- scan: only pending/in_progress buckets are still open for capture.
    AND status IN ('pending','in_progress')
    -- Bucket MODE gate. A per-animal weight belongs in an individual_animal
    -- bucket; the lump-sum bucket has its own writer (RecordShedObservation).
    -- This is a weighing-owned check about the BUCKET, not about the animal, so
    -- it is not a herd dependency. It is explicit here because the removed
    -- expected-animal roster join used to enforce it as a side effect.
    AND weighing_category='individual_animal'
  ORDER BY created_at, campaign_shed_id
  LIMIT 1
  FOR NO KEY UPDATE OF cs
), rejected_proof_guard AS (
  -- Re-check INSIDE this transaction, under the bucket lock taken above. The service layer also
  -- checks, but that read runs in its own connection before this transaction opens: a verifier
  -- rejecting the proof in that window would slip through and the operator would "re-record" with
  -- the very video that was sent back. The lump-sum writer has carried this CTE for the same
  -- reason; the individual-animal writer had only the outside-the-transaction read.
  SELECT 1
  FROM assigned_shed s
  WHERE NOT EXISTS (
    SELECT 1 FROM weighing_observations rejected
    WHERE rejected.tenant_id=$1::uuid
      AND rejected.campaign_shed_id=s.campaign_shed_id
      AND rejected.verification_status='rework'
      AND rejected.proof_artifact_id=$5::uuid
  )
), proof_ok AS (
  SELECT proof.proof_id
  FROM assigned_shed s
  JOIN rejected_proof_guard ON true
  JOIN proof_artifacts proof ON true
  WHERE proof.tenant_id=$1::uuid
    AND proof.proof_id=$5::uuid
    AND proof.upload_state='completed'
    AND proof.proof_type='video'
    AND proof.scope_type='shed'
    AND proof.scope_id=s.location_id
	), submitted_duplicate AS (
	  -- A tag already captured AND SUBMITTED (submitted_at IS NOT NULL) in an
	  -- earlier round, for this SAME bucket and SAME business day, blocks a
	  -- brand-new capture: it must be rejected, not silently merged into the
	  -- old row and not inserted as a second row. Comparison is
	  -- case-insensitive and trimmed. A tag still in the current un-submitted
	  -- round (submitted_at IS NULL) is NOT a duplicate -- it updates in place
	  -- below.
	  SELECT 1
	  FROM assigned_shed s
	  JOIN weighing_observations observation
	    ON observation.tenant_id=$1::uuid
	   AND observation.campaign_id=$2::uuid
	   AND observation.campaign_shed_id=s.campaign_shed_id
	   AND lower(btrim(observation.scanned_identifier))=lower(btrim($3))
	   AND observation.submitted_at IS NOT NULL
	   AND observation.verification_status <> 'rework'
	   AND observation.submitted_at >= $10::timestamptz
	   AND observation.submitted_at < $11::timestamptz
	  LIMIT 1
	), prior AS (
	  -- Pre-update snapshot of the row the updated CTE is about to touch, so the write can tell
	  -- a REAL edit from a content-identical re-post. All CTEs see the same snapshot, so
	  -- prior.* is the OLD weight/proof even though the UPDATE below runs in the same
	  -- statement (observation.* inside RETURNING is already the NEW value and cannot
	  -- answer "did anything change?").
	  --
	  -- The row lock is taken here, AFTER assigned_shed's bucket lock, and the join on
	  -- assigned_shed forces that ordering: campaign -> bucket -> observation, always.
	  -- Locking here (rather than trusting the pre-query unknownAnimalObservationBefore
	  -- read) is what makes the comparison race-free against a concurrent editor.
	  SELECT observation.observation_id, observation.weight_kg, observation.proof_artifact_id
	  FROM assigned_shed s
	  JOIN weighing_observations observation
	    ON observation.tenant_id=$1::uuid
	   AND observation.campaign_id=$2::uuid
	   AND observation.campaign_shed_id=s.campaign_shed_id
	   AND lower(btrim(observation.scanned_identifier))=lower(btrim($3))
	   AND (observation.submitted_at IS NULL OR observation.verification_status='rework')
	  FOR NO KEY UPDATE OF observation
	), updated AS (
	  -- A re-post that changes NOTHING is not a new evidence round.
	  --
	  -- The Android client captures in two phases (weight, then the same weight again
	  -- carrying the uploaded proof id) under two different idempotency keys. Keys are all
	  -- this statement can compare, so both phases land here and the second one used to
	  -- stamp a fresh accepted_at and report is_update=true -- an EDIT. The service layer
	  -- then withdrew the pending verification item and raised a second round, and a second
	  -- weighing.observation_accepted event was emitted, for a capture nobody edited.
	  --
	  -- The changed-test is the same weight-or-proof comparison auditAnimalObservation already
	  -- makes to choose between 'weighing.observation_updated' and the no-op action
	  -- 'weighing.observation_reaccepted'. The audit trail has always distinguished these
	  -- two cases; the evidence round and the outbox now do too. A genuine edit (either
	  -- field differs) is untouched: it still resets verification, still advances
	  -- accepted_at, and still reports is_update=true.
	  UPDATE weighing_observations observation
	  SET weight_kg=$4,
	      proof_artifact_id=p.proof_id,
	      recorded_by=$7::uuid,
	      accepted_at=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                         OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                       THEN now() ELSE observation.accepted_at END,
	      submitted_at=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                          OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                        THEN NULL ELSE observation.submitted_at END,
	      verification_status=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                                 OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                               THEN 'pending' ELSE observation.verification_status END,
	      verified_by=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                         OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                       THEN NULL ELSE observation.verified_by END,
	      verified_at=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                         OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                       THEN NULL ELSE observation.verified_at END,
	      rework_reason=CASE WHEN prior.weight_kg IS DISTINCT FROM $4::numeric
	                           OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id
	                         THEN NULL ELSE observation.rework_reason END
	  FROM campaign c
	  JOIN assigned_shed s ON true
	  JOIN proof_ok p ON true
	  -- Correlated in WHERE, not in an ON clause: a FROM-item join condition may not
	  -- reference the UPDATE target. prior yields at most one row (the single open row for
	  -- this tag, guaranteed by weighing_observations_one_open_tag_uidx).
	  JOIN prior ON true
	  WHERE prior.observation_id=observation.observation_id
	    AND observation.tenant_id=$1::uuid
    AND observation.campaign_id=$2::uuid
	    AND observation.campaign_shed_id=s.campaign_shed_id
	    AND lower(btrim(observation.scanned_identifier))=lower(btrim($3))
	    -- Only the current, un-submitted round or an explicit verifier rework
	    -- may be updated in place. Other submitted rows are frozen; classify()
	    -- turns them into ErrDuplicateScan via submitted_duplicate above.
	    AND (observation.submitted_at IS NULL OR observation.verification_status='rework')
  RETURNING observation.observation_id::text, observation.campaign_id::text,
    COALESCE(observation.campaign_shed_id::text,'') AS campaign_shed_id_text,
    observation.scanned_identifier AS scanned_identifier_text, observation.weight_kg::float8,
    observation.proof_artifact_id::text,
    COALESCE(observation.expected_location_id::text,'') AS expected_location_id_text,
    -- The row's REAL actual location, not a hardcoded ''. These two used to return empty
    -- strings, so every edit-path event silently dropped the operator-supplied location that
    -- the inserted-path event carries -- a value the row itself has had all along (the UPDATE
    -- above deliberately does not touch actual_location_id, so this is the location captured
    -- when the animal was first scanned). The fan-out fix removes the two-phase traffic that
    -- was flooding this path, but the path still runs for what it was always for: a genuine
    -- weight correction and a verifier-rework re-capture. Those must not lose the location.
    COALESCE(observation.actual_location_id::text,'') AS actual_location_id_text,
    COALESCE(observation.actual_location_label,'') AS actual_location_label_text, observation.accepted_at,
    -- is_update means "this write opened a NEW evidence round", not merely "a row already
    -- existed". A content-identical re-post reports FALSE so the service does not withdraw
    -- and re-raise a verification item for evidence that never changed.
    (prior.weight_kg IS DISTINCT FROM $4::numeric
       OR prior.proof_artifact_id IS DISTINCT FROM p.proof_id) AS is_update,
    TRUE AS row_existed
), inserted AS (
  INSERT INTO weighing_observations (
    tenant_id, campaign_id, campaign_shed_id, scanned_identifier,
    weight_kg, proof_artifact_id, expected_location_id, expected_location_label,
    actual_location_id, actual_location_label,
    recorded_by, idempotency_key
  )
  SELECT $1::uuid, $2::uuid, s.campaign_shed_id, $3,
    $4, p.proof_id, s.location_id, s.display_name,
    -- Where the operator says the animal actually was. CLIENT-SUPPLIED ($9), never
    -- read from goats.current_location_id. The human-readable label is deliberately
    -- NOT resolved here: the write path is restricted to weighing-owned tables, so
    -- the locations catalogue is joined on the READ path instead.
    NULLIF($9, '')::uuid, NULL,
    -- No roster verdict is stored. mismatch_status was DROPPED (000082): free-flow
    -- has no expected set, so a scan cannot be "expected", "wrong shed" or "extra".
    -- Stamping 'extra_scan' on every row turned an operator's correct, in-shed work
    -- into an exception queue for whoever read the table.
    $7::uuid, $6
  FROM campaign c
  JOIN assigned_shed s ON true
  JOIN proof_ok p ON true
  WHERE NOT EXISTS (SELECT 1 FROM updated)
    AND NOT EXISTS (SELECT 1 FROM submitted_duplicate)
  ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
  RETURNING observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,'') AS campaign_shed_id_text, scanned_identifier AS scanned_identifier_text, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,'') AS expected_location_id_text, COALESCE(actual_location_id::text,'') AS actual_location_id_text, COALESCE(actual_location_label,'') AS actual_location_label_text, accepted_at, FALSE AS is_update, FALSE AS row_existed
)
SELECT observation_id, campaign_id, campaign_shed_id_text, scanned_identifier_text, weight_kg, proof_artifact_id, expected_location_id_text, actual_location_id_text, actual_location_label_text, accepted_at, is_update, row_existed FROM updated
UNION ALL
SELECT observation_id, campaign_id, campaign_shed_id_text, scanned_identifier_text, weight_kg, proof_artifact_id::text, expected_location_id_text, actual_location_id_text, actual_location_label_text, accepted_at, is_update, row_existed FROM inserted`,
		cmd.TenantID, cmd.CampaignID, tag, cmd.WeightKg, cmd.ProofArtifactID, cmd.IdempotencyKey, cmd.RecordedBy, cmd.CampaignShedID, cmd.ActualLocationID, businessDayStart, businessDayEnd).
		Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.ScannedIdentifier, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt, &obs.Superseded, &rowExisted)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Observation{}, r.classifyFreeFlowObservationRejection(ctx, tx, cmd, tag, businessDayStart, businessDayEnd)
	}
	if err != nil {
		// A concurrent capture of the SAME tag in the SAME bucket, with a
		// DIFFERENT idempotency key, can win the "updated" CTE's race and
		// insert first; this transaction's own insert then trips
		// weighing_observations_one_open_tag_uidx (added in migration 000072)
		// instead of silently creating a second "current" open row for the
		// tag. Map it to the same typed conflict a same-bucket submitted
		// duplicate already returns, so the loser reads as a clean domain
		// conflict, never a raw 500.
		return domain.Observation{}, mapObservationUniqueViolation(err)
	}
	// A bucket that has taken a scan is being WORKED ON, and must stop reading as
	// untouched. Same transaction as the capture: the write above and this status
	// are one fact, and the bucket row is already held FOR NO KEY UPDATE by the
	// assigned_shed CTE of that same statement, so no new lock is taken and no new
	// lock ordering is introduced.
	if err := r.markScopeInProgressOnCapture(ctx, tx, cmd.TenantID, obs.CampaignShedID); err != nil {
		return domain.Observation{}, err
	}
	if err := r.completeCampaignIfDone(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
		return domain.Observation{}, err
	}
	fingerprint := idempotencyFingerprint(cmd)
	if err := r.recordIdempotency(ctx, tx, cmd.TenantID, "weighing.observation_accepted", cmd.IdempotencyKey, fingerprint, "weighing_observation", obs.ObservationID, obs); err != nil {
		return domain.Observation{}, err
	}
	// weighing.observation_accepted announces an accepted observation STATE. A re-post that
	// left every field exactly as it was announces nothing: the state consumers already saw
	// is still the current state. Emitting it anyway is pure fan-out -- the first real device
	// run put 20 of these on the outbox for 10 captures, because the two-phase client posted
	// each capture under two keys and each key minted its own event. The idempotency record
	// above is still written for the new key, so an exact replay of that key still short-
	// circuits to the cached result.
	//
	// A brand-new capture (!rowExisted) and a genuine edit (obs.Superseded) both still emit.
	if !rowExisted || obs.Superseded {
		if err := r.enqueue(ctx, tx, cmd.TenantID, "weighing.observation_accepted", obs.ObservationID, cmd.IdempotencyKey, fingerprint, obs); err != nil {
			return domain.Observation{}, err
		}
	}
	if err := r.auditAnimalObservation(ctx, tx, cmd, before, obs); err != nil {
		return domain.Observation{}, err
	}
	// A losing concurrent capture can also surface its serialization_failure
	// (40001) only at COMMIT, not on the write query itself, depending on
	// exactly when PostgreSQL's SSI conflict detection fires relative to the
	// row-lock wait -- so this Commit error is mapped through the same
	// translator as the write query's error, not returned raw.
	if err := tx.Commit(ctx); err != nil {
		return domain.Observation{}, mapObservationUniqueViolation(err)
	}
	return obs, nil
}

// classifyFreeFlowObservationRejection turns an empty write into the RIGHT error.
//
// Every gate lives in one CTE chain, so a refusal arrives as ErrNoRows with no reason
// attached. Without this, a wrong-operator capture reported 404 not_found instead of
// 403 permission_denied -- an authorization signal degraded into "does not exist".
//
// Reads ONLY weighing-owned tables plus proof_artifacts, in the same transaction: no
// goats, no expected-animal roster, no clinical state.
func (r *Repository) classifyFreeFlowObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordAnimalObservation, tag string, businessDayStart, businessDayEnd time.Time) error {
	var campaignStatus string
	err := tx.QueryRow(ctx, `
SELECT status FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, cmd.TenantID, cmd.CampaignID).Scan(&campaignStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	if campaignStatus != domain.StatusPublished && campaignStatus != domain.StatusInProgress && campaignStatus != domain.StatusDelayed {
		return ports.ErrImmutable
	}

	var operatorID, category, shedStatus string
	err = tx.QueryRow(ctx, `
SELECT COALESCE(operator_user_id::text,''), weighing_category, status
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid`,
		cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID).Scan(&operatorID, &category, &shedStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrNotFound
	}
	if err != nil {
		return err
	}
	// A bucket in any terminal state (completed, closed, canceled) is immutable
	// to new capture. "completed" here means the operator already submitted —
	// there is no separate "submitted" enum value; completed IS submitted,
	// awaiting verification. Only pending/in_progress buckets still accept
	// scans.
	if shedStatus != "pending" && shedStatus != domain.StatusInProgress {
		return ports.ErrImmutable
	}
	if operatorID != cmd.RecordedBy {
		return ports.ErrForbidden
	}
	if category != domain.CategoryIndividualAnimal {
		return ports.ErrNotFound
	}

	var proofOK bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM weighing_campaign_sheds cs
  JOIN proof_artifacts proof
    ON proof.tenant_id=cs.tenant_id
   AND proof.proof_id=$4::uuid
   AND proof.upload_state='completed'
   AND proof.proof_type='video'
   AND proof.scope_type='shed'
   AND proof.scope_id=cs.location_id
  WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid AND cs.campaign_shed_id=$3::uuid
)`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.ProofArtifactID).Scan(&proofOK); err != nil {
		return err
	}
	if !proofOK {
		return ports.ErrInvalidArgument
	}

	// Every ordinary gate passed, so the write's absence must be the
	// duplicate-scan gate: this tag was already captured AND SUBMITTED in an
	// earlier round for this same bucket and business day.
	var duplicate bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM weighing_observations observation
  WHERE observation.tenant_id=$1::uuid
    AND observation.campaign_id=$2::uuid
    AND observation.campaign_shed_id=$3::uuid
    AND lower(btrim(observation.scanned_identifier))=lower(btrim($4))
    AND observation.submitted_at IS NOT NULL
    AND observation.verification_status <> 'rework'
    AND observation.submitted_at >= $5::timestamptz
    AND observation.submitted_at < $6::timestamptz
)`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, tag, businessDayStart, businessDayEnd).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate {
		return ports.ErrDuplicateScan
	}
	return ports.ErrNotFound
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
	// Lock-ordering convention (see lockCampaignRowForNoKeyUpdate): campaign
	// row before bucket row, always. Must happen before the query below,
	// whose scope CTE takes the bucket's FOR NO KEY UPDATE lock -- this
	// UPDATE reaches the campaign row a second time later via
	// completeCampaignIfDone, so the campaign lock must be acquired here,
	// up front, not implicitly deferred to that later statement.
	if err := r.lockCampaignRowForNoKeyUpdate(ctx, tx, cmd.TenantID, cmd.CampaignID); err != nil {
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
	), scope AS (
	  -- F8: same fix as RecordAnimalObservation's assigned_shed CTE above -
	  -- lock the bucket before trusting its status, using CloseCampaign's own
	  -- FOR NO KEY UPDATE lock mode. See the assigned_shed comment for the full
	  -- rationale and the lock-ordering argument (this locks only its own
	  -- single bucket, so no cycle with CloseCampaign or ReopenScope).
	  SELECT cs.campaign_shed_id, cs.location_id
	  FROM campaign c
	  JOIN weighing_campaign_sheds cs
	    ON cs.tenant_id=$1::uuid
	   AND cs.campaign_id=c.campaign_id
	   AND cs.campaign_shed_id=$3::uuid
	   AND cs.weighing_category='per_shed_partition'
	   AND cs.operator_user_id=$7::uuid
	   -- Terminal-state gate: a lump-sum write must NEVER resurrect a
	   -- completed/closed/canceled bucket back to completed.
	   AND cs.status IN ('pending','in_progress')
	  FOR NO KEY UPDATE OF cs
	), rejected_proof_guard AS (
	  -- Prevent re-using a proof that was attached to a verifier-rejected (rework)
	  -- observation for this same bucket. The operator must record a new video.
	  SELECT 1
	  FROM scope
	  WHERE NOT EXISTS (
	    SELECT 1 FROM weighing_shed_observations rejected_obs
	    JOIN weighing_shed_observation_proofs rejected_proofs
	      ON rejected_proofs.shed_observation_id=rejected_obs.shed_observation_id
	    WHERE rejected_obs.tenant_id=$1::uuid
	      AND rejected_obs.campaign_shed_id=scope.campaign_shed_id
	      AND rejected_obs.verification_status='rework'
	      AND rejected_proofs.proof_artifact_id=ANY($5::uuid[])
	  )
	), proof_bundle AS (
	  SELECT array_agg(proof.proof_id ORDER BY requested.proof_position) AS proof_ids
	  FROM scope
	  CROSS JOIN unnest($5::uuid[]) WITH ORDINALITY AS requested(proof_id, proof_position)
	  JOIN rejected_proof_guard ON true
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
		// A concurrent lump-sum submit for the same bucket trips
		// weighing_shed_observations_one_open_scope_uidx; map it to the domain
		// conflict instead of letting the raw 23505 escape as a 500.
		return domain.Observation{}, mapShedUniqueViolation(err, "", "")
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
	// NO markScopeInProgressOnCapture here, deliberately. On the lump-sum path the
	// capture IS the submit: one RecordShedObservation carries the total weight, the
	// count and the whole video bundle, and weighing_shed_observations_one_open_scope_uidx
	// admits exactly one open row per bucket, so there is no second lump-sum capture
	// to be "mid-shed" between. This bucket goes pending -> completed in one
	// transaction and has no in-progress window to report. Writing 'in_progress'
	// three lines above this UPDATE would be a status no reader could ever observe.
	completed, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='completed', completed_at=COALESCE(completed_at, now()), updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status IN ('pending','in_progress')`, cmd.TenantID, cmd.CampaignShedID)
	if err != nil {
		return domain.Observation{}, err
	}
	// F8 defense in depth: the `scope` CTE above now takes FOR NO KEY UPDATE OF
	// cs on this exact row before the INSERT, so a concurrent CloseScope/
	// CloseCampaign can no longer commit between that read and this UPDATE -
	// RowsAffected()==0 here should therefore be structurally unreachable.
	// Treat it as an error anyway rather than silently swallowing it: a
	// mismatch here would otherwise mean the caller is told the capture
	// succeeded while the bucket's own status transition silently failed,
	// leaving a 'pending' verification row nothing will ever surface as done.
	if completed.RowsAffected() == 0 {
		return domain.Observation{}, fmt.Errorf("record shed observation: bucket %s did not transition to completed after accepting observation %s: %w", cmd.CampaignShedID, obs.ObservationID, ports.ErrImmutable)
	}
	if err := r.enqueueShedSubmissionCompleted(ctx, tx, cmd.TenantID, cmd.CampaignShedID); err != nil {
		return domain.Observation{}, err
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

// markScopeInProgressOnCapture moves a bucket from 'pending' to 'in_progress' the
// moment the FIRST capture lands in it.
//
// WHY. weighing_campaign_sheds.status has always admitted 'in_progress', but no
// capture path ever wrote it: only ReopenScope and the rework/reactivate verdict
// paths did. A shed with scans in it therefore read 'pending' — indistinguishable
// from one nobody had touched — right up until the operator pressed Submit. A
// director watching the Operators screen could not tell anyone was mid-shed, and
// operator_summaries' `count(*) FILTER (WHERE cs.status='in_progress')` column was
// structurally always 0: it rendered a state that could not occur.
//
// ONLY pending -> in_progress. The WHERE clause names the source status
// explicitly rather than excluding the terminal ones, which buys two properties at
// once:
//
//   - A completed/closed/canceled bucket is untouched. A late capture must not
//     resurrect a submitted bucket. The capture statement itself already refuses
//     those buckets (its assigned_shed / scope CTE gates on status IN
//     ('pending','in_progress')), and this must not become a second, laxer door
//     into the same table.
//   - It is idempotent by construction. The second, third and tenth capture find
//     the bucket already 'in_progress', match no row, and write nothing — no
//     status rewrite, no updated_at churn, no event. RowsAffected is therefore
//     deliberately NOT checked: zero rows is the normal, expected answer for
//     every capture after the first.
//
// NOT a work_state write. work_state on weighing_work_items has a single writer,
// the kernel sweeper (see kernel.go). pending -> in_progress is not a terminal
// transition either way, so reconcileTerminalWorkItems does not act on it and
// ReactivateWorkItemsForBucket has nothing to undo — the item was and stays
// 'scheduled'.
//
// NO EVENT. Every reader of this state — operatorSummaries, the campaign-sheds
// page, the leadership sheds page, close.go's readiness fragments — reads
// cs.status live from this table. There is no derived projection row to keep in
// step, so an outbox event would have no consumer, and this repo bans a producer
// without one.
func (r *Repository) markScopeInProgressOnCapture(ctx context.Context, tx pgx.Tx, tenantID, campaignShedID string) error {
	if campaignShedID == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds
SET status='in_progress', updated_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND status='pending'`, tenantID, campaignShedID)
	return err
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
  AND cs.status NOT IN ('completed','closed','canceled')`, tenantID, campaignID, campaignShedID)
	if err != nil || completed.RowsAffected() == 0 {
		return err
	}
	return r.enqueueShedSubmissionCompleted(ctx, tx, tenantID, campaignShedID)
}

// classifyIndividualScopeSubmitFailure runs inside the SAME transaction as the
// failed completion UPDATE in SubmitIndividualScope, to distinguish an
// ordinary "bucket not found / not owned by this operator" 404 from the
// scope-self-consistency failure where the submitted scan list omits an
// already-captured, proof-complete observation for this shed. It must never
// join weighing_expected_animals, the herd register, vaccination, or shed
// ownership — only the shed's own weighing_observations.
func (r *Repository) classifyIndividualScopeSubmitFailure(ctx context.Context, tx pgx.Tx, tenantID, campaignID, campaignShedID, actorID string, scannedIdentifiers []string) error {
	var bucketExists bool
	var shedStatus string
	if err := tx.QueryRow(ctx, `
SELECT true, cs.status
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
  AND cs.weighing_category='individual_animal'
  AND cs.operator_user_id=$4::uuid`, tenantID, campaignID, campaignShedID, actorID).Scan(&bucketExists, &shedStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrNotFound
		}
		return fmt.Errorf("check individual scope bucket exists: %w", err)
	}
	if !bucketExists {
		return ports.ErrNotFound
	}
	// A closed or canceled bucket must not be completed by submit. A bucket
	// already 'completed' means "operator already submitted" (there is no
	// separate "submitted" enum value) -- a second, non-replay submit attempt
	// against it is also a terminal-state conflict, not a scope-incompleteness
	// finding.
	if shedStatus != "pending" && shedStatus != domain.StatusInProgress {
		return ports.ErrImmutable
	}
	// Named separately from scope-incompleteness: the operator HAS captured every
	// animal, but a verifier sent one back and it has not been re-recorded yet.
	// Collect the TAGS, not just a boolean. "Re-record that animal" is unactionable in a shed
	// of forty: the operator cannot tell which card to redo, and the card itself still renders
	// as captured. The identifiers are already in hand here, so name them.
	reworkTags := []string{}
	reworkRows, err := tx.Query(ctx, `
SELECT observation.scanned_identifier
FROM weighing_observations observation
WHERE observation.tenant_id=$1::uuid
  AND observation.campaign_id=$2::uuid
  AND observation.campaign_shed_id=$3::uuid
  AND observation.verification_status='rework'
ORDER BY observation.scanned_identifier
LIMIT 20`, tenantID, campaignID, campaignShedID)
	if err != nil {
		return fmt.Errorf("check individual scope rework rows: %w", err)
	}
	for reworkRows.Next() {
		var tag string
		if err := reworkRows.Scan(&tag); err != nil {
			reworkRows.Close()
			return fmt.Errorf("scan individual scope rework row: %w", err)
		}
		reworkTags = append(reworkTags, tag)
	}
	reworkRows.Close()
	if err := reworkRows.Err(); err != nil {
		return fmt.Errorf("check individual scope rework rows: %w", err)
	}
	if len(reworkTags) > 0 {
		return ports.ReworkNotRecapturedFor(reworkTags)
	}
	var omitsObserved bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_observations observation
  JOIN proof_artifacts proof
    ON proof.tenant_id=observation.tenant_id
   AND proof.proof_id=observation.proof_artifact_id
   AND proof.upload_state='completed'
   AND proof.proof_type='video'
  WHERE observation.tenant_id=$1::uuid
    AND observation.campaign_id=$2::uuid
    AND observation.campaign_shed_id=$3::uuid
    AND observation.weight_kg > 0
    AND lower(btrim(observation.scanned_identifier)) <> ALL(SELECT lower(btrim(unnest($4::text[]))))
)`, tenantID, campaignID, campaignShedID, scannedIdentifiers).Scan(&omitsObserved); err != nil {
		return fmt.Errorf("check individual scope submit omits observed animals: %w", err)
	}
	if omitsObserved {
		return ports.ErrScopeIncomplete
	}
	// The pair rule. The submit UPDATE above already REFUSES to complete a bucket
	// where any scanned animal lacks a weight or a completed video, but that
	// refusal is a silent RowsAffected()==0 -- it used to fall through to
	// ErrNotFound and told the operator that a shed they are standing in does not
	// exist. Name the animals instead.
	//
	// This runs in the SAME transaction as the failed completion UPDATE, not as a
	// pre-check: a pre-check would race a concurrent capture and could report an
	// animal as incomplete a millisecond after its video finished (or, worse,
	// pass a bucket that went incomplete in between). Only the observations of
	// THIS bucket are consulted -- no roster, no herd register, no expected count.
	incomplete := &ports.CaptureIncomplete{}
	rows, err := tx.Query(ctx, `
SELECT requested.scanned_identifier,
       EXISTS (
         SELECT 1 FROM weighing_observations observation
         WHERE observation.tenant_id=$1::uuid
           AND observation.campaign_id=$2::uuid
           AND observation.campaign_shed_id=$3::uuid
           AND lower(btrim(observation.scanned_identifier))=requested.scanned_identifier
           AND observation.weight_kg > 0
       ) AS has_weight,
       EXISTS (
         SELECT 1 FROM weighing_observations observation
         JOIN proof_artifacts proof
           ON proof.tenant_id=observation.tenant_id
          AND proof.proof_id=observation.proof_artifact_id
          AND proof.upload_state='completed'
          AND proof.proof_type='video'
         WHERE observation.tenant_id=$1::uuid
           AND observation.campaign_id=$2::uuid
           AND observation.campaign_shed_id=$3::uuid
           AND lower(btrim(observation.scanned_identifier))=requested.scanned_identifier
       ) AS has_video
FROM (SELECT DISTINCT lower(btrim(unnest($4::text[]))) AS scanned_identifier) AS requested
ORDER BY 1`, tenantID, campaignID, campaignShedID, scannedIdentifiers)
	if err != nil {
		return fmt.Errorf("classify individual scope submit pair completeness: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var identifier string
		var hasWeight, hasVideo bool
		if err := rows.Scan(&identifier, &hasWeight, &hasVideo); err != nil {
			return fmt.Errorf("scan individual scope submit pair completeness: %w", err)
		}
		switch {
		case !hasWeight:
			// No weight at all (including "no capture row for this tag"). The video
			// cannot stand alone, so this is reported as the missing half that it is.
			incomplete.MissingWeight = append(incomplete.MissingWeight, identifier)
		case !hasVideo:
			incomplete.MissingVideo = append(incomplete.MissingVideo, identifier)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate individual scope submit pair completeness: %w", err)
	}
	if len(incomplete.MissingWeight) > 0 || len(incomplete.MissingVideo) > 0 {
		return incomplete
	}
	return ports.ErrNotFound
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
  AND cs.operator_user_id=$4::uuid
  -- Terminal-state gate: a closed or canceled bucket must never be completed
  -- by submit. An already-completed bucket is also excluded here (submit is
  -- not re-entrant against a completed bucket outside idempotency replay).
  AND cs.status IN ('pending','in_progress')
  AND cardinality($5::text[]) > 0
  -- Rework gate: a REJECTED animal must be re-captured before this bucket can be
  -- submitted again. Re-capture is what clears submitted_at and returns the row to
  -- 'pending' (see the recapture UPDATE's submitted_at/verification_status CASE);
  -- a re-submit of the UNCHANGED capture used to fall straight through here and mark
  -- the bucket 'completed' while the rejected animal stayed in 'rework' forever, with
  -- no new verification item ever raised. The verifier's rejection was silently
  -- dropped and the bucket read as done.
  AND NOT EXISTS (
    SELECT 1 FROM weighing_observations rework_row
    WHERE rework_row.tenant_id=cs.tenant_id
      AND rework_row.campaign_id=cs.campaign_id
      AND rework_row.campaign_shed_id=cs.campaign_shed_id
      AND rework_row.verification_status='rework'
  )
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
      AND lower(btrim(observation.scanned_identifier))=lower(btrim(captured.scanned_identifier))
      AND observation.weight_kg > 0
    )
  )
  AND NOT EXISTS (
    SELECT 1 FROM weighing_observations observation
    JOIN proof_artifacts proof
      ON proof.tenant_id=observation.tenant_id
     AND proof.proof_id=observation.proof_artifact_id
     AND proof.upload_state='completed'
     AND proof.proof_type='video'
    WHERE observation.tenant_id=cs.tenant_id
      AND observation.campaign_id=cs.campaign_id
      AND observation.campaign_shed_id=cs.campaign_shed_id
      AND observation.weight_kg > 0
      AND lower(btrim(observation.scanned_identifier)) <> ALL(SELECT lower(btrim(unnest($5::text[]))))
  )`, tenantID, campaignID, campaignShedID, actorID, scannedIdentifiers)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return r.classifyIndividualScopeSubmitFailure(ctx, tx, tenantID, campaignID, campaignShedID, actorID, scannedIdentifiers)
	}
	// Freeze this round's captures. Marking submitted_at is what lets a
	// rescan of the SAME tag after a future ReopenScope be recognised as a
	// duplicate of already-accepted work, rather than a silent update of the
	// old row. Only rows still in the current draft round (submitted_at IS
	// NULL) are marked -- this is idempotent-safe to run again for a
	// non-replay path since NULL rows are the only target.
	if _, err := tx.Exec(ctx, `
UPDATE weighing_observations
SET submitted_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND submitted_at IS NULL
  AND lower(btrim(scanned_identifier))=ANY(SELECT lower(btrim(unnest($4::text[]))))`, tenantID, campaignID, campaignShedID, scannedIdentifiers); err != nil {
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

// ReopenScope returns the shed-observation ids whose submissions this reopen
// superseded, so the caller can retire the verification items raised for them
// through the verification module's own port (this module never writes another
// module's tables). A replay of the same reopen key re-reports the ids it
// withdrew the first time, so a retry after a mid-flight crash heals the
// still-pending item instead of leaving it decidable forever.
func (r *Repository) ReopenScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey, reason string) ([]string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	fingerprint := idempotencyFingerprint(map[string]any{
		"campaign_id":      campaignID,
		"campaign_shed_id": campaignShedID,
		"reopened_by":      actorID,
		"reason":           reason,
	})
	if _, resourceType, _, ok, err := r.idempotencyResource(ctx, tx, tenantID, "weighing.scope_reopened", idempotencyKey, fingerprint); err != nil || ok {
		if err != nil {
			return nil, err
		}
		if resourceType != "weighing_campaign_shed" {
			return nil, ports.ErrIdempotencyConflict
		}
		alreadyWithdrawn, err := withdrawnShedObservationIDs(ctx, tx, tenantID, campaignID, campaignShedID)
		if err != nil {
			return nil, err
		}
		return alreadyWithdrawn, tx.Commit(ctx)
	}
	result, err := tx.Exec(ctx, `
UPDATE weighing_campaign_sheds cs
SET status='in_progress', completed_at=NULL, closed_at=NULL, closed_by=NULL, close_reason=NULL, closure_kind=NULL, updated_at=now()
FROM weighing_campaigns campaign
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id=$2::uuid
  AND cs.campaign_shed_id=$3::uuid
  AND cs.status IN ('completed','closed')
  AND campaign.tenant_id=cs.tenant_id
  AND campaign.campaign_id=cs.campaign_id`, tenantID, campaignID, campaignShedID)
	if err != nil {
		// Since 000089 a finished bucket no longer holds its (park, date, shed)
		// slot, so that slot may already have been given to a NEWER task. Pulling
		// this one back to 'in_progress' would then create the one thing still
		// forbidden -- two people owing the same shed on the same date -- and the
		// unique index refuses it. Surface it as the scheduling conflict it is,
		// naming the bucket, instead of an opaque 500. The name/date lookup runs
		// only on this error path, so the happy path keeps its single write.
		weighDate, displayName := r.shedLabelForConflict(ctx, tenantID, campaignShedID)
		return nil, mapShedUniqueViolation(err, weighDate, displayName)
	}
	if result.RowsAffected() == 0 {
		return nil, ports.ErrNotFound
	}
	// B09: the bucket just left its terminal status (completed/closed), so the
	// kernel work item the sweeper terminalized for it must be reactivated in the
	// SAME transaction -- reconcileTerminalWorkItems only drives work items TOWARD
	// terminal, nothing moves one back on its own. Without this, a reopened bucket
	// stays outside every sweep's claimable predicate and Calendar/Control Tower
	// keep reporting it as finished.
	if _, err := r.ReactivateWorkItemsForBucket(ctx, tx, tenantID, campaignShedID); err != nil {
		return nil, err
	}
	// Withdraw the bucket's lump-sum submission so the reopened bucket can be
	// submitted AGAIN. weighing_shed_observations carries at most one OPEN row per
	// (tenant_id, campaign_shed_id) - enforced by
	// weighing_shed_observations_one_open_scope_uidx, partial on
	// withdrawn_at IS NULL - so leaving the old row open made the rework loop
	// unusable: reopen succeeded and the very next lump-sum resubmit died on a
	// 23505 that surfaced as a 500.
	//
	// SUPERSEDE, never delete. The rejected attempt is immutable history: what was
	// submitted, by whom, against which video is exactly what a rework dispute is
	// adjudicated on. Stamping withdrawn_at frees the one-open-submission slot,
	// drops the row out of the close-gate's pending-verification count (a withdrawn
	// submission is not work the verifier still owes a verdict on) and out of every
	// "the bucket's accepted work" read, while the row, its proof bundle and its
	// weighing.shed_observation_accepted outbox event all survive.
	//
	// Individual buckets own no rows in this table, so this is a no-op for them.
	withdrawn, err := tx.Query(ctx, `
UPDATE weighing_shed_observations
SET withdrawn_at=now()
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND withdrawn_at IS NULL
RETURNING shed_observation_id::text`, tenantID, campaignID, campaignShedID)
	if err != nil {
		return nil, err
	}
	var superseded []string
	for withdrawn.Next() {
		var observationID string
		if err := withdrawn.Scan(&observationID); err != nil {
			withdrawn.Close()
			return nil, err
		}
		superseded = append(superseded, observationID)
	}
	withdrawn.Close()
	if err := withdrawn.Err(); err != nil {
		return nil, err
	}
	// Invalidate the withdrawn submissions' idempotency records IN THIS TRANSACTION.
	// RecordShedObservation short-circuits on that record and replays the stored snapshot
	// without touching a table, so leaving it behind means the operator's offline outbox can
	// replay the pre-reopen key and get HTTP 200 with the OLD observation id: the bucket stays
	// in_progress, no observation is written, no event is emitted, no verification item is
	// raised, and the operator's redone work is silently lost. Withdrawing the row without this
	// is the same hole.
	//
	// One set-based statement, not a DELETE per id: a reopen supersedes every open observation
	// in the scope, so the loop form was one round trip per row.
	if len(superseded) > 0 {
		if _, err := tx.Exec(ctx, `
DELETE FROM weighing_idempotency_records
WHERE tenant_id=$1::uuid
  AND event_type='weighing.shed_observation_accepted'
  AND resource_type='weighing_shed_observation'
  AND resource_id = ANY($2::uuid[])`, tenantID, superseded); err != nil {
			return nil, err
		}
	}
	// F6: restore the PARENT campaign to a capturable status too, not just the
	// bucket. CloseCampaign can leave the campaign itself in 'closed' (cascade
	// close) or 'completed' (every bucket finished on its own). A reopened
	// bucket needs its parent campaign back in a status the capture gate
	// (campaign.status IN ('published','in_progress','delayed'), checked in
	// RecordAnimalObservation/RecordShedObservation and their classify*
	// helpers) accepts, or the bucket flip above is cosmetic: capture stays
	// rejected forever.
	//
	// Only 'closed' and 'completed' are eligible - both are states this same
	// campaign reaches ONLY via completeCampaignIfDone or CloseCampaign, i.e.
	// derived from bucket state, never a deliberate terminal decision distinct
	// from "all buckets are done". 'canceled' is excluded on purpose: that is
	// a genuine terminal outcome (the whole campaign was withdrawn) and must
	// never be resurrected by a single bucket's reopen.
	//
	// Reopening one bucket out of several closed/completed buckets does NOT
	// misrepresent the others: this UPDATE only ever touches the
	// weighing_campaigns row (one row, campaign-grain status), never cascades
	// to sibling weighing_campaign_sheds rows, which keep whatever status they
	// were already in. The campaign simply becomes 'in_progress' again, which
	// is a truthful "this campaign has open work" - correct whether one
	// bucket or all of them are reopened.
	//
	// Re-closing afterwards stays consistent for free: CloseCampaign's own
	// gate re-evaluates the LIVE bucket set (pending verification, terminal
	// status) at close time, and completeCampaignIfDone re-derives 'completed'
	// once every non-canceled bucket is terminal again - neither function
	// trusts anything cached from before this reopen.
	if _, err := tx.Exec(ctx, `
UPDATE weighing_campaigns
SET status='in_progress', completed_at=NULL, closed_at=NULL, closed_by=NULL, close_reason=NULL, closure_kind=NULL, updated_at=now(), row_version=row_version+1
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND status IN ('completed','closed')`, tenantID, campaignID); err != nil {
		return nil, err
	}
	if err := r.enqueueShedReopened(ctx, tx, tenantID, campaignShedID, actorID, reason); err != nil {
		return nil, err
	}
	if err := r.recordIdempotency(ctx, tx, tenantID, "weighing.scope_reopened", idempotencyKey, fingerprint, "weighing_campaign_shed", campaignShedID, map[string]any{
		"campaign_id":      campaignID,
		"campaign_shed_id": campaignShedID,
		"reopened_by":      actorID,
		"reason":           reason,
	}); err != nil {
		return nil, err
	}
	return superseded, tx.Commit(ctx)
}

// withdrawnShedObservationIDs re-reports the submissions a previous run of this
// reopen already superseded. The reopen write itself is idempotency-guarded, but
// the verification withdrawal that follows it happens through another module's
// port AFTER this transaction commits, so a crash in between must be healable: a
// retry with the same key returns the same ids and the withdrawal re-runs as a
// no-op.
func withdrawnShedObservationIDs(ctx context.Context, tx pgx.Tx, tenantID, campaignID, campaignShedID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT shed_observation_id::text
FROM weighing_shed_observations
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid
  AND campaign_shed_id=$3::uuid
  AND withdrawn_at IS NOT NULL`, tenantID, campaignID, campaignShedID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
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

// CampaignParkID reads the park of one campaign by its primary key. It is the routing key a
// weighing verification item must carry (weighing/adapters/verificationbridge), because the
// notification consumer resolves the park's verify-duty holders and cannot route without it.
func (r *Repository) CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var parkID string
	if err := r.pool.QueryRow(ctx, `
SELECT park_id::text
FROM weighing_campaigns
WHERE tenant_id=$1::uuid
  AND campaign_id=$2::uuid`, tenantID, campaignID).Scan(&parkID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ports.ErrNotFound
		}
		return "", err
	}
	return parkID, nil
}

// CampaignShedLocation reads the shed a campaign-shed bucket stands for: its canonical
// location_id and the operational display name the VERIFIER should read ("Godel 1 - Part 3").
//
// It exists because a weighing verification item used to name no shed at all. The lump-sum label
// was the hardcoded literal "Whole shed", and the individual label carried only the scanned tag --
// so a verifier reviewing a queue of clips could not tell which shed, let alone which PARTITION,
// any of them came from. Both facts are already flattened onto weighing_campaign_sheds at
// bucket-creation time, so this reads them in one indexed lookup by primary key.
//
// Isolation (AGENTS.md "Weighing Is ISOLATED"): this touches ONLY weighing's own bucket table. It
// does not join goats, goat_identifiers, or goat_shed_partitions to learn the partition -- the
// partition is parsed back out of the catalog display name by splitShedPartitionName, exactly as
// applyShedPartitionDisplay already does for the campaign-shed read models.
//
// The returned display is composed through oploc.OperationalLocation.Display() so it follows the
// same rendering rule as every other module and can never surface the "whole" sentinel.
func (r *Repository) CampaignShedLocation(ctx context.Context, tenantID, campaignShedID string) (locationID, display string, err error) {
	ctx, cancel := r.timeout(ctx)
	defer cancel()
	var displayName string
	if err := r.pool.QueryRow(ctx, `
SELECT location_id::text, display_name
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid`, tenantID, campaignShedID).Scan(&locationID, &displayName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ports.ErrNotFound
		}
		return "", "", err
	}
	// display_name is used VERBATIM. It is the operational shed name already flattened onto the
	// bucket from the `locations` catalog ("Godel 1 - Part 3", "Castro 2"), which is what the
	// operator picked and what every other module names the same place.
	//
	// Deliberately NOT re-split through SplitShedPartitionName + oploc.Display here. That pass is
	// right for weighing's own campaign-shed read models, but it rewrites "Castro 2" into
	// "Castro - 2" -- and on the verifier's Actions queue a weighing row sits directly beside a
	// vaccination row, which renders the same physical place as "Castro 2" (shed name plus the
	// locations partition column). Splitting here would put two spellings of one shed in one list.
	return locationID, strings.TrimSpace(displayName), nil
}

// RefreshAvailability and completeResolvedIndividualScopes were DELETED
// (free-flow weighing mandate): they read goats.health_status /
// goats.lifecycle_status and wrote clinical/herd state ('icu', 'quarantine',
// 'dead', 'moved_other_shed') into the now-dropped weighing_expected_animals
// table. Weighing has no expected set and never reads herd/clinical state --
// see AGENTS.md, SKILLS.md, and migration 000059. Neither function had a
// production caller; they were dead code reachable only from each other and
// from tests.

func (r *Repository) timeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, r.queryTimeout)
}

// shedScheduleConflicts answers ONE product rule: a shed may hold only one OPEN
// weighing row per (park, weigh date). It returns the display names of the
// requested buckets that some OTHER open task already owns on that date.
//
// It is the friendly half of the guard. The authoritative half is the partial
// unique index uq_weighing_open_shed_per_park_date (migration 000062), which also
// holds under concurrency; this check exists so the planner gets the bucket NAMES
// to render instead of a bare constraint violation. Both run inside the same
// transaction as the write.
//
// GRAIN: one row per open bucket. OPEN means work still owed: 'canceled',
// 'closed' AND 'completed' are all finished history and never block scheduling
// that shed again -- including the same date. That last part is a deliberate
// reversal of migration 000062's original rule (maintainer decision 2026-08-03,
// migration 000089): a shed whose weighing is DONE is free work capacity, and
// re-weighing it is the CEO's call, not something the schema refuses. What is
// still impossible is two people owing the same shed on the same date.
// Served by uq_weighing_open_shed_per_park_date (tenant_id, park_id,
// start_business_date, location_id).
func (r *Repository) shedScheduleConflicts(ctx context.Context, tx pgx.Tx, tenantID, parkID, weighDate, excludeCampaignID string, locationIDs []string) ([]string, error) {
	if len(locationIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
SELECT DISTINCT cs.display_name
FROM weighing_campaign_sheds cs
WHERE cs.tenant_id=$1::uuid
  AND cs.park_id=$2::uuid
  AND cs.start_business_date=$3::date
  AND cs.location_id = ANY($4::uuid[])
  AND cs.status NOT IN ('canceled', 'closed', 'completed')
  AND ($5::uuid IS NULL OR cs.campaign_id <> $5::uuid)
ORDER BY cs.display_name`, tenantID, parkID, weighDate, locationIDs, nullableString(strings.TrimSpace(excludeCampaignID)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// campaignScheduleConflicts is the publish-time re-check: are THIS campaign's own
// open buckets still the only claim on their (park, weigh date, shed) slots?
//
// Publishing is the moment the work becomes an operator's, so it re-asks even
// though creation already checked -- a sibling task could have taken the slot in
// between. Same index, same open definition as shedScheduleConflicts.
func (r *Repository) campaignScheduleConflicts(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) ([]string, error) {
	rows, err := tx.Query(ctx, `
SELECT DISTINCT mine.display_name
FROM weighing_campaign_sheds mine
JOIN weighing_campaign_sheds other
  ON other.tenant_id=mine.tenant_id
 AND other.park_id=mine.park_id
 AND other.start_business_date=mine.start_business_date
 AND other.location_id=mine.location_id
 AND other.campaign_id <> mine.campaign_id
 AND other.status NOT IN ('canceled', 'closed', 'completed')
WHERE mine.tenant_id=$1::uuid
  AND mine.campaign_id=$2::uuid
  AND mine.status NOT IN ('canceled', 'closed', 'completed')
ORDER BY mine.display_name`, tenantID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// shedLabelForConflict reads the weigh date and bucket name used to render a
// scheduling conflict. Error-path only: it best-effort returns empty strings
// rather than masking the original failure with a lookup failure.
//
// It deliberately reads through the POOL, not the caller's transaction: the
// statement that raised the constraint violation has already aborted that
// transaction, so any further query on it fails with 25P02.
//
// scale-guard:ignore: single-row lookup by primary key, on an error path only
func (r *Repository) shedLabelForConflict(ctx context.Context, tenantID, campaignShedID string) (string, string) {
	var weighDate, displayName string
	if err := r.pool.QueryRow(ctx, `
SELECT start_business_date::text, display_name
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, tenantID, campaignShedID).Scan(&weighDate, &displayName); err != nil {
		return "", ""
	}
	return weighDate, displayName
}

// shedScheduleConflictError builds the typed 409 the client renders.
func shedScheduleConflictError(weighDate string, sheds []string) error {
	return &ports.ShedScheduleConflict{WeighDate: weighDate, Sheds: sheds}
}

// mapShedUniqueViolation converts a race that slipped past the pre-check into the
// same typed conflict, so a concurrent double-book reads identically to a detected
// one instead of surfacing as an opaque 500.
//
// Two shed-grain unique indexes land here:
//
//   - uq_weighing_open_shed_per_park_date - scheduling: this shed is already
//     somebody's work on that date. weighDate/displayName name the blocked bucket
//     for the planner's inline 409 and only apply to this case.
//   - weighing_shed_observations_one_open_scope_uidx (pre-000067:
//     weighing_shed_observations_one_active_scope_uidx) - submission: this bucket
//     already holds its one OPEN lump-sum submission. The write path serializes this
//     through the bucket status gate ('pending'/'in_progress' only), so reaching
//     it means a concurrent submitter won the race; the loser must read as a
//     clean 409 invalid_state, never a 500.
func mapShedUniqueViolation(err error, weighDate, displayName string) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return err
	}
	switch pgErr.ConstraintName {
	// Both index generations are matched: the pre-000089 name still exists on a
	// database that has not taken that migration yet, and a rolled-back one goes
	// back to it. Dropping either name would turn the race into a 500 exactly
	// when the schema is mid-migration.
	case "uq_weighing_open_shed_per_park_date_v2",
		"uq_weighing_open_shed_per_park_date":
		return shedScheduleConflictError(weighDate, []string{displayName})
	case "weighing_shed_observations_one_active_scope_uidx",
		"weighing_shed_observations_one_open_scope_uidx":
		return ports.ErrImmutable
	}
	return err
}

// mapObservationUniqueViolation converts the two shapes a losing concurrent
// per-animal capture can take into TWO DIFFERENT typed errors -- they are
// different facts and must not collapse into one caller-visible outcome --
// instead of letting a raw Postgres error escape as an opaque 500:
//
//   - 23505 on weighing_observations_one_open_tag_uidx (tenant_id,
//     campaign_shed_id, lower(btrim(scanned_identifier))) WHERE submitted_at
//     IS NULL means exactly what it says: this SAME tag already has an open
//     capture in this bucket (insert-vs-insert, both transactions' "updated"
//     CTE saw no existing row before either committed). This genuinely is a
//     duplicate -- mapped to ports.ErrDuplicateScan, the same typed conflict a
//     same-bucket submitted duplicate already returns via
//     classifyFreeFlowObservationRejection. The caller must NOT retry: retrying
//     would just collide with the same row again.
//   - 40001 serialization_failure says NOTHING about duplication -- it means
//     "this transaction's read was invalidated by a concurrent commit", full
//     stop. RecordAnimalObservation runs at SERIALIZABLE, and the
//     "assigned_shed" CTE's FOR NO KEY UPDATE bucket lock (F8) is taken on the
//     SAME weighing_campaign_sheds row by every capture in that bucket --
//     including two DIFFERENT animals captured at overlapping instants, the
//     ordinary field case, not a race. Mapping 40001 to ErrDuplicateScan would
//     tell that operator they double-scanned an animal they scanned once and
//     DISCARD a real capture with a real video -- worse than the
//     update-vs-update bug this fix exists to close. Mapped instead to
//     ports.ErrWriteConflict, which RecordAnimalObservation retries internally
//     (see the retry loop there) rather than surfacing to the caller at all,
//     except after every retry has also lost.
//
// Any other error (including a different constraint or SQLSTATE) passes
// through unchanged.
func mapObservationUniqueViolation(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == "23505" && pgErr.ConstraintName == "weighing_observations_one_open_tag_uidx":
		return ports.ErrDuplicateScan
	case pgErr.Code == "40001":
		return ports.ErrWriteConflict
	}
	return err
}

func createCampaignLocationIDs(sheds []domain.CreateCampaignShed) []string {
	ids := make([]string, 0, len(sheds))
	for _, shed := range sheds {
		ids = append(ids, shed.LocationID)
	}
	return ids
}

func weighingShedOperatorID(defaultOperatorID string, shed domain.CreateCampaignShed) string {
	operatorID := strings.TrimSpace(shed.OperatorUserID)
	if operatorID != "" {
		return operatorID
	}
	return strings.TrimSpace(defaultOperatorID)
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
	err := tx.QueryRow(ctx, `SELECT campaign_id::text, tenant_id::text, park_id::text, period_start_date::text, period_end_date::text, start_business_date::text, status, planned_cap_per_day, operator_user_id::text, created_by::text, created_at, updated_at, row_version, COALESCE(close_reason, ''), COALESCE(closure_kind, '') FROM weighing_campaigns WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, tenantID, campaignID).
		Scan(&c.CampaignID, &c.TenantID, &c.ParkID, &c.PeriodStartDate, &c.PeriodEndDate, &c.StartBusinessDate, &c.Status, &c.PlannedCapPerDay, &c.OperatorUserID, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.RowVersion, &c.CloseReason, &c.ClosureKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ports.ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, err
	}
	// projection-review: membership=campaign shed buckets for one weighing campaign; group_key=(tenant_id,campaign_id,campaign_shed_id); join_cardinality=each campaign_shed row is hydrated once and observation rollups stay keyed by campaign_shed_id; pagination=single campaign detail read without page slicing; scope=exact campaign_id and tenant_id detail scope.
	// B21-SQL: same LEFT JOIN workforce_members ListCampaignSheds
	// (campaign_sheds_page.go) already uses to hydrate OperatorDisplayName --
	// reused here rather than reinvented so both surfaces resolve an
	// operator's display name identically (status='active' only, same join
	// keys). Before this join neither this query nor hydrateCampaigns below
	// selected the name at all, so admin-web always fell back to its
	// "Roster gap (operator not found)" placeholder for every assigned shed.
	rows, err := tx.Query(ctx, `
SELECT cs.campaign_shed_id::text, cs.campaign_id::text, cs.location_id::text, cs.location_type, cs.display_name, cs.expected_animal_count, cs.weighing_category, cs.operator_user_id::text, COALESCE(op.display_name, ''), cs.status,
  `+readyToCloseCountsSQL+`
, cs.partition_label
FROM weighing_campaign_sheds cs
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE cs.tenant_id=$1::uuid AND cs.campaign_id=$2::uuid
ORDER BY cs.display_name`, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var shed domain.CampaignShed
		var submitted int
		var storedPartition *string
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName, &shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.OperatorUserID, &shed.OperatorDisplayName, &shed.Status, &shed.ClosureKind, &submitted, &shed.PendingVerificationCount, &shed.ReworkCount, &shed.VerifiedCount, &shed.AnimalsWeighedCount, &shed.AnimalsSubmittedCount, &storedPartition); err != nil {
			return domain.Campaign{}, err
		}
		shed.ReadyToClose = shed.Status == domain.StatusCompleted && submitted > 0 && shed.PendingVerificationCount == 0
		if storedPartition != nil {
			shed.PartitionLabel = *storedPartition
		}
		applyShedPartitionDisplay(&shed)
		c.Sheds = append(c.Sheds, shed)
	}
	completedAnimals, completedScopes, wrongShed, missing, err := r.progressStats(ctx, tx, tenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	c.Progress = progress(c.Sheds, completedAnimals, completedScopes, wrongShed, missing)
	return c, rows.Err()
}

func (r *Repository) hydrateCampaigns(ctx context.Context, tenantID string, ids []string, campaigns []domain.Campaign, operatorUserID string) error {
	if len(ids) == 0 {
		return nil
	}
	byID := make(map[string]int, len(campaigns))
	for i := range campaigns {
		byID[campaigns[i].CampaignID] = i
	}
	operatorFilter := strings.TrimSpace(operatorUserID)
	// B21-SQL: see getCampaignTx's identical join above -- reusing
	// ListCampaignSheds' join, not inventing a second way to resolve an
	// operator's display name.
	rows, err := r.pool.Query(ctx, `
SELECT cs.campaign_shed_id::text, cs.campaign_id::text, cs.location_id::text, cs.location_type, cs.display_name, cs.expected_animal_count, cs.weighing_category, cs.operator_user_id::text, COALESCE(op.display_name, ''), cs.status,
  `+readyToCloseCountsSQL+`
, cs.partition_label
FROM weighing_campaign_sheds cs
LEFT JOIN workforce_members op
  ON op.tenant_id=cs.tenant_id AND op.user_id=cs.operator_user_id AND op.status='active'
WHERE cs.tenant_id=$1::uuid
  AND cs.campaign_id = ANY($2::uuid[])
  AND ($3::uuid IS NULL OR cs.operator_user_id=$3::uuid)
ORDER BY cs.campaign_id, cs.display_name`, tenantID, ids, nullableString(operatorFilter))
	if err != nil {
		return err
	}
	for rows.Next() {
		var shed domain.CampaignShed
		var submitted int
		var storedPartition *string
		if err := rows.Scan(&shed.CampaignShedID, &shed.CampaignID, &shed.LocationID, &shed.LocationType, &shed.DisplayName, &shed.ExpectedAnimalCount, &shed.WeighingCategory, &shed.OperatorUserID, &shed.OperatorDisplayName, &shed.Status, &shed.ClosureKind, &submitted, &shed.PendingVerificationCount, &shed.ReworkCount, &shed.VerifiedCount, &shed.AnimalsWeighedCount, &shed.AnimalsSubmittedCount, &storedPartition); err != nil {
			rows.Close()
			return err
		}
		shed.ReadyToClose = shed.Status == domain.StatusCompleted && submitted > 0 && shed.PendingVerificationCount == 0
		if storedPartition != nil {
			shed.PartitionLabel = *storedPartition
		}
		applyShedPartitionDisplay(&shed)
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
	// FREE-FLOW: there is no expected roster, so there is no "wrong shed" or
	// "missing animal" fact to compute -- both banned by the free-flow mandate
	// (AGENTS.md, SKILLS.md, migration 000059). wrong_shed/missing stay at their
	// zero value in `stats` (never populated from a herd/expected-animal query).

	// projection-review: membership=weighing_observations rows captured against the operator-visible individual_animal buckets of the listed campaigns; group_key=campaign_id, measure=count(DISTINCT lower(btrim(scanned_identifier))); join_cardinality=weighing_campaign_sheds cs is exactly 1 row per campaign_shed_id (primary key) restricted to individual_animal buckets, so the join adds no rows and the count stays scan-grain by construction; pagination=one bounded set-based aggregate over the campaign ids of the current page; scope=tenant_id=$1 plus the same optional operator bucket predicate $3 used for the bucket rows above.
	//
	// This is the SAME canonical source (weighing_observations.scanned_identifier -- the table has no animal_id column, dropped by 000078_weighing_observations_drop_animal_id.sql) the bucket/roster screens already read -- fixes the cross-surface count-parity defect where campaign "captured" totals read a column (weighing_expected_animals.status='weighed') the write path never sets.
	//
	// Grain proof (a) producer unique columns vs consumer match/group columns: producer weighing_observations has no per-tag uniqueness -- a rescan before submission updates the SAME row in place (recordUnknownAnimalObservationTx), so re-capturing a tag does not inflate the count; consumer group=campaign_id, measure=count(DISTINCT normalized identifier).
	// Grain proof (b) row multiplicity: cs is 1:1 with campaign_shed_id; restricted to individual_animal buckets, matching IndividualExpectedCount's own bucket set.
	// Grain proof (c) ratio key sets: numerator and IndividualExpectedCount (summed in `progress()` from the same individual_animal buckets) both range over (tenant_id, campaign_id, campaign_shed_id IN individual_animal buckets of this campaign, operator-visible set).
	rows, err = r.pool.Query(ctx, `
SELECT o.campaign_id::text,
  count(DISTINCT lower(btrim(o.scanned_identifier)))::int AS completed_animals
FROM weighing_observations o
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=o.tenant_id
 AND cs.campaign_id=o.campaign_id
 AND cs.campaign_shed_id=o.campaign_shed_id
 AND cs.weighing_category='individual_animal'
WHERE o.tenant_id=$1::uuid
  AND o.campaign_id = ANY($2::uuid[])
  AND ($3::uuid IS NULL OR cs.operator_user_id=$3::uuid)
GROUP BY o.campaign_id`, tenantID, ids, nullableString(operatorFilter))
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var completedAnimals int
		if err := rows.Scan(&id, &completedAnimals); err != nil {
			rows.Close()
			return err
		}
		row := stats[id]
		row.completedAnimals = completedAnimals
		stats[id] = row
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	// projection-review: membership=per_shed_partition buckets of the listed campaigns that carry at least one shed observation, restricted to the operator-visible bucket set; group_key=campaign_id with count(DISTINCT campaign_shed_id) as the completed-bucket measure; join_cardinality=weighing_shed_observations is 0..1 OPEN row per bucket (weighing_shed_observations_one_open_scope_uidx is unique on (tenant_id, campaign_shed_id) WHERE withdrawn_at IS NULL, and this read filters withdrawn_at IS NULL, so a reopened bucket drops back to 0 while its superseded submission stays on the table as history) and weighing_campaign_sheds cs is exactly 1 row per campaign_shed_id (primary key), so the count is bucket-grain by construction; the DISTINCT is defence in depth, not a collapse the data actually needs; pagination=one bounded set-based aggregate over the campaign ids of the current page; scope=tenant_id=$1 plus the same optional operator bucket predicate $3 used for the bucket rows and the expected-animal rollup.
	//
	// Grain proof (a) producer unique columns vs consumer match/group columns:
	//   producer weighing_campaign_sheds unique: (campaign_shed_id) PK; tenant/campaign-scoped (tenant_id, campaign_id, campaign_shed_id)
	//   consumer rollup                  match:  (cs.tenant_id, cs.campaign_id, cs.campaign_shed_id) = the same three columns on wso
	//   consumer group:                          (wso.campaign_id), measure count(DISTINCT wso.campaign_shed_id)
	// Grain proof (b) row multiplicity: cs is 1:1 with the joined campaign_shed_id (primary key), wso is 0..1 per bucket (unique on
	//   (tenant_id, campaign_shed_id)), so a bucket contributes at most one row; the DISTINCT campaign_shed_id keeps the measure
	//   bucket-grain even if that uniqueness is ever relaxed.
	// Grain proof (c) ratio key sets: numerator (completed per-shed buckets) and denominator (PerScopeExpectedCount, counted from
	//   the operator-visible bucket rows above) both range over (tenant_id, campaign_id, campaign_shed_id IN the operator-visible
	//   bucket set) - identical on both sides.
	rows, err = r.pool.Query(ctx, `
SELECT wso.campaign_id::text, count(DISTINCT wso.campaign_shed_id)::int
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=wso.tenant_id
 AND cs.campaign_id=wso.campaign_id
 AND cs.campaign_shed_id=wso.campaign_shed_id
 AND cs.weighing_category='per_shed_partition'
WHERE wso.tenant_id=$1::uuid
  AND wso.campaign_id = ANY($2::uuid[])
  AND wso.withdrawn_at IS NULL
  AND ($3::uuid IS NULL OR cs.operator_user_id=$3::uuid)
GROUP BY wso.campaign_id`, tenantID, ids, nullableString(operatorFilter))
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

// projection-review: membership=weighing_observations rows captured against the campaign's individual_animal buckets (free-flow write path: no animal_id column exists at all; scanned_identifier carries the raw RFID); group_key=campaign_id, measure=count(DISTINCT lower(btrim(scanned_identifier))); join_cardinality=weighing_campaign_sheds cs is exactly 1 row per campaign_shed_id (primary key) restricted to individual_animal buckets, so the join adds no rows and the count stays scan-grain by construction; pagination=one bounded aggregate per campaign (not paginated, whole-filter); scope=tenant_id=$1, campaign_id=$2.
//
// This REPLACES the old `weighing_expected_animals.status='weighed'` read: nothing in the free-flow write path ever sets that column, so it always read zero while this bucket's own captured rows were nonzero -- the cross-surface count-parity defect AGENTS.md names (the 200-vs-400 incident). The canonical source for "how many animals were captured" is the same weighing_observations table the roster/bucket screens already read; this is that same grain, not a new estimate.
//
// Grain proof (a) producer unique columns vs consumer match/group columns: producer weighing_observations has no uniqueness constraint on scanned_identifier, so a rescan (edit before submission) updates the SAME row in place (recordUnknownAnimalObservationTx) and is not duplicated per edit; consumer group=campaign_id, measure=count(DISTINCT normalized identifier), so two rows for the same tag (extra_scan mismatch path) still count the animal once.
// Grain proof (b) row multiplicity: cs is 1:1 with campaign_shed_id (primary key); the predicate restricts to individual_animal buckets only, matching the IndividualCompletedCount denominator built from those same buckets' ExpectedAnimalCount.
// Grain proof (c) ratio key sets: numerator (captured animals) and IndividualExpectedCount (summed from the same individual_animal buckets in `progress()`) both range over (tenant_id, campaign_id, campaign_shed_id IN individual_animal buckets of this campaign).
func (r *Repository) progressStats(ctx context.Context, tx pgx.Tx, tenantID, campaignID string) (completedAnimals int, completedScopes int, wrongShed int, missing int, err error) {
	err = tx.QueryRow(ctx, `
SELECT count(DISTINCT lower(btrim(o.scanned_identifier)))::int
FROM weighing_observations o
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=o.tenant_id
 AND cs.campaign_id=o.campaign_id
 AND cs.campaign_shed_id=o.campaign_shed_id
 AND cs.weighing_category='individual_animal'
WHERE o.tenant_id=$1::uuid AND o.campaign_id=$2::uuid`, tenantID, campaignID).
		Scan(&completedAnimals)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	// FREE-FLOW: no expected roster, so no "wrong shed" / "missing animal" fact
	// exists to compute. Both stay 0 (see AGENTS.md, SKILLS.md, migration 000059).
	err = tx.QueryRow(ctx, `
SELECT count(DISTINCT wso.campaign_shed_id)::int
FROM weighing_shed_observations wso
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=wso.tenant_id
 AND cs.campaign_id=wso.campaign_id
 AND cs.campaign_shed_id=wso.campaign_shed_id
 AND cs.weighing_category='per_shed_partition'
WHERE wso.tenant_id=$1::uuid AND wso.campaign_id=$2::uuid
  AND wso.withdrawn_at IS NULL`, tenantID, campaignID).
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
		queryErr = tx.QueryRow(ctx, `SELECT observation_id::text, campaign_id::text, COALESCE(campaign_shed_id::text,''), scanned_identifier, weight_kg::float8, proof_artifact_id::text, COALESCE(expected_location_id::text,''), COALESCE(actual_location_id::text,''), COALESCE(actual_location_label,''), accepted_at FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, tenantID, id).
			Scan(&obs.ObservationID, &obs.CampaignID, &obs.CampaignShedID, &obs.ScannedIdentifier, &obs.WeightKg, &obs.ProofArtifactID, &obs.ExpectedLocationID, &obs.ActualLocationID, &obs.ActualLocationLabel, &obs.AcceptedAt)
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
  AND lower(btrim(scanned_identifier))=lower(btrim($4))
ORDER BY accepted_at DESC, observation_id
LIMIT 1`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, tag).
		Scan(&before.ObservationID, &before.CampaignID, &before.CampaignShedID, &before.ScannedIdentifier, &before.WeightKg, &before.ProofArtifactID, &before.ExpectedLocationID, &before.ActualLocationID, &before.ActualLocationLabel, &before.AcceptedAt)
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

func (r *Repository) classifyShedObservationRejection(ctx context.Context, tx pgx.Tx, cmd domain.RecordShedObservation) error {
	// ReopenScope drops the withdrawn submission's idempotency record so a replay
	// cannot short-circuit into a stale 200, but the WITHDRAWN ROW keeps its
	// idempotency_key (weighing_shed_observations_idempotency_uidx is table-wide),
	// so the replay's INSERT is swallowed by ON CONFLICT DO NOTHING and lands here.
	// Say so plainly - a superseded key is a 409 invalid_state, not a 404 - so an
	// offline outbox replay of the pre-reopen key can never be mistaken for either
	// a success or a vanished bucket.
	var superseded bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_shed_observations
  WHERE tenant_id=$1::uuid
    AND idempotency_key=$2
    AND withdrawn_at IS NOT NULL
)`, cmd.TenantID, cmd.IdempotencyKey).Scan(&superseded); err != nil {
		return err
	}
	if superseded {
		return ports.ErrImmutable
	}
	var status, operatorID, shedStatus string
	err := tx.QueryRow(ctx, `
SELECT campaign.status, cs.operator_user_id::text, cs.status
FROM weighing_campaigns campaign
JOIN weighing_campaign_sheds cs
  ON cs.tenant_id=campaign.tenant_id
 AND cs.campaign_id=campaign.campaign_id
 AND cs.campaign_shed_id=$3::uuid
WHERE campaign.tenant_id=$1::uuid
  AND campaign.campaign_id=$2::uuid`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID).
		Scan(&status, &operatorID, &shedStatus)
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
	// Bucket-level terminal-state gate. "completed" means the operator already
	// submitted (there is no separate "submitted" enum value); a lump-sum write
	// must not resurrect a completed/closed/canceled bucket.
	if shedStatus != "pending" && shedStatus != domain.StatusInProgress {
		return ports.ErrImmutable
	}
	var category string
	var proofOK bool
	// proofsAreThisShedsVideos repeats the accept-side proof predicate WITHOUT the
	// upload_state clause. It is what separates "your video is still uploading"
	// (a real state an operator can wait out, ErrProofNotReady) from "these are not
	// this shed's videos at all" -- a photo, another shed's proof, or more than the
	// five the bundle allows -- which stays a plain invalid argument. Collapsing
	// the two would tell an operator who attached a PHOTO to wait for an upload
	// that is already finished.
	var proofsAreThisShedsVideos bool
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
	  ) AS proof_ok,
	  (
	    SELECT count(*)=cardinality($4::uuid[])
	      AND count(*) BETWEEN 1 AND 5
	    FROM unnest($4::uuid[]) AS requested(proof_id)
	    JOIN proof_artifacts proof
	      ON proof.tenant_id=$1::uuid
	     AND proof.proof_id=requested.proof_id
	     AND proof.proof_type='video'
	     AND proof.scope_type='shed'
	     AND proof.scope_id=cs.location_id
	     AND proof.subject_type='shed'
	     AND proof.subject_id=cs.location_id
	  ) AS proofs_are_this_sheds_videos
	FROM weighing_campaign_sheds cs
	WHERE cs.tenant_id=$1::uuid
	  AND cs.campaign_id=$2::uuid
	  AND cs.campaign_shed_id=$3::uuid`, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.ProofArtifactIDs).
		Scan(&category, &proofOK, &proofsAreThisShedsVideos)
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
		if proofsAreThisShedsVideos {
			// The lump-sum pair is (total weight + animal count) + at least one
			// FINISHED video for THIS shed. Weight and count are already rejected
			// upstream with a 400; the right video still uploading is the remaining
			// half, and it used to render as "request is invalid" -- true of a
			// malformed request, useless to an operator who only has to wait.
			return ports.ErrProofNotReady
		}
		// Not this shed's videos (wrong type, wrong shed, wrong count). Genuinely a
		// bad request, and it stays one.
		return ports.ErrInvalidArgument
	}
	// Check if any proof in the bundle is attached to a verifier-rejected (rework)
	// observation for this campaign_shed. If so, the operator must record a new video
	// instead of re-using the rejected proof.
	var rejectedProofReused bool
	if err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1 FROM weighing_shed_observations rejected_obs
  JOIN weighing_shed_observation_proofs rejected_proofs
    ON rejected_proofs.shed_observation_id=rejected_obs.shed_observation_id
  WHERE rejected_obs.tenant_id=$1::uuid
    AND rejected_obs.campaign_shed_id=$2::uuid
    AND rejected_obs.verification_status='rework'
    AND rejected_proofs.proof_artifact_id=ANY($3::uuid[])
  LIMIT 1
)`, cmd.TenantID, cmd.CampaignShedID, cmd.ProofArtifactIDs).Scan(&rejectedProofReused); err != nil {
		return err
	}
	if rejectedProofReused {
		return ports.ErrRejectedProofReuse
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
		"trace_id":         boundedTraceID(idem),
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
VALUES ($1::uuid, $2::uuid, $3, '1.0.0', 'weighing', $4::uuid, 'domain-events', $5::jsonb, $6::jsonb, $7, $8, 'pending', now())
ON CONFLICT DO NOTHING`, tenantID, eventID, eventType, aggregateID, string(raw), string(headers), idem, boundedTraceID(idem))
	return err
}

// maxEnvelopeTraceIDLen mirrors trace_id's maxLength in
// contracts/jsonschema/domain-event-envelope.schema.json. The envelope allows an
// idempotency_key of up to 320 characters but a trace_id of only 200, and this
// producer used the idempotency key verbatim as the trace id -- so any key longer
// than 200 produced an envelope that could never validate.
const maxEnvelopeTraceIDLen = 200

// boundedTraceID keeps trace_id inside the envelope contract.
//
// This is the ACTUAL cause of the 12 undeliverable events in the first relay run
// against real device data -- not the missing actual_location_id, which is a real
// but separate data-loss bug on the edit path (the envelope schema does not
// constrain payload contents at all, so a missing payload field cannot fail
// validation). The two were perfectly correlated because the client's second,
// `:proof:`-suffixed post was BOTH the one that took the edit path AND the one
// whose key was long enough (226 chars) to overflow trace_id; the 15 that
// published had 183-char keys. Two campaign_created events failed the same way at
// 249 and 431 characters.
//
// The relay marks a failed envelope 'failed' on attempt 1 and never retries it, so
// this is silent, permanent event loss. trace_id is a correlation id, not an
// identity, so shortening it loses nothing -- but a plain truncation would make two
// distinct long keys collide, so the overflow keeps a readable prefix plus a
// deterministic digest of the WHOLE key.
func boundedTraceID(idem string) string {
	if len(idem) <= maxEnvelopeTraceIDLen {
		return idem
	}
	sum := sha256.Sum256([]byte(idem))
	digest := hex.EncodeToString(sum[:])[:32]
	return idem[:maxEnvelopeTraceIDLen-1-len(digest)] + ":" + digest
}

func weighingSubjectType(eventType string) string {
	switch eventType {
	case "weighing.campaign_created", "weighing.campaign_updated", "weighing.campaign_published":
		return "weighing_campaign"
	case "weighing.shed_submission.completed", "weighing.shed.reopened", eventTypeScopeClosed,
		eventTypeScopeVerifiedClosed:
		return "weighing_campaign_shed"
	case eventTypeObservationReworkDigest:
		// A digest names several observations at once, so it is not observation-grained. Its
		// outbox aggregate is the campaign (see enqueue in rework_digest.go); the bucket it
		// is about is carried in the payload.
		return "weighing_campaign"
	case domain.EventWorkItemDayStart, domain.EventWorkItemRolledForward, domain.EventWorkItemDelayed:
		// Cadence events are campaign-aggregated (one per campaign+operator) and
		// their outbox aggregate_id is the campaign id.
		return "weighing_campaign"
	case eventTypeCampaignClosed, eventTypeCampaignVerifiedClosed:
		return "weighing_campaign"
	default:
		return "weighing_observation"
	}
}

func progress(sheds []domain.CampaignShed, completedAnimals, completedScopes, wrongShed, missing int) domain.Progress {
	p := domain.Progress{}
	for _, shed := range sheds {
		// FREE-FLOW: an individual_animal bucket has NO expected animal total. It
		// is a place to weigh whatever walks through, so IndividualExpectedCount
		// stays 0 ("no expectation") and is never summed from
		// expected_animal_count, which the write path stores as 0 and which no
		// longer carries any signal. A per_shed_partition bucket is different: the
		// unit there is the BUCKET itself (one lump-sum weight per bucket), so
		// counting buckets is a real, non-invented expectation.
		if shed.WeighingCategory == domain.CategoryPerShedPartition {
			p.PerScopeExpectedCount++
		}
	}
	p.IndividualCompletedCount = completedAnimals
	p.PerScopeCompletedCount = completedScopes
	p.WrongShedCount = wrongShed
	p.MissingCount = missing
	// "Still open" can only be counted where an expectation actually exists: the
	// lump-sum buckets. The individual side used to be folded in as
	// (IndividualExpectedCount - IndividualCompletedCount), which with a real
	// expectation of 0 turns every animal an operator weighs into a NEGATIVE
	// remainder that silently eats the lump-sum figure next to it.
	p.RemainingCount = p.PerScopeExpectedCount - p.PerScopeCompletedCount
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

type observationsCursor struct {
	AcceptedAt    time.Time `json:"accepted_at"`
	ObservationID string    `json:"observation_id"`
}

func encodeObservationsCursor(cursor observationsCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeObservationsCursor(value string) (observationsCursor, error) {
	if strings.TrimSpace(value) == "" {
		return observationsCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return observationsCursor{}, err
	}
	var cursor observationsCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return observationsCursor{}, err
	}
	if cursor.AcceptedAt.IsZero() || cursor.ObservationID == "" {
		return observationsCursor{}, ports.ErrInvalidArgument
	}
	return cursor, nil
}

// plannerBucketCursor is the keyset over the sheds of ONE park, ordered by
// (display_order, name, location_id). It is a WITHIN-PARK cursor: the park is a
// separate request argument, so a page can never wander into another park the
// way the old flattened park+shed cursor did.
//
// Set distinguishes "no cursor, start at the beginning" from a real cursor whose
// first component happens to be 0 -- display_order defaults to 0, so a
// nil-if-zero encoding would silently restart the scan on the second page.
type plannerBucketCursor struct {
	Set       bool   `json:"-"`
	ShedOrder int    `json:"shed_order"`
	ShedName  string `json:"shed_name"`
	ShedID    string `json:"shed_id"`
}

func (c plannerBucketCursor) orderArg() any {
	if !c.Set {
		return nil
	}
	return c.ShedOrder
}

func (c plannerBucketCursor) nameArg() any {
	if !c.Set {
		return nil
	}
	return c.ShedName
}

func (c plannerBucketCursor) idArg() any {
	if !c.Set {
		return nil
	}
	return c.ShedID
}

func encodePlannerBucketCursor(cursor plannerBucketCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePlannerBucketCursor(value string) (plannerBucketCursor, error) {
	if strings.TrimSpace(value) == "" {
		return plannerBucketCursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return plannerBucketCursor{}, err
	}
	var cursor plannerBucketCursor
	if err := json.Unmarshal(raw, &cursor); err != nil {
		return plannerBucketCursor{}, err
	}
	if cursor.ShedID == "" {
		return plannerBucketCursor{}, ports.ErrInvalidArgument
	}
	cursor.Set = true
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
