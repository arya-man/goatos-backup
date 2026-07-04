package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

type fakeRepo struct {
	result domain.ListResult
	row    domain.Row
	found  bool
}

func (f fakeRepo) ListRows(context.Context, domain.Query) (domain.ListResult, error) {
	return f.result, nil
}

func (f fakeRepo) GetRow(context.Context, domain.Query, string) (domain.Row, bool, error) {
	return f.row, f.found, nil
}

func TestControlTowerUsesFilteredCountsAndAlerts(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r1", domain.WorkStateOwnerMissing, domain.SeverityBroken, due)
	svc := NewService(fakeRepo{result: domain.ListResult{
		Rows: []domain.Row{row},
		CountsByWorkState: []domain.CountByWorkState{
			{WorkState: domain.WorkStateOwnerMissing, Count: 2},
			{WorkState: domain.WorkStateVerificationPending, Count: 3},
		},
	}}).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if got.Summary.ProcessIntact {
		t.Fatal("summary should not be intact when open gaps exist")
	}
	if got.Summary.CriticalCount != 2 || got.Summary.WarningCount != 3 || got.Summary.VerificationBacklog != 3 || got.Summary.OwnerMissingCount != 2 {
		t.Fatalf("summary counts = %+v", got.Summary)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].EvidenceLink != "/workflows/r1" {
		t.Fatalf("alerts = %+v", got.Alerts)
	}
}

func TestControlTowerUsesFeedDirectionWorkflowLinks(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("feed_projection_exception:10000000-0000-4000-8000-000000000099", domain.WorkStateBlocked, domain.SeverityBroken, due)
	row.Category = domain.CategoryFeedDirection
	svc := NewService(fakeRepo{result: domain.ListResult{Rows: []domain.Row{row}}}).WithClock(func() time.Time { return due })

	got, err := svc.ControlTower(context.Background(), domain.Query{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("control tower: %v", err)
	}
	if len(got.Alerts) != 1 || got.Alerts[0].EvidenceLink != "/workflows/"+row.RowID+"?category=feed_direction" {
		t.Fatalf("alerts = %+v", got.Alerts)
	}
}

func TestProtocolAdherenceIncludesDeferredExplainedRows(t *testing.T) {
	due := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	row := processRow("r2", domain.WorkStateDeferred, domain.SeverityWatch, due)
	row.DeferredCount = 4
	row.GapType = "deferred_explained"
	svc := NewService(fakeRepo{result: domain.ListResult{
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
	svc := NewService(fakeRepo{result: domain.ListResult{
		Rows:             []domain.Row{pageRow},
		TotalCount:       1000,
		AdherenceSummary: fullSummary,
	}}).WithClock(func() time.Time { return due })

	got, err := svc.ProtocolAdherence(context.Background(), domain.Query{TenantID: "tenant-1", Limit: 1, Offset: 500})
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
	svc := NewService(fakeRepo{row: row, found: true}).WithClock(func() time.Time { return due })

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
	svc := NewService(fakeRepo{row: row, found: true}).WithClock(func() time.Time { return due })

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
		NextAction:        "Assign operator / owner chain",
		ProcessIntact:     state == domain.WorkStateDue || state == domain.WorkStateDeferred,
		Evidence:          domain.Evidence{ProofIDs: []string{}},
	}
}

func strPtr(v string) *string { return &v }
