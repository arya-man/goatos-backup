// Package boardsource is Health's contribution to the cross-module Work Board: one board row
// per treatment session, read from health_treatment_sessions and health_cases (plus
// health_session_steps for the step tiles) and the org tables every module may read. It
// lives INSIDE the health package so the isolation rule holds in both directions: health
// reads only its own tables here, and the board never reads a health table at all.
//
// It reads NO animal table. health_cases snapshots park_id / shed_id / partition_label at
// diagnosis (000098, 000123) and that snapshot is the pen this row shows; the animal itself
// is named only by goat_id, which is a uuid and never copy, so the title names the course
// (disease + day) rather than the animal. Naming the animal here would need the health
// module to snapshot the tag onto the case at diagnosis, the same way 000123 snapshotted the
// partition -- a health decision, not a board one.
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates a completion, a verdict or a hold. The
// mapping from a session status to a board work state is the ONLY business meaning this
// file adds, and it is stated once in workStateSQL.
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

// SourceType is the ref type carried on every health board row.
const SourceType = "health_treatment_session"

// Source implements ports.Source over health's own treatment-session rows.
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

func (s *Source) Module() domain.Module { return domain.ModuleHealth }
func (s *Source) SourceType() string    { return SourceType }

// workStateSQL is the one place a treatment session becomes a board work state.
//
//	scheduled, due_at passed  -> due          (mirrors the module's own worklist, which derives
//	                                           'due' at read time from scheduled + due_at <= now())
//	scheduled                 -> scheduled
//	due                       -> due
//	in_progress               -> in_progress
//	completed                 -> completed    (NOT verification_pending: health verification is
//	                                           post-task evidence review, never a completion gate --
//	                                           the treatment was already given; an approve only
//	                                           stamps verified_by, a reject flips the row to
//	                                           'rework'. 000230 and app/verification_handler.go.)
//	rework                    -> rejected     (bounced; the operator re-does and re-films)
//	held_death_review         -> blocked      (held while the animal's death is reviewed)
//
// `canceled_death` (the animal died) and `canceled` (stopped by a clinical decision, 000230)
// are excluded in the WHERE clause: a session that will never be worked is not work.
const workStateSQL = `CASE
  WHEN s.status = 'scheduled' AND s.due_at <= now() THEN 'due'
  WHEN s.status = 'scheduled' THEN 'scheduled'
  WHEN s.status = 'due' THEN 'due'
  WHEN s.status = 'in_progress' THEN 'in_progress'
  WHEN s.status = 'completed' THEN 'completed'
  WHEN s.status = 'rework' THEN 'rejected'
  WHEN s.status = 'held_death_review' THEN 'blocked'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one business date and one park.
//
// Index: health_sessions_worklist_idx (tenant_id, business_date, due_at, health_session_id)
// -- tenant + business_date are the leading equalities, so one tenant-day of sessions is
// read from the index. The park lives on the CASE (health_cases.park_id, snapshotted at
// diagnosis), reached through the (tenant_id, health_case_id) unique index, and filters
// inside that day slice. A case whose park snapshot is NULL is on no park's board.
//
// A session is a claim pool: nobody is named until completed_by is stamped, so the owner
// lens matches completed_by.
const baseWhere = `
  s.tenant_id = $1::uuid
  AND s.business_date = $3::date
  AND c.park_id = $2::uuid
  AND s.status NOT IN ('canceled_death', 'canceled')
  AND ($4::uuid IS NULL OR s.completed_by = $4::uuid OR s.completed_by IS NULL)`

// projection-review: membership=health_treatment_sessions rows of ONE tenant, park and business_date (canceled_death excluded), one row per session (primary key); group_key=(tenant_id, health_session_id) for the list and the derived board_state for the count; join_cardinality=health_cases joined on its primary key (1:1, the session's own case), step tallies are a correlated aggregate over health_session_steps per returned row (pre-aggregated), locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so no join fans a session out; pagination=keyset on the session id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, business_date and the optional completed_by predicate, repeated verbatim in countSQL.
const listSQL = `
WITH sessions AS (
  SELECT s.health_session_id, s.health_case_id, s.day_no, s.business_date, s.session, s.due_at,
         s.status, s.completed_by,
         c.disease_name, c.park_id, c.shed_id, COALESCE(c.partition_label, '') AS partition_label,
         ` + workStateSQL + ` AS board_state
  FROM health_treatment_sessions s
  JOIN health_cases c ON c.tenant_id = s.tenant_id AND c.health_case_id = s.health_case_id
  WHERE ` + baseWhere + `
    AND ($5::uuid IS NULL OR s.health_session_id > $5::uuid)
)
SELECT i.health_session_id::text, i.health_case_id::text, i.park_id::text, COALESCE(park.name, ''),
       COALESCE(i.shed_id::text, ''), COALESCE(shed.name, ''), i.partition_label,
       i.disease_name, i.day_no, i.business_date::text, i.session, i.due_at, i.status, i.board_state,
       COALESCE(i.completed_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, ''),
       (SELECT count(*) FROM health_session_steps st
         WHERE st.tenant_id = $1::uuid AND st.health_session_id = i.health_session_id AND st.status = 'completed') AS steps_done,
       (SELECT count(*) FROM health_session_steps st
         WHERE st.tenant_id = $1::uuid AND st.health_session_id = i.health_session_id AND st.status <> 'completed') AS steps_pending
FROM sessions i
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = i.park_id
LEFT JOIN locations shed ON shed.tenant_id = $1::uuid AND shed.location_id = i.shed_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = i.completed_by AND m.status = 'active'
WHERE ($6::text[] IS NULL OR i.board_state = ANY($6::text[]))
ORDER BY i.health_session_id
LIMIT $7`

// projection-review: membership=health_treatment_sessions rows of ONE tenant, park and business_date (canceled_death excluded), one row per session (primary key); group_key=(tenant_id, health_session_id) (the count query groups by the SAME derived board_state over the SAME membership); join_cardinality=health_cases joined on its primary key (1:1, the session's own case), step tallies are a correlated aggregate over health_session_steps per returned row (pre-aggregated), locations park/shed on their primary key (1:1) and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1), so no join fans a session out; pagination=keyset on the session id ASC after the cursor with LIMIT, state filter inside WHERE; scope=tenant_id, park_id, business_date and the optional completed_by predicate, repeated verbatim in countSQL.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + workStateSQL + ` AS board_state
  FROM health_treatment_sessions s
  JOIN health_cases c ON c.tenant_id = s.tenant_id AND c.health_case_id = s.health_case_id
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
		return nil, fmt.Errorf("health boardsource list: %w", err)
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
		return nil, fmt.Errorf("health boardsource list rows: %w", err)
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.pool.Query(ctx, countSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates))
	if err != nil {
		return nil, fmt.Errorf("health boardsource count: %w", err)
	}
	defer rows.Close()
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, fmt.Errorf("health boardsource count scan: %w", err)
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

// sessionLabel is the farm word for a session slot.
func sessionLabel(session string) string {
	switch session {
	case "morning":
		return "Morning"
	case "afternoon":
		return "Afternoon"
	case "evening":
		return "Evening"
	case "unscheduled":
		return "Unscheduled"
	}
	return session
}

func scanRow(rows pgx.Rows) (domain.Row, error) {
	var (
		sessionID, caseID, parkID, parkName   string
		shedID, shedName, partitionLabel      string
		diseaseName, businessDate, session    string
		dayNo                                 int
		dueAt                                 time.Time
		status, boardState                    string
		ownerUserID, ownerMemberID, ownerName string
		stepsDone, stepsPending               int
	)
	if err := rows.Scan(&sessionID, &caseID, &parkID, &parkName,
		&shedID, &shedName, &partitionLabel,
		&diseaseName, &dayNo, &businessDate, &session, &dueAt, &status, &boardState,
		&ownerUserID, &ownerMemberID, &ownerName, &stepsDone, &stepsPending); err != nil {
		return domain.Row{}, fmt.Errorf("health boardsource scan: %w", err)
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: parkName, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}
	due := dueAt.In(biztime.DefaultLocation())

	title := diseaseName + " · Day " + strconv.Itoa(dayNo)
	// The subtitle is the session only: the pen is carried in Pen and every surface renders
	// it beside the card, so naming it here showed it twice (phone E2E 2026-09-11).
	subtitle := sessionLabel(session)

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	counts := domain.Counts{Done: stepsDone, Pending: stepsPending}
	if stepsDone+stepsPending == 0 {
		// A session with no materialised steps is still one piece of work.
		if state == domain.WorkStateCompleted {
			counts.Done = 1
		} else {
			counts.Pending = 1
		}
	}
	switch state {
	case domain.WorkStateRejected:
		counts.NeedsAttention = 1
		severity = domain.SeverityWatch
	case domain.WorkStateBlocked:
		counts.NeedsAttention = 1
		severity = domain.SeverityAtRisk
	}
	// A session is a claim pool until completed_by names who did it.
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		ownerState = domain.OwnerStatePool
	}
	return domain.Row{
		Module: domain.ModuleHealth, SourceType: SourceType, SourceID: sessionID,
		ParkID: parkID, ParkName: parkName, Pen: pen,
		BusinessDate: businessDate, DueAt: &due, ClockLabel: due.Format("15:04") + " session",
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle, Counts: counts,
		// Health analytics is the module's web surface; it reads no case parameter, so the
		// link lands on the page rather than on this session.
		Href: "/health/analytics",
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
