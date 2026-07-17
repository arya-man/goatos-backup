package app

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeRepo struct {
	rows        []domain.ExecutionProjection
	opsRows     []domain.OperationsRow
	roster      []domain.ScanRosterRow
	gapsRows    []domain.GapProjectionRow
	shedRows    []domain.ShedSummaryProjection
	shedAnimals []domain.ShedAnimalRow
	capacityCfg domain.CapacityConfig
	schedule    domain.ScheduleProjectionState
	err         error
}

func (r fakeRepo) ShedSummary(_ context.Context, _ domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.shedRows, nil
}

func (r fakeRepo) CapacityConfig(_ context.Context, _ string) (domain.CapacityConfig, error) {
	if r.err != nil {
		return domain.CapacityConfig{}, r.err
	}
	if r.capacityCfg.MaxPerDay == 0 {
		return domain.DefaultCapacityConfig(), nil
	}
	return r.capacityCfg, nil
}

func (r fakeRepo) ShedAnimals(_ context.Context, _ domain.ShedAnimalQuery) ([]domain.ShedAnimalRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.shedAnimals, nil
}

func (r fakeRepo) VaccinationGaps(_ context.Context, _ domain.GapsQuery) ([]domain.GapProjectionRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.gapsRows, nil
}

func (r fakeRepo) ScanRoster(_ context.Context, _ domain.ScanRosterQuery) (domain.ScanRosterResult, error) {
	if r.err != nil {
		return domain.ScanRosterResult{}, r.err
	}
	return domain.ScanRosterResult{Rows: r.roster}, nil
}

func (r fakeRepo) TaskOptionValues(_ context.Context, _, _ string) (domain.TaskOptionValuesResponse, error) {
	return domain.TaskOptionValuesResponse{}, r.err
}

func (r fakeRepo) VaccinationOperations(_ context.Context, _ domain.OperationsQuery) ([]domain.OperationsRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.opsRows, nil
}

func (r fakeRepo) VaccinationSchedule(_ context.Context, _ domain.ScheduleQuery) ([]domain.OperationsRow, domain.ScheduleProjectionState, error) {
	if r.err != nil {
		return nil, domain.ScheduleProjectionState{}, r.err
	}
	state := r.schedule
	if state.ProjectionVersion == 0 {
		state = domain.ScheduleProjectionState{
			ProjectionVersion: 1,
			ProjectedAt:       time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
			AsOf:              time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
			FreshnessStatus:   "green",
			ServingState:      "fresh",
			RowCount:          len(r.opsRows),
		}
	}
	return r.opsRows, state, nil
}

func (r fakeRepo) ListVaccinationExecutionPage(_ context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error) {
	if r.err != nil {
		return domain.ExecutionProjectionPage{}, r.err
	}
	if q.WorkState == nil {
		return domain.ExecutionProjectionPage{Rows: r.rows, TotalCount: int64(len(r.rows))}, nil
	}
	out := make([]domain.ExecutionProjection, 0, len(r.rows))
	for _, p := range r.rows {
		if workStateFromProjection(p, q) == *q.WorkState {
			out = append(out, p)
		}
	}
	return domain.ExecutionProjectionPage{Rows: out, TotalCount: int64(len(out))}, nil
}

func (r fakeRepo) ListVaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error) {
	page, err := r.ListVaccinationExecutionPage(ctx, q)
	return page.Rows, err
}

func TestVaccinationExecutionMapsProcessStates(t *testing.T) {
	asOf := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	dueYesterday := asOf.Add(-24 * time.Hour)
	dueTomorrow := asOf.Add(24 * time.Hour)
	operator := "Operator A"
	parkHead := "Park Head"
	rows := []domain.ExecutionProjection{
		projection("shed-overdue", dueYesterday, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.DueCount = 1
		}),
		projection("shed-unassigned", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
			p.DueCount = 1
		}),
		projection("shed-proof", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.ParkHeadName = &parkHead
			p.CompletionRecorded = 1
		}),
		projection("shed-blocked", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.UsableForVaccination = false
		}),
		projection("shed-missed", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.MissedCount = 1
		}),
		projection("shed-complete", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.CompletedCount = 1
			p.CompletionAccepted = 1
		}),
	}
	svc := NewService(fakeRepo{rows: rows})

	got, err := svc.VaccinationExecution(context.Background(), domain.ExecutionQuery{
		TenantID:  "tenant",
		AsOf:      asOf,
		DueBefore: asOf.Add(30 * 24 * time.Hour),
		Limit:     100,
	})
	if err != nil {
		t.Fatalf("VaccinationExecution() error = %v", err)
	}
	wantStates := []domain.WorkState{
		domain.WorkStateOverdue,
		domain.WorkStateBlocked,
		domain.WorkStateVerificationPending,
		domain.WorkStateBlocked,
		domain.WorkStateMissed,
		domain.WorkStateCompleted,
	}
	if len(got) != len(wantStates) {
		t.Fatalf("got %d rows want %d", len(got), len(wantStates))
	}
	for i, want := range wantStates {
		if got[i].WorkState != want {
			t.Fatalf("row %d workState = %q want %q", i, got[i].WorkState, want)
		}
	}
	if got[0].DueDate == nil || *got[0].DueDate != "2026-06-23" {
		t.Fatalf("due date = %v want 2026-06-23", got[0].DueDate)
	}
	if got[2].ProofStatus != domain.ProofStatusUploaded || got[2].VerificationStatus != domain.VerificationStatusPending {
		t.Fatalf("proof/verification = %q/%q want uploaded/pending", got[2].ProofStatus, got[2].VerificationStatus)
	}
	if got[3].BlockerReason == nil {
		t.Fatal("blocked row should include blocker reason")
	}
	if got[4].Severity != domain.SeverityAtRisk {
		t.Fatalf("missed severity = %q want at_risk", got[4].Severity)
	}
	if got[5].Severity != domain.SeverityOK {
		t.Fatalf("completed severity = %q want ok", got[5].Severity)
	}
}

func TestVaccinationExecutionFiltersWorkStateAndBuildsDrilldown(t *testing.T) {
	asOf := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	due := asOf.Add(24 * time.Hour)
	operator := "Operator A"
	state := domain.WorkStateVerificationPending
	svc := NewService(fakeRepo{rows: []domain.ExecutionProjection{
		projection("shed-1", due, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.CompletionRecorded = 1
		}),
		projection("shed-1", due, 2, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.CompletedCount = 1
			p.CompletionAccepted = 1
		}),
	}})

	rows, err := svc.VaccinationExecution(context.Background(), domain.ExecutionQuery{AsOf: asOf, WorkState: &state})
	if err != nil {
		t.Fatalf("VaccinationExecution() error = %v", err)
	}
	if len(rows) != 1 || rows[0].WorkState != domain.WorkStateVerificationPending {
		t.Fatalf("filtered rows = %#v, want one verification_pending row", rows)
	}

	detail, found, err := svc.ShedDrilldown(context.Background(), domain.ExecutionQuery{AsOf: asOf})
	if err != nil {
		t.Fatalf("ShedDrilldown() error = %v", err)
	}
	if !found {
		t.Fatal("ShedDrilldown() found = false")
	}
	if detail.Summary.Total != 2 || detail.Summary.VerificationPending != 1 || detail.Summary.Completed != 1 {
		t.Fatalf("summary = %#v, want total 2 verificationPending 1 completed 1", detail.Summary)
	}
	if len(detail.AnimalStages) != 1 || detail.AnimalStages[0] != "K1" {
		t.Fatalf("animal stages = %#v want [K1]", detail.AnimalStages)
	}
}

func TestVaccinationGapsBuildsPerAnimalRowsAndCursor(t *testing.T) {
	shedID := "shed-1"
	tag1 := "901007000503717"
	gapsRows := []domain.GapProjectionRow{
		{GoatID: "goat-1", DisplayID: "G-000001", AnimalIdentifier1: &tag1, ParkID: "park-1", ParkName: "CBE", ShedID: &shedID, ReasonCode: domain.GapReasonNoDateOfBirth},
		{GoatID: "goat-2", DisplayID: "G-000002", ParkID: "park-1", ParkName: "CBE", ReasonCode: domain.GapReasonNoBreedOnRecord},
	}
	svc := NewService(fakeRepo{gapsRows: gapsRows})

	resp, err := svc.VaccinationGaps(context.Background(), domain.GapsQuery{TenantID: "tenant", Limit: len(gapsRows)})
	if err != nil {
		t.Fatalf("VaccinationGaps() error = %v", err)
	}
	// Data gaps are strictly per-animal: one row per goat, no by-reason aggregate on the response.
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %#v want 2", resp.Rows)
	}
	if resp.Rows[0].ReasonLabel != "No date of birth" {
		t.Fatalf("row 0 label = %q want %q", resp.Rows[0].ReasonLabel, "No date of birth")
	}
	if resp.Rows[0].AnimalIdentifier1 == nil || *resp.Rows[0].AnimalIdentifier1 != tag1 {
		t.Fatalf("row 0 tag1 = %v want %q", resp.Rows[0].AnimalIdentifier1, tag1)
	}
	if resp.Rows[1].ReasonLabel != "No breed on record" {
		t.Fatalf("row 1 label = %q want %q", resp.Rows[1].ReasonLabel, "No breed on record")
	}
	// Response filled the full requested limit, so the service must signal a next cursor to keep paging.
	if resp.NextCursor == nil || *resp.NextCursor != "goat-2" {
		t.Fatalf("next cursor = %v want goat-2", resp.NextCursor)
	}
}

func TestVaccinationGapsNoNextCursorWhenUnderLimit(t *testing.T) {
	gapsRows := []domain.GapProjectionRow{
		{GoatID: "goat-1", DisplayID: "G-000001", ParkID: "park-1", ParkName: "CBE", ReasonCode: domain.GapReasonNoDateOfBirth},
	}
	svc := NewService(fakeRepo{gapsRows: gapsRows})

	resp, err := svc.VaccinationGaps(context.Background(), domain.GapsQuery{TenantID: "tenant", Limit: 200})
	if err != nil {
		t.Fatalf("VaccinationGaps() error = %v", err)
	}
	if resp.NextCursor != nil {
		t.Fatalf("next cursor = %v want nil (fewer rows than limit)", *resp.NextCursor)
	}
}

func TestVaccinationSchedulePaginatesByCohortAndSurfacesStaleState(t *testing.T) {
	rows := make([]domain.OperationsRow, 0, 501)
	for i := 1; i <= 501; i++ {
		rows = append(rows, domain.OperationsRow{
			ParkID:       "70000000-0000-4000-8000-000000000001",
			ParkName:     "CBE Park",
			ShedID:       fmt.Sprintf("70000000-0000-4000-8000-%012d", i),
			ShedName:     fmt.Sprintf("Shed %03d", i),
			Stage:        "K1",
			ProtocolID:   "90000000-0000-4000-8000-000000000001",
			ProtocolName: "PPR",
			Animals:      1,
			TotalCount:   1,
		})
	}
	svc := NewService(fakeRepo{
		opsRows: rows,
		schedule: domain.ScheduleProjectionState{
			ProjectionVersion: 7,
			ProjectedAt:       time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
			AsOf:              time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC),
			FreshnessStatus:   "yellow",
			ServingState:      "stale",
			Stale:             true,
			RebuildRequired:   true,
			RowCount:          501,
		},
	})

	resp, err := svc.VaccinationSchedule(context.Background(), domain.ScheduleQuery{TenantID: "tenant", Limit: 500})
	if err != nil {
		t.Fatalf("VaccinationSchedule() error = %v", err)
	}
	if len(resp.Cohorts) != 500 {
		t.Fatalf("cohorts = %d want 500", len(resp.Cohorts))
	}
	if resp.NextCursor == nil {
		t.Fatal("next cursor missing for 501 schedule cohorts")
	}
	cursor, err := domain.DecodeOperationsCursor(*resp.NextCursor)
	if err != nil {
		t.Fatalf("decode next cursor: %v", err)
	}
	if cursor.ShedID != "70000000-0000-4000-8000-000000000500" {
		t.Fatalf("cursor shed = %s want 500th shed", cursor.ShedID)
	}
	if resp.Freshness == nil || resp.Freshness.Status != "yellow" || !resp.Freshness.Stale || !resp.Freshness.RebuildRequired || resp.Freshness.ServingState != "stale" || resp.Freshness.RowCount != 501 {
		t.Fatalf("freshness = %#v want stale/rebuild row_count=501", resp.Freshness)
	}
}

func TestCoverageRollupAggregatesByProtocolAcrossCohorts(t *testing.T) {
	opsRows := []domain.OperationsRow{
		{ParkID: "park-1", ShedID: "shed-1", Stage: "K1", ProtocolID: "ppr", ProtocolName: "PPR", AcceptedCount: 10, TotalCount: 20},
		{ParkID: "park-1", ShedID: "shed-2", Stage: "K2", ProtocolID: "ppr", ProtocolName: "PPR", AcceptedCount: 5, TotalCount: 5},
		{ParkID: "park-1", ShedID: "shed-1", Stage: "K1", ProtocolID: "fmd", ProtocolName: "FMD", AcceptedCount: 1, TotalCount: 4},
	}
	svc := NewService(fakeRepo{opsRows: opsRows})

	resp, err := svc.CoverageRollup(context.Background(), domain.OperationsQuery{TenantID: "tenant"})
	if err != nil {
		t.Fatalf("CoverageRollup() error = %v", err)
	}
	if len(resp.Protocols) != 2 {
		t.Fatalf("protocols = %#v want 2", resp.Protocols)
	}
	byID := map[string]domain.CoverageProtocol{}
	for _, p := range resp.Protocols {
		byID[p.ProtocolID] = p
	}
	ppr, ok := byID["ppr"]
	if !ok {
		t.Fatal("missing ppr protocol")
	}
	if ppr.GivenCount != 15 || ppr.TotalCount != 25 || ppr.CoveragePercent != 60 {
		t.Fatalf("ppr = %#v want given=15 total=25 coverage=60", ppr)
	}
	fmd, ok := byID["fmd"]
	if !ok {
		t.Fatal("missing fmd protocol")
	}
	if fmd.GivenCount != 1 || fmd.TotalCount != 4 || fmd.CoveragePercent != 25 {
		t.Fatalf("fmd = %#v want given=1 total=4 coverage=25", fmd)
	}
}

func TestCoverageRollupZeroTotalIsZeroPercentNotDivideByZero(t *testing.T) {
	svc := NewService(fakeRepo{opsRows: []domain.OperationsRow{
		{ParkID: "park-1", ShedID: "shed-1", Stage: "K1", ProtocolID: "ppr", ProtocolName: "PPR", AcceptedCount: 0, TotalCount: 0},
	}})
	resp, err := svc.CoverageRollup(context.Background(), domain.OperationsQuery{TenantID: "tenant"})
	if err != nil {
		t.Fatalf("CoverageRollup() error = %v", err)
	}
	if len(resp.Protocols) != 1 || resp.Protocols[0].CoveragePercent != 0 {
		t.Fatalf("protocols = %#v want single 0%% entry", resp.Protocols)
	}
}

func projection(shedID string, dueAt time.Time, dose int, mutate func(*domain.ExecutionProjection)) domain.ExecutionProjection {
	batchID := shedID + "-batch"
	p := domain.ExecutionProjection{
		ParkID:               "30000000-0000-4000-8000-000000000001",
		ParkName:             "CBE Park",
		ShedID:               shedID,
		ShedName:             "K1 Shed",
		AnimalStage:          "K1",
		BatchID:              &batchID,
		ProtocolName:         "Rabies",
		DoseCode:             "D" + string(rune('0'+dose)),
		DueAt:                &dueAt,
		ObligationCount:      1,
		ScheduledCount:       1,
		UsableForVaccination: true,
	}
	if mutate != nil {
		mutate(&p)
	}
	return p
}
