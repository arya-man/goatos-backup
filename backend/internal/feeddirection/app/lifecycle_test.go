package app

import (
	"context"
	"errors"
	"fmt"
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
	// SERVING A FROZEN SHEET LIVE-COMPUTES NOTHING: no config snapshot, no shed scope, no counts read.
	if config.shedCalls != shedCallsBefore || config.snapshotCalls != snapCallsBefore || counts.calls != countCallsBefore {
		t.Fatalf("serving an issued sheet issued generation reads (shed %d->%d, snap %d->%d, counts %d->%d)",
			shedCallsBefore, config.shedCalls, snapCallsBefore, config.snapshotCalls, countCallsBefore, counts.calls)
	}
}

// ---------------------------------------------------------------------------
// Serve: pending / never-issued
// ---------------------------------------------------------------------------

// A future date with no issue returns the pending state naming WHEN it will be issued, and does NOT
// live-compute a speculative number.
func TestServeFutureWithNoIssueReturnsPending(t *testing.T) {
	t.Parallel()
	now := istInstant(2026, 7, 28, 6) // before the 2026-07-29 07:00 issue instant
	svc, config, counts, _ := newLifecycleService(now)

	page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.Lifecycle.State != domain.LifecycleStatePending {
		t.Fatalf("state = %q, want pending", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("pending must return NO computed rows, got %d", len(page.Items))
	}
	if len(page.Lifecycle.Workflows) != 1 || page.Lifecycle.Workflows[0].ExpectedIssueAt == nil {
		t.Fatalf("pending must name the expected issue instant, got %+v", page.Lifecycle.Workflows)
	}
	want := domain.FormatBusinessInstant(istInstant(2026, 7, 29, 7))
	if *page.Lifecycle.Workflows[0].ExpectedIssueAt != want {
		t.Fatalf("expected_issue_at = %s, want %s", *page.Lifecycle.Workflows[0].ExpectedIssueAt, want)
	}
	if config.snapshotCalls != 0 || counts.calls != 0 {
		t.Fatal("pending must NOT live-compute")
	}
}

// A past/current date with no issue is the honest never-issued state, again not a recompute.
func TestServePastWithNoIssueReturnsNotIssued(t *testing.T) {
	t.Parallel()
	now := istInstant(2026, 7, 30, 8) // after the 2026-07-29 07:00 issue instant
	svc, config, counts, _ := newLifecycleService(now)

	page, err := svc.Preview(context.Background(), domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if page.Lifecycle.State != domain.LifecycleStateNotIssued {
		t.Fatalf("state = %q, want not_issued", page.Lifecycle.State)
	}
	if len(page.Items) != 0 {
		t.Fatalf("not_issued must return no rows, got %d", len(page.Items))
	}
	if config.snapshotCalls != 0 || counts.calls != 0 {
		t.Fatal("not_issued must NOT live-compute")
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
	now := istInstant(2026, 7, 28, 6)
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

	// The non-draft serve of the same future day computes nothing (it is pending).
	snapBefore, countBefore := config.snapshotCalls, counts.calls
	page, err := svc.Preview(ctx, domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget()})
	if err != nil {
		t.Fatalf("serve Preview: %v", err)
	}
	if page.Draft {
		t.Fatal("a non-draft serve must not be draft")
	}
	if config.snapshotCalls != snapBefore || counts.calls != countBefore {
		t.Fatal("a non-draft serve must not live-compute")
	}
}

// feedDayTarget is the feed day the lifecycle fixtures issue for: as-of 2026-07-29 -> feed_day
// 2026-07-30.
func feedDayTarget() time.Time {
	return time.Date(2026, 7, 30, 0, 0, 0, 0, biztime.DefaultLocation())
}
