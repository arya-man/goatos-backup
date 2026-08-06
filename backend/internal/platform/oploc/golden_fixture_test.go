package oploc

import "testing"

// This file is the CANONICAL golden fixture for the operational-location
// convention (AGENTS.md -> "That normalization is a STORAGE rule..."), settled
// against the master `Sheds DB.xlsx`, live BigQuery `counting`/`Shiftings`, and
// legacy Slack/dashboard code.
//
// The SAME row shapes and the SAME expected display strings are pinned in:
//   - Go (this file):          backend/internal/platform/oploc/golden_fixture_test.go
//   - TypeScript (admin-web):  apps/admin-web/lib/operational-location.test.mjs
//   - Kotlin (Android):        apps/goatos-android/core/core-ui/.../PartitionLabelTest.kt
//
// If a row is added/changed here, the same row must be added/changed in both
// other files with the identical `want` string, or cross-surface parity is
// unproven. This is the "extend the SQL-vs-Go pattern to the client helpers"
// fixture referenced in
// backend/internal/obligation/adapters/postgres/partition_label_display_parity_integration_test.go.
//
// This test is PURE (no DB, no network) and therefore runs unconditionally in
// `go test ./...` / `make ci-local` -- it is NOT gated behind
// GOATOS_RUN_POSTGRES_TESTS=1.
type goldenFixtureRow struct {
	name      string
	shedID    string
	shedName  string
	partition string
	want      string
}

// goldenFixture is the one canonical table. Keep row `name` identical across
// all three language files -- it is how a human maps a failure across surfaces.
var goldenFixture = []goldenFixtureRow{
	{
		name:      "subdivided shed, numeric-suffixed name, worded partition",
		shedID:    "shed-godel-1",
		shedName:  "Godel 1",
		partition: "Part 3",
		want:      "Godel 1 - Part 3",
	},
	{
		name:      "subdivided shed, numeric-suffixed name, two-digit worded partition",
		shedID:    "shed-godel-1",
		shedName:  "Godel 1",
		partition: "Part 10",
		want:      "Godel 1 - Part 10",
	},
	{
		name:      "subdivided shed, plain name, bare numeric partition",
		shedID:    "shed-castro-cbe",
		shedName:  "Castro",
		partition: "2",
		want:      "Castro - 2",
	},
	{
		name:      "undivided shed, no partition",
		shedID:    "shed-yashoda-cbe",
		shedName:  "Yashoda",
		partition: "",
		want:      "Yashoda",
	},
	{
		name:      "'whole' sentinel must never reach the user",
		shedID:    "shed-yashoda-cbe",
		shedName:  "Yashoda",
		partition: "whole",
		want:      "Yashoda",
	},
	{
		name:      "empty-string label",
		shedID:    "shed-mandela-1",
		shedName:  "Mandela 1",
		partition: "",
		want:      "Mandela 1",
	},
	{
		name:      "NULL label (read out as empty string)",
		shedID:    "shed-mandela-1",
		shedName:  "Mandela 1",
		partition: "",
		want:      "Mandela 1",
	},
	{
		name:      "two same-named sheds, different parks -- CBE",
		shedID:    "shed-castro-cbe",
		shedName:  "Castro",
		partition: "1",
		want:      "Castro - 1",
	},
	{
		name:      "two same-named sheds, different parks -- CPT",
		shedID:    "shed-castro-cpt",
		shedName:  "Castro",
		partition: "1",
		want:      "Castro - 1",
	},
}

// TestGoldenFixtureDisplay pins oploc.Display() to the canonical cross-surface
// fixture table. See goldenFixture doc comment for the full cross-language
// contract this test is one third of.
func TestGoldenFixtureDisplay(t *testing.T) {
	for _, row := range goldenFixture {
		t.Run(row.name, func(t *testing.T) {
			loc := OperationalLocation{ShedID: row.shedID, ShedName: row.shedName, PartitionLabel: row.partition}
			got := loc.Display()
			if got != row.want {
				t.Fatalf("Display() = %q, want %q", got, row.want)
			}
		})
	}
}

// TestGoldenFixtureKeyDistinguishesSameNamedShedsAcrossParks proves the two
// "Castro" rows in the fixture (defect class: keying by shed NAME instead of
// shed_id merges two different physical sheds in two different parks) resolve
// to different Key()s despite identical display strings and partition labels.
func TestGoldenFixtureKeyDistinguishesSameNamedShedsAcrossParks(t *testing.T) {
	var cbe, cpt OperationalLocation
	for _, row := range goldenFixture {
		loc := OperationalLocation{ShedID: row.shedID, ShedName: row.shedName, PartitionLabel: row.partition}
		switch row.name {
		case "two same-named sheds, different parks -- CBE":
			cbe = loc
		case "two same-named sheds, different parks -- CPT":
			cpt = loc
		}
	}
	if cbe.ShedID == "" || cpt.ShedID == "" {
		t.Fatal("test bug: fixture rows for the same-named-shed case not found")
	}
	if cbe.Display() != cpt.Display() {
		t.Fatalf("test bug: fixture rows should share a display string, got %q vs %q", cbe.Display(), cpt.Display())
	}
	if cbe.Key() == cpt.Key() {
		t.Fatalf("two parks' Castro partition 1 collapsed to one Key() (%q); grouping/counting by name instead of shed_id would merge parks", cbe.Key())
	}
}
