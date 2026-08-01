package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// projection-review: GET /vaccination/command must resolve park scope from the caller's grants,
// exactly like ListVaccinationExecution/Schedule/Gaps/Coverage/ShedSummary do via
// authorizedParkID -> ResolveAuthorizedParkScope. Reading park_id straight off the query string
// let a park-bound park head see the OTHER park's command board by omitting it.
func TestVaccinationCommandBoardDefaultsToActorParkAndRejectsOtherPark(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const ownPark = "30000000-0000-4000-8000-000000000001"
	const otherPark = "30000000-0000-4000-8000-000000000099"

	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	ctx := httpmiddleware.WithTenantID(t.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{
		{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: ownPark},
	})

	req := httptest.NewRequest(http.MethodGet, "/vaccination/command", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if reader.lastCommandBoard.ParkID == nil || *reader.lastCommandBoard.ParkID != ownPark {
		t.Fatalf("omitted park_id resolved to %v, want %s (tenant-wide board leaks the other park)", reader.lastCommandBoard.ParkID, ownPark)
	}

	req = httptest.NewRequest(http.MethodGet, "/vaccination/command?park_id="+otherPark, nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("explicit other-park status = %d want 403 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "park_scope_forbidden") {
		t.Fatalf("body=%s, want park_scope_forbidden", rec.Body.String())
	}
}
