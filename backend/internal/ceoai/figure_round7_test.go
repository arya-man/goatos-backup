package ceoai

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
)

// A KEY IN THE LABEL SLOT DESCRIBES NOTHING.
//
// The behavioural review counted 12 lines across 86 live answers reading
// "Castro 1 in Castro 1" / "Boer in Boer": a model-drafted read is free to
// write `SELECT park_label AS label, park_label AS scope, … AS value`, and
// then the series KEY lands in the label slot. renderFacts now collapses the
// sentence — but the FIGURE had already been rendered from that label, and no
// park name carries a measure word, so the precision fell to unknownPlaces.
//
// Measured LIVE on this branch before the fix: "what is the average weight per
// park" -> "Channapatna: 32.948" against a database whose average really is
// 32.9480000000000000 kg. Three decimals of a kilogram, out of the one file
// whose stated job is that a leader never reads a figure at a precision its
// measure does not carry.
func TestAKeyInTheLabelSlotDoesNotDecideTheFiguresPrecision(t *testing.T) {
	const tenant = "00000000-0000-4000-8000-000000000001"
	const weightSQL = "SELECT park_label AS label, park_label AS scope, avg(weight_kg) AS value FROM ceo_ai.weighing_latest_individual_weight"

	rows := []sqlguard.Row{
		{"label": "Channapatna", "scope": "Channapatna", "value": "32.9480000000000000"},
		{"label": "Coimbatore", "scope": "Coimbatore", "value": "29.2925925925925926"},
	}
	tr := rowsToToolResult(tenant, rows, weightSQL)
	want := []string{"32.95", "29.29"}
	for i, f := range tr.Facts {
		if f.Value != want[i] {
			t.Errorf("fact %d: value %q, want %q — the park name in the label slot decided the precision of a weight in kilograms",
				i, f.Value, want[i])
		}
	}

	// THE LABEL IS STILL THE EVIDENCE WHENEVER IT IS A MEASURE. When the two
	// slots differ, the label is the phrase the read wrote for the measure and
	// nothing about it changes — including when the label and the view would
	// disagree about the family.
	rows = []sqlguard.Row{
		{"label": "Average rate per kilo", "scope": "Boer", "value": "565.81390000"},
		{"label": "Feed conversion ratio", "scope": "Castro 1", "value": "3.14159265358979"},
	}
	tr = rowsToToolResult(tenant, rows, "SELECT 1 FROM ceo_ai.sales_deal_lines_closed")
	for i, want := range []string{"565.81", "3.1416"} {
		if tr.Facts[i].Value != want {
			t.Errorf("a differently-spelled label is the measure and must still decide: fact %d = %q, want %q",
				i, tr.Facts[i].Value, want)
		}
	}
}

// TestTheViewOnlyStandsInWhenTheLabelIsTheKey pins the substitution's SHAPE
// rather than one reading of it: the source view may speak only where the
// label has said nothing, so a card name can never quietly outrank a measure
// a read actually named.
func TestTheViewOnlyStandsInWhenTheLabelIsTheKey(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		label, scope, sourceView string
		want                     string
	}{
		{"key in both slots yields to the view", "Channapatna", "Channapatna", "weighing_latest_individual_weight", "weighing_latest_individual_weight"},
		{"case and space differences are the same slot", " castro 1", "Castro 1 ", "growth_adg_pairs", "growth_adg_pairs"},
		{"a measure label keeps the label", "Average weight kg", "Channapatna", "sales_deal_lines_closed", "Average weight kg"},
		{"no scope keeps the label", "Channapatna", "", "sales_deal_lines_closed", "Channapatna"},
		{"no view keeps the label", "Channapatna", "Channapatna", "", "Channapatna"},
	} {
		if got := measureDescription(tc.label, tc.scope, tc.sourceView); got != tc.want {
			t.Errorf("%s: measureDescription(%q,%q,%q) = %q, want %q",
				tc.name, tc.label, tc.scope, tc.sourceView, got, tc.want)
		}
	}
}
