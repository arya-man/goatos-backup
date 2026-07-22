package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// A "plot vaccination overdue by park" question over a per-park Cube result
// must emit a bar chart whose x/data match the tool rows verbatim.
func TestBuildChartPlotByPark(t *testing.T) {
	results := []domain.ToolResult{{
		Route:    domain.RouteCube,
		ToolName: "vaccination_overdue",
		Surface:  "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			{Label: "Vaccination overdue", Value: "12", Scope: "Castro 1"},
			{Label: "Vaccination overdue", Value: "7", Scope: "Gandhi 2"},
			{Label: "Vaccination overdue", Value: "3", Scope: "Nehru 3"},
		},
	}}

	chart := buildChart("plot vaccination overdue by park", results)
	if chart == nil {
		t.Fatal("expected a chart for a plot-by-park question, got nil")
	}
	if chart.Type != "bar" {
		t.Fatalf("expected bar chart, got %q", chart.Type)
	}
	wantX := []string{"Castro 1", "Gandhi 2", "Nehru 3"}
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
			{Label: "Vaccination overdue", Value: "22"},
		},
	}}

	if chart := buildChart("how many vaccinations are overdue?", results); chart != nil {
		t.Fatalf("expected no chart for a plain count, got %+v", chart)
	}
}

// A dimensioned series alone (no explicit plot ask) still charts, since the
// result is a real per-park breakdown.
func TestBuildChartDimensionedSeriesWithoutPlotWord(t *testing.T) {
	results := []domain.ToolResult{{
		Route:   domain.RouteCube,
		Surface: "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			{Label: "Overdue", Value: "5", Scope: "Castro 1"},
			{Label: "Overdue", Value: "9", Scope: "Gandhi 2"},
		},
	}}
	if chart := buildChart("overdue vaccinations for each park", results); chart == nil {
		t.Fatal("expected a chart for a dimensioned per-park series")
	}
}

// A trend question yields a line chart.
func TestBuildChartTrendIsLine(t *testing.T) {
	results := []domain.ToolResult{{
		Route:   domain.RouteCube,
		Surface: "Cube · vaccination_overdue",
		Facts: []domain.Fact{
			{Label: "Overdue", Value: "5", Scope: "Jan"},
			{Label: "Overdue", Value: "9", Scope: "Feb"},
			{Label: "Overdue", Value: "4", Scope: "Mar"},
		},
	}}
	chart := buildChart("show the overdue trend over time", results)
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
			{Label: "park", Value: "Castro 1"},
			{Label: "Castro 1", Value: "12"},
			{Label: "Gandhi 2", Value: "8"},
		},
	}}
	chart := buildChart("plot overdue by park", results)
	if chart == nil {
		t.Fatal("expected a chart from the numeric facts")
	}
	if len(chart.Series[0].Data) != 2 {
		t.Fatalf("expected 2 numeric points, got %d", len(chart.Series[0].Data))
	}
}
