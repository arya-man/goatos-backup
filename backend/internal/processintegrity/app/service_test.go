package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

type fakeRepo struct {
	result       domain.ListResult
	counts       []domain.CountByWorkState
	row          domain.Row
	found        bool
	listCalls    int
	countCalls   int
	listQueries  []domain.Query
	countQueries []domain.Query
}

func (f *fakeRepo) ListRows(_ context.Context, q domain.Query) (domain.ListResult, error) {
	f.listCalls++
	f.listQueries = append(f.listQueries, q)
	return f.result, nil
}

func (f *fakeRepo) CountByWorkState(_ context.Context, q domain.Query) ([]domain.CountByWorkState, error) {
	f.countCalls++
	f.countQueries = append(f.countQueries, q)
	if f.counts != nil {
		return f.counts, nil
	}
	return f.result.CountsByWorkState, nil
}

func (f *fakeRepo) GetRow(context.Context, domain.Query, string) (domain.Row, bool, error) {
	return f.row, f.found, nil
}

func TestActionCenterUsesBoundedClosedHistoryByDefault(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo).WithClock(func() time.Time {
		return time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	})

	if _, err := svc.ActionCenter(context.Background(), domain.Query{TenantID: "tenant-1"}); err != nil {
		t.Fatalf("action center: %v", err)
	}
	if len(repo.listQueries) != 1 || repo.listQueries[0].IncludeCompleted {
		t.Fatalf("default Action Center query must use bounded closed history, got queries=%+v", repo.listQueries)
	}

	if _, err := svc.ActionCenterCounts(context.Background(), domain.Query{TenantID: "tenant-1"}); err != nil {
		t.Fatalf("action center counts: %v", err)
	}
	if len(repo.listQueries) != 2 || repo.listQueries[1].IncludeCompleted {
		t.Fatalf("default Action Center counts must use bounded closed history, got queries=%+v", repo.listQueries)
	}

	completed := domain.WorkStateCompleted
	if _, err := svc.ActionCenter(context.Background(), domain.Query{TenantID: "tenant-1", WorkState: &completed}); err != nil {
		t.Fatalf("completed action center: %v", err)
	}
	if len(repo.listQueries) != 3 || !repo.listQueries[2].IncludeCompleted {
		t.Fatalf("completed work-state filter must include closed history, got queries=%+v", repo.listQueries)
	}
}

func TestControlTowerUsesFilteredCountsAndAlerts(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r1", domain.WorkStateBlocked, domain.SeverityBroken, due)
	repo := &fakeRepo{result: domain.ListResult{
		Rows: []domain.Row{row},
		CountsByWorkState: []domain.CountByWorkState{
			{WorkState: domain.WorkStateBlocked, Count: 2},
			{WorkState: domain.WorkStateVerificationPending, Count: 3},
		},
	}}
	svc := NewService(repo).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if got.Summary.ProcessIntact {
		t.Fatal("summary should not be intact when open gaps exist")
	}
	if got.Summary.CriticalCount != 2 || got.Summary.WarningCount != 3 || got.Summary.VerificationBacklog != 3 || got.Summary.ConfigOrSOPBlockers != 2 {
		t.Fatalf("summary counts = %+v", got.Summary)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].EvidenceLink != "/workflows/r1" {
		t.Fatalf("alerts = %+v", got.Alerts)
	}
	if repo.listCalls != 1 || repo.countCalls != 0 {
		t.Fatalf("control tower default queries list=%d count=%d, want one list and no extra count", repo.listCalls, repo.countCalls)
	}
}

func TestControlTowerFetchesUnfilteredSummaryCountsForFilteredAlerts(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r1", domain.WorkStateBlocked, domain.SeverityBroken, due)
	severity := domain.SeverityBroken
	repo := &fakeRepo{
		result: domain.ListResult{
			Rows:              []domain.Row{row},
			CountsByWorkState: []domain.CountByWorkState{{WorkState: domain.WorkStateBlocked, Count: 1}},
		},
		counts: []domain.CountByWorkState{
			{WorkState: domain.WorkStateBlocked, Count: 2},
			{WorkState: domain.WorkStateVerificationPending, Count: 3},
		},
	}
	svc := NewService(repo).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1", Severity: &severity})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if got.Summary.CriticalCount != 2 || got.Summary.WarningCount != 3 {
		t.Fatalf("summary counts = %+v", got.Summary)
	}
	if repo.listCalls != 1 || repo.countCalls != 1 {
		t.Fatalf("filtered control tower queries list=%d count=%d, want one list and one count", repo.listCalls, repo.countCalls)
	}
	if len(repo.countQueries) != 1 || repo.countQueries[0].Severity != nil || repo.countQueries[0].WorkState != nil || repo.countQueries[0].OwnerID != nil {
		t.Fatalf("summary query did not clear alert-only filters: %+v", repo.countQueries)
	}
}

func TestControlTowerUsesFeedDirectionWorkflowLinks(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("feed_projection_exception:10000000-0000-4000-8000-000000000099", domain.WorkStateBlocked, domain.SeverityBroken, due)
	row.Category = domain.CategoryFeedDirection
	svc := NewService(&fakeRepo{result: domain.ListResult{Rows: []domain.Row{row}}}).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].EvidenceLink != "/workflows/"+row.RowID+"?category=feed_direction" {
		t.Fatalf("alerts = %+v", got.Alerts)
	}
}

func TestVaccinationCommandSurfacesUseHumanDoseLabel(t *testing.T) {
	due := time.Date(2026, 7, 23, 4, 27, 0, 0, time.UTC)
	row := processRow("human-dose", domain.WorkStateOverdue, domain.SeverityAtRisk, due)
	row.ProtocolName = "Preventive Care Vaccination Matrix"
	row.DoseCode = "ET+TT adult course dose 2"
	row.DriveName = strPtr("Preventive Care Vaccination Matrix - ET+TT adult course dose 2")
	row.NextAction = "Start scheduled vaccination SOP"
	repo := &fakeRepo{
		result: domain.ListResult{
			Rows:              []domain.Row{row},
			CountsByWorkState: []domain.CountByWorkState{{WorkState: domain.WorkStateOverdue, Count: 1}},
			AdherenceSummary:  domain.AdherenceSummary{ExpectedCount: row.ExpectedCount, OpenGapCount: 1},
		},
		row:   row,
		found: true,
	}
	svc := NewService(repo).WithClock(func() time.Time { return due })

	ac, err := svc.ActionCenter(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("action center: %v", err)
	}
	pa, err := svc.ProtocolAdherence(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("protocol adherence: %v", err)
	}
	ct, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	wf, found, err := svc.WorkflowDrilldown(context.Background(), domain.Query{TenantID: "tenant-1"}, row.RowID)
	if err != nil || !found {
		t.Fatalf("workflow found=%v err=%v", found, err)
	}

	joined := strings.Join([]string{
		ac.Items[0].DoseCode,
		pa.Rows[0].Expected,
		ct.Alerts[0].Title,
		wf.Row.DoseCode,
	}, "\n")
	if strings.Contains(joined, "et_tt_adult_w2") {
		t.Fatalf("command surfaces leaked raw vaccine rule code:\n%s", joined)
	}
	if got := strings.Count(joined, "ET+TT adult course dose 2"); got < 4 {
		t.Fatalf("command surfaces did not preserve human dose label in all views, occurrences=%d text:\n%s", got, joined)
	}
}

func TestProtocolAdherenceIncludesDeferredExplainedRows(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r2", domain.WorkStateDeferred, domain.SeverityWatch, due)
	row.DeferredCount = 4
	row.GapType = "deferred_explained"
	svc := NewService(&fakeRepo{result: domain.ListResult{
		Rows: []domain.Row{row},
		AdherenceSummary: domain.AdherenceSummary{
			ExpectedCount:      row.ExpectedCount,
			CompletedCount:     row.CompletedCount,
			DeferredCount:      4,
			ProcessIntactCount: 1,
			AdherencePercent:   0,
		},
	}}).WithClock(func() time.Time { return due })

	got, err := svc.ProtocolAdherence(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("adherence: %v", err)
	}
	if got.Summary.DeferredCount != 4 {
		t.Fatalf("deferred count = %d", got.Summary.DeferredCount)
	}
	if len(got.Rows) != 1 || got.Rows[0].Gap != "deferred_explained" {
		t.Fatalf("rows = %+v", got.Rows)
	}
}

func TestProtocolAdherenceShowsOverCapRequiredAsVaccinationWork(t *testing.T) {
	due := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	latestSafe := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	row := processRow("over-cap", domain.WorkStateDue, domain.SeverityBroken, due)
	row.DriveCapacityState = domain.DriveCapacityStateOverCapRequired
	row.DriveAnimalsRequired = 240
	row.DriveAnimalsAssigned = 240
	row.DriveOperatorCap = 200
	row.DriveAvailableOperators = 1
	row.DriveLatestSafeDate = &latestSafe
	row.NextAction = "Finish today over operator cap; do not push past latest safe date"
	svc := NewService(&fakeRepo{result: domain.ListResult{
		Rows: []domain.Row{row},
		AdherenceSummary: domain.AdherenceSummary{
			ExpectedCount: row.ExpectedCount,
			OpenGapCount:  1,
		},
	}}).WithClock(func() time.Time { return due })

	got, err := svc.ProtocolAdherence(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("adherence: %v", err)
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(got.Rows))
	}
	r := got.Rows[0]
	if r.Gap != "over_cap_required" {
		t.Fatalf("gap = %q, want over_cap_required", r.Gap)
	}
	if r.DriveCapacityState != domain.DriveCapacityStateOverCapRequired || r.DriveAnimalsAssigned != 240 || r.DriveOperatorCap != 200 || r.DriveAvailableOperators != 1 {
		t.Fatalf("drive fields = %+v", r)
	}
	if !strings.Contains(r.Actual, "finish over cap") || strings.Contains(strings.ToLower(r.NextAction), "escalate") {
		t.Fatalf("actual/next action should direct vaccination, not escalation: actual=%q next=%q", r.Actual, r.NextAction)
	}
}

func TestControlTowerShowsMedicalDefersDistinctFromOverCapWork(t *testing.T) {
	due := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	reason := "icu"
	row := processRow("medical-defer", domain.WorkStateDeferred, domain.SeverityWatch, due)
	row.DriveCapacityState = domain.DriveCapacityStateMedicalDefer
	row.DriveMedicalDeferReason = &reason
	row.NextAction = "Keep animal deferred until medical clearance"
	svc := NewService(&fakeRepo{result: domain.ListResult{Rows: []domain.Row{row}}}).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if len(got.Alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(got.Alerts))
	}
	alert := got.Alerts[0]
	if alert.DriveCapacityState != domain.DriveCapacityStateMedicalDefer || alert.DriveMedicalDeferReason == nil || *alert.DriveMedicalDeferReason != "icu" {
		t.Fatalf("medical defer fields = %+v", alert)
	}
	if strings.Contains(alert.Detail, "over") {
		t.Fatalf("medical defer detail leaked over-cap language: %q", alert.Detail)
	}
}

func TestProtocolAdherenceSummaryUsesFullFilteredSetNotCurrentPage(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	pageRow := processRow("page-row", domain.WorkStateCompleted, domain.SeverityOK, due)
	pageRow.ExpectedCount = 1
	pageRow.CompletedCount = 1
	fullSummary := domain.AdherenceSummary{
		ExpectedCount:      1000,
		CompletedCount:     900,
		OpenGapCount:       25,
		DeferredCount:      75,
		ProcessIntactCount: 975,
		AdherencePercent:   90,
	}
	svc := NewService(&fakeRepo{result: domain.ListResult{
		Rows:             []domain.Row{pageRow},
		TotalCount:       1000,
		AdherenceSummary: fullSummary,
	}}).WithClock(func() time.Time { return due })

	got, err := svc.ProtocolAdherence(context.Background(), domain.Query{TenantID: "tenant-1", Limit: 1})
	if err != nil {
		t.Fatalf("adherence: %v", err)
	}
	if got.Summary != fullSummary {
		t.Fatalf("summary=%+v want full filtered summary %+v", got.Summary, fullSummary)
	}
	if len(got.Rows) != 1 || got.Rows[0].RowID != "page-row" {
		t.Fatalf("rows=%+v, want current page row preserved", got.Rows)
	}
}

func TestWorkflowDrilldownBuildsConfigToCompletionNodes(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r3", domain.WorkStateVerificationPending, domain.SeverityWatch, due)
	row.ProofState = domain.ProofStateUploaded
	row.Evidence.ProofIDs = []string{"70000000-0000-4000-8000-000000000001"}
	row.Evidence.EvidenceCount = 1
	svc := NewService(&fakeRepo{row: row, found: true}).WithClock(func() time.Time { return due })

	got, found, err := svc.WorkflowDrilldown(context.Background(), domain.Query{TenantID: "tenant-1"}, "r3")
	if err != nil || !found {
		t.Fatalf("workflow found=%v err=%v", found, err)
	}
	if len(got.Nodes) != 8 {
		t.Fatalf("nodes = %d", len(got.Nodes))
	}
	if got.Nodes[0].Key != "config_published" || got.Nodes[4].State != "uploaded" {
		t.Fatalf("nodes = %+v", got.Nodes)
	}
}

func TestWorkflowDrilldownBuildsFeedDirectionExceptionNodes(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("feed_projection_exception:10000000-0000-4000-8000-000000000099", domain.WorkStateBlocked, domain.SeverityBroken, due)
	row.Category = domain.CategoryFeedDirection
	row.BlockerReason = strPtr("pregnant destination shed shortage")
	row.Evidence.AuditRef = strPtr("count_projection_exception:10000000-0000-4000-8000-000000000099")
	svc := NewService(&fakeRepo{row: row, found: true}).WithClock(func() time.Time { return due })

	got, found, err := svc.WorkflowDrilldown(context.Background(), domain.Query{TenantID: "tenant-1"}, row.RowID)
	if err != nil || !found {
		t.Fatalf("workflow found=%v err=%v", found, err)
	}
	if len(got.Nodes) != 4 {
		t.Fatalf("nodes = %d", len(got.Nodes))
	}
	if got.Nodes[0].Key != "counts_projection" || got.Nodes[1].Key != "exception_open" || got.Nodes[2].Blocker == nil {
		t.Fatalf("nodes = %+v", got.Nodes)
	}
}

func processRow(rowID string, state domain.WorkState, severity domain.Severity, due time.Time) domain.Row {
	return domain.Row{
		Category:          domain.CategoryVaccination,
		RowID:             rowID,
		ProcessKey:        rowID,
		ObligationID:      "10000000-0000-4000-8000-000000000001",
		ParkID:            "20000000-0000-4000-8000-000000000001",
		ParkName:          "CBE",
		ShedID:            "30000000-0000-4000-8000-000000000001",
		ShedName:          "K1",
		AnimalStage:       "K1",
		ProtocolID:        "40000000-0000-4000-8000-000000000001",
		ProtocolVersionID: "50000000-0000-4000-8000-000000000001",
		RuleID:            "60000000-0000-4000-8000-000000000001",
		ProtocolName:      "Enterotox",
		DoseCode:          "booster",
		ProofPolicy:       "{}",
		DueAt:             due,
		ExpectedCount:     17,
		ObligationStatus:  "due",
		SOPTaskState:      domain.SOPStateNotStarted,
		ProofState:        domain.ProofStateMissing,
		VerificationState: domain.VerificationStateNotReady,
		WorkState:         state,
		GapType:           string(state),
		Severity:          severity,
		OwnerState:        domain.OwnerStateMissing,
		NextAction:        "Resolve blocker before execution",
		ProcessIntact:     state == domain.WorkStateDue || state == domain.WorkStateDeferred,
		Evidence:          domain.Evidence{ProofIDs: []string{}},
	}
}

func strPtr(v string) *string { return &v }
