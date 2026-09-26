package postgres

import (
	"strings"
	"testing"
)

// TestHerdAnalyticsCompositionLabelsComeFromTheVocabulary pins the /counts/analytics copy defect:
// the composition read returned the stored code as the label, so the page showed "K1", "F2-Male",
// "kid" and "female" to a CEO. The label now comes from the tenant's stage vocabulary and the farm
// words for sex and kid/adult, while every KEY stays the stored value the filters and links use.
// The Postgres round trip is covered by the opt-in integration suite; this keeps the shape honest
// in the default run.
func TestHerdAnalyticsCompositionLabelsComeFromTheVocabulary(t *testing.T) {
	sql := herdAnalyticsCompositionSQL
	for _, want := range []string{
		"LEFT JOIN animal_stage_lookup asl",
		"asl.tenant_id = $1::uuid AND asl.stage_code = s.stage",
		"COALESCE(NULLIF(btrim(asl.name), ''), s.stage)",
		"WHEN 'male' THEN 'Male' WHEN 'female' THEN 'Female'",
		"CASE WHEN is_kid THEN 'Kid' ELSE 'Adult' END",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("composition SQL lost %q", want)
		}
	}
	// The stage KEY is still the stored code, never the display name.
	if !strings.Contains(sql, "SELECT 'stage', s.stage,") {
		t.Fatal("stage key must stay the stored management_stage code")
	}
	// The retired shape rendered the code as its own label.
	if strings.Contains(sql, "SELECT 'stage', stage, stage,") || strings.Contains(sql, "SELECT 'sex', sex, sex,") {
		t.Fatal("composition SQL renders a stored code as its own label again")
	}
}
