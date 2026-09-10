package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

const (
	tenant   = "00000000-0000-4000-8000-000000000001"
	parkCBE  = "00000000-0000-4000-8000-000000003001"
	parkCPT  = "00000000-0000-4000-8000-000000003002"
	actorOp  = "00000000-0000-4000-8000-000000000301"
	actorCEO = "00000000-0000-4000-8000-000000000999"
)

// fakeService records the query the handler built, which is the whole point of these
// tests: every scope rule lives in the handler, and the service must receive its result.
type fakeService struct{ last domain.Query }

func (f *fakeService) List(_ context.Context, q domain.Query) (domain.Page, error) {
	f.last = q
	return domain.Page{Rows: []domain.Row{}}, nil
}
func (f *fakeService) Summary(_ context.Context, q domain.Query) (domain.Summary, error) {
	f.last = q
	return domain.NewSummary(q.Modules), nil
}
// RegisteredModules mirrors production: procurement and toxin have no source yet.
func (f *fakeService) RegisteredModules() []domain.Module {
	return []domain.Module{domain.ModuleFeed, domain.ModuleHealth, domain.ModuleVaccination, domain.ModuleWeighing, domain.ModuleCounts, domain.ModuleMilk, domain.ModulePCCare, domain.ModuleVerification}
}

func get(t *testing.T, h *Handler, path, actor string, grants []permissions.ActiveGrant) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithActorID(ctx, actor)
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	Register(mux, h)
	mux.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec, body
}

func operatorGrant(park string) []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: park}}
}
func parkHeadGrant(park string) []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: park}}
}
func tenantGrant(role string) []permissions.ActiveGrant {
	return []permissions.ActiveGrant{{Role: role, ScopeType: "tenant", ScopeID: tenant}}
}

// TestOperatorLensIsOwnRowsOnlyInOwnPark: work_board.read without oversee clamps the read
// to the caller's own rows, and the park to the caller's park even when none is named.
func TestOperatorLensIsOwnRowsOnlyInOwnPark(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/rows?owner="+actorCEO, actorOp, operatorGrant(parkCBE))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %v", rec.Code, body)
	}
	if svc.last.OwnerUserID != actorOp {
		t.Fatalf("operator must be clamped to their own rows, got owner %q", svc.last.OwnerUserID)
	}
	if svc.last.ParkID != parkCBE {
		t.Fatalf("operator's park must be resolved from the grant, got %q", svc.last.ParkID)
	}
	if body["own_rows_only"] != true {
		t.Fatalf("payload must say the read was clamped: %v", body["own_rows_only"])
	}
	// An operator holds vaccination/weighing/feed/health/counts/pc_care execute-type
	// permissions and sees those modules; never verification or toxin.
	mods := svc.last.Modules
	for _, m := range mods {
		if m == domain.ModuleVerification || m == domain.ModuleToxin {
			t.Fatalf("operator must not see %s", m)
		}
	}
	if len(mods) == 0 {
		t.Fatal("operator should see the modules they execute")
	}
}

// TestParkHeadSeesEveryoneInTheirParkAndNothingOutside.
func TestParkHeadSeesEveryoneInTheirParkAndNothingOutside(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, _ := get(t, h, "/work-board/summary?park="+parkCBE, actorOp, parkHeadGrant(parkCBE))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if svc.last.OwnerUserID != "" {
		t.Fatalf("park head with oversee must see everyone, got owner %q", svc.last.OwnerUserID)
	}
	rec, body := get(t, h, "/work-board/summary?park="+parkCPT, actorOp, parkHeadGrant(parkCBE))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("the other park must be refused, got %d %v", rec.Code, body)
	}
}

// TestTenantWideCallerMustNameAPark: the board is bounded to one park per request.
func TestTenantWideCallerMustNameAPark(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/rows", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusBadRequest || body["error"] != "park_required" {
		t.Fatalf("expected 400 park_required, got %d %v", rec.Code, body)
	}
	rec, _ = get(t, h, "/work-board/rows?park="+parkCPT+"&owner=me", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK || svc.last.ParkID != parkCPT || svc.last.OwnerUserID != actorCEO {
		t.Fatalf("CEO may pick any park and owner=me resolves to the actor: %d %+v", rec.Code, svc.last)
	}
	if len(svc.last.Modules) != len(svc.RegisteredModules()) {
		t.Fatalf("CEO sees every REGISTERED module, got %v", svc.last.Modules)
	}
}

// TestDirectorSeesOnlyTheirModule: a Feed Director's module set is feed, and asking for
// health resolves to an empty board rather than a 403.
func TestDirectorSeesOnlyTheirModule(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/rows?park="+parkCBE, actorCEO, tenantGrant(permissions.RoleFeedDirector))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if len(svc.last.Modules) != 1 || svc.last.Modules[0] != domain.ModuleFeed {
		t.Fatalf("feed director sees feed only, got %v", svc.last.Modules)
	}
	rec, _ = get(t, h, "/work-board/rows?park="+parkCBE+"&module=health", actorCEO, tenantGrant(permissions.RoleFeedDirector))
	if rec.Code != http.StatusOK {
		t.Fatalf("an invisible module is an empty board, not a refusal: %d", rec.Code)
	}
	if len(svc.last.Modules) != 1 || svc.last.Modules[0] != domain.Module("none") {
		t.Fatalf("expected the empty sentinel, got %v", svc.last.Modules)
	}
}

// TestBadInputsAreRefusedWithStableCodes.
func TestBadInputsAreRefusedWithStableCodes(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)
	cases := map[string]string{
		"/work-board/rows?park=" + parkCBE + "&cursor=garbage":         "invalid_cursor",
		"/work-board/rows?park=" + parkCBE + "&business_date=10-09-26": "invalid_business_date",
		"/work-board/rows?park=" + parkCBE + "&module=laundry":         "invalid_module",
		"/work-board/rows?park=" + parkCBE + "&state=asleep":           "invalid_state",
		"/work-board/rows?park=" + parkCBE + "&limit=0":                "invalid_limit",
		"/work-board/rows?park=not-a-uuid":                             "invalid_park_id",
	}
	for path, code := range cases {
		rec, body := get(t, h, path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
		if rec.Code != http.StatusBadRequest || body["error"] != code {
			t.Errorf("%s: want 400 %s, got %d %v", path, code, rec.Code, body)
		}
	}
}
