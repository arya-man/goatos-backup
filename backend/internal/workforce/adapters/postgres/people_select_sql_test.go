package postgres

import (
	"strings"
	"testing"
)

// TestPeopleSelectSQLStartsWithSelect pins the 2026-09-11 defect: a `// scale-guard:ignore`
// marker was placed INSIDE the SQL backtick literal, so Postgres received a Go comment as the
// first token and every People / HRMS list call failed with `syntax error at or near "//"`.
// The guard marker belongs on the line above the finding, outside the string.
func TestPeopleSelectSQLStartsWithSelect(t *testing.T) {
	sql := strings.TrimSpace(peopleSelectSQL(""))
	if !strings.HasPrefix(strings.ToUpper(sql), "SELECT") {
		t.Fatalf("peopleSelectSQL must start with SELECT, got %q", sql[:min(len(sql), 60)])
	}
	if strings.Contains(sql, "//") {
		t.Fatalf("peopleSelectSQL carries a Go-style comment inside the SQL literal: %q", sql[:min(len(sql), 120)])
	}
}
