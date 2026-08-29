package http

import (
	"context"
	"encoding/json"

	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
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
func (*fakeService) CloseCase(context.Context, domain.CloseCaseInput) (domain.CloseCaseResult, error) {
	return domain.CloseCaseResult{}, nil
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

func TestCloseCaseRequiresIdempotencyKeyAndStrictBody(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/app/health/cases/40000000-0000-4000-8000-000000000001/close", strings.NewReader(`{"outcome":"recovered"}`))
	req.SetPathValue("health_case_id", "40000000-0000-4000-8000-000000000001")
	w := httptest.NewRecorder()
	h.CloseCase(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "idempotency_key_required") {
		t.Fatalf("missing key: status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/app/health/cases/40000000-0000-4000-8000-000000000001/close", strings.NewReader(`{"outcome":"recovered","surprise":true}`))
	req.SetPathValue("health_case_id", "40000000-0000-4000-8000-000000000001")
	req.Header.Set("Idempotency-Key", "k1")
	w = httptest.NewRecorder()
	h.CloseCase(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloseCaseMapsCaseNotOpenToConflict(t *testing.T) {
	h := NewHandler(&erroringCloseService{err: ports.ErrCaseNotOpen}, nil)
	req := httptest.NewRequest(http.MethodPost, "/app/health/cases/40000000-0000-4000-8000-000000000001/close", strings.NewReader(`{"outcome":"recovered"}`))
	req.SetPathValue("health_case_id", "40000000-0000-4000-8000-000000000001")
	req.Header.Set("Idempotency-Key", "k1")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	w := httptest.NewRecorder()
	h.CloseCase(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "case_not_open") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

type erroringCloseService struct {
	fakeService
	err error
}

func (s *erroringCloseService) CloseCase(context.Context, domain.CloseCaseInput) (domain.CloseCaseResult, error) {
	return domain.CloseCaseResult{}, s.err
}

func TestCompleteRequiresIdempotencyKeyAndForwardsProofRef(t *testing.T) {
	svc := &recordingCompleteService{}
	h := NewHandler(svc, nil)

	req := httptest.NewRequest(http.MethodPost, "/app/health/work-items/50000000-0000-4000-8000-000000000001/complete", strings.NewReader(`{"proof_ref":"p-1"}`))
	req.SetPathValue("health_session_id", "50000000-0000-4000-8000-000000000001")
	w := httptest.NewRecorder()
	h.CompleteWorkItem(w, req)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "idempotency_key_required") {
		t.Fatalf("missing key: status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/app/health/work-items/50000000-0000-4000-8000-000000000001/complete", strings.NewReader(`{"proof_ref":"p-1"}`))
	req.SetPathValue("health_session_id", "50000000-0000-4000-8000-000000000001")
	req.Header.Set("Idempotency-Key", "k1")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	w = httptest.NewRecorder()
	h.CompleteWorkItem(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if svc.completed.ProofRef != "p-1" || svc.completed.SessionID != "50000000-0000-4000-8000-000000000001" || svc.completed.IdempotencyKey != "k1" {
		t.Fatalf("forwarded=%+v", svc.completed)
	}
	// A proof-less completion is still accepted at the HTTP layer (installed-APK compatibility);
	// the review-path consequences live in the app service.
	req = httptest.NewRequest(http.MethodPost, "/app/health/work-items/50000000-0000-4000-8000-000000000001/complete", strings.NewReader(`{}`))
	req.SetPathValue("health_session_id", "50000000-0000-4000-8000-000000000001")
	req.Header.Set("Idempotency-Key", "k2")
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001"))
	w = httptest.NewRecorder()
	h.CompleteWorkItem(w, req)
	if w.Code != http.StatusOK || svc.completed.ProofRef != "" {
		t.Fatalf("proof-less: status=%d forwarded=%+v", w.Code, svc.completed)
	}
}

func TestGetWorkItemDerivesCapabilitiesFromTheCallersOwnGrants(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)
	cases := []struct {
		name                    string
		roles                   []string
		wantComplete, wantClose bool
	}{
		{"operator executes but does not close", []string{"operator"}, true, false},
		{"health director closes but does not execute", []string{"health_director"}, false, true},
		{"verifier gets neither", []string{"verifier"}, false, false},
		{"ceo gets both", []string{"ceo_internal"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grants := make([]permissions.ActiveGrant, 0, len(tc.roles))
			for _, role := range tc.roles {
				grants = append(grants, permissions.ActiveGrant{Role: role, ScopeType: "tenant", ScopeID: "10000000-0000-4000-8000-000000000001"})
			}
			req := httptest.NewRequest(http.MethodGet, "/app/health/work-items/50000000-0000-4000-8000-000000000001", nil)
			req.SetPathValue("health_session_id", "50000000-0000-4000-8000-000000000001")
			ctx := httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001")
			ctx = httpmiddleware.WithAuthGrants(ctx, grants)
			w := httptest.NewRecorder()
			h.GetWorkItem(w, req.WithContext(ctx))
			if w.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var got struct {
				CanComplete  bool `json:"can_complete"`
				CanCloseCase bool `json:"can_close_case"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.CanComplete != tc.wantComplete || got.CanCloseCase != tc.wantClose {
				t.Fatalf("capabilities=%+v want complete=%v close=%v", got, tc.wantComplete, tc.wantClose)
			}
		})
	}
}

type recordingCompleteService struct {
	fakeService
	completed domain.CompleteInput
}

func (s *recordingCompleteService) CompleteWorkItem(_ context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	s.completed = in
	return domain.CompleteResult{SessionID: in.SessionID, Status: "completed"}, nil
}
