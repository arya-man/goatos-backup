package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// ---------------------------------------------------------------------------
// In-memory fakes mirroring the store/schedule semantics (the SQL itself is proved by the pg
// integration test; these prove the SERVICE state machine without Docker).
// ---------------------------------------------------------------------------

type fakeIssueStore struct {
	headers map[string]*domain.IssueHeader
	cells   map[string][]domain.StoredCell
	seq     int
}

func newFakeIssueStore() *fakeIssueStore {
	return &fakeIssueStore{headers: map[string]*domain.IssueHeader{}, cells: map[string][]domain.StoredCell{}}
}

func issueKey(park, feedDay, workflow string) string { return park + "|" + feedDay + "|" + workflow }

func cloneCells(in []domain.StoredCell) []domain.StoredCell {
	out := make([]domain.StoredCell, len(in))
	copy(out, in)
	return out
}

func (f *fakeIssueStore) PersistIssue(_ context.Context, cmd ports.PersistIssueCommand) (ports.IssueResult, error) {
	k := issueKey(cmd.ParkID, cmd.FeedDay, cmd.Workflow)
	if h, ok := f.headers[k]; ok {
		if h.GenerationInputFingerprint == cmd.Fingerprint {
			return ports.IssueResult{Header: *h, Outcome: ports.IssueOutcomeReplayed}, nil
		}
		if h.State != domain.IssueStateIssued {
			return ports.IssueResult{}, ports.ErrReissueAfterAmendOrLock
		}
		h.GenerationInputFingerprint = cmd.Fingerprint
		h.IssuedAt = cmd.IssuedAt
		f.cells[h.IssueID] = cloneCells(cmd.Cells)
		return ports.IssueResult{Header: *h, Outcome: ports.IssueOutcomeReissued}, nil
	}
	f.seq++
	id := fmt.Sprintf("issue-%d", f.seq)
	h := &domain.IssueHeader{
		IssueID: id, TenantID: cmd.TenantID, ParkID: cmd.ParkID, FeedDay: cmd.FeedDay,
		Workflow: cmd.Workflow, State: domain.IssueStateIssued, IssuedAt: cmd.IssuedAt,
		GenerationInputFingerprint: cmd.Fingerprint,
	}
	f.headers[k] = h
	f.cells[id] = cloneCells(cmd.Cells)
	return ports.IssueResult{Header: *h, Outcome: ports.IssueOutcomeInserted}, nil
}

func (f *fakeIssueStore) AmendIssue(_ context.Context, cmd ports.AmendIssueCommand) (ports.AmendResult, error) {
	h, ok := f.headers[issueKey(cmd.ParkID, cmd.FeedDay, cmd.Workflow)]
	if !ok {
		return ports.AmendResult{}, ports.ErrIssueNotFound
	}
	if h.State == domain.IssueStateLocked {
		return ports.AmendResult{}, ports.ErrAmendAfterLock
	}
	if h.GenerationInputFingerprint == cmd.Fingerprint {
		if h.State == domain.IssueStateIssued {
			h.State = domain.IssueStateAmended
		}
		at := cmd.AmendedAt
		h.AmendedAt = &at
		return ports.AmendResult{Header: *h, Outcome: ports.AmendOutcomeUnchanged}, nil
	}

	diff := domain.DiffCells(f.cells[h.IssueID], cmd.Cells)
	byKey := map[domain.CellKey]domain.StoredCell{}
	for _, c := range f.cells[h.IssueID] {
		byKey[c.Key()] = c
	}
	for _, c := range diff.Changed {
		c.Amended = true
		byKey[c.Key()] = c
	}
	for _, k := range diff.RemovedKeys {
		delete(byKey, k)
	}
	rebuilt := make([]domain.StoredCell, 0, len(byKey))
	for _, c := range byKey {
		rebuilt = append(rebuilt, c)
	}
	f.cells[h.IssueID] = rebuilt

	h.State = domain.IssueStateAmended
	at := cmd.AmendedAt
	h.AmendedAt = &at
	h.AmendmentCount++
	h.GenerationInputFingerprint = cmd.Fingerprint
	return ports.AmendResult{Header: *h, Outcome: ports.AmendOutcomeAmended, AffectedShedIDs: diff.AffectedShedIDs}, nil
}

func (f *fakeIssueStore) LockIssue(_ context.Context, cmd ports.LockIssueCommand) (ports.LockResult, error) {
	h, ok := f.headers[issueKey(cmd.ParkID, cmd.FeedDay, cmd.Workflow)]
	if !ok {
		return ports.LockResult{}, ports.ErrIssueNotFound
	}
	if h.State == domain.IssueStateLocked {
		return ports.LockResult{Header: *h, Outcome: ports.LockOutcomeAlreadyDone}, nil
	}
	h.State = domain.IssueStateLocked
	at := cmd.LockedAt
	h.LockedAt = &at
	return ports.LockResult{Header: *h, Outcome: ports.LockOutcomeLocked}, nil
}

func (f *fakeIssueStore) LoadIssueHeaders(_ context.Context, _, parkID, feedDay, workflow string) ([]domain.IssueHeader, error) {
	out := []domain.IssueHeader{}
	for _, h := range f.headers {
		if h.ParkID == parkID && h.FeedDay == feedDay && (workflow == "" || h.Workflow == workflow) {
			out = append(out, *h)
		}
	}
	return out, nil
}

func (f *fakeIssueStore) LoadIssueRows(_ context.Context, _ string, issueIDs []string) (map[string][]domain.StoredCell, error) {
	out := map[string][]domain.StoredCell{}
	for _, id := range issueIDs {
		out[id] = cloneCells(f.cells[id])
	}
	return out, nil
}

type fakeScheduleReader struct {
	clocks []domain.WorkflowClock
	parks  []string
}

func (f *fakeScheduleReader) ListScheduleClocks(_ context.Context, _, _ string, _ time.Time) ([]domain.WorkflowClock, error) {
	return f.clocks, nil
}

func (f *fakeScheduleReader) ListScheduledParks(_ context.Context, _ string, _ time.Time) ([]string, error) {
	return f.parks, nil
}

func normalClock() domain.WorkflowClock {
	return domain.WorkflowClock{Workflow: domain.WorkflowNormal, DirectionTime: "07:00:00", CorrectionTime: "14:00:00"}
}

func newLifecycleService(now time.Time) (*Service, *fakeConfigRepo, *fakeCountsReader, *fakeIssueStore) {
	svc, config, counts := newTestService()
	store := newFakeIssueStore()
	sched := &fakeScheduleReader{clocks: []domain.WorkflowClock{normalClock()}, parks: []string{testPark}}
	svc = svc.WithIssueStore(store).WithScheduleReader(sched).WithClock(func() time.Time { return now })
	return svc, config, counts, store
}

func TestAdvanceScheduledLifecycleFreezesRowsAndResumesFromDurableState(t *testing.T) {
	transportTime := "15:45:00"
	svc, _, _, store := newLifecycleService(istInstant(2026, 7, 29, 6))
	svc.schedule = &fakeScheduleReader{
		parks: []string{testPark},
		clocks: []domain.WorkflowClock{{
			Workflow:       domain.WorkflowNormal,
			DirectionTime:  "07:00:00",
			CorrectionTime: "14:00:00",
			TransportTime:  &transportTime,
		}},
	}

	beforeIssue, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 6))
	if err != nil {
		t.Fatalf("advance before issue: %v", err)
	}
	if len(beforeIssue) != 0 || len(store.headers) != 0 {
		t.Fatalf("before issue cutoff reports=%d headers=%d, want no durable work", len(beforeIssue), len(store.headers))
	}

	issued, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 7))
	if err != nil {
		t.Fatalf("advance at issue: %v", err)
	}
	if len(issued) != 1 || issued[0].Header.State != domain.IssueStateIssued {
		t.Fatalf("issue reports = %+v, want one issued transition", issued)
	}
	header := store.headers[issueKey(testPark, "2026-07-30", domain.WorkflowNormal)]
	if header == nil || len(store.cells[header.IssueID]) == 0 {
		t.Fatal("scheduled issue did not freeze generated rows")
	}
	issuedAt := header.IssuedAt

	retry, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 8))
	if err != nil {
		t.Fatalf("advance issue retry: %v", err)
	}
	if len(retry) != 0 || !header.IssuedAt.Equal(issuedAt) {
		t.Fatalf("issue retry repeated a durable transition: reports=%+v issued_at=%s", retry, header.IssuedAt)
	}

	amended, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 14))
	if err != nil {
		t.Fatalf("advance at correction: %v", err)
	}
	if len(amended) != 1 || header.State != domain.IssueStateAmended {
		t.Fatalf("correction reports=%+v state=%q, want one amended transition", amended, header.State)
	}

	locked, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 16))
	if err != nil {
		t.Fatalf("advance after transport: %v", err)
	}
	if len(locked) != 1 || header.State != domain.IssueStateLocked {
		t.Fatalf("transport reports=%+v state=%q, want one locked transition", locked, header.State)
	}

	lockedRetry, err := svc.AdvanceScheduledLifecycle(context.Background(), testTenant, istInstant(2026, 7, 29, 17))
	if err != nil {
		t.Fatalf("advance locked retry: %v", err)
	}
	if len(lockedRetry) != 0 {
		t.Fatalf("locked retry repeated a transition: %+v", lockedRetry)
	}
}

func istInstant(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, biztime.DefaultLocation())
}

// ---------------------------------------------------------------------------
// Issue
// ---------------------------------------------------------------------------

// Issuing freezes the WHOLE generated scope, and an exact re-issue (same inputs) is an idempotent
// no-op replay.
func TestIssueDirectionPersistsFullScopeAndReplays(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, _, _, store := newLifecycleService(asOf)

	report, err := svc.IssueDirection(context.Background(), IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf})
	if err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}
	if report.Outcome != ports.IssueOutcomeInserted {
		t.Fatalf("outcome = %q, want inserted", report.Outcome)
	}
	if report.FeedDay != "2026-07-30" {
		t.Fatalf("feed_day = %q, want 2026-07-30 (as-of + 1)", report.FeedDay)
	}
	// The 2-normal-shed fixture generates 6 rows (3 grains x 2 sessions), one cell each -> 6 cells.
	stored := len(store.cells[report.Header.IssueID])
	if stored != 6 {
		t.Fatalf("stored cells = %d, want the whole 6-cell scope", stored)
	}

	replay, err := svc.IssueDirection(context.Background(), IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf})
	if err != nil {
		t.Fatalf("re-issue: %v", err)
	}
	if replay.Outcome != ports.IssueOutcomeReplayed {
		t.Fatalf("re-issue outcome = %q, want replayed (idempotent no-op)", replay.Outcome)
	}
}

// An issue for a workflow the park does not run is refused -- there is no dispatch clock to issue
// against.
func TestIssueDirectionRefusesUnconfiguredWorkflow(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, _, _, _ := newLifecycleService(asOf)
	_, err := svc.IssueDirection(context.Background(), IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowExperiment, AsOf: asOf})
	if !errors.Is(err, ports.ErrWorkflowNotConfigured) {
		t.Fatalf("err = %v, want ErrWorkflowNotConfigured", err)
	}
}

// ---------------------------------------------------------------------------
// Serve: issued
// ---------------------------------------------------------------------------

// Serving an issued date returns the STORED rows, reports state=issued, is NOT draft, and the
// whole-scope summary is identical across page sizes (the analog of the 182 kg CBE total round-trip).
func TestServeIssuedReturnsStoredRowsWithPageInvariantSummary(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, config, counts, _ := newLifecycleService(asOf)
	if _, err := svc.IssueDirection(context.Background(), IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}

	// The draft (live) sheet is the reference; the served sheet must total identically.
	draft, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Draft: true})
	if err != nil {
		t.Fatalf("draft Preview: %v", err)
	}

	// Reads issued so far come only from the issue + this draft; record them, then prove SERVE adds none.
	shedCallsBefore, snapCallsBefore, countCallsBefore := config.shedCalls, config.snapshotCalls, counts.calls

	var reference domain.PreviewSummary
	for i, limit := range []int32{1, 2, 50} {
		page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Limit: limit})
		if err != nil {
			t.Fatalf("serve Preview(limit=%d): %v", limit, err)
		}
		if page.Draft {
			t.Fatal("a served issued sheet must not be draft")
		}
		if page.Lifecycle.State != domain.IssueStateIssued {
			t.Fatalf("state = %q, want issued", page.Lifecycle.State)
		}
		if i == 0 {
			reference = page.Summary
		} else if page.Summary.TotalKgByFeedItem[0].QuantityKg != reference.TotalKgByFeedItem[0].QuantityKg {
			t.Fatalf("summary total moved with page size: %s vs %s",
				page.Summary.TotalKgByFeedItem[0].QuantityKg, reference.TotalKgByFeedItem[0].QuantityKg)
		}
	}
	// Served total equals the live draft total: persist -> serve changed nothing.
	if reference.TotalKgByFeedItem[0].QuantityKg != draft.Summary.TotalKgByFeedItem[0].QuantityKg {
		t.Fatalf("served total %s != draft total %s", reference.TotalKgByFeedItem[0].QuantityKg, draft.Summary.TotalKgByFeedItem[0].QuantityKg)
	}
	// A serve RE-GENERATES nothing (no config snapshot, no counts read), whatever the page size.
	// The only reads a serve makes are the backend-owned filter vocabulary: one park-catalog + one
	// shed-catalog read per request, both page-size-independent — a picker vocabulary, not a
	// generation read.
	const servedTimes = 3 // limits {1, 2, 50}
	if config.snapshotCalls != snapCallsBefore || counts.calls != countCallsBefore {
		t.Fatalf("serving an issued sheet RE-GENERATED (snap %d->%d, counts %d->%d)",
			snapCallsBefore, config.snapshotCalls, countCallsBefore, counts.calls)
	}
	if config.shedCalls != shedCallsBefore+servedTimes {
		t.Fatalf("serving %d times should add exactly %d filter-vocabulary shed reads, got shed %d->%d",
			servedTimes, servedTimes, shedCallsBefore, config.shedCalls)
	}
}

// ---------------------------------------------------------------------------
// Serve: generated preview (no issued sheet)
// ---------------------------------------------------------------------------

// BEFORE the dispatch clock, a feed day has NO ROWS AT ALL (maintainer decision 2026-08-08,
// superseding the 2026-07-20 always-generate preview). The screen must not show a number that can
// still move: it shows WHEN the sheet arrives. Critically the gate must also not live-compute --
// generating rows and then hiding them would burn a config snapshot + counts read on every poll.
func TestServeBeforeDispatchClockShowsNoRowsAndNamesTheArrivalTime(t *testing.T) {
	t.Parallel()
	// now = 2026-07-29 06:00 -> tomorrow 07-30 (= feedDayTarget) is issued at 07-29 07:00: one hour away.
	now := istInstant(2026, 7, 29, 6)
	svc, config, counts, _ := newLifecycleService(now)

	page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.Lifecycle.State != domain.LifecycleStatePending {
		t.Fatalf("state = %q, want pending before the dispatch clock", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("a gated day must serve NO rows, got %d", len(page.Items))
	}
	if len(page.Lifecycle.Workflows) != 1 || page.Lifecycle.Workflows[0].ExpectedIssueAt == nil {
		t.Fatalf("a gated day must name the expected issue instant, got %+v", page.Lifecycle.Workflows)
	}
	want := domain.FormatBusinessInstant(istInstant(2026, 7, 29, 7))
	if *page.Lifecycle.Workflows[0].ExpectedIssueAt != want {
		t.Fatalf("expected_issue_at = %s, want %s", *page.Lifecycle.Workflows[0].ExpectedIssueAt, want)
	}
	if config.snapshotCalls != 0 || counts.calls != 0 {
		t.Fatalf("a gated day must NOT live-compute (snapshots %d, counts %d)", config.snapshotCalls, counts.calls)
	}
}

// AT/AFTER the dispatch clock the FIRST read generates the sheet AND FREEZES it, then serves the
// frozen rows. This is the whole point of the gate: what the packing crew sees at 08:00 is what the
// sheet said at 07:00, and it cannot drift afterwards. The second read must serve the SAME stored
// rows without recomputing -- if it recomputed, a shifting approved between the two reads would move
// the kilograms under a crew that has already packed the bags.
func TestServeAfterDispatchClockFreezesOnFirstReadAndNeverRecomputes(t *testing.T) {
	t.Parallel()
	// now = 2026-07-30 08:00 -> today 07-30 (= feedDayTarget), past its 07-29 07:00 issue instant.
	now := istInstant(2026, 7, 30, 8)
	svc, config, counts, store := newLifecycleService(now)

	first, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview (first): %v", err)
	}
	if first.Lifecycle.State != domain.IssueStateIssued {
		t.Fatalf("state = %q, want issued: the first read past the clock must FREEZE", first.Lifecycle.State)
	}
	if len(first.Items) == 0 {
		t.Fatal("the frozen sheet must carry rows")
	}
	if len(store.headers) == 0 {
		t.Fatal("the first read past the clock must PERSIST the sheet, not just render it")
	}

	// The freeze is stamped at the SCHEDULED instant, not at first-read time, so the audit trail does
	// not record whoever happened to open the screen first.
	for _, header := range store.headers {
		if got := header.IssuedAt.In(biztime.DefaultLocation()); !got.Equal(istInstant(2026, 7, 29, 7)) {
			t.Fatalf("issued_at = %s, want the scheduled 07-29 07:00 instant", got)
		}
	}

	snapAfterFreeze, countsAfterFreeze := config.snapshotCalls, counts.calls
	second, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview (second): %v", err)
	}
	if config.snapshotCalls != snapAfterFreeze || counts.calls != countsAfterFreeze {
		t.Fatalf("the second read RECOMPUTED (snapshots %d->%d, counts %d->%d): frozen rows must be served verbatim",
			snapAfterFreeze, config.snapshotCalls, countsAfterFreeze, counts.calls)
	}
	if len(second.Items) != len(first.Items) {
		t.Fatalf("frozen serve returned %d rows, first read had %d", len(second.Items), len(first.Items))
	}
}

// BEYOND the horizon (tomorrow+1 or later) with no issue is REFUSED, not fabricated (maintainer
// decision 2026-07-20). The projected counts are unknown past tomorrow, so generating would silently
// freeze today's herd onto the wrong day. The serve path returns an honest `beyond_horizon` state
// with NO rows, an empty whole-scope summary, and a message naming tomorrow as the limit — and it
// must NOT touch the generation reads (no config snapshot, no counts).
func TestServeBeyondHorizonReturnsHonestNoData(t *testing.T) {
	t.Parallel()
	// now = 2026-07-28 06:00 -> today 07-28, tomorrow 07-29. feedDayTarget 07-30 is tomorrow+1.
	now := istInstant(2026, 7, 28, 6)
	svc, config, counts, _ := newLifecycleService(now)

	page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.Lifecycle.State != domain.LifecycleStateBeyondHorizon {
		t.Fatalf("state = %q, want beyond_horizon", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("beyond-horizon serve must return NO rows (not fabricated), got %d", len(page.Items))
	}
	if len(page.Summary.TotalKgByFeedItem) != 0 || page.Summary.ShedCount != 0 || page.Summary.RowCount != 0 {
		t.Fatalf("beyond-horizon summary must be empty, got %+v", page.Summary)
	}
	if !strings.Contains(page.Lifecycle.Message, "2026-07-29") {
		t.Fatalf("beyond-horizon message must name the tomorrow limit 2026-07-29, got %q", page.Lifecycle.Message)
	}
	// The whole point: it did NOT fabricate a sheet, so it never ran generation.
	if config.snapshotCalls != 0 || counts.calls != 0 {
		t.Fatalf("beyond-horizon serve must NOT live-compute (snapshot=%d counts=%d)", config.snapshotCalls, counts.calls)
	}
}

// A PAST date with no issue is ALSO refused generation (same reasoning: today's herd is not what the
// past day's was). It returns the honest `beyond_horizon` no-data state with no rows and no
// generation. Critically, the draft-only past-date regeneration guard is not what stops it here — the
// non-draft serve path simply refuses to generate outside [today, tomorrow].
func TestServePastWithNoIssueReturnsHonestNoData(t *testing.T) {
	t.Parallel()
	now := istInstant(2026, 8, 1, 8) // target feed day 2026-07-30 is strictly in the past
	svc, config, counts, _ := newLifecycleService(now)

	page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview (past date must not error): %v", err)
	}
	if page.Lifecycle.State != domain.LifecycleStateBeyondHorizon {
		t.Fatalf("state = %q, want beyond_horizon", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("a past day with no issue must return NO rows, got %d", len(page.Items))
	}
	if config.snapshotCalls != 0 || counts.calls != 0 {
		t.Fatalf("a past day with no issue must NOT live-compute (snapshot=%d counts=%d)", config.snapshotCalls, counts.calls)
	}
}

// An ISSUED sheet whose feed day has drifted OUTSIDE the horizon (e.g. it is now a past day) still
// serves its FROZEN rows unchanged. The horizon guard is on GENERATION only; a real historical record
// is not a fabrication.
func TestServeIssuedBeyondHorizonStillServesFrozenRows(t *testing.T) {
	t.Parallel()
	// Issue for feed day 2026-07-30 (as-of 07-29), then read it from a clock far in the future where
	// 07-30 is well past the [today, tomorrow] window.
	svc, _, _, _ := newLifecycleService(istInstant(2026, 8, 10, 8))
	ctx := context.Background()
	if _, err := svc.IssueDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: istInstant(2026, 7, 29, 9)}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}

	page, err := svc.Preview(ctx, domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("serve issued far-future/past: %v", err)
	}
	if page.Lifecycle.State != domain.IssueStateIssued {
		t.Fatalf("state = %q, want issued (frozen rows serve regardless of the horizon)", page.Lifecycle.State)
	}
	if len(page.Items) == 0 {
		t.Fatal("an issued sheet must serve its frozen rows even when its feed day is beyond the horizon")
	}
}

// The served summary is WHOLE-SCOPE and page-size invariant, and blocked-vs-zero survives the
// freeze-on-first-read path exactly as it does on an already-issued sheet: a blocked cell is null +
// counted, never a 0.
func TestServePreviewSummaryIsWholeScopeAndPreservesBlocked(t *testing.T) {
	t.Parallel()
	// 08:00 is PAST the 07:00 dispatch clock for feedDayTarget, so the first read freezes the sheet
	// and every read after it serves those frozen rows.
	now := istInstant(2026, 7, 29, 8)
	svc, _, counts, _ := newLifecycleService(now)
	// Give shed B a management stage with no authored ration so at least one cell BLOCKS.
	counts.grains[shedB] = []domain.ShedGrain{{ManagementStage: "No-Such-Stage", Breed: "No-Such-Breed", HeadCount: 12}}

	var reference domain.PreviewSummary
	for i, limit := range []int32{1, 2, 50} {
		page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Limit: limit})
		if err != nil {
			t.Fatalf("preview(limit=%d): %v", limit, err)
		}
		if page.Lifecycle.State != domain.IssueStateIssued {
			t.Fatalf("state = %q, want issued", page.Lifecycle.State)
		}
		if i == 0 {
			reference = page.Summary
		} else {
			if page.Summary.RowCount != reference.RowCount || page.Summary.BlockedCount != reference.BlockedCount {
				t.Fatalf("summary moved with page size: rows %d/%d blocked %d/%d",
					page.Summary.RowCount, reference.RowCount, page.Summary.BlockedCount, reference.BlockedCount)
			}
			if page.Summary.TotalKgByFeedItem[0].QuantityKg != reference.TotalKgByFeedItem[0].QuantityKg {
				t.Fatalf("total moved with page size: %s vs %s",
					page.Summary.TotalKgByFeedItem[0].QuantityKg, reference.TotalKgByFeedItem[0].QuantityKg)
			}
		}
	}
	if reference.BlockedCount == 0 {
		t.Fatal("the unauthored shed must produce a blocked cell in the preview summary")
	}
}

// ---------------------------------------------------------------------------
// Amend / lock
// ---------------------------------------------------------------------------

// An amend with no change is a no-op that STILL records it ran (state amended, no count bump);
// a real change marks the affected sheds; an amend after lock is refused.
func TestAmendAndLockLifecycle(t *testing.T) {
	t.Parallel()
	asOf := istInstant(2026, 7, 29, 9)
	svc, config, counts, store := newLifecycleService(asOf)
	ctx := context.Background()
	if _, err := svc.IssueDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: asOf}); err != nil {
		t.Fatalf("IssueDirection: %v", err)
	}

	// Unchanged amend: the herd/config are identical, so nothing moves -- but the run is recorded.
	amendAt := istInstant(2026, 7, 29, 14)
	noop, err := svc.AmendDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: amendAt})
	if err != nil {
		t.Fatalf("AmendDirection (no-op): %v", err)
	}
	if noop.Outcome != ports.AmendOutcomeUnchanged {
		t.Fatalf("outcome = %q, want unchanged", noop.Outcome)
	}
	if noop.Header.AmendedAt == nil {
		t.Fatal("an unchanged amend must still stamp amended_at (it ran)")
	}

	// Now change the herd so a real amendment touches shed B only.
	counts.grains[shedB] = []domain.ShedGrain{{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 40}}
	changed, err := svc.AmendDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: amendAt})
	if err != nil {
		t.Fatalf("AmendDirection (changed): %v", err)
	}
	if changed.Outcome != ports.AmendOutcomeAmended {
		t.Fatalf("outcome = %q, want amended", changed.Outcome)
	}
	if len(changed.AffectedShedIDs) != 1 || changed.AffectedShedIDs[0] != shedB {
		t.Fatalf("affected sheds = %v, want only shed B", changed.AffectedShedIDs)
	}
	if changed.Header.AmendmentCount != 1 {
		t.Fatalf("amendment_count = %d, want 1", changed.Header.AmendmentCount)
	}
	// The changed cells are marked amended; shed A's are not.
	amendedSheds := map[string]bool{}
	for _, c := range store.cells[changed.Header.IssueID] {
		if c.Amended {
			amendedSheds[c.ShedID] = true
		}
	}
	if !amendedSheds[shedB] || amendedSheds[shedA] {
		t.Fatalf("only shed B's cells must be marked amended, got %v", amendedSheds)
	}

	// Lock, then prove lock is idempotent and amend is refused after lock.
	lockAt := istInstant(2026, 7, 29, 15)
	if l, err := svc.LockDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: lockAt}); err != nil || l.Outcome != ports.LockOutcomeLocked {
		t.Fatalf("LockDirection = (%v, %v), want locked", l.Outcome, err)
	}
	if l2, err := svc.LockDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: lockAt}); err != nil || l2.Outcome != ports.LockOutcomeAlreadyDone {
		t.Fatalf("second LockDirection = (%v, %v), want already_locked (idempotent)", l2.Outcome, err)
	}
	counts.grains[shedA] = []domain.ShedGrain{{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 99}}
	if _, err := svc.AmendDirection(ctx, IssueRequest{TenantID: testTenant, ParkID: testPark, Workflow: domain.WorkflowNormal, AsOf: lockAt}); !errors.Is(err, ports.ErrAmendAfterLock) {
		t.Fatalf("amend after lock err = %v, want ErrAmendAfterLock", err)
	}
	// Serving the locked day reports state=locked.
	page, err := svc.Preview(ctx, domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("serve after lock: %v", err)
	}
	if page.Lifecycle.State != domain.IssueStateLocked {
		t.Fatalf("served state = %q, want locked", page.Lifecycle.State)
	}
	_ = config
}

// ---------------------------------------------------------------------------
// Draft
// ---------------------------------------------------------------------------

// draft=true live-computes and is labelled draft; the same request WITHOUT draft (and no issue)
// live-computes nothing.
func TestDraftLiveComputesAndIsLabelled(t *testing.T) {
	t.Parallel()
	now := istInstant(2026, 7, 29, 6) // tomorrow = feedDayTarget 07-30, so the non-draft serve is in-window
	svc, config, counts, _ := newLifecycleService(now)
	ctx := context.Background()

	draft, err := svc.Preview(ctx, domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Draft: true})
	if err != nil {
		t.Fatalf("draft Preview: %v", err)
	}
	if !draft.Draft || draft.Lifecycle.State != domain.LifecycleStateDraft {
		t.Fatalf("draft response must be labelled draft, got draft=%v state=%q", draft.Draft, draft.Lifecycle.State)
	}
	if len(draft.Items) == 0 {
		t.Fatal("draft must live-compute rows")
	}
	if config.snapshotCalls == 0 || counts.calls == 0 {
		t.Fatal("draft must actually hit the generation reads")
	}

	// Draft is the ONLY path that still live-computes an un-issued day on demand. The non-draft serve
	// of that same day is GATED (the dispatch clock has not fired), so it returns `pending` with no
	// rows -- proving the 2026-08-08 gate did not accidentally close the config-authoring what-if
	// hatch along with the drifting preview.
	page, err := svc.Preview(ctx, domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("serve Preview: %v", err)
	}
	if page.Draft {
		t.Fatal("a non-draft serve must not be draft")
	}
	if page.Lifecycle.State != domain.LifecycleStatePending {
		t.Fatalf("non-draft serve of a gated day = %q, want pending", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("a gated day must serve no rows, got %d", len(page.Items))
	}
}

// feedDayTarget is the feed day the lifecycle fixtures issue for: as-of 2026-07-29 -> feed_day
// 2026-07-30.
func feedDayTarget() time.Time {
	return time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation())
}
