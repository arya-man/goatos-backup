package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// The Colostrum day lens (docs/decisions/colostrum-milk-module.md).
//
// Birth's list read (ListWorkflows) cannot serve this page, for three separate reasons, each of
// which would be a silent wrong answer rather than an error:
//
//	event_date filter          -> a kid born 5 Aug has feeds due 6 Aug; filtering on the BIRTH date
//	                              hides that kid on the 6th
//	actions_total/actions_done -> count EVERY operator action (iodine, weight, standing, tagging),
//	                              so a colostrum card would show the wrong denominator
//	next_action_title/due_at   -> the next action of any section; can be "Tag the kid"
//
// So the lens selects by the date each FEED is due and re-counts at that grain. Maintainer decision
// 2026-08-06: a card shows THAT DAY's feeds only — tomorrow's progress is seen tomorrow.
//
// This is a read-time aggregate, which the scale rules ban by default. It is admitted here because
// it is bounded by construction rather than by hope: the WHERE clause pins one tenant and one IST
// day of colostrum rows, and a day holds (kids born in a 2-day window) x <= 11 feeds — low
// thousands at the 50k-animal release envelope. See the scale-guard annotation on the query below.

// colostrumActionPredicate selects the feed rows. It mirrors domain.IsColostrumAction EXACTLY; the
// two must change together. 1st Colostrum lives in section 'main' (it is part of the delivery
// sequence) but is the first feed of the series the operator sees, so it belongs on this page.
const colostrumActionPredicate = `(wa.section = 'colostrum_session' OR wa.action_key = 'first_colostrum')`

// colostrumDayCTE aggregates one IST business day of feeds to one row per kid.
//
// The next-feed columns use `(array_agg(... ORDER BY seq, action_id) FILTER (...))[1]` rather than a
// join back to workflow_actions on (workflow_id, seq). That is deliberate: seq is unique per
// workflow in practice but is NOT backed by a unique index, so a join on it could silently
// multiply a card if a template ever repeated a seq. An aggregate cannot fan out — it returns
// exactly one row per group by definition.
//
// $1 tenant_id · $2 day start (inclusive) · $3 day end (exclusive)
const colostrumDayCTE = `
WITH day AS (
  SELECT wa.workflow_id,
         count(*)::int                                        AS total,
         count(*) FILTER (WHERE wa.status = 'completed')::int  AS done,
         (array_agg(wa.action_key ORDER BY wa.seq, wa.action_id)
            FILTER (WHERE wa.status <> 'completed'))[1]        AS next_key,
         (array_agg(wa.title ORDER BY wa.seq, wa.action_id)
            FILTER (WHERE wa.status <> 'completed'))[1]        AS next_title,
         (array_agg(wa.due_at ORDER BY wa.seq, wa.action_id)
            FILTER (WHERE wa.status <> 'completed'))[1]        AS next_due_at
  FROM workflow_actions wa
  JOIN workflow_instances w
    ON w.tenant_id = wa.tenant_id AND w.workflow_id = wa.workflow_id
  WHERE wa.tenant_id = $1::uuid
    AND wa.due_at >= $2::timestamptz
    AND wa.due_at <  $3::timestamptz
    AND ` + colostrumActionPredicate + `
    AND wa.status <> 'canceled'
    AND w.state <> 'canceled'
  GROUP BY wa.workflow_id
)`

// colostrumCardColumns mirrors cardSelectColumns POSITION FOR POSITION so scanCard reads both, but
// substitutes the day-scoped values for the four workflow-grain ones:
//
//	module                -> the lens name, so a response is self-describing in logs and clients
//	actions_total/done    -> this day's feeds, not the kid's whole task list
//	next_*                -> this day's next feed, never "Tag the kid"
//	state                 -> the DAY's state; the kid's own workflow state is not this card's subject
//	awaiting_verification -> always false: verification is enqueued at whole-workflow grain, so one
//	                         day's feeds can never sit in that bucket (domain.ColostrumFilterAllowed)
const colostrumCardColumns = `
  wi.workflow_id::text, '` + domain.ModuleColostrum + `'::text, wi.template_key, wi.subject_goat_id::text,
  wi.event_at, wi.event_date::text,
  CASE WHEN c.next_key IS NULL THEN 'completed' ELSE 'open' END,
  c.total, c.done,
  c.next_key, c.next_title, c.next_due_at, false,
  g.display_id, g.row_version, g.sex, COALESCE(g.breed, ''),
  COALESCE(tag.identifier_value, ''),
  COALESCE(park.name, ''), COALESCE(shed.name, '')`

// colostrumOverdueLookbackDays bounds the previous-day attention bell.
//
// Without a lower bound the summary would scan every past day the tenant has ever recorded, which
// is an unbounded read for a five-row popup. Feeds only exist for two days after a birth, so a
// missed feed older than this window is history rather than today's attention; the date picker
// still reaches those days directly.
const colostrumOverdueLookbackDays = 30

// ListColostrumDay serves one keyset page of colostrum cards plus that day's chips and the
// previous-day attention summary.
//
// scale-guard:ignore: bounded read-time aggregate — the day CTE is pinned to one tenant and one
// half-open IST day by workflow_actions_colostrum_day_idx, so it ranges over (kids born in a 2-day
// window) x <= 11 feed rows, low thousands at the documented 5k-50k envelope. It is not a
// god-CTE over raw event history and does not grow with herd age. If measurement at the envelope's
// upper bound says otherwise, the replacement is a (tenant_id, workflow_id, colostrum_date)
// projection maintained in the action-write transaction — not a longer timeout.
//
// projection-review: membership=workflow_actions rows matching colostrumActionPredicate within
// [dayStart, dayEnd) for the tenant, on non-canceled workflows; producer unique key=
// workflow_actions (workflow_id, action_key); consumer card key=(workflow_id, business date) = one
// card per kid per date, guaranteed by GROUP BY wa.workflow_id inside a single-day window;
// join_cardinality=workflow_instances joins the CTE on its primary key workflow_id (1:1), goats on
// its PK (1:1), the identifier LATERAL is LIMIT 1 (1:{0,1}), and both locations join on their PK
// (1:{0,1}) — no join can multiply a card; pagination=chips aggregate the whole day while cards
// keyset on (next_due_at ASC NULLS LAST, workflow_id ASC), so page size never changes chip truth;
// scope=tenant_id + day window + colostrum predicate, IDENTICAL for the chips numerator and
// denominator (both range over the same `day` CTE rows, and the four buckets are mutually exclusive
// by construction: overdue needs next_due_at < now, due is the remaining incomplete cards, and
// completed is exactly next_key IS NULL).
func (r *Repository) ListColostrumDay(ctx context.Context, q domain.ColostrumDayQuery) (domain.WorkflowListPage, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	pageSize := q.PageSize
	if pageSize <= 0 || pageSize > domain.MaxWorkflowPageSize {
		pageSize = domain.MaxWorkflowPageSize
	}
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	dayStart, dayEnd, err := domain.ColostrumDayWindow(q.Date)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	todayDate := q.TodayDate
	if todayDate == "" {
		todayDate = q.Date
	}
	todayStart, _, err := domain.ColostrumDayWindow(todayDate)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}

	var page domain.WorkflowListPage
	if err := r.pool.QueryRow(ctx, colostrumDayCTE+`
SELECT
  count(*)::int,
  count(*) FILTER (WHERE next_due_at IS NOT NULL AND next_due_at < $4::timestamptz)::int,
  count(*) FILTER (WHERE next_key IS NOT NULL AND (next_due_at IS NULL OR next_due_at >= $4::timestamptz))::int,
  count(*) FILTER (WHERE next_key IS NULL)::int
FROM day`,
		q.TenantID, dayStart.UTC(), dayEnd.UTC(), now.UTC()).Scan(
		&page.Chips.All, &page.Chips.Overdue, &page.Chips.Due, &page.Chips.Completed); err != nil {
		return domain.WorkflowListPage{}, err
	}
	// AwaitingVideo stays zero for this lens by contract, not by accident — see
	// domain.ColostrumFilterAllowed.

	overdueDates, err := r.colostrumOverdueDates(ctx, q.TenantID, now, todayStart)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	page.OverdueDates = overdueDates

	// The `now` bind is appended ONLY by the filters that reference it: Postgres cannot infer the
	// type of an unreferenced parameter, and binding it unconditionally fails the All / Completed
	// loads with SQLSTATE 42P18 (the same trap ListWorkflows documents).
	args := []any{q.TenantID, dayStart.UTC(), dayEnd.UTC()}
	filterSQL := ""
	switch q.Filter {
	case "", domain.FilterAll:
	case domain.FilterOverdue:
		args = append(args, now.UTC())
		filterSQL = fmt.Sprintf(` AND c.next_due_at IS NOT NULL AND c.next_due_at < $%d::timestamptz`, len(args))
	case domain.FilterDue:
		args = append(args, now.UTC())
		filterSQL = fmt.Sprintf(` AND c.next_key IS NOT NULL AND (c.next_due_at IS NULL OR c.next_due_at >= $%d::timestamptz)`, len(args))
	case domain.FilterCompleted:
		filterSQL = ` AND c.next_key IS NULL`
	default:
		return domain.WorkflowListPage{}, domain.ErrInvalidCursor
	}

	cursorSQL := ""
	if q.Cursor != nil {
		if q.Cursor.DueIsNull {
			args = append(args, q.Cursor.WorkflowID)
			cursorSQL = fmt.Sprintf(` AND c.next_due_at IS NULL AND wi.workflow_id > $%d::uuid`, len(args))
		} else {
			args = append(args, q.Cursor.NextDueAt.UTC())
			dueArg := len(args)
			args = append(args, q.Cursor.WorkflowID)
			idArg := len(args)
			cursorSQL = fmt.Sprintf(` AND (c.next_due_at IS NULL OR c.next_due_at > $%d::timestamptz OR (c.next_due_at = $%d::timestamptz AND wi.workflow_id > $%d::uuid))`,
				dueArg, dueArg, idArg)
		}
	}
	args = append(args, pageSize+1)

	rows, err := r.pool.Query(ctx, colostrumDayCTE+`
SELECT `+colostrumCardColumns+cardJoins+`
JOIN day c ON c.workflow_id = wi.workflow_id
WHERE wi.tenant_id = $1::uuid`+filterSQL+cursorSQL+`
ORDER BY c.next_due_at ASC NULLS LAST, wi.workflow_id ASC
LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return domain.WorkflowListPage{}, err
	}
	defer rows.Close()

	items := make([]domain.WorkflowCard, 0, pageSize)
	for rows.Next() {
		card, err := scanCard(rows, now)
		if err != nil {
			return domain.WorkflowListPage{}, err
		}
		items = append(items, card)
	}
	if err := rows.Err(); err != nil {
		return domain.WorkflowListPage{}, err
	}
	if len(items) > pageSize {
		items = items[:pageSize]
		last := items[len(items)-1]
		cursor := domain.WorkflowCursor{WorkflowID: last.WorkflowID, DueIsNull: last.NextDueAt == nil}
		if last.NextDueAt != nil {
			cursor.NextDueAt = *last.NextDueAt
		}
		encoded := domain.EncodeWorkflowCursor(cursor)
		page.NextCursor = &encoded
	}
	page.Items = items
	return page, nil
}

// colostrumOverdueDates is the bell: at most the five most recent PAST business dates that still
// hold an unfed colostrum feed whose time has passed.
//
// projection-review: producer=workflow_actions colostrum rows; consumer=one row per business date;
// the count is count(DISTINCT workflow_id), i.e. KID CARDS on that date, matching what tapping the
// date then shows — counting feed rows instead would report 3 for one kid with three missed feeds.
// The join to workflow_instances is on its primary key (1:1) and only filters canceled workflows,
// so it cannot change the count.
func (r *Repository) colostrumOverdueDates(
	ctx context.Context,
	tenantID string,
	now, todayStart time.Time,
) ([]domain.WorkflowOverdueDate, error) {
	// Two upper bounds, deliberately: `due_at < now` means the feed's time has actually passed
	// (overdue), and `due_at < todayStart` restricts the summary to PREVIOUS days. The lookback
	// floor keeps this bounded (see colostrumOverdueLookbackDays).
	rows, err := r.pool.Query(ctx, `
SELECT (wa.due_at AT TIME ZONE 'Asia/Kolkata')::date::text, count(DISTINCT wa.workflow_id)::integer
FROM workflow_actions wa
JOIN workflow_instances w
  ON w.tenant_id = wa.tenant_id AND w.workflow_id = wa.workflow_id
WHERE wa.tenant_id = $1::uuid
  AND wa.due_at >= $2::timestamptz
  AND wa.due_at <  $3::timestamptz
  AND wa.due_at <  $4::timestamptz
  AND `+colostrumActionPredicate+`
  AND wa.status IN ('pending', 'rework')
  AND w.state <> 'canceled'
GROUP BY 1
ORDER BY 1 DESC
LIMIT 5`,
		tenantID,
		todayStart.AddDate(0, 0, -colostrumOverdueLookbackDays).UTC(),
		todayStart.UTC(),
		now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.WorkflowOverdueDate
	for rows.Next() {
		var item domain.WorkflowOverdueDate
		if err := rows.Scan(&item.Date, &item.WorkflowCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
