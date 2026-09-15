package boardsource

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

const (
	vsTenant   = "00000000-0000-4000-8000-000000000001"
	vsPark     = "00000000-0000-4000-8000-000000003001"
	vsShed     = "00000000-0000-4000-8000-000000003101"
	vsUser     = "00000000-0000-4000-8000-000000000301"
	vsMember   = "00000000-0000-4000-8000-000000000401"
	vsOtherMem = "00000000-0000-4000-8000-000000000402"
	vsDate     = "2026-09-10"

	rowA = "batch:71000000-0000-4000-8000-000000000012:rule:71000000-0000-4000-8000-000000000009:shed:00000000-0000-4000-8000-000000003101"
	rowB = "obligation:71000000-0000-4000-8000-000000000013"
	rowC = "obligation:71000000-0000-4000-8000-000000000020"
	rowD = "obligation:71000000-0000-4000-8000-000000000023"
)

// fakeLister serves a fixed row set in the wrapped read's own order (NOT row_id order) across
// two pages, and records every Query it was asked, so the test can assert the binding.
type fakeLister struct {
	pages   [][]pidomain.Row
	queries []pidomain.Query
}

func (f *fakeLister) ListRows(_ context.Context, q pidomain.Query) (pidomain.ListResult, error) {
	f.queries = append(f.queries, q)
	page := 0
	if q.Cursor != nil {
		page = q.Cursor.SortPriority // the fake encodes the page number in the cursor
	}
	if page >= len(f.pages) {
		return pidomain.ListResult{}, nil
	}
	res := pidomain.ListResult{Rows: f.pages[page]}
	if page+1 < len(f.pages) {
		enc, err := pidomain.EncodeCursor(pidomain.Cursor{SortPriority: page + 1, DueAt: time.Now(), RowID: rowB})
		if err != nil {
			return pidomain.ListResult{}, err
		}
		res.NextCursor = &enc
	}
	return res, nil
}

type slowCounterLister struct {
	listCalls  int
	countCalls int
}

func (f *slowCounterLister) ListRows(ctx context.Context, q pidomain.Query) (pidomain.ListResult, error) {
	f.listCalls++
	<-ctx.Done()
	return pidomain.ListResult{}, ctx.Err()
}

func (f *slowCounterLister) CountByWorkState(ctx context.Context, q pidomain.Query) ([]pidomain.CountByWorkState, error) {
	f.countCalls++
	<-ctx.Done()
	return nil, ctx.Err()
}

type fakeMembers struct{ byUser map[string]string }

func (f fakeMembers) WorkforceMemberIDForUser(_ context.Context, _ string, userID string) (string, bool, error) {
	id, ok := f.byUser[userID]
	return id, ok, nil
}

func (f fakeMembers) UserIDsForMembers(_ context.Context, _ string, memberIDs []string) (map[string]string, error) {
	out := map[string]string{}
	for user, member := range f.byUser {
		for _, id := range memberIDs {
			if id == member {
				out[member] = user
			}
		}
	}
	return out, nil
}

func str(s string) *string { return &s }

func dueAt() time.Time {
	return time.Date(2026, 9, 10, 9, 0, 0, 0, biztime.DefaultLocation())
}

func piRow(rowID, dose string, state pidomain.WorkState, sev pidomain.Severity, operator, operatorName *string, expected, completed, rejected int) pidomain.Row {
	return pidomain.Row{
		Category: pidomain.CategoryVaccination, RowID: rowID,
		ParkID: vsPark, ParkName: "Coimbatore", ShedID: vsShed, ShedName: "Godel 1", PartitionLabel: str("Part 3"),
		OperationalLocationDisplay: "Godel 1 - Part 3",
		ProtocolName:               "Preventive Care Vaccination Matrix", DoseCode: dose,
		DueAt: dueAt(), ExpectedCount: expected, CompletedCount: completed, RejectedCount: rejected,
		WorkState: state, Severity: sev,
		Owner: pidomain.Owner{OperatorID: operator, OperatorName: operatorName, ParkHeadID: str(vsMember), ParkHeadName: str("Head")},
	}
}

func fixture() *fakeLister {
	driveRow := piRow(rowA, "et_tt_adult_w1", pidomain.WorkStateDue, pidomain.SeverityWatch, str(vsMember), str("Dinakar"), 40, 12, 0)
	driveRow.DriveName = str("CBE adult drive")
	// Owned by ANOTHER member, but the park head is vsMember: the wrapped owner filter would
	// return this row for vsMember, and the source must drop it.
	headOnly := piRow(rowC, "ppr_booster", pidomain.WorkStateOverdue, pidomain.SeverityAtRisk, str(vsOtherMem), str("Other"), 10, 0, 2)
	unowned := piRow(rowB, "goat_pox_first", pidomain.WorkStateCompleted, pidomain.SeverityOK, nil, nil, 5, 5, 0)
	completedOver := piRow(rowD, "hs_kid_w2", pidomain.WorkStateVerificationPending, pidomain.SeverityOK, str(vsMember), str("Dinakar"), 3, 4, 0)
	feed := piRow("feed_projection_exception:71000000-0000-4000-8000-000000000021", "", pidomain.WorkStateBlocked, pidomain.SeverityBroken, nil, nil, 0, 0, 0)
	feed.Category = pidomain.CategoryFeedDirection
	return &fakeLister{pages: [][]pidomain.Row{{headOnly, driveRow}, {unowned, completedOver, feed}}}
}

func query(owner string, states ...domain.WorkState) ports.SourceQuery {
	return ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate, OwnerUserID: owner, WorkStates: states, Limit: 50}
}

func TestVaccinationBoardMapsRowsAndNeverLeaksDoseCodes(t *testing.T) {
	lister := fixture()
	src := New(lister).WithClock(func() time.Time { return dueAt().Add(3 * time.Hour) })

	rows, err := src.ListRows(context.Background(), query(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("4 vaccination rows expected (the feed exception is not this source's), got %d", len(rows))
	}
	// Sorted by source id, not by the wrapped read's order.
	for i := 1; i < len(rows); i++ {
		if rows[i-1].SourceID >= rows[i].SourceID {
			t.Fatalf("rows not ordered by source id: %s before %s", rows[i-1].SourceID, rows[i].SourceID)
		}
	}
	got := map[string]domain.Row{}
	for _, r := range rows {
		got[r.SourceID] = r
	}

	drive := got[rowA]
	if drive.Module != domain.ModuleVaccination || drive.SourceType != SourceType || drive.RowKey != "vaccination|"+SourceType+"|"+rowA {
		t.Errorf("identity %s/%s/%s", drive.Module, drive.SourceType, drive.RowKey)
	}
	if drive.Title != "ET+TT adult course dose 1 · Godel 1 - Part 3" || drive.Subtitle != "ET+TT adult course dose 1 · 40 animals" {
		t.Errorf("drive copy title=%q subtitle=%q", drive.Title, drive.Subtitle)
	}
	if drive.Href != "/vaccination/execution/sheds/"+vsShed+"?scope_mode=park&park="+vsPark+"&partition_label=Part+3" {
		t.Errorf("drive href %q", drive.Href)
	}
	if drive.WorkState != domain.WorkStateDue || drive.Lane != domain.LaneToDo || drive.Severity != domain.SeverityWatch {
		t.Errorf("drive state %s/%s/%s copied verbatim?", drive.WorkState, drive.Lane, drive.Severity)
	}
	if drive.Pen != (domain.Pen{ShedID: vsShed, ShedName: "Godel 1", PartitionLabel: "Part 3", Display: "Godel 1 - Part 3"}) || drive.ParkID != vsPark || drive.ParkName != "Coimbatore" {
		t.Errorf("pen/park %+v %s %s", drive.Pen, drive.ParkID, drive.ParkName)
	}
	if drive.BusinessDate != vsDate || drive.DueAt == nil || !drive.DueAt.Equal(dueAt()) || drive.ClockLabel != "Due 10/09/2026" {
		t.Errorf("clock %s %v %q", drive.BusinessDate, drive.DueAt, drive.ClockLabel)
	}
	// OperatorID is a workforce member id: it lands on WorkforceMemberID and UserID stays blank.
	if drive.Owner != (domain.Owner{WorkforceMemberID: vsMember, Name: "Dinakar"}) || drive.OwnerState != domain.OwnerStateAssigned {
		t.Errorf("owner %+v %s", drive.Owner, drive.OwnerState)
	}
	if drive.Counts != (domain.Counts{Done: 12, Pending: 28}) {
		t.Errorf("drive counts %+v", drive.Counts)
	}

	// No drive name: the vaccine label heads the title; no operator: owner missing, never a
	// park-head fallback.
	unowned := got[rowB]
	if unowned.Title != "Goat Pox · Godel 1 - Part 3" || unowned.Subtitle != "Goat Pox · 5 animals" {
		t.Errorf("unowned copy title=%q subtitle=%q", unowned.Title, unowned.Subtitle)
	}
	if unowned.OwnerState != domain.OwnerStateMissing || unowned.Owner != (domain.Owner{}) || unowned.Lane != domain.LaneDone {
		t.Errorf("unowned owner %+v %s lane %s", unowned.Owner, unowned.OwnerState, unowned.Lane)
	}
	// Rejections need attention; pending never goes negative.
	if got[rowC].Counts != (domain.Counts{Done: 0, Pending: 10, NeedsAttention: 2}) || got[rowC].Subtitle != "PPR · Booster · 10 animals" {
		t.Errorf("rejected counts %+v subtitle %q", got[rowC].Counts, got[rowC].Subtitle)
	}
	if got[rowD].Counts != (domain.Counts{Done: 4, Pending: 0}) || got[rowD].Lane != domain.LaneInReview {
		t.Errorf("over-complete counts %+v lane %s", got[rowD].Counts, got[rowD].Lane)
	}

	// The raw-token firewall: no dose code, no protocol family name, no "shed" on a card.
	for _, r := range rows {
		for _, s := range []string{r.Title, r.Subtitle, r.ClockLabel} {
			lower := strings.ToLower(s)
			for _, banned := range []string{"et_tt", "ppr_booster", "goat_pox", "hs_kid", "_w1", "_w2", "vaccination matrix", "shed"} {
				if strings.Contains(lower, banned) {
					t.Errorf("visible copy %q leaks %q", s, banned)
				}
			}
		}
	}
}

func TestVaccinationBoardBindsTheWrappedReadToOneParkDay(t *testing.T) {
	lister := fixture()
	asOf := dueAt().Add(3 * time.Hour)
	src := New(lister).WithClock(func() time.Time { return asOf })
	if _, err := src.ListRows(context.Background(), query("")); err != nil {
		t.Fatal(err)
	}
	if len(lister.queries) != 2 {
		t.Fatalf("the walk should ask for both pages, got %d queries", len(lister.queries))
	}
	dayStart := time.Date(2026, 9, 10, 0, 0, 0, 0, biztime.DefaultLocation())
	for i, q := range lister.queries {
		if q.TenantID != vsTenant || q.ParkID == nil || *q.ParkID != vsPark || q.Category == nil || *q.Category != pidomain.CategoryVaccination {
			t.Errorf("query %d scope tenant=%s park=%v category=%v", i, q.TenantID, q.ParkID, q.Category)
		}
		if q.DueAfter == nil || !q.DueAfter.Equal(dayStart) || !q.DueBefore.Equal(dayStart) {
			t.Errorf("query %d must bound both ends to the business day: after=%v before=%v", i, q.DueAfter, q.DueBefore)
		}
		if !q.IncludeCompleted || q.OwnerID != nil || !q.AsOf.Equal(asOf) || q.Limit != walkPageSize {
			t.Errorf("query %d flags include_completed=%v owner=%v as_of=%v limit=%d", i, q.IncludeCompleted, q.OwnerID, q.AsOf, q.Limit)
		}
	}
	if lister.queries[0].Cursor != nil || lister.queries[1].Cursor == nil {
		t.Fatalf("the second page must carry the wrapped read's cursor")
	}
}

func TestVaccinationBoardKeysetStateFilterAndCounts(t *testing.T) {
	src := New(fixture()).WithClock(func() time.Time { return dueAt() })
	ctx := context.Background()

	seen := map[string]bool{}
	after := ""
	for i := 0; i < 6; i++ {
		q := query("")
		q.Limit = 2
		q.AfterSourceID = after
		page, err := src.ListRows(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) > 2 {
			t.Fatalf("limit not honoured: %d", len(page))
		}
		for _, r := range page {
			if r.SourceID <= after || seen[r.SourceID] {
				t.Fatalf("keyset broken at %s after %s", r.SourceID, after)
			}
			seen[r.SourceID] = true
			after = r.SourceID
		}
	}
	if len(seen) != 4 {
		t.Fatalf("keyset walk saw %d rows, want 4", len(seen))
	}

	open, err := src.ListRows(ctx, query("", domain.WorkStateDue, domain.WorkStateOverdue))
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("due+overdue: 2 expected, got %d", len(open))
	}
	counts, err := src.CountByState(ctx, query(""))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateOverdue] != 1 || counts[domain.WorkStateCompleted] != 1 || counts[domain.WorkStateVerificationPending] != 1 || len(counts) != 4 {
		t.Fatalf("counts %+v", counts)
	}
	filtered, err := src.CountByState(ctx, query("", domain.WorkStateCompleted))
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[domain.WorkStateCompleted] != 1 {
		t.Fatalf("filtered counts %+v", filtered)
	}
}

func TestVaccinationBoardCountsUseAggregateAndBoundSlowProcessIntegrity(t *testing.T) {
	repo := &slowCounterLister{}
	src := New(repo).WithClock(func() time.Time { return dueAt() })
	src.timeout = time.Millisecond

	start := time.Now()
	_, err := src.CountByState(context.Background(), query(""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CountByState err = %v, want deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("CountByState should fail fast for Work Board degradation, elapsed=%s", elapsed)
	}
	if repo.countCalls != 1 || repo.listCalls != 0 {
		t.Fatalf("summary counts must use aggregate only, list=%d count=%d", repo.listCalls, repo.countCalls)
	}

	start = time.Now()
	_, err = src.ListRows(context.Background(), query(""))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListRows err = %v, want deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("ListRows should fail fast for Work Board degradation, elapsed=%s", elapsed)
	}
}

func TestVaccinationDueWorkPrecheckMatchesEffectiveExecutionDateSources(t *testing.T) {
	for _, needle := range []string{
		"obligation_batches ob",
		"ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata'",
		"vaccination_drive_assignment_members vdam",
		"vdam.obligation_id = oi.obligation_id",
		"vaccination_drive_assignments vda",
		"vda.batch_id = oi.batch_id",
		"vda.park_id = $2::uuid",
	} {
		if !strings.Contains(vaccinationDueWorkPrecheckSQL, needle) {
			t.Fatalf("vaccination due-work precheck must include %q so effective planned-date rows cannot be hidden", needle)
		}
	}
}

func TestVaccinationWorkBoardReadBudgetCoversLocalOCICountPath(t *testing.T) {
	if workBoardVaccinationPrecheckBudget < 300*time.Millisecond {
		t.Fatalf("vaccination work-board precheck budget %s is too tight for the local OCI no-work path", workBoardVaccinationPrecheckBudget)
	}
	if workBoardVaccinationPrecheckBudget >= workBoardVaccinationReadBudget {
		t.Fatalf("vaccination work-board precheck budget %s must stay below read budget %s", workBoardVaccinationPrecheckBudget, workBoardVaccinationReadBudget)
	}
	if workBoardVaccinationReadBudget < 400*time.Millisecond {
		t.Fatalf("vaccination work-board read budget %s is too tight for the local OCI count path", workBoardVaccinationReadBudget)
	}
	if workBoardVaccinationReadBudget > 500*time.Millisecond {
		t.Fatalf("vaccination work-board read budget %s must stay below the route latency target", workBoardVaccinationReadBudget)
	}
}

func TestVaccinationBoardOwnerLensResolvesTheMemberAndNarrowsToTheOperator(t *testing.T) {
	ctx := context.Background()

	// No resolver: loud, never a fabricated answer.
	if _, err := New(fixture()).ListRows(ctx, query(vsUser)); !errors.Is(err, ErrOwnerScopeUnresolvable) {
		t.Fatalf("owner scope without a resolver must fail loudly, got %v", err)
	}

	lister := fixture()
	src := New(lister).WithMemberResolver(fakeMembers{byUser: map[string]string{vsUser: vsMember}}).WithClock(func() time.Time { return dueAt() })
	rows, err := src.ListRows(ctx, query(vsUser))
	if err != nil {
		t.Fatal(err)
	}
	// vsMember operates rowA and rowD; rowC lists vsMember only as park head and must be
	// dropped even though the wrapped filter would have returned it.
	if len(rows) != 2 || rows[0].SourceID != rowA || rows[1].SourceID != rowD {
		ids := []string{}
		for _, r := range rows {
			ids = append(ids, r.SourceID)
		}
		t.Fatalf("owner lens rows %v, want [rowA rowD]", ids)
	}
	if q := lister.queries[0]; q.OwnerID == nil || *q.OwnerID != vsMember {
		t.Fatalf("the wrapped read must be asked with the MEMBER id, got %v", q.OwnerID)
	}
	// And on the wire the owner carries the USER id too, resolved from the member in one
	// batched read, so an assignee picker keyed on user id can offer this person.
	for _, r := range rows {
		if r.Owner.UserID != vsUser || r.Owner.WorkforceMemberID != vsMember {
			t.Fatalf("owner must carry both ids: %+v", r.Owner)
		}
	}
	counts, err := src.CountByState(ctx, query(vsUser))
	if err != nil {
		t.Fatal(err)
	}
	if counts[domain.WorkStateDue] != 1 || counts[domain.WorkStateVerificationPending] != 1 || len(counts) != 2 {
		t.Fatalf("owner-scoped counts %+v", counts)
	}

	// A user with no workforce profile owns nothing in vaccination: empty, not an error.
	none, err := src.ListRows(ctx, query("00000000-0000-4000-8000-000000000999"))
	if err != nil || len(none) != 0 {
		t.Fatalf("unprofiled user: rows=%d err=%v", len(none), err)
	}
}

type rowsOnlyFake struct {
	*fakeLister
	fullCalls int
}

func (f *rowsOnlyFake) ListRows(context.Context, pidomain.Query) (pidomain.ListResult, error) {
	f.fullCalls++
	return pidomain.ListResult{}, errors.New("unexpected aggregate-backed list")
}
func (f *rowsOnlyFake) ListRowsOnly(ctx context.Context, q pidomain.Query) (pidomain.ListResult, error) {
	return f.fakeLister.ListRows(ctx, q)
}
func TestBoardWalkUsesRowsOnlyWithoutChangingRowsOrCursor(t *testing.T) {
	q := ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate, Limit: 100}
	want, err := New(fixture()).ListRows(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	repo := &rowsOnlyFake{fakeLister: fixture()}
	got, err := New(repo).ListRows(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if repo.fullCalls != 0 || len(repo.queries) < 2 {
		t.Fatalf("full list calls=%d pages=%d", repo.fullCalls, len(repo.queries))
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows-only walk changed rows: got=%+v want=%+v", got, want)
	}
}

type liveCountFake struct {
	*fakeLister
	cachedCalls, liveCalls int
	state                  pidomain.WorkState
}

func (f *liveCountFake) CountByWorkState(context.Context, pidomain.Query) ([]pidomain.CountByWorkState, error) {
	f.cachedCalls++
	return []pidomain.CountByWorkState{{WorkState: pidomain.WorkStateDue, Count: 1}}, nil
}
func (f *liveCountFake) CountByWorkStateLive(context.Context, pidomain.Query) ([]pidomain.CountByWorkState, error) {
	f.liveCalls++
	return []pidomain.CountByWorkState{{WorkState: f.state, Count: 1}}, nil
}
func TestBoardSummarySeesLaneTransitionWithoutCachedZero(t *testing.T) {
	f := &liveCountFake{fakeLister: fixture(), state: pidomain.WorkStateDue}
	s := New(f)
	q := ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate}
	before, err := s.CountByState(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	f.state = pidomain.WorkStateCompleted
	after, err := s.CountByState(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if before[domain.WorkStateDue] != 1 || after[domain.WorkStateCompleted] != 1 || after[domain.WorkStateDue] != 0 || f.cachedCalls != 0 || f.liveCalls != 2 {
		t.Fatalf("transition hidden: before=%v after=%v cached=%d live=%d", before, after, f.cachedCalls, f.liveCalls)
	}
}

type snapshotFake struct {
	*fakeLister
	liveCalls int
	failFirst bool
}

func (f *snapshotFake) ListRowsOnly(ctx context.Context, q pidomain.Query) (pidomain.ListResult, error) {
	if f.failFirst {
		f.failFirst = false
		return pidomain.ListResult{}, errors.New("transient list failure")
	}
	return f.fakeLister.ListRows(ctx, q)
}
func (f *snapshotFake) CountByWorkState(ctx context.Context, q pidomain.Query) ([]pidomain.CountByWorkState, error) {
	return f.CountByWorkStateLive(ctx, q)
}
func (f *snapshotFake) CountByWorkStateLive(context.Context, pidomain.Query) ([]pidomain.CountByWorkState, error) {
	f.liveCalls++
	m := map[pidomain.WorkState]int64{}
	for _, page := range f.pages {
		for _, r := range page {
			if r.Category == pidomain.CategoryVaccination {
				m[r.WorkState]++
			}
		}
	}
	out := []pidomain.CountByWorkState{}
	for state, n := range m {
		out = append(out, pidomain.CountByWorkState{WorkState: state, Count: n})
	}
	return out, nil
}

func TestRequestSnapshotCountsCompleteCanonicalPageAndFallsBackOnOverflow(t *testing.T) {
	states := []pidomain.WorkState{pidomain.WorkStateDue, pidomain.WorkStateOverdue, pidomain.WorkStateInProgress, pidomain.WorkStateVerificationPending, pidomain.WorkStateCompleted, pidomain.WorkStateRejected, pidomain.WorkStateBlocked}
	for _, n := range []int{0, 99, 100, 101, 205} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := &snapshotFake{fakeLister: &fakeLister{}}
			want := map[domain.WorkState]int{}
			for i := 0; i < n; i++ {
				if i%walkPageSize == 0 {
					f.pages = append(f.pages, []pidomain.Row{})
				}
				state := states[i%len(states)]
				f.pages[len(f.pages)-1] = append(f.pages[len(f.pages)-1], piRow(fmt.Sprintf("row-%03d", i), "dose", state, pidomain.SeverityOK, nil, nil, 1, 0, 0))
				want[domain.WorkState(state)]++
			}
			s := New(f)
			ctx := ports.WithRequestReadMemo(context.Background())
			q := ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate, Limit: 300}
			counts, err := s.CountByState(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(counts, want) {
				t.Fatalf("counts=%v want=%v", counts, want)
			}
			expectedCalls := 0
			if n > walkPageSize {
				expectedCalls = 1
			}
			if f.liveCalls != expectedCalls {
				t.Fatalf("canonical aggregate calls=%d want=%d", f.liveCalls, expectedCalls)
			}
			firstReads := len(f.queries)
			for _, state := range states {
				laneQ := q
				laneQ.WorkStates = []domain.WorkState{domain.WorkState(state)}
				rows, err := s.ListRows(ctx, laneQ)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) != want[domain.WorkState(state)] {
					t.Fatalf("state %s rows=%d want=%d", state, len(rows), want[domain.WorkState(state)])
				}
			}
			if n <= walkPageSize && len(f.queries) != firstReads {
				t.Fatalf("complete canonical page fetched again: %d -> %d", firstReads, len(f.queries))
			}
		})
	}
}

func TestRequestSnapshotScopeAndFreshNextRequest(t *testing.T) {
	f := &snapshotFake{fakeLister: fixture()}
	s := New(f).WithMemberResolver(fakeMembers{byUser: map[string]string{vsUser: vsMember}})
	q := ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate, Limit: 100}
	// Use one complete canonical page so the snapshot's post-mutation count is unambiguous.
	f.pages = [][]pidomain.Row{{piRow(rowA, "dose", pidomain.WorkStateDue, pidomain.SeverityOK, str(vsMember), str("Operator"), 1, 0, 0)}}
	ctx := ports.WithRequestReadMemo(context.Background())
	before, err := s.CountByState(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"park", "day", "tenant", "owner"} {
		other := q
		switch scope {
		case "park":
			other.ParkID = vsShed
		case "day":
			other.BusinessDate = "2026-09-11"
		case "tenant":
			other.TenantID = vsUser
		case "owner":
			other.OwnerUserID = vsUser
		}
		old := len(f.queries)
		if _, err = s.ListRows(ctx, other); err != nil {
			t.Fatal(err)
		}
		if len(f.queries) != old+1 {
			t.Fatalf("%s reused another scope", scope)
		}
	}
	f.pages = [][]pidomain.Row{{piRow(rowA, "dose", pidomain.WorkStateCompleted, pidomain.SeverityOK, str(vsMember), str("Operator"), 1, 1, 0)}}
	after, err := s.CountByState(ports.WithRequestReadMemo(context.Background()), q)
	if err != nil {
		t.Fatal(err)
	}
	if before[domain.WorkStateDue] != 1 || after[domain.WorkStateCompleted] != 1 || after[domain.WorkStateDue] != 0 {
		t.Fatalf("fresh request hid transition: before=%v after=%v", before, after)
	}
}

func TestRequestSnapshotReadFailureFallsBackAndLaneCanRecover(t *testing.T) {
	f := &snapshotFake{fakeLister: &fakeLister{pages: [][]pidomain.Row{{piRow(rowA, "dose", pidomain.WorkStateDue, pidomain.SeverityOK, nil, nil, 1, 0, 0)}}}, failFirst: true}
	s := New(f)
	q := ports.SourceQuery{TenantID: vsTenant, ParkID: vsPark, BusinessDate: vsDate, Limit: 100}
	ctx := ports.WithRequestReadMemo(context.Background())
	counts, err := s.CountByState(ctx, q)
	if err != nil || counts[domain.WorkStateDue] != 1 || f.liveCalls != 1 {
		t.Fatalf("fallback: counts=%v calls=%d err=%v", counts, f.liveCalls, err)
	}
	rows, err := s.ListRows(ctx, q)
	if err != nil || len(rows) != 1 {
		t.Fatalf("lane recovery rows=%d err=%v", len(rows), err)
	}
}
