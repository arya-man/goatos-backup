package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// recordingApprovalRepo is a ports.Repository whose only served method is the list; it records
// the query the REAL ApprovalService lowered the request onto.
type recordingApprovalRepo struct {
	ports.Repository
	last  *domain.ApprovalRequestQuery
	count int
}

func (r *recordingApprovalRepo) ListApprovalRequests(_ context.Context, q domain.ApprovalRequestQuery) (domain.ApprovalRequestPage, error) {
	r.last = &q
	return domain.ApprovalRequestPage{Items: []domain.ApprovalRequestSummary{}}, nil
}

func (r *recordingApprovalRepo) CountPendingApprovalRequests(_ context.Context, q domain.ApprovalRequestQuery) (int, error) {
	r.last = &q
	return r.count, nil
}

// realApprovalWorkflow serves the list through the real ApprovalService (validation, narrowing,
// cursor binding) over the recording repo; the submit/decide half is the shared fake.
type realApprovalWorkflow struct {
	*fakeApprovalWorkflow
	svc *countsapp.ApprovalService
}

func (w *realApprovalWorkflow) ListPending(ctx context.Context, tenantID, status string, types, parks []string, pageSize int, cursor string) (domain.ApprovalRequestPage, error) {
	return w.svc.ListPending(ctx, tenantID, status, types, parks, pageSize, cursor)
}

func (w *realApprovalWorkflow) ListFiltered(ctx context.Context, tenantID, status string, types, parks []string, filter domain.ApprovalListFilter, pageSize int, cursor string) (domain.ApprovalRequestPage, error) {
	return w.svc.ListFiltered(ctx, tenantID, status, types, parks, filter, pageSize, cursor)
}

// GET /app/counts/approvals and /admin-web/counts/approvals accept request_type and park_id as
// SERVER-SIDE filters (2026-09-25; the web filtered one 20-row page client-side). Unknown values
// are 400, never widened; a type the caller may not decide narrows to nothing.
func TestApprovalListFiltersReachTheQueryAndRefuseUnknownValues(t *testing.T) {
	const parkID = "00000000-0000-4000-8000-000000003001"
	for _, route := range []string{appApprovalsRoute, adminWebApprovalsRoute} {
		repo := &recordingApprovalRepo{}
		approvals := &realApprovalWorkflow{fakeApprovalWorkflow: newFakeApprovalWorkflow(), svc: countsapp.NewApprovalService(repo, nil, nil)}
		mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, newFakeGoatValidator())
		RegisterAdminWebApprovals(mux, handlersByMux[mux])
		get := func(query string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, route+query, nil)
			ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
			ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCountsApprover, ScopeType: "tenant"}})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req.WithContext(ctx))
			return rec
		}

		repo.last = nil
		if rec := get("?request_type=death&park_id=" + parkID); rec.Code != http.StatusOK {
			t.Fatalf("%s filtered list: status=%d body=%s", route, rec.Code, rec.Body.String())
		}
		if repo.last == nil || len(repo.last.RequestTypes) != 1 || repo.last.RequestTypes[0] != domain.ApprovalRequestTypeDeath ||
			repo.last.FilterParkID != parkID {
			t.Fatalf("%s: the filter did not reach the query: %+v", route, repo.last)
		}
		for _, bad := range []struct{ query, code string }{
			{"?request_type=weighing", "invalid_request_type"},
			{"?park_id=CBE", "invalid_park_id"},
		} {
			rec := get(bad.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s%s: status=%d, want 400 (%s)", route, bad.query, rec.Code, rec.Body.String())
			}
			var body struct {
				Code  string `json:"code"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			decodeBody(t, rec, &body)
			if body.Code != bad.code && body.Error.Code != bad.code {
				t.Fatalf("%s%s: body=%s, want code %s", route, bad.query, rec.Body.String(), bad.code)
			}
		}
	}
}

// The badge is the pending count the caller may decide, read from the SAME grants the list reads;
// a principal with no approval authority has no badge and the count is never asked.
func TestApprovalsBadgeUsesTheCallersDecidableTypesAndScope(t *testing.T) {
	repo := &recordingApprovalRepo{count: 3}
	badges := NewApprovalsBadges(nil, countsapp.NewApprovalService(repo, nil, nil))
	approver := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{
		{Role: permissions.RoleCountsApprover, ScopeType: "park", ScopeID: "00000000-0000-4000-8000-000000003001"},
	})
	counts, err := badges.ModuleBadgeCounts(approver, testTenantID, testActorID, []string{ApprovalsModuleKey})
	if err != nil || counts[ApprovalsModuleKey] != 3 {
		t.Fatalf("approver badge = %v err %v, want 3", counts, err)
	}
	if repo.last == nil || len(repo.last.CallerParkIDs) != 1 || len(repo.last.RequestTypes) == 0 {
		t.Fatalf("the count must use the caller's decidable types and park scope: %+v", repo.last)
	}
	items, err := badges.NavItemBadgeCounts(approver, testTenantID, testActorID, []string{ApprovalsNavHref})
	if err != nil || items[ApprovalsNavHref] != 3 {
		t.Fatalf("approvals tab badge = %v err %v, want 3", items, err)
	}
	repo.last = nil
	operator := httpmiddleware.WithAuthGrants(context.Background(), []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: "x"}})
	counts, err = badges.ModuleBadgeCounts(operator, testTenantID, testActorID, []string{ApprovalsModuleKey})
	if err != nil || counts[ApprovalsModuleKey] != 0 || repo.last != nil {
		t.Fatalf("non-approver badge = %v err %v (count asked: %v), want 0 and no count", counts, err, repo.last != nil)
	}
}

// The Approvals calendar filter (maintainer request 2026-09-25): raised_from / raised_to are whole
// INDIA business days, applied in SQL, so a request raised at 02:00 IST on the 16th is on the 16th
// (not the 15th, as a UTC day would file it). A bad date or an inverted range is refused, never
// read as "no filter".
func TestApprovalListRaisedDateRangeIsIndiaDaysAndRefusesBadDates(t *testing.T) {
	for _, route := range []string{appApprovalsRoute, adminWebApprovalsRoute} {
		repo := &recordingApprovalRepo{}
		approvals := &realApprovalWorkflow{fakeApprovalWorkflow: newFakeApprovalWorkflow(), svc: countsapp.NewApprovalService(repo, nil, nil)}
		mux := newTestServer(t, countsapp.NewService(newFakeShiftingRepo()), approvals, newFakeGoatValidator())
		RegisterAdminWebApprovals(mux, handlersByMux[mux])
		get := func(query string) *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, route+query, nil)
			ctx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(req.Context(), testTenantID), testActorID)
			ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCountsApprover, ScopeType: "tenant"}})
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req.WithContext(ctx))
			return rec
		}
		repo.last = nil
		if rec := get("?raised_from=2026-09-16&raised_to=2026-09-17"); rec.Code != http.StatusOK {
			t.Fatalf("%s date range: status=%d body=%s", route, rec.Code, rec.Body.String())
		}
		ist := time.FixedZone("IST", 5*3600+1800)
		wantFrom := time.Date(2026, 9, 16, 0, 0, 0, 0, ist)
		wantBefore := time.Date(2026, 9, 18, 0, 0, 0, 0, ist)
		if repo.last == nil || repo.last.RaisedFrom == nil || repo.last.RaisedBefore == nil ||
			!repo.last.RaisedFrom.Equal(wantFrom) || !repo.last.RaisedBefore.Equal(wantBefore) {
			t.Fatalf("%s: the date range did not reach the query as India days: %+v", route, repo.last)
		}
		if repo.last.FilterKey == "" {
			t.Fatalf("%s: a dated page must bind its cursor to the dates", route)
		}
		for _, bad := range []string{"?raised_from=16-09-2026", "?raised_to=2026-13-01", "?raised_from=2026-09-18&raised_to=2026-09-16"} {
			if rec := get(bad); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_date_range") {
				t.Fatalf("%s%s: status=%d body=%s, want 400 invalid_date_range", route, bad, rec.Code, rec.Body.String())
			}
		}
	}
}
