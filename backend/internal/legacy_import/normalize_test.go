package legacy_import

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const (
	syntheticTenant = "00000000-0000-4000-8000-000000000001"
	longRFID        = "9900000000000000001234567890123"
)

func TestSyntheticWorkbookParsingAndNormalization(t *testing.T) {
	policy := syntheticPolicy(t)
	data, err := os.ReadFile("testdata/synthetic_rfid_import.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ParseXLSX(data)
	if err != nil {
		t.Fatalf("ParseXLSX: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("rows=%d, want 12", len(rows))
	}
	if rows[0].RowNumber != 2 {
		t.Fatalf("first data row number=%d, want 2", rows[0].RowNumber)
	}
	if rows[0].Raw["RFID"] != longRFID {
		t.Fatalf("RFID precision lost: %q", rows[0].Raw["RFID"])
	}

	staged, err := NormalizeWorkbookRows(policy, syntheticTenant, rows)
	if err != nil {
		t.Fatalf("NormalizeWorkbookRows: %v", err)
	}
	assertState(t, staged, 2, StatePending)
	assertState(t, staged, 3, StateError)
	assertErrorReason(t, staged, 3, "duplicate_rfid_in_workbook")
	assertState(t, staged, 5, StateNeedsReview)
	assertReason(t, staged, 5, "duplicate_old_tag_same_scope")
	assertState(t, staged, 7, StatePending)
	assertState(t, staged, 8, StatePending)
	assertState(t, staged, 9, StateNeedsReview)
	assertReason(t, staged, 9, "blank_old_tag_suffix")
	assertState(t, staged, 10, StateNeedsReview)
	assertReason(t, staged, 10, "blank_gender")
	assertState(t, staged, 13, StateNeedsReview)
	assertReason(t, staged, 13, "missing_rfid")

	rowF2Male := stagedByNumber(t, staged, 11)
	var normalized map[string]any
	if err := json.Unmarshal(rowF2Male.NormalizedPayload, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized["sex"] != "male" || normalized["growth_cohort_tag"] != "F2" {
		t.Fatalf("F2 male row normalized incorrectly: %#v", normalized)
	}
	rowF2BlankGender := stagedByNumber(t, staged, 10)
	normalized = map[string]any{}
	if err := json.Unmarshal(rowF2BlankGender.NormalizedPayload, &normalized); err != nil {
		t.Fatal(err)
	}
	if normalized["sex"] != nil || normalized["growth_cohort_tag"] != "F2" {
		t.Fatalf("F2 blank gender inferred sex: %#v", normalized)
	}
}

func TestSourceKeyAndHashRecipes(t *testing.T) {
	policy := syntheticPolicy(t)
	row := WorkbookRow{RowNumber: 2, Raw: map[string]string{
		"Farm":          "CBE",
		"Old ID":        "1900",
		"Old ID Suffix": "CBE",
		"RFID":          longRFID,
		"Age":           "Adult",
		"Gender":        "Female",
		"Breed":         "Boer",
		"Tag":           "",
		"Shed":          "S1",
		"Partition":     "P1",
	}}
	staged, err := NormalizeWorkbookRows(policy, syntheticTenant, []WorkbookRow{row})
	if err != nil {
		t.Fatal(err)
	}
	key := staged[0].SourceRowKey
	if strings.HasPrefix(key, syntheticTenant) || strings.Contains(key, "tenant") {
		t.Fatalf("source row key contains tenant prefix: %s", key)
	}
	if strings.Contains(key, "source_key_recipe_v1") {
		t.Fatalf("source row key contains recipe version even though version is stored separately: %s", key)
	}
	if strings.Contains(key, "row_number") || strings.Contains(key, "2") && !strings.Contains(key, "1900") {
		t.Fatalf("source row key appears row-number based: %s", key)
	}
	if !strings.Contains(key, "source_system=legacy_rfid_db") || !strings.Contains(key, "source_dataset=rfid_db_first_import") {
		t.Fatalf("source row key does not follow policy fields: %s", key)
	}
	if staged[0].SourceKeyRecipeVer != "source_key_recipe_v1" || staged[0].HashRecipeVersion != "hash_recipe_v1" {
		t.Fatalf("recipe versions missing: %#v", staged[0])
	}

	row.RowNumber = 99
	again, err := NormalizeWorkbookRows(policy, syntheticTenant, []WorkbookRow{row})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].SourceRowKey != key || again[0].SourceRowVersionHash != staged[0].SourceRowVersionHash {
		t.Fatalf("row number changed key/hash: key %s/%s hash %s/%s", key, again[0].SourceRowKey, staged[0].SourceRowVersionHash, again[0].SourceRowVersionHash)
	}

	row.Raw["Breed"] = "Saanen"
	changed, err := NormalizeWorkbookRows(policy, syntheticTenant, []WorkbookRow{row})
	if err != nil {
		t.Fatal(err)
	}
	if changed[0].SourceRowKey != key {
		t.Fatalf("identity-stable breed change altered source key: %s vs %s", changed[0].SourceRowKey, key)
	}
	if changed[0].SourceRowVersionHash == staged[0].SourceRowVersionHash {
		t.Fatal("meaningful normalized projection change did not alter hash")
	}
}

func TestSpreadsheetNumericIdentifierNormalization(t *testing.T) {
	for _, tc := range []struct {
		name      string
		raw       string
		want      string
		malformed bool
	}{
		{name: "plain digits", raw: "901007000504915", want: "901007000504915"},
		{name: "decimal integer", raw: "901007000504915.0", want: "901007000504915"},
		{name: "scientific integer", raw: "9.01007000504915E14", want: "901007000504915"},
		{name: "lowercase scientific integer", raw: "9.01007000504915e14", want: "901007000504915"},
		{name: "fractional scientific stays raw", raw: "9.010070005049155E14", want: "9.010070005049155E14"},
		{name: "embedded space malformed", raw: "901007 000504915", want: "901007 000504915", malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, malformed := normalizeRFID(tc.raw)
			if got != tc.want || malformed != tc.malformed {
				t.Fatalf("normalizeRFID(%q)=(%q,%v), want (%q,%v)", tc.raw, got, malformed, tc.want, tc.malformed)
			}
		})
	}

	for _, tc := range []struct {
		raw  string
		want string
	}{
		{raw: "1245.0", want: "1245"},
		{raw: "1.245E3", want: "1245"},
		{raw: "SA2328252", want: "SA2328252"},
		{raw: "None", want: "NONE"},
	} {
		t.Run("old tag "+tc.raw, func(t *testing.T) {
			if got := normalizeLegacyIdentifier(tc.raw); got != tc.want {
				t.Fatalf("normalizeLegacyIdentifier(%q)=%q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func syntheticPolicy(t *testing.T) Policy {
	t.Helper()
	var sourceRecipe SourceKeyRecipe
	if err := json.Unmarshal([]byte(`{"strategy":"stable_identity_projection","fields":["source_system","source_dataset","normalized_old_tag","normalized_park_code","rfid"],"forbidden_fields":["row_number","sorted_position","export_line_number"]}`), &sourceRecipe); err != nil {
		t.Fatal(err)
	}
	var hashRecipe HashRecipe
	if err := json.Unmarshal([]byte(`{"strategy":"normalized_meaningful_fields","include_fields":["farm","origin_farm","old_tag_id","rfid","breed","gender","shed","shed_tag","age"],"exclude_fields":["exported_at","formatting","row_number","formula_timestamp"]}`), &hashRecipe); err != nil {
		t.Fatal(err)
	}
	return Policy{
		PolicyVersion:           DefaultPolicyVersion,
		SourceSystem:            "legacy_rfid_db",
		SourceDataset:           "rfid_db_first_import",
		IdentifierPolicyVersion: "phase1-identifier-v1",
		SourceKeyRecipe:         sourceRecipe,
		SourceKeyRecipeVersion:  "source_key_recipe_v1",
		HashRecipe:              hashRecipe,
		HashRecipeVersion:       "hash_recipe_v1",
		NormalizerVersion:       "legacy_rfid_normalizer_v1",
	}
}

func assertState(t *testing.T, rows []StagedRow, rowNumber int, want string) {
	t.Helper()
	got := stagedByNumber(t, rows, rowNumber)
	if got.ProcessingState != want {
		t.Fatalf("row %d state=%s, want %s; error=%v payload=%s", rowNumber, got.ProcessingState, want, got.ErrorReason, string(got.NormalizedPayload))
	}
}

func assertReason(t *testing.T, rows []StagedRow, rowNumber int, want string) {
	t.Helper()
	row := stagedByNumber(t, rows, rowNumber)
	var payload map[string]any
	if err := json.Unmarshal(row.NormalizedPayload, &payload); err != nil {
		t.Fatal(err)
	}
	for _, value := range payload["processing_reasons"].([]any) {
		if value == want {
			return
		}
	}
	t.Fatalf("row %d missing reason %s in %#v", rowNumber, want, payload["processing_reasons"])
}

func assertErrorReason(t *testing.T, rows []StagedRow, rowNumber int, want string) {
	t.Helper()
	row := stagedByNumber(t, rows, rowNumber)
	if row.ErrorReason == nil || *row.ErrorReason != want {
		t.Fatalf("row %d error=%v, want %s", rowNumber, row.ErrorReason, want)
	}
}

func stagedByNumber(t *testing.T, rows []StagedRow, rowNumber int) StagedRow {
	t.Helper()
	for _, row := range rows {
		if row.RowNumber == rowNumber {
			return row
		}
	}
	t.Fatalf("row %d not found", rowNumber)
	return StagedRow{}
}
