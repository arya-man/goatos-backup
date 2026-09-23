package ceoai

import (
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// goStructDump matches the shape a %v of a Go struct produces: a brace-wrapped
// run of space-separated fields. `{1494 -1 false finite true}` is the
// pgtype.Numeric a `numeric` column decodes into, and it reached a CEO verbatim
// on the most basic feed question in the held-out set. Nothing a reader is shown
// may look like this.
var goStructDump = regexp.MustCompile(`^\{.*\s.*\}$`)

// numericLooking is the bar a FIGURE has to clear: a reader's answer says
// "149.4", never "{1494 -1 false finite true}" and never "149.40000000000000".
var numericLooking = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func numericFromString(t *testing.T, s string) pgtype.Numeric {
	t.Helper()
	var n pgtype.Numeric
	if err := n.Scan(s); err != nil {
		t.Fatalf("scan numeric %q: %v", s, err)
	}
	return n
}

// TestSQLRowValuesNeverRenderAsAGoStruct is the rendering-boundary guard: every
// value pgx can decode out of a ceo_ai read goes through rowsToToolResult, and
// no fact it produces may carry a struct dump or an unreadable figure.
func TestSQLRowValuesNeverRenderAsAGoStruct(t *testing.T) {
	rows := []sqlguard.Row{
		// feed-01: the defect exactly as the reviewer saw it.
		{"label": "Channapatna", "value": pgtype.Numeric{Int: big.NewInt(1494), Exp: -1, Valid: true}, "unit": "kg"},
		// wt-01: the same defect from the weighing route.
		{"label": "Average weight", "value": numericFromString(t, "30.2500000000000000")},
		// sl-03: oracle-exact, printed to 20 decimal places.
		{"label": "Boer", "value": numericFromString(t, "596.71800000000000000000")},
		// A count, a share and a negative variance.
		{"label": "Open cases", "value": int64(21)},
		{"label": "Coverage", "value": float64(0.875)},
		{"label": "Shortfall", "value": numericFromString(t, "-35.00")},
		// Non-figure scalars a grouped read carries beside the value.
		{"label": "Weighed on", "value": pgtype.Date{Time: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC), Valid: true}},
		{"label": "Verified", "value": true},
		{"label": "Shed", "value": "Castro 1", "scope": "Channapatna"},
		// A NULL numeric is absence, not a struct and not a zero.
		{"label": "Not recorded", "value": pgtype.Numeric{}},
	}

	// Which labels carry a FIGURE, and what the reader must be shown.
	wantFigures := map[string]string{
		"Channapatna":    "149.4",
		"Average weight": "30.25",
		"Boer":           "596.718",
		"Open cases":     "21",
		"Coverage":       "0.875",
		"Shortfall":      "-35",
		"Not recorded":   "",
	}

	res := rowsToToolResult("tenant-a", rows, "SELECT label, value FROM ceo_ai.feed_adherence WHERE tenant_id = 'tenant-a' LIMIT 10")
	if len(res.Facts) != len(rows) {
		t.Fatalf("got %d facts for %d rows", len(res.Facts), len(rows))
	}
	for _, f := range res.Facts {
		// The blanket rule, applied to EVERY rendered string a reader sees:
		// label, value, scope and unit alike.
		for field, got := range map[string]string{"label": f.Label, "value": f.Value, "scope": f.Scope, "unit": f.Unit} {
			if goStructDump.MatchString(got) {
				t.Errorf("fact %q %s rendered as a Go struct: %q", f.Label, field, got)
			}
		}
		want, isFigure := wantFigures[f.Label]
		if !isFigure {
			continue
		}
		if f.Value != want {
			t.Errorf("fact %q value = %q, want %q", f.Label, f.Value, want)
		}
		if want == "" {
			continue
		}
		if !numericLooking.MatchString(f.Value) {
			t.Errorf("fact %q value %q does not look like a figure", f.Label, f.Value)
		}
		if _, err := strconv.ParseFloat(f.Value, 64); err != nil {
			t.Errorf("fact %q value %q does not parse as a number: %v", f.Label, f.Value, err)
		}
	}
}

// TestSeriesValuesReadANumericColumn pins the second consequence of the same
// defect: seriesValues parses scalarString's output, so a %v'd struct made every
// numeric series silently vanish from a grouped chart.
func TestSeriesValuesReadANumericColumn(t *testing.T) {
	row := sqlguard.Row{
		"label":        "Channapatna",
		"value":        numericFromString(t, "149.4"),
		"series_fed":   numericFromString(t, "149.4"),
		"series_plan":  numericFromString(t, "184.90"),
		"series_label": "not a number",
	}
	got := seriesValues(row)
	if len(got) != 2 {
		t.Fatalf("seriesValues = %v, want exactly the two numeric series", got)
	}
	if got["fed"] != 149.4 {
		t.Errorf("series fed = %v, want 149.4", got["fed"])
	}
	if got["plan"] != 184.9 {
		t.Errorf("series plan = %v, want 184.9", got["plan"])
	}
}

func TestTrimDecimalZerosNeverRounds(t *testing.T) {
	cases := map[string]string{
		"596.71800000000000000000": "596.718",
		"30.2500000000000000":      "30.25",
		"149.4":                    "149.4",
		"21":                       "21",
		"0.000":                    "0",
		"-35.00":                   "-35",
		"1e-07":                    "1e-07", // exponent form is left alone
		"0.3333333333":             "0.3333333333",
	}
	for in, want := range cases {
		if got := trimDecimalZeros(in); got != want {
			t.Errorf("trimDecimalZeros(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScalarStringAsksTheValueForItsOwnText pins the DEFAULT path, which is what
// actually broke: a wrapper type must be asked for its SQL text form rather than
// %v'd. The assertion is deliberately about the fallthrough, so deleting the
// driver.Valuer branch turns it red even if every named case above stays.
func TestScalarStringAsksTheValueForItsOwnText(t *testing.T) {
	uuid := pgtype.UUID{Bytes: [16]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x30, 0x02}, Valid: true}
	got := scalarString(uuid)
	if strings.ContainsAny(got, "{}[]") {
		t.Fatalf("uuid rendered as a struct/array dump: %q", got)
	}
	if got != "00000000-0000-4000-8000-000000003002" {
		t.Fatalf("uuid rendered %q", got)
	}
	if scalarString(pgtype.Text{}) != "" {
		t.Fatalf("a NULL wrapper must render empty, got %q", scalarString(pgtype.Text{}))
	}
}
