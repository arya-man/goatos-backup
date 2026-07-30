package http

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeService struct{ listed domain.ListFilter }

func (*fakeService) OpenCase(context.Context, domain.OpenCaseInput) (domain.OpenCaseResult, error) {
	return domain.OpenCaseResult{CaseID: "case"}, nil
}
func (f *fakeService) ListWorkItems(_ context.Context, in domain.ListFilter) (domain.WorkItemPage, error) {
	f.listed = in
	return domain.WorkItemPage{Items: []domain.WorkItem{}}, nil
}
func (*fakeService) GetWorkItem(context.Context, string, string) (domain.WorkItemDetail, error) {
	return domain.WorkItemDetail{}, nil
}
func (*fakeService) CompleteWorkItem(context.Context, domain.CompleteInput) (domain.CompleteResult, error) {
	return domain.CompleteResult{}, nil
}
func TestListForwardsAgeBandAndDate(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	req := httptest.NewRequest(http.MethodGet, "/app/health/work-items?date=2026-07-30&age_band=adult", nil)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	w := httptest.NewRecorder()
	h.ListWorkItems(w, req)
	if w.Code != http.StatusOK || svc.listed.AgeBand != "adult" || svc.listed.Date != "2026-07-30" {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestOpenRequiresIdempotencyKey(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)
	req := httptest.NewRequest(http.MethodPost, "/app/health/cases", strings.NewReader(`{"goat_id":"30000000-0000-4000-8000-000000000001","disease_key":"fever","age_band":"adult","start_date":"2026-07-30"}`))
	w := httptest.NewRecorder()
	h.OpenCase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
}
