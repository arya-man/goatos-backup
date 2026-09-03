package app

import "testing"

// The dashboard calls the operational location a PEN (maintainer decision 2026-09-02), so the
// bulk-import template's location column is labelled "Pen name" where it used to say "Shed".
//
// The header parser matches by NAME, so a label change is a parser change. This pins the half
// that matters: EVERY header the old template and the old aliases accepted still resolves to
// exactly the same field. A sheet saved before the rename imports byte for byte as it did.
//
// It also pins the trap that made this worth a test. "pen" already meant PARTITION here
// (pen_label/penlabel are its siblings), so labelling the location column "Pen" would have
// silently filed a shed name into partition_label -- a wrong location on every imported animal,
// with no error. The template says "Pen name" precisely so bare "pen" can keep its meaning.
func TestImportHeaderAliasesSurviveThePenRename(t *testing.T) {
	for header, want := range map[string]string{
		// Location -- what the template used to say, and what it says now.
		"shed":     "shed",
		"Shed":     "shed",
		"SHED":     "shed",
		"Pen name": "shed",
		"pen_name": "shed",
		"penname":  "shed",

		// Partition -- unchanged, and "pen" is still one of its names.
		"partition":       "partition_label",
		"Partition":       "partition_label",
		"partition_label": "partition_label",
		"pen":             "partition_label",
		"Pen":             "partition_label",
		"pen_label":       "partition_label",
		"penlabel":        "partition_label",

		// A sample of untouched neighbours, so a future edit to this switch cannot quietly
		// rewrite the columns beside the two that moved.
		"Tag 1":      "tag_1",
		"Park":       "park",
		"Species":    "species",
		"stage":      "management_stage",
		"Weight(kg)": "weight_kg",
	} {
		if got := normalizeHeader(header); got != want {
			t.Errorf("normalizeHeader(%q) = %q, want %q", header, got, want)
		}
	}
}
