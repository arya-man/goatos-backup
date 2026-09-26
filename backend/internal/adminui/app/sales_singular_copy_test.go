package app

import (
	"context"
	"testing"
)

// TestSalesCountCopyHasASingular (2026-09-26, Sales E2E): the Sales pages printed "1 lines",
// "1 live animals", "about every 1 days" and "1 days ago" because each count had one plural noun.
// Every page that serves a plural count phrase must also serve its singular, and the singular must
// read as one thing; the renderer picks it when the count is exactly 1.
func TestSalesCountCopyHasASingular(t *testing.T) {
	resp := NewService(fakeFamilies{}).Bootstrap(context.Background(), BootstrapInput{
		TenantID: "00000000-0000-4000-8000-000000000001",
		ActorID:  "00000000-0000-4000-8000-000000000099",
	})
	pairs := map[string]struct{ one, want string }{
		"summary.lines.lines":   {"summary.lines.line", "line"},
		"summary.lines.animals": {"summary.lines.animal", "animal"},
		"summary.lines.pieces":  {"summary.lines.piece", "item"},
		"value.live_animals":    {"value.live_animal", "live animal"},
		"value.every_days":      {"value.every_day", "about every day"},
		"value.days_ago":        {"value.day_ago", "1 day ago"},
	}
	seen := map[string]bool{}
	for _, page := range resp.Pages {
		for many, p := range pairs {
			if page.Copy[many] == "" {
				continue
			}
			seen[many] = true
			if got := page.Copy[p.one]; got != p.want {
				t.Fatalf("page %s serves %q but its singular %q = %q, want %q", page.RouteID, many, p.one, got, p.want)
			}
		}
	}
	for many := range pairs {
		if !seen[many] {
			t.Fatalf("no page serves %q any more; update this test", many)
		}
	}
}
