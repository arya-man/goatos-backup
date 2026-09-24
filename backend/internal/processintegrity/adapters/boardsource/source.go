// Package boardsource is Vaccination's contribution to the cross-module Work Board: one
// board row per vaccination drive pen (a batch x rule x pen x day, the grain Action Center
// already renders), read by WRAPPING the existing process-integrity canonical read
// (processintegrity/adapters/postgres.Repository.ListRows). Vaccination's cross-module read
// already lives in processintegrity, so this file adds no SQL of its own: it binds that read
// to one park and one business day, and normalises its rows to domain.Row.
//
// READ-ONLY and REPORTING-ONLY. The work state, severity, park/pen/display and counts are
// copied VERBATIM from the process-integrity row; the only things composed here are the
// title, the subtitle (through the vaccination display mapper, so a raw dose code never
// reaches a screen) and the owner identity mapping.
//
// KEYSET NOTE. The board contract orders a source by source_id ascending and keysets on it.
// The wrapped read orders by (sort_priority, due_at, row_id) and its cursor is that triple,
// so a native row_id keyset is not available without a repository change. The read is
// therefore WALKED for the bounded park-day (pens x drives, tens of rows, capped at
// maxWalkRows) with its own cursor, sorted by row_id here, and the board keyset applied to
// that set. This is a bounded park-day read, not a whole-table scan; the trade is recorded so
// a future repository keyset on row_id can replace it.
package boardsource

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

// SourceType is the ref type carried on every vaccination board row.
const SourceType = "vaccination_drive_pen"

const (
	// walkPageSize is the wrapped read's page size per walk step (its maxLimit is 500; 100 is
	// its default and comfortably above a park-day's pen count).
	walkPageSize = 100
	// maxWalkPages bounds the park-day walk (maxWalkPages x walkPageSize rows). A park has
	// ~100-175 operational pens and a day carries a handful of drives, so a park-day past
	// this is a data defect, not a page.
	maxWalkPages = 20
	maxWalkRows  = maxWalkPages * walkPageSize
	// The Work Board must load even when the canonical process-integrity read is slow for a
	// park/day. The app-level composer records this source as degraded and serves the rest of
	// the board; waiting for the full process-integrity repository timeout recreated the
	// 5s-7s page load that surfaced as "The board could not be loaded" on admin-web.
	workBoardVaccinationPrecheckBudget = 300 * time.Millisecond
	workBoardVaccinationReadBudget     = 400 * time.Millisecond
)

// vaccinationDueWorkPrecheckSQL answers "does this park have any vaccination work on this
// business day" before the Work Board pays for the canonical process-integrity read.
//
// A goat obligation is on the day when ANY of four dates falls in it: its own due_at, its
// batch's planned_date, the planned_date of a drive assignment it is a member of, or the
// planned_date of a drive assignment of its batch in this park. Those four used to be one OR
// inside one EXISTS over obligation_instances; the OR across three joined tables left only
// tenant_id sargable, so the empty-day answer (the common "today, no drive" case) walked every
// obligation of the tenant: 86,616 rows / 133k buffers / 228 ms on the stg clone, past the 300 ms
// fail-open budget on stg itself. Each date source is now its own UNION ALL arm with its own
// index path -- the due_at arm on obligation_instances_goat_live_due_idx (000415), the others
// from the small day-bounded batch / assignment tables into obligation_instances by batch_id or
// obligation_id -- and the goat/park/protocol/status filters apply once to the union: 5 ms.
//
// $6/$7 are the business day as DATEs. dayStart is always an Asia/Kolkata midnight
// (time.ParseInLocation of the business date), so planned_date in [$6, $7) is exactly the
// legacy "planned_date at IST midnight in [$3, $4)" -- and it is sargable.
const vaccinationDueWorkPrecheckSQL = `
WITH cand AS (
  SELECT oi.target_id, oi.protocol_version_id, oi.status
  FROM obligation_instances oi
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status <> 'canceled'
    AND oi.due_at >= $3::timestamptz
    AND oi.due_at < $4::timestamptz
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status
  FROM obligation_batches ob
  JOIN obligation_instances oi
    ON oi.tenant_id = ob.tenant_id
   AND oi.batch_id = ob.batch_id
  WHERE ob.tenant_id = $1::uuid
    AND ob.planned_date >= $6::date
    AND ob.planned_date < $7::date
    AND oi.target_type = 'goat'
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status
  FROM vaccination_drive_assignments vda
  JOIN vaccination_drive_assignment_members vdam
    ON vdam.tenant_id = vda.tenant_id
   AND vdam.assignment_id = vda.assignment_id
  JOIN obligation_instances oi
    ON oi.tenant_id = vdam.tenant_id
   AND oi.obligation_id = vdam.obligation_id
  WHERE vda.tenant_id = $1::uuid
    AND vda.planned_date >= $6::date
    AND vda.planned_date < $7::date
    AND oi.target_type = 'goat'
  UNION ALL
  SELECT oi.target_id, oi.protocol_version_id, oi.status
  FROM vaccination_drive_assignments vda
  JOIN obligation_instances oi
    ON oi.tenant_id = vda.tenant_id
   AND oi.batch_id = vda.batch_id
  WHERE vda.tenant_id = $1::uuid
    AND vda.park_id = $2::uuid
    AND vda.planned_date >= $6::date
    AND vda.planned_date < $7::date
    AND oi.target_type = 'goat'
)
SELECT EXISTS (
  SELECT 1
  FROM cand c
  JOIN goats g
    ON g.tenant_id = $1::uuid
   AND g.goat_id = c.target_id
   AND g.park_id = $2::uuid
  JOIN protocol_versions pv
    ON pv.tenant_id = $1::uuid
   AND pv.protocol_version_id = c.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = $1::uuid
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  WHERE c.status <> 'canceled'
    AND ($5::boolean OR c.status <> 'completed')
)`

// ErrOwnerScopeUnresolvable is returned when the board asks for one user's rows and the
// source has no way to translate that user id into the workforce member id the wrapped read
// keys owners by. It is loud on purpose: silently serving unscoped or empty rows would be a
// fabricated answer either way.
var ErrOwnerScopeUnresolvable = errors.New("vaccination boardsource: owner scope needs a workforce member resolver")

// ErrWalkExceeded is returned when a park-day holds more rows than maxWalkRows.
var ErrWalkExceeded = errors.New("vaccination boardsource: park-day exceeds the bounded walk")

// Lister is the one method of the process-integrity repository this source depends on;
// *postgres.Repository satisfies it, and a test substitutes a fake.
type Lister interface {
	ListRows(ctx context.Context, q pidomain.Query) (pidomain.ListResult, error)
}

// RowsOnlyLister avoids recomputing the canonical summary on every bounded page.
// The Work Board obtains its summary through Counter independently.
type RowsOnlyLister interface {
	ListRowsOnly(ctx context.Context, q pidomain.Query) (pidomain.ListResult, error)
}

// Counter is the aggregate half of the process-integrity repository. The Work Board summary
// needs counts only, so using this avoids fetching and sorting a vaccination row page just to
// throw the rows away.
type Counter interface {
	CountByWorkState(ctx context.Context, q pidomain.Query) ([]pidomain.CountByWorkState, error)
}

// LiveCounter prevents a cached aggregate from pruning newly moved board rows.
type LiveCounter interface {
	CountByWorkStateLive(ctx context.Context, q pidomain.Query) ([]pidomain.CountByWorkState, error)
}

// MemberResolver answers "which workforce member is this user" for the owner lens. The
// wrapped read keys its operator by workforce_member_id, the board scopes by user id, and
// workforce_members is the one org table that joins the two.
type MemberResolver interface {
	WorkforceMemberIDForUser(ctx context.Context, tenantID, userID string) (memberID string, found bool, err error)
	// UserIDsForMembers is the reverse, batched: one read for a page's operators, so every
	// row carries owner.user_id like every other module's. Without it the web assignee picker
	// (keyed on user id) offered nobody on a vaccination-only day (live E2E 2026-09-11).
	UserIDsForMembers(ctx context.Context, tenantID string, memberIDs []string) (map[string]string, error)
}

// Source implements ports.Source over the process-integrity vaccination read.
type Source struct {
	repo    Lister
	counter Counter
	members MemberResolver
	now     func() time.Time
	// pool serves the per-animal subtask read (subtasks.go); nil until WithPool.
	pool    *pgxpool.Pool
	timeout time.Duration
}

// New constructs the source over the process-integrity repository.
func New(repo Lister) *Source {
	s := &Source{repo: repo, now: func() time.Time { return time.Now().In(biztime.DefaultLocation()) }}
	if counter, ok := repo.(Counter); ok {
		s.counter = counter
	}
	return s
}

// WithMemberResolver enables the owner (operator lens) scope.
func (s *Source) WithMemberResolver(r MemberResolver) *Source {
	s.members = r
	return s
}

// WithClock pins the as-of instant the wrapped read derives due/overdue from (tests).
func (s *Source) WithClock(now func() time.Time) *Source {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *Source) Module() domain.Module { return domain.ModuleVaccination }
func (s *Source) SourceType() string    { return SourceType }

// ListRows implements ports.Source.
func (s *Source) ListRows(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	rows, err := s.collect(ctx, q)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = domain.DefaultLimit
	}
	out := make([]domain.Row, 0, limit)
	for _, r := range rows {
		if q.AfterSourceID != "" && r.SourceID <= q.AfterSourceID {
			continue
		}
		out = append(out, r)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// CountByState implements ports.Source.
func (s *Source) CountByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	if s.counter != nil && q.OwnerUserID == "" {
		counts, err := s.countByState(ctx, q)
		if err != nil {
			return nil, err
		}
		return counts, nil
	}
	rows, err := s.collect(ctx, q)
	if err != nil {
		return nil, err
	}
	out := map[domain.WorkState]int{}
	for _, r := range rows {
		out[r.WorkState]++
	}
	return out, nil
}

func (s *Source) countByState(ctx context.Context, q ports.SourceQuery) (map[domain.WorkState]int, error) {
	dayStart, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.BusinessDate), biztime.DefaultLocation())
	if err != nil {
		return nil, fmt.Errorf("vaccination boardsource: business date %q (%v): %w", q.BusinessDate, err, domain.ErrInvalidQuery)
	}
	hasWork, err := s.hasVaccinationDueWork(ctx, q, dayStart)
	if err != nil {
		return nil, err
	}
	if !hasWork {
		return map[domain.WorkState]int{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.readBudget())
	defer cancel()
	// projection-review: membership=complete canonical vaccination rows with no next cursor; group_key=work_state; join_cardinality=each canonical grouped row contributes once; pagination=only a complete first page is counted and overflow uses the full SQL aggregate; scope=tenant/park/business date from the canonical query and requested work states filtered below, while owner-scoped summaries use collect.
	// A complete canonical first page has exactly the same membership and state
	// grain as COUNT(*) GROUP BY work_state. Share it only within this bundled
	// request. Overflow falls back to the full aggregate; never count a partial page.
	piq := processIntegrityQuery(q, dayStart, s.now())
	if rowsOnly, ok := s.repo.(RowsOnlyLister); ok && ports.HasRequestReadMemo(ctx) {
		result, readErr := s.firstCanonicalPage(ctx, piq, rowsOnly.ListRowsOnly)
		if readErr == nil && (result.NextCursor == nil || *result.NextCursor == "") {
			counts := map[domain.WorkState]int{}
			for _, row := range result.Rows {
				if row.Category == pidomain.CategoryVaccination && wantsState(q.WorkStates, row.WorkState) {
					counts[domain.WorkState(row.WorkState)]++
				}
			}
			return counts, nil
		}
	}
	countRows := s.counter.CountByWorkState
	if live, ok := s.counter.(LiveCounter); ok {
		countRows = live.CountByWorkStateLive
	}
	counts, err := countRows(ctx, piq)
	if err != nil {
		return nil, fmt.Errorf("vaccination boardsource count: %w", err)
	}
	want := map[domain.WorkState]struct{}{}
	for _, state := range q.WorkStates {
		want[state] = struct{}{}
	}
	out := map[domain.WorkState]int{}
	for _, count := range counts {
		state := domain.WorkState(count.WorkState)
		if len(want) > 0 {
			if _, ok := want[state]; !ok {
				continue
			}
		}
		out[state] += int(count.Count)
	}
	return out, nil
}

// collect walks the wrapped read for one tenant, park and business day, applies the owner and
// state scope exactly, and returns the rows sorted by source id.
func (s *Source) collect(ctx context.Context, q ports.SourceQuery) ([]domain.Row, error) {
	dayStart, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.BusinessDate), biztime.DefaultLocation())
	if err != nil {
		return nil, fmt.Errorf("vaccination boardsource: business date %q (%v): %w", q.BusinessDate, err, domain.ErrInvalidQuery)
	}
	hasWork, err := s.hasVaccinationDueWork(ctx, q, dayStart)
	if err != nil {
		return nil, err
	}
	if !hasWork {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.readBudget())
	defer cancel()
	memberID, ownsNothing, err := s.ownerMember(ctx, q)
	if err != nil || ownsNothing {
		return nil, err
	}

	piq := processIntegrityQuery(q, dayStart, s.now())
	if memberID != "" {
		// The wrapped $8 owner filter matches operator OR park head OR verifier OR escalation
		// owner; it is a superset, narrowed below to the row's own operator.
		owner := memberID
		piq.OwnerID = &owner
	}

	listRows := s.repo.ListRows
	if rowsOnly, ok := s.repo.(RowsOnlyLister); ok {
		listRows = rowsOnly.ListRowsOnly
	}
	out := make([]domain.Row, 0, walkPageSize)
	// A bounded PAGE walk, not a per-row fan-out: at most maxWalkPages keyset pages of
	// walkPageSize over one park-day (pens x drives on one day); see the KEYSET NOTE in the
	// package doc. The wrapped read's own cursor advances piq each page.
	for page := 0; page < maxWalkPages; page++ {
		res, err := s.firstCanonicalPage(ctx, piq, listRows) // scale-guard:ignore: bounded park-day page walk (<= maxWalkPages keyset pages), one read per PAGE not per row; the wrapped read has no row_id keyset yet (KEYSET NOTE)
		if err != nil {
			return nil, fmt.Errorf("vaccination boardsource: %w", err)
		}
		if len(res.Rows) == 0 {
			break
		}
		for _, pr := range res.Rows {
			if pr.Category != pidomain.CategoryVaccination {
				continue
			}
			if memberID != "" && (pr.Owner.OperatorID == nil || *pr.Owner.OperatorID != memberID) {
				continue
			}
			if !wantsState(q.WorkStates, pr.WorkState) {
				continue
			}
			out = append(out, mapRow(pr))
			if len(out) > maxWalkRows {
				return nil, ErrWalkExceeded
			}
		}
		if res.NextCursor == nil || *res.NextCursor == "" {
			break
		}
		cur, err := pidomain.DecodeCursor(*res.NextCursor)
		if err != nil {
			return nil, fmt.Errorf("vaccination boardsource: walk cursor: %w", err)
		}
		if page == maxWalkPages-1 {
			return nil, ErrWalkExceeded
		}
		next := piq
		next.Cursor = &cur
		piq = next
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceID < out[j].SourceID })
	if err := s.fillOwnerUserIDs(ctx, q.TenantID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ownerMember resolves the owner lens to the workforce member id the wrapped read keys
// operators by. ownsNothing is true for a user with no workforce profile: the wrapped read keys
// every operator through workforce_members, so nothing can be theirs.
func (s *Source) ownerMember(ctx context.Context, q ports.SourceQuery) (memberID string, ownsNothing bool, err error) {
	if q.OwnerUserID == "" {
		return "", false, nil
	}
	if s.members == nil {
		return "", false, ErrOwnerScopeUnresolvable
	}
	// Every lane of one page asks the same question; the answer is memoized per request (and
	// primed into the summary's first batch by PrimeStatements).
	member, err := ports.RequestRead(ctx, memberKey{s, q.TenantID, q.OwnerUserID}, func(ctx context.Context) (memberResult, error) {
		id, found, err := s.members.WorkforceMemberIDForUser(ctx, q.TenantID, q.OwnerUserID)
		return memberResult{id, found}, err
	})
	id, found := member.id, member.found
	if err != nil {
		return "", false, fmt.Errorf("vaccination boardsource: resolve owner: %w", err)
	}
	if !found {
		return "", true, nil
	}
	return id, false, nil
}

// FindRowByID implements ports.SingleRowSource: the row a subtask drill or a flag names, read
// with ONE row_id-keyed canonical statement ($12 row_id) on the same park-day window, owner
// lens and category the board's ListRows applies. It skips the due-work precheck (a keyed
// read of one row is already cheap) and the park-day page walk FindRow used to pay
// (precheck + up to maxWalkPages canonical pages) before a 5 ms subtask read.
func (s *Source) FindRowByID(ctx context.Context, q ports.SourceQuery, sourceID string) (domain.Row, bool, error) {
	if pidomain.ValidateRowID(sourceID) != nil {
		return domain.Row{}, false, nil
	}
	dayStart, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.BusinessDate), biztime.DefaultLocation())
	if err != nil {
		return domain.Row{}, false, fmt.Errorf("vaccination boardsource: business date %q (%v): %w", q.BusinessDate, err, domain.ErrInvalidQuery)
	}
	ctx, cancel := context.WithTimeout(ctx, s.readBudget())
	defer cancel()
	memberID, ownsNothing, err := s.ownerMember(ctx, q)
	if err != nil || ownsNothing {
		return domain.Row{}, false, err
	}
	piq := processIntegrityQuery(q, dayStart, s.now())
	if memberID != "" {
		owner := memberID
		piq.OwnerID = &owner
	}
	rowID := sourceID
	piq.RowID = &rowID
	listRows := s.repo.ListRows
	if rowsOnly, ok := s.repo.(RowsOnlyLister); ok {
		listRows = rowsOnly.ListRowsOnly
	}
	res, err := listRows(ctx, piq)
	if err != nil {
		return domain.Row{}, false, fmt.Errorf("vaccination boardsource: find row: %w", err)
	}
	for _, pr := range res.Rows {
		if pr.RowID != sourceID || pr.Category != pidomain.CategoryVaccination {
			continue
		}
		if memberID != "" && (pr.Owner.OperatorID == nil || *pr.Owner.OperatorID != memberID) {
			continue
		}
		out := []domain.Row{mapRow(pr)}
		if err := s.fillOwnerUserIDs(ctx, q.TenantID, out); err != nil {
			return domain.Row{}, false, err
		}
		return out[0], true, nil
	}
	return domain.Row{}, false, nil
}

// The source query always uses one fixed page size and canonical all-state
// membership; lane filters are applied after reading. Owner and cursor retain
// their exact scope, and the Source pointer isolates different repository instances.
type canonicalPageKey struct {
	source                   *Source
	tenant, park, day, owner string
}

func (s *Source) firstCanonicalPage(ctx context.Context, q pidomain.Query, read func(context.Context, pidomain.Query) (pidomain.ListResult, error)) (pidomain.ListResult, error) {
	if q.Cursor != nil {
		return read(ctx, q)
	}
	key := canonicalPageKey{source: s, tenant: q.TenantID, day: q.DueBefore.Format("2006-01-02")}
	if q.ParkID != nil {
		key.park = *q.ParkID
	}
	if q.OwnerID != nil {
		key.owner = *q.OwnerID
	}
	return ports.RequestRead(ctx, key, func(ctx context.Context) (pidomain.ListResult, error) { return read(ctx, q) })
}

func (s *Source) readBudget() time.Duration {
	if s.timeout > 0 && s.timeout < workBoardVaccinationReadBudget {
		return s.timeout
	}
	return workBoardVaccinationReadBudget
}

func processIntegrityQuery(q ports.SourceQuery, dayStart, asOf time.Time) pidomain.Query {
	category := pidomain.CategoryVaccination
	park := q.ParkID
	return pidomain.Query{
		TenantID: q.TenantID,
		Category: &category,
		ParkID:   &park,
		// Both bounds inside the business day: the repository normalises DueAfter to the IST
		// day start and DueBefore to that day's last instant, so the window IS the day.
		DueAfter:  &dayStart,
		DueBefore: dayStart,
		AsOf:      asOf,
		// Completed pens belong in the Done lane; without this the read hides them once
		// their due date is behind the closed-history age.
		IncludeCompleted: true,
		Limit:            walkPageSize,
	}
}

type dueWorkKey struct {
	source            *Source
	tenant, park, day string
	includeCompleted  bool
}

func (s *Source) hasVaccinationDueWork(ctx context.Context, q ports.SourceQuery, dayStart time.Time) (bool, error) {
	if s.pool == nil {
		return true, nil
	}
	key, _ := s.precheck(ctx, q, dayStart)
	return ports.RequestRead(ctx, key, func(ctx context.Context) (bool, error) {
		ctx, cancel := context.WithTimeout(ctx, s.precheckBudget())
		defer cancel()
		var ok bool
		dayEnd := dayStart.AddDate(0, 0, 1)
		bound := sqlbind.MustBind(vaccinationDueWorkPrecheckSQL, q.TenantID, q.ParkID, dayStart, dayEnd, key.includeCompleted,
			dayStart.Format("2006-01-02"), dayEnd.Format("2006-01-02"))
		err := s.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(&ok)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
				// The precheck is only a skip optimization. If it is slow, fail open so a valid
				// vaccination card cannot be hidden before the bounded canonical read gets a chance.
				return true, nil
			}
			return false, fmt.Errorf("vaccination boardsource: due-work precheck: %w", err)
		}
		return ok, nil
	})
}

// precheck is the due-work precheck's memo key and bound statement for one request (the
// statement PrimeStatements batches; hasVaccinationDueWork binds the same one itself).
func (s *Source) precheck(ctx context.Context, q ports.SourceQuery, dayStart time.Time) (dueWorkKey, sqlbind.BoundQuery) {
	includeCompleted := len(q.WorkStates) == 0
	for _, state := range q.WorkStates {
		if state == domain.WorkStateCompleted {
			includeCompleted = true
			break
		}
	}
	if ports.HasRequestReadMemo(ctx) {
		includeCompleted = true
	}
	key := dueWorkKey{source: s, tenant: q.TenantID, park: q.ParkID, day: q.BusinessDate, includeCompleted: includeCompleted}
	dayEnd := dayStart.AddDate(0, 0, 1)
	bound := sqlbind.MustBind(vaccinationDueWorkPrecheckSQL, q.TenantID, q.ParkID, dayStart, dayEnd, includeCompleted,
		dayStart.Format("2006-01-02"), dayEnd.Format("2006-01-02"))
	return key, bound
}

type memberKey struct {
	source       *Source
	tenant, user string
}

type memberResult struct {
	id    string
	found bool
}

// MemberStatementer is the optional MemberResolver capability that hands the owner lookup to a
// shared batch instead of sending it alone. *PoolMemberResolver implements it.
type MemberStatementer interface {
	MemberStatement(tenantID, userID string, id *string, found *bool) ports.Statement
}

// PrimeStatements implements ports.PrimingSource: the due-work precheck and, for an owner lens,
// the user -> workforce member lookup -- the two reads that gate the canonical read -- ride the
// board summary's first batch, seeded into the request memo under the keys hasVaccinationDueWork
// and ownerMember look up. The canonical read itself keeps its own round trip and its own
// read budget (a pipelined batch shares one deadline, and this is the read that can be slow).
func (s *Source) PrimeStatements(ctx context.Context, q ports.SourceQuery) ([]ports.Statement, error) {
	if s.pool == nil || !ports.HasRequestReadMemo(ctx) {
		return nil, nil
	}
	dayStart, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(q.BusinessDate), biztime.DefaultLocation())
	if err != nil {
		return nil, nil // the source's own read reports the bad date
	}
	key, bound := s.precheck(ctx, q, dayStart)
	stmts := []ports.Statement{{
		Query: bound,
		Read: func(rows ports.ResultRows) error {
			var ok bool
			if !rows.Next() {
				if err := rows.Err(); err != nil {
					return err
				}
				return errors.New("vaccination boardsource: due-work precheck returned no row")
			}
			if err := rows.Scan(&ok); err != nil {
				return err
			}
			ports.SeedRequestRead(ctx, key, ok)
			return nil
		},
	}}
	if q.OwnerUserID == "" {
		return stmts, nil
	}
	resolver, ok := s.members.(MemberStatementer)
	if !ok {
		return stmts, nil
	}
	var member memberResult
	st := resolver.MemberStatement(q.TenantID, q.OwnerUserID, &member.id, &member.found)
	read := st.Read
	st.Read = func(rows ports.ResultRows) error {
		if err := read(rows); err != nil {
			return err
		}
		ports.SeedRequestRead(ctx, memberKey{s, q.TenantID, q.OwnerUserID}, member)
		return nil
	}
	return append(stmts, st), nil
}

func (s *Source) precheckBudget() time.Duration {
	if s.timeout > 0 && s.timeout < workBoardVaccinationPrecheckBudget {
		return s.timeout
	}
	return workBoardVaccinationPrecheckBudget
}

// fillOwnerUserIDs resolves the page's workforce member ids to user ids in ONE batched read,
// so the owner an operator lens or an assignee filter keys on is present on the wire.
func (s *Source) fillOwnerUserIDs(ctx context.Context, tenantID string, rows []domain.Row) error {
	if s.members == nil {
		return nil
	}
	want := map[string]struct{}{}
	for _, r := range rows {
		if r.Owner.WorkforceMemberID != "" && r.Owner.UserID == "" {
			want[r.Owner.WorkforceMemberID] = struct{}{}
		}
	}
	if len(want) == 0 {
		return nil
	}
	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	byMember, err := s.members.UserIDsForMembers(ctx, tenantID, ids)
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].Owner.UserID == "" {
			rows[i].Owner.UserID = byMember[rows[i].Owner.WorkforceMemberID]
		}
	}
	return nil
}

// mapRow is the one place a process-integrity row becomes a board row.
func mapRow(pr pidomain.Row) domain.Row {
	partition := ""
	if pr.PartitionLabel != nil {
		partition = *pr.PartitionLabel
	}
	// Park, pen and the composed display are copied verbatim: the wrapped read already
	// resolved them through the operational-location convention, and recomposing here
	// would be a second implementation of the same rule.
	pen := domain.Pen{ShedID: pr.ShedID, ShedName: pr.ShedName, PartitionLabel: partition, Display: pr.OperationalLocationDisplay}

	// OWNER IDENTITY DOMAIN. The wrapped SQL emits operator_id as
	// `operator.workforce_member_id::text` (workforce_members joined on
	// COALESCE(conducted_by, assigned_to)), so OperatorID is a WORKFORCE MEMBER id, never a
	// user id. It lands on Owner.WorkforceMemberID; the separate OperatorUserID
	// comes from that same canonical workforce row and never guesses identity. No owner -> missing; the read never invents an operator fallback.
	owner := domain.Owner{}
	ownerState := domain.OwnerStateMissing
	if pr.Owner.OperatorID != nil && *pr.Owner.OperatorID != "" {
		owner.WorkforceMemberID = *pr.Owner.OperatorID
		if pr.Owner.OperatorUserID != nil {
			owner.UserID = *pr.Owner.OperatorUserID
		}
		if pr.Owner.OperatorName != nil {
			owner.Name = *pr.Owner.OperatorName
		}
		ownerState = domain.OwnerStateAssigned
	}

	// Copy for the card: the vaccine/dose label, never the raw protocol name or dose code.
	// ControlTowerDoseLabel composes through vaccination/domain.DoseDisplayLabel, the single
	// backend-owned label source. The row's DriveName is deliberately NOT used: the
	// process-integrity read synthesises it as "<protocol name> - <dose label>" whenever SQL
	// carries no drive name, which put "Preventive Care Vaccination Matrix - HS adult course
	// dose 1" on every card (2026-09-10 E2E) -- a banned raw protocol token on a screen.
	doseLabel := pidomain.ControlTowerDoseLabel(pr.ProtocolName, pr.DoseCode)
	title := doseLabel
	if pen.Display != "" {
		title += " · " + pen.Display
	}
	subtitle := doseLabel + " · " + animalsText(pr.ExpectedCount)

	pending := pr.ExpectedCount - pr.CompletedCount
	if pending < 0 {
		pending = 0
	}
	// The shed execution page exists per shed and reads partition_label for the pen; as_of
	// must be a live RFC3339 instant there, so the link shows the pen's current state.
	href := ""
	if pr.ShedID != "" {
		href = "/vaccination/execution/sheds/" + url.PathEscape(pr.ShedID) + "?scope_mode=park&park=" + url.QueryEscape(pr.ParkID)
		if partition != "" {
			href += "&partition_label=" + url.QueryEscape(partition)
		}
	}
	due := pr.DueAt
	return domain.Row{
		Module: domain.ModuleVaccination, SourceType: SourceType, SourceID: pr.RowID,
		ParkID: pr.ParkID, ParkName: pr.ParkName, Pen: pen,
		BusinessDate: biztime.BusinessDate(pr.DueAt), DueAt: &due,
		ClockLabel: "Due " + biztime.FarmDate(pr.DueAt),
		WorkState:  pr.WorkState, Severity: pr.Severity,
		Owner: owner, OwnerState: ownerState,
		Title: title, Subtitle: subtitle,
		Counts: domain.Counts{Done: pr.CompletedCount, Pending: pending, NeedsAttention: pr.RejectedCount},
		Href:   href,
	}.Finalize()
}

func animalsText(n int) string {
	if n == 1 {
		return "1 animal"
	}
	return strconv.Itoa(n) + " animals"
}

func wantsState(states []domain.WorkState, s domain.WorkState) bool {
	if len(states) == 0 {
		return true
	}
	for _, x := range states {
		if x == s {
			return true
		}
	}
	return false
}

// PoolMemberResolver answers the owner lens with one primary-key-shaped probe on
// workforce_members (an org table every module may read): tenant + user -> member.
type PoolMemberResolver struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

// NewPoolMemberResolver constructs the resolver.
func NewPoolMemberResolver(pool *pgxpool.Pool, timeout time.Duration) *PoolMemberResolver {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &PoolMemberResolver{pool: pool, timeout: timeout}
}

const memberForUserSQL = `
SELECT workforce_member_id::text
FROM workforce_members
WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
ORDER BY workforce_member_id
LIMIT 1`

// MemberStatement implements MemberStatementer: WorkforceMemberIDForUser's statement, for a
// shared batch. No row is "not found", as ErrNoRows is there.
func (r *PoolMemberResolver) MemberStatement(tenantID, userID string, id *string, found *bool) ports.Statement {
	return ports.Statement{
		Query: sqlbind.MustBind(memberForUserSQL, tenantID, userID),
		Read: func(rows ports.ResultRows) error {
			if !rows.Next() {
				*found = false
				return rows.Err()
			}
			*found = true
			return rows.Scan(id)
		},
	}
}

// WorkforceMemberIDForUser implements MemberResolver. Index: workforce_members' tenant+user
// lookup (the same join the wrapped read and the weighing source use).
func (r *PoolMemberResolver) WorkforceMemberIDForUser(ctx context.Context, tenantID, userID string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id string
	bound := sqlbind.MustBind(memberForUserSQL, tenantID, userID)
	err := r.pool.QueryRow(ctx, bound.SQL(), bound.Args()...).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("vaccination boardsource: member for user: %w", err)
	}
	return id, true, nil
}

// UserIDsForMembers implements MemberResolver: one indexed read over the page's member ids.
func (r *PoolMemberResolver) UserIDsForMembers(ctx context.Context, tenantID string, memberIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(memberIDs) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
SELECT workforce_member_id::text, COALESCE(user_id::text, '')
FROM workforce_members
WHERE tenant_id = $1::uuid AND workforce_member_id = ANY($2::uuid[])`, tenantID, memberIDs)
	if err != nil {
		return nil, fmt.Errorf("vaccination boardsource: users for members: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var member, user string
		if err := rows.Scan(&member, &user); err != nil {
			return nil, fmt.Errorf("vaccination boardsource: users for members scan: %w", err)
		}
		if user != "" {
			out[member] = user
		}
	}
	return out, rows.Err()
}
