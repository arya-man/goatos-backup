// Package boardsource is the pen visit's contribution to the cross-module Work Board
// (maintainer decision 2026-09-14, retiring the 2026-09-12 shape): on the day a pen's
// next-day visit is DUE, the board rows it under TASKS -- "Pen visit · Castro 2", subtitle
// "Deworming yesterday" -- the same place the phone lists it (the Tasks module's "For me"
// tab). It is a task of its own on the board as on the phone; the module whose work raised
// it (Preventive Care, Vaccination) shows only that work's own state.
//
// ONE source, whatever raised the visit. It reads pen_visit_tasks and pen_visit_park_assignees
// (this module's own tables) plus the org tables every module may read (locations,
// workforce_members).
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a submit or a verdict; the mapping from a
// visit's (status, work_state) to a board work state is the ONLY business meaning this file
// adds, stated once in workStateSQL.
package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pvdomain "github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every pen visit board row.
const SourceType = "pen_visit_task"

// Source implements ports.Source over every pen visit, under the Tasks module.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the one pen-visit board source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

func (s *Source) Module() domain.Module { return domain.ModuleTasks }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a visit becomes a board work state. The gate speaks first
// (a clip with the verifier, or sent back) and the kernel clock only while a recording is
// still owed -- the same precedence the phone chip uses (pvdomain.StateChip).
//
//	status completed            -> completed            (the verifier approved the visit)
//	status pending_verification -> verification_pending (submitted; awaiting the verdict)
//	status rework               -> rejected             (back with the visitor)
//	work_state delayed          -> overdue              (rolled past the day it was owed)
//	otherwise                   -> due
const workStateSQL = `CASE
  WHEN v.status = 'completed' THEN 'completed'
  WHEN v.status = 'pending_verification' THEN 'verification_pending'
  WHEN v.status = 'rework' THEN 'rejected'
  WHEN v.work_state = 'delayed' THEN 'overdue'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one park and one business date on
// pen_visit_tasks_park_idx (tenant_id, park_id, work_state, due_business_date). The business
// date is the CURRENT due date: a rolled-forward visit appears on the day it is now due, and
// its clock label names the day it was owed.
//
// The owner scope matches any CONFIGURED VISITOR of the park (pen_visit_park_assignees, PK
// (tenant_id, park_id, user_id)): a visit belongs to whoever the HRMS config names, and any
// one of them may record it. A visit is never in the unclaimed pool -- a park with nobody
// configured raises no visit at all.
func (s *Source) baseWhere() string {
	return `
  v.tenant_id = $1::uuid
  AND v.park_id = $2::uuid
  AND v.due_business_date = $3::date
  AND v.work_state <> 'canceled'
  AND ($4::uuid IS NULL OR EXISTS (
        SELECT 1 FROM pen_visit_park_assignees a
        WHERE a.tenant_id = v.tenant_id AND a.park_id = v.park_id AND a.user_id = $4::uuid))`
}

// projection-review: membership=pen_visit_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per visit (primary key); group_key=(tenant_id, task_id) for the list and the derived board_state for the count; join_cardinality=the owner is a LATERAL LIMIT 1 over pen_visit_park_assignees ordered by the scoped caller then profile (one row), the visitor count a window inside it, locations park/shed join on their primary key (1:1), and the owner-scope EXISTS on pen_visit_park_assignees matches ANY configured visitor without multiplying rows; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, due_business_date and the optional visitor EXISTS, repeated verbatim in the count query.
func (s *Source) listSQL() string {
	return `
WITH visits AS (
  SELECT v.task_id, v.park_id, v.shed_id, COALESCE(v.partition_label, '') AS partition_label,
         v.reasons, v.source_business_date, v.planned_business_date, v.due_business_date,
         v.work_state, v.status, v.delayed_since_business_date, v.submitted_by,
         ` + workStateSQL + ` AS board_state
  FROM pen_visit_tasks v
  WHERE ` + s.baseWhere() + `
    AND ($5::uuid IS NULL OR v.task_id > $5::uuid)
)
SELECT i.task_id::text, i.park_id::text, COALESCE(park.name, ''),
       i.shed_id::text, COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), i.partition_label,
       i.reasons, i.source_business_date::text, i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.status, i.board_state,
       COALESCE(owner.user_id, ''), COALESCE(owner.member_id, ''), COALESCE(owner.display_name, ''),
       COALESCE(owner.visitor_count, 0)
FROM visits i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_id
LEFT JOIN LATERAL (
  SELECT a.user_id::text AS user_id,
         m.workforce_member_id::text AS member_id,
         m.display_name,
         count(*) OVER ()::int AS visitor_count
  FROM pen_visit_park_assignees a
  LEFT JOIN workforce_members m
    ON m.tenant_id = a.tenant_id AND m.user_id = a.user_id AND m.status = 'active'
  WHERE a.tenant_id = $1::uuid AND a.park_id = i.park_id
  -- The person who actually went first, then the scoped caller, then a stable profile
  -- order: a submitted visit names its visitor, an owed one names the caller on their own
  -- board and the first configured person on everyone else's.
  ORDER BY (a.user_id = i.submitted_by) DESC NULLS LAST,
           (a.user_id = $4::uuid) DESC NULLS LAST,
           m.workforce_member_id ASC NULLS LAST, a.user_id ASC
  LIMIT 1
) owner ON true
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.task_id
LIMIT $7`
}

// projection-review: membership=pen_visit_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per visit (primary key); group_key=(tenant_id, task_id) (the count groups by the SAME derived board_state over the SAME membership); join_cardinality=none in the count (the owner-scope EXISTS matches ANY configured visitor without multiplying rows); pagination=none, whole-filter aggregate; scope=tenant_id, park_id, due_business_date and the optional visitor EXISTS, repeated verbatim from the list query.
func (s *Source) countSQL() string {
	return `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM pen_visit_tasks v
  WHERE ` + s.baseWhere() + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`
}

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
		return nil, fmt.Errorf("pen visit boardsource list: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("pen visit boardsource list rows: %w", err)
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
	sql, args := s.listSQL(), []any{q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit}
	return ports.Statement{Query: sqlbind.MustBind(sql, args...), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadRows(rows, limit, s.scanRow)
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
		return nil, fmt.Errorf("pen visit boardsource count: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("pen visit boardsource count scan: %w", err)
	}
	return out, nil
}

// CountStatement implements ports.BatchSource: the exact statement CountByState runs.
func (s *Source) CountStatement(q ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	return ports.Statement{Query: sqlbind.MustBind(s.countSQL(), q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates)), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadCounts(rows)
		*out = got
		return err
	}}, nil
}

func (s *Source) scanRow(rows ports.ResultRows) (domain.Row, error) {
	var (
		taskID, parkID, parkName, shedID, shedName, partitionLabel string
		reasons                                                    []string
		sourceDate, planned, due, kernelState, status, boardState  string
		ownerUserID, ownerMemberID, ownerName                      string
		visitorCount                                               int
	)
	if err := rows.Scan(&taskID, &parkID, &parkName, &shedID, &shedName, &partitionLabel,
		&reasons, &sourceDate, &planned, &due, &kernelState, &status, &boardState,
		&ownerUserID, &ownerMemberID, &ownerName, &visitorCount); err != nil {
		return domain.Row{}, fmt.Errorf("pen visit boardsource scan: %w", err)
	}
	if oploc.NormalizePartition(partitionLabel) == oploc.WholeSentinel {
		partitionLabel = ""
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	clock := "Visit due " + biztime.FarmDateFromBusinessDate(due)
	if kernelState == pvdomain.WorkStateDelayed {
		clock = "Visit delayed · owed " + biztime.FarmDateFromBusinessDate(planned)
		severity = domain.SeverityWatch
		if state == domain.WorkStateOverdue {
			severity = domain.SeverityAtRisk
		}
	}
	if state == domain.WorkStateRejected {
		severity = domain.SeverityAtRisk
	}

	// Title: the visit itself -- "Pen visit · Castro 2" -- the same words the phone's Tasks tab
	// uses; the subtitle names what happened in the pen and when, so the reader knows why.
	title := "Pen visit"
	if pen.Display != "" {
		title += " · " + pen.Display
	}
	labels := make([]string, 0, len(reasons))
	for i, r := range pvdomain.SortReasons(reasons) {
		l := pvdomain.ReasonLabel(r)
		if i > 0 {
			l = strings.ToLower(l)
		}
		labels = append(labels, l)
	}
	subtitle := strings.Join(labels, ", ")
	if subtitle == "" {
		subtitle = "Preventive care"
	}
	subtitle += " · work done " + biztime.FarmDateFromBusinessDate(sourceDate)

	counts := domain.Counts{}
	switch state {
	case domain.WorkStateCompleted, domain.WorkStateVerificationPending:
		counts.Done = 1
	default:
		counts.Pending = 1
	}
	if state == domain.WorkStateOverdue || state == domain.WorkStateRejected {
		counts.NeedsAttention = 1
	}

	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	if visitorCount > 1 && owner.Name != "" && status == pvdomain.StatusOpen {
		// Owed to any of the park's configured visitors: name the first and say how many more.
		owner.Name += " +" + strconv.Itoa(visitorCount-1)
	}
	ownerState := domain.OwnerStateAssigned
	if visitorCount == 0 {
		ownerState = domain.OwnerStateMissing
	}

	// Visits are phone-only (recorded on the Tasks module's "For me" tab); there is no web
	// page to land on, so the row carries no href rather than one that lands nowhere.
	return domain.Row{
		Module: domain.ModuleTasks, SourceType: SourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: due, ClockLabel: clock,
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle, Counts: counts,
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
