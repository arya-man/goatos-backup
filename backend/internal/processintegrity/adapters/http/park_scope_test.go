package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// projection-review: control-tower / action-center park scope is BACKEND-owned. The query string
// may only NARROW within the actor's grant scope; it may never widen it. Without this, a
// park-bound park head omitting park_id read the whole tenant (both parks) -- and because this
// same feed backs mobile Alerts, their alert COUNT included the other park's rows.
func TestProcessIntegrityReadsDefaultToActorParkAndReject403OnOtherPark(t *testing.T) {
	const otherPark = "70000000-0000-4000-8000-000000000099"
	paths := []string{
		"/vaccination/action-center",
		"/vaccination/action-center/counts",
		"/control-tower/vaccination",
		"/vaccination/adherence",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			reader := &fakeReader{}
			mux := http.NewServeMux()
			Register(mux, NewHandler(reader))

			ctx := httpmiddleware.WithTenantID(t.Context(), handlerTenant)
			ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
				{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: handlerPark},
			})

			req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			got := reader.lastQuery()
			if got.ParkID == nil || *got.ParkID != handlerPark {
				t.Fatalf("omitted park_id resolved to %v, want the actor's own park %s (tenant-wide read leaks the other park)", got.ParkID, handlerPark)
			}

			req = httptest.NewRequest(http.MethodGet, path+"?park_id="+otherPark, nil).WithContext(ctx)
			rec = httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("explicit other-park status = %d want 403 body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
				t.Fatalf("body=%s, want park_scope_forbidden", rec.Body.String())
			}
		})
	}
}
