// Package boardsource is the Counts vertical's contribution to the cross-module Work Board:
// two sources, one per kind of work the vertical owns on a day -- a milk feeding session
// (milk_feeding_tasks) and an approval request (counts_approval_requests). Each reads ONLY
// its own module table plus the org tables every module may read (locations,
// workforce_members). It lives INSIDE the counts package so the isolation rule holds in both
// directions: counts reads only its own tables here, and the board never reads a counts
// table at all.
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a submit, a verdict or a decision. The
// mapping from a module status to a board work state is the ONLY business meaning each
// source adds, and each is stated once in its own *WorkStateSQL constant.
package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// MilkFeedingSourceType is the ref type carried on every milk feeding board row.
const MilkFeedingSourceType = "milk_feeding_task"

// MilkFeedingSource implements ports.Source over milk_feeding_tasks: one row per farm
// feeding session (migration 000097 moved the grain from shed to FARM, so an active task
// names a park and a session number; shed_id is NULL on every non-retired row).
type MilkFeedingSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewMilkFeeding constructs the milk feeding source.
func NewMilkFeeding(pool *pgxpool.Pool, timeout time.Duration) *MilkFeedingSource {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &MilkFeedingSource{pool: pool, timeout: timeout}
}

func (s *MilkFeedingSource) Module() domain.Module { return domain.ModuleMilk }
func (s *MilkFeedingSource) SourceType() string    { return MilkFeedingSourceType }

// milkWorkStateSQL is the one place a milk feeding task becomes a board work state.
//
//	not_submitted        -> due                  (the session is owed; the clock is its due_at)
//	pending_verification -> verification_pending (proof submitted; the verifier decides)
//	completed            -> completed
//	rework               -> rejected             (bounced; back with the operator to re-shoot)
//
// `retired` rows are excluded in the WHERE clause: 000097 retired the pre-farm-grain shed
// rows as immutable history, and history is not work.
const milkWorkStateSQL = `CASE t.status
  WHEN 'not_submitted' THEN 'due'
  WHEN 'pending_verification' THEN 'verification_pending'
  WHEN 'completed' THEN 'completed'
  WHEN 'rework' THEN 'rejected'
  ELSE 'due'
END`

// milkBaseWhere binds every read to one tenant, one feeding date and one park, which keeps
// the scan on milk_feeding_tasks_worklist_idx (tenant_id, feeding_date, status, park_id,
// session_no): tenant + feeding_date are the leading equality columns, status <> 'retired'
// and park_id are filtered inside that slice.
//
// The owner filter matches assigned_operator_id, which the module's own submit path sets to
// the FIRST submitter (coalesce(assigned_operator_id, submitted_by)) -- a task is a claim
// pool until someone submits it, so an unassigned task matches no owner lens.
const milkBaseWhere = `
  t.tenant_id = $1::uuid
  AND t.feeding_date = $3::date
  AND t.park_id = $2::uuid
  AND t.status <> 'retired'
  AND ($4::uuid IS NULL OR t.assigned_operator_id = $4::uuid)`

const milkListSQL = `
WITH tasks AS (
  SELECT t.task_id, t.park_id, t.shed_id, t.feeding_date, t.session_no, t.due_at, t.status,
         t.head_count, t.assigned_operator_id,
         ` + milkWorkStateSQL + ` AS board_state
  FROM milk_feeding_tasks t
  WHERE ` + milkBaseWhere + `
    AND ($5::uuid IS NULL OR t.task_id > $5::uuid)
)
SELECT i.task_id::text, i.park_id::text, COALESCE(park.name, ''),
       COALESCE(i.shed_id::text, ''), COALESCE(shed.name, ''),
       i.feeding_date::text, i.session_no, i.due_at, i.status, i.board_state, i.head_count,
       COALESCE(i.assigned_operator_id::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM tasks i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = i.assigned_operator_id AND m.status = 'active'
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.task_id
LIMIT $7`

const milkCountSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + milkWorkStateSQL + ` AS board_state
  FROM milk_feeding_tasks t
  WHERE ` + milkBaseWhere + `
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`

// ListRows implements ports.Source.
func (s *MilkFeedingSource) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	rows, err := s.pool.Query(ctx, milkListSQL,
		q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit)
	if err != nil {
		return nil, fmt.Errorf("milk boardsource list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := scanMilkRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("milk boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *MilkFeedingSource) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, milkCountSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("milk boardsource count: %w", err)
	}
	defer rows.Close()
	return scanCounts(rows, "milk boardsource count")
}

func scanMilkRow(rows pgx.Rows) (domain.Row, error) {
	var (
		taskID, parkID, parkName, shedID, shedName string
		feedingDate, status, boardState            string
		sessionNo, headCount                       int
		dueAt                                      time.Time
		ownerUserID, ownerMemberID, ownerName      string
	)
	if err := rows.Scan(&taskID, &parkID, &parkName, &shedID, &shedName,
		&feedingDate, &sessionNo, &dueAt, &status, &boardState, &headCount,
		&ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Row{}, fmt.Errorf("milk boardsource scan: %w", err)
	}
	// Active milk tasks are farm-grain (shed_id NULL), so the pen is usually empty and the
	// title names the park instead. A legacy shed-grain row still composes through oploc.
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, Display: loc.Display()}
	where := pen.Display
	if where == "" {
		where = parkName
	}
	title := "Milk feeding"
	if where != "" {
		title += " · " + where
	}
	session := "Session " + strconv.Itoa(sessionNo)
	due := dueAt.In(biztime.DefaultLocation())

	state := domain.WorkState(boardState)
	counts := domain.Counts{}
	severity := domain.SeverityOK
	switch state {
	case domain.WorkStateCompleted, domain.WorkStateVerificationPending:
		counts.Done = 1
	case domain.WorkStateRejected:
		counts.Pending = 1
		counts.NeedsAttention = 1
		severity = domain.SeverityWatch
	default:
		counts.Pending = 1
	}
	// A task is a claim pool until the first submit names the operator (the module's own
	// write: assigned_operator_id = coalesce(assigned_operator_id, submitted_by)).
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		ownerState = domain.OwnerStatePool
	}
	return domain.Row{
		Module: domain.ModuleMilk, SourceType: MilkFeedingSourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: feedingDate, DueAt: &due, ClockLabel: due.Format("15:04") + " session",
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: session, Counts: counts,
	}.Finalize(), nil
}

func scanCounts(rows pgx.Rows, what string) (map[domain.WorkState]int, error) {
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("%s scan: %w", what, err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
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
