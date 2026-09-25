package boardsource

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// MilkPreparationSourceType is the ref type carried on every milk preparation board row.
const MilkPreparationSourceType = "milk_preparation"

// MilkPreparationSource implements ports.Source over milk_preparation_completions: ONE card per
// park per preparation day, the day BEFORE the feeding it prepares for.
//
// Preparation and feeding are completely different tasks (maintainer, 2026-09-25): a different
// day, a different submit, a different proof and its own verifier queue (milk_preparation). The
// feeding card used to carry a "Prepare" step it had no fact for, while the preparation itself
// -- whose proofs the Verification lane leaves to the Milk cards -- appeared nowhere.
//
// A preparation row exists only once someone submits. Before that the card is OWED when the park
// has at least one kid the Milk Preparation page prepares for on that date -- the page's own
// countspg.MilkPreparationKidPredicate, so card and page cannot disagree. A submitted preparation
// keeps its card whatever the herd says now.
type MilkPreparationSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewMilkPreparation constructs the milk preparation source.
func NewMilkPreparation(pool *pgxpool.Pool, timeout time.Duration) *MilkPreparationSource {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &MilkPreparationSource{pool: pool, timeout: timeout}
}

func (s *MilkPreparationSource) Module() domain.Module { return domain.ModuleMilk }
func (s *MilkPreparationSource) SourceType() string    { return MilkPreparationSourceType }

// milkPrepWorkStateSQL is the one place a park's preparation becomes a board work state.
//
//	no submission yet    -> due
//	pending_verification -> verification_pending
//	completed            -> completed
//	rework               -> rejected
const milkPrepWorkStateSQL = `CASE u.status
  WHEN 'pending_verification' THEN 'verification_pending'
  WHEN 'completed' THEN 'completed'
  WHEN 'rework' THEN 'rejected'
  ELSE 'due'
END`

// milkPrepUnitSQL is the park's ONE preparation unit on $3: its live completion (unique per park
// and day, milk_preparation_completions_farm_day_uidx) or, when there is none yet, the owed card
// -- present only when the park has a kid the page prepares for. Binds $1 tenant, $2 park, $3 the
// preparation date; the kid predicate reads $3 as that date.
var milkPrepUnitSQL = `
unit AS (
  SELECT $2::uuid AS park_id, c.completion_id, c.status, c.submitted_by, c.submitted_at,
         COALESCE(c.rework_reason, '') AS rework_reason
  FROM (SELECT 1) one
  LEFT JOIN milk_preparation_completions c
    ON c.tenant_id = $1::uuid AND c.park_id = $2::uuid AND c.preparation_date = $3::date
   AND c.status <> 'retired' AND c.shed_id IS NULL
  WHERE c.completion_id IS NOT NULL
     OR EXISTS (SELECT 1 FROM goats g
                 WHERE g.tenant_id = $1::uuid AND g.park_id = $2::uuid
                   AND ` + countspg.MilkPreparationKidPredicate + `)
)`

// projection-review: membership=at most ONE unit per (tenant, park, preparation_date): the live milk_preparation_completions row (unique by milk_preparation_completions_farm_day_uidx on (tenant_id, park_id, preparation_date) WHERE status <> 'retired') or, with none, the owed card when an EXISTS over goats finds one kid the page prepares for; group_key=(tenant_id, park_id) -- the source id IS the park id -- and the derived board_state for the count; join_cardinality=the completion LEFT JOIN is 0..1 by that unique index, the goats read is an EXISTS (never a join, so the herd cannot fan the unit out), locations park on its primary key (1:1) and workforce_members filtered to status='active' on the partial-unique (tenant_id,user_id) index (at most 1); pagination=keyset on park_id ASC after $5 with LIMIT $7 over the single unit, state filter inside WHERE; scope=tenant_id, park_id, preparation_date and the optional submitter predicate, repeated verbatim in milkPrepCountSQL.
var milkPrepListSQL = `
WITH ` + milkPrepUnitSQL + `,
rows AS (
  SELECT u.*, ` + milkPrepWorkStateSQL + ` AS board_state
  FROM unit u
  WHERE ($4::uuid IS NULL OR u.submitted_by = $4::uuid OR u.submitted_by IS NULL)
    AND ($5::uuid IS NULL OR u.park_id > $5::uuid)
)
SELECT r.park_id::text, COALESCE(park.name, ''), COALESCE(r.completion_id::text, ''),
       r.board_state, COALESCE(r.submitted_by::text, ''),
       COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM rows r
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = r.park_id
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = r.submitted_by AND m.status = 'active'
WHERE ($6::text[] IS NULL OR r.board_state = ANY($6::text[]))
ORDER BY r.park_id
LIMIT $7`

// projection-review: membership=at most ONE unit per (tenant, park, preparation_date), the same unit milkPrepListSQL reads; group_key=(tenant_id, park_id) grouped by the SAME derived board_state; join_cardinality=the completion LEFT JOIN is 0..1 by milk_preparation_completions_farm_day_uidx and the goats read is an EXISTS, so the count is 0 or 1; pagination=none, a count; scope=tenant_id, park_id, preparation_date and the optional submitter predicate, verbatim from milkPrepListSQL.
var milkPrepCountSQL = `
WITH ` + milkPrepUnitSQL + `
SELECT board_state, count(*)
FROM (
  SELECT ` + milkPrepWorkStateSQL + ` AS board_state
  FROM unit u
  WHERE ($4::uuid IS NULL OR u.submitted_by = $4::uuid OR u.submitted_by IS NULL)
) x
WHERE ($5::text[] IS NULL OR board_state = ANY($5::text[]))
GROUP BY board_state`

// ListRows implements ports.Source.
func (s *MilkPreparationSource) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
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
		return nil, fmt.Errorf("milk preparation boardsource list: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("milk preparation boardsource list rows: %w", err)
	}
	return out, nil
}

// ListStatement implements ports.BatchSource: the exact statement and decoding ListRows runs.
func (s *MilkPreparationSource) ListStatement(q ports.SourceQuery, out *[]domain.Row) (ports.Statement, error) {
	if err := ports.CheckUUIDSourceID(q.AfterSourceID); err != nil {
		return ports.Statement{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	date := q.BusinessDate
	return ports.Statement{
		Query: sqlbind.MustBind(milkPrepListSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), nullUUID(q.AfterSourceID), statesArg(q.WorkStates), limit),
		Read: func(rows ports.ResultRows) error {
			got, err := ports.ReadRows(rows, limit, func(r ports.ResultRows) (domain.Row, error) { return scanMilkPrepRow(r, date) })
			*out = got
			return err
		},
	}, nil
}

// CountByState implements ports.Source.
func (s *MilkPreparationSource) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
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
		return nil, fmt.Errorf("milk preparation boardsource count: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("milk preparation boardsource count scan: %w", err)
	}
	return out, nil
}

// CountStatement implements ports.BatchSource: the exact statement CountByState runs.
func (s *MilkPreparationSource) CountStatement(q ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	return ports.Statement{Query: sqlbind.MustBind(milkPrepCountSQL, q.TenantID, q.ParkID, q.BusinessDate, nullUUID(q.OwnerUserID), statesArg(q.WorkStates)), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadCounts(rows)
		*out = got
		return err
	}}, nil
}

// feedingDayLabel names the day the preparation is for: the preparation date + 1, the
// module's own CHECK (feeding_date = preparation_date + 1).
func feedingDayLabel(preparationDate string) string {
	d, err := time.ParseInLocation("2006-01-02", preparationDate, biztime.DefaultLocation())
	if err != nil {
		return ""
	}
	return biztime.FarmDateFromBusinessDate(d.AddDate(0, 0, 1).Format("2006-01-02"))
}

func scanMilkPrepRow(rows ports.ResultRows, date string) (domain.Row, error) {
	var parkID, parkName, completionID, boardState, ownerUserID, ownerMemberID, ownerName string
	if err := rows.Scan(&parkID, &parkName, &completionID, &boardState, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Row{}, fmt.Errorf("milk preparation boardsource scan: %w", err)
	}
	state := domain.WorkState(boardState)
	counts := domain.Counts{}
	severity := domain.SeverityOK
	switch state {
	case domain.WorkStateCompleted, domain.WorkStateVerificationPending:
		counts.Done = 1
	case domain.WorkStateRejected:
		counts.Pending, counts.NeedsAttention = 1, 1
		severity = domain.SeverityWatch
	default:
		counts.Pending = 1
	}
	title := "Milk preparation"
	if parkName != "" {
		title += " · " + parkName
	}
	owner := domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName}
	ownerState := domain.OwnerStateAssigned
	if ownerUserID == "" {
		// Anyone on the milk crew may prepare; the first submit names who did.
		ownerState = domain.OwnerStatePool
	}
	forDay := feedingDayLabel(date)
	return domain.Row{
		Module: domain.ModuleMilk, SourceType: MilkPreparationSourceType, SourceID: parkID,
		ParkID: parkID, ParkName: parkName,
		BusinessDate: date, ClockLabel: "For feeding on " + forDay,
		WorkState: state, Severity: severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: "Tomorrow's milk", Counts: counts,
		Href: "/counts/milk-preparation?mp_park=" + url.QueryEscape(parkID),
	}.Finalize(), nil
}

// projection-review: membership=the ONE preparation unit of (tenant, park, preparation_date), the same unit the row read served (source id = park id, so $2 and the row's source id agree); group_key=(tenant_id, park_id) as one subtask; join_cardinality=the completion LEFT JOIN is 0..1 by milk_preparation_completions_farm_day_uidx, goats is an EXISTS, workforce_members at most 1 on the partial-unique active index, so the total is 1 or 0; pagination=keyset on (rank, park id) ASC after ($4, $5) with LIMIT $6 over the single unit; scope=tenant_id, park_id and preparation_date.
var milkPrepSubtasksSQL = `
WITH ` + milkPrepUnitSQL + `,
ranked AS (
  SELECT u.*, CASE u.status WHEN 'rework' THEN 0 WHEN 'pending_verification' THEN 3 WHEN 'completed' THEN 4 ELSE 1 END AS rank
  FROM unit u
)
SELECT r.park_id::text, COALESCE(r.status, ''), r.rework_reason, r.submitted_at, r.rank,
       COALESCE(r.submitted_by::text, ''), COALESCE(m.workforce_member_id::text, ''), COALESCE(m.display_name, '')
FROM ranked r
LEFT JOIN workforce_members m
  ON m.tenant_id = $1::uuid AND m.user_id = r.submitted_by AND m.status = 'active'
WHERE (r.rank, r.park_id::text) > ($4::int, $5::text)
ORDER BY r.rank, r.park_id
LIMIT $6`

// ListSubtasks implements ports.SubtaskSource: the preparation itself, prepare -> verify.
func (s *MilkPreparationSource) ListSubtasks(ctx context.Context, q ports.SubtaskQuery) (domain.SubtaskPage, error) {
	if q.SourceID != q.ParkID {
		// The source id IS the park: a drill that names another park is not this card.
		return domain.SubtaskPage{Subtasks: []domain.Subtask{}}, nil
	}
	afterRank, afterID, err := domain.ParseSubtaskKey(q.AfterKey)
	if err != nil {
		return domain.SubtaskPage{}, err
	}
	if q.AfterKey == "" {
		afterRank = -1
	}
	limit := domain.BoundSubtaskLimit(q.Limit)
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	bound := sqlbind.MustBind(milkPrepSubtasksSQL, q.TenantID, q.ParkID, q.BusinessDate, afterRank, afterID, limit+1)
	rows, err := s.pool.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("milk preparation boardsource subtasks: %w", err)
	}
	defer rows.Close()
	page := domain.SubtaskPage{Subtasks: []domain.Subtask{}}
	for rows.Next() {
		st, err := scanMilkPrepSubtask(rows, q.BusinessDate)
		if err != nil {
			return domain.SubtaskPage{}, err
		}
		page.Subtasks = append(page.Subtasks, st)
	}
	if err := rows.Err(); err != nil {
		return domain.SubtaskPage{}, fmt.Errorf("milk preparation boardsource subtasks rows: %w", err)
	}
	page.Total = len(page.Subtasks)
	return page, nil
}

func scanMilkPrepSubtask(rows pgx.Rows, date string) (domain.Subtask, error) {
	var (
		parkID, status, reason                string
		submittedAt                           *time.Time
		rank                                  int
		ownerUserID, ownerMemberID, ownerName string
	)
	if err := rows.Scan(&parkID, &status, &reason, &submittedAt, &rank, &ownerUserID, &ownerMemberID, &ownerName); err != nil {
		return domain.Subtask{}, fmt.Errorf("milk preparation boardsource subtask scan: %w", err)
	}
	prepare := domain.Step{Name: "Prepare", State: domain.StepTodo}
	verify := domain.Step{Name: "Verify", State: domain.StepLocked}
	state := domain.WorkStateDue
	attention := false
	switch status {
	case "pending_verification":
		prepare.State, verify.State = domain.StepDone, domain.StepInReview
		state = domain.WorkStateVerificationPending
	case "completed":
		prepare.State, verify.State = domain.StepDone, domain.StepDone
		state = domain.WorkStateCompleted
	case "rework":
		prepare.State = domain.StepDone
		verify.State, verify.Detail = domain.StepRework, reason
		state, attention = domain.WorkStateRejected, true
	}
	if submittedAt != nil && prepare.State == domain.StepDone {
		prepare.Detail = "Submitted " + submittedAt.In(biztime.DefaultLocation()).Format("15:04")
	}
	return domain.Subtask{
		Key: domain.SubtaskKey(rank, parkID), Name: "Milk preparation",
		Subtitle:  "For feeding on " + feedingDayLabel(date),
		WorkState: state, NeedsAttention: attention,
		Owner: domain.Owner{UserID: ownerUserID, WorkforceMemberID: ownerMemberID, Name: ownerName},
		Steps: []domain.Step{prepare, verify},
	}.Finalize(), nil
}
