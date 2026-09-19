package ceoai

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// TestSQLFactsSeriesColumns pins the SQL fact contract
// `label, scope, value[, unit][, series_<name>…]` (plan v3 D4): every numeric
// `series_<name>` column lands in Fact.Values[name], a non-numeric one is
// ignored, `unit` is carried, and every fact is stamped with the SESSION tenant
// passed in — never with a tenant_id column from the row.
func TestSQLFactsSeriesColumns(t *testing.T) {
	rows := []sqlguard.Row{
		{
			"label":        "Beetal",
			"scope":        "Coimbatore",
			"value":        "142.5",
			"unit":         "g/day",
			"series_min":   "80",
			"series_max":   142.9,
			"series_notes": "n/a", // non-numeric: ignored, never guessed
			"tenant_id":    "tenant-b",
		},
		{"label": "Sirohi", "scope": "Coimbatore", "value": "120"},
	}
	tr := rowsToToolResult("tenant-a", rows)
	if len(tr.Facts) != 2 {
		t.Fatalf("want 2 facts, got %d", len(tr.Facts))
	}
	f := tr.Facts[0]
	if f.TenantID != "tenant-a" {
		t.Fatalf("TenantID must come from the session tenant, got %q", f.TenantID)
	}
	if f.Label != "Beetal" || f.Scope != "Coimbatore" || f.Value != "142.5" || f.Unit != "g/day" {
		t.Fatalf("contract columns mis-mapped: %+v", f)
	}
	if len(f.Values) != 2 || f.Values["min"] != 80 || f.Values["max"] != 142.9 {
		t.Fatalf("series columns mis-mapped: %+v", f.Values)
	}
	if _, ok := f.Values["notes"]; ok {
		t.Fatalf("non-numeric series column must be ignored, got %+v", f.Values)
	}
	g := tr.Facts[1]
	if g.TenantID != "tenant-a" || g.Values != nil || g.Unit != "" {
		t.Fatalf("plain row: want stamped tenant, nil Values, empty Unit; got %+v", g)
	}

	// Free-shape rows (no label/value pair) are still stamped per column.
	loose := rowsToToolResult("tenant-a", []sqlguard.Row{{"goats": 12, "sheep": 3}})
	for _, f := range loose.Facts {
		if f.TenantID != "tenant-a" {
			t.Fatalf("loose fact %q not stamped: %+v", f.Label, f)
		}
	}
}
