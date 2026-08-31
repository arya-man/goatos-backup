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

func (r fakeRepo) VaccinationCommandBoard(_ context.Context, _ domain.CommandBoardQuery) (domain.CommandBoardResponse, error) {
	return domain.CommandBoardResponse{}, nil
}

// The command-board drilldowns. Stubs: the Service is a pass-through for these, so the behaviour
// under test lives in the repository's SQL and its own integration tests.

func (r fakeRepo) CommandBoardCohortMatrix(_ context.Context, _ domain.CommandBoardDrilldownQuery) (domain.CommandBoardCohortMatrixPage, error) {
	return domain.CommandBoardCohortMatrixPage{}, nil
}

func (r fakeRepo) CommandBoardShedDoseMatrix(_ context.Context, _ domain.CommandBoardDrilldownQuery) (domain.CommandBoardShedDoseMatrixPage, error) {
	return domain.CommandBoardShedDoseMatrixPage{}, nil
}

func (r fakeRepo) CommandBoardClosedWithoutDoseAnimals(_ context.Context, _ domain.CommandBoardDrilldownQuery) (domain.CommandBoardClosedWithoutDosePage, error) {
	return domain.CommandBoardClosedWithoutDosePage{}, nil
}

func (r fakeRepo) CommandBoardShedVaccineAnimals(_ context.Context, _ domain.CommandBoardShedVaccineAnimalsQuery) (domain.CommandBoardShedVaccineAnimalsPage, error) {
	return domain.CommandBoardShedVaccineAnimalsPage{}, nil
}

func (r fakeRepo) CommandBoardCohortExceptions(_ context.Context, _ domain.CommandBoardCohortCellQuery) (domain.CommandBoardCohortExceptionsPage, error) {
	return domain.CommandBoardCohortExceptionsPage{}, nil
}

func (r fakeRepo) CommandBoardCohortDays(_ context.Context, _ domain.CommandBoardCohortCellQuery) (domain.CommandBoardCohortDaysPage, error) {
	return domain.CommandBoardCohortDaysPage{}, nil
}

func (r fakeRepo) CommandBoardDriveOptions(_ context.Context, _ domain.CommandBoardDriveOptionsQuery) (domain.CommandBoardDriveOptionsPage, error) {
	return domain.CommandBoardDriveOptionsPage{}, nil
}

func (r fakeRepo) LiveTracker(_ context.Context, _ domain.LiveTrackerQuery) (domain.LiveTrackerResponse, error) {
	return domain.LiveTrackerResponse{}, nil
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

func (r fakeRepo) UpsertCapacityConfig(_ context.Context, _ string, cfg domain.CapacityConfig) (domain.CapacityConfig, error) {
	cfg.RowVersion++
	return cfg, nil
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
			p.ProofSubmittedCount = 2
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
	if row.AcceptedCount != 0 || row.ReviewCount != 0 {
		t.Fatalf("accepted/review = %d/%d want 0/0", row.AcceptedCount, row.ReviewCount)
	}
}

func TestVaccinationExecutionPartialProofProgressRemainsOpen(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	due := asOf
	operator := "Natheswar"
	batchStatus := "planned"
	taskID := "1c83c480-2c57-4527-8579-bde3d7c29244"
	rows := []domain.ExecutionProjection{
		projection("ho-chi-minh-1", due, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.SOPTaskID = &taskID
			p.BatchStatus = &batchStatus
			p.ObligationCount = 17
			p.ScheduledCount = 17
			p.ScannedCount = 11
			p.ProofSubmittedCount = 11
			p.WorkState = domain.WorkStateVerificationPending
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
	if row.WorkState != domain.WorkStateInProgress {
		t.Fatalf("workState = %q want %q for partial proof progress", row.WorkState, domain.WorkStateInProgress)
	}
	if row.SOPStatus != domain.SOPStatusNotStarted || row.VerificationStatus != domain.VerificationStatusNotReady {
		t.Fatalf("sop/verification = %q/%q want not_started/not_ready while 6 animals are still open", row.SOPStatus, row.VerificationStatus)
	}
	if row.PrimaryActionKey != "scan" {
		t.Fatalf("primaryActionKey = %q want scan", row.PrimaryActionKey)
	}
	if row.TargetCount != 17 || row.OpenCount != 6 || row.DoneCount != 11 {
		t.Fatalf("counts = target %d open %d done %d want 17/6/11", row.TargetCount, row.OpenCount, row.DoneCount)
	}
}

func TestVaccinationExecutionAcceptedCompletionWinsOverStaleSubmittedProof(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	due := asOf.Add(24 * time.Hour)
	operator := "Operator A"
	rows := []domain.ExecutionProjection{
		projection("shed-complete", due, 1, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.ObligationCount = 2
			p.CompletedCount = 2
			p.CompletionAccepted = 2
			p.ProofSubmittedCount = 1
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
	if row.WorkState != domain.WorkStateCompleted {
		t.Fatalf("workState = %q want completed", row.WorkState)
	}
	if row.ProofStatus != domain.ProofStatusAccepted || row.VerificationStatus != domain.VerificationStatusVerified {
		t.Fatalf("proof/verification = %q/%q want accepted/verified", row.ProofStatus, row.VerificationStatus)
	}
	if row.AcceptedCount != 2 || row.ReviewCount != 0 {
		t.Fatalf("accepted/review = %d/%d want 2/0", row.AcceptedCount, row.ReviewCount)
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
	// With openCount=1 (1 animal done, 1 still open), state stays in_progress even with proof submitted
	if got[0].WorkState != domain.WorkStateInProgress || got[0].ProofStatus != domain.ProofStatusUploaded {
		t.Fatalf("partially proofed shed state/proof = %q/%q want in_progress/uploaded (openCount=1)", got[0].WorkState, got[0].ProofStatus)
	}
	if got[0].PrimaryActionKey != "scan" {
		t.Fatalf("partially proofed shed primaryActionKey = %q want scan", got[0].PrimaryActionKey)
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
			p.PhysicalShed = "Godel 1"
			p.Partition = "Part 3"
		}),
		projection("shed-1", due, 2, func(p *domain.ExecutionProjection) {
			p.OperatorName = &operator
			p.CompletedCount = 1
			p.CompletionAccepted = 1
			p.PhysicalShed = "Godel 1"
			p.Partition = "Part 3"
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
	if detail.PartitionLabel == nil || *detail.PartitionLabel != "Part 3" || detail.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("drilldown location = partition %v display %q", detail.PartitionLabel, detail.OperationalLocationDisplay)
	}
	if len(detail.Rows) != 2 || detail.Rows[0].PartitionLabel == nil || *detail.Rows[0].PartitionLabel != "Part 3" || detail.Rows[0].OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("execution row location = %#v", detail.Rows)
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

func TestExecutionDisplayCountsDoNotTreatScansAsDone(t *testing.T) {
	target, open, done := executionDisplayCounts(domain.ExecutionProjection{
		ObligationCount: 2,
		ScannedCount:    2,
	})
	if target != 2 || open != 2 || done != 0 {
		t.Fatalf("counts=(target=%d open=%d done=%d) want (2,2,0)", target, open, done)
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

func (r fakeRepo) VaccinationExecutionCardSummaries(_ context.Context, q domain.ExecutionQuery) (map[string]*domain.ShedCardSummary, error) {
	if r.err != nil {
		return nil, r.err
	}
	// Compute summaries from FILTERED rows matching the page query's filter predicates.
	// This is the whole-filter aggregate that the SQL GROUP BY will do.
	// Convert ExecutionProjection rows to ExecutionRow, apply filters, and compute summaries.
	var execRows []domain.ExecutionRow
	for _, p := range r.rows {
		// Apply the same filters as the page query:
		// 1. work_state filter
		if q.WorkState != nil {
			if workStateFromProjection(p, q) != *q.WorkState {
				continue
			}
		}
		// 2. severity filter
		if q.Severity != nil {
			state := workStateFromProjection(p, q)
			sev := severity(state)
			if sev != *q.Severity {
				continue
			}
		}
		// 3. open-only filter (if OpenOnly is true, skip completed/deferred/missed rows)
		if q.OpenOnly {
			state := workStateFromProjection(p, q)
			if state == domain.WorkStateCompleted || state == domain.WorkStateDeferred || state == domain.WorkStateMissed {
				continue
			}
		}
		// Row passed filters; convert to ExecutionRow
		row := rowFromProjection(p, q)
		execRows = append(execRows, row)
	}
	return computeCardSummariesFromRows(execRows), nil
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

// TestExecutionDisplayCountsTreatsRejectedAsOpenNotDone is the completionEvidence fix (required
// scenario 4's counting half): a rejected animal is outstanding work, so it must land in `open`,
// not be folded into `done`. A shed of 5 with 1 rejection must report done=4, open=1 -- never
// done=5/open=0, which hid the redo from the operator's own card.
func TestExecutionDisplayCountsTreatsRejectedAsOpenNotDone(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:    5,
		CompletedCount:     0,
		CompletionRecorded: 4,
		CompletionAccepted: 0,
		CompletionRejected: 1,
	}
	target, open, done := executionDisplayCounts(p)
	if target != 5 {
		t.Fatalf("target = %d, want 5", target)
	}
	if done != 4 {
		t.Fatalf("done = %d, want 4 (rejected must NOT count as done)", done)
	}
	if open != 1 {
		t.Fatalf("open = %d, want 1 (the rejected animal is outstanding work)", open)
	}
}

// Value receiver: this fake is used as a struct value, not a pointer.
// TestVaccinationCardLockInvariant_NeedsReviewWithOpenWork reproduces the field bug: a card at
// target=17 done=11 open=6, mixed needs_review/pending/verification_pending state, with no final
// submission. CORE INVARIANT: this MUST NOT lock the card.
func TestVaccinationCardLockInvariant_NeedsReviewWithOpenWork(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:    17,
		CompletedCount:     11,
		DoneCount:          11,
		CompletionRecorded: 11, // proofed, awaiting verdict (needs_review)
		CompletionAccepted: 0,
		CompletionRejected: 0,
		OperatorName:       strPtr("Amit"),
	}
	_, openCount, _ := executionDisplayCounts(p)
	if openCount != 6 {
		t.Fatalf("openCount = %d, want 6", openCount)
	}
	canContinue, reason := computeOperatorLockState(p, openCount, domain.WorkStateVerificationPending)
	if !canContinue {
		t.Fatalf("OperatorCanContinue = %v, want true (reason=%s)", canContinue, reason)
	}
	if reason != "none" {
		t.Fatalf("OperatorLockedReason = %q, want %q", reason, "none")
	}
}

// TestVaccinationCardLockInvariant_FinalSubmit covers the genuinely locked case: target=17
// done=17 open=0, all completions accepted (a real final submission). MUST lock the card.
func TestVaccinationCardLockInvariant_FinalSubmit(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:    17,
		CompletedCount:     17,
		DoneCount:          17,
		CompletionRecorded: 0,
		CompletionAccepted: 17,
		CompletionRejected: 0,
		OperatorName:       strPtr("Amit"),
	}
	_, openCount, _ := executionDisplayCounts(p)
	if openCount != 0 {
		t.Fatalf("openCount = %d, want 0", openCount)
	}
	canContinue, reason := computeOperatorLockState(p, openCount, domain.WorkStateCompleted)
	if canContinue {
		t.Fatalf("OperatorCanContinue = %v, want false", canContinue)
	}
	if reason != "final_submitted" {
		t.Fatalf("OperatorLockedReason = %q, want %q", reason, "final_submitted")
	}
}

// TestVaccinationCardLockInvariant_AllProofedNotFinalized covers the drive-close loop state:
// All animals proofed (done_count = obligation_count) but not finalized submission yet.
// Card should be UNLOCKED so operator can finalize the submission.
func TestVaccinationCardLockInvariant_AllProofedNotFinalized(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:    10,
		CompletedCount:     0,  // No direct completion path
		DoneCount:          10, // All done via proof
		CompletionRecorded: 10, // All proofed, awaiting finalization
		CompletionAccepted: 0,  // None finalized yet
		CompletionRejected: 0,
		OperatorName:       strPtr("Amit"),
	}
	_, openCount, _ := executionDisplayCounts(p)
	if openCount != 0 {
		t.Fatalf("openCount = %d, want 0 (all proofed)", openCount)
	}
	canContinue, reason := computeOperatorLockState(p, openCount, domain.WorkStateVerificationPending)
	if !canContinue {
		t.Fatalf("OperatorCanContinue = %v, want true for all-proofed-not-finalized (reason=%s)", canContinue, reason)
	}
	if reason != "none" {
		t.Fatalf("OperatorLockedReason = %q, want %q for all-proofed state", reason, "none")
	}
}

// TestVaccinationCardLockInvariant_FinalizedAllAccepted covers the locked case with all completions accepted.
// This is a true final submission: all work done, all proofs verified and accepted.
// Card is LOCKED with reason="final_submitted".
func TestVaccinationCardLockInvariant_FinalizedAllAccepted(t *testing.T) {
	p := domain.ExecutionProjection{
		ObligationCount:    10,
		CompletedCount:     0,
		DoneCount:          10,
		CompletionRecorded: 0,  // All verified
		CompletionAccepted: 10, // All accepted
		CompletionRejected: 0,
		OperatorName:       strPtr("Amit"),
	}
	_, openCount, _ := executionDisplayCounts(p)
	if openCount != 0 {
		t.Fatalf("openCount = %d, want 0", openCount)
	}
	canContinue, reason := computeOperatorLockState(p, openCount, domain.WorkStateCompleted)
	if canContinue {
		t.Fatalf("OperatorCanContinue = %v, want false for finalized+accepted card", canContinue)
	}
	if reason != "final_submitted" {
		t.Fatalf("OperatorLockedReason = %q, want %q for finalized+accepted", reason, "final_submitted")
	}
}

func strPtr(s string) *string { return &s }

// paginatingFakeRepo extends fakeRepo to properly implement pagination.
// It computes card summaries from ALL rows (the full filter set, not just the page).
type paginatingFakeRepo struct {
	fakeRepo
}

// ListVaccinationExecutionPage returns paginated results with proper limit enforcement.
func (r *paginatingFakeRepo) ListVaccinationExecutionPage(_ context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error) {
	if r.fakeRepo.err != nil {
		return domain.ExecutionProjectionPage{}, r.fakeRepo.err
	}

	// Filter by work state if specified
	filtered := r.fakeRepo.rows
	if q.WorkState != nil {
		out := make([]domain.ExecutionProjection, 0, len(r.fakeRepo.rows))
		for _, p := range r.fakeRepo.rows {
			if workStateFromProjection(p, q) == *q.WorkState {
				out = append(out, p)
			}
		}
		filtered = out
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 200
	}

	// Paginate: return limit rows, set NextCursor if more exist
	var nextCursor *domain.ExecutionCursor
	if len(filtered) > limit {
		filtered = filtered[:limit]
		// For simplicity, use a basic cursor (in real code, the Postgres adapter creates proper ones)
		nextCursor = &domain.ExecutionCursor{SortRank: 1, SortDueMicros: 0, SortRowKey: "next"}
	}

	return domain.ExecutionProjectionPage{
		Rows:       filtered,
		TotalCount: int64(len(r.fakeRepo.rows)), // Total count ignores limit (page-independent)
		NextCursor: nextCursor,
	}, nil
}

// TestCardSummaryReflectsAllRowsNotPaginatedSubset verifies that card summaries are computed
// from ALL matching rows (full-filter aggregate), not just the paginated subset. A card
// straddling a page boundary must not report incorrect counts based on the loaded page alone.
// RED test: pass paginatingFakeRepo with >limit rows for ONE card. Request page 1. Assert summary
// counts reflect ALL rows (both pages), not just page 1.
//
// HONEST SCOPE: computeCardSummariesFromRows (the function this test exercises) is a Go-level
// fake that mirrors the same page-independent, work_state-filtered aggregation semantics as
// production's cardSummariesSQL, but production never calls it -- VaccinationExecutionCardSummaries
// in app/service.go always queries Postgres via repo.VaccinationExecutionCardSummaries. This test
// therefore covers "does this aggregation LOGIC do the right thing in Go", not "does the SQL do the
// right thing"; it renders with the same helper it filters with, so it cannot catch a divergence
// between cardSummariesSQL and vaccinationExecutionSQL. The SQL itself -- including the card-grain
// work_state/severity classification shared via executionClassifiedCTE -- is pinned against real
// Postgres by adapters/postgres/card_summaries_integration_test.go (opt-in via
// GOATOS_RUN_POSTGRES_TESTS=1), which is the source of truth for the query text.
func TestCardSummaryReflectsAllRowsNotPaginatedSubset(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	due := asOf.Add(24 * time.Hour)
	shedID := "shed-123"
	batchID := "batch-456"
	limit := 10

	// Create 15 rows for ONE card (same shed, batch) — more than limit (10).
	// Only the first 10 will be returned on page 1, but the summary MUST reflect all 15.
	rows := make([]domain.ExecutionProjection, 0, 15)
	for i := 1; i <= 15; i++ {
		operator := fmt.Sprintf("Operator %d", i)
		p := projection(shedID, due, 1, func(p *domain.ExecutionProjection) {
			p.ShedID = shedID
			p.BatchID = &batchID
			p.OperatorName = &operator
			p.ObligationCount = 1
			// Row 1-10: open (not done)
			// Row 11-15: done (represents the second page)
			if i <= 10 {
				p.DoneCount = 0
				p.CompletionRecorded = 0
				p.CompletionAccepted = 0
			} else {
				p.DoneCount = 1
				p.CompletedCount = 1
				p.CompletionAccepted = 1
			}
		})
		rows = append(rows, p)
	}

	// Use paginating fake repo to enforce limit
	fakeRepoImpl := &paginatingFakeRepo{fakeRepo: fakeRepo{rows: rows}}
	svc := NewService(fakeRepoImpl)

	// Request page 1 with limit=10 (will get rows 1-10 only)
	resp, err := svc.VaccinationExecutionPage(context.Background(), domain.ExecutionQuery{
		TenantID:  "tenant",
		AsOf:      asOf,
		DueBefore: asOf.Add(30 * 24 * time.Hour),
		Limit:     limit,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionPage() error = %v", err)
	}

	// Page should have 10 rows (the limit)
	if len(resp.Rows) != limit {
		t.Fatalf("got %d rows on page 1, want %d", len(resp.Rows), limit)
	}

	// There must be a next cursor since totalCount (15) > limit (10)
	if resp.NextCursor == nil {
		t.Fatal("expected NextCursor to be set when totalCount > limit")
	}

	// TotalCount should reflect ALL rows, not just the paginated subset
	if resp.TotalCount != 15 {
		t.Fatalf("TotalCount = %d, want 15 (all matching rows)", resp.TotalCount)
	}

	// The critical check: card summary MUST reflect ALL 15 rows, not just the 10 on this page.
	// Expected: TargetCount=15 (all rows), DoneCount=5 (rows 11-15), OpenCount=10 (rows 1-10)
	if resp.CardSummaries == nil {
		t.Fatal("CardSummaries should not be nil")
	}

	// Build the expected card ID
	cardID := domain.BuildCardID(shedID, "whole", "", batchID, "")
	summary, ok := resp.CardSummaries[cardID]
	if !ok {
		t.Fatalf("card %q not found in summaries. available cards: %v", cardID, len(resp.CardSummaries))
	}

	// CRITICAL: Card summary must reflect ALL rows across BOTH pages.
	// The current broken implementation computes the summary from only page 1 rows (1-10),
	// which have DoneCount=0 and CompletionAccepted=0.
	// Under maxInt logic (current broken code):
	// - TargetCount = maxInt(1,1,...,1) = 1 (WRONG: should be 15)
	// - DoneCount = maxInt(0,0,...,0) = 0 (WRONG: should be 5)
	//
	// After the FIX (SQL GROUP BY aggregate):
	// - TargetCount = SUM(ObligationCount) = 15 (sum across all matching rows)
	// - DoneCount = SUM(DoneCount) = 5 (rows 11-15 have DoneCount=1)
	// - OpenCount = SUM(1-DoneCount) = 10 (rows 1-10 have DoneCount=0)

	t.Logf("Card summary: target=%d done=%d open=%d", summary.TargetCount, summary.DoneCount, summary.OpenCount)

	// This will FAIL on current code (before the fix) because the summary aggregates
	// only the paginated subset (page 1 rows).
	wantTargetCount := 15 // All 15 rows, each with ObligationCount=1
	wantDoneCount := 5    // Rows 11-15 have DoneCount=1
	wantOpenCount := 10   // Rows 1-10 have DoneCount=0 (so open=1)

	if summary.TargetCount != wantTargetCount {
		t.Errorf("TargetCount = %d, want %d (all rows). BUG: aggregates only paginated subset!", summary.TargetCount, wantTargetCount)
	}
	if summary.DoneCount != wantDoneCount {
		t.Errorf("DoneCount = %d, want %d (rows 11-15). BUG: aggregates only paginated subset!", summary.DoneCount, wantDoneCount)
	}
	if summary.OpenCount != wantOpenCount {
		t.Errorf("OpenCount = %d, want %d (rows 1-10). BUG: aggregates only paginated subset!", summary.OpenCount, wantOpenCount)
	}
}

// TestCardSummaryRespectsWorkStateFilter verifies that card summaries respect work_state filters.
// A card may have rows with different work states (e.g., completed and overdue). When filtering by
// work_state, the summary must count ONLY rows matching that filter, not all rows in the card.
//
// HONEST SCOPE: same fake-mirrors-production-semantics caveat as
// TestCardSummaryReflectsAllRowsNotPaginatedSubset above -- this exercises the Go-level
// computeCardSummariesFromRows fake, not the production SQL path. The SQL truth (including that
// work_state can reach 'blocked'/'rejected', which was unreachable through cardSummariesSQL's old
// per-row eff_status proxy before the executionClassifiedCTE sharing fix) is pinned by
// adapters/postgres/card_summaries_integration_test.go.
// RED test: card has 6 rows: 3 completed (done=1) + 3 overdue (done=0). Filter by work_state=overdue.
// Page shows 3 rows, summary must show TargetCount=3 (only overdue), DoneCount=0, OpenCount=3.
func TestCardSummaryRespectsWorkStateFilter(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	pastDue := asOf.Add(-24 * time.Hour)  // Overdue date
	futureDue := asOf.Add(24 * time.Hour) // Due (not overdue)
	shedID := "shed-work-filter"
	batchID := "batch-work-filter"

	// Create 6 rows for ONE card (same shed, batch):
	// Rows 1-3: completed (DueCount=0, CompletedCount=1, done_count=1)
	// Rows 4-6: overdue (DueCount=0, CompletedCount=0, done_count=0, due date in past)
	rows := make([]domain.ExecutionProjection, 0, 6)

	// Add 3 completed rows
	for i := 1; i <= 3; i++ {
		operator := fmt.Sprintf("Operator %d", i)
		p := projection(shedID, futureDue, 1, func(p *domain.ExecutionProjection) {
			p.ShedID = shedID
			p.BatchID = &batchID
			p.OperatorName = &operator
			p.ObligationCount = 1
			p.DoneCount = 1
			p.CompletedCount = 1
			p.DueCount = 0
			p.CompletionAccepted = 1
		})
		rows = append(rows, p)
	}

	// Add 3 overdue rows (due in past, not completed)
	for i := 4; i <= 6; i++ {
		operator := fmt.Sprintf("Operator %d", i)
		p := projection(shedID, pastDue, 1, func(p *domain.ExecutionProjection) {
			p.ShedID = shedID
			p.BatchID = &batchID
			p.OperatorName = &operator
			p.ObligationCount = 1
			p.DoneCount = 0
			p.CompletedCount = 0
			p.DueCount = 0
			p.CompletionAccepted = 0
		})
		rows = append(rows, p)
	}

	// Use paginating fake repo
	fakeRepoImpl := &paginatingFakeRepo{fakeRepo: fakeRepo{rows: rows}}
	svc := NewService(fakeRepoImpl)

	// Request page with work_state filter = overdue
	overdue := domain.WorkStateOverdue
	resp, err := svc.VaccinationExecutionPage(context.Background(), domain.ExecutionQuery{
		TenantID:  "tenant",
		AsOf:      asOf,
		DueBefore: asOf.Add(30 * 24 * time.Hour),
		Limit:     10,
		WorkState: &overdue,
	})
	if err != nil {
		t.Fatalf("VaccinationExecutionPage() error = %v", err)
	}

	// Page should have only 3 rows (the overdue ones, not the completed ones)
	if len(resp.Rows) != 3 {
		t.Fatalf("got %d rows on page 1, want 3 (only overdue)", len(resp.Rows))
	}

	// Verify all returned rows are overdue
	for i, row := range resp.Rows {
		if row.WorkState != domain.WorkStateOverdue {
			t.Errorf("row %d workState = %q, want overdue", i, row.WorkState)
		}
	}

	// The critical check: card summary MUST reflect ONLY the 3 overdue rows, not all 6.
	if resp.CardSummaries == nil {
		t.Fatal("CardSummaries should not be nil")
	}

	cardID := domain.BuildCardID(shedID, "whole", "", batchID, "")
	summary, ok := resp.CardSummaries[cardID]
	if !ok {
		t.Fatalf("card %q not found in summaries", cardID)
	}

	// CRITICAL: Card summary must reflect ONLY the work_state-filtered rows.
	// Expected after fix: TargetCount=3 (overdue rows), DoneCount=0 (none are done), OpenCount=3
	// Expected before fix (BROKEN): TargetCount=6 (all rows) or TargetCount=1 (aggregation bug)
	t.Logf("Card summary: target=%d done=%d open=%d (filtered by work_state=overdue)", summary.TargetCount, summary.DoneCount, summary.OpenCount)

	wantTargetCount := 3 // Only 3 overdue rows
	wantDoneCount := 0   // None of the overdue rows are done
	wantOpenCount := 3   // All 3 overdue rows are open

	if summary.TargetCount != wantTargetCount {
		t.Errorf("TargetCount = %d, want %d (only overdue rows). BUG: summary includes completed rows outside filter!", summary.TargetCount, wantTargetCount)
	}
	if summary.DoneCount != wantDoneCount {
		t.Errorf("DoneCount = %d, want %d", summary.DoneCount, wantDoneCount)
	}
	if summary.OpenCount != wantOpenCount {
		t.Errorf("OpenCount = %d, want %d", summary.OpenCount, wantOpenCount)
	}
}

func (fakeRepo) ListAlerts(
	_ context.Context, _, _ string, _ bool, _ []string, _ string, _ int,
) (domain.AlertPage, error) {
	return domain.AlertPage{Items: []domain.Alert{}}, nil
}
