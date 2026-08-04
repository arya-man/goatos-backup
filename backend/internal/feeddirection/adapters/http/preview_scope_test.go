package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

type previewScopeService struct {
	Service
	calls int
	last  domain.PreviewQuery
}

func (s *previewScopeService) Preview(_ context.Context, q domain.PreviewQuery) (domain.PreviewPage, error) {
	s.calls++
	s.last = q
	return domain.PreviewPage{}, nil
}

func getPreviewWithGrants(t *testing.T, service Service, rawQuery string, grants []permissions.ActiveGrant) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/feed-direction/preview?target_date=2026-08-04&"+rawQuery, nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), "10000000-0000-4000-8000-000000000001")
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	rec := httptest.NewRecorder()
	NewHandler(service, nil).GetPreview(rec, req.WithContext(ctx))
	return rec
}

func TestGetPreviewEnforcesFeedDirectionParkScope(t *testing.T) {
	const (
		tenantID = "10000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-00000000000a"
		parkB    = "20000000-0000-4000-8000-00000000000b"
	)
	parkAGrant := permissions.ActiveGrant{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}

	t.Run("omitted park defaults to the authorized park", func(t *testing.T) {
		service := &previewScopeService{}
		rec := getPreviewWithGrants(t, service, "", []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if service.last.ParkID != parkA {
			t.Fatalf("park_id=%q, want authorized park %q", service.last.ParkID, parkA)
		}
	})

	t.Run("foreign park is forbidden before the service read", func(t *testing.T) {
		service := &previewScopeService{}
		rec := getPreviewWithGrants(t, service, "park_id="+parkB, []permissions.ActiveGrant{parkAGrant})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
		if service.calls != 0 {
			t.Fatalf("service calls=%d, want 0 for forbidden park", service.calls)
		}
	})

	t.Run("unrelated tenant grant does not bypass feed scope", func(t *testing.T) {
		service := &previewScopeService{}
		grants := []permissions.ActiveGrant{
			parkAGrant,
			{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: tenantID},
		}
		rec := getPreviewWithGrants(t, service, "park_id="+parkB, grants)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("tenant-wide feed reader may query any park", func(t *testing.T) {
		service := &previewScopeService{}
		grants := []permissions.ActiveGrant{{Role: permissions.RoleFeedDirector, ScopeType: "tenant", ScopeID: tenantID}}
		rec := getPreviewWithGrants(t, service, "park_id="+parkB, grants)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if service.last.ParkID != parkB {
			t.Fatalf("park_id=%q, want %q", service.last.ParkID, parkB)
		}
	})
}
