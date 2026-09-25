package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// chartActor is the acting tenant every chart test composes for. Facts are
// stamped with the same tenant (tf) — the typed Fact.TenantID gate rejects
// anything else, which TestComposerRejectsMixedTenantFacts covers.
var chartActor = domain.Actor{TenantID: "tenant-a", UserID: "ceo-a"}

// tf builds a fact already stamped for chartActor, the way an executor/reader
// stamps it from the session actor.
func tf(label, value, scope string) domain.Fact {
	return domain.Fact{TenantID: chartActor.TenantID, Label: label, Value: value, Scope: scope}
}

func mustChart(t *testing.T, question string, results []domain.ToolResult) *domain.Chart {
	t.Helper()
	chart, err := buildChart(chartActor, question, results)
	if err != nil {
		t.Fatalf("buildChart(%q): unexpected error: %v", question, err)
	}
	return chart
}

// A "plot vaccination overdue by shed" question over a per-shed Cube result
// must emit a bar chart whose x/data match the tool rows verbatim.
func TestBuildChartPlotByShed(t *testing.T) {
	results := []domain.ToolResult{{
		Route:    domain.RouteCube,
		ToolName: "vaccination_overdue",
		Surface:  "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			tf("Vaccination overdue", "12", "Shed A"),
			tf("Vaccination overdue", "7", "Shed B"),
			tf("Vaccination overdue", "3", "Shed C"),
		},
	}}

	chart := mustChart(t, "plot vaccination overdue by shed", results)
	if chart == nil {
		t.Fatal("expected a chart for a plot-by-shed question, got nil")
	}
	if chart.Type != "bar" {
		t.Fatalf("expected bar chart, got %q", chart.Type)
	}
	wantX := []string{"Shed A", "Shed B", "Shed C"}
	if len(chart.X) != len(wantX) {
		t.Fatalf("x len = %d, want %d", len(chart.X), len(wantX))
	}
	for i, x := range wantX {
		if chart.X[i] != x {
			t.Fatalf("x[%d] = %q, want %q", i, chart.X[i], x)
		}
	}
	if len(chart.Series) != 1 {
		t.Fatalf("series len = %d, want 1", len(chart.Series))
	}
	wantData := []float64{12, 7, 3}
	got := chart.Series[0].Data
	if len(got) != len(wantData) {
		t.Fatalf("data len = %d, want %d", len(got), len(wantData))
	}
	for i, d := range wantData {
		if got[i] != d {
			t.Fatalf("data[%d] = %v, want %v (must match tool rows verbatim)", i, got[i], d)
		}
	}
	if chart.Series[0].Name != "Vaccination overdue" {
		t.Fatalf("series name = %q, want %q", chart.Series[0].Name, "Vaccination overdue")
	}
}

// A plain single-value count question must NOT produce a chart.
func TestBuildChartNoChartForPlainCount(t *testing.T) {
	results := []domain.ToolResult{{
		Route:    domain.RouteCube,
		ToolName: "vaccination_overdue",
		Surface:  "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			tf("Vaccination overdue", "22", ""),
		},
	}}

	if chart := mustChart(t, "how many vaccinations are overdue?", results); chart != nil {
		t.Fatalf("expected no chart for a plain count, got %+v", chart)
	}
}

// A dimensioned series alone (no explicit plot ask) still charts, since the
// result is a real per-shed breakdown.
func TestBuildChartDimensionedSeriesWithoutPlotWord(t *testing.T) {
	results := []domain.ToolResult{{
		Route:   domain.RouteCube,
		Surface: "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			tf("Overdue", "5", "Shed A"),
			tf("Overdue", "9", "Shed B"),
		},
	}}
	if chart := mustChart(t, "overdue vaccinations for each shed", results); chart == nil {
		t.Fatal("expected a chart for a dimensioned per-shed series")
	}
}

// A trend question yields a line chart.
func TestBuildChartTrendIsLine(t *testing.T) {
	results := []domain.ToolResult{{
		Route:   domain.RouteCube,
		Surface: "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			tf("Overdue", "5", "Jan"),
			tf("Overdue", "9", "Feb"),
			tf("Overdue", "4", "Mar"),
		},
	}}
	chart := mustChart(t, "show the overdue trend over time", results)
	if chart == nil || chart.Type != "line" {
		t.Fatalf("expected a line chart for a trend question, got %+v", chart)
	}
}

// Non-numeric facts are skipped; a chart still forms from the numeric part.
func TestBuildChartSkipsNonNumeric(t *testing.T) {
	results := []domain.ToolResult{{
		Route:   domain.RouteSQL,
		Surface: "SQL fallback",
		Facts: []domain.Fact{
			tf("shed", "Shed A", ""),
			tf("Shed A", "12", ""),
			tf("Shed B", "8", ""),
		},
	}}
	chart := mustChart(t, "plot overdue by shed", results)
	if chart == nil {
		t.Fatal("expected a chart from the numeric facts")
	}
	if len(chart.Series[0].Data) != 2 {
		t.Fatalf("expected 2 numeric points, got %d", len(chart.Series[0].Data))
	}
}

// TestComposerRejectsMixedTenantFacts is the typed Fact.TenantID gate (plan v3
// D0 "Chart / facts"): a fact set with more than one TenantID, a TenantID that
// is not the actor's, or an unstamped fact is rejected by composeFor, by
// buildChart and by the cache gate — none of them ever renders a figure.
func TestComposerRejectsMixedTenantFacts(t *testing.T) {
	cases := map[string][]domain.Fact{
		"mixed tenants in one result": {
			tf("Overdue", "5", "Shed A"),
			{TenantID: "tenant-b", Label: "Overdue", Value: "9", Scope: "Shed B"},
		},
		"all facts foreign": {
			{TenantID: "tenant-b", Label: "Overdue", Value: "5", Scope: "Shed A"},
			{TenantID: "tenant-b", Label: "Overdue", Value: "9", Scope: "Shed B"},
		},
		"unstamped fact": {
			tf("Overdue", "5", "Shed A"),
			{Label: "Overdue", Value: "9", Scope: "Shed B"},
		},
	}
	for name, facts := range cases {
		t.Run(name, func(t *testing.T) {
			results := []domain.ToolResult{{Route: domain.RouteCube, Surface: "Cube · vaccination_overdue", Facts: facts}}

			if err := validateFactTenants(chartActor, results); !errors.Is(err, ErrForeignTenantFacts) {
				t.Fatalf("validateFactTenants: want ErrForeignTenantFacts, got %v", err)
			}
			body, cites, ground, err := composer{}.composeFor(chartActor, results)
			if !errors.Is(err, ErrForeignTenantFacts) {
				t.Fatalf("composeFor: want ErrForeignTenantFacts, got %v", err)
			}
			if body != "" || cites != nil || ground != nil {
				t.Fatalf("composeFor must render nothing on rejection, got body=%q cites=%v ground=%v", body, cites, ground)
			}
			chart, err := buildChart(chartActor, "plot overdue by shed", results)
			if !errors.Is(err, ErrForeignTenantFacts) || chart != nil {
				t.Fatalf("buildChart: want (nil, ErrForeignTenantFacts), got (%+v, %v)", chart, err)
			}
		})
	}

	// Two results, both the actor's, is fine; two results from two tenants is not
	// (the gate is over the whole fact SET, not per result).
	ok := []domain.ToolResult{
		{Route: domain.RouteCube, Facts: []domain.Fact{tf("Overdue", "5", "Shed A")}},
		{Route: domain.RouteAPI, Facts: []domain.Fact{tf("Goats", "40", "")}},
	}
	if err := validateFactTenants(chartActor, ok); err != nil {
		t.Fatalf("same-tenant set must pass, got %v", err)
	}
	bad := append(ok, domain.ToolResult{Route: domain.RouteSQL, Facts: []domain.Fact{{TenantID: "tenant-b", Label: "Goats", Value: "99"}}})
	if err := validateFactTenants(chartActor, bad); !errors.Is(err, ErrForeignTenantFacts) {
		t.Fatalf("cross-result foreign tenant must fail, got %v", err)
	}
	// An errored result carries no facts to render and does not trip the gate.
	errored := append(ok, domain.ToolResult{Route: domain.RouteSQL, Err: errors.New("boom"), Facts: []domain.Fact{{TenantID: "tenant-b", Label: "x", Value: "1"}}})
	if err := validateFactTenants(chartActor, errored); err != nil {
		t.Fatalf("errored result must be skipped, got %v", err)
	}
	// An actor without a tenant can never compose.
	if err := validateFactTenants(domain.Actor{}, ok); !errors.Is(err, ErrForeignTenantFacts) {
		t.Fatalf("empty actor tenant must fail, got %v", err)
	}
	if err := validateFactTenants(chartActor, bad); err == nil || !strings.Contains(err.Error(), "another tenant") {
		t.Fatalf("error should say the fact belongs to another tenant, got %v", err)
	}
}

// TestAskRefusesForeignTenantFacts drives the gate through the live Ask path:
// a metric result that carries another tenant's rows (a bypassed lower layer)
// must become a refusal — no answer body with the figure, no chart, no cache
// entry — instead of being composed for the caller.
func TestAskRefusesForeignTenantFacts(t *testing.T) {
	metrics := &fakeMetrics{
		specs: []ports.MetricSpec{{Name: "vaccination_overdue", Status: domain.MetricApproved}},
		result: domain.ToolResult{Surface: "Cube · vaccination_overdue", Facts: []domain.Fact{
			{TenantID: "t1", Label: "overdue", Value: "42", Scope: "Shed A"},
			{TenantID: "t2", Label: "overdue", Value: "77", Scope: "Shed B"},
		}},
	}
	reg := NewRegistry(metrics, nil, nil)
	prov := &fakeProvider{byModel: true, plan: domain.Plan{SubQuestions: []domain.SubQuestion{
		{ID: "0", ToolName: "vaccination_overdue", Route: domain.RouteCube},
	}}}
	cache := &countingCache{m: map[string]domain.Answer{}}
	audit := &fakeAudit{}
	tel := &fakeSQLTelemetry{}
	a := NewAssistant(Config{}, Deps{Parks: testParks{}, Provider: prov, Registry: reg, Metrics: metrics, Cache: cache, Audit: audit, Telemetry: tel})
	ans, err := a.Ask(context.Background(), domain.Question{Actor: leadershipActor(), Text: "plot overdue by shed"})
	if err != nil {
		t.Fatal(err)
	}
	if ans.Mode != domain.ModeRefused {
		t.Fatalf("mixed-tenant facts must refuse, got mode=%q answer=%q", ans.Mode, ans.Answer)
	}
	// D0 "each attempt audited": the gate rejection is observable — an audit
	// row in ModeRefused whose trace carries the tenant_gate step, and one
	// ceoai_tenant_gate_reject_total{reason} increment.
	if audit.rec == nil || audit.rec.Mode != domain.ModeRefused {
		t.Fatalf("tenant gate rejection must be audited as refused, got %+v", audit.rec)
	}
	gateStep := false
	for _, st := range audit.rec.Steps {
		if st.SubQuestionID == "tenant_gate" && strings.Contains(st.Err, "another tenant") {
			gateStep = true
		}
	}
	if !gateStep || audit.rec.Review.ScopeSafe || len(audit.rec.Review.FailReasons) == 0 {
		t.Fatalf("audit must carry the tenant_gate step and a scope-unsafe verdict, got steps=%+v review=%+v", audit.rec.Steps, audit.rec.Review)
	}
	if len(tel.tenantGates) != 1 || tel.tenantGates[0] != "foreign" {
		t.Fatalf("expected one tenant-gate counter increment with reason=foreign, got %v", tel.tenantGates)
	}
	if strings.Contains(ans.Answer, "42") || strings.Contains(ans.Answer, "77") {
		t.Fatalf("refusal must not leak a figure: %q", ans.Answer)
	}
	if ans.Chart != nil {
		t.Fatalf("refusal must carry no chart, got %+v", ans.Chart)
	}
	if len(cache.m) != 0 {
		t.Fatalf("a rejected fact set must never be cached, got %d entries", len(cache.m))
	}
}
