package readtools

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// THE MONTH THE SALES READ BINDS COMES FROM THE WINDOW THE SERVER RESOLVED,
// not from whether the question contains the word "month".
//
// It used to be the words: any text containing "month" got month="current", so
// "how much money did we make last month" asked the reader for THIS month's
// row. The server already resolves the period and threads it into every
// sub-question as from/to (app.injectWindow) — the same window the SQL route
// binds and states as "Window: last month (01/08/2026 to 31/08/2026)". Reading
// it here is what makes the two routes agree about which month they mean.

func TestTheSalesMonthComesFromTheBoundWindow(t *testing.T) {
	for _, tc := range []struct {
		name      string
		params    map[string]any
		want      string
		wantFound bool
	}{
		{"a whole calendar month binds it", map[string]any{"from": "2026-08-01", "to": "2026-08-31"}, "2026-08", true},
		{"february is its own length", map[string]any{"from": "2027-02-01", "to": "2027-02-28"}, "2027-02", true},
		{"a leap february too", map[string]any{"from": "2028-02-01", "to": "2028-02-29"}, "2028-02", true},
		{"a partial month is not a month", map[string]any{"from": "2026-08-01", "to": "2026-08-15"}, "", false},
		{"a window that does not start on the first is not a month", map[string]any{"from": "2026-08-02", "to": "2026-08-31"}, "", false},
		{"a 30-day rolling window spanning two months is not a month", map[string]any{"from": "2026-07-15", "to": "2026-08-14"}, "", false},
		{"no window at all", map[string]any{}, "", false},
		{"a malformed window binds nothing", map[string]any{"from": "august", "to": "2026-08-31"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := monthFromBoundWindow(tc.params)
			if got != tc.want {
				t.Fatalf("monthFromBoundWindow(%v) = %q, want %q", tc.params, got, tc.want)
			}
			if (got != "") != tc.wantFound {
				t.Fatalf("found = %v, want %v", got != "", tc.wantFound)
			}
		})
	}
}

// TestLastMonthNeverBindsThisMonth is the defect itself, at the executor.
func TestLastMonthNeverBindsThisMonth(t *testing.T) {
	var seen map[string]any
	exec := &salesOverviewExecutor{
		salesDataReader: func(_ context.Context, _ string, params map[string]any) ([]domain.Fact, error) {
			seen = params
			return []domain.Fact{{Label: "Sales revenue in 2026-08", Value: "291600", Scope: "2026-08"}}, nil
		},
	}
	actor := domain.Actor{TenantID: "t1"}

	// "last month", with August resolved by the server.
	_, err := exec.Execute(context.Background(), actor, domain.SubQuestion{
		Text:     "how much money did we make last month",
		ToolName: "sales_overview",
		Params:   map[string]any{"from": "2026-08-01", "to": "2026-08-31"},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if seen["month"] != "2026-08" {
		t.Errorf("a \"last month\" question bound month=%v; the server resolved August", seen["month"])
	}

	// A month question with no resolved window still falls back to the
	// calendar month the question is asked in — the one branch that may.
	seen = nil
	if _, err := exec.Execute(context.Background(), actor, domain.SubQuestion{
		Text:     "how many animals were sold this month",
		ToolName: "sales_overview",
		Params:   map[string]any{},
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if seen["month"] != "current" {
		t.Errorf("with no resolved window a month question falls back to current, got %v", seen["month"])
	}

	// A question that is not about a month binds no month at all.
	seen = nil
	if _, err := exec.Execute(context.Background(), actor, domain.SubQuestion{
		Text:     "who are our biggest buyers",
		ToolName: "sales_overview",
		Params:   map[string]any{},
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, ok := seen["month"]; ok {
		t.Errorf("a question with no period bound month=%v", seen["month"])
	}

	// A month the planner set explicitly is never overwritten.
	seen = nil
	if _, err := exec.Execute(context.Background(), actor, domain.SubQuestion{
		Text:     "how much revenue did we make in august",
		ToolName: "sales_overview",
		Params:   map[string]any{"month": "2026-08", "from": "2026-09-01", "to": "2026-09-30"},
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if seen["month"] != "2026-08" {
		t.Errorf("an explicit month was overwritten with %v", seen["month"])
	}
}
