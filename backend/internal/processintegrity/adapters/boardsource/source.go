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

const vaccinationDueWorkPrecheckSQL = `
	SELECT EXISTS (
  SELECT 1
  FROM obligation_instances oi
  LEFT JOIN obligation_batches ob
    ON ob.tenant_id = oi.tenant_id
   AND ob.batch_id = oi.batch_id
  JOIN protocol_versions pv
    ON pv.tenant_id = oi.tenant_id
   AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd
    ON pd.tenant_id = oi.tenant_id
   AND pd.protocol_id = pv.protocol_id
   AND pd.category = 'vaccination'
  JOIN goats g
    ON g.tenant_id = oi.tenant_id
   AND g.goat_id = oi.target_id
   AND g.park_id = $2::uuid
	  WHERE oi.tenant_id = $1::uuid
	    AND oi.target_type = 'goat'
	    AND oi.status <> 'canceled'
	    AND ($5::boolean OR oi.status <> 'completed')
	    AND (
	      (oi.due_at >= $3::timestamptz AND oi.due_at < $4::timestamptz)
	      OR ((ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
	        AND (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz)
	      OR EXISTS (
	        SELECT 1
	        FROM vaccination_drive_assignment_members vdam
	        JOIN vaccination_drive_assignments vda
	          ON vda.tenant_id = vdam.tenant_id
	         AND vda.assignment_id = vdam.assignment_id
	        WHERE vdam.tenant_id = oi.tenant_id
	          AND vdam.obligation_id = oi.obligation_id
	          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
	          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz
	      )
	      OR EXISTS (
	        SELECT 1
	        FROM vaccination_drive_assignments vda
	        WHERE vda.tenant_id = oi.tenant_id
	          AND vda.batch_id = oi.batch_id
	          AND vda.park_id = $2::uuid
	          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
	          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz
	      )
	    )
	  LIMIT 1
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
	countRows := s.counter.CountByWorkState
	if live, ok := s.counter.(LiveCounter); ok {
		countRows = live.CountByWorkStateLive
	}
	counts, err := countRows(ctx, processIntegrityQuery(q, dayStart, s.now()))
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
	memberID := ""
	if q.OwnerUserID != "" {
		if s.members == nil {
			return nil, ErrOwnerScopeUnresolvable
		}
		id, found, err := s.members.WorkforceMemberIDForUser(ctx, q.TenantID, q.OwnerUserID)
		if err != nil {
			return nil, fmt.Errorf("vaccination boardsource: resolve owner: %w", err)
		}
		if !found {
			// A user with no workforce profile owns no vaccination pen: the wrapped read keys
			// every operator through workforce_members, so nothing can be theirs.
			return nil, nil
		}
		memberID = id
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
		res, err := listRows(ctx, piq) // scale-guard:ignore: bounded park-day page walk (<= maxWalkPages keyset pages), one read per PAGE not per row; the wrapped read has no row_id keyset yet (KEYSET NOTE)
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

func (s *Source) hasVaccinationDueWork(ctx context.Context, q ports.SourceQuery, dayStart time.Time) (bool, error) {
	if s.pool == nil {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.precheckBudget())
	defer cancel()
	dayEnd := dayStart.AddDate(0, 0, 1)
	includeCompleted := len(q.WorkStates) == 0
	for _, state := range q.WorkStates {
		if state == domain.WorkStateCompleted {
			includeCompleted = true
			break
		}
	}
	var ok bool
	err := s.pool.QueryRow(ctx, vaccinationDueWorkPrecheckSQL, q.TenantID, q.ParkID, dayStart, dayEnd, includeCompleted).Scan(&ok)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// The precheck is only a skip optimization. If it is slow, fail open so a valid
			// vaccination card cannot be hidden before the bounded canonical read gets a chance.
			return true, nil
		}
		return false, fmt.Errorf("vaccination boardsource: due-work precheck: %w", err)
	}
	return ok, nil
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
	// user id. It lands on Owner.WorkforceMemberID; Owner.UserID is deliberately left blank
	// rather than guessed. No owner -> missing; the read never invents an operator fallback.
	owner := domain.Owner{}
	ownerState := domain.OwnerStateMissing
	if pr.Owner.OperatorID != nil && *pr.Owner.OperatorID != "" {
		owner.WorkforceMemberID = *pr.Owner.OperatorID
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

// WorkforceMemberIDForUser implements MemberResolver. Index: workforce_members' tenant+user
// lookup (the same join the wrapped read and the weighing source use).
func (r *PoolMemberResolver) WorkforceMemberIDForUser(ctx context.Context, tenantID, userID string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id string
	err := r.pool.QueryRow(ctx, `
SELECT workforce_member_id::text
FROM workforce_members
WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
ORDER BY workforce_member_id
LIMIT 1`, tenantID, userID).Scan(&id)
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
