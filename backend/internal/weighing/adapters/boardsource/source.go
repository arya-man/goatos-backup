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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
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
//	completed (item OR bucket)   -> verification_pending (submitted; the close gate waits on the verdict)
//	bucket started, a scan bounced -> rejected           (the verifier sent an animal back; re-shoot owed)
//
// The bucket's own status is read alongside the item's because CLOSE writes only the bucket
// (weighing_campaign_sheds.status = 'closed'); the work item is moved to 'closed' by the
// kernel sweep, which reconciles only scheduled/delayed items, so a submitted bucket's item
// stays 'completed' after the close. Reading the item alone left every closed-after-submit
// bucket in "In review" forever (119 of 134 closed buckets on the 2026-09-10 clone). The same
// holds for SUBMIT of a whole pen: RecordShedObservation writes only the bucket, so a
// whole-pen bucket read "due" after its submit (live E2E 2026-09-11); and for CANCEL, which
// also writes only the bucket, so a dropped bucket stayed owed on the board.
//
// A bounced scan is the module's own `verification_status = 'rework'` on the observation --
// one mutable row kept until the operator re-shoots it -- so "any rework scan on a started
// bucket" is the rejected state, exactly what the module's rework_count reads.
//
//	delayed, past the D+2 band   -> overdue
//	otherwise, bucket started    -> in_progress
//	otherwise                    -> due                  (scheduled, or delayed inside the band)
//
// `canceled` rows are excluded in the WHERE clause: a canceled bucket is not work.
const workStateSQL = `CASE
  WHEN w.work_state = 'closed' OR b.status = 'closed' THEN 'completed'
  WHEN w.work_state = 'completed' OR b.status = 'completed' THEN 'verification_pending'
  WHEN b.status = 'in_progress' AND (` + reworkCountSQL + `) > 0 THEN 'rejected'
  WHEN w.work_state = 'delayed' AND (w.due_business_date - w.planned_business_date) > ` + "2" + ` THEN 'overdue'
  WHEN b.status = 'in_progress' THEN 'in_progress'
  ELSE 'due'
END`

const countWorkStateSQL = `CASE
  WHEN w.work_state = 'closed' OR b.status = 'closed' THEN 'completed'
  WHEN w.work_state = 'completed' OR b.status = 'completed' THEN 'verification_pending'
  WHEN b.status = 'in_progress' AND (` + reworkExistsSQL + `) THEN 'rejected'
  WHEN w.work_state = 'delayed' AND (w.due_business_date - w.planned_business_date) > ` + "2" + ` THEN 'overdue'
  WHEN b.status = 'in_progress' THEN 'in_progress'
  ELSE 'due'
END`

// reworkCountSQL is the module's own rework_count (close.go), verbatim in shape: submitted
// individual scans bounced by the verifier plus a bounced open whole-pen weigh. Weighing
// tables only.
const reworkCountSQL = `
  (SELECT count(*) FROM weighing_observations wo
    WHERE wo.tenant_id = w.tenant_id AND wo.campaign_shed_id = w.campaign_shed_id
      AND wo.submitted_at IS NOT NULL AND wo.verification_status = 'rework')
  + (SELECT count(*) FROM weighing_shed_observations wso
      WHERE wso.tenant_id = w.tenant_id AND wso.campaign_shed_id = w.campaign_shed_id
        AND wso.withdrawn_at IS NULL AND wso.verification_status = 'rework')`

const reworkExistsSQL = `
  EXISTS (
    SELECT 1 FROM weighing_observations wo
    WHERE wo.tenant_id = w.tenant_id AND wo.campaign_shed_id = w.campaign_shed_id
      AND wo.submitted_at IS NOT NULL AND wo.verification_status = 'rework')
  OR EXISTS (
    SELECT 1 FROM weighing_shed_observations wso
    WHERE wso.tenant_id = w.tenant_id AND wso.campaign_shed_id = w.campaign_shed_id
      AND wso.withdrawn_at IS NULL AND wso.verification_status = 'rework')`

// baseWhere binds every read to one tenant, one park and one business date, which is
// what keeps the scan on weighing_work_items_sweep_due_idx / the park+date index rather
// than the whole table. The business date is the CURRENT due date: a rolled-forward
// bucket appears on the day it is now due, and its clock label names the original plan.
const baseWhere = `
  w.tenant_id = $1::uuid
  AND w.park_id = $2::uuid
  AND w.due_business_date = $3::date
  AND w.work_state <> 'canceled'
  AND b.status <> 'canceled'
  AND ($4::uuid IS NULL OR w.operator_user_id = $4::uuid)`

// projection-review: membership=weighing_work_items rows of ONE tenant, park and due business date (canceled excluded on the item AND on its bucket), one row per campaign bucket (weighing_work_items_bucket_uidx); group_key=(tenant_id, work_item_id) for the list and the derived board_state for the count; join_cardinality=weighing_campaign_sheds joined on its primary key (1:1), locations park/shed on their primary key (1:1), workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), and the observed and rework counts are CORRELATED SUBQUERIES over weighing_observations and weighing_shed_observations per row, so nothing fans a bucket out; pagination=keyset on work_item_id ASC after $5, LIMIT $7, with the state filter inside WHERE so a page is never short after the cut; scope=tenant_id, park_id, due_business_date and the optional owner predicate, repeated verbatim in countSQL so rows and counts describe one set.
const listSQL = `
WITH items AS (
  SELECT w.work_item_id, w.campaign_id, w.campaign_shed_id, w.park_id, w.operator_user_id,
         w.weighing_category, w.shed_label, w.shed_location_id,
         w.planned_business_date, w.due_business_date, w.work_state, w.rolled_forward_count,
         b.status AS bucket_status, COALESCE(b.partition_label, '') AS partition_label,
         (` + reworkCountSQL + `) AS rework,
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
       i.work_state, i.bucket_status, i.board_state, i.rework,
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

const listClosedSQL = `
WITH items AS (
  SELECT w.work_item_id, w.campaign_id, w.campaign_shed_id, w.park_id, w.operator_user_id,
         w.weighing_category, w.shed_label, w.shed_location_id,
         w.planned_business_date, w.due_business_date, w.work_state, w.rolled_forward_count,
         b.status AS bucket_status, COALESCE(b.partition_label, '') AS partition_label,
         0 AS rework,
         'completed' AS board_state
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR w.work_item_id > $5::uuid)
    AND (w.work_state = 'closed' OR b.status = 'closed')
)
SELECT i.work_item_id::text, i.campaign_id::text, i.campaign_shed_id::text, i.park_id::text,
       COALESCE(park.name, ''),
       i.shed_location_id::text, COALESCE(NULLIF(shed.name, ''), i.shed_label), i.partition_label,
       i.weighing_category, i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.bucket_status, i.board_state, i.rework,
       COALESCE(i.operator_user_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, ''),
       (SELECT count(*) FROM weighing_observations o
         WHERE o.tenant_id = $1::uuid AND o.campaign_shed_id = i.campaign_shed_id) AS observed
FROM items i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_location_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = i.operator_user_id AND m.status = 'active'
ORDER BY i.work_item_id
LIMIT $6`

const listVerificationPendingSQL = `
WITH items AS (
  SELECT w.work_item_id, w.campaign_id, w.campaign_shed_id, w.park_id, w.operator_user_id,
         w.weighing_category, w.shed_label, w.shed_location_id,
         w.planned_business_date, w.due_business_date, w.work_state, w.rolled_forward_count,
         b.status AS bucket_status, COALESCE(b.partition_label, '') AS partition_label,
         0 AS rework,
         'verification_pending' AS board_state
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR w.work_item_id > $5::uuid)
    AND NOT (w.work_state = 'closed' OR b.status = 'closed')
    AND (w.work_state = 'completed' OR b.status = 'completed')
)
SELECT i.work_item_id::text, i.campaign_id::text, i.campaign_shed_id::text, i.park_id::text,
       COALESCE(park.name, ''),
       i.shed_location_id::text, COALESCE(NULLIF(shed.name, ''), i.shed_label), i.partition_label,
       i.weighing_category, i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.bucket_status, i.board_state, i.rework,
       COALESCE(i.operator_user_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, ''),
       (SELECT count(*) FROM weighing_observations o
         WHERE o.tenant_id = $1::uuid AND o.campaign_shed_id = i.campaign_shed_id) AS observed
FROM items i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_location_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = i.operator_user_id AND m.status = 'active'
ORDER BY i.work_item_id
LIMIT $6`

// projection-review: membership=weighing_work_items rows of ONE tenant, park and due business date (canceled excluded on the item AND on its bucket), one row per campaign bucket (weighing_work_items_bucket_uidx); group_key=(tenant_id, work_item_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=weighing_campaign_sheds joined on its primary key (1:1), locations park/shed on their primary key (1:1), workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), and the observed and rework counts are CORRELATED SUBQUERIES over weighing_observations and weighing_shed_observations per row, so nothing fans a bucket out; pagination=keyset on work_item_id ASC after $5, LIMIT $7, with the state filter inside WHERE so a page is never short after the cut; scope=tenant_id, park_id, due_business_date and the optional owner predicate, repeated verbatim in countSQL so rows and counts describe one set.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + countWorkStateSQL + ` AS board_state
  FROM weighing_work_items w
  JOIN weighing_campaign_sheds b
    ON b.tenant_id = w.tenant_id AND b.campaign_shed_id = w.campaign_shed_id
  WHERE ` + baseWhere + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`

// ListRows implements ports.Source.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	var out []domain.Row
	st, err := s.ListStatement(q, &out)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	bound := sqlbind.MustBind(st.Query.SQL(), st.Query.Args()...)
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("weighing boardsource list: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("weighing boardsource list rows: %w", err)
	}
	return out, nil
}

// ListStatement implements ports.BatchSource: the exact statement and decoding ListRows runs.
func (s *Source) ListStatement(q ports.SourceQuery, out *[]domain.Row) (ports.Statement, error) {
	if err := ports.CheckUUIDSourceID(q.AfterSourceID); err != nil {
		return ports.Statement{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	sql, args := listSQL, []any{q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit}
	if onlyWorkState(q.WorkStates, domain.WorkStateCompleted) {
		sql, args = listClosedSQL, []any{q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), limit}
	} else if onlyWorkState(q.WorkStates, domain.WorkStateVerificationPending) {
		sql, args = listVerificationPendingSQL, []any{q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), limit}
	}
	return ports.Statement{Query: sqlbind.MustBind(sql, args...), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadRows(rows, limit, scanRow)
		*out = got
		return err
	}}, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	var out map[domain.WorkState]int
	st, err := s.CountStatement(q, &out)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	bound := sqlbind.MustBind(st.Query.SQL(), st.Query.Args()...)
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return nil, fmt.Errorf("weighing boardsource count: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("weighing boardsource count scan: %w", err)
	}
	return out, nil
}

// CountStatement implements ports.BatchSource: the exact statement CountByState runs.
func (s *Source) CountStatement(q ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	return ports.Statement{Query: sqlbind.MustBind(countSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates)), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadCounts(rows)
		*out = got
		return err
	}}, nil
}

func scanRow(rows ports.ResultRows) (domain.Row, error) {
	var (
		workItemID, campaignID, bucketID, parkID, parkName  string
		shedID, shedName, partitionLabel, category          string
		planned, due, kernelState, bucketStatus, boardState string
		rework, observed                                    int
		ownerUserID, ownerMemberID, ownerName               string
	)
	if err := rows.Scan(&workItemID, &campaignID, &bucketID, &parkID, &parkName,
		&shedID, &shedName, &partitionLabel, &category, &planned, &due,
		&kernelState, &bucketStatus, &boardState, &rework,
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
		// Free-flow: there is NO expected animal count (AGENTS.md: weighing knows a scanned
		// string and a weight, never a roster), so Done is what was scanned and the only
		// pending unit before the first scan is the bucket itself. The earlier
		// `expected - observed` was a banned denominator that could never fire, because the
		// module writes expected_animal_count = 0 by design; it left every fresh bucket at
		// 0/0/0 (live E2E 2026-09-11).
		subtitle = "Scan each animal"
		counts.Done = observed
		if observed == 0 && state != domain.WorkStateCompleted {
			counts.Pending = 1
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
	if rework > 0 {
		// The verifier sent scans back: those are the card's attention, and the card is amber.
		counts.NeedsAttention = rework
		if severity == domain.SeverityOK {
			severity = domain.SeverityWatch
		}
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

func onlyWorkState(states []domain.WorkState, want domain.WorkState) bool {
	return len(states) == 1 && states[0] == want
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
