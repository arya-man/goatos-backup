package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type fakeRepo struct {
	rows    []domain.ExecutionProjection
	opsRows []domain.OperationsRow
	roster  []domain.ScanRosterRow
	err     error
}

func (r fakeRepo) ScanRoster(_ context.Context, _ domain.ScanRosterQuery) ([]domain.ScanRosterRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.roster, nil
}

func (r fakeRepo) VaccinationOperations(_ context.Context, _ domain.OperationsQuery) ([]domain.OperationsRow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.opsRows, nil
}

func (r fakeRepo) ListVaccinationExecution(_ context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error) {
	if r.err != nil {
		return nil, r.err
	}
	if q.WorkState == nil {
		return r.rows, nil
	}
	out := make([]domain.ExecutionProjection, 0, len(r.rows))
	for _, p := range r.rows {
		if workState(p, q) == *q.WorkState {
			out = append(out, p)
		}
	}
	return out, nil
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
		projection("shed-owner-missing", dueTomorrow, 1, func(p *domain.ExecutionProjection) {
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
		domain.WorkStateOwnerMissing,
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
