package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// BUG-019: the Vaccination Operators screen inferred its park from the FIRST row
// of an unscoped roster read. Park scope has to be backend-owned, so
// GET /vaccination/operator-assignment/config now (a) resolves the caller's
// authorized park when park_id is omitted, (b) echoes the resolved parkId the
// client must scope its roster read to, and (c) refuses (409) rather than
// silently picking a park when the caller's scope spans more than one.
func TestOperatorAssignmentConfigResolvesParkScopeFromCaller(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const parkA = "20000000-0000-4000-8000-00000000000a"

	reader := &fakeReader{operatorCfg: &vaccexecapp.OperatorAssignmentConfigView{
		Config: vaccexecd.OperatorAssignmentConfig{
			ParkID:                parkA,
			ActiveOperatorsPerDay: 1,
			DefaultOperatorID:     "30000000-0000-4000-8000-000000000077",
			RowVersion:            3,
		},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	// Park-scoped actor, NO park_id in the query: the backend owns the scope.
	req := httptest.NewRequest(http.MethodGet, "/vaccination/operator-assignment/config", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: parkA}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with a backend-resolved park scope, got %d: %s", rec.Code, rec.Body.String())
	}
	if reader.lastOperatorCfgPark != parkA {
		t.Fatalf("config read was not scoped to the actor's park: got %q want %q", reader.lastOperatorCfgPark, parkA)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["parkId"] != parkA {
		t.Fatalf("response must echo the resolved parkId so the client can scope its roster read; got %v", body["parkId"])
	}
}

// A tenant-wide actor in a multi-park tenant has no single park. Blending parks
// (what the screen did) is the bug; the backend must refuse instead.
func TestOperatorAssignmentConfigRefusesAmbiguousParkScope(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	reader := &fakeReader{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/operator-assignment/config", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 park_scope_ambiguous for a tenant-wide actor, got %d: %s", rec.Code, rec.Body.String())
	}
	if reader.lastOperatorCfgPark != "" {
		t.Fatalf("no config read may happen without a resolved park; got %q", reader.lastOperatorCfgPark)
	}
}

// BUG-019 (completion): refusing a tenant-wide actor with a bare 409 is a dead end
// -- the maintainer's own ceo_internal TENANT-scoped account could not open the
// screen at all. The refusal stays, but it must carry the BACKEND-OWNED park
// options (Postgres-backed ids + labels) the caller may choose from, so the
// client renders a selector instead of throwing. The frontend never assembles
// this list itself.
func TestOperatorAssignmentConfigAmbiguousScopeReturnsAuthorizedParkOptions(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const parkA = "20000000-0000-4000-8000-00000000000a"
	const parkB = "20000000-0000-4000-8000-00000000000b"

	reader := &fakeReader{parks: []vaccexecd.ParkOption{
		{ParkID: parkA, Code: "CPT", Name: "Channapatna"},
		{ParkID: parkB, Code: "CBE", Name: "Coimbatore"},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/operator-assignment/config", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 park_scope_ambiguous, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Code           string                 `json:"code"`
		AvailableParks []vaccexecd.ParkOption `json:"availableParks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "park_scope_ambiguous" {
		t.Fatalf("code = %q want park_scope_ambiguous", body.Code)
	}
	if len(body.AvailableParks) != 2 {
		t.Fatalf("the 409 must carry the caller's authorized parks so the client can render a backend-owned selector; got %+v", body.AvailableParks)
	}
	if body.AvailableParks[0].ParkID != parkA || body.AvailableParks[0].Name != "Channapatna" {
		t.Fatalf("park options must carry canonical Postgres ids+labels; got %+v", body.AvailableParks[0])
	}
	// (d) no park may be silently auto-picked for a multi-park actor.
	if reader.lastOperatorCfgPark != "" {
		t.Fatalf("no config read may happen without an explicitly chosen park; got %q", reader.lastOperatorCfgPark)
	}
}

// (b) a tenant-wide actor in a SINGLE-park tenant must still be zero-click: the
// scope is not genuinely ambiguous, so the backend resolves it and returns 200.
func TestOperatorAssignmentConfigSingleParkTenantNeedsNoSelection(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const parkA = "20000000-0000-4000-8000-00000000000a"

	reader := &fakeReader{
		parks: []vaccexecd.ParkOption{{ParkID: parkA, Code: "CPT", Name: "Channapatna"}},
		operatorCfg: &vaccexecapp.OperatorAssignmentConfigView{Config: vaccexecd.OperatorAssignmentConfig{
			ParkID: parkA, ActiveOperatorsPerDay: 1, DefaultOperatorID: "30000000-0000-4000-8000-000000000077", RowVersion: 3,
		}},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/operator-assignment/config", nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("a single-park tenant is not ambiguous; expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["parkId"] != parkA {
		t.Fatalf("parkId = %v want %v", body["parkId"], parkA)
	}
}

// (c) after the actor chooses a park, that park -- not a blended scope -- is what
// every downstream read is scoped to.
func TestOperatorAssignmentConfigHonoursChosenParkForTenantWideActor(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const parkA = "20000000-0000-4000-8000-00000000000a"
	const parkB = "20000000-0000-4000-8000-00000000000b"

	reader := &fakeReader{
		parks: []vaccexecd.ParkOption{
			{ParkID: parkA, Code: "CPT", Name: "Channapatna"},
			{ParkID: parkB, Code: "CBE", Name: "Coimbatore"},
		},
		operatorCfg: &vaccexecapp.OperatorAssignmentConfigView{Config: vaccexecd.OperatorAssignmentConfig{
			ParkID: parkB, ActiveOperatorsPerDay: 2, DefaultOperatorID: "30000000-0000-4000-8000-000000000077", RowVersion: 1,
		}},
	}
	mux := http.NewServeMux()
	Register(mux, NewHandler(reader, &fakeWriter{}))

	req := httptest.NewRequest(http.MethodGet, "/vaccination/operator-assignment/config?park_id="+parkB, nil)
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithAuthGrants(ctx, []permissions.ActiveGrant{{Role: permissions.RoleCEOInternal, ScopeType: "tenant", ScopeID: tenant}})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req.WithContext(ctx))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for an explicitly chosen park, got %d: %s", rec.Code, rec.Body.String())
	}
	if reader.lastOperatorCfgPark != parkB {
		t.Fatalf("config read scoped to %q, want the chosen park %q", reader.lastOperatorCfgPark, parkB)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["parkId"] != parkB {
		t.Fatalf("the response must echo the chosen park so downstream reads scope to it; got %v", body["parkId"])
	}
}
