package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type milkPreparationHandlerService struct {
	query            domain.MilkPreparationQuery
	page             domain.MilkPreparationPage
	submission       domain.MilkPreparationSubmission
	submitResult     domain.MilkPreparationSubmissionResult
	feedingSubmitErr error
}

func (f *milkPreparationHandlerService) GetSummary(context.Context, domain.HerdRegisterSummaryQuery) (domain.HerdRegisterSummary, error) {
	return domain.HerdRegisterSummary{}, nil
}

func (f *milkPreparationHandlerService) GetBreakdown(context.Context, domain.CountsBreakdownQuery) (domain.CountsBreakdown, error) {
	return domain.CountsBreakdown{}, nil
}

func (f *milkPreparationHandlerService) GetHerdAnalytics(context.Context, domain.HerdAnalyticsQuery) (domain.HerdAnalytics, error) {
	return domain.HerdAnalytics{}, nil
}

func (f *milkPreparationHandlerService) GetMilkPreparation(_ context.Context, query domain.MilkPreparationQuery) (domain.MilkPreparationPage, error) {
	f.query = query
	return f.page, nil
}

func (f *milkPreparationHandlerService) SubmitMilkPreparation(_ context.Context, in domain.MilkPreparationSubmission) (domain.MilkPreparationSubmissionResult, error) {
	f.submission = in
	return f.submitResult, nil
}

func (f *milkPreparationHandlerService) ListMilkFeedingTasks(context.Context, domain.MilkFeedingQuery) (domain.MilkFeedingPage, error) {
	return domain.MilkFeedingPage{}, nil
}

func (f *milkPreparationHandlerService) SubmitMilkFeeding(context.Context, domain.MilkFeedingSubmission) (domain.MilkFeedingSubmissionResult, error) {
	return domain.MilkFeedingSubmissionResult{}, f.feedingSubmitErr
}

func (f *milkPreparationHandlerService) ListAlerts(context.Context, string, string, bool, []string, string, int) (domain.AlertPage, error) {
	return domain.AlertPage{}, nil
}

func TestSubmitMilkFeedingRejectsBeforeSessionUnlock(t *testing.T) {
	service := &milkPreparationHandlerService{feedingSubmitErr: ports.ErrMilkFeedingNotYetAvailable}
	handler := NewHandler(service, slog.Default())
	body := `{"park_id":"20000000-0000-4000-8000-000000000001","feeding_date":"2026-07-30","session_no":4,"answers":{},"proofs":{}}`
	req := httptest.NewRequest(http.MethodPost, "/app/counts/milk-feeding/tasks/task-1/submit", strings.NewReader(body))
	req.SetPathValue("task_id", "task-1")
	ctx := httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithActorID(ctx, "40000000-0000-4000-8000-000000000001")
	req = req.WithContext(ctx)
	req.Header.Set("Idempotency-Key", "feeding-early-1")
	recorder := httptest.NewRecorder()

	handler.SubmitMilkFeeding(recorder, req)

	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "task_not_yet_available") {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestSubmitMilkPreparationCarriesFiveIndependentStepProofs(t *testing.T) {
	service := &milkPreparationHandlerService{submitResult: domain.MilkPreparationSubmissionResult{
		CompletionID: "30000000-0000-4000-8000-000000000001", Status: domain.MilkPreparationVerificationPending, AttemptNo: 1, RowVersion: 1,
	}}
	handler := NewHandler(service, slog.Default())
	body := `{"park_id":"20000000-0000-4000-8000-000000000001","preparation_date":"2026-07-29","goat_milk_used":true,"answers":{"morning_milk_collected_litres":4,"evening_milk_collected_litres":3,"goat_milk_quantity_litres":2,"boiling_temperature_c":100,"cooled_temperature_c":38,"uht_milk_quantity_litres":8,"citric_acid_grams":44},"proofs":{"goat_milk_quantity_proof_ref":"p1","boiling_temperature_proof_ref":"p2","cooled_temperature_proof_ref":"p3","uht_milk_quantity_proof_ref":"p4","citric_acid_mixing_proof_ref":"p5"}}`
	req := httptest.NewRequest(http.MethodPost, "/app/counts/milk-preparation/submit", strings.NewReader(body))
	ctx := httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithActorID(ctx, "40000000-0000-4000-8000-000000000001")
	req = req.WithContext(ctx)
	req.Header.Set("Idempotency-Key", "milk-attempt-1")
	recorder := httptest.NewRecorder()
	handler.SubmitMilkPreparation(recorder, req)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if refs := service.submission.Proofs.OrderedRefs(true); len(refs) != 5 || refs[0] != "p1" || refs[4] != "p5" {
		t.Fatalf("proof refs=%v", refs)
	}
	if service.submission.ParkID != "20000000-0000-4000-8000-000000000001" {
		t.Fatalf("farm=%q", service.submission.ParkID)
	}
}

func TestGetMilkPreparationPassesTenantParkAndPaging(t *testing.T) {
	service := &milkPreparationHandlerService{page: domain.MilkPreparationPage{
		Items:   []domain.MilkPreparationRow{},
		Summary: domain.MilkPreparationSummary{Scope: "filtered"},
		Limit:   25,
		Offset:  50,
	}}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/milk-preparation?park_id=00000000-0000-4000-8000-000000000001&limit=25&offset=50", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetMilkPreparation(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if service.query.TenantID != "10000000-0000-4000-8000-000000000001" || service.query.ParkID == nil || *service.query.ParkID != "00000000-0000-4000-8000-000000000001" {
		t.Fatalf("scope query=%+v", service.query)
	}
	if service.query.Limit != 25 || service.query.Offset != 50 || service.query.AsOf.IsZero() {
		t.Fatalf("paging/as-of query=%+v", service.query)
	}
	var body domain.MilkPreparationPage
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Summary.Scope != "filtered" {
		t.Fatalf("summary scope=%q", body.Summary.Scope)
	}
}

func TestGetMilkPreparationRejectsInvalidPaging(t *testing.T) {
	service := &milkPreparationHandlerService{}
	handler := NewHandler(service, slog.Default())
	req := httptest.NewRequest(http.MethodGet, "/counts/milk-preparation?limit=500", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	recorder := httptest.NewRecorder()

	handler.GetMilkPreparation(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400 body=%s", recorder.Code, recorder.Body.String())
	}
}
