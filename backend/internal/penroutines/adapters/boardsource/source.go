// Package boardsource is the pen routine's contribution to the cross-module Work Board: on the
// day a routine check is DUE, the board rows it under TASKS -- "Pen cleaning · Castro 2",
// subtitle the routine's cadence line or the work that raised it -- the same place the phone
// lists it (the Routines module).
//
// ONE source for every routine. It reads pen_routine_tasks, pen_routine_definitions,
// pen_routine_versions and pen_routine_assignees (this module's own tables) plus the org
// tables every module may read (locations, workforce_members).
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a submit or a verdict; the mapping from a
// task's (status, work_state) to a board work state is the ONLY business meaning this file
// adds, stated once in workStateSQL.
package boardsource

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	prdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every pen routine board row.
const SourceType = "pen_routine_task"

// Source implements ports.Source over every routine task, under the Tasks module.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the one pen-routine board source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

func (s *Source) Module() domain.Module { return domain.ModuleTasks }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a routine task becomes a board work state. The gate speaks
// first (with the verifier, or sent back) and the kernel clock only while work is still owed
// -- the same precedence the phone chip uses (prdomain.StateChip).
const workStateSQL = `CASE
  WHEN v.status = 'completed' THEN 'completed'
  WHEN v.status = 'pending_verification' THEN 'verification_pending'
  WHEN v.status = 'rework' THEN 'rejected'
  WHEN v.work_state = 'delayed' THEN 'overdue'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one park and one business date on
// pen_routine_tasks_park_day_idx (tenant_id, park_id, due_business_date, work_state, task_id).
// The business date is the CURRENT due date: a rolled-forward task appears on the day it is
// now due, and its clock label names the day it was owed. The owner scope matches any
// ASSIGNEE of the routine (pen_routine_assignees, PK (tenant, routine, user)).
func (s *Source) baseWhere() string {
	return `
  v.tenant_id = $1::uuid
  AND v.park_id = $2::uuid
  AND v.due_business_date = $3::date
  AND v.work_state <> 'canceled'
  AND ($4::uuid IS NULL OR EXISTS (
        SELECT 1 FROM pen_routine_assignees a
        WHERE a.tenant_id = v.tenant_id AND a.routine_id = v.routine_id AND a.user_id = $4::uuid))`
}

// projection-review: membership=pen_routine_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per task (primary key); group_key=(tenant_id, task_id) for the list and the derived board_state for the count; join_cardinality=definition and pinned version 1:1 on their PKs, the owner a LATERAL LIMIT 1 over pen_routine_assignees ordered by the submitter then the scoped caller then profile (one row), the assignee count a window inside it, locations park/shed 1:1 (PK), and the owner-scope EXISTS matches ANY assignee without multiplying rows; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, due_business_date and the optional assignee EXISTS, repeated verbatim in the count query.
func (s *Source) listSQL() string {
	return `
WITH tasks AS (
  SELECT v.task_id, v.routine_id, v.routine_version, v.park_id, v.shed_id, COALESCE(v.partition_label, '') AS partition_label,
         v.trigger_kinds, v.source_business_date, v.planned_business_date, v.due_business_date,
         v.work_state, v.status, v.submitted_by,
         ` + workStateSQL + ` AS board_state
  FROM pen_routine_tasks v
  WHERE ` + s.baseWhere() + `
    AND ($5::uuid IS NULL OR v.task_id > $5::uuid)
)
SELECT i.task_id::text, i.park_id::text, COALESCE(park.name, ''),
       i.shed_id::text, COALESCE(NULLIF(shed.name, ''), shed.location_code, ''), i.partition_label,
       ver.name, d.cadence_kind, d.weekdays, d.month_days, d.after_work_kinds, d.due_offset_days,
       i.trigger_kinds, i.source_business_date::text, i.planned_business_date::text, i.due_business_date::text,
       i.work_state, i.status, i.board_state,
       COALESCE(owner.user_id, ''), COALESCE(owner.member_id, ''), COALESCE(owner.display_name, ''),
       COALESCE(owner.assignee_count, 0)
FROM tasks i
JOIN pen_routine_definitions d ON d.tenant_id = $1::uuid AND d.routine_id = i.routine_id
JOIN pen_routine_versions ver ON ver.tenant_id = $1::uuid AND ver.routine_id = i.routine_id AND ver.version = i.routine_version
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_id
LEFT JOIN LATERAL (
  SELECT a.user_id::text AS user_id,
         m.workforce_member_id::text AS member_id,
         m.display_name,
         count(*) OVER ()::int AS assignee_count
  FROM pen_routine_assignees a
  LEFT JOIN workforce_members m
    ON m.tenant_id = a.tenant_id AND m.user_id = a.user_id AND m.status = 'active'
  WHERE a.tenant_id = $1::uuid AND a.routine_id = i.routine_id
  ORDER BY (a.user_id = i.submitted_by) DESC NULLS LAST,
           (a.user_id = $4::uuid) DESC NULLS LAST,
           m.workforce_member_id ASC NULLS LAST, a.user_id ASC
  LIMIT 1
) owner ON true
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.task_id
LIMIT $7`
}

// projection-review: membership=pen_routine_tasks rows of ONE tenant, park and due_business_date (canceled excluded), one row per task (primary key); group_key=(tenant_id, task_id) (the count groups by the SAME derived board_state over the SAME membership); join_cardinality=none in the count (the owner-scope EXISTS matches ANY assignee without multiplying rows); pagination=none, whole-filter aggregate; scope=tenant_id, park_id, due_business_date and the optional assignee EXISTS, repeated verbatim from the list query.
func (s *Source) countSQL() string {
	return `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM pen_routine_tasks v
  WHERE ` + s.baseWhere() + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`
}

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
	rows, err := s.pool.Query(ctx, s.listSQL(),
		q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("pen routine boardsource list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := s.scanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pen routine boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, s.countSQL(), q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("pen routine boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("pen routine boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

func (s *Source) scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		taskID, parkID, parkName, shedID, shedName, partitionLabel string
		routineName, cadenceKind                                   string
		weekdays, monthDays                                        []int16
		afterWorkKinds, triggerKinds                               []string
		dueOffset                                                  int
		sourceDate, planned, due, kernelState, status, boardState  string
		ownerUserID, ownerMemberID, ownerName                      string
		assigneeCount                                              int
	)
	if err := rows.Scan(&taskID, &parkID, &parkName, &shedID, &shedName, &partitionLabel,
		&routineName, &cadenceKind, &weekdays, &monthDays, &afterWorkKinds, &dueOffset,
		&triggerKinds, &sourceDate, &planned, &due, &kernelState, &status, &boardState,
		&ownerUserID, &ownerMemberID, &ownerName, &assigneeCount); err != nil {
		return domain.Row{}, fmt.Errorf("pen routine boardsource scan: %w", err)
	}
	if oploc.NormalizePartition(partitionLabel) == oploc.WholeSentinel {
		partitionLabel = ""
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	clock := "Due " + biztime.FarmDateFromBusinessDate(due)
	if kernelState == prdomain.WorkStateDelayed {
		clock = "Delayed · owed " + biztime.FarmDateFromBusinessDate(planned)
		severity = domain.SeverityWatch
		if state == domain.WorkStateOverdue {
			severity = domain.SeverityAtRisk
		}
	}
	if state == domain.WorkStateRejected {
		severity = domain.SeverityAtRisk
	}

	// Title and subtitle are the domain's own words: the routine at the pen, and why today.
	task := prdomain.Task{
		RoutineName:  routineName,
		PenLabel:     pen.Display,
		TriggerKinds: triggerKinds,
		SourceDate:   sourceDate,
		CadenceLine: prdomain.CadenceLine(prdomain.Definition{
			CadenceKind: cadenceKind, Weekdays: toInts(weekdays), MonthDays: toInts(monthDays),
			AfterWorkKinds: afterWorkKinds, DueOffsetDays: dueOffset,
		}),
	}
	title := prdomain.Title(task)
	subtitle := prdomain.ReasonLine(task, due)

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
	if assigneeCount > 1 && owner.Name != "" && status == prdomain.StatusOpen {
		// Owed to any of the routine's people: name the first and say how many more.
		owner.Name += " +" + strconv.Itoa(assigneeCount-1)
	}
	ownerState := domain.OwnerStateAssigned
	if assigneeCount == 0 {
		ownerState = domain.OwnerStateMissing
	}

	return domain.Row{
		Module: domain.ModuleTasks, SourceType: SourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: due, ClockLabel: clock,
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle, Counts: counts,
		Href: "/routines?park_id=" + parkID + "&business_date=" + due,
	}.Finalize(), nil
}

func toInts(in []int16) []int {
	out := make([]int, 0, len(in))
	for _, v := range in {
		out = append(out, int(v))
	}
	return out
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

// projection-review: membership=the ONE pen_routine_tasks row named by (tenant_id, park_id, due_business_date, task_id) with canceled excluded, so a row the board would not show is an empty page; group_key=(tenant_id, task_id), one task is one subtask; join_cardinality=workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), version 1:1 on PK; pagination=none -- one unit, total is 1 or 0; scope=tenant_id, park_id, due_business_date and task_id, the same predicate the row read binds.
func (s *Source) subtaskSQL() string {
	return `
SELECT v.task_id::text, ver.name, ver.review_kind, v.trigger_kinds, v.source_business_date::text, v.planned_business_date::text,
       v.work_state, v.status, COALESCE(v.rework_reason, ''), v.entered_at, v.submitted_at, v.verified_at,
       COALESCE(v.submitted_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM pen_routine_tasks v
JOIN pen_routine_versions ver ON ver.tenant_id = $1::uuid AND ver.routine_id = v.routine_id AND ver.version = v.routine_version
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = v.submitted_by AND m.status = 'active'
WHERE v.tenant_id = $1::uuid
  AND v.park_id = $2::uuid
  AND v.due_business_date = $3::date
  AND v.task_id = $4::uuid
  AND v.work_state <> 'canceled'`
}

// ListSubtasks implements ports.SubtaskSource: the task drills into itself as ONE subtask with
// the chain check in -> submit -> verify.
func (s *Source) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	afterRank, _, err := domain.ParseSubtaskKey(q.AfterKey)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	if err := ports.CheckUUIDSourceID(q.SourceID); err != nil {
		return domain.SubtaskPage{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	st, err := s.readSubtask(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return page, nil
	}
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	page.Total = 1
	if q.AfterKey != "" {
		rank, _, _ := domain.ParseSubtaskKey(st.Key)
		if afterRank >= rank {
			return page, nil
		}
	}
	page.Subtasks = append(page.Subtasks, st)
	return page, nil
}

func (s *Source) readSubtask(ctx context.Context, q ports.SubtaskQuery) (domain.Subtask, error) {
	var (
		taskID, routineName, reviewKind                   string
		triggerKinds                                      []string
		sourceDate, planned, workState, status, reworkMsg string
		enteredAt, submittedAt, verifiedAt                *time.Time
		ownerUserID, ownerMemberID, ownerName             string
	)
	err := s.pool.QueryRow(ctx, s.subtaskSQL(), q.TenantID, q.ParkID, q.BusinessDate, q.SourceID).Scan(
		&taskID, &routineName, &reviewKind, &triggerKinds, &sourceDate, &planned, &workState, &status, &reworkMsg,
		&enteredAt, &submittedAt, &verifiedAt, &ownerUserID, &ownerMemberID, &ownerName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Subtask{}, err
		}
		return domain.Subtask{}, fmt.Errorf("pen routine boardsource subtask: %w", err)
	}
	do := domain.Step{Name: "Do", State: domain.StepTodo}
	steps := []domain.Step{do}
	state := domain.WorkStateDue
	attention := false
	if submittedAt != nil {
		do.State = domain.StepDone
		do.Detail = "Submitted " + biztime.FarmDate(submittedAt.In(biztime.DefaultLocation()))
	}
	if reviewKind == prdomain.ReviewVerifier {
		verify := domain.Step{Name: "Verify", State: domain.StepLocked}
		switch status {
		case prdomain.StatusRework:
			do.State = domain.StepTodo
			verify.State, verify.Detail = domain.StepRework, reworkMsg
			state, attention = domain.WorkStateRejected, true
		case prdomain.StatusCompleted:
			verify.State = domain.StepDone
			if verifiedAt != nil {
				verify.Detail = "Verified " + biztime.FarmDate(verifiedAt.In(biztime.DefaultLocation()))
			}
			state = domain.WorkStateCompleted
		case prdomain.StatusPendingVerification:
			verify.State = domain.StepInReview
			state = domain.WorkStateVerificationPending
		default:
			if workState == prdomain.WorkStateDelayed {
				state, attention = domain.WorkStateOverdue, true
				do.Detail = "Owed " + biztime.FarmDateFromBusinessDate(planned)
			}
		}
		steps = []domain.Step{do, verify}
	} else {
		switch status {
		case prdomain.StatusCompleted:
			state = domain.WorkStateCompleted
		default:
			if workState == prdomain.WorkStateDelayed {
				state, attention = domain.WorkStateOverdue, true
				do.Detail = "Owed " + biztime.FarmDateFromBusinessDate(planned)
			}
		}
		steps = []domain.Step{do}
	}
	task := prdomain.Task{RoutineName: routineName, TriggerKinds: triggerKinds, SourceDate: sourceDate}
	subtitle := prdomain.ReasonLine(task, q.BusinessDate)
	name := strings.TrimSpace(routineName)
	if name == "" {
		name = "Routine check"
	}
	rank := domain.RankFor(state, attention)
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, taskID), Name: name, Subtitle: subtitle,
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: steps,
	}.Finalize(), nil
}
