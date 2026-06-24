package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

const (
	testTenant = "00000000-0000-4000-8000-000000000001"
	testLoad   = "ac000000-0000-4000-8000-000000000001"
	testGoat   = "10000000-0000-4000-8000-000000000001"
	testParty  = "20000000-0000-4000-8000-000000000001"
	testPark   = "54000000-0000-4000-8000-000000000001"
	testShed   = "55000000-0000-4000-8000-000000000001"
)

func TestSourceWarmupFortyFiveToSeventyDaysRemainsValid(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	start := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	end := start.Add(70 * 24 * time.Hour)
	goat, err := svc.AddGoatToLoad(context.Background(), ports.AddGoatToLoad{
		TenantID:        testTenant,
		LoadID:          testLoad,
		SourceTag:       strPtr("SRC-70"),
		WarmupStartedAt: &start,
		WarmupEndedAt:   &end,
		IdempotencyKey:  "add-70-day-warmup",
	})
	if err != nil {
		t.Fatalf("AddGoatToLoad() error = %v", err)
	}
	if goat.WarmupDays == nil || *goat.WarmupDays != 70 {
		t.Fatalf("warmup days = %v, want 70", goat.WarmupDays)
	}
	if repo.lastAdd.CurrentState != domain.GoatStateSourceWarmup {
		t.Fatalf("current state = %q", repo.lastAdd.CurrentState)
	}
}

func TestRejectAndFailedSourceHealthCancelOpenVaccination(t *testing.T) {
	repo := &fakeRepo{}
	cancel := &fakeCanceler{}
	svc := NewService(repo).WithVaccinationCanceler(cancel)

	_, err := svc.RecordSourceHealth(context.Background(), ports.SourceHealth{
		TenantID:       testTenant,
		LoadID:         testLoad,
		GoatID:         testGoat,
		HealthState:    domain.HealthFailed,
		IdempotencyKey: "health-failed",
	})
	if err != nil {
		t.Fatalf("RecordSourceHealth() error = %v", err)
	}
	_, err = svc.PreDispatchDecision(context.Background(), ports.Decision{
		TenantID:       testTenant,
		LoadID:         testLoad,
		GoatID:         testGoat,
		DecisionType:   domain.DecisionRejected,
		IdempotencyKey: "pre-dispatch-reject",
	})
	if err != nil {
		t.Fatalf("PreDispatchDecision() error = %v", err)
	}
	if len(cancel.reasons) != 2 {
		t.Fatalf("cancel calls = %v, want 2", cancel.reasons)
	}
	_, err = svc.PreDispatchDecision(context.Background(), ports.Decision{
		TenantID:       testTenant,
		LoadID:         testLoad,
		GoatID:         testGoat,
		DecisionType:   domain.DecisionRejected,
		Reason:         "sold during holding",
		IdempotencyKey: "holding-sold",
	})
	if err != nil {
		t.Fatalf("PreDispatchDecision(sold) error = %v", err)
	}
	if len(cancel.reasons) != 3 {
		t.Fatalf("terminal exit cancel calls = %v, want 3", cancel.reasons)
	}
}

func TestArrivalRejectedAndUnknownExtraStayOutOfVaccination(t *testing.T) {
	repo := &fakeRepo{}
	cancel := &fakeCanceler{}
	svc := NewService(repo).WithVaccinationCanceler(cancel)
	_, err := svc.RecordArrivalReview(context.Background(), ports.ArrivalReview{
		TenantID:       testTenant,
		LoadID:         testLoad,
		ParkLocationID: testPark,
		ExpectedCount:  2,
		LoadedCount:    2,
		ArrivedCount:   3,
		MatchedCount:   1,
		MissingCount:   1,
		ExtraCount:     1,
		Status:         "mismatch",
		IdempotencyKey: "arrival-mismatch",
		Goats: []ports.ArrivalGoat{
			{GoatID: strPtr(testGoat), ArrivalState: "rejected"},
			{TemporaryID: strPtr("UNKNOWN-1"), ArrivalState: "extra_unresolved"},
		},
	})
	if err != nil {
		t.Fatalf("RecordArrivalReview() error = %v", err)
	}
	if len(cancel.reasons) == 0 {
		t.Fatal("arrival rejected goat did not cancel open vaccination obligations")
	}
}

func TestAcceptedIntakeCreatesPHCHandoffWithoutCancel(t *testing.T) {
	repo := &fakeRepo{}
	cancel := &fakeCanceler{}
	svc := NewService(repo).WithVaccinationCanceler(cancel)
	handoffs, err := svc.AcceptIntake(context.Background(), ports.AcceptIntake{
		TenantID:       testTenant,
		LoadID:         testLoad,
		GoatIDs:        []string{testGoat},
		ParkLocationID: testPark,
		ShedLocationID: testShed,
		IdempotencyKey: "accepted-intake",
	})
	if err != nil {
		t.Fatalf("AcceptIntake() error = %v", err)
	}
	if len(handoffs) != 1 || handoffs[0].GoatID != testGoat {
		t.Fatalf("handoffs = %#v", handoffs)
	}
	if len(cancel.reasons) != 0 {
		t.Fatalf("accepted intake should not cancel: %#v", cancel.reasons)
	}
}

func TestAcceptedIntakeRejectsInvalidTransition(t *testing.T) {
	repo := &fakeRepo{acceptErr: ports.ErrInvalidTransition}
	svc := NewService(repo)
	_, err := svc.AcceptIntake(context.Background(), ports.AcceptIntake{
		TenantID:       testTenant,
		LoadID:         testLoad,
		GoatIDs:        []string{testGoat},
		ParkLocationID: testPark,
		ShedLocationID: testShed,
		IdempotencyKey: "arrival-rejected-intake",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "invalid_intake_transition" {
		t.Fatalf("AcceptIntake() error = %v, want invalid_intake_transition", err)
	}
}

func TestWorkDefaultsAcceptsOwnerMissingContractState(t *testing.T) {
	ownerMissing := "owner_missing"
	repo := &fakeRepo{}
	svc := NewService(repo)
	_, err := svc.ActionCenter(context.Background(), domain.WorkQuery{
		TenantID:  testTenant,
		WorkState: &ownerMissing,
	})
	if err != nil {
		t.Fatalf("ActionCenter(owner_missing) error = %v", err)
	}
	if repo.lastWorkQuery.WorkState == nil || *repo.lastWorkQuery.WorkState != ownerMissing {
		t.Fatalf("work_state query = %#v, want owner_missing", repo.lastWorkQuery.WorkState)
	}
}

func TestControlTowerOnlyIncludesExceptionRows(t *testing.T) {
	repo := &fakeRepo{workRows: []domain.WorkRow{
		{
			RowID:      "load_goat:10000000-0000-4000-8000-000000000010",
			LoadID:     testLoad,
			WorkType:   "source_health",
			WorkState:  "due",
			Severity:   "watch",
			Title:      "Complete source health SOP",
			Detail:     "health pending",
			NextAction: "Complete source health SOP",
		},
		{
			RowID:      "load_goat:10000000-0000-4000-8000-000000000011",
			LoadID:     testLoad,
			WorkType:   "ownership",
			WorkState:  "owner_missing",
			Severity:   "watch",
			Title:      "Resolve source ownership",
			Detail:     "ownership pending",
			NextAction: "Resolve source ownership",
		},
		{
			RowID:      "load_goat:10000000-0000-4000-8000-000000000012",
			LoadID:     testLoad,
			WorkType:   "dispatch_proof",
			WorkState:  "proof_pending",
			Severity:   "watch",
			Title:      "Upload truck loading proof",
			Detail:     "proof missing",
			NextAction: "Upload truck loading proof",
		},
		{
			RowID:      "load_goat:10000000-0000-4000-8000-000000000013",
			LoadID:     testLoad,
			WorkType:   "accepted_intake",
			WorkState:  "completed",
			Severity:   "ok",
			Title:      "Accepted intake complete",
			Detail:     "complete",
			NextAction: "No action",
		},
	}}
	svc := NewService(repo)
	response, err := svc.ControlTower(context.Background(), domain.WorkQuery{TenantID: testTenant})
	if err != nil {
		t.Fatalf("ControlTower() error = %v", err)
	}
	if !repo.lastWorkQuery.ExceptionOnly {
		t.Fatal("ControlTower() did not request exception-only work rows")
	}
	if len(response.Alerts) != 2 {
		t.Fatalf("alerts = %#v, want owner_missing and proof_pending only", response.Alerts)
	}
	if response.Summary.OpenGapCount != 2 || response.Summary.OwnerMissingCount != 1 || response.Summary.MissingProofCount != 1 {
		t.Fatalf("summary = %#v, want exception-only counts", response.Summary)
	}
	for _, alert := range response.Alerts {
		if alert.WorkState == "due" || alert.WorkState == "completed" {
			t.Fatalf("non-exception alert leaked into Control Tower: %#v", alert)
		}
	}
}

type fakeRepo struct {
	lastAdd       ports.AddGoatToLoad
	lastWorkQuery domain.WorkQuery
	workRows      []domain.WorkRow
	acceptErr     error
}

func (f *fakeRepo) Ping(context.Context) error { return nil }
func (f *fakeRepo) ListLoads(context.Context, domain.LoadQuery) (domain.LoadListResult, error) {
	return domain.LoadListResult{}, nil
}
func (f *fakeRepo) CreateLoad(context.Context, ports.CreateLoad) (domain.Load, error) {
	return domain.Load{LoadID: testLoad, TenantID: testTenant, SourcePartyID: testParty, Context: json.RawMessage(`{}`)}, nil
}
func (f *fakeRepo) GetLoadDetail(context.Context, string, string) (domain.LoadDetail, error) {
	return domain.LoadDetail{Goats: []domain.LoadGoat{{GoatID: testGoat, CurrentState: domain.GoatStateArrivalRejected}}}, nil
}
func (f *fakeRepo) AddGoatToLoad(_ context.Context, in ports.AddGoatToLoad) (domain.LoadGoat, error) {
	f.lastAdd = in
	return domain.LoadGoat{LoadID: in.LoadID, GoatID: testGoat, CurrentState: in.CurrentState, WarmupDays: in.WarmupDays}, nil
}
func (f *fakeRepo) RecordSourceHealth(_ context.Context, in ports.SourceHealth) (domain.SourceHealthCheck, error) {
	return domain.SourceHealthCheck{LoadID: in.LoadID, GoatID: in.GoatID, HealthState: in.HealthState}, nil
}
func (f *fakeRepo) RecordDecision(_ context.Context, in ports.Decision) (domain.Decision, error) {
	return domain.Decision{LoadID: in.LoadID, GoatID: in.GoatID, DecisionType: in.DecisionType}, nil
}
func (f *fakeRepo) DispatchLoad(_ context.Context, in ports.DispatchLoad) (domain.TransitHandoff, error) {
	return domain.TransitHandoff{LoadID: in.LoadID, LoadedCount: len(in.GoatIDs)}, nil
}
func (f *fakeRepo) RecordArrivalReview(_ context.Context, in ports.ArrivalReview) (domain.ArrivalReview, error) {
	return domain.ArrivalReview{LoadID: in.LoadID, Status: in.Status}, nil
}
func (f *fakeRepo) AcceptIntake(_ context.Context, in ports.AcceptIntake) ([]domain.PHCHandoff, error) {
	if f.acceptErr != nil {
		return nil, f.acceptErr
	}
	return []domain.PHCHandoff{{LoadID: in.LoadID, GoatID: in.GoatIDs[0], ParkLocationID: in.ParkLocationID, ShedLocationID: in.ShedLocationID}}, nil
}
func (f *fakeRepo) ListWorkRows(_ context.Context, q domain.WorkQuery) (domain.WorkListResult, error) {
	f.lastWorkQuery = q
	return domain.WorkListResult{Rows: f.workRows}, nil
}
func (f *fakeRepo) GetWorkRow(context.Context, domain.WorkQuery, string) (domain.WorkRow, bool, error) {
	return domain.WorkRow{}, false, nil
}

type fakeCanceler struct {
	reasons []string
}

func (f *fakeCanceler) CancelOpenForGoat(_ context.Context, _, _, reason string) (int, error) {
	f.reasons = append(f.reasons, reason)
	return 1, nil
}

func strPtr(v string) *string { return &v }
