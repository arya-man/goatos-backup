// Package boardsource is PC Care's contribution to the cross-module Work Board: one board
// row per pc_care_tasks row (a category on a pen on a day), read from pc_care_tasks,
// pc_care_task_assignees and pc_care_task_animals plus the org tables every module may read
// (locations, workforce_members). It lives INSIDE the pccare package so the isolation rule
// holds in both directions: PC Care reads only its own tables here, and the board never
// reads a PC Care table at all.
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a scan, a submit, a verdict, a close or a
// reopen. The mapping from a task's (status, work_state) to a board work state is the ONLY
// business meaning this file adds, and it is stated once in workStateSQL.
package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	pcdomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every PC Care board row.
const SourceType = "pc_care_task"

// Source implements ports.Source over PC Care's own task rows.
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

func (s *Source) Module() domain.Module { return domain.ModulePCCare }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a PC Care task becomes a board work state. A task carries
// TWO columns: status is the evidence lifecycle (open -> pending_verification -> completed |
// rework) and work_state is the kernel clock (scheduled | delayed | closed | canceled).
// Status is read first because a submitted or judged task has left the clock behind:
//
//	status completed                -> completed            (the verifier accepted the work; the
//	                                                        kernel clock may still wait on the
//	                                                        pen's next-day visit, which rows on
//	                                                        its own under Tasks -- 2026-09-14)
//	status pending_verification     -> verification_pending (submitted; awaiting the verdict)
//	status rework                   -> rejected             (back with the operator)
//	work_state closed               -> completed            (the close landed; the pen-day is done)
//	work_state delayed              -> overdue              (rolled past its planned day; PC Care
//	                                                        has no documented carry band, so none
//	                                                        is invented here)
//	otherwise, an animal is scanned -> in_progress
//	otherwise                       -> due
//
// `canceled` rows are excluded in the WHERE clause: a canceled task is not work (nothing
// writes the value any more, migration 000257; pre-existing rows are history).
const workStateSQL = `CASE
  WHEN t.status = 'completed' THEN 'completed'
  WHEN t.status = 'pending_verification' THEN 'verification_pending'
  WHEN t.status = 'rework' THEN 'rejected'
  WHEN t.work_state = 'closed' THEN 'completed'
  WHEN t.work_state = 'delayed' THEN 'overdue'
  WHEN animals.scanned > 0 THEN 'in_progress'
  ELSE 'due'
END`

const countWorkStateSQL = `CASE
  WHEN t.status = 'completed' THEN 'completed'
  WHEN t.status = 'pending_verification' THEN 'verification_pending'
  WHEN t.status = 'rework' THEN 'rejected'
  WHEN t.work_state = 'closed' THEN 'completed'
  WHEN t.work_state = 'delayed' THEN 'overdue'
  WHEN EXISTS (
    SELECT 1 FROM pc_care_task_animals an
    WHERE an.tenant_id = t.tenant_id AND an.task_id = t.task_id
  ) THEN 'in_progress'
  ELSE 'due'
END`

// animalsLateral counts the task's own captures ONCE per task on
// pc_care_task_animals_task_idx (tenant_id, task_id, animal_row_id). An animal is DONE when it
// carries every compulsory slot of the task's PINNED card -- the keys snapshotted on the task
// row (PC CARE SOP, 2026-09-22) -- the same predicate the submit applies, so the board and the
// submit cannot disagree.
const animalsLateral = `
LEFT JOIN LATERAL (
  SELECT count(*)::int AS scanned,
         count(*) FILTER (WHERE an.sop_proofs ?& t.required_slot_keys)::int AS slots_done
  FROM pc_care_task_animals an
  WHERE an.tenant_id = t.tenant_id AND an.task_id = t.task_id
) animals ON true`

// baseWhere binds every read to one tenant, one park and one business date, which keeps the
// scan on pc_care_tasks_serving_idx (tenant_id, park_id, due_business_date, work_state)
// rather than the whole table. The business date is the CURRENT due date: a rolled-forward
// task appears on the day it is now due, and its clock label names the original plan.
//
// The owner scope matches ANY assignee, or a task with NO assignee at all (the park's pool,
// which an operator's own board must show -- live E2E 2026-09-11): an EXISTS on
// pc_care_task_assignees, whose primary
// key (tenant_id, task_id, operator_user_id) answers it as a single probe per task.
// Assignees are stored as USER ids, so the board's OwnerUserID binds directly.
const baseWhere = `
  t.tenant_id = $1::uuid
  AND t.park_id = $2::uuid
  AND t.due_business_date = $3::date
  AND t.work_state <> 'canceled'
  AND ($4::uuid IS NULL OR EXISTS (
        SELECT 1 FROM pc_care_task_assignees ao
        WHERE ao.tenant_id = t.tenant_id AND ao.task_id = t.task_id AND ao.operator_user_id = $4::uuid)
      OR NOT EXISTS (
        SELECT 1 FROM pc_care_task_assignees any_ao
        WHERE any_ao.tenant_id = t.tenant_id AND any_ao.task_id = t.task_id))`

// listSQL: the first assignee is the row's Owner -- the owner-scoped caller when there is one,
// else by workforce_member_id order; the assignee
// count lets the scanner append " +N". Assignees join workforce_members on user_id (the
// assignee table stores user ids); an assignee with no active profile still counts and still
// owns, but sorts last and renders a blank name.
// projection-review: membership=pc_care_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per task (primary key); group_key=(tenant_id, task_id) for the list and the derived board_state for the count; join_cardinality=the animal tallies are a LATERAL aggregate over pc_care_task_animals evaluated once per task (pre-aggregated, never a row fan-out), the owner is a LATERAL LIMIT 1 over pc_care_task_assignees ordered by the scoped caller then profile (one row), locations park/shed join on their primary key (1:1), and the owner-scope EXISTS on pc_care_task_assignees matches ANY assignee without multiplying rows; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, due_business_date and the optional assignee EXISTS, repeated verbatim in countSQL.
const listSQL = `
WITH tasks AS (
  SELECT t.task_id, t.category, t.park_id, t.shed_id, COALESCE(t.partition_label, '') AS partition_label,
         t.planned_business_date, t.due_business_date, t.work_state, t.status,
         animals.scanned, animals.video_done, animals.triple_done,
         ` + workStateSQL + ` AS board_state
  FROM pc_care_tasks t
  ` + animalsLateral + `
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR t.task_id > $5::uuid)
)
SELECT i.task_id::text, i.category, i.park_id::text, COALESCE(park.name, ''),
       COALESCE(i.shed_id::text, ''), COALESCE(shed.name, ''), i.partition_label,
       i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.status, i.board_state,
       i.scanned, i.video_done, i.triple_done,
       COALESCE(owner.user_id, ''), COALESCE(owner.member_id, ''), COALESCE(owner.display_name, ''),
       COALESCE(owner.assignee_count, 0)
FROM tasks i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_id
LEFT JOIN LATERAL (
  SELECT a.operator_user_id::text AS user_id,
         m.workforce_member_id::text AS member_id,
         m.display_name,
         count(*) OVER ()::int AS assignee_count
  FROM pc_care_task_assignees a
  LEFT JOIN workforce_members m
    ON m.tenant_id = a.tenant_id AND m.user_id = a.operator_user_id AND m.status = 'active'
  WHERE a.tenant_id = $1::uuid AND a.task_id = i.task_id
  -- The scoped caller comes first: on their own board a two-assignee task names THEM,
  -- not their partner. With no owner scope the expression is NULL and the stable
  -- profile order below decides.
  ORDER BY (a.operator_user_id = $4::uuid) DESC NULLS LAST,
           m.workforce_member_id ASC NULLS LAST, a.operator_user_id ASC
  LIMIT 1
) owner ON true
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.task_id
LIMIT $7`

// projection-review: membership=pc_care_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per task (primary key); group_key=(tenant_id, task_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=the animal tallies are a LATERAL aggregate over pc_care_task_animals evaluated once per task (pre-aggregated, never a row fan-out), the owner is a LATERAL LIMIT 1 over pc_care_task_assignees ordered by the scoped caller then profile (one row), locations park/shed join on their primary key (1:1), and the owner-scope EXISTS on pc_care_task_assignees matches ANY assignee without multiplying rows; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, due_business_date and the optional assignee EXISTS, repeated verbatim in countSQL.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + countWorkStateSQL + ` AS board_state
  FROM pc_care_tasks t
  WHERE ` + baseWhere + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`

// ListRows implements ports.Source.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	if err := ports.CheckUUIDSourceID(q.AfterSourceID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	rows, err := s.pool.Query(ctx, listSQL,
		q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("pccare boardsource list: %w", err)
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
		return nil, fmt.Errorf("pccare boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, countSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("pccare boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("pccare boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

func scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		taskID, category, parkID, parkName            string
		shedID, shedName, partitionLabel              string
		planned, due, kernelState, status, boardState string
		scanned, slotsDone                            int
		ownerUserID, ownerMemberID, ownerName         string
		assigneeCount                                 int
	)
	if err := rows.Scan(&taskID, &category, &parkID, &parkName,
		&shedID, &shedName, &partitionLabel, &planned, &due,
		&kernelState, &status, &boardState, &scanned, &slotsDone,
		&ownerUserID, &ownerMemberID, &ownerName, &assigneeCount); err != nil {
		return domain.Row{}, fmt.Errorf("pccare boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	clock := "Planned " + biztime.FarmDateFromBusinessDate(planned)
	if kernelState == pcdomain.WorkStateDelayed {
		clock = "Delayed · planned " + biztime.FarmDateFromBusinessDate(planned)
		severity = domain.SeverityWatch
		if state == domain.WorkStateOverdue {
			severity = domain.SeverityAtRisk
		}
	}
	if state == domain.WorkStateRejected {
		severity = domain.SeverityAtRisk
	}

	// Title: "<category label> · <pen>". A park-level task (inventory_vaccine, round-grain
	// feed & water removal) has no pen and carries the label alone rather than a dangling
	// separator.
	title := pcdomain.CategoryLabel(category)
	if pen.Display != "" {
		title += " · " + pen.Display
	}

	// Counts are the task's OWN captures: an animal is done when it carries every compulsory
	// slot of the task's pinned card (the same readiness rule submit applies), pending while
	// scanned but short of one. A task with no scan yet is one unit of work, done or pending
	// by state.
	done := slotsDone
	counts := domain.Counts{}
	subtitle := ""
	if scanned > 0 {
		counts.Done = done
		if scanned > done {
			counts.Pending = scanned - done
		}
		subtitle = strconv.Itoa(scanned) + " animals"
		if scanned == 1 {
			subtitle = "1 animal"
		}
	} else if state == domain.WorkStateCompleted || state == domain.WorkStateVerificationPending {
		counts.Done = 1
	} else {
		counts.Pending = 1
	}
	if state == domain.WorkStateOverdue || state == domain.WorkStateRejected {
		counts.NeedsAttention = 1
	}

	// One Owner per row: the first assignee; " +N" says how many more share the task.
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	if assigneeCount > 1 && owner.Name != "" {
		owner.Name += " +" + strconv.Itoa(assigneeCount-1)
	}
	ownerState := domain.OwnerStateAssigned
	if assigneeCount == 0 {
		ownerState = domain.OwnerStateMissing
	}
	return domain.Row{
		Module: domain.ModulePCCare, SourceType: SourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: due, ClockLabel: clock,
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle, Counts: counts,
		// PC Care has NO admin-web page yet (tasks are planned and worked on the phone, and
		// reviewed on /verify), so Href stays empty: a link that lands nowhere is worse than
		// no link. Fill it when a PC Care web surface ships.
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
