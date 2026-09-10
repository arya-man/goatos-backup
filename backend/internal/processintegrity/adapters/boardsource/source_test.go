package boardsource

import (
	"context"
	"errors"
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

type fakeMembers struct{ byUser map[string]string }

func (f fakeMembers) WorkforceMemberIDForUser(_ context.Context, _ string, userID string) (string, bool, error) {
	id, ok := f.byUser[userID]
	return id, ok, nil
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
	if drive.Title != "CBE adult drive · Godel 1 - Part 3" || drive.Subtitle != "ET+TT adult course dose 1 · 40 animals" {
		t.Errorf("drive copy title=%q subtitle=%q", drive.Title, drive.Subtitle)
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
