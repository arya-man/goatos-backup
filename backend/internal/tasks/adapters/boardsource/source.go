// Package boardsource is the shared tasks engine's contribution to the cross-module Work Board
// (maintainer instruction 2026-09-25: "procurement and toxin testing are not linked to the work
// board ... in future also, any new module should automatically link to the work board").
//
// EVERY workflow the engine runs rows on the board -- a birth, a death, a pen move, a pen return,
// a sale, an animal purchase, a feed purchase, a general SOP run -- from ONE read of the engine's
// own tables, so a module that plugs into the engine (the rule every new operational feature
// follows, docs/decisions/sop-driven-herd-operations.md) is on the board the day it ships with no
// board code of its own. Which board LANE a workflow sits in is the one table below,
// engineModuleLanes; an engine module that table does not name still rows, under Tasks, rather
// than vanishing -- and TestEveryEngineModuleHasABoardLane fails the build until it is named.
//
// It reads workflow_instances and workflow_actions (this module's own tables) through the ONE
// card read the phone's list uses (postgres.BoardCardColumns / BoardCardJoins, whose 1:0..1
// display joins name the animal, the buyer or the load), plus workforce_members for a name.
//
// READ-ONLY and REPORTING-ONLY. Nothing here gates an answer, a completion or a verdict; the
// mapping from a workflow's card fields to a board work state is the ONLY business meaning this
// file adds, stated once in boardStateSQL.
package boardsource

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every engine workflow row, whatever its lane.
const SourceType = "workflow"

// engineModuleLanes is THE mapping from an engine module (workflow_instances.module) to the
// board lane its workflows row under. Herd operations sit with Counts beside their approvals;
// the purchase intakes with Procurement; the sale with Sales; a general SOP run with Tasks.
//
// A new engine module is added to workflow_instances_module_check by its migration; until it is
// named HERE its workflows row under Tasks (the catch-all) and
// TestEveryEngineModuleHasABoardLane fails, so the choice of lane is made on purpose.
var engineModuleLanes = map[string]domain.Module{
	tasksdomain.ModuleBirth:       domain.ModuleCounts,
	tasksdomain.ModuleDeath:       domain.ModuleCounts,
	tasksdomain.ModuleReconcile:   domain.ModuleCounts,
	tasksdomain.ModuleShifting:    domain.ModuleCounts,
	tasksdomain.ModuleProcurement: domain.ModuleProcurement,
	tasksdomain.ModuleSales:       domain.ModuleSales,
	tasksdomain.ModuleGeneral:     domain.ModuleTasks,
}

// catchAllLane is where a workflow of an engine module engineModuleLanes does not name rows.
const catchAllLane = domain.ModuleTasks

// BoardLaneFor is the lane a workflow of engine module m rows under.
func BoardLaneFor(engineModule string) domain.Module {
	if lane, ok := engineModuleLanes[engineModule]; ok {
		return lane
	}
	return catchAllLane
}

// EngineModules returns every engine module the mapping names, sorted.
func EngineModules() []string {
	out := make([]string, 0, len(engineModuleLanes))
	for m := range engineModuleLanes {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// Source implements ports.Source for the engine workflows of ONE board lane.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	lane    domain.Module
	// modules are the engine modules this lane owns.
	modules []string
	// catchAll: this lane also carries every engine module the mapping does not name.
	catchAll bool
	now      func() time.Time
}

// Sources builds one Source per board lane the mapping names. Registering these is the whole of
// what the board needs for every engine workflow, today's and tomorrow's.
func Sources(pool *pgxpool.Pool, timeout time.Duration) []*Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	byLane := map[domain.Module][]string{}
	for engine, lane := range engineModuleLanes {
		byLane[lane] = append(byLane[lane], engine)
	}
	if _, ok := byLane[catchAllLane]; !ok {
		byLane[catchAllLane] = nil
	}
	out := make([]*Source, 0, len(byLane))
	for _, lane := range domain.Modules() {
		engines, ok := byLane[lane]
		if !ok {
			continue
		}
		sort.Strings(engines)
		out = append(out, &Source{
			pool: pool, timeout: timeout, lane: lane, modules: engines,
			catchAll: lane == catchAllLane, now: time.Now,
		})
	}
	return out
}

// WithClock pins the clock "overdue" is judged against (tests).
func (s *Source) WithClock(now func() time.Time) *Source {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Source) Module() domain.Module { return s.lane }
func (s *Source) SourceType() string    { return SourceType }

// knownArg is the full mapped set, bound only on the catch-all lane so it also admits an
// engine module nobody mapped yet.
func (s *Source) knownArg() []string {
	if !s.catchAll {
		return nil
	}
	return EngineModules()
}

// boardStateSQL is the one place a workflow becomes a board work state, from the card fields the
// engine maintains on write (awaiting_verification, next_due_at, actions_done) plus one indexed
// EXISTS for a step sent back:
//
//	completed, nothing with the verifier -> completed
//	awaiting_verification                -> verification_pending (nothing left to record)
//	a step sent back (rework)            -> rejected
//	next step past its due instant       -> overdue
//	some steps done                      -> in_progress
//	otherwise                            -> due
//
// $6 is the server clock, bound so a pinned test clock and production agree.
const boardStateSQL = `CASE
  WHEN wi.state = 'completed' AND NOT wi.awaiting_verification THEN 'completed'
  WHEN wi.awaiting_verification THEN 'verification_pending'
  WHEN EXISTS (SELECT 1 FROM workflow_actions wa
               WHERE wa.workflow_id = wi.workflow_id AND wa.status = 'rework') THEN 'rejected'
  WHEN wi.next_due_at IS NOT NULL AND wi.next_due_at < $6::timestamptz THEN 'overdue'
  WHEN wi.actions_done > 0 THEN 'in_progress'
  ELSE 'due'
END`

// A workflow belongs to day D when it was RAISED on D, when it was raised earlier and is STILL
// OPEN (owed work carries forward until it is done, as a pen visit does), or when it was raised
// earlier and COMPLETED on D. Canceled workflows are nobody's work (state is open | completed |
// canceled, workflow_instances_state_check, so "not canceled" is exactly open or completed).
//
// The three are written ONCE, as disjoint arms, each bounded by its own index (migration 000434):
//
//	raised on D           state IN (open, completed), event_date = D   workflow_instances_board_idx
//	open from before D    state = open, event_date < D                  workflow_instances_board_idx
//	completed on D        state = completed, updated_at in D's IST day  workflow_instances_board_completed_idx
//
// The completed arm compares updated_at to D's IST bounds as a RANGE, never
// (updated_at AT TIME ZONE ...)::date = D: that form cannot use an index, so every completed
// workflow the park ever had was walked and filtered (review of PR #429). The open arm stays
// bounded because open work is what is still owed, not history.
//
// Owner: engine steps are owned by a DESIGNATION, not a person, and whoever holds it may do the
// step; every workflow is therefore a pool row, which the operator lens always includes.
const scopeSQL = `wi.tenant_id = $1::uuid
    AND wi.park_id = $2::uuid
    AND (wi.module = ANY($4::text[]) OR ($5::text[] IS NOT NULL AND NOT (wi.module = ANY($5::text[]))))`

var dayArms = []string{
	`wi.state IN ('open', 'completed') AND wi.event_date = $3::date`,
	`wi.state = 'open' AND wi.event_date < $3::date`,
	`wi.state = 'completed' AND wi.event_date < $3::date
    AND wi.updated_at >= ($3::date::timestamp AT TIME ZONE 'Asia/Kolkata')
    AND wi.updated_at < (($3::date + 1)::timestamp AT TIME ZONE 'Asia/Kolkata')`,
}

// baseWhere is the row predicate for a read already pinned to ONE workflow (the subtask drill):
// the scope and the three arms OR-ed.
func baseWhere() string {
	return "\n  " + scopeSQL + "\n  AND ((" + strings.Join(dayArms, ")\n    OR (") + "))"
}

// dayMembersWhere is the day's membership for the list and the count: the three arms as a
// UNION ALL of workflow ids (disjoint, so nothing repeats), each arm planned on its own index,
// joined back on the primary key. An OR over the arms lets the planner fall back to one walk of
// every completed workflow of the park.
func dayMembersWhere() string {
	arms := make([]string, len(dayArms))
	for i, arm := range dayArms {
		arms[i] = "SELECT wi.workflow_id FROM workflow_instances wi\n    WHERE " + scopeSQL + "\n      AND " + arm
	}
	return "\n  wi.tenant_id = $1::uuid\n  AND wi.workflow_id IN (\n    " + strings.Join(arms, "\n    UNION ALL\n    ") + ")"
}

// extraColumns follow BoardCardColumns: the ids and raw names the board row carries (the card
// composes its own pen display; the board also needs the parts) and the derived state.
const extraColumns = `,
  COALESCE(wi.park_id::text, ''), COALESCE(wi.shed_id::text, ''), COALESCE(shed.name, ''),
  COALESCE(CASE WHEN gsp.shed_id = wi.shed_id AND lower(btrim(gsp.partition_label)) <> 'whole'
                THEN btrim(gsp.partition_label) END, ''),
  w.board_state`

// projection-review: membership=workflow_instances rows of ONE tenant and park whose engine module belongs to this lane (or, on the catch-all lane, to no mapped lane), not canceled, and in day D by the raised / still-open / completed-that-day predicate, one row per workflow (primary key workflow_id); group_key=(tenant_id, workflow_id) for the list and the derived board_state for the count; join_cardinality=the card joins are the phone list's 1:0..1 display enrichments (goats and both locations on their primary keys, the tag LATERAL LIMIT 1, goat_shed_partitions on its (tenant_id, goat_id) primary key, sop_definitions on (tenant_id, code), sales_deals / animal_purchase_loads / feed_purchases on their primary keys gated by template key), the CTE joins back on workflow_id (1:1), and the rework EXISTS never multiplies a row; pagination=keyset on workflow_id ASC after the cursor, state filter and LIMIT applied inside the MATERIALIZED page CTE, so the card joins run for the page's ids only (a primary-key ANY lookup); the day membership is a UNION ALL of three disjoint arms (raised on D, open from before D, completed on D), each bounded by its own day index; scope=tenant_id, park_id, the day predicate and the lane's module set, repeated verbatim in the count query.
func listSQL() string {
	return `
WITH w AS MATERIALIZED (
  SELECT x.workflow_id, x.board_state
  FROM (
    SELECT wi.workflow_id, ` + boardStateSQL + ` AS board_state
    FROM workflow_instances wi
    WHERE ` + dayMembersWhere() + `
      AND ($7::uuid IS NULL OR wi.workflow_id > $7::uuid)
  ) x
  WHERE ($8::text[] IS NULL OR x.board_state = ANY($8::text[]))
  ORDER BY x.workflow_id
  LIMIT $9
)
SELECT ` + taskspg.BoardCardColumns + extraColumns + taskspg.BoardCardJoins + `
JOIN w ON w.workflow_id = wi.workflow_id
WHERE wi.tenant_id = $1::uuid
  -- The card joins start from workflow_instances; the page's ids reach them as a primary-key
  -- lookup, so the card is built for this page only, never for every workflow of the tenant.
  AND wi.workflow_id = ANY(ARRAY(SELECT workflow_id FROM w))
ORDER BY wi.workflow_id
LIMIT $9`
}

// projection-review: membership=the SAME workflow_instances rows as the list (tenant, park, lane module set, not canceled, day predicate); group_key=the derived board_state over that membership; join_cardinality=none (the rework EXISTS never multiplies a row); pagination=none, whole-filter aggregate; scope=repeated verbatim from the list query.
func countSQL() string {
	return `
SELECT board_state, count(*)
FROM (
  SELECT ` + boardStateSQL + ` AS board_state
  FROM workflow_instances wi
  WHERE ` + dayMembersWhere() + `
) x
WHERE ($7::text[] IS NULL OR board_state = ANY($7::text[]))
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
		return nil, fmt.Errorf("workflow boardsource list: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("workflow boardsource list rows: %w", err)
	}
	return out, nil
}

// ListStatement implements ports.BatchSource.
func (s *Source) ListStatement(q ports.SourceQuery, out *[]domain.Row) (ports.Statement, error) {
	if err := ports.CheckUUIDSourceID(q.AfterSourceID); err != nil {
		return ports.Statement{}, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	now := s.now()
	args := []any{q.TenantID, q.ParkID, q.BusinessDate, s.modules, s.knownArg(), now.UTC(),
		nullString(q.AfterSourceID), statesArg(q.WorkStates), limit}
	return ports.Statement{Query: sqlbind.MustBind(listSQL(), args...), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadRows(rows, limit, func(r ports.ResultRows) (domain.Row, error) {
			return s.scanRow(r, q.BusinessDate, now)
		})
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
		return nil, fmt.Errorf("workflow boardsource count: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("workflow boardsource count scan: %w", err)
	}
	return out, nil
}

// CountStatement implements ports.BatchSource.
func (s *Source) CountStatement(q ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	args := []any{q.TenantID, q.ParkID, q.BusinessDate, s.modules, s.knownArg(), s.now().UTC(), statesArg(q.WorkStates)}
	return ports.Statement{Query: sqlbind.MustBind(countSQL(), args...), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadCounts(rows)
		*out = got
		return err
	}}, nil
}

func (s *Source) scanRow(rows ports.ResultRows, businessDate string, now time.Time) (domain.Row, error) {
	var parkID, shedID, shedName, partitionLabel, boardState string
	card, err := taskspg.ScanBoardCard(rows, now, &parkID, &shedID, &shedName, &partitionLabel, &boardState)
	if err != nil {
		return domain.Row{}, fmt.Errorf("workflow boardsource scan: %w", err)
	}
	if oploc.NormalizePartition(partitionLabel) == oploc.WholeSentinel {
		partitionLabel = ""
	}
	loc := oploc.OperationalLocation{ParkID: parkID, ParkName: card.ParkLabel, ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel}
	pen := domain.Pen{ShedID: shedID, ShedName: shedName, PartitionLabel: partitionLabel, Display: loc.Display()}

	state := domain.WorkState(boardState)
	severity := domain.SeverityOK
	switch state {
	case domain.WorkStateOverdue, domain.WorkStateRejected:
		severity = domain.SeverityAtRisk
	}
	counts := domain.Counts{Done: card.ActionsDone, Pending: card.ActionsTotal - card.ActionsDone}
	if counts.Pending < 0 {
		counts.Pending = 0
	}
	if state == domain.WorkStateOverdue || state == domain.WorkStateRejected {
		counts.NeedsAttention = 1
	}
	return domain.Row{
		Module: s.lane, SourceType: SourceType, SourceID: card.WorkflowID,
		ParkID: parkID, ParkName: card.ParkLabel, Pen: pen,
		BusinessDate: businessDate, DueAt: card.NextDueAt, ClockLabel: clockLabel(card, businessDate),
		WorkState: state, Severity: severity,
		OwnerState: domain.OwnerStatePool,
		Title:      Title(card), Subtitle: Subtitle(card),
		Counts: counts,
	}.Finalize(), nil
}

// Title is the workflow's kind and its subject, in the phone card's words: "Feed purchase · Dry
// Masoor Bhusa · Load 355 · CBE", "Sale · Kumar Traders · 12 animals · CBE", "Birth · 3400…".
// A general run is named by its SOP. An animal is named by its tag, else its display id.
func Title(card tasksdomain.WorkflowCard) string {
	kind := tasksdomain.TemplateLabel(card.TemplateKey)
	if _, general := tasksdomain.GeneralSOPCode(card.TemplateKey); general && strings.TrimSpace(card.SOPName) != "" {
		kind = card.SOPName
	}
	if kind == "" {
		kind = "Task"
	}
	subject := card.SubjectLabel
	if subject == "" {
		subject = card.Subject.Tag
	}
	if subject == "" {
		subject = card.Subject.DisplayID
	}
	if subject == "" {
		return kind
	}
	return kind + " · " + subject
}

// Subtitle names what is next and how far along the workflow is: "Kid · Next: Tag the kid ·
// 3 of 8 steps done". A finished workflow reads "All 8 steps done".
func Subtitle(card tasksdomain.WorkflowCard) string {
	parts := []string{}
	if card.Subject.RoleLabel != "" {
		parts = append(parts, card.Subject.RoleLabel)
	}
	if card.State == tasksdomain.WorkflowStateOpen && card.NextAction != nil && strings.TrimSpace(card.NextAction.Title) != "" {
		parts = append(parts, "Next: "+card.NextAction.Title)
	}
	switch {
	case card.ActionsTotal == 0:
	case card.ActionsDone >= card.ActionsTotal:
		parts = append(parts, fmt.Sprintf("All %d steps done", card.ActionsTotal))
	default:
		parts = append(parts, fmt.Sprintf("%d of %d steps done", card.ActionsDone, card.ActionsTotal))
	}
	return strings.Join(parts, " · ")
}

// clockLabel: an open workflow names its next step's due date; one raised on an earlier day and
// still open says since when it is owed.
func clockLabel(card tasksdomain.WorkflowCard, businessDate string) string {
	if card.State != tasksdomain.WorkflowStateOpen {
		return ""
	}
	if card.EventDate != "" && card.EventDate < businessDate {
		return "Raised " + biztime.FarmDateFromBusinessDate(card.EventDate)
	}
	if card.NextDueAt != nil {
		return "Next step due " + biztime.FarmDate(card.NextDueAt.In(biztime.DefaultLocation()))
	}
	return ""
}

func nullString(s string) *string {
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
	for _, st := range states {
		out = append(out, string(st))
	}
	return out
}
