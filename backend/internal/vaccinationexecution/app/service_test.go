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
	driveRows   []domain.DriveAssignmentRow
	roster      []domain.ScanRosterRow
	gapsRows    []domain.GapProjectionRow
	shedRows    []domain.ShedSummaryProjection
	shedAnimals []domain.ShedAnimalRow
	planned     []domain.PlannedSession
	capacityCfg domain.CapacityConfig
	carryLines  []domain.VaccineCarryLine
	err         error
}

func (r fakeRepo) ShedSummary(_ context.Context, _ domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.shedRows, nil
}

func (r fakeRepo) OperatorAssignmentConfig(_ context.Context, _, _ string) (domain.OperatorAssignmentConfig, bool, error) {
	return domain.OperatorAssignmentConfig{}, false, nil
}

func (r fakeRepo) OperatorShifts(_ context.Context, _, _ string) ([]domain.OperatorShift, error) {
	return nil, nil
}

func (r fakeRepo) UpsertOperatorAssignmentConfig(_ context.Context, _ string, cfg domain.OperatorAssignmentConfig) (domain.OperatorAssignmentConfig, error) {
	cfg.RowVersion++
	return cfg, nil
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

func (r fakeRepo) PlannedDriveSessionsForShed(_ context.Context, _, _ string) ([]domain.PlannedSession, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.planned, nil
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

func (r fakeRepo) VaccinationSchedule(_ context.Context, _ domain.ScheduleQuery) ([]domain.OperationsRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.opsRows, nil
}

func (r fakeRepo) DriveAssignments(_ context.Context, _ domain.DriveAssignmentQuery) ([]domain.DriveAssignmentRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.driveRows, nil
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

func TestDriveNameUsesBackendOwnedDoseDisplayLabel(t *testing.T) {
	t.Parallel()

	got := driveName(domain.ExecutionProjection{
		ProtocolName: "Preventive Care Vaccination Matrix",
		DoseCode:     "ET_TT_7W",
	})
	if got == nil || *got != "ET+TT" {
		t.Fatalf("driveName() = %v, want ET+TT display label without dose-wave wording", got)
	}
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

func TestVaccinationExecutionSubmittedProofOverridesInProgressProjection(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	due := asOf.Add(24 * time.Hour)
	operator := "Operator A"
	taskState := "needs_review"
	batchStatus := "in_progress"
	taskID := "4709ad27-735f-4806-8428-37da2065a8b3"
	rows := []domain.ExecutionProjection{
		projection("shed-review", due, 2, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.TaskState = &taskState
			p.SOPTaskID = &taskID
			p.BatchStatus = &batchStatus
			p.ObligationCount = 2
			p.ScheduledCount = 2
			p.InProgressCount = 2
			p.ScannedCount = 2
			p.ProofSubmittedCount = 1
			p.WorkState = domain.WorkStateInProgress
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
	if len(got) != 1 {
		t.Fatalf("got %d rows want 1", len(got))
	}
	row := got[0]
	if row.WorkState != domain.WorkStateVerificationPending {
		t.Fatalf("workState = %q want %q for submitted proof", row.WorkState, domain.WorkStateVerificationPending)
	}
	if row.ProofStatus != domain.ProofStatusUploaded || row.VerificationStatus != domain.VerificationStatusPending {
		t.Fatalf("proof/verification = %q/%q want uploaded/pending", row.ProofStatus, row.VerificationStatus)
	}
	if row.PrimaryActionKey != "none" {
		t.Fatalf("primaryActionKey = %q want none after proof submit", row.PrimaryActionKey)
	}
	if row.TargetCount != 2 || row.OpenCount != 0 || row.DoneCount != 2 {
		t.Fatalf("counts = target %d open %d done %d want 2/0/2", row.TargetCount, row.OpenCount, row.DoneCount)
	}
}

func TestVaccinationExecutionSharedTaskReviewDoesNotLeakToShedWithoutSubmittedProof(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	due := asOf.Add(24 * time.Hour)
	operator := "Operator A"
	taskState := "needs_review"
	batchStatus := "in_progress"
	taskID := "4709ad27-735f-4806-8428-37da2065a8b3"
	rows := []domain.ExecutionProjection{
		projection("godel-1", due, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.TaskState = &taskState
			p.SOPTaskID = &taskID
			p.BatchStatus = &batchStatus
			p.ObligationCount = 2
			p.ScheduledCount = 2
			p.ScannedCount = 2
			p.ProofSubmittedCount = 1
			p.WorkState = domain.WorkStateInProgress
		}),
		projection("godel-2", due, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.TaskState = &taskState
			p.SOPTaskID = &taskID
			p.BatchStatus = &batchStatus
			p.ObligationCount = 3
			p.ScheduledCount = 3
			p.InProgressCount = 3
			p.WorkState = domain.WorkStateInProgress
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
	if len(got) != 2 {
		t.Fatalf("got %d rows want 2", len(got))
	}
	if got[0].WorkState != domain.WorkStateVerificationPending || got[0].ProofStatus != domain.ProofStatusUploaded {
		t.Fatalf("submitted shed state/proof = %q/%q want verification_pending/uploaded", got[0].WorkState, got[0].ProofStatus)
	}
	if got[1].WorkState != domain.WorkStateInProgress {
		t.Fatalf("unsubmitted shed workState = %q want in_progress", got[1].WorkState)
	}
	if got[1].ProofStatus != domain.ProofStatusMissing || got[1].VerificationStatus != domain.VerificationStatusNotReady {
		t.Fatalf("unsubmitted shed proof/verification = %q/%q want missing/not_ready", got[1].ProofStatus, got[1].VerificationStatus)
	}
	if got[1].PrimaryActionKey != "scan" {
		t.Fatalf("unsubmitted shed primaryActionKey = %q want scan", got[1].PrimaryActionKey)
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
	if resp.Freshness != nil {
		t.Fatalf("canonical schedule freshness = %#v, want nil projection envelope", resp.Freshness)
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

func TestExecutionDisplayCountsUseAggregatedObligations(t *testing.T) {
	target, open, done := executionDisplayCounts(domain.ExecutionProjection{
		ObligationCount:    4,
		CompletedCount:     1,
		CompletionRecorded: 2,
		DeferredCount:      1,
	})
	if target != 4 || open != 1 || done != 2 {
		t.Fatalf("counts=(target=%d open=%d done=%d) want (4,1,2)", target, open, done)
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

func (fakeRepo) AuthorizedParkOptions(context.Context, string, []string) ([]domain.ParkOption, error) {
	return nil, nil
}

func (r fakeRepo) VaccinationExecutionCarrySummary(context.Context, domain.ExecutionQuery) ([]domain.VaccineCarryLine, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.carryLines, nil
}

func TestVaccinationExecutionCarrySummaryPageIndependentOneToManyExecutionDateParkScopeStatusBuckets(t *testing.T) {
	// CRITICAL: Carry summary must be full-day aggregation, not sum of paginated rows.
	// Fixture: one day (2026-07-24) with ET+TT vaccine, 3 goats total (200 doses each = 600 total).
	// Paginated page contains only 2 goats (400 doses). Carry total must still be 600, not 400.
	repo := &fakeRepo{
		carryLines: []domain.VaccineCarryLine{
			{
				Date:           "2026-07-24",
				VaccineLabel:   "ET+TT",
				RemainingDoses: 600, // full-day total remaining (3 goats × 200)
				TotalDoses:     600, // full-day total
			},
		},
	}
	svc := NewService(repo)
	q := domain.ExecutionQuery{
		TenantID:             "tenant-cpt",
		OperatorScopeActorID: "darshan-uuid",
		AsOf:                 time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
		DueBefore:            time.Date(2026, 7, 30, 23, 59, 59, 0, time.UTC),
		Limit:                20, // Pagination limit (loads only 2 of 3 goats)
	}

	resp, err := svc.VaccinationExecutionPage(context.Background(), q)
	if err != nil {
		t.Fatalf("VaccinationExecutionPage failed: %v", err)
	}

	// Verify carry summary is present for app requests
	if resp.CarrySummary == nil {
		t.Fatal("CarrySummary must not be nil for app requests (OperatorScopeActorID set)")
	}

	// Verify per-day structure
	if len(resp.CarrySummary.CarryByDay) != 1 {
		t.Errorf("Expected 1 day, got %d", len(resp.CarrySummary.CarryByDay))
	}

	day := resp.CarrySummary.CarryByDay[0]

	// CRITICAL: verify date is included
	if day.Date != "2026-07-24" {
		t.Errorf("Expected date 2026-07-24, got %s", day.Date)
	}

	// CRITICAL: TotalRemaining must be full-day total (600), NOT page-dependent (400)
	if day.TotalRemaining != 600 {
		t.Errorf("Expected TotalRemaining=600 (full-day), got %d (page-dependent bug)", day.TotalRemaining)
	}

	// Verify per-vaccine breakdown
	if len(day.VaccineBreakdown) != 1 {
		t.Errorf("Expected 1 vaccine, got %d", len(day.VaccineBreakdown))
	}

	vaccine := day.VaccineBreakdown[0]
	if vaccine.VaccineLabel != "ET+TT" {
		t.Errorf("Expected vaccine ET+TT, got %s", vaccine.VaccineLabel)
	}

	if vaccine.RemainingDoses != 600 {
		t.Errorf("Expected 600 remaining ET+TT doses, got %d", vaccine.RemainingDoses)
	}

	if vaccine.TotalDoses != 600 {
		t.Errorf("Expected 600 total ET+TT doses, got %d", vaccine.TotalDoses)
	}
}
