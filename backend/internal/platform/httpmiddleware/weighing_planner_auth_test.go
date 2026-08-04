package httpmiddleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestWeighingPlannerRoutesAreCEOOnly drives the REAL production caller: the HTTP auth
// middleware that returns 403 permission_denied.
//
// Planning a weighing task is CEO-only (maintainer decision 2026-08-01). The Growth Director
// monitors weighing across both parks, oversees the operators and may execute -- but he does not
// raise the task. All five planner routes belong to ceo_internal, including the two GETs: they
// feed the create wizard's park picker and its per-park shed page, so they are planning surfaces
// despite being reads. Leaving them on an either/or gate let the Director open a wizard whose
// submit he would then be refused.
func TestWeighingPlannerRoutesAreCEOOnly(t *testing.T) {
	plannerRoutes := []struct{ method, path string }{
		{http.MethodPost, "/weighing/campaigns"},
		{http.MethodPut, "/weighing/campaigns/80000000-0000-4000-8000-000000000001"},
		{http.MethodPost, "/weighing/campaigns/80000000-0000-4000-8000-000000000001/publish"},
		{http.MethodGet, "/app/weighing/planner/catalog"},
		{http.MethodGet, "/app/weighing/planner/parks/86000000-0000-4000-8000-000000000701/buckets"},
	}

	handlerFor := func(role string) http.Handler {
		mw := testBearerMiddleware(t, fakeGrantSource{roles: map[string][]string{
			authTestUser + "|" + authTestTenant: {role},
		}})
		return RequestContext(slog.New(slog.NewTextHandler(io.Discard, nil)))(mw.Wrap(
			http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})))
	}

	call := func(h http.Handler, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+testToken(t, authTestUser, authTestTenant, nil))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	ceo := handlerFor(permissions.RoleCEOInternal)
	director := handlerFor(permissions.RoleGrowthDirector)

	for _, rt := range plannerRoutes {
		t.Run("ceo "+rt.method+" "+rt.path, func(t *testing.T) {
			if rec := call(ceo, rt.method, rt.path); rec.Code != http.StatusNoContent {
				t.Fatalf("ceo_internal must reach the planner: status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
		t.Run("growth director "+rt.method+" "+rt.path, func(t *testing.T) {
			if rec := call(director, rt.method, rt.path); rec.Code != http.StatusForbidden {
				t.Fatalf("growth_director must NOT reach the planner (CEO-only): status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
