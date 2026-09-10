// Package boardsource is Weighing's contribution to the cross-module Work Board: one
// board row per weighing work item (one per campaign bucket), read from
// weighing_work_items and weighing_campaign_sheds plus the org tables every module may
// read. It lives INSIDE the weighing package so the isolation rule holds in both
// directions: weighing reads only its own tables here, and the board never reads a
// weighing table at all.
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a scan, a submit, a close or a
// verdict. The mapping from a work item's state to a board work state is the ONLY
// business meaning this file adds, and it is stated once in workStateSQL.
package boardsource

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every weighing board row.
const SourceType = "weighing_work_item"

// carryBandDays is the accepted carry-forward for weighing (the D+2 band in
// docs/decisions/task-timing-alerting-violations-and-appeals.md). A delayed bucket inside
// the band is a progress signal (due, severity watch); past it the row reads overdue.
const carryBandDays = 2

// Source implements ports.Source over weighing's own kernel rows.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

func (s *Source) Module() domain.Module { return domain.ModuleWeighing }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a weighing work item becomes a board work state.
//
//	closed (item OR bucket)      -> completed            (the close landed; the bucket is done)
//	completed                    -> verification_pending (submitted; the close gate waits on the verdict)
//
// The bucket's own status is read alongside the item's because CLOSE writes only the bucket
// (weighing_campaign_sheds.status = 'closed'); the work item is moved to 'closed' by the
// kernel sweep, which reconciles only scheduled/delayed items, so a submitted bucket's item
// stays 'completed' after the close. Reading the item alone left every closed-after-submit
// bucket in "In review" forever (119 of 134 closed buckets on the 2026-09-10 clone).
//
//	delayed, past the D+2 band   -> overdue
//	otherwise, bucket started    -> in_progress
//	otherwise                    -> due                  (scheduled, or delayed inside the band)
//
// `canceled` rows are excluded in the WHERE clause: a canceled bucket is not work.
const workStateSQL = `CASE
  WHEN w.work_state = 'closed' OR b.status = 'closed' THEN 'completed'
  WHEN w.work_state = 'completed' THEN 'verification_pending'
  WHEN w.work_state = 'delayed' AND (w.due_business_date - w.planned_business_date) > ` + "2" + ` THEN 'overdue'
  WHEN b.status = 'in_progress' THEN 'in_progress'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one park and one business date, which is
// what keeps the scan on weighing_work_items_sweep_due_idx / the park+date index rather
// than the whole table. The business date is the CURRENT due date: a rolled-forward
// bucket appears on the day it is now due, and its clock label names the original plan.
const baseWhere = `
  w.tenant_id = $1::uuid
  AND w.park_id = $2::uuid
  AND w.due_business_date = $3::date
  AND w.work_state <> 'canceled'
  AND ($4::uuid IS NULL OR w.operator_user_id = $4::uuid)`

// projection-review: membership=weighing_work_items rows of ONE tenant, park and due business date (canceled excluded), one row per campaign bucket (weighing_work_items_bucket_uidx); group_key=(tenant_id, work_item_id) for the list and the derived board_state for the count; join_cardinality=weighing_campaign_sheds joined on its primary key (1:1), locations park/shed on their primary key (1:1), workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), and the observed count is a CORRELATED SUBQUERY over weighing_observations per returned row, so nothing fans a bucket out; pagination=keyset on work_item_id ASC after $5, LIMIT $7, with the state filter inside WHERE so a page is never short after the cut; scope=tenant_id, park_id, due_business_date and the optional owner predicate, repeated verbatim in countSQL so rows and counts describe one set.
const listSQL = `
WITH items AS (
  SELECT w.work_item_id, w.campaign_id, w.campaign_shed_id, w.park_id, w.operator_user_id,
         w.weighing_category, w.shed_label, w.shed_location_id,
         w.planned_business_date, w.due_business_date, w.work_state, w.rolled_forward_count,
         b.status AS bucket_status, COALESCE(b.partition_label, '') AS partition_label,
         b.expected_animal_count,
         ` + workStateSQL + ` AS board_state
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR w.work_item_id > $5::uuid)
)
SELECT i.work_item_id::text, i.campaign_id::text, i.campaign_shed_id::text, i.park_id::text,
       COALESCE(park.name, ''),
       i.shed_location_id::text, COALESCE(NULLIF(shed.name, ''), i.shed_label), i.partition_label,
       i.weighing_category, i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.bucket_status, i.board_state, i.expected_animal_count,
       COALESCE(i.operator_user_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, ''),
       (SELECT count(*) FROM weighing_observations o
         WHERE o.tenant_id = $1::uuid AND o.campaign_shed_id = i.campaign_shed_id) AS observed
FROM items i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_location_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = i.operator_user_id AND m.status = 'active'
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.work_item_id
LIMIT $7`

// projection-review: membership=weighing_work_items rows of ONE tenant, park and due business date (canceled excluded), one row per campaign bucket (weighing_work_items_bucket_uidx); group_key=(tenant_id, work_item_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=weighing_campaign_sheds joined on its primary key (1:1), locations park/shed on their primary key (1:1), workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), and the observed count is a CORRELATED SUBQUERY over weighing_observations per returned row, so nothing fans a bucket out; pagination=keyset on work_item_id ASC after $5, LIMIT $7, with the state filter inside WHERE so a page is never short after the cut; scope=tenant_id, park_id, due_business_date and the optional owner predicate, repeated verbatim in countSQL so rows and counts describe one set.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE ` + baseWhere + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`

// ListRows implements ports.Source.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	rows, err := s.pool.Query(ctx, listSQL,
		q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("weighing boardsource list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("weighing boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, countSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("weighing boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("weighing boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

func scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		workItemID, campaignID, bucketID, parkID, parkName  string
		shedID, shedName, partitionLabel, category          string
		planned, due, kernelState, bucketStatus, boardState string
		expected, observed                                  int
		ownerUserID, ownerMemberID, ownerName               string
	)
	if err := rows.Scan(&workItemID, &campaignID, &bucketID, &parkID, &parkName,
		&shedID, &shedName, &partitionLabel, &category, &planned, &due,
		&kernelState, &bucketStatus, &boardState, &expected,
		&ownerUserID, &ownerMemberID, &ownerName, &observed); err != nil {
		return domain.Row{}, fmt.Errorf("weighing boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	clock := "Planned " + biztime.FarmDateFromBusinessDate(planned)
	if kernelState == "delayed" {
		clock = "Delayed · planned " + biztime.FarmDateFromBusinessDate(planned)
		severity = domain.SeverityWatch
		if state == domain.WorkStateOverdue {
			severity = domain.SeverityAtRisk
		}
	}
	title := "Weigh " + pen.Display
	subtitle := "Whole pen"
	counts := domain.Counts{}
	switch category {
	case "individual_animal":
		subtitle = "Scan each animal"
		counts.Done = observed
		if expected > observed {
			counts.Pending = expected - observed
		}
	default:
		if state == domain.WorkStateCompleted || state == domain.WorkStateVerificationPending {
			counts.Done = 1
		} else {
			counts.Pending = 1
		}
	}
	if state == domain.WorkStateOverdue {
		counts.NeedsAttention = 1
	}
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		ownerState = domain.OwnerStateMissing
	}
	return domain.Row{
		Module: domain.ModuleWeighing, SourceType: SourceType, SourceID: workItemID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: due, ClockLabel: clock,
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle, Counts: counts,
		// Admin-web has no per-bucket weighing page (buckets are worked on the phone); the
		// Weights page filtered to the bucket's park and capture mode is where its numbers
		// land, so that is where "Open in module" goes.
		Href: "/weighing/weights?park=" + url.QueryEscape(parkID) + "&weighing=" + url.QueryEscape(category),
	}.Finalize(), nil
}

func nullUUID(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func statesArg(states []domain.WorkState) []string {
	if len(states) == 0 {
		return nil
	}
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, string(s))
	}
	return out
}
