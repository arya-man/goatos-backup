package bootstrap

import (
	"context"
	"strings"
	"testing"

	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
)

type stubSalesOverview struct{ overview salesdomain.Overview }

func (s stubSalesOverview) GetOverview(context.Context, string, string) (salesdomain.Overview, error) {
	return s.overview, nil
}

// THE SALES READ LABELS ITS FACTS WITH THE MONTH IT READ.
//
// These labels used to say "this month" whatever month was bound, and the
// composer repeated the phrase: "how much revenue did we make in august"
// answered "This month … Sales revenue was 291600" — August's figure under
// September's name, with September's own revenue 5.7x higher.
func TestTheMonthlySalesReadLabelsTheMonthItRead(t *testing.T) {
	svc := stubSalesOverview{overview: salesdomain.Overview{
		Monthly: []salesdomain.MonthlyRow{
			{Month: "2026-08", GoatCount: 16, GoatRevenue: 291600},
			{Month: "2026-09", GoatCount: 91, GoatRevenue: 1669049},
		},
	}}
	read := buildSalesOverviewReader(svc)

	facts, err := read(context.Background(), "t1", map[string]any{"month": "2026-08"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(facts) != 4 {
		t.Fatalf("want the four monthly facts, got %d", len(facts))
	}
	for _, f := range facts {
		if strings.Contains(strings.ToLower(f.Label), "this month") {
			t.Errorf("fact %q calls a bound month \"this month\"", f.Label)
		}
		if !strings.Contains(f.Label, "2026-08") {
			t.Errorf("fact %q does not name the month it read", f.Label)
		}
		if f.Scope != "2026-08" {
			t.Errorf("fact %q scope = %q, want the month it matched the row on", f.Label, f.Scope)
		}
	}
	var revenue string
	for _, f := range facts {
		if strings.Contains(strings.ToLower(f.Label), "sales revenue") {
			revenue = f.Value
		}
	}
	if revenue != "291600" {
		t.Errorf("August revenue = %q, want 291600 (September is 1669049)", revenue)
	}

	// A month with no closed deals is still labelled with the month it looked
	// for, not with "this month".
	facts, err = read(context.Background(), "t1", map[string]any{"month": "2026-07"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, f := range facts {
		if strings.Contains(strings.ToLower(f.Label), "this month") || !strings.Contains(f.Label, "2026-07") {
			t.Errorf("an empty month's fact %q must still name the month it read", f.Label)
		}
	}

	// The all-time summary binds no month and must gain none.
	facts, err = read(context.Background(), "t1", map[string]any{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, f := range facts {
		if strings.Contains(strings.ToLower(f.Label), "month") || f.Scope == "2026-08" {
			t.Errorf("the all-time summary claimed a period: %q / %q", f.Label, f.Scope)
		}
	}
}
