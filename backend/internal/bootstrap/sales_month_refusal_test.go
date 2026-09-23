package bootstrap

import (
	"context"
	"strings"
	"testing"

	salesdomain "github.com/vgoats/goatos/backend/internal/sales/domain"
)

// A PERIOD THIS READ CANNOT BIND IS REFUSED, NOT ZEROED.
//
// `month` is matched against row.Month, which is "2006-01". When it arrived as
// anything else — live, the planner sent the English "August" — no row could
// match and the loop fell through to a row of hard zeros. Those zeros then
// reached a composer that could find no month in them and introduced them as
// "Across all recorded sales, 0 animals were sold … Sales revenue was 0", for
// an August that made 291,600.
//
// A fabricated zero under the wrong period is the worst answer this read can
// give, and it is indistinguishable from a real one. An error is not.
func TestAMonthThisReadCannotBindIsRefusedRatherThanZeroed(t *testing.T) {
	svc := stubSalesOverview{overview: salesdomain.Overview{
		Monthly: []salesdomain.MonthlyRow{
			{Month: "2026-08", GoatCount: 16, GoatRevenue: 291600},
		},
	}}
	read := buildSalesOverviewReader(svc)

	for _, bad := range []string{"August", "aug", "2026-13", "last month", "08-2026"} {
		facts, err := read(context.Background(), "t1", map[string]any{"month": bad})
		if err == nil {
			t.Errorf("month %q was accepted and produced %d facts; a period the read cannot bind must be refused", bad, len(facts))
			continue
		}
		if !strings.Contains(err.Error(), bad) {
			t.Errorf("the refusal for %q must name the period it could not bind: %v", bad, err)
		}
		if facts != nil {
			t.Errorf("a refused period must return no facts, got %v", facts)
		}
	}
}

// TestAMonthWithNoSalesStillReportsZeroHonestly: refusing an UNBINDABLE month
// must not start refusing a bindable month that genuinely has no sales. Zero is
// the true answer there, and it arrives carrying its own period.
func TestAMonthWithNoSalesStillReportsZeroHonestly(t *testing.T) {
	svc := stubSalesOverview{overview: salesdomain.Overview{
		Monthly: []salesdomain.MonthlyRow{
			{Month: "2026-08", GoatCount: 16, GoatRevenue: 291600},
		},
	}}
	read := buildSalesOverviewReader(svc)

	facts, err := read(context.Background(), "t1", map[string]any{"month": "2026-05"})
	if err != nil {
		t.Fatalf("a bindable month with no rows is an answerable question: %v", err)
	}
	if len(facts) != 4 {
		t.Fatalf("want the four monthly facts, got %d", len(facts))
	}
	for _, f := range facts {
		if f.Scope != "2026-05" {
			t.Errorf("fact %q carries scope %q; the zero must name the month it covers", f.Label, f.Scope)
		}
		if f.Value != "0" {
			t.Errorf("fact %q = %q, want 0", f.Label, f.Value)
		}
	}
}

// TestTheAllTimeReadIsUnaffected: with no month asked for at all, the read
// still returns its all-time summary and is not touched by the refusal.
func TestTheAllTimeReadIsUnaffected(t *testing.T) {
	svc := stubSalesOverview{overview: salesdomain.Overview{
		Summary: salesdomain.Summary{Deals: 3, Animals: 78, Goats: 78, Revenue: 1453010},
	}}
	read := buildSalesOverviewReader(svc)

	facts, err := read(context.Background(), "t1", map[string]any{})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var revenue string
	for _, f := range facts {
		if f.Label == "Sales revenue" {
			revenue = f.Value
		}
	}
	if revenue != "1453010" {
		t.Errorf("all-time revenue = %q, want 1453010", revenue)
	}
}
