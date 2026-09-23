package readtools

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
)

// A MONTH THE READER CANNOT BIND IS WORSE THAN NO MONTH AT ALL.
//
// Measured live on this branch, after the bound-window fix landed: "how much
// revenue did we make in august" planned
// {month:"August", from:"2026-08-01", to:"2026-08-31"} and answered "Across all
// recorded sales, 0 animals were sold: 0 goats and 0 sheep. Sales revenue was
// 0" — against an August that made 291,600. The month arrived in ENGLISH, the
// read matches its rows on "2006-01", nothing matched, and the zeros that came
// back carried no month the composer could read, so the sentence lost the
// period too and called them "all recorded sales".
//
// The bound window said 2026-08 in the very same params and was never read,
// because the window was only consulted when `month` was ABSENT.

func TestTheBoundWindowOutranksTheWordTheModelTyped(t *testing.T) {
	params := map[string]any{"from": "2026-08-01", "to": "2026-08-31", "as_of": "2026-09-23"}
	bound := monthFromBoundWindow(params)
	if bound != "2026-08" {
		t.Fatalf("precondition: bound window = %q, want 2026-08", bound)
	}
	for _, raw := range []string{"August", "august", "Aug", "current", "this_month", "2026-09"} {
		got := normalizeMonthParam(raw, bound, params)
		want := "2026-08"
		if raw == "2026-09" {
			// An explicit, bindable month the model named is already the shape
			// the read matches on and is left exactly as it arrived.
			want = "2026-09"
		}
		if got != want {
			t.Errorf("normalizeMonthParam(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAnEnglishMonthResolvesFromTheRequestWhenNoWindowWasBound(t *testing.T) {
	params := map[string]any{"as_of": "2026-09-23"}
	for _, tc := range []struct{ raw, want string }{
		{"August", "2026-08"},
		{"aug", "2026-08"},
		{"August 2025", "2025-08"},
		{"jan", "2026-01"},
		{"December 2027", "2027-12"},
		{"current", "current"},
		{"this_month", "this_month"},
		{"2026-07", "2026-07"},
	} {
		if got := normalizeMonthParam(tc.raw, "", params); got != tc.want {
			t.Errorf("normalizeMonthParam(%q, no window) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestAnUnbindableMonthIsHandedOnUnchangedRatherThanInvented: this function
// never guesses a period. A word it cannot resolve, with no window to fall back
// on, is passed through so the READ refuses it — which is the honest outcome,
// because the alternative is a zero row filed under a month nobody asked for.
func TestAnUnbindableMonthIsHandedOnUnchangedRatherThanInvented(t *testing.T) {
	params := map[string]any{"as_of": "2026-09-23"}
	for _, raw := range []string{"last quarter", "harvest", "Q3", ""} {
		if got := normalizeMonthParam(raw, "", params); got != raw {
			t.Errorf("normalizeMonthParam(%q) = %q; an unbindable period must be passed through, never invented", raw, got)
		}
	}
	// With no as-of year in the request, a bare month name resolves to nothing
	// rather than to time.Now()'s year.
	if got := normalizeMonthParam("August", "", map[string]any{}); got != "August" {
		t.Errorf("a month with no year in the request must not be resolved from the wall clock; got %q", got)
	}
}

// TestTheExecutorBindsTheWindowOverTheEnglishMonth is the defect at the
// executor, end to end.
func TestTheExecutorBindsTheWindowOverTheEnglishMonth(t *testing.T) {
	var seen map[string]any
	exec := &salesOverviewExecutor{
		salesDataReader: func(_ context.Context, _ string, params map[string]any) ([]domain.Fact, error) {
			seen = params
			return []domain.Fact{{Label: "Sales revenue in 2026-08", Value: "291600", Scope: "2026-08"}}, nil
		},
	}
	_, err := exec.Execute(context.Background(), domain.Actor{TenantID: "t1"}, domain.SubQuestion{
		Text: "total revenue for August",
		Params: map[string]any{
			"month": "August",
			"from":  "2026-08-01",
			"to":    "2026-08-31",
			"as_of": "2026-09-23",
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := seen["month"]; got != "2026-08" {
		t.Errorf("the read was asked for month %v; the server had resolved 2026-08", got)
	}
	// The caller's own map must not be mutated — the executor clones.
	if seen["from"] != "2026-08-01" {
		t.Errorf("the bound window must survive the rewrite: %v", seen)
	}
}
