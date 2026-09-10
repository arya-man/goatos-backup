// Package boardsource is Feed's contribution to the cross-module Work Board: one board row
// per feed transport task (one trip per physical shed per feed day), read from
// feed_transport_tasks plus the org tables every module may read. It lives INSIDE the
// feeddirection package so the isolation rule holds in both directions: feed reads only
// its own table here, and the board never reads a feed table at all.
//
// READ-ONLY and REPORTING-ONLY. Nothing here materializes, submits, verifies or reworks a
// transport task. The mapping from a task status to a board work state is the ONLY
// business meaning this file adds, and it is stated once in workStateSQL.
//
// Transport is SHED grain (one trip per physical shed, migration 000155), so
// partition_label is carried for the surviving pen rows that still hold evidence but is
// ” on every row the materializer writes today.
package boardsource

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every feed transport board row.
const SourceType = "feed_transport_task"

// Source implements ports.Source over feed's own transport task rows.
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

func (s *Source) Module() domain.Module { return domain.ModuleFeed }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a transport task status becomes a board work state.
//
//	completed        -> completed            (verifier approved the trip's video)
//	verification_due -> verification_pending (operator filmed the trip; verdict outstanding)
//	rework           -> rejected             (verifier sent it back; a new video is owed)
//	due              -> due                  (the trip is owed today; staged by 15:00)
//
// `retired` rows are excluded in the WHERE clause: a retired task was superseded by the
// shed-grain row (000143/000155) and is not work. There is no in_progress: a transport task
// has no partial state between "owed" and "filmed".
const workStateSQL = `CASE
  WHEN t.status = 'completed' THEN 'completed'
  WHEN t.status = 'verification_due' THEN 'verification_pending'
  WHEN t.status = 'rework' THEN 'rejected'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one park and one business date.
//
// Index: feed_transport_tasks_today_partition_idx (tenant_id, business_date, status,
// shed_id, partition_label) from migration 000143 -- the leading (tenant_id, business_date)
// pair is an equality seek; park_id and the status exclusion are residual filters over the
// one feed day's rows (one row per shed, ~200 per tenant-day). When the owner lens is set,
// feed_transport_tasks_operator_idx (tenant_id, operator_id, business_date, status) from
// 000054 serves the same predicate keyed on the operator instead.
const baseWhere = `
  t.tenant_id = $1::uuid
  AND t.business_date = $2::date
  AND t.park_id = $3::uuid
  AND t.status <> 'retired'
  AND ($4::uuid IS NULL OR ` + ownerSQL + ` = $4::uuid)`

// ownerSQL is who the row belongs to. The materializer writes no operator on a transport
// task and SubmitTransport records the operator on the ATTEMPT, so a task assigned to
// nobody still has a real owner once someone filmed it: the operator of the task's current
// attempt. Without this every transport row on the board read owner "missing", including
// work someone demonstrably did (339 of 339 completed rows on the 2026-09-10 clone).
const ownerSQL = `COALESCE(t.operator_id, att.operator_id)`

// fromSQL joins the task's current attempt on its primary key (1:1, absent until the first
// submit). feed_transport_attempts is this module's own table.
const fromSQL = `feed_transport_tasks t
  LEFT JOIN feed_transport_attempts att ON att.tenant_id = t.tenant_id AND att.attempt_id = t.current_attempt_id`

// projection-review: membership=feed_transport_tasks rows of ONE tenant, park and business date (retired excluded), one row per physical shed per day (feed_transport_tasks_daily_shed_uq); group_key=(tenant_id, task_id) for the list and the derived board_state for the count; join_cardinality=feed_transport_attempts on its primary key via the task's current_attempt_id (1:{0,1}), locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), so no join fans a task out; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, business_date and the optional operator predicate, repeated verbatim in countSQL.
const listSQL = `
WITH tasks AS (
  SELECT t.task_id, t.park_id, t.shed_id, COALESCE(t.partition_label, '') AS partition_label,
         t.business_date, t.scheduled_at, t.status, ` + ownerSQL + ` AS operator_id,
         ` + workStateSQL + ` AS board_state
  FROM ` + fromSQL + `
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR t.task_id > $5::uuid)
)
SELECT x.task_id::text, x.park_id::text, COALESCE(park.name, ''),
       x.shed_id::text, COALESCE(shed.name, ''), x.partition_label,
       x.business_date::text, x.scheduled_at, x.status, x.board_state,
       COALESCE(x.operator_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM tasks x
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = x.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = x.shed_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = x.operator_id AND m.status = 'active'
WHERE ($6::text[] IS NULL OR x.board_state = ANY($6::text[]))
ORDER BY x.task_id
LIMIT $7`

// projection-review: membership=feed_transport_tasks rows of ONE tenant, park and business date (retired excluded), one row per physical shed per day (feed_transport_tasks_daily_shed_uq); group_key=(tenant_id, task_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=feed_transport_attempts on its primary key via the task's current_attempt_id (1:{0,1}), locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' whose (tenant_id,user_id) is unique by the partial active index (at most 1), so no join fans a task out; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, business_date and the optional operator predicate, repeated verbatim in countSQL.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM ` + fromSQL + `
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
		q.TenantID, q.BusinessDate, q.ParkID, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("feed transport boardsource list: %w", err)
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
		return nil, fmt.Errorf("feed transport boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, countSQL, q.TenantID, q.BusinessDate, q.ParkID, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("feed transport boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("feed transport boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

func scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		taskID, parkID, parkName, shedID, shedName, partitionLabel string
		businessDate, status, boardState                           string
		scheduledAt                                                time.Time
		ownerUserID, ownerMemberID, ownerName                      string
	)
	if err := rows.Scan(&taskID, &parkID, &parkName, &shedID, &shedName, &partitionLabel,
		&businessDate, &scheduledAt, &status, &boardState,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Row{}, fmt.Errorf("feed transport boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	counts := domain.Counts{}
	switch state {
	case domain.WorkStateCompleted:
		counts.Done = 1
	case domain.WorkStateRejected:
		counts.Pending = 1
		counts.NeedsAttention = 1
	default:
		counts.Pending = 1
	}
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		ownerState = domain.OwnerStateMissing
	}
	due := scheduledAt
	return domain.Row{
		Module: domain.ModuleFeed, SourceType: SourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: businessDate, DueAt: &due, ClockLabel: "Stage by 15:00",
		WorkState: state, Severity: domain.SeverityOK,
		Owner: owner, OwnerState: ownerState,
		Title: "Transport " + pen.Display, Subtitle: "One trip · stage by 15:00", Counts: counts,
		// Transport has no admin-web page of its own (the trip is filmed and verified on the
		// phone); the Feed analytics page is the module's web surface.
		Href: "/feed/analytics",
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
