package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// repairProvider is a fakeProvider that also implements the optional
// usagePlanner + sqlRepairer capabilities (as adapters/vertex does).
type repairProvider struct {
	fakeProvider
	usage       TokenUsage
	repairSQL   string
	repairErr   error
	repairCalls int
	lastReason  string
	lastCard    string
	lastWindow  string
}

func (r *repairProvider) PlanWithUsage(ctx context.Context, q domain.Question, mem []domain.ResolvedEntities, cat []ports.ToolSpec) (domain.Plan, TokenUsage, error) {
	p, err := r.Plan(ctx, q, mem, cat)
	return p, r.usage, err
}

func (r *repairProvider) RepairSQL(_ context.Context, _ domain.Question, _ string, reason, card, window string) (string, TokenUsage, error) {
	r.repairCalls++
	r.lastReason, r.lastCard, r.lastWindow = reason, card, window
	return r.repairSQL, TokenUsage{PromptTokens: 100, OutputTokens: 10}, r.repairErr
}

// sequencedSQLFallback fails the first N executes with a scripted error, then
// succeeds; it records every statement it was handed.
//
// It is mutex-guarded because the step executor runs a plan's independent
// sub-questions CONCURRENTLY: a plan with two SQL reads calls this double from
// two goroutines at once, exactly as it calls the real pgx-backed executor.
type sequencedSQLFallback struct {
	mu     sync.Mutex
	errs   []error
	calls  int
	sqls   []string
	result domain.ToolResult
}

func (f *sequencedSQLFallback) Execute(_ context.Context, _ domain.Actor, sql string, _ []any) (domain.ToolResult, error) {
	f.mu.Lock()
	f.sqls = append(f.sqls, sql)
	i := f.calls
	f.calls++
	f.mu.Unlock()
	if i < len(f.errs) && f.errs[i] != nil {
		return domain.ToolResult{}, f.errs[i]
	}
	r := f.result
	r.Route = domain.RouteSQL
	r.ToolName = "sql_fallback"
	if r.Surface == "" {
		r.Surface = "Mesha operational data"
	}
	return r, nil
}

func (f *sequencedSQLFallback) ExecuteTrusted(ctx context.Context, a domain.Actor, sql string, args []any) (domain.ToolResult, error) {
	return f.Execute(ctx, a, sql, args)
}

type fakeBudget struct {
	in, out int
	calls   int
}

func (b *fakeBudget) Reserve(context.Context, domain.Actor) (bool, string) { return true, "" }
func (b *fakeBudget) Record(_ context.Context, _ domain.Actor, in, out int) {
	b.calls++
	b.in, b.out = in, out
}

type fakeSQLTelemetry struct {
	nopTelemetry
	rejects     []string
	pgCodes     []string
	tenantGates []string
}

func (t *fakeSQLTelemetry) TenantGateReject(_ context.Context, reason string) {
	t.tenantGates = append(t.tenantGates, reason)
}

func (t *fakeSQLTelemetry) SQLReject(_ context.Context, reason string) {
	t.rejects = append(t.rejects, reason)
}
func (t *fakeSQLTelemetry) SQLPGError(_ context.Context, code string) {
	t.pgCodes = append(t.pgCodes, code)
}

type nopTelemetry struct{}

func (nopTelemetry) RecordRequest(context.Context, string, string, float64) {}
func (nopTelemetry) RecordToolRows(context.Context, string, int)            {}
func (nopTelemetry) VertexFailover(context.Context)                         {}
func (nopTelemetry) RateLimitTrip(context.Context)                          {}
func (nopTelemetry) BudgetRejection(context.Context)                        {}
func (nopTelemetry) CacheLookup(context.Context, bool)                      {}
func (nopTelemetry) ReviewCorrection(context.Context)                       {}
func (nopTelemetry) InjectionBlocked(context.Context)                       {}

const (
	badSQL   = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' OFFSET 5 LIMIT 100"
	goodSQL  = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' LIMIT 100"
	juneSQL  = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2026-06-01' AND event_date < '2026-07-01' LIMIT 100"
	scopeSQL = "SELECT 'Active animals' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.animal_current_scope WHERE tenant_id = 't1' AND lifecycle_status = 'alive' LIMIT 100"
)

func sqlPlan(sql string) domain.Plan {
	return domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID: "0", Text: "deaths", IntentClass: "mortality", Route: domain.RouteSQL, ToolName: "sql_fallback",
		Params: map[string]any{"sql": sql},
	}}}
}

func deathsFacts() domain.ToolResult {
	return domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Deaths", Value: "7"}}}
}

var repairNow = time.Date(2026, 7, 22, 9, 0, 0, 0, biztime.DefaultLocation())

// TestRepairLoopOnceThenPartial is the D1.3 contract: a guard rejection of a
// model-drafted draft is re-prompted exactly once with the reason + card; the
// repaired draft runs through the same guard and grounds the answer. When the
// repair also fails, no third attempt is made and the existing honest-partial
// path answers.
func TestRepairLoopOnceThenPartial(t *testing.T) {
	t.Run("reject then repaired draft grounds the answer", func(t *testing.T) {
		sqlFB := &sequencedSQLFallback{result: deathsFacts()}
		prov := &repairProvider{fakeProvider: fakeProvider{plan: sqlPlan(badSQL), byModel: true}, repairSQL: goodSQL, usage: TokenUsage{PromptTokens: 3000, OutputTokens: 120}}
		tel := &fakeSQLTelemetry{}
		budget := &fakeBudget{}
		audit := &fakeAudit{}
		a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel, Budget: budget, Audit: audit})

		ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets", AsOf: repairNow})
		if err != nil {
			t.Fatal(err)
		}
		if prov.repairCalls != 1 {
			t.Fatalf("expected exactly one repair call, got %d", prov.repairCalls)
		}
		if !strings.Contains(prov.lastReason, "OFFSET") || !strings.Contains(prov.lastCard, "ceo_ai.mortality_base") {
			t.Fatalf("repair prompt must carry the validator reason and the view card: reason=%q card=%q", prov.lastReason, prov.lastCard)
		}
		// The rejected draft never reached the executor; only the repaired one did.
		if sqlFB.calls != 1 || sqlFB.sqls[0] != goodSQL {
			t.Fatalf("executor calls=%d sqls=%v", sqlFB.calls, sqlFB.sqls)
		}
		if !strings.Contains(ans.Answer, "7") || ans.Mode != domain.ModePlanned {
			t.Fatalf("expected grounded planned answer, got mode=%s answer=%q", ans.Mode, ans.Answer)
		}
		if len(tel.rejects) != 1 || tel.rejects[0] != "banned_keyword" {
			t.Fatalf("ceoai_sql_reject_total reason: %v", tel.rejects)
		}
		// Token accounting: plan usage + repair usage, not len/4.
		if budget.in != 3100 || budget.out != 130 {
			t.Fatalf("budget recorded %d/%d, want 3100/130 from usageMetadata", budget.in, budget.out)
		}
		// The validator reason is in the admin trace: the failed step plus the repair step.
		if audit.rec == nil {
			t.Fatal("no audit record")
		}
		var sawReject, sawRepair bool
		for _, st := range audit.rec.Steps {
			if st.ToolName == "sql_fallback" && strings.Contains(st.Err, "OFFSET") {
				sawReject = true
			}
			if st.ToolName == "sql_repair" && st.Err == "" && st.RowCount == 1 {
				sawRepair = true
			}
		}
		if !sawReject || !sawRepair {
			t.Fatalf("trace must record the validator reason and the repair step: %+v", audit.rec.Steps)
		}
	})

	t.Run("pg error then repaired draft", func(t *testing.T) {
		pgErr := &pgconn.PgError{Code: "42703", Message: "column deathz does not exist"}
		sqlFB := &sequencedSQLFallback{errs: []error{pgErr}, result: deathsFacts()}
		prov := &repairProvider{fakeProvider: fakeProvider{plan: sqlPlan(goodSQL), byModel: true}, repairSQL: goodSQL}
		tel := &fakeSQLTelemetry{}
		a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
		ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets", AsOf: repairNow})
		if err != nil {
			t.Fatal(err)
		}
		if prov.repairCalls != 1 || sqlFB.calls != 2 {
			t.Fatalf("repair=%d exec=%d", prov.repairCalls, sqlFB.calls)
		}
		if !strings.Contains(prov.lastReason, "deathz") {
			t.Fatalf("pg error text must reach the repair prompt: %q", prov.lastReason)
		}
		if len(tel.pgCodes) != 1 || tel.pgCodes[0] != "42703" {
			t.Fatalf("ceoai_sql_pg_error_total code: %v", tel.pgCodes)
		}
		if !strings.Contains(ans.Answer, "7") {
			t.Fatalf("expected grounded answer, got %q", ans.Answer)
		}
	})

	t.Run("second failure is honest partial, no third attempt", func(t *testing.T) {
		sqlFB := &sequencedSQLFallback{result: deathsFacts()}
		// The "repair" is just as bad: still OFFSET, still rejected by the guard.
		prov := &repairProvider{fakeProvider: fakeProvider{plan: sqlPlan(badSQL), byModel: true}, repairSQL: badSQL}
		tel := &fakeSQLTelemetry{}
		a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
		ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets", AsOf: repairNow})
		if err != nil {
			t.Fatal(err)
		}
		if prov.repairCalls != 1 {
			t.Fatalf("expected exactly one repair attempt, got %d", prov.repairCalls)
		}
		if sqlFB.calls != 0 {
			t.Fatalf("a rejected draft must never reach the executor, got %d calls", sqlFB.calls)
		}
		if len(tel.rejects) != 2 {
			t.Fatalf("both rejections counted: %v", tel.rejects)
		}
		if strings.Contains(ans.Answer, "7") {
			t.Fatalf("no fact was ever read; answer must not contain a figure: %q", ans.Answer)
		}
		if ans.Mode == domain.ModeRefused {
			t.Fatalf("a failed read is an honest partial, not a refusal: %q", ans.Answer)
		}
	})

	t.Run("provider without repair capability is untouched", func(t *testing.T) {
		sqlFB := &sequencedSQLFallback{result: deathsFacts()}
		prov := &fakeProvider{plan: sqlPlan(badSQL), byModel: true}
		a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB)})
		if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets", AsOf: repairNow}); err != nil {
			t.Fatal(err)
		}
		if sqlFB.calls != 0 {
			t.Fatalf("rejected draft executed: %d", sqlFB.calls)
		}
	})
}

// TestWindowThreadedIntoSQLAndEnforced: a period question threads from/to into
// every sub-question; a model-drafted SQL read on a dated view must bind the
// window exactly (rejected + repaired otherwise) and the answer prints the
// window once.
func TestWindowThreadedIntoSQLAndEnforced(t *testing.T) {
	sqlFB := &sequencedSQLFallback{result: deathsFacts()}
	// Draft ignores the period; the repair binds it.
	prov := &repairProvider{fakeProvider: fakeProvider{plan: sqlPlan(goodSQL), byModel: true}, repairSQL: juneSQL}
	tel := &fakeSQLTelemetry{}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets last month", AsOf: repairNow})
	if err != nil {
		t.Fatal(err)
	}
	if len(tel.rejects) != 1 || tel.rejects[0] != "window" {
		t.Fatalf("unbound period must be rejected as a window reject: %v", tel.rejects)
	}
	if !strings.Contains(prov.lastWindow, "event_date >= '2026-06-01' AND event_date < '2026-07-01'") {
		t.Fatalf("repair prompt must state the exact binding: %q", prov.lastWindow)
	}
	if sqlFB.calls != 1 || sqlFB.sqls[0] != juneSQL {
		t.Fatalf("only the bound draft may execute: %v", sqlFB.sqls)
	}
	if !strings.Contains(ans.Answer, "Window: last month (01/06/2026 to 30/06/2026)") {
		t.Fatalf("answer must print the window once: %q", ans.Answer)
	}
	if strings.Count(ans.Answer, "Window:") != 1 {
		t.Fatalf("window printed more than once: %q", ans.Answer)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("mode %s", ans.Mode)
	}
}

// TestWindowOnCurrentStateViewAnswersAsOfNow: a period question routed to a
// current-state view (no date column) is not rejected; from/to are stripped,
// the read runs as of now and the answer says "Window: as of <date>".
func TestWindowOnCurrentStateViewAnswersAsOfNow(t *testing.T) {
	sqlFB := &sequencedSQLFallback{result: domain.ToolResult{Facts: []domain.Fact{{TenantID: "t1", Label: "Active animals", Value: "972"}}}}
	prov := &repairProvider{fakeProvider: fakeProvider{plan: sqlPlan(scopeSQL), byModel: true}}
	tel := &fakeSQLTelemetry{}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets did we have last month", AsOf: repairNow})
	if err != nil {
		t.Fatal(err)
	}
	if prov.repairCalls != 0 || len(tel.rejects) != 0 || sqlFB.calls != 1 {
		t.Fatalf("current-state read must run once without repair: repair=%d rejects=%v exec=%d", prov.repairCalls, tel.rejects, sqlFB.calls)
	}
	if !strings.Contains(ans.Answer, "972") || !strings.Contains(ans.Answer, "Window: as of 22/07/2026") {
		t.Fatalf("expected the figure and an explicit as-of window: %q", ans.Answer)
	}
}

func TestInjectWindowAndPrepareSQLWindows(t *testing.T) {
	w, ok := ResolveWindow("aug vs sep", repairNow, nil)
	if !ok || w.Compare == nil {
		t.Fatal("expected comparison window")
	}
	subs := []domain.SubQuestion{
		{ID: "0", Route: domain.RouteCube, ToolName: "active_animals"},
		{ID: "1", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": scopeSQL}},
		{ID: "2", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": goodSQL, paramFrom: "2026-01-01", paramTo: "2026-01-31"}},
	}
	injectWindow(subs, w, "")
	for _, s := range subs[:2] {
		if s.Params[paramFrom] != "2025-08-01" || s.Params[paramTo] != "2025-08-31" || s.Params[paramCompareFrom] != "2025-09-01" || s.Params[paramCompareTo] != "2025-09-30" || s.Params[paramWindowLabel] != "aug" {
			t.Fatalf("sub %s params: %+v", s.ID, s.Params)
		}
	}
	// M2: a plan-seeded from/to is OVERWRITTEN by the server-resolved window.
	if subs[2].Params[paramFrom] != "2025-08-01" || subs[2].Params[paramTo] != "2025-08-31" {
		t.Fatalf("a plan-seeded from/to must be overwritten by the resolved window: %+v", subs[2].Params)
	}
	// Cube params: window keys are reserved, never equality filters.
	_, _, filters := cubeParams(subs[0].Params)
	for _, k := range []string{paramFrom, paramTo, paramWindowLabel, paramCompareFrom, paramCompareTo} {
		if _, leaked := filters[k]; leaked {
			t.Fatalf("%s leaked into Cube filters: %v", k, filters)
		}
	}
	// Current-state SQL sub is converted to as-of; the dated one is kept.
	if !prepareSQLWindows(subs, w, repairNow) {
		t.Fatal("expected a conversion")
	}
	if _, has := subs[1].Params[paramFrom]; has || subs[1].Params[paramWindowAsOf] != "2026-07-22" {
		t.Fatalf("current-state sub not converted: %+v", subs[1].Params)
	}
	if subs[2].Params[paramFrom] != "2025-08-01" || subs[2].Params[paramWindowAsOf] != nil {
		t.Fatalf("dated sub must keep its window: %+v", subs[2].Params)
	}
	// Zero window: nothing injected.
	fresh := []domain.SubQuestion{{ID: "x", Route: domain.RouteCube}}
	injectWindow(fresh, Window{}, "")
	if _, has := fresh[0].Params[paramFrom]; has {
		t.Fatal("zero window must inject nothing")
	}
}

// TestModelSuppliedWindowIgnored (PR #318 M2): the window the guard enforces
// is the SERVER-resolved one, never a from/to the model-influenced plan
// seeded. With a resolved window the model's from/to are overwritten and a
// draft bound to the model's own period is rejected; with NO resolved window
// the model's from/to are stripped, so an unrequested period is never
// validated (or reported) as if the leader had asked for it.
func TestModelSuppliedWindowIgnored(t *testing.T) {
	const modelWindowSQL = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2020-01-01' AND event_date < '2021-01-01' LIMIT 100"
	modelParams := func() map[string]any {
		return map[string]any{"sql": modelWindowSQL, paramFrom: "2020-01-01", paramTo: "2020-12-31", paramCompareFrom: "2019-01-01", paramCompareTo: "2019-12-31"}
	}

	// Before injection the model-seeded params would validate their own SQL:
	// that is exactly the hole.
	if err := validateModelSQL(modelWindowSQL, modelParams()); err != nil {
		t.Fatalf("precondition: model params validate model SQL: %v", err)
	}

	// 1. Resolved window ("last month" as of 2026-07-22 = June 2026): the
	//    model's from/to/compare_* are overwritten and the 2020 draft is rejected.
	w, ok := ResolveWindow("deaths last month", repairNow, nil)
	if !ok {
		t.Fatal("expected a resolved window")
	}
	subs := []domain.SubQuestion{{ID: "0", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: modelParams()}}
	injectWindow(subs, w, "")
	if subs[0].Params[paramFrom] != "2026-06-01" || subs[0].Params[paramTo] != "2026-06-30" {
		t.Fatalf("server window must overwrite model from/to: %+v", subs[0].Params)
	}
	if _, has := subs[0].Params[paramCompareFrom]; has {
		t.Fatalf("model-seeded compare window must not survive a non-comparison question: %+v", subs[0].Params)
	}
	err := validateModelSQL(modelWindowSQL, subs[0].Params)
	if err == nil || !strings.Contains(err.Error(), "2026-06-01") {
		t.Fatalf("draft bound to the model's own period must be rejected against the server window, got %v", err)
	}
	if err := validateModelSQL(juneSQL, subs[0].Params); err != nil {
		t.Fatalf("draft bound to the server window must pass: %v", err)
	}

	// 2. No resolved window: the model's from/to are stripped, the draft is
	//    not window-checked, and no "Window:" line can be produced from them.
	subs = []domain.SubQuestion{{ID: "0", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: modelParams()}}
	injectWindow(subs, Window{}, "")
	for _, k := range []string{paramFrom, paramTo, paramWindowLabel, paramCompareFrom, paramCompareTo} {
		if _, has := subs[0].Params[k]; has {
			t.Fatalf("model-supplied %s must be stripped when no window resolved: %+v", k, subs[0].Params)
		}
	}
	if _, ok := sqlWindowFromParams(subs[0].Params); ok {
		t.Fatal("no window must be derivable from stripped params")
	}
	if wr := windowResult(leadershipActor(), subs, []domain.ToolResult{{Route: domain.RouteSQL}}, Window{}); wr != nil {
		t.Fatalf("no Window line without a server-resolved window, got %+v", wr)
	}

	// 3. End to end through Ask: the model plan seeds 2020 params for a
	//    "last month" question; the guard rejects the 2020 draft against the
	//    server window, the repair is asked for June, and the answer states
	//    the server window — never 2020.
	sqlFB := &sequencedSQLFallback{result: deathsFacts()}
	plan := domain.Plan{SubQuestions: []domain.SubQuestion{{
		ID: "0", Text: "deaths", IntentClass: "mortality", Route: domain.RouteSQL, ToolName: "sql_fallback",
		Params: modelParams(),
	}}}
	prov := &repairProvider{fakeProvider: fakeProvider{plan: plan, byModel: true}, repairSQL: juneSQL}
	tel := &fakeSQLTelemetry{}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets last month", AsOf: repairNow})
	if err != nil {
		t.Fatal(err)
	}
	if prov.repairCalls != 1 || !strings.Contains(prov.lastWindow, "2026-06-01") {
		t.Fatalf("expected one repair asking for the server window: calls=%d window=%q", prov.repairCalls, prov.lastWindow)
	}
	if sqlFB.calls != 1 || sqlFB.sqls[0] != juneSQL {
		t.Fatalf("only the June draft may run: calls=%d sqls=%v", sqlFB.calls, sqlFB.sqls)
	}
	if !strings.Contains(ans.Answer, "Window: last month (01/06/2026 to 30/06/2026)") || strings.Contains(ans.Answer, "2020") {
		t.Fatalf("answer must state the server window only: %q", ans.Answer)
	}
	if len(tel.rejects) != 1 || tel.rejects[0] != "window" {
		t.Fatalf("expected one window reject: %v", tel.rejects)
	}
}

// TestCompareWindowsThreadedAndValidated (PR #318 M1): for "this quarter vs
// last" the planner drafts one sub-question per window. Both sub-questions
// carry from/to AND compare_from/compare_to; a draft bound to the PRIMARY
// window passes, a draft bound to the COMPARISON window passes too, a draft
// bound to neither is rejected with a message naming both; the repair prompt
// offers both windows; and the composed answer renders both windows.
func TestCompareWindowsThreadedAndValidated(t *testing.T) {
	w, ok := ResolveWindow("deaths this quarter vs last quarter", repairNow, nil)
	if !ok || w.Compare == nil {
		t.Fatalf("expected a two-window comparison, got %+v ok=%v", w, ok)
	}
	// as of 2026-07-22: this quarter = Q3 2026, last quarter = Q2 2026.
	const (
		q3SQL = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2026-07-01' AND event_date < '2026-10-01' LIMIT 100"
		q2SQL = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2026-04-01' AND event_date < '2026-07-01' LIMIT 100"
	)
	subs := []domain.SubQuestion{
		{ID: "0", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": q3SQL}},
		{ID: "1", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": q2SQL}},
	}
	injectWindow(subs, w, "")
	for _, s := range subs {
		if s.Params[paramFrom] != "2026-07-01" || s.Params[paramTo] != "2026-09-30" || s.Params[paramCompareFrom] != "2026-04-01" || s.Params[paramCompareTo] != "2026-06-30" {
			t.Fatalf("sub %s must carry both windows: %+v", s.ID, s.Params)
		}
	}
	if err := validateModelSQL(q3SQL, subs[0].Params); err != nil {
		t.Fatalf("primary-window draft must pass: %v", err)
	}
	if err := validateModelSQL(q2SQL, subs[1].Params); err != nil {
		t.Fatalf("comparison-window draft must pass: %v", err)
	}
	if err := validateModelSQL(juneSQL, subs[1].Params); err == nil || !strings.Contains(err.Error(), "2026-07-01") || !strings.Contains(err.Error(), "comparison arm") || !strings.Contains(err.Error(), "2026-04-01") {
		t.Fatalf("draft bound to neither window must be rejected naming both, got %v", err)
	}
	// Without a comparison window the comparison draft is still rejected.
	single := map[string]any{"sql": q2SQL, paramFrom: "2026-07-01", paramTo: "2026-09-30"}
	if err := validateModelSQL(q2SQL, single); err == nil {
		t.Fatal("a non-comparison question must not accept a different window")
	}

	// End to end: the model plans both arms; the comparison arm was previously
	// rejected and burned the repair. Now both run, no repair, and the answer
	// states both windows once.
	sqlFB := &sequencedSQLFallback{result: deathsFacts()}
	plan := domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", Text: "deaths this quarter", IntentClass: "mortality", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": q3SQL}},
		{ID: "1", Text: "deaths last quarter", IntentClass: "mortality", Route: domain.RouteSQL, ToolName: "sql_fallback", Params: map[string]any{"sql": q2SQL}},
	}}
	prov := &repairProvider{fakeProvider: fakeProvider{plan: plan, byModel: true}, repairSQL: q2SQL}
	tel := &fakeSQLTelemetry{}
	a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: tel})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets this quarter vs last quarter", AsOf: repairNow})
	if err != nil {
		t.Fatal(err)
	}
	if prov.repairCalls != 0 || len(tel.rejects) != 0 || sqlFB.calls != 2 {
		t.Fatalf("both arms must run without repair: repair=%d rejects=%v exec=%d", prov.repairCalls, tel.rejects, sqlFB.calls)
	}
	if ans.Mode != domain.ModePlanned {
		t.Fatalf("expected a planned answer, got %s: %q", ans.Mode, ans.Answer)
	}
	if !strings.Contains(ans.Answer, "01/07/2026 to 30/09/2026") || !strings.Contains(ans.Answer, " vs ") || !strings.Contains(ans.Answer, "01/04/2026 to 30/06/2026") {
		t.Fatalf("answer must render both windows: %q", ans.Answer)
	}
	if strings.Count(ans.Answer, "Window:") != 1 {
		t.Fatalf("the period must be stated exactly once: %q", ans.Answer)
	}

	// The repair prompt for a rejected comparison arm offers both windows.
	sqlFB = &sequencedSQLFallback{result: deathsFacts()}
	plan.SubQuestions[1].Params = map[string]any{"sql": juneSQL}
	prov = &repairProvider{fakeProvider: fakeProvider{plan: plan, byModel: true}, repairSQL: q2SQL}
	a = NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: &fakeSQLTelemetry{}})
	if _, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "how many widgets this quarter vs last quarter", AsOf: repairNow}); err != nil {
		t.Fatal(err)
	}
	if prov.repairCalls != 1 || !strings.Contains(prov.lastWindow, "2026-07-01") || !strings.Contains(prov.lastWindow, "2026-04-01") || !strings.Contains(prov.lastWindow, "comparison window") {
		t.Fatalf("repair must offer both windows: calls=%d window=%q", prov.repairCalls, prov.lastWindow)
	}
}

func TestRejectClassAndFailureKind(t *testing.T) {
	cases := map[string]error{
		"banned_keyword":       sqlguard.Validate(badSQL),
		"window_current_state": sqlguard.ErrWindowOnCurrentStateView,
		"tenant_binding":       sqlguard.ErrTenantBinding,
		"join":                 sqlguard.Validate("SELECT 1 FROM ceo_ai.a JOIN ceo_ai.b ON true WHERE tenant_id = 'x' LIMIT 1"),
		"limit":                sqlguard.Validate("SELECT 1 FROM ceo_ai.a WHERE tenant_id = 'x'"),
		"":                     errors.New("dial tcp: connection refused"),
	}
	for want, err := range cases {
		if got := rejectClass(err); got != want {
			t.Errorf("rejectClass(%v) = %q want %q", err, got, want)
		}
	}
	if sqlFailureKind(&pgconn.PgError{Code: "57014"}) != "pg" || sqlFailureKind(nil) != "" || sqlFailureKind(errors.New("x")) != "" {
		t.Fatal("sqlFailureKind classification")
	}
	// Wrapped errors (the adapter wraps with "sqlguard fallback: %w") still classify.
	wrapped := errors.Join(errors.New("sqlguard fallback"), sqlguard.ErrWindowOnCurrentStateView)
	if rejectClass(wrapped) != "window_current_state" {
		t.Fatal("wrapped sentinel must classify")
	}
}

// TestWindowLineReflectsExecutedArms (PR #318 R2-2): the "Window:" line
// describes the windows the executed arms actually bound, never the
// resolved question. "Compare August with September" resolves August with a
// September comparison; (a) a plan whose two arms are BOTH drafted for August
// passes the either-window rule twice but answers no comparison, so the line
// names August alone and says so; (b) a plan with ONE arm bound to August
// likewise; (c) one arm per window still renders "A vs B" (regression guard
// for the happy path); (d) a single arm bound to the COMPARISON window names
// September alone.
func TestWindowLineReflectsExecutedArms(t *testing.T) {
	const question = "Compare August with September widgets"
	// As of mid-October 2026 both months are the most recent past ones.
	armsNow := time.Date(2026, 10, 15, 9, 0, 0, 0, biztime.DefaultLocation())
	w, ok := ResolveWindow(question, armsNow, nil)
	if !ok || w.Compare == nil {
		t.Fatalf("expected a two-window comparison, got %+v ok=%v", w, ok)
	}
	const (
		augSQL = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2026-08-01' AND event_date < '2026-09-01' LIMIT 100"
		sepSQL = "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base WHERE tenant_id = 't1' AND event_date >= '2026-09-01' AND event_date < '2026-10-01' LIMIT 100"
		aug    = "august (01/08/2026 to 31/08/2026)"
		sep    = "september (01/09/2026 to 30/09/2026)"
	)
	ask := func(t *testing.T, sqls ...string) (domain.Answer, *repairProvider, *sequencedSQLFallback) {
		t.Helper()
		plan := domain.Plan{}
		for i, sql := range sqls {
			plan.SubQuestions = append(plan.SubQuestions, domain.SubQuestion{
				ID: fmt.Sprint(i), Text: "deaths", IntentClass: "mortality", Route: domain.RouteSQL, ToolName: "sql_fallback",
				Params: map[string]any{"sql": sql},
			})
		}
		sqlFB := &sequencedSQLFallback{result: deathsFacts()}
		prov := &repairProvider{fakeProvider: fakeProvider{plan: plan, byModel: true}, repairSQL: augSQL}
		a := NewAssistant(Config{}, Deps{Provider: prov, Registry: NewRegistry(nil, nil, sqlFB), Telemetry: &fakeSQLTelemetry{}})
		ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: question, AsOf: armsNow})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(ans.Answer, "Window:") != 1 {
			t.Fatalf("the period must be stated exactly once: %q", ans.Answer)
		}
		return ans, prov, sqlFB
	}

	// (a) degenerate: both arms bound to August.
	ans, prov, sqlFB := ask(t, augSQL, augSQL)
	if prov.repairCalls != 0 || sqlFB.calls != 2 {
		t.Fatalf("both August arms pass the either-window rule and run: repair=%d exec=%d", prov.repairCalls, sqlFB.calls)
	}
	if !strings.Contains(ans.Answer, "Window: "+aug+" (comparison with "+sep+" could not be answered)") || strings.Contains(ans.Answer, " vs ") {
		t.Fatalf("two August arms must not be labelled as a comparison: %q", ans.Answer)
	}

	// (b) single arm bound to August.
	ans, _, sqlFB = ask(t, augSQL)
	if sqlFB.calls != 1 {
		t.Fatalf("one arm runs: exec=%d", sqlFB.calls)
	}
	if !strings.Contains(ans.Answer, "Window: "+aug+" (comparison with "+sep+" could not be answered)") || strings.Contains(ans.Answer, " vs ") {
		t.Fatalf("a single August arm must not be labelled as a comparison: %q", ans.Answer)
	}

	// (c) happy path: one arm per window renders both.
	ans, _, sqlFB = ask(t, augSQL, sepSQL)
	if sqlFB.calls != 2 || !strings.Contains(ans.Answer, "Window: "+aug+" vs "+sep) || strings.Contains(ans.Answer, "could not be answered") {
		t.Fatalf("one arm per window must render both: exec=%d %q", sqlFB.calls, ans.Answer)
	}

	// (d) single arm bound to the comparison window names September alone.
	ans, _, _ = ask(t, sepSQL)
	if !strings.Contains(ans.Answer, "Window: "+sep+" (comparison with "+aug+" could not be answered)") || strings.Contains(ans.Answer, " vs ") {
		t.Fatalf("a lone September arm must name September only: %q", ans.Answer)
	}

	// The pure helper agrees with the end-to-end lines.
	if got := describeExecutedWindows(w, true, true); got != aug+" vs "+sep {
		t.Fatalf("both bound: %q", got)
	}
	if got := describeExecutedWindows(w, false, false); !strings.HasPrefix(got, aug+" (comparison with") {
		t.Fatalf("neither reported must not claim a comparison: %q", got)
	}
	if got := describeExecutedWindows(Window{From: w.From, To: w.To, Label: w.Label}, true, false); got != aug {
		t.Fatalf("non-comparison window renders as before: %q", got)
	}
}
