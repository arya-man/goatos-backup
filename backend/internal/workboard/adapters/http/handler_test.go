package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

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
	actorAlt = "00000000-0000-4000-8000-000000000302"
	actorCEO = "00000000-0000-4000-8000-000000000999"
)

// fakeService records the query the handler built, which is the whole point of these
// tests: every scope rule lives in the handler, and the service must receive its result.
type fakeService struct {
	mu           sync.Mutex
	last         domain.Query
	lists        []domain.Query
	summaries    int
	counts       map[domain.WorkState]int
	degraded     []domain.Module
	listDegraded []domain.Module
	// found is what FindRow answers; lastRowKey / lastAfter / lastLimit record the subtask read.
	found               bool
	lastRowKey          string
	lastAfter           string
	lastLimit           int
	listDelay           time.Duration
	activeList          int
	maxList             int
	failOwnerVocabulary bool
}

func (f *fakeService) FindRow(_ context.Context, q domain.Query, rowKey string) (domain.Row, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = q
	f.lastRowKey = rowKey
	if !f.found {
		return domain.Row{}, false, nil
	}
	return domain.Row{RowKey: rowKey}, true, nil
}

func (f *fakeService) ListSubtasks(_ context.Context, q domain.Query, rowKey, afterKey string, limit int) (domain.SubtaskPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = q
	f.lastRowKey, f.lastAfter, f.lastLimit = rowKey, afterKey, limit
	st := domain.Subtask{Key: "1:a", Name: "Tag 1", WorkState: domain.WorkStateDue, Steps: []domain.Step{{Name: "Scan", State: domain.StepTodo}}}.Finalize()
	return domain.SubtaskPage{Subtasks: []domain.Subtask{st}, Total: 1}, nil
}

func (f *fakeService) List(_ context.Context, q domain.Query) (domain.Page, error) {
	f.mu.Lock()
	f.last = q
	f.lists = append(f.lists, q)
	f.activeList++
	if f.activeList > f.maxList {
		f.maxList = f.activeList
	}
	f.mu.Unlock()
	if f.listDelay > 0 {
		time.Sleep(f.listDelay)
	}
	if f.failOwnerVocabulary && q.OwnerUserID == "" && q.Limit == 100 {
		return domain.Page{}, errors.New("owner vocabulary unavailable")
	}
	defer func() {
		f.mu.Lock()
		f.activeList--
		f.mu.Unlock()
	}()
	state := domain.WorkStateDue
	if len(q.WorkStates) > 0 {
		state = q.WorkStates[0]
	}
	owner := domain.Owner{UserID: actorOp, Name: "Operator One"}
	if q.OwnerUserID != "" {
		owner = domain.Owner{UserID: q.OwnerUserID, Name: "Selected Operator"}
	}
	row := domain.Row{Module: domain.ModuleFeed, SourceType: "feed_task", SourceID: string(state), ParkID: q.ParkID, BusinessDate: q.BusinessDate, WorkState: state, Owner: owner, OwnerState: domain.OwnerStateAssigned, Title: "Feed work"}.Finalize()
	rows := []domain.Row{row}
	if q.OwnerUserID == "" && q.Limit == 100 {
		rows = append(rows, domain.Row{Module: domain.ModuleFeed, SourceType: "feed_task", SourceID: string(state) + "-alt", ParkID: q.ParkID, BusinessDate: q.BusinessDate, WorkState: state, Owner: domain.Owner{UserID: actorAlt, Name: "Operator Two"}, OwnerState: domain.OwnerStateAssigned, Title: "Feed work 2"}.Finalize())
	}
	return domain.Page{Rows: rows, Degraded: f.listDegraded}, nil
}
func (f *fakeService) Summary(_ context.Context, q domain.Query) (domain.Summary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = q
	f.summaries++
	sum := domain.NewSummary(q.Modules)
	for state, n := range f.counts {
		sum.Add(domain.ModuleFeed, map[domain.WorkState]int{state: n})
	}
	sum.Degraded = append(sum.Degraded, f.degraded...)
	return sum, nil
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

// TestPageBundlesSummaryAndLaneRows proves the admin-web can open one park with one backend
// request instead of separate summary plus per-lane row calls.
func TestPageBundlesSummaryAndLaneRows(t *testing.T) {
	svc := &fakeService{counts: map[domain.WorkState]int{
		domain.WorkStateDue:                 1,
		domain.WorkStateInProgress:          1,
		domain.WorkStateVerificationPending: 1,
		domain.WorkStateCompleted:           1,
	}}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&limit=10&cursor_done=feed|feed_task|completed", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if _, ok := body["summary"].(map[string]any); !ok {
		t.Fatalf("page must carry the summary: %#v", body)
	}
	lanes, ok := body["lanes"].(map[string]any)
	if !ok || len(lanes) != len(domain.Lanes()) {
		t.Fatalf("page must carry every lane, got %#v", body["lanes"])
	}
	if len(svc.lists) != len(domain.Lanes()) {
		t.Fatalf("expected one service list per lane, got %d", len(svc.lists))
	}
	if svc.lists[0].Limit != 10 {
		t.Fatalf("page limit must flow to lane reads, got %d", svc.lists[0].Limit)
	}
	gotCursor := false
	for _, q := range svc.lists {
		for _, state := range q.WorkStates {
			if domain.LaneFor(state) == domain.LaneDone && q.Cursor.SourceID == "completed" {
				gotCursor = true
			}
		}
	}
	if !gotCursor {
		t.Fatalf("done lane cursor did not flow to the done lane read: %#v", svc.lists)
	}
}

func TestPageReadsFreshScopedBundleAfterMutation(t *testing.T) {
	svc := &fakeService{counts: map[domain.WorkState]int{domain.WorkStateDue: 1}}
	h := NewHandler(svc, nil)
	path := "/work-board/page?park=" + parkCBE + "&business_date=2026-09-10&limit=5"
	rec, _ := get(t, h, path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("initial status %d", rec.Code)
	}
	// Simulate another request completing the last outstanding task.
	svc.counts = map[domain.WorkState]int{domain.WorkStateCompleted: 1}
	rec, body := get(t, h, path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("fresh status %d", rec.Code)
	}
	lanes := body["lanes"].(map[string]any)
	todo := lanes["todo"].(map[string]any)["rows"].([]any)
	done := lanes["done"].(map[string]any)["rows"].([]any)
	if svc.summaries != 2 || len(todo) != 0 || len(done) != 1 {
		t.Fatalf("completed task remained stale: reads=%d todo=%d done=%d", svc.summaries, len(todo), len(done))
	}
	rec, _ = get(t, h, path, actorOp, parkHeadGrant(parkCBE))
	if rec.Code != http.StatusOK || svc.summaries != 3 || svc.last.ParkID != parkCBE {
		t.Fatalf("changed actor scope: status=%d reads=%d query=%+v", rec.Code, svc.summaries, svc.last)
	}
}

func TestPageSkipsZeroCountLaneReads(t *testing.T) {
	svc := &fakeService{counts: map[domain.WorkState]int{domain.WorkStateDue: 1}}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if len(svc.lists) != 1 {
		t.Fatalf("expected only non-empty todo lane to read rows, got %d: %#v", len(svc.lists), svc.lists)
	}
	if got := svc.lists[0].WorkStates; len(got) == 0 || domain.LaneFor(got[0]) != domain.LaneToDo {
		t.Fatalf("expected todo lane read, got %#v", svc.lists[0])
	}
	lanes := body["lanes"].(map[string]any)
	if _, ok := lanes[string(domain.LaneDone)]; !ok {
		t.Fatalf("zero-count lanes must still be present in payload: %#v", lanes)
	}
}

func TestPageCursorForcesLaneReadEvenWhenSummaryIsZero(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&cursor_done=feed|feed_task|completed", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if len(svc.lists) != 1 {
		t.Fatalf("cursor lane must be read even when summary count is zero, got %d: %#v", len(svc.lists), svc.lists)
	}
	if svc.lists[0].Cursor.SourceID != "completed" {
		t.Fatalf("cursor did not flow to forced lane read: %#v", svc.lists[0])
	}
}

func TestPageDoesNotPruneDegradedSummaryModules(t *testing.T) {
	svc := &fakeService{
		counts:   map[domain.WorkState]int{domain.WorkStateDue: 1},
		degraded: []domain.Module{domain.ModuleVaccination},
	}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	foundVaccination := false
	for _, q := range svc.lists {
		for _, module := range q.Modules {
			if module == domain.ModuleVaccination {
				foundVaccination = true
			}
		}
	}
	if !foundVaccination {
		t.Fatalf("a degraded summary module is unknown, not zero; lane reads must still include it: %#v", svc.lists)
	}
}

func TestPageReadsZeroCountLaneWhenSummaryDegraded(t *testing.T) {
	svc := &fakeService{degraded: []domain.Module{domain.ModuleFeed}}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&page_lane=todo", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	lanes := body["lanes"].(map[string]any)
	rows := lanes["todo"].(map[string]any)["rows"].([]any)
	if len(rows) == 0 || len(svc.lists) != 1 {
		t.Fatalf("unknown count suppressed recoverable rows: lists=%d rows=%d", len(svc.lists), len(rows))
	}
	if svc.lists[0].NoModules {
		t.Fatal("degraded source must remain eligible")
	}
}

func TestPageDoesNotCacheDegradedBundle(t *testing.T) {
	for _, source := range []string{"summary", "lane"} {
		t.Run(source, func(t *testing.T) {
			svc := &fakeService{counts: map[domain.WorkState]int{domain.WorkStateDue: 1}}
			if source == "summary" {
				svc.degraded = []domain.Module{domain.ModuleFeed}
			} else {
				svc.listDegraded = []domain.Module{domain.ModuleFeed}
			}
			h := NewHandler(svc, nil)
			path := "/work-board/page?park=" + parkCBE + "&page_lane=todo&debug_timing=1"
			rec, _ := get(t, h, path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
			if rec.Code != http.StatusOK {
				t.Fatalf("first status %d", rec.Code)
			}
			svc.degraded = nil
			svc.listDegraded = nil
			rec, body := get(t, h, path, actorCEO, tenantGrant(permissions.RoleCEOInternal))
			if svc.summaries != 2 || strings.Contains(rec.Header().Get("X-GoatOS-Route-Timing"), "cache_hit") {
				t.Fatalf("transient degradation was cached: summary reads=%d", svc.summaries)
			}
			if degraded, _ := body["degraded"].([]any); len(degraded) > 0 {
				t.Fatalf("recovered response still degraded: %v", degraded)
			}
		})
	}
}

func TestPageHonorsRequestedLanes(t *testing.T) {
	svc := &fakeService{counts: map[domain.WorkState]int{domain.WorkStateDue: 1, domain.WorkStateCompleted: 1}}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&page_lane=todo,done", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	lanes := body["lanes"].(map[string]any)
	if _, ok := lanes["in_progress"]; ok {
		t.Fatalf("page_lane must not fetch in_progress: %#v", lanes)
	}
	if len(svc.lists) != 2 {
		t.Fatalf("expected two lane reads, got %d", len(svc.lists))
	}
}

func TestPageBoundsLaneServiceConcurrency(t *testing.T) {
	svc := &fakeService{
		counts: map[domain.WorkState]int{
			domain.WorkStateDue:                 1,
			domain.WorkStateInProgress:          1,
			domain.WorkStateVerificationPending: 1,
			domain.WorkStateCompleted:           1,
		},
		listDelay: 10 * time.Millisecond,
	}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if len(svc.lists) != len(domain.Lanes()) {
		t.Fatalf("expected every lane to be read, got %d", len(svc.lists))
	}
	if svc.maxList != maxPageLaneServiceConcurrency {
		t.Fatalf("max concurrent lane service reads=%d want %d", svc.maxList, maxPageLaneServiceConcurrency)
	}
}

func TestPageSummaryOnlyReadKeepsScopedVocabulary(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&page_lane=__none__", actorCEO, tenantGrant(permissions.RoleFeedDirector))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if len(svc.lists) != 0 {
		t.Fatalf("summary-only page must not read lane rows, got %d", len(svc.lists))
	}
	summary := body["summary"].(map[string]any)
	modules := summary["modules"].([]any)
	if len(modules) != 1 || modules[0] != string(domain.ModuleFeed) {
		t.Fatalf("feed director vocabulary must stay permission scoped, got %#v", modules)
	}
}

func TestPageOwnerVocabularyUsesWorkBoardScopeNotSelectedOwnerRows(t *testing.T) {
	svc := &fakeService{counts: map[domain.WorkState]int{domain.WorkStateDue: 1}}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&owner="+actorOp+"&include_owner_vocabulary=1", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	vocab := body["owner_vocabulary"].([]any)
	if len(vocab) != 2 {
		t.Fatalf("owner vocabulary must come from the unselected Work Board scope, got %#v", vocab)
	}
	if len(svc.lists) != 2 {
		t.Fatalf("expected one lane read plus one Work Board-authorized owner vocabulary read, got %d list reads", len(svc.lists))
	}
}

func TestPageOwnerVocabularyFailureDoesNotBlankBoard(t *testing.T) {
	svc := &fakeService{
		counts:              map[domain.WorkState]int{domain.WorkStateDue: 1},
		failOwnerVocabulary: true,
	}
	h := NewHandler(svc, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&owner="+actorOp+"&include_owner_vocabulary=1", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("optional owner vocabulary failure must not fail the board, got %d %v", rec.Code, body)
	}
	lanes := body["lanes"].(map[string]any)
	todo := lanes["todo"].(map[string]any)
	rows := todo["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("board lane rows should still render, got %#v", todo)
	}
	if _, ok := body["owner_vocabulary"]; ok {
		t.Fatalf("failed optional vocabulary should be omitted, got %#v", body["owner_vocabulary"])
	}
	if len(svc.lists) != 2 {
		t.Fatalf("expected one lane read plus one attempted owner vocabulary read, got %d list reads", len(svc.lists))
	}
}

func TestPageRejectsBadLaneCursor(t *testing.T) {
	h := NewHandler(&fakeService{}, nil)
	rec, body := get(t, h, "/work-board/page?park="+parkCBE+"&cursor_done=garbage", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("one bad lane cursor must not fail the whole page, got %d %v", rec.Code, body)
	}
	lanes := body["lanes"].(map[string]any)
	done := lanes["done"].(map[string]any)
	if degraded, _ := done["degraded"].([]any); len(degraded) == 0 {
		t.Fatalf("bad lane cursor should degrade that lane: %#v", done)
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

	svc = &fakeService{found: true}
	h = NewHandler(svc, nil)
	rec, body = get(t, h, "/work-board/rows/"+key+"/subtasks?park="+parkCBE+"&owner="+actorOp, actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("owner scoped subtasks: status %d %v", rec.Code, body)
	}
	if svc.last.OwnerUserID != actorOp {
		t.Fatalf("subtasks must preserve the selected owner scope, got query %+v", svc.last)
	}
	if svc.lastRowKey != key {
		t.Fatalf("owner scoped row key %q", svc.lastRowKey)
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

// TestLaneParameterReadsOneColumn: `lane=done` is the column read (the lane's states), so a
// board can page each column on its own; an unknown lane is refused; lane and state intersect.
func TestLaneParameterReadsOneColumn(t *testing.T) {
	svc := &fakeService{}
	h := NewHandler(svc, nil)
	rec, out := get(t, h, "/work-board/rows?park="+parkCBE+"&lane=done", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK {
		t.Fatalf("lane=done: %d %v", rec.Code, out)
	}
	if len(svc.last.WorkStates) != 1 || svc.last.WorkStates[0] != domain.WorkStateCompleted {
		t.Fatalf("done column is the completed state, got %v", svc.last.WorkStates)
	}
	rec, _ = get(t, h, "/work-board/rows?park="+parkCBE+"&lane=in_progress", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK || len(svc.last.WorkStates) != 4 {
		t.Fatalf("in_progress column holds four states, got %v", svc.last.WorkStates)
	}
	rec, out = get(t, h, "/work-board/rows?park="+parkCBE+"&lane=bogus", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusBadRequest || out["error"] != "invalid_lane" {
		t.Fatalf("unknown lane: %d %v", rec.Code, out)
	}
	rec, _ = get(t, h, "/work-board/rows?park="+parkCBE+"&lane=done&state=due", actorCEO, tenantGrant(permissions.RoleCEOInternal))
	if rec.Code != http.StatusOK || len(svc.last.WorkStates) != 1 || svc.last.WorkStates[0] != domain.WorkStateNone {
		t.Fatalf("a state outside the lane is an empty column, got %v", svc.last.WorkStates)
	}
}
