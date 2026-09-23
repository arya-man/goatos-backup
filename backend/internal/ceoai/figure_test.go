package ceoai

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// absurdPlaces is the number of decimal places past which a figure has stopped
// describing anything a leader can act on. It is deliberately LOOSER than any
// family's own allowance: this guard is not a second copy of the precision
// policy, it is the floor under it, so a measure family added later with a
// sensible precision passes and a raw avg() never does.
const absurdPlaces = 4

// TestNoRenderedFigureCarriesAbsurdPrecision is the rounding-boundary guard,
// and it sits alongside TestSQLRowValuesNeverRenderAsAGoStruct deliberately:
// that one proved a figure is a figure and not a struct dump, and stopped
// there, so `Average animal weight: Castro 2 31.4941176470588235` shipped past
// it. This one walks every rendered figure of every fact and fails on any that
// carries more precision than its measure can possibly have.
func TestNoRenderedFigureCarriesAbsurdPrecision(t *testing.T) {
	rows := []sqlguard.Row{
		// The reviewer's per-shed weights, exactly as avg() returns them.
		{"label": "Average animal weight", "scope": "Castro 2", "value": numericFromString(t, "31.4941176470588235"), "unit": "kg"},
		{"label": "Average animal weight", "scope": "Castro 3", "value": numericFromString(t, "30.1111111111111111"), "unit": "kg"},
		// The per-animal probe, where the read casts to text instead.
		{"label": "Average weight kg", "scope": "selected scope", "value": "29.6683333333333333"},
		// ADG: a per-day gain in grams.
		{"label": "Average daily gain", "scope": "Channapatna", "value": numericFromString(t, "109.5238095238095238"), "unit": "g/day"},
		// Money keeps the minor unit and no more.
		{"label": "Realised price per kg", "scope": "Boer", "value": numericFromString(t, "596.71833333333333"), "unit": "INR"},
		// A ratio is the family whose small digits ARE the measurement.
		{"label": "Feed conversion ratio", "scope": "Channapatna", "value": numericFromString(t, "3.14159265358979")},
		// A percent is read to a tenth.
		{"label": "Vaccination coverage", "scope": "Channapatna", "value": numericFromString(t, "94.23076923076923"), "unit": "%"},
		// Already short enough: must come back BYTE-IDENTICAL.
		{"label": "Total fed kg", "scope": "Channapatna", "value": numericFromString(t, "149.4"), "unit": "kg"},
		{"label": "Open cases", "scope": "Channapatna", "value": int64(21)},
		// A label is a group key, not a figure, however numeric it looks.
		{"label": "25.00", "scope": "weight band", "value": int64(7)},
	}

	want := map[string]string{
		"Castro 2":       "31.49",
		"Castro 3":       "30.11",
		"selected scope": "29.67",
		"Channapatna":    "", // three different measures share this scope
		"Boer":           "596.72",
		"weight band":    "7",
	}

	tr := rowsToToolResult("00000000-0000-4000-8000-000000000001", rows, "SELECT 1 FROM ceo_ai.weighing_capture_activity")
	if len(tr.Facts) != len(rows) {
		t.Fatalf("got %d facts from %d rows", len(tr.Facts), len(rows))
	}

	byLabel := map[string]string{}
	for _, f := range tr.Facts {
		byLabel[f.Label] = f.Value
		// THE BLANKET ASSERTION: whatever the measure, whatever the route,
		// nothing a reader is handed may carry this much precision.
		if n := renderedDecimals(f.Value); n > absurdPlaces {
			t.Errorf("fact %q (%s) rendered %q with %d decimal places", f.Label, f.Scope, f.Value, n)
		}
		if f.Value == "" {
			continue
		}
		if strings.ContainsAny(f.Value, "{}") {
			t.Errorf("fact %q rendered a struct dump: %q", f.Label, f.Value)
		}
		if w, ok := want[f.Scope]; ok && w != "" && f.Value != w {
			t.Errorf("fact %q (%s) = %q, want %q", f.Label, f.Scope, f.Value, w)
		}
	}

	// Per-family expectations, so a blanket "round everything to 2" cannot
	// satisfy the guard: it would destroy the ratio and the percent below.
	if got := byLabel["Average daily gain"]; got != "109.52" {
		t.Errorf("a per-day gain in grams rendered %q, want 109.52", got)
	}
	if got := byLabel["Feed conversion ratio"]; got != "3.1416" {
		t.Errorf("a RATIO rendered %q, want 3.1416 -- its small digits are the measurement", got)
	}
	if got := byLabel["Vaccination coverage"]; got != "94.2" {
		t.Errorf("a percent rendered %q, want 94.2", got)
	}
	if got := byLabel["Total fed kg"]; got != "149.4" {
		t.Errorf("a figure already short enough was rewritten to %q", got)
	}
	if got := byLabel["Open cases"]; got != "21" {
		t.Errorf("a count was given a decimal point: %q", got)
	}
	// The numeric-looking LABEL is still a name.
	if _, ok := byLabel["25.00"]; !ok {
		t.Errorf("a numeric-looking label was rounded away; labels: %v", byLabel)
	}
}

// TestFigurePrecisionIsDerivedNotBlanket pins the derivation itself: the same
// digits, four measures, four answers. A single rule for every figure -- the
// thing the reviewer warned against -- fails this outright.
func TestFigurePrecisionIsDerivedNotBlanket(t *testing.T) {
	const raw = "12.3456789012345678"
	cases := []struct {
		unit, label, want string
	}{
		{"%", "Coverage", "12.3"},
		{"", "Coverage percentage", "12.3"},
		{"INR", "Realised price", "12.35"},
		{"₹/kg", "Price per kg", "12.35"},
		{"", "Feed conversion ratio", "12.3457"},
		{"", "Cull share", "12.3457"},
		{"kg", "Average weight", "12.35"},
		{"g/day", "Average daily gain", "12.35"},
		{"days", "Average age at sale", "12.3"},
		{"", "Something nobody named", "12.346"},
	}
	for _, c := range cases {
		if got := readableFigure(raw, c.unit, c.label); got != c.want {
			t.Errorf("readableFigure(%q, unit=%q, label=%q) = %q, want %q", raw, c.unit, c.label, got, c.want)
		}
	}
	// A figure SHORTER than its allowance is returned byte-identical -- the
	// boundary removes precision, it never invents any.
	for _, s := range []string{"149.4", "21", "-35", "0.5", "1e-07", "not a figure", ""} {
		if got := readableFigure(s, "kg", "Average weight"); got != s {
			t.Errorf("readableFigure(%q) = %q, want it untouched", s, got)
		}
	}
}

// TestTheEarTagIsEchoedInOneCase pins the second render-path inconsistency the
// reviewer found: one answer printed the same animal as both "MG-100001" and
// "mg-100001", which reads as two animals.
func TestTheEarTagIsEchoedInOneCase(t *testing.T) {
	rows := []sqlguard.Row{
		{"label": "Animal mg-100001", "scope": "2026-09-17", "value": numericFromString(t, "22.900"), "unit": "kg"},
		{"label": "MG-100001", "scope": "2026-08-27", "value": numericFromString(t, "20.600"), "unit": "kg"},
	}
	tr := rowsToToolResult("00000000-0000-4000-8000-000000000001", rows, "SELECT 1 FROM ceo_ai.growth_adg_pairs")
	for _, f := range tr.Facts {
		if strings.Contains(f.Label, "mg-100001") {
			t.Errorf("the tag is still echoed lower case: %q", f.Label)
		}
		if !strings.Contains(f.Label, "MG-100001") {
			t.Errorf("the tag was lost from the label: %q", f.Label)
		}
		// The DATE in the scope is not a tag and must survive untouched.
		if f.Scope != "2026-09-17" && f.Scope != "2026-08-27" {
			t.Errorf("a scope that is a date was rewritten: %q", f.Scope)
		}
	}
	if got := tr.Facts[0].Label; got != "Animal MG-100001" {
		t.Errorf("label = %q, want the surrounding words untouched", got)
	}
}

func TestCanonicalTagTextLeavesEverythingElseAlone(t *testing.T) {
	for _, s := range []string{
		"Castro 1", "Godel 2 - Part 1", "Mandela 1 - Part 10",
		"2026-09-17", "Deal Closed", "e-commerce",
		"00000000-0000-4000-8000-000000003002",
		"0123abcd-abcd-4000-8000-0123456789ab",
	} {
		if got := canonicalTagText(s); got != s {
			t.Errorf("canonicalTagText(%q) = %q, want it untouched", s, got)
		}
	}
}

// renderedDecimals counts the decimal places of a rendered value, or 0 when it
// is not a plain figure.
func renderedDecimals(s string) int {
	if !plainDecimal.MatchString(s) {
		return 0
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return 0
	}
	return decimalPlaces(s)
}
