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

type packingScopeService struct {
	Service
	last domain.PackingQuery
}

func (s *packingScopeService) PackingWorklist(_ context.Context, q domain.PackingQuery) (domain.PackingPage, error) {
	s.last = q
	return domain.PackingPage{}, nil
}

// TestFeedReadsPassTheCallersParkSetForFilterVocabulary closes the wiring gap between the clamp and
// the response. The clamp above only decides WHICH park is served; the authorized SET is what stops
// the response from advertising parks this caller cannot open in its farm dropdown. A field declared
// on the query but never populated by the handler reads as done and ships an unnarrowed dropdown, so
// this asserts the value actually crosses the boundary on BOTH feed reads.
func TestFeedReadsPassTheCallersParkSetForFilterVocabulary(t *testing.T) {
	const (
		tenantID = "10000000-0000-4000-8000-000000000001"
		parkA    = "20000000-0000-4000-8000-00000000000a"
	)
	parkAGrant := permissions.ActiveGrant{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: parkA}

	t.Run("preview carries the park-scoped caller's own set", func(t *testing.T) {
		service := &previewScopeService{}
		if rec := getPreviewWithGrants(t, service, "", []permissions.ActiveGrant{parkAGrant}); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		got := service.last.AuthorizedParkIDs
		if len(got) != 1 || got[0] != parkA {
			t.Fatalf("authorized parks = %v, want only the caller's park %q", got, parkA)
		}
	})

	t.Run("packing carries the park-scoped caller's own set", func(t *testing.T) {
		service := &packingScopeService{}
		req := httptest.NewRequest(http.MethodGet, "/feed-packing/worklist?target_date=2026-08-04", nil)
		ctx := httpmiddleware.WithTenantID(req.Context(), tenantID)
		ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{parkAGrant})
		rec := httptest.NewRecorder()
		NewHandler(service, nil).GetPackingWorklist(rec, req.WithContext(ctx))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		got := service.last.AuthorizedParkIDs
		if len(got) != 1 || got[0] != parkA {
			t.Fatalf("authorized parks = %v, want only the caller's park %q", got, parkA)
		}
	})

	// A tenant-wide principal must reach the service with NO restriction, or the narrowing would
	// blank leadership's own farm picker instead of widening it.
	t.Run("tenant-wide caller carries no restriction", func(t *testing.T) {
		service := &previewScopeService{}
		grants := []permissions.ActiveGrant{{Role: permissions.RoleFeedDirector, ScopeType: "tenant", ScopeID: tenantID}}
		if rec := getPreviewWithGrants(t, service, "", grants); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if len(service.last.AuthorizedParkIDs) != 0 {
			t.Fatalf("authorized parks = %v, want empty (unrestricted) for a tenant-wide principal", service.last.AuthorizedParkIDs)
		}
	})
}
