// Package boardsource is the toxin module's contribution to the cross-module Work Board
// (maintainer instruction 2026-09-25: toxin testing was "not linked to the work board"). Every
// aflatoxin test round a feed load owes rows under Toxin -- "Aflatoxin test · Dry Masoor Bhusa",
// subtitle the load -- on every day from the day it was opened until the day it is signed off.
//
// It reads toxin_test_tasks and toxin_test_step_completions (this module's own tables), the
// module's own authored procedure (the procurement.toxin_test SOP version the round was opened on,
// the same read procedure_source.go makes) for how many working steps the round has, and the org
// table locations for the park: a round is load-grain and carries its park only as the load's
// park code (farm_label), so the park is resolved by that code, never through procurement.
//
// READ-ONLY and REPORTING-ONLY. Nothing here completes a step, gates a wait or casts a verdict;
// the mapping from a round's status to a board work state is the ONLY business meaning this file
// adds, stated once in boardStateSQL.
package boardsource

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	toxindomain "github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every toxin board row.
const SourceType = "toxin_test_task"

// Source implements ports.Source over every live or signed-off toxin round, under Toxin.
type Source struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the one toxin board source.
func New(pool *pgxpool.Pool, timeout time.Duration) *Source {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Source{pool: pool, timeout: timeout}
}

func (s *Source) Module() domain.Module { return domain.ModuleToxin }
func (s *Source) SourceType() string    { return SourceType }

// boardStateSQL is the one place a round becomes a board work state:
//
//	accepted                         -> completed            (CEO/CXO signed the reading off)
//	pending_review                   -> verification_pending (reading submitted, awaiting the verdict)
//	a retest with no step done yet   -> rejected             (the last round came back void or refused)
//	some steps done                  -> in_progress
//	otherwise                        -> due
//
// A cancelled round is never on the board: its retest (minted in the same transaction) is the
// work, and the cancelled round is history, as a withdrawn verification item is.
const boardStateSQL = `CASE
  WHEN t.status = 'accepted' THEN 'completed'
  WHEN t.status = 'pending_review' THEN 'verification_pending'
  WHEN EXISTS (SELECT 1 FROM toxin_test_step_completions c
               WHERE c.tenant_id = t.tenant_id AND c.task_id = t.task_id) THEN 'in_progress'
  WHEN t.origin IN ('invalid_retest', 'rejected_retest') THEN 'rejected'
  ELSE 'due'
END`

// baseWhere binds every read to one tenant, one park (by the park's code) and one business day.
// A round belongs to day D from the IST day it was opened until it is signed off: a live round
// (in_progress / pending_review) on every day since it was opened, an accepted round up to and
// including the day it was accepted. The pool is everyone's: steps are person-independent among
// testers, so every round is a pool row the operator lens includes.
const baseWhere = `
  t.tenant_id = $1::uuid
  AND upper(btrim(t.farm_label)) = (
        SELECT upper(btrim(l.location_code)) FROM locations l
        WHERE l.tenant_id = $1::uuid AND l.location_id = $2::uuid AND l.location_type = 'park')
  AND t.status IN ('in_progress', 'pending_review', 'accepted')
  AND (t.created_at AT TIME ZONE 'Asia/Kolkata')::date <= $3::date
  AND (t.status <> 'accepted' OR (t.reviewed_at AT TIME ZONE 'Asia/Kolkata')::date >= $3::date)`

// projection-review: membership=toxin_test_tasks rows of ONE tenant whose farm_label is the requested park's code, live or accepted (cancelled excluded), in day D by the opened-on-or-before / not-yet-signed-off predicate, one row per round (primary key task_id); group_key=(tenant_id, task_id) for the list and the derived board_state for the count; join_cardinality=the park-code subquery reads ONE locations row by primary key, the completions count is a correlated aggregate (one number per round), the procedure LATERAL is one aggregate row over the round's own sop_version (sop_versions unique per (sop_id, version), sop_definitions unique per (tenant_id, code)), and locations park joins on its primary key; pagination=keyset on task_id ASC after the cursor with LIMIT, state filter applied before the limit; scope=tenant_id, the park code and the day predicate, repeated verbatim in the count query.
const listSQL = `
WITH r AS (
  SELECT t.task_id, t.feed_item_label, t.vendor, t.batch_no, t.quantity_kg, t.round_no, t.origin,
         t.status, t.outcome, t.sop_version, (t.created_at AT TIME ZONE 'Asia/Kolkata')::date::text AS opened_on,
         ` + boardStateSQL + ` AS board_state
  FROM toxin_test_tasks t
  WHERE ` + baseWhere + `
    AND ($4::uuid IS NULL OR t.task_id > $4::uuid)
)
SELECT r.task_id::text, $2::text, COALESCE(park.name, ''),
       r.feed_item_label, r.vendor, r.batch_no, r.quantity_kg::float8, r.round_no, r.origin,
       r.status, COALESCE(r.outcome, ''), r.opened_on, r.board_state,
       (SELECT count(*)::int FROM toxin_test_step_completions c
         WHERE c.tenant_id = $1::uuid AND c.task_id = r.task_id),
       COALESCE(proc.working, 0)
FROM r
LEFT JOIN locations park ON park.tenant_id = $1::uuid AND park.location_id = $2::uuid
LEFT JOIN LATERAL (
  SELECT count(*) FILTER (WHERE st->>'kind' IS DISTINCT FROM 'wait')::int AS working
  FROM sop_versions v
  JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
  CROSS JOIN LATERAL jsonb_array_elements(COALESCE(v.form_dsl->'toxin'->'steps', '[]'::jsonb)) st
  WHERE v.tenant_id = $1::uuid AND d.code = '` + toxindomain.SOPCodeToxinTest + `'
    AND v.version = r.sop_version AND v.status IN ('published', 'retired')
) proc ON true
WHERE ($5::text[] IS NULL OR r.board_state = ANY($5::text[]))
ORDER BY r.task_id
LIMIT $6`

// projection-review: membership=the SAME toxin_test_tasks rows as the list (tenant, park code, live or accepted, day predicate); group_key=the derived board_state over that membership; join_cardinality=none (the completions EXISTS never multiplies a row); pagination=none, whole-filter aggregate; scope=repeated verbatim from the list query.
const countSQL = `
SELECT board_state, count(*)
FROM (
  SELECT ` + boardStateSQL + ` AS board_state
  FROM toxin_test_tasks t
  WHERE ` + baseWhere + `
) x
WHERE ($4::text[] IS NULL OR board_state = ANY($4::text[]))
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
		return nil, fmt.Errorf("toxin boardsource list: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("toxin boardsource list rows: %w", err)
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
	args := []any{q.TenantID, q.ParkID, q.BusinessDate, nullString(q.AfterSourceID), statesArg(q.WorkStates), limit}
	return ports.Statement{Query: sqlbind.MustBind(listSQL, args...), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadRows(rows, limit, func(r ports.ResultRows) (domain.Row, error) {
			return scanRow(r, q.BusinessDate)
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
		return nil, fmt.Errorf("toxin boardsource count: %w", err)
	}
	defer rows.Close()
	if err := st.Read(rows); err != nil {
		return nil, fmt.Errorf("toxin boardsource count scan: %w", err)
	}
	return out, nil
}

// CountStatement implements ports.BatchSource.
func (s *Source) CountStatement(q ports.SourceQuery, out *map[domain.WorkState]int) (ports.Statement, error) {
	return ports.Statement{Query: sqlbind.MustBind(countSQL, q.TenantID, q.ParkID, q.BusinessDate, statesArg(q.WorkStates)), Read: func(rows ports.ResultRows) error {
		got, err := ports.ReadCounts(rows)
		*out = got
		return err
	}}, nil
}

func scanRow(rows ports.ResultRows, businessDate string) (domain.Row, error) {
	var (
		taskID, parkID, parkName, feedItem, vendor, origin, status, outcome, openedOn, boardState string
		batchNo, roundNo, done, working                                                           int
		quantityKg                                                                                float64
	)
	if err := rows.Scan(&taskID, &parkID, &parkName, &feedItem, &vendor, &batchNo, &quantityKg, &roundNo, &origin,
		&status, &outcome, &openedOn, &boardState, &done, &working); err != nil {
		return domain.Row{}, fmt.Errorf("toxin boardsource scan: %w", err)
	}
	if working == 0 {
		// Version 1 of a tenant that never authored the procedure is the seeded document, exactly
		// as procedure_source.ProcedureVersion answers it.
		working = len(toxindomain.SeededProcedure().WorkingSteps())
	}
	state := domain.WorkState(boardState)
	counts := domain.Counts{Done: done, Pending: working - done}
	if counts.Pending < 0 {
		counts.Pending = 0
	}
	severity := domain.SeverityOK
	if status == toxindomain.StatusAccepted && outcome == toxindomain.OutcomePositive {
		// An accepted Positive FLAGS the load (it does not block feeding): the board says so.
		severity = domain.SeverityWatch
	}
	clock := ""
	if status != toxindomain.StatusAccepted && openedOn < businessDate {
		clock = "Owed since " + biztime.FarmDateFromBusinessDate(openedOn)
	}
	return domain.Row{
		Module: domain.ModuleToxin, SourceType: SourceType, SourceID: taskID,
		ParkID: parkID, ParkName: parkName,
		BusinessDate: businessDate, ClockLabel: clock,
		WorkState: state, Severity: severity,
		OwnerState: domain.OwnerStatePool,
		Title:      Title(feedItem),
		Subtitle:   Subtitle(batchNo, vendor, quantityKg, origin, status, outcome),
		Counts:     counts,
	}.Finalize(), nil
}

// Title names the test and the feed it is on.
func Title(feedItem string) string {
	if strings.TrimSpace(feedItem) == "" {
		return "Aflatoxin test"
	}
	return "Aflatoxin test · " + strings.TrimSpace(feedItem)
}

// Subtitle names the load, why this round exists when it is a retest, and the signed-off
// reading once there is one: "Load 355 · Sri Balaji · 2400 kg · Negative".
func Subtitle(batchNo int, vendor string, quantityKg float64, origin, status, outcome string) string {
	parts := []string{}
	if batchNo > 0 {
		parts = append(parts, "Load "+strconv.Itoa(batchNo))
	}
	if v := strings.TrimSpace(vendor); v != "" {
		parts = append(parts, v)
	}
	if quantityKg > 0 {
		parts = append(parts, strconv.FormatFloat(quantityKg, 'f', -1, 64)+" kg")
	}
	if line := toxindomain.OriginLine(origin); line != "" {
		parts = append(parts, line)
	}
	if status == toxindomain.StatusAccepted {
		if label := toxindomain.OutcomeLabel(outcome); label != "" {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, " · ")
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
