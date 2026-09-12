package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ltdomain "github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	ltports "github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/workboard/app"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
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
type fakeService struct {
	last domain.Query
	// found is what FindRow answers; lastRowKey / lastAfter / lastLimit record the subtask read.
	found      bool
	lastRowKey string
	lastAfter  string
	lastLimit  int
}

func (f *fakeService) FindRow(_ context.Context, q domain.Query, rowKey string) (domain.Row, bool, error) {
	f.last = q
	f.lastRowKey = rowKey
	if !f.found {
		return domain.Row{}, false, nil
	}
	return domain.Row{RowKey: rowKey}, true, nil
}

func (f *fakeService) ListSubtasks(_ context.Context, q domain.Query, rowKey, afterKey string, limit int) (domain.SubtaskPage, error) {
	f.last = q
	f.lastRowKey, f.lastAfter, f.lastLimit = rowKey, afterKey, limit
	st := domain.Subtask{Key: "1:a", Name: "Tag 1", WorkState: domain.WorkStateDue, Steps: []domain.Step{{Name: "Scan", State: domain.StepTodo}}}.Finalize()
	return domain.SubtaskPage{Subtasks: []domain.Subtask{st}, Total: 1}, nil
}

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
	if !svc.last.NoModules || len(svc.last.Modules) != 0 {
		t.Fatalf("expected an empty board (NoModules), got %v", svc.last.Modules)
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

type fakeFlags struct {
	last ports.FlagParams
	err  error
}

func (f *fakeFlags) Flag(_ context.Context, p ports.FlagParams) (ports.FlagResult, error) {
	f.last = p
	if f.err != nil {
		return ports.FlagResult{}, f.err
	}
	return ports.FlagResult{TaskID: "t1", TaskNo: 12, AssigneeName: "Naveen R."}, nil
}

func post(t *testing.T, h *Handler, body, actor string, grants []permissions.ActiveGrant, key string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/work-board/flags", strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
	ctx = httpmiddleware.WithActorID(ctx, actor)
	ctx = httpmiddleware.WithAuthGrants(ctx, grants)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	Register(mux, h)
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

// TestFlagRaisesToTheParkHeadWithTheDirectorsDesignation.
func TestFlagRaisesToTheParkHeadWithTheDirectorsDesignation(t *testing.T) {
	flags := &fakeFlags{}
	h := NewHandler(&fakeService{}, nil).WithFlags(flags)
	body := `{"row_key":"feed|feed_transport_task|f1","park_id":"` + parkCBE + `","business_date":"2026-09-09","note":"Please check"}`
	rec, out := post(t, h, body, actorCEO, tenantGrant(permissions.RoleFeedDirector), "k1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %v", rec.Code, out)
	}
	if flags.last.Board.ParkID != parkCBE || flags.last.Board.TenantID != tenant || flags.last.ActorID != actorCEO || flags.last.IdempotencyKey != "k1" {
		t.Fatalf("params %+v", flags.last)
	}
	if flags.last.Board.BusinessDate != "2026-09-09" || flags.last.RowKey != "feed|feed_transport_task|f1" || flags.last.Note != "Please check" {
		t.Fatalf("the flag must name the row on the day it was seen: %+v", flags.last)
	}
	// The board the row is looked up on is the caller's own visibility: a feed director's
	// board holds feed and nothing else, so a weighing key can never resolve for them.
	if len(flags.last.Board.Modules) != 1 || flags.last.Board.Modules[0] != domain.ModuleFeed {
		t.Fatalf("feed director's board must be feed only, got %v", flags.last.Board.Modules)
	}
	if flags.last.ActorDesignation != permissions.RoleFeedDirector {
		t.Fatalf("designation should be the raising director's desk, got %q", flags.last.ActorDesignation)
	}
	if out["task_no"] != float64(12) || out["assignee_name"] != "Naveen R." {
		t.Fatalf("result %v", out)
	}
}

// TestFlagRefusalsCarryStableCodes.
func TestFlagRefusalsCarryStableCodes(t *testing.T) {
	good := `{"row_key":"feed|feed_transport_task|f1","park_id":"` + parkCBE + `"}`
	h := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{})
	if rec, out := post(t, h, good, actorCEO, tenantGrant(permissions.RoleCEOInternal), ""); rec.Code != http.StatusBadRequest || out["error"] != "missing_idempotency_key" {
		t.Fatalf("no key: %d %v", rec.Code, out)
	}
	if rec, out := post(t, h, `{"row_key":"k","park_id":"`+parkCBE+`","bogus":1}`, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusBadRequest || out["error"] != "invalid_body" {
		t.Fatalf("unknown field: %d %v", rec.Code, out)
	}
	// The row's copy is never accepted from the client: the old echo fields are unknown.
	if rec, out := post(t, h, `{"row_key":"k","park_id":"`+parkCBE+`","row_title":"Fabricated"}`, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusBadRequest || out["error"] != "invalid_body" {
		t.Fatalf("client-supplied title: %d %v", rec.Code, out)
	}
	if rec, out := post(t, h, `{"row_key":"k","park_id":"`+parkCBE+`","business_date":"yesterday"}`, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusBadRequest || out["error"] != "invalid_business_date" {
		t.Fatalf("bad date: %d %v", rec.Code, out)
	}
	// A row the caller's board does not hold is 404, and an unparseable key 400.
	h4 := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{err: app.ErrFlagRowNotFound})
	if rec, out := post(t, h4, good, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusNotFound || out["error"] != "row_not_found" {
		t.Fatalf("not found: %d %v", rec.Code, out)
	}
	h5 := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{err: domain.ErrInvalidRowKey})
	if rec, out := post(t, h5, good, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusBadRequest || out["error"] != "invalid_row_key" {
		t.Fatalf("bad key: %d %v", rec.Code, out)
	}
	// A park head flagging their own park is refused by the raise rule, mapped to 422.
	h2 := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{err: ltdomain.ErrSelfAssignment})
	if rec, out := post(t, h2, good, actorOp, parkHeadGrant(parkCBE), "k"); rec.Code != http.StatusUnprocessableEntity || out["error"] != "flag_to_self" {
		t.Fatalf("self: %d %v", rec.Code, out)
	}
	h3 := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{err: ports.ErrParkHeadMissing})
	if rec, out := post(t, h3, good, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusUnprocessableEntity || out["error"] != "park_head_missing" {
		t.Fatalf("missing head: %d %v", rec.Code, out)
	}
	// Outside the caller's park scope is a 403 before anything is raised.
	if rec, _ := post(t, h, `{"row_key":"k","park_id":"`+parkCPT+`"}`, actorOp, parkHeadGrant(parkCBE), "k"); rec.Code != http.StatusForbidden {
		t.Fatalf("other park: %d", rec.Code)
	}
	// The same Idempotency-Key with a different body is a 409, never a 500 (live E2E 2026-09-11).
	h6 := NewHandler(&fakeService{}, nil).WithFlags(&fakeFlags{err: ltports.ErrIdempotencyConflict})
	if rec, out := post(t, h6, good, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusConflict || out["error"] != "idempotency_conflict" {
		t.Fatalf("idempotency conflict: %d %v", rec.Code, out)
	}
	// Without a flag service the route answers 404, never a panic.
	if rec, _ := post(t, NewHandler(&fakeService{}, nil), good, actorCEO, tenantGrant(permissions.RoleCEOInternal), "k"); rec.Code != http.StatusNotFound {
		t.Fatalf("no service: %d", rec.Code)
	}
}

// TestSubtasksAreServedOnlyForARowOnTheCallersBoard: the row is resolved through FindRow on
// the same scoped query the rows read uses (park, modules, owner clamp), a row the board does
// not hold is 404, and the page parameters reach the service.
func TestSubtasksAreServedOnlyForARowOnTheCallersBoard(t *testing.T) {
	const key = "weighing|weighing_work_item|00000000-0000-4000-8000-000000009101"
	svc := &fakeService{found: true}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/rows/"+key+"/subtasks?limit=20&cursor=1:abc", actorOp, operatorGrant(parkCBE))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if svc.last.OwnerUserID != actorOp || svc.last.ParkID != parkCBE {
		t.Fatalf("the operator lens must clamp the row lookup to their own rows in their park: %+v", svc.last)
	}
	if svc.lastRowKey != key || svc.lastAfter != "1:abc" || svc.lastLimit != 20 {
		t.Fatalf("service received %q %q %d", svc.lastRowKey, svc.lastAfter, svc.lastLimit)
	}
	if body["row_key"] != key || body["total"] != float64(1) || body["park_id"] != parkCBE {
		t.Fatalf("payload %v", body)
	}
	subs, _ := body["subtasks"].([]any)
	if len(subs) != 1 {
		t.Fatalf("subtasks %v", body["subtasks"])
	}
	first, _ := subs[0].(map[string]any)
	if first["name"] != "Tag 1" || first["lane"] != "todo" {
		t.Fatalf("subtask %v", first)
	}

	// Not on the caller's board: 404 with a stable code, never a leak of the row.
	missing := &fakeService{found: false}
	rec, body = get(t, NewHandler(missing, nil), "/work-board/rows/"+key+"/subtasks", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusBadRequest || body["error"] != "park_required" {
		t.Fatalf("a tenant-wide caller still names the park: %d %v", rec.Code, body)
	}
	rec, body = get(t, NewHandler(missing, nil), "/work-board/rows/"+key+"/subtasks?park="+parkCBE, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusNotFound || body["error"] != "row_not_found" {
		t.Fatalf("want 404 row_not_found, got %d %v", rec.Code, body)
	}
	if missing.lastLimit != 0 {
		t.Fatal("the subtask read must not run for a row the board does not hold")
	}

	// Bad inputs carry stable codes.
	cases := map[string]string{
		"/work-board/rows/garbage/subtasks?park=" + parkCBE:                    "invalid_row_key",
		"/work-board/rows/" + key + "/subtasks?park=" + parkCBE + "&cursor=zz": "invalid_cursor",
		"/work-board/rows/" + key + "/subtasks?park=" + parkCBE + "&limit=0":   "invalid_limit",
		"/work-board/rows/" + key + "/subtasks?park=not-a-uuid":                "invalid_park_id",
	}
	for path, code := range cases {
		rec, body := get(t, NewHandler(&fakeService{found: true}, nil), path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
		if body["error"] != code {
			t.Errorf("%s: want %s, got %d %v", path, code, rec.Code, body)
		}
	}
	// A park head outside their park is refused before the row is looked up.
	rec, body = get(t, NewHandler(&fakeService{found: true}, nil), "/work-board/rows/"+key+"/subtasks?park="+parkCPT, actorOp, parkHeadGrant(parkCBE))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("other park must be refused: %d %v", rec.Code, body)
	}
}

// TestNoModulePermissionMeansNoModuleNeverEveryModule: a person ticked for the Work Board
// (read, even oversee) with NO module permission sees an empty board, not the whole
// registry, and cannot flag a row. Found live 2026-09-11: the permission-derived visible set
// was intersected with the "empty means all" helper, so an empty set inverted to all eight
// modules on the rows, summary, subtasks AND flag paths.
func TestNoModulePermissionMeansNoModuleNeverEveryModule(t *testing.T) {
	svc := &fakeService{}
	flags := &fakeFlags{}
	h := NewHandler(svc, nil).WithFlags(flags)
	perms := []string{permissions.WorkBoardRead, permissions.WorkBoardOversee}
	withPerson := func(req *http.Request) *http.Request {
		ctx := httpmiddleware.WithTenantID(req.Context(), tenant)
		ctx = httpmiddleware.WithActorID(ctx, actorCEO)
		ctx = httpmiddleware.WithAuthGrants(ctx, tenantGrant(permissions.RoleCEOInternal))
		ctx = httpmiddleware.WithPersonPermissions(ctx, perms)
		return req.WithContext(ctx)
	}
	mux := http.NewServeMux()
	Register(mux, h)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withPerson(httptest.NewRequest(http.MethodGet, "/work-board/rows?park="+parkCBE, nil)))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusOK {
		t.Fatalf("rows: %d %v", rec.Code, body)
	}
	if mods, _ := body["modules"].([]any); len(mods) != 0 {
		t.Fatalf("no module permission must serve no module on the wire, got %v", mods)
	}
	if !svc.last.NoModules || len(svc.last.Modules) != 0 {
		t.Fatalf("service must be asked for NO module: %+v", svc.last)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withPerson(httptest.NewRequest(http.MethodGet, "/work-board/summary?park="+parkCBE, nil)))
	if rec.Code != http.StatusOK || !svc.last.NoModules {
		t.Fatalf("summary must be asked for NO module: %d %+v", rec.Code, svc.last)
	}

	rec = httptest.NewRecorder()
	req := withPerson(httptest.NewRequest(http.MethodPost, "/work-board/flags", strings.NewReader(`{"row_key":"weighing|weighing_work_item|w1","park_id":"`+parkCBE+`"}`)))
	req.Header.Set("Idempotency-Key", "k-no-modules")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("the handler forwards to the flag service; got %d", rec.Code)
	}
	if !flags.last.Board.NoModules || len(flags.last.Board.Modules) != 0 {
		t.Fatalf("the flag must be resolved on a board with NO module, got %+v", flags.last.Board)
	}
}
