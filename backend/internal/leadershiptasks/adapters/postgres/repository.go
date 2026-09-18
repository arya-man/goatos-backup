// Package postgres persists leadership tasks: the per-tenant numbered brief one authorized
// worker raises for another, its attachments (pointers into the proof store), the seen stamp
// the drawer badge counts, and the status ladder. Every write is idempotent, audited and
// announced through the outbox in ONE transaction.
package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

const (
	idemScopeRaise   = "leadership_task.raise"
	idemScopeEdit    = "leadership_task.edit"
	idemScopeStatus  = "leadership_task.status"
	idemScopeComment = "leadership_task.comment"

	resourceType = "leadership_task"
)

// Repository is the module's Postgres adapter.
type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	now     func() time.Time
}

// NewRepository wires the adapter over the shared pool.
func NewRepository(pool *pgxpool.Pool, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Repository{pool: pool, timeout: timeout, now: time.Now}
}

// WithClock pins the clock, for tests.
func (r *Repository) WithClock(now func() time.Time) *Repository {
	r.now = now
	return r
}

var _ ports.Repository = (*Repository)(nil)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// taskColumns is the single projection every task read uses. Column order here and in
// scanTask must move together. Names are resolved in the SAME query (two LEFT JOINs on
// the active roster row, never a per-row lookup); a person who does not resolve reads as
// ” so the screen omits the attribution rather than showing a uuid.
const taskColumns = sqlRepository1

const taskFrom = sqlRepository2

func scanTask(row pgx.Row) (domain.Task, error) {
	var t domain.Task
	err := row.Scan(
		&t.TaskID, &t.TenantID, &t.TaskNo, &t.Title, &t.Body, &t.Status,
		&t.RaisedByUserID, &t.RaisedByName, &t.RaisedByDesignation,
		&t.AssigneeUserID, &t.AssigneeName,
		&t.RaisedAt, &t.DeadlineAt, &t.UpdatedAt, &t.DoneAt, &t.CancelledAt, &t.SeenAt, &t.AssigneeComment, &t.RowVersion,
	)
	if err != nil {
		return domain.Task{}, err
	}
	t.RaisedAt = t.RaisedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	if t.DeadlineAt != nil {
		utc := t.DeadlineAt.UTC()
		t.DeadlineAt = &utc
	}
	return t, nil
}

// ListAssignees lists every person a task may be raised for: an ACTIVE roster member whose
// own mobile ticks carry the Tasks module at Oversee (maintainer decision 2026-09-04: "keep it
// optional -- if they are selected there, only for them") AND who holds an active leadership
// grant -- CEO/CXO, a park head or a director (maintainer request 2026-09-11: the picker was
// listing every ticked worker; it must show only CXOs, park heads and directors). The tick
// still opts a leader in or out on /people; the role grant keeps the rank and file off the list.
func (r *Repository) ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, sqlListAssignees, tenantID, permissions.SurfaceMobile, leadershipTasksModuleKey, permissions.LevelOversee, assignableLeadershipRoles)
	if err != nil {
		return nil, fmt.Errorf("leadership task: list assignees: %w", err)
	}
	defer rows.Close()
	out := make([]ports.Assignee, 0, 8)
	for rows.Next() {
		var a ports.Assignee
		if err := rows.Scan(&a.UserID, &a.Name, &a.Title); err != nil {
			return nil, fmt.Errorf("leadership task: scan assignee: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListTasks pages the caller's tasks by keyset in the requested sort order, applies the
// request's filters, and batch-loads each page row's attachments in ONE query.
//
// projection-review: membership=leadership_tasks at its task_id key (one row per task) filtered by the party predicate raised_by = $user OR assignee_user_id = $user PLUS the request filters (free text over title/body/task_no, assignee, raiser, deadline range, raise range), which the page rows, the status counts AND the scope counts all share so a chip never advertises a row the list hides; group_key=status for the chip counts and scope for the tab counts, each over that same filter set minus only the one predicate the chip itself varies (status for filters[], party for scopes[]); join_cardinality=leadership_task_attachments is at most 12 rows per task and is fetched as a second bounded query keyed by the page's task ids (never multiplied into the page query), and the two workforce_members name LEFT JOINs are 1:1 on workforce_members_active_user_unique_idx; pagination=keyset on (raised_at, task_id) or on (deadline_at IS NULL, deadline_at, task_id) per the active sort -- NULL deadlines sort LAST in both directions so the order stays total -- with counts computed whole-list never page-local and NO OFFSET anywhere; scope=tenant_id on every branch, party predicate on every read
func (r *Repository) ListTasks(ctx context.Context, p ports.ListParams) (ports.Page, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	sortKey := ports.SortOrDefault(p.Sort)
	if !ports.IsSortKey(sortKey) {
		return ports.Page{}, fmt.Errorf("%w: unknown sort %q", ports.ErrInvalidArgument, sortKey)
	}

	// Bind only what the predicate reads. Team progress is tenant-wide and names no party, so
	// the user id must NOT be bound for it: pgx counts placeholders, and an unused $2 with no
	// later $3 is "expected 1 arguments, got 2" -- which is exactly the unfiltered web read
	// (CXO, scope_mode=company). EVERY predicate below this switch takes its placeholder
	// number from bindArg (len(args) after the append), so no filter, sort or cursor can
	// disturb the per-scope $2 rule however many of them the request carries.
	args := []any{p.TenantID}
	where := "t.tenant_id = $1"
	switch p.Scope {
	case domain.ScopeAssignedByMe:
		args = append(args, p.UserID)
		where += " AND t.raised_by = $2::uuid"
	case domain.ScopeTeamProgress:
		where += " AND t.status <> 'cancelled'"
	default:
		args = append(args, p.UserID)
		where += " AND t.assignee_user_id = $2::uuid"
	}
	if len(p.Statuses) > 0 {
		where += fmt.Sprintf(" AND t.status = ANY($%d)", bindArg(&args, p.Statuses))
	}
	if p.OverdueBefore != nil {
		// The overdue lens: still-working tasks (Statuses carries open/in_progress) whose
		// deadline has passed. Strict `<` so a task due exactly now is not late yet.
		where += fmt.Sprintf(" AND t.deadline_at < $%d::timestamptz", bindArg(&args, *p.OverdueBefore))
	}
	where += filterPredicates(&args, "t.", p, true, true)
	if p.Cursor != "" {
		frag, err := keysetPredicate(&args, sortKey, p.Cursor)
		if err != nil {
			return ports.Page{}, err
		}
		where += frag
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 20
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY %s LIMIT %d`, taskColumns, taskFrom, where, orderByForSort(sortKey), limit+1)
	// The chip counts are SEPARATE from the row query, so they must be built from the SAME
	// filters as the rows or the chips advertise rows the list hides (ports/repository.go:49-51).
	// They are all answered in ONE statement (see sqlListAggregates); the bind list starts fresh
	// from the tenant and every predicate is numbered by bindArg, so the filters cannot collide
	// with the party placeholder here either.
	//
	// The page rows and the aggregates do not depend on each other, so they travel in ONE
	// pipelined batch: on the ~40ms-per-round-trip tunnel this page runs on, the two sequential
	// reads they replaced were a third of the list's p90.
	countArgs := []any{p.TenantID}
	aggSQL := sqlListAggregates(&countArgs, p)
	batch := &pgx.Batch{}
	batch.Queue(query, args...)
	batch.Queue(aggSQL, countArgs...)
	results := r.pool.SendBatch(ctx, batch)

	rows, err := results.Query()
	if err != nil {
		results.Close()
		return ports.Page{}, fmt.Errorf("leadership task: list: %w", err)
	}
	tasks := make([]domain.Task, 0, limit)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			rows.Close()
			results.Close()
			return ports.Page{}, fmt.Errorf("leadership task: list scan: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		results.Close()
		return ports.Page{}, fmt.Errorf("leadership task: list rows: %w", err)
	}
	rows.Close()

	page := ports.Page{StatusCounts: map[string]int{}, ScopeCounts: map[string]int{}, ScopeTotals: map[string]int{}}
	aggRows, err := results.Query()
	if err != nil {
		results.Close()
		return ports.Page{}, fmt.Errorf("leadership task: list aggregates: %w", err)
	}
	for aggRows.Next() {
		var kind, key string
		var n int
		if err := aggRows.Scan(&kind, &key, &n); err != nil {
			aggRows.Close()
			results.Close()
			return ports.Page{}, fmt.Errorf("leadership task: list aggregates scan: %w", err)
		}
		switch kind {
		case "status":
			page.StatusCounts[key] = n
		case "scope":
			page.ScopeCounts[key] = n
		case "scope_total":
			page.ScopeTotals[key] = n
		case "unseen":
			page.UnseenCount = n
		case "overdue":
			page.OverdueCount = n
		}
	}
	if err := aggRows.Err(); err != nil {
		aggRows.Close()
		results.Close()
		return ports.Page{}, err
	}
	aggRows.Close()
	if err := results.Close(); err != nil {
		return ports.Page{}, fmt.Errorf("leadership task: list batch: %w", err)
	}

	if len(tasks) > limit {
		last := tasks[limit-1]
		page.NextCursor = encodeSortCursor(sortKey, last)
		tasks = tasks[:limit]
	}
	if err := r.enrichPage(ctx, p.TenantID, tasks); err != nil {
		return ports.Page{}, err
	}
	page.Rows = tasks
	return page, nil
}

// bindArg appends one bind value and returns ITS placeholder number. Every predicate added
// after the per-scope switch numbers itself this way: the scope branch binds 1 arg for
// team_progress and 2 for the party scopes, and a hand-counted $3 would be wrong for one of
// them (the bug TestTeamProgressUnfilteredReadBindsOnlyWhatItReads pins).
func bindArg(args *[]any, v any) int {
	*args = append(*args, v)
	return len(*args)
}

// filterPredicates renders the request filters shared by the row query, the status counts and
// the scope counts. alias is "t." for the aliased row query and "" for the bare count queries.
// withAssignee/withRaiser are false where the surrounding query already pins that person, so
// one person is never filtered twice with two different ids (which would always be zero rows).
func filterPredicates(args *[]any, alias string, p ports.ListParams, withAssignee, withRaiser bool) string {
	var b strings.Builder
	if p.Query != "" {
		// ILIKE on the bare column (never lower(col) LIKE '%..%', which is the non-SARGable
		// form the scale guard bans): the pg_trgm GIN indexes from migration 000345 serve this
		// leading wildcard on both title and body, escaped patterns included.
		//
		// The typed text is ESCAPED, so a leader searching for "50%" or "shed_4" matches those
		// characters LITERALLY. Unescaped, `%` and `_` are ILIKE wildcards, and a search that
		// silently matches more than the words typed reads as a broken box rather than a
		// feature. Only the two wildcards this query adds itself stay live.
		text := bindArg(args, escapeLikePattern(p.Query))
		arms := fmt.Sprintf("%stitle ILIKE '%%' || $%d || '%%' ESCAPE '\\' OR %sbody ILIKE '%%' || $%d || '%%' ESCAPE '\\'", alias, text, alias, text)
		if p.QueryTaskNo != nil {
			// "15" is how a leader names task #15 out loud, so a bare integer also matches the
			// farm's own number exactly -- served by leadership_tasks_no_uq.
			arms += fmt.Sprintf(" OR %stask_no = $%d", alias, bindArg(args, *p.QueryTaskNo))
		}
		b.WriteString(" AND (" + arms + ")")
	}
	// A person filter is one uuid or a comma-separated list (the toolbar's checkboxes); the
	// app layer validated every entry, so splitting here is safe. `= ANY(uuid[])` keeps the
	// single-person plan on the same index.
	if withAssignee && p.AssigneeUserID != "" {
		b.WriteString(fmt.Sprintf(" AND %sassignee_user_id = ANY($%d::uuid[])", alias, bindArg(args, strings.Split(p.AssigneeUserID, ","))))
	}
	if withRaiser && p.RaisedBy != "" {
		b.WriteString(fmt.Sprintf(" AND %sraised_by = ANY($%d::uuid[])", alias, bindArg(args, strings.Split(p.RaisedBy, ","))))
	}
	// Both ends of a range are required together upstream, so a half-range never reaches here.
	// A deadline range therefore also excludes every task that has no deadline at all.
	if p.DeadlineFrom != nil && p.DeadlineTo != nil {
		b.WriteString(fmt.Sprintf(" AND %sdeadline_at >= $%d::timestamptz AND %sdeadline_at <= $%d::timestamptz",
			alias, bindArg(args, *p.DeadlineFrom), alias, bindArg(args, *p.DeadlineTo)))
	}
	if p.RaisedFrom != nil && p.RaisedTo != nil {
		b.WriteString(fmt.Sprintf(" AND %sraised_at >= $%d::timestamptz AND %sraised_at <= $%d::timestamptz",
			alias, bindArg(args, *p.RaisedFrom), alias, bindArg(args, *p.RaisedTo)))
	}
	return b.String()
}

// escapeLikePattern neutralizes the ILIKE metacharacters in text a person typed, so the
// pattern matches those characters literally. The escape character itself goes first: escaping
// it after the wildcards would re-escape the backslashes this function just added.
func escapeLikePattern(text string) string {
	text = strings.ReplaceAll(text, `\`, `\\`)
	text = strings.ReplaceAll(text, "%", `\%`)
	return strings.ReplaceAll(text, "_", `\_`)
}

// orderByForSort is the total order for one sort key. A NULL deadline sorts LAST in BOTH
// deadline directions -- a task with no deadline is not "the most urgent" and not "the least
// urgent", it is simply not on the deadline list -- and task_id breaks every tie so the
// keyset below can never skip or repeat a row.
//
// The null rule is written as NULLS LAST rather than as a leading `(deadline_at IS NULL)`
// term deliberately: the two mean the same thing, but only this form is servable by a btree,
// and EXPLAIN on 40k tenant rows turns the leading-expression form into a Seq Scan plus a
// top-N sort of the whole tenant while this form walks the matching deadline index from
// 000345 in order, with no Sort node at all. It is an ordinary Index Scan and not an Index
// ONLY Scan -- taskColumns projects 19 columns, so each ordered row still costs a heap
// fetch -- and the property worth having is the absent sort, not the absent heap access.
func orderByForSort(sortKey string) string {
	switch sortKey {
	case ports.SortRaisedAtAsc:
		return "t.raised_at ASC, t.task_id ASC"
	case ports.SortDeadlineAsc:
		return "t.deadline_at ASC NULLS LAST, t.task_id ASC"
	case ports.SortDeadlineDesc:
		return "t.deadline_at DESC NULLS LAST, t.task_id DESC"
	default:
		return "t.raised_at DESC, t.task_id DESC"
	}
}

// keysetPredicate turns a cursor into the "strictly after the last row of the previous page"
// predicate for the ACTIVE sort. The cursor carries the sort it was minted under; a mismatch
// is refused rather than served, because the same instant means a different position in a
// different order and the caller would silently read a wrong page.
func keysetPredicate(args *[]any, sortKey, cursor string) (string, error) {
	c, err := decodeSortCursor(sortKey, cursor)
	if err != nil {
		return "", err
	}
	switch sortKey {
	case ports.SortRaisedAtAsc:
		return fmt.Sprintf(" AND (t.raised_at, t.task_id) > ($%d::timestamptz, $%d::uuid)",
			bindArg(args, c.value), bindArg(args, c.taskID)), nil
	case ports.SortDeadlineAsc:
		if c.valueNull {
			// Already inside the trailing NULL-deadline block: stay in it and walk task_id.
			return fmt.Sprintf(" AND t.deadline_at IS NULL AND t.task_id > $%d::uuid", bindArg(args, c.taskID)), nil
		}
		return fmt.Sprintf(" AND (t.deadline_at IS NULL OR (t.deadline_at, t.task_id) > ($%d::timestamptz, $%d::uuid))",
			bindArg(args, c.value), bindArg(args, c.taskID)), nil
	case ports.SortDeadlineDesc:
		if c.valueNull {
			return fmt.Sprintf(" AND t.deadline_at IS NULL AND t.task_id < $%d::uuid", bindArg(args, c.taskID)), nil
		}
		return fmt.Sprintf(" AND (t.deadline_at IS NULL OR (t.deadline_at, t.task_id) < ($%d::timestamptz, $%d::uuid))",
			bindArg(args, c.value), bindArg(args, c.taskID)), nil
	default:
		return fmt.Sprintf(" AND (t.raised_at, t.task_id) < ($%d::timestamptz, $%d::uuid)",
			bindArg(args, c.value), bindArg(args, c.taskID)), nil
	}
}

// statusCountsWhere is the filters[] count predicate: whole-list, over the SAME scope and the
// SAME request filters as the rows, MINUS the status predicate the chips themselves vary. It is
// a WHERE clause rather than a whole query because its one caller, sqlListAggregates, inlines it
// as one arm of the single aggregate statement; the clause text is unchanged from when this was
// its own SELECT.
//
// projection-review: membership=leadership_tasks for tenant plus the active scope's party predicate and the request filters; group_key=status; join_cardinality=no joins; pagination=whole-result summary independent of the task page, no OFFSET; scope=tenant plus actor party (tenant-wide for the monitor scope)
func statusCountsWhere(args *[]any, p ports.ListParams) string {
	// DELIBERATE FOR NOW, and out of scope for the worklist change: this query drops the status
	// predicate entirely, including the team_progress row query's own `status <> 'cancelled'`,
	// so the monitor scope still reports a `cancelled` bucket for rows that tab never lists.
	// That is the behaviour this list shipped with; the filters are honest about q, the people
	// and the date ranges, and only that one bucket overstates. Do not read these counts as
	// fully scope-exact until that is fixed on its own.
	where := "tenant_id = $1"
	switch p.Scope {
	case domain.ScopeAssignedByMe:
		// The scope pins the raiser, so a raised_by filter is dropped here as it is for the rows.
		where += fmt.Sprintf(" AND raised_by = $%d::uuid", bindArg(args, p.UserID))
		where += filterPredicates(args, "", p, true, false)
	case domain.ScopeTeamProgress:
		where += filterPredicates(args, "", p, true, true)
	default:
		where += fmt.Sprintf(" AND assignee_user_id = $%d::uuid", bindArg(args, p.UserID))
		where += filterPredicates(args, "", p, false, true)
	}
	return where
}

// scopeCountsWheres is the scopes[] tab count predicates: for each tab, how many rows THAT tab
// would show under the request's current filters, MINUS the scope predicate itself. Each branch
// drops the person filter its own scope pins, exactly as the row query does when that tab is
// opened -- otherwise the badge would count rows the tab then refuses to filter on. They are
// returned in the order the aggregate statement -- and therefore the shared bind list --
// consumes them, and each clause's text is unchanged from when this was its own SELECT.
//
// projection-review: membership=leadership_tasks for tenant, three disjoint party arms unioned; group_key=scope; join_cardinality=no joins (a task counts once per arm it belongs to, which is the tab semantics); pagination=whole-result summary, no OFFSET; scope=tenant on every arm
func scopeCountsWheres(args *[]any, p ports.ListParams) (toMe, byMe, team string) {
	toMe = fmt.Sprintf("tenant_id = $1 AND assignee_user_id = $%d::uuid AND status <> 'cancelled'", bindArg(args, p.UserID))
	toMe += filterPredicates(args, "", p, false, true)
	byMe = fmt.Sprintf("tenant_id = $1 AND raised_by = $%d::uuid AND status <> 'cancelled'", bindArg(args, p.UserID))
	byMe += filterPredicates(args, "", p, true, false)
	team = "tenant_id = $1 AND status <> 'cancelled'" + filterPredicates(args, "", p, true, true)
	return toMe, byMe, team
}

// sqlListAggregates is the ONE statement that answers all three chip aggregates the list page
// renders: the status chips, the scope tabs and the caller's unseen badge.
//
// WHY ONE STATEMENT. These were three separate Query/QueryRow calls, and MEASURED against the
// staging database over its SSH tunnel that was the dominant cost of the whole endpoint: each
// of the three executes in well under a millisecond of server time (0.24ms, 0.43ms, 0.07ms on
// 470 tenant rows) while each round trip to reach it costs ~20ms of pure network. Three trips
// to fetch 1ms of work is the problem; the queries themselves are not. Unioning them spends
// one trip instead of three and changes no predicate.
//
// Every arm's WHERE clause is the SAME text its own query rendered before, taken from
// statusCountsWhere and scopeCountsWheres, so the chips still cannot advertise a row the list
// hides -- that invariant lives in those two builders and is unchanged here. The arms bind in
// textual order through the shared bindArg list, which is why the status arm is rendered first
// and the unseen arm last.
//
// projection-review: membership=leadership_tasks, three independent aggregate arms over the same tenant plus the scope/party predicates and request filters the individual count queries already used; group_key=(kind, key) -- status for the chip arm, scope for the tab arm, a single row for the unseen arm; join_cardinality=no joins on any arm; pagination=whole-result summaries independent of the task page, no OFFSET; scope=tenant_id on every arm, plus the actor party predicate on every arm that names a person
// scopeTotalsWheres is the scopes[] tab SIZE predicates: the same three party arms as
// scopeCountsWheres with NO request filter appended, so each answers "how many tasks are on
// that tab at all". They feed ports.Page.ScopeTotals (see the comment there for why the tab
// label needs the unfiltered number beside the filtered one).
//
// projection-review: membership=leadership_tasks for tenant, three disjoint party arms unioned; group_key=scope; join_cardinality=no joins (a task counts once per arm it belongs to, which is the tab semantics); pagination=whole-result summary, no OFFSET; scope=tenant on every arm
func scopeTotalsWheres(args *[]any, p ports.ListParams) (toMe, byMe, team string) {
	toMe = fmt.Sprintf("tenant_id = $1 AND assignee_user_id = $%d::uuid AND status <> 'cancelled'", bindArg(args, p.UserID))
	byMe = fmt.Sprintf("tenant_id = $1 AND raised_by = $%d::uuid AND status <> 'cancelled'", bindArg(args, p.UserID))
	team = "tenant_id = $1 AND status <> 'cancelled'"
	return toMe, byMe, team
}

func sqlListAggregates(args *[]any, p ports.ListParams) string {
	statusWhere := statusCountsWhere(args, p)
	toMe, byMe, team := scopeCountsWheres(args, p)
	toMeAll, byMeAll, teamAll := scopeTotalsWheres(args, p)

	// The unseen badge is the caller's own; a non-UUID actor has no badge rather than a cast
	// error, which is the guard unseenCount applies before it binds anything.
	unseenArm := sqlListAggregatesNoUnseenArm
	if uuidutil.IsUUIDString(p.UserID) {
		unseenArm = fmt.Sprintf(sqlListAggregatesUnseenArmTemplate, bindArg(args, p.UserID))
	}
	// The overdue chip's count: the SAME party + request filters as the status arm (so the chip
	// never advertises a row the lens hides), narrowed to the two working statuses and a
	// deadline before the request's farm clock. A zero clock counts nothing rather than
	// comparing against year 1.
	overdueArm := sqlListAggregatesNoOverdueArm
	if !p.OverdueAt.IsZero() {
		overdueWhere := statusCountsWhere(args, p)
		overdueArm = fmt.Sprintf(sqlListAggregatesOverdueArmTemplate, overdueWhere, bindArg(args, domain.OverdueStatuses), bindArg(args, p.OverdueAt))
	}

	return fmt.Sprintf(sqlListAggregatesTemplate, statusWhere, toMe, byMe, team, toMeAll, byMeAll, teamAll, unseenArm, overdueArm)
}

// sqlListAggregatesTemplate is the aggregate-bundle skeleton, hoisted to package level so a
// query-plan test and the scale guard can reach it (the convention the retired scope-count
// template used). The nine %s are, in bind order: the status arm's WHERE, the three per-tab
// filtered WHEREs, the three per-tab unfiltered WHEREs (the tab sizes), the unseen arm and
// the overdue arm.
const sqlListAggregatesTemplate = `
SELECT 'status'::text AS kind, status AS key, count(*) AS n FROM public.leadership_tasks
  WHERE %s
  GROUP BY status
UNION ALL
SELECT 'scope'::text, scope, count(*) FROM (
  SELECT '` + domain.ScopeAssignedToMe + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
  UNION ALL
  SELECT '` + domain.ScopeAssignedByMe + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
  UNION ALL
  SELECT '` + domain.ScopeTeamProgress + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
) s
  GROUP BY scope
UNION ALL
SELECT 'scope_total'::text, scope, count(*) FROM (
  SELECT '` + domain.ScopeAssignedToMe + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
  UNION ALL
  SELECT '` + domain.ScopeAssignedByMe + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
  UNION ALL
  SELECT '` + domain.ScopeTeamProgress + `'::text AS scope FROM public.leadership_tasks
  WHERE %s
) st
  GROUP BY scope
UNION ALL
%s
UNION ALL
%s`

// sqlListAggregatesOverdueArmTemplate is the overdue chip's arm: %s is the status arm's WHERE
// (rebound), then the working-status array bind and the farm-clock bind. It is served by
// leadership_tasks_tenant_deadline_idx (tenant_id, deadline_at, task_id): a range scan up to
// the clock, then the status test on the heap rows -- the same shape the deadline sorts use.
const sqlListAggregatesOverdueArmTemplate = `SELECT 'overdue'::text, ''::text, count(*) FROM public.leadership_tasks
  WHERE %s AND status = ANY($%d) AND deadline_at < $%d::timestamptz`

// sqlListAggregatesNoOverdueArm keeps the bundle's shape when no clock was supplied.
const sqlListAggregatesNoOverdueArm = `SELECT 'overdue'::text, ''::text, 0::bigint`

// sqlListAggregatesUnseenArmTemplate is the unseen-badge arm; its one %d is the actor bind.
const sqlListAggregatesUnseenArmTemplate = `SELECT 'unseen'::text, ''::text, count(*) FROM public.leadership_tasks
  WHERE tenant_id = $1 AND assignee_user_id = $%d::uuid AND seen_at IS NULL AND status <> 'cancelled'`

// sqlListAggregatesNoUnseenArm is the same arm for an actor who is not a uuid: a literal zero,
// binding nothing, so the bundle keeps its third row and the badge reads 0 rather than erroring
// on the cast.
const sqlListAggregatesNoUnseenArm = `SELECT 'unseen'::text, ''::text, 0::bigint`

// enrichPage fills the page's attachments, notes and note mentions in ONE round trip.
//
// WHY PIPELINED. These are three keyed reads over the page's own task ids -- each already ONE
// set-based query for the whole page, never one per task -- so there was no N+1 left to fix.
// What remained was three SEQUENTIAL round trips, and MEASURED against the staging database
// over its SSH tunnel that is what the time actually went on: the three execute in well under
// a millisecond of server work between them, while each trip to reach them costs ~20ms of pure
// network. pgx sends the batch as one exchange, so the three queries cost one trip.
//
// The SQL is byte-identical to what attachTo / notesTo / mentionsTo send, and the results are
// read in the order they were queued. The ORDER MATTERS for the third one: the mention scan
// hangs each mention on a note POINTER, so the notes must already be on the tasks before
// noteIndexOf is called -- which is why the note result is drained before the mention result
// even though both were sent together. The mention query is keyed on TASK ids, not note ids,
// so it is safe to send before the notes have landed; only its scan depends on them.
func (r *Repository) enrichPage(ctx context.Context, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids, index := taskIndexOf(tasks)

	batch := &pgx.Batch{}
	batch.Queue(sqlRepository5, tenantID, ids)                            // attachments
	batch.Queue(sqlListNotes, tenantID, ids)                              // notes
	batch.Queue(sqlListMentions, tenantID, ids)                           // mentions of those notes
	batch.Queue(sqlListEventsCapped, tenantID, ids, listEventsCapPerTask) // activity, newest 30 per row
	results := r.pool.SendBatch(ctx, batch)
	defer results.Close()

	attachments, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list attachments: %w", err)
	}
	if err := scanAttachmentsInto(attachments, tasks, index); err != nil {
		attachments.Close()
		return err
	}
	attachments.Close()

	noteRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list notes: %w", err)
	}
	if err := scanNotesInto(noteRows, tasks, index); err != nil {
		noteRows.Close()
		return err
	}
	noteRows.Close()

	mentionRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list mentions: %w", err)
	}
	// Built only now, because the notes it points into landed in the step above.
	if err := scanMentionsInto(mentionRows, noteIndexOf(tasks)); err != nil {
		mentionRows.Close()
		return err
	}
	mentionRows.Close()

	eventRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list events: %w", err)
	}
	if err := scanEventsInto(eventRows, tasks, index); err != nil {
		eventRows.Close()
		return err
	}
	eventRows.Close()

	return results.Close()
}

// attachTo loads the attachments of a bounded task set in one query and fills them in.
func (r *Repository) attachTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids, index := taskIndexOf(tasks)
	rows, err := q.Query(ctx, sqlRepository5, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list attachments: %w", err)
	}
	defer rows.Close()
	return scanAttachmentsInto(rows, tasks, index)
}

// taskIndexOf is the page's id list and its id -> row index, built once so the three
// enrichment reads key their rows the same way.
func taskIndexOf(tasks []domain.Task) ([]string, map[string]int) {
	ids := make([]string, 0, len(tasks))
	index := make(map[string]int, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].TaskID)
		index[tasks[i].TaskID] = i
	}
	return ids, index
}

// scanAttachmentsInto hangs one attachment result set on its tasks. Split out of attachTo so
// the pipelined page read and the single-query path share one scan; the attachment count is
// derived here, never trusted from a column.
func scanAttachmentsInto(rows pgx.Rows, tasks []domain.Task, index map[string]int) error {
	for i := range tasks {
		tasks[i].Attachments = nil
	}
	for rows.Next() {
		var taskID string
		var a domain.Attachment
		if err := rows.Scan(&taskID, &a.AttachmentID, &a.ProofID, &a.Kind, &a.MimeType, &a.FileName, &a.SizeBytes, &a.DurationMS, &a.Position); err != nil {
			return fmt.Errorf("leadership task: attachments scan: %w", err)
		}
		if i, ok := index[taskID]; ok {
			tasks[i].Attachments = append(tasks[i].Attachments, a)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range tasks {
		tasks[i].AttachmentCount = len(tasks[i].Attachments)
	}
	return nil
}

// notesTo loads the chronological task notes of a bounded task set in one query.
func (r *Repository) notesTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids, index := taskIndexOf(tasks)
	rows, err := q.Query(ctx, sqlListNotes, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list notes: %w", err)
	}
	defer rows.Close()
	return scanNotesInto(rows, tasks, index)
}

// scanNotesInto hangs one note result set on its tasks, split out of notesTo so the pipelined
// page read and the single-query path share one scan.
func scanNotesInto(rows pgx.Rows, tasks []domain.Task, index map[string]int) error {
	for i := range tasks {
		tasks[i].Notes = nil
	}
	for rows.Next() {
		var taskID string
		var n domain.Note
		if err := rows.Scan(&taskID, &n.NoteID, &n.AuthorID, &n.AuthorName, &n.Body, &n.CreatedAt); err != nil {
			return fmt.Errorf("leadership task: notes scan: %w", err)
		}
		n.CreatedAt = n.CreatedAt.UTC()
		if i, ok := index[taskID]; ok {
			tasks[i].Notes = append(tasks[i].Notes, n)
		}
	}
	return rows.Err()
}

// The cursor is base64url of "<sort>\x1f<value>\x1f<task_id>": the name of the sort it was
// minted under, the row's sort value (RFC3339Nano of raised_at or deadline_at, or "" for a
// NULL deadline), and the tie-breaking task id. The sort name is IN the payload so a cursor
// carried across a sort change is refused instead of silently paging the wrong order.
const cursorSep = "\x1f"

// sortCursor is one decoded cursor position.
type sortCursor struct {
	value     string
	valueNull bool
	taskID    string
}

// encodeSortCursor mints the next-page cursor from the last row served.
func encodeSortCursor(sortKey string, last domain.Task) string {
	value := ""
	switch sortKey {
	case ports.SortDeadlineAsc, ports.SortDeadlineDesc:
		if last.DeadlineAt != nil {
			value = last.DeadlineAt.UTC().Format(time.RFC3339Nano)
		}
	default:
		value = last.RaisedAt.UTC().Format(time.RFC3339Nano)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(sortKey + cursorSep + value + cursorSep + last.TaskID))
}

// decodeSortCursor reads a cursor for the ACTIVE sort. It also still accepts the legacy
// plaintext "<RFC3339Nano>|<uuid>" form, but ONLY under the default sort it was minted for,
// so an Android build in flight keeps paging while a new sort can never inherit it.
func decodeSortCursor(sortKey, cursor string) (sortCursor, error) {
	if raw, err := base64.RawURLEncoding.DecodeString(cursor); err == nil {
		parts := strings.Split(string(raw), cursorSep)
		if len(parts) == 3 && ports.IsSortKey(parts[0]) {
			if parts[0] != sortKey {
				return sortCursor{}, fmt.Errorf("%w: cursor was minted for sort %q, not %q", ports.ErrInvalidArgument, parts[0], sortKey)
			}
			c := sortCursor{value: parts[1], valueNull: parts[1] == "", taskID: parts[2]}
			if !c.valueNull {
				if _, err := time.Parse(time.RFC3339Nano, c.value); err != nil {
					return sortCursor{}, fmt.Errorf("%w: bad cursor timestamp: %v", ports.ErrInvalidArgument, err)
				}
			} else if sortKey != ports.SortDeadlineAsc && sortKey != ports.SortDeadlineDesc {
				// Only a deadline sort has a NULL sort value; raised_at is NOT NULL.
				return sortCursor{}, fmt.Errorf("%w: bad cursor", ports.ErrInvalidArgument)
			}
			if !uuidutil.IsUUIDString(c.taskID) {
				return sortCursor{}, fmt.Errorf("%w: bad cursor task id", ports.ErrInvalidArgument)
			}
			return c, nil
		}
	}
	if sortKey != ports.SortRaisedAtDesc {
		return sortCursor{}, fmt.Errorf("%w: bad cursor for sort %q", ports.ErrInvalidArgument, sortKey)
	}
	raisedAt, taskID, err := decodeLegacyCursor(cursor)
	if err != nil {
		return sortCursor{}, err
	}
	return sortCursor{value: raisedAt, taskID: taskID}, nil
}

// decodeLegacyCursor reads the pre-sort plaintext cursor shape.
func decodeLegacyCursor(cursor string) (string, string, error) {
	parts := strings.SplitN(cursor, "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("%w: bad cursor", ports.ErrInvalidArgument)
	}
	if _, err := time.Parse(time.RFC3339Nano, parts[0]); err != nil {
		return "", "", fmt.Errorf("%w: bad cursor timestamp: %v", ports.ErrInvalidArgument, err)
	}
	if !uuidutil.IsUUIDString(parts[1]) {
		return "", "", fmt.Errorf("%w: bad cursor task id", ports.ErrInvalidArgument)
	}
	return parts[0], parts[1], nil
}

// GetTask reads one task with its attachments.
func (r *Repository) GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.getRow(ctx, r.pool, tenantID, taskID, false)
}

// batchQuerier is what a pgx.Tx and a *pgxpool.Pool have in common for the pipelined read.
type batchQuerier interface {
	querier
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// getRow is the full detail read: the task row, then its attachments, notes, mentions,
// participants and activity in ONE pipelined batch (one round trip, not five). Over the
// ~40ms tunnel this page runs on, the five sequential reads it replaced were the bulk of a
// 1.3s comment.
//
// forUpdate is the LOCKED read the write transactions take before they change the row: it
// returns the row and its participants only (the two things CanEdit/CanComment/CanRead need),
// because the notes, attachments and activity that the response carries are re-read AFTER
// the write anyway.
func (r *Repository) getRow(ctx context.Context, q batchQuerier, tenantID, taskID string, forUpdate bool) (domain.Task, error) {
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE OF t"
	}
	query := fmt.Sprintf(`SELECT %s %s WHERE t.tenant_id = $1 AND t.task_id = $2%s`, taskColumns, taskFrom, lock)
	t, err := scanTask(q.QueryRow(ctx, query, tenantID, taskID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: get: %w", err)
	}
	tasks := []domain.Task{t}
	if forUpdate {
		if err := r.participantsTo(ctx, q, tenantID, tasks); err != nil {
			return domain.Task{}, err
		}
		return tasks[0], nil
	}
	if err := r.enrichWith(ctx, q, tenantID, tasks); err != nil {
		return domain.Task{}, err
	}
	return tasks[0], nil
}

// enrichWith fills attachments, notes, mentions, participants and activity for a bounded
// task set in ONE pipelined batch against q (a tx or the pool).
func (r *Repository) enrichWith(ctx context.Context, q batchQuerier, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids, index := taskIndexOf(tasks)
	for i := range tasks {
		tasks[i].ParticipantUserIDs = nil
	}

	batch := &pgx.Batch{}
	batch.Queue(sqlRepository5, tenantID, ids)      // attachments
	batch.Queue(sqlListNotes, tenantID, ids)        // notes
	batch.Queue(sqlListMentions, tenantID, ids)     // mentions of those notes
	batch.Queue(sqlListParticipants, tenantID, ids) // mention-granted readers
	batch.Queue(sqlListEvents, tenantID, ids)       // activity, newest first
	results := q.SendBatch(ctx, batch)
	defer results.Close()

	attachments, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list attachments: %w", err)
	}
	if err := scanAttachmentsInto(attachments, tasks, index); err != nil {
		attachments.Close()
		return err
	}
	attachments.Close()

	noteRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list notes: %w", err)
	}
	if err := scanNotesInto(noteRows, tasks, index); err != nil {
		noteRows.Close()
		return err
	}
	noteRows.Close()

	mentionRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list mentions: %w", err)
	}
	if err := scanMentionsInto(mentionRows, noteIndexOf(tasks)); err != nil {
		mentionRows.Close()
		return err
	}
	mentionRows.Close()

	partRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list participants: %w", err)
	}
	for partRows.Next() {
		var tID, userID string
		if err := partRows.Scan(&tID, &userID); err != nil {
			partRows.Close()
			return fmt.Errorf("leadership task: participants scan: %w", err)
		}
		if i, ok := index[tID]; ok {
			tasks[i].ParticipantUserIDs = append(tasks[i].ParticipantUserIDs, userID)
		}
	}
	if err := partRows.Err(); err != nil {
		partRows.Close()
		return err
	}
	partRows.Close()

	eventRows, err := results.Query()
	if err != nil {
		return fmt.Errorf("leadership task: list events: %w", err)
	}
	if err := scanEventsInto(eventRows, tasks, index); err != nil {
		eventRows.Close()
		return err
	}
	eventRows.Close()

	return results.Close()
}

// Raise records a new task, mints its number, stores its attachments, audits it and
// announces it -- one transaction.
func (r *Repository) Raise(ctx context.Context, p ports.RaiseParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin raise: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.ActorID, p.AssigneeUserID, p.Title, p.Body, attachmentFingerprint(p.Attachments), deadlineFingerprint(p.DeadlineAt))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeRaise, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		if reservation.resultID == "" {
			return domain.Task{}, ports.ErrIdempotencyConflict
		}
		return r.getRow(ctx, r.pool, p.TenantID, reservation.resultID, false)
	}

	// The assignee must be assignable NOW, by the same tick AND leadership grant the picker
	// reads, checked inside the write: the picker is a read that can go stale between the form
	// opening and the send.
	var assignable bool
	if err := tx.QueryRow(ctx, sqlAssigneeIsTicked, p.TenantID, p.AssigneeUserID, permissions.SurfaceMobile, leadershipTasksModuleKey, permissions.LevelOversee, assignableLeadershipRoles).Scan(&assignable); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: check assignee: %w", err)
	}
	if !assignable {
		return domain.Task{}, domain.ErrAssigneeNotAssignable
	}

	// The running number is minted under a per-tenant advisory lock so two directors raising
	// at once cannot both take #12. The lock is transaction-scoped and released at commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('leadership_tasks:' || $1::text))`, p.TenantID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: number lock: %w", err)
	}
	now := r.now().UTC()
	var taskID string
	if err := tx.QueryRow(ctx, sqlRepository7,
		p.TenantID, p.Title, p.Body, domain.StatusOpen, p.ActorID, p.ActorDesignation, p.AssigneeUserID, now, p.DeadlineAt,
	).Scan(&taskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: insert: %w", err)
	}
	if err := insertAttachments(ctx, tx, p.TenantID, taskID, p.Attachments); err != nil {
		return domain.Task{}, err
	}
	// The feed's first row, written by the raise itself (migration 000349).
	if err := recordEvent(ctx, tx, p.TenantID, taskID, now, p.ActorID, domain.EventCreated, "", "", ""); err != nil {
		return domain.Task{}, err
	}
	task, err := r.getRow(ctx, tx, p.TenantID, taskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "leadership_task.raised",
		ResourceType: resourceType,
		ResourceID:   taskID,
		AfterState:   auditState(task),
		Metadata: map[string]any{
			"domain":           "leadership_tasks",
			"module":           "leadership_tasks",
			"task_no":          task.TaskNo,
			"assignee_user_id": p.AssigneeUserID,
			"attachment_count": len(p.Attachments),
			"idempotency_key":  p.IdempotencyKey,
			"operation_id":     p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit raise: %w", err)
	}
	if err := emitEvent(ctx, tx, EventTaskRaised, task, "", p.ActorID, p.IdempotencyKey, now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeRaise, p.IdempotencyKey, resourceType, taskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete raise idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit raise: %w", err)
	}
	return task, nil
}

// Edit replaces the brief and the attachment list of a task the caller raised, under the
// row lock and the row version the screen loaded with.
func (r *Repository) Edit(ctx context.Context, p ports.EditParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin edit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.ActorID, p.Title, p.Body, fmt.Sprintf("%d", p.RowVersion), attachmentFingerprint(p.Attachments), deadlineFingerprint(p.DeadlineAt))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeEdit, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	actor := p.Actor
	if actor.UserID == "" {
		actor = domain.Actor{UserID: p.ActorID, CanRaise: true}
	}
	// ONE rule, domain.Task.CanEdit -- the predicate the detail payload's can_edit answers
	// with -- so the Edit button and this refusal can never disagree.
	if !before.CanEdit(actor) {
		if !domain.IsOpenForWork(before.Status) {
			return domain.Task{}, domain.ErrTaskClosed
		}
		return domain.Task{}, domain.ErrNotRaiser
	}
	if before.RowVersion != p.RowVersion {
		return domain.Task{}, ports.ErrVersionConflict
	}
	// The deadline is checked against the STORED raise instant, here under the row lock. A nil
	// deadline keeps the stored one (an older phone editing the brief), so it is never
	// validated against nothing and never wiped.
	deadline := before.DeadlineAt
	if p.DeadlineAt != nil {
		if err := domain.ValidateDeadline(p.DeadlineAt, before.RaisedAt, true); err != nil {
			return domain.Task{}, err
		}
		deadline = p.DeadlineAt
	}
	now := r.now().UTC()
	if _, err := tx.Exec(ctx, sqlRepository8, p.TenantID, p.TaskID, p.Title, p.Body, now, deadline); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: update brief: %w", err)
	}
	// The attachment list is REPLACED: the phone sends the full list it shows, and the
	// server keeps exactly that. Removed rows go; kept rows keep their ids; new rows are
	// inserted. Proof bytes are never deleted here -- the proof store owns retention.
	if _, err := tx.Exec(ctx, sqlRepository9,
		p.TenantID, p.TaskID, proofIDs(p.Attachments)); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: prune attachments: %w", err)
	}
	if err := insertAttachments(ctx, tx, p.TenantID, p.TaskID, p.Attachments); err != nil {
		return domain.Task{}, err
	}
	// One feed row per field that actually moved (title / brief / deadline), compared
	// against the row read under the lock above, so an unchanged re-save writes nothing.
	edited := before
	edited.Title, edited.Body, edited.DeadlineAt = p.Title, p.Body, deadline
	if err := recordEditEvents(ctx, tx, p.TenantID, p.TaskID, now, p.ActorID, before, edited); err != nil {
		return domain.Task{}, err
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    "human",
		Action:       "leadership_task.edited",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit edit: %w", err)
	}
	// The other party is owed the news that the brief, the deadline or the attachments moved
	// (maintainer instruction 2026-09-18). Emitted INSIDE the write, like every other task
	// event, so a rolled-back edit never announces itself.
	if err := emitEvent(ctx, tx, EventTaskUpdated, after, "", p.ActorID, p.IdempotencyKey, now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeEdit, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete edit idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit edit: %w", err)
	}
	return after, nil
}

// ChangeStatus moves the task along its ladder under the row lock. The rule is
// domain.CheckTransition -- the same function that composes status_options -- so the
// buttons the screen showed are exactly the writes this accepts.
func (r *Repository) ChangeStatus(ctx context.Context, p ports.StatusParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(p.TaskID, p.Actor.UserID, p.Status, fmt.Sprintf("%d", p.RowVersion))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeStatus, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.CheckTransition(before, p.Actor, p.Status); err != nil {
		return domain.Task{}, err
	}
	if before.RowVersion != p.RowVersion {
		return domain.Task{}, ports.ErrVersionConflict
	}
	now := r.now().UTC()
	if _, err := tx.Exec(ctx, sqlRepository10, p.TenantID, p.TaskID, p.Status, now); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: update status: %w", err)
	}
	// A cancel is its own kind on the feed ("Hemant cancelled the task"); every other move is
	// a status change carrying both ends, so the panel can draw the two chips and the arrow.
	statusKind := domain.EventStatusChanged
	if p.Status == domain.StatusCancelled {
		statusKind = domain.EventCancelled
	}
	if err := recordEvent(ctx, tx, p.TenantID, p.TaskID, now, p.Actor.UserID, statusKind, before.Status, p.Status, ""); err != nil {
		return domain.Task{}, err
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "leadership_task.status_changed",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  auditState(before),
		AfterState:   auditState(after),
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"from":            before.Status,
			"to":              after.Status,
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit status: %w", err)
	}
	if err := emitEvent(ctx, tx, EventTaskStatusChanged, after, before.Status, p.Actor.UserID, p.IdempotencyKey, now); err != nil {
		return domain.Task{}, err
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeStatus, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete status idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit status: %w", err)
	}
	return after, nil
}

// SetComment records the assignee's note under the row lock. No version fence: a note is
// its owner's own text and the latest one wins, the way a text field works.
func (r *Repository) SetComment(ctx context.Context, p ports.CommentParams) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin comment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The mention list is part of the request's identity: the same key with a different set of
	// people named is a DIFFERENT note, and replaying it as the first one would drop mentions.
	fingerprint := requestFingerprint(p.TaskID, p.Actor.UserID, p.Comment, strings.Join(p.MentionUserIDs, ","))
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, idemScopeComment, p.IdempotencyKey, fingerprint)
	if err != nil {
		return domain.Task{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Task{}, err
		}
		return r.getRow(ctx, r.pool, p.TenantID, p.TaskID, false)
	}
	before, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, true)
	if err != nil {
		return domain.Task{}, err
	}
	if !before.CanComment(p.Actor) {
		if before.Status == domain.StatusCancelled {
			return domain.Task{}, domain.ErrTaskClosed
		}
		return domain.Task{}, domain.ErrNotAssignee
	}
	now := r.now().UTC()
	if before.IsAssignee(p.Actor) {
		if _, err := tx.Exec(ctx, sqlSetComment, p.TenantID, p.TaskID, p.Comment, now); err != nil {
			return domain.Task{}, fmt.Errorf("leadership task: update comment: %w", err)
		}
	} else if _, err := tx.Exec(ctx, sqlTouchTask, p.TenantID, p.TaskID, now); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: touch comment: %w", err)
	}
	// The mention targets are re-validated HERE, under the row lock taken above, against the
	// same list the `@` autocomplete reads. An id from a stale or hostile client can therefore
	// never reach someone who cannot already see this task.
	mentions, err := r.resolveMentionTargets(ctx, tx, p.TenantID, p.TaskID, p.MentionUserIDs)
	if err != nil {
		return domain.Task{}, err
	}
	noteID := ""
	if p.Comment != "" {
		if err := tx.QueryRow(ctx, sqlInsertNote, p.TenantID, p.TaskID, p.Actor.UserID, p.Comment, now).Scan(&noteID); err != nil {
			return domain.Task{}, fmt.Errorf("leadership task: insert note: %w", err)
		}
		if err := insertMentions(ctx, tx, p.TenantID, p.TaskID, noteID, p.Actor.UserID, mentions, now); err != nil {
			return domain.Task{}, err
		}
		if err := recordEvent(ctx, tx, p.TenantID, p.TaskID, now, p.Actor.UserID, domain.EventCommented, "", "", noteID); err != nil {
			return domain.Task{}, err
		}
	}
	after, err := r.getRow(ctx, tx, p.TenantID, p.TaskID, false)
	if err != nil {
		return domain.Task{}, err
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.Actor.UserID,
		ActorType:    "human",
		Action:       "leadership_task.commented",
		ResourceType: resourceType,
		ResourceID:   p.TaskID,
		BeforeState:  map[string]any{"comment": before.AssigneeComment},
		AfterState:   map[string]any{"comment": after.AssigneeComment},
		Metadata: map[string]any{
			"domain":          "leadership_tasks",
			"module":          "leadership_tasks",
			"task_no":         after.TaskNo,
			"mention_count":   len(mentions),
			"idempotency_key": p.IdempotencyKey,
			"operation_id":    p.IdempotencyKey,
		},
	}); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: audit comment: %w", err)
	}
	// Only an actual NOTE is news. A comment call that wrote no note (an older phone clearing
	// the assignee field) has nothing to tell anyone and emits nothing.
	if noteID != "" {
		if err := emitEventWith(ctx, tx, EventTaskCommented, after, "", p.Actor.UserID, p.IdempotencyKey, now, eventExtras{
			NoteID:           noteID,
			NoteExcerpt:      noteExcerpt(p.Comment),
			MentionedUserIDs: mentions,
		}); err != nil {
			return domain.Task{}, err
		}
	}
	if err := completeIdempotency(ctx, tx, p.TenantID, idemScopeComment, p.IdempotencyKey, resourceType, p.TaskID); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: complete comment idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit comment: %w", err)
	}
	return after, nil
}

// MarkSeen stamps seen_at once for the assignee. A replay updates nothing (the WHERE
// keeps it a set-if-null), so no idempotency key is needed. Audited only when it changed.
func (r *Repository) MarkSeen(ctx context.Context, tenantID, taskID, userID string) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: begin seen: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := r.now().UTC()
	tag, err := tx.Exec(ctx, sqlRepository11,
		tenantID, taskID, userID, now)
	if err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: mark seen: %w", err)
	}
	if tag.RowsAffected() == 1 {
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorID:      userID,
			ActorType:    "human",
			Action:       "leadership_task.seen",
			ResourceType: resourceType,
			ResourceID:   taskID,
			Metadata:     map[string]any{"domain": "leadership_tasks", "module": "leadership_tasks"},
		}); err != nil {
			return domain.Task{}, fmt.Errorf("leadership task: audit seen: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("leadership task: commit seen: %w", err)
	}
	return r.getRow(ctx, r.pool, tenantID, taskID, false)
}

// UnseenCount answers the drawer badge: tasks addressed to the person, not yet opened,
// not cancelled.
func (r *Repository) UnseenCount(ctx context.Context, tenantID, userID string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.unseenCount(ctx, r.pool, tenantID, userID)
}

func (r *Repository) unseenCount(ctx context.Context, q querier, tenantID, userID string) (int, error) {
	if !uuidutil.IsUUIDString(userID) {
		return 0, nil
	}
	var n int
	if err := q.QueryRow(ctx, sqlRepository12,
		tenantID, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("leadership task: unseen count: %w", err)
	}
	return n, nil
}

func insertAttachments(ctx context.Context, tx pgx.Tx, tenantID, taskID string, attachments []domain.Attachment) error {
	for _, a := range attachments {
		// scale-guard:ignore: bounded by domain.MaxAttachments (12) per write.
		if _, err := tx.Exec(ctx, sqlRepository13,
			tenantID, taskID, a.ProofID, a.Kind, a.MimeType, a.FileName, a.SizeBytes, a.DurationMS, a.Position); err != nil {
			return fmt.Errorf("leadership task: insert attachment: %w", err)
		}
	}
	return nil
}

func proofIDs(attachments []domain.Attachment) []string {
	out := make([]string, 0, len(attachments))
	for _, a := range attachments {
		out = append(out, a.ProofID)
	}
	return out
}

func attachmentFingerprint(attachments []domain.Attachment) string {
	parts := make([]string, 0, len(attachments))
	for _, a := range attachments {
		parts = append(parts, a.ProofID+"/"+a.Kind+"/"+a.FileName)
	}
	return strings.Join(parts, ",")
}

func auditState(t domain.Task) map[string]any {
	return map[string]any{
		"task_no":               t.TaskNo,
		"title":                 t.Title,
		"status":                t.Status,
		"raised_by_designation": t.RaisedByDesignation,
		"assignee_user_id":      t.AssigneeUserID,
		"attachment_count":      len(t.Attachments),
		"row_version":           t.RowVersion,
		"deadline_at":           deadlineFingerprint(t.DeadlineAt),
	}
}

// deadlineFingerprint is the deadline as one stable string for fingerprints and audit: the
// RFC3339 UTC instant, or "" for none.
// noteExcerpt is the short form of a note the push quotes. It keeps the copy specific -- a
// reader must see WHAT was said, not just that something was -- while staying readable on a
// lock screen.
func noteExcerpt(body string) string {
	body = strings.Join(strings.Fields(body), " ")
	const max = 120
	if len([]rune(body)) > max {
		return string([]rune(body)[:max-1]) + "\u2026"
	}
	return body
}

func deadlineFingerprint(deadline *time.Time) string {
	if deadline == nil {
		return ""
	}
	return deadline.UTC().Format(time.RFC3339)
}

// SQL hoisted to package level so the scale guard and query-plan tests can reach it.
//
// Two of these are COUNT projections rather than row reads -- sqlRepository4 (the per-status
// chip counts over the party-OR predicate) and sqlRepository12 (the assignee's unseen badge) --
// so they carry their own grain proof. Both are the pre-optimisation single-purpose forms: the
// list page now answers the chips through sqlListAggregates instead, and sqlRepository12's text
// is the unseen arm of that bundle, still reached on its own by UnseenCount/unseenCount.
//
// projection-review: membership=leadership_tasks at its task_id primary key, one row per task,
// for one tenant plus the party predicate (raised_by = $2 OR assignee_user_id = $2 for the
// status chips, assignee_user_id = $2 AND seen_at IS NULL AND status <> 'cancelled' for the
// unseen badge) -- no other table decides who belongs to either population; group_key=status
// for sqlRepository4 (one row per live status value, disjoint because status is a single
// non-null column, so the buckets sum to the filtered population exactly once) and none for
// sqlRepository12, which returns one scalar; join_cardinality=neither statement joins at all --
// they read public.leadership_tasks alone, so no dimension, attachment, note or grant row can
// multiply a task into two counted rows (the name LEFT JOINs of sqlRepository2 belong to the
// ROW projection and are 1:1 on workforce_members_active_user_unique_idx); pagination=both are
// whole-result aggregates over the full filtered population, computed independently of the
// row page -- there is no LIMIT, no OFFSET and no cursor predicate in either, so neither
// number can change with the page size or the page position; scope=tenant_id = $1 is the first
// predicate of both, and each also pins the actor party, so neither can count another tenant's
// or another person's task.
const (
	sqlRepository1 = `
	t.task_id::text, t.tenant_id::text, t.task_no, t.title, t.body, t.status,
	t.raised_by::text, COALESCE(rb.display_name, ''), COALESCE(t.raised_by_designation, ''),
	t.assignee_user_id::text, COALESCE(asg.display_name, ''),
	t.raised_at, t.deadline_at, t.updated_at, t.done_at, t.cancelled_at, t.seen_at, t.assignee_comment, t.row_version`
	sqlRepository2 = `
FROM public.leadership_tasks t
LEFT JOIN public.workforce_members rb
       ON rb.tenant_id = t.tenant_id AND rb.user_id = t.raised_by AND rb.status = 'active'
LEFT JOIN public.workforce_members asg
       ON asg.tenant_id = t.tenant_id AND asg.user_id = t.assignee_user_id AND asg.status = 'active'`
	sqlRepository3 = `
SELECT DISTINCT g.user_id::text, m.display_name
FROM public.user_scope_grants g
JOIN public.workforce_members m
  ON m.tenant_id = g.tenant_id AND m.user_id = g.user_id AND m.status = 'active'
WHERE g.tenant_id = $1 AND g.role = $2 AND g.status = 'active'
  AND (g.valid_to IS NULL OR g.valid_to > now())
ORDER BY m.display_name, g.user_id::text
LIMIT 100`
	sqlRepository4 = `
SELECT status, count(*) FROM public.leadership_tasks
WHERE tenant_id = $1 AND (raised_by = $2::uuid OR assignee_user_id = $2::uuid)
GROUP BY status`
	sqlRepository5 = `
SELECT task_id::text, attachment_id::text, proof_id::text, kind, mime_type, file_name, size_bytes, duration_ms, position
FROM public.leadership_task_attachments
WHERE tenant_id = $1 AND task_id = ANY($2::uuid[])
ORDER BY task_id, position, attachment_id`
	sqlRepository6 = `
SELECT EXISTS (
  SELECT 1 FROM public.user_scope_grants g
  WHERE g.tenant_id = $1 AND g.user_id = $2::uuid AND g.role = $3 AND g.status = 'active'
    AND (g.valid_to IS NULL OR g.valid_to > now())
)`
	sqlRepository7 = `
INSERT INTO public.leadership_tasks (
  tenant_id, task_no, title, body, status, raised_by, raised_by_designation, assignee_user_id, raised_at, updated_at, deadline_at
) VALUES (
  $1::uuid,
  (SELECT COALESCE(MAX(task_no), 0) + 1 FROM public.leadership_tasks WHERE tenant_id = $1::uuid),
  $2, $3, $4, $5::uuid, nullif($6, ''), $7::uuid, $8, $8, $9::timestamptz
)
RETURNING task_id::text`
	sqlRepository8 = `
UPDATE public.leadership_tasks
SET title = $3, body = $4, updated_at = $5, deadline_at = $6::timestamptz, row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`
	sqlRepository9 = `
DELETE FROM public.leadership_task_attachments
WHERE tenant_id = $1 AND task_id = $2 AND NOT (proof_id = ANY($3::uuid[]))`
	sqlRepository10 = `
UPDATE public.leadership_tasks
SET status = $3::text,
    updated_at = $4::timestamptz,
    done_at = CASE WHEN $3::text = 'done' THEN $4::timestamptz ELSE NULL::timestamptz END,
    cancelled_at = CASE WHEN $3::text = 'cancelled' THEN $4::timestamptz ELSE cancelled_at END,
    row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`
	sqlRepository11 = `
UPDATE public.leadership_tasks
SET seen_at = $4
WHERE tenant_id = $1 AND task_id = $2 AND assignee_user_id = $3::uuid AND seen_at IS NULL`
	sqlRepository12 = `
SELECT count(*) FROM public.leadership_tasks
WHERE tenant_id = $1 AND assignee_user_id = $2::uuid AND seen_at IS NULL AND status <> 'cancelled'`
	sqlRepository13 = `
INSERT INTO public.leadership_task_attachments (
  tenant_id, task_id, proof_id, kind, mime_type, file_name, size_bytes, duration_ms, position
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9)
ON CONFLICT (tenant_id, task_id, proof_id) DO UPDATE
SET kind = EXCLUDED.kind, file_name = EXCLUDED.file_name, position = EXCLUDED.position`
)

const sqlSetComment = `
UPDATE public.leadership_tasks
SET assignee_comment = $3::text, updated_at = $4::timestamptz, row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`

const sqlTouchTask = `
UPDATE public.leadership_tasks
SET updated_at = $3::timestamptz, row_version = row_version + 1
WHERE tenant_id = $1 AND task_id = $2`

const sqlInsertNote = `
INSERT INTO public.leadership_task_notes (tenant_id, task_id, author_user_id, body, created_at)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)
RETURNING note_id::text`

const sqlListNotes = `
SELECT n.task_id::text, n.note_id::text, n.author_user_id::text, COALESCE(w.display_name, ''), n.body, n.created_at
FROM public.leadership_task_notes n
LEFT JOIN public.workforce_members w
       ON w.tenant_id = n.tenant_id AND w.user_id = n.author_user_id AND w.status = 'active'
WHERE n.tenant_id = $1 AND n.task_id = ANY($2::uuid[])
ORDER BY n.task_id, n.created_at, n.note_id`

// leadershipTasksModuleKey is the module_key the /people ticks store for this module.
const leadershipTasksModuleKey = "leadership_tasks"

// assignableLeadershipRoles are the grants a person must hold (active, unexpired) on top of
// the Oversee tick to appear in the picker and to be raised for: the CXO desk, every park
// head and every director. An operator, verifier, HR or procurement manager with a stray
// tick is NOT assignable.
var assignableLeadershipRoles = []string{
	permissions.RoleCEOInternal,
	permissions.RoleParkHead,
	permissions.RolePCDirector,
	permissions.RoleGrowthDirector,
	permissions.RoleFeedDirector,
	permissions.RoleHealthDirector,
	permissions.RoleBreedingDirector,
	permissions.RoleProcurementDirector,
}

// projection-review: membership=person_module_access ticked at oversee on the mobile Tasks
// module, intersected with an active leadership user_scope_grants role -- the picker's
// population is decided by those two facts and nothing else; group_key=none, the statement
// aggregates nothing and emits exactly one row per ticked person, keyed on m.user_id;
// join_cardinality=workforce_members is 1:1 on the member PK, workforce_member_titles 1:1 on
// (tenant_id, workforce_member_id) PK, person_access 1:1 on (tenant_id, workforce_member_id),
// designation_catalog 1:1 on designation_code, and the grant is read as a SEMIJOIN (EXISTS)
// rather than a JOIN precisely so a person holding several leadership roles is still one row
// -- no arm of this statement can fan one person out, which is what
// TestListAssigneesTitleJoinsKeepOneToManyGrantsPaginationAndStatusBucketsHonest pins;
// pagination=none and no OFFSET, the result is bounded by the leadership tick itself and is
// returned whole, so no caller derives a count from a page of it;
// scope=tenant_id on person_module_access, on both title joins, on person_access and inside
// the grant semijoin, so no row can cross a tenant.
const sqlListAssignees = `
SELECT m.user_id::text, m.display_name,
       COALESCE(NULLIF(btrim(wt.title), ''), dc.label, '') AS title
FROM public.person_module_access a
JOIN public.workforce_members m
  ON m.tenant_id = a.tenant_id AND m.workforce_member_id = a.workforce_member_id AND m.status = 'active'
-- The title the picker shows: the HRMS business title (000293) when set, else the
-- designation catalog label. Both joins are 1:1 on the person's PK.
LEFT JOIN public.workforce_member_titles wt
  ON wt.tenant_id = m.tenant_id AND wt.workforce_member_id = m.workforce_member_id
LEFT JOIN public.person_access pa
  ON pa.tenant_id = m.tenant_id AND pa.workforce_member_id = m.workforce_member_id
LEFT JOIN public.designation_catalog dc
  ON dc.designation_code = pa.designation_code
WHERE a.tenant_id = $1 AND a.surface = $2 AND a.module_key = $3 AND $4 = ANY(a.capabilities)
  AND m.user_id IS NOT NULL
  -- A local development login (seed-dev-grant stamps its own rows dev_account=true) is not a
  -- person anyone assigns work to; the marker is the seeder's, never a name pattern.
  AND COALESCE((m.metadata->>'dev_account')::boolean, false) = false
  AND EXISTS (
    SELECT 1 FROM public.user_scope_grants g
    WHERE g.tenant_id = m.tenant_id AND g.user_id = m.user_id
      AND g.status = 'active' AND (g.valid_to IS NULL OR g.valid_to > now())
      AND g.role = ANY($5::text[])
  )
ORDER BY title, m.display_name, m.user_id::text`

const sqlAssigneeIsTicked = `
SELECT EXISTS (
  SELECT 1 FROM public.person_module_access a
  JOIN public.workforce_members m
    ON m.tenant_id = a.tenant_id AND m.workforce_member_id = a.workforce_member_id AND m.status = 'active'
  WHERE a.tenant_id = $1 AND m.user_id = $2::uuid
    AND a.surface = $3 AND a.module_key = $4 AND $5 = ANY(a.capabilities)
    AND COALESCE((m.metadata->>'dev_account')::boolean, false) = false
    AND EXISTS (
      SELECT 1 FROM public.user_scope_grants g
      WHERE g.tenant_id = m.tenant_id AND g.user_id = m.user_id
        AND g.status = 'active' AND (g.valid_to IS NULL OR g.valid_to > now())
        AND g.role = ANY($6::text[])
    )
)`
