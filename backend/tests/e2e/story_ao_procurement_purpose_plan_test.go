package e2e

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

func TestKernelStoryAO_ProcurementPurposePlansDriveObligations(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ao", "Procurement purpose plans drive generated vaccination obligations",
		"A breeding procurement animal and a fattening procurement animal arrive into the same adult shed. "+
			"The published vaccination plan carries separate procurement purpose plans. Generation must create "+
			"only the selected breeding waves for the breeding animal and only the selected fattening waves for "+
			"the fattening animal.")
	defer story.Finish()
	story.Certify("backend kernel")

	const shedID = "ec000000-0000-4000-8000-000000000001"
	const stageID = "ec000000-0000-4000-8000-000000000002"
	fx.SeedAdultShed(shedID, "E2E-AO", stageID, "A1")

	entry := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dob := entry.AddDate(-2, 0, 0)
	const breedingGoat = "ec000000-0000-4000-8000-000000000101"
	const fatteningGoat = "ec000000-0000-4000-8000-000000000102"
	fx.SeedProcurementGoat(breedingGoat, shedID, entry, "A1", dob)
	fx.SeedProcurementPurpose(breedingGoat, "breeding", entry)
	fx.SeedProcurementGoat(fatteningGoat, shedID, entry, "A1", dob)
	fx.SeedProcurementPurpose(fatteningGoat, "fattening", entry)

	ruleDSL := `{
		"eligibility":{"animal_stage":"adult","species":["goat"],"sex":["female"],"breed":["all"],"lifecycle":["alive"],"health":["healthy"],"reproductive":["any"],"defer_states":["sick","under_treatment","recovering","icu","quarantine"]},
		"procurement_policy":{
			"warmup_no_vaccination_days":0,
			"kids_normal_schedule_until_weeks":16,
			"adult_prior_vaccination_allowed":true,
			"purpose_plans":{
				"breeding":{"first_wave":["ET+TT"],"second_wave_after_days":28,"goat_second_wave":["FMD"],"sheep_second_wave":[]},
				"fattening":{"first_wave":["PPR"],"second_wave_after_days":28,"goat_second_wave":["HS"],"sheep_second_wave":[]}
			}
		}
	}`
	versionID, _ := fx.PublishScheduleProtocol("vaccination.e2e.story_ao", ruleDSL, []RuleSpec{
		{DoseCode: "et_tt_adult_w1", Sequence: 1, TriggerType: "manual_campaign", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: matrixEligibility("ET_TT", "ET+TT", "killed", "bacterial")},
		{DoseCode: "ppr_adult_w1", Sequence: 2, TriggerType: "manual_campaign", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: matrixEligibility("PPR", "PPR", "live", "viral")},
		{DoseCode: "fmd_adult_w1", Sequence: 3, TriggerType: "manual_campaign", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: matrixEligibility("FMD", "FMD", "killed", "viral")},
		{DoseCode: "hs_adult_w1", Sequence: 4, TriggerType: "manual_campaign", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: matrixEligibility("HS", "HS", "killed", "bacterial")},
	})

	story.Step("Generate obligations from the published purpose-specific procurement plan",
		"Run the real GenerationService over the published version. The animal purpose comes from "+
			"procurement_load_goats.purpose, not from an in-memory fake.")
	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, entry)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("two obligations per procured animal were generated", res.Generated == 4, "generated=%d", res.Generated)

	breedingVaccines := generatedDoseCodes(fx, breedingGoat)
	fatteningVaccines := generatedDoseCodes(fx, fatteningGoat)
	story.Assert("breeding animal gets the breeding first and goat-second waves only",
		strings.Join(breedingVaccines, ",") == "et_tt_adult_w1,fmd_adult_w1",
		"got=%v", breedingVaccines)
	story.Assert("fattening animal gets the fattening first and goat-second waves only",
		strings.Join(fatteningVaccines, ",") == "hs_adult_w1,ppr_adult_w1",
		"got=%v", fatteningVaccines)
}

func matrixEligibility(code, name, vaccineType, pathogenClass string) string {
	body := map[string]any{
		"eligibility": map[string]any{
			"species":      []string{"goat"},
			"animal_stage": []string{"adult"},
			"sex":          []string{"female"},
			"breed":        []string{"all"},
			"lifecycle":    []string{"alive"},
			"health":       []string{"healthy"},
			"reproductive": []string{"any"},
		},
		"vaccine": map[string]any{
			"code":                code,
			"name":                name,
			"type":                vaccineType,
			"pathogen_class":      pathogenClass,
			"course_type":         "single",
			"compatibility_group": code,
		},
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

func generatedDoseCodes(fx *Fixture, goatID string) []string {
	fx.T.Helper()
	rows, err := fx.Pool.Query(fx.Ctx, `
		SELECT pr.dose_code
		FROM obligation_instances oi
		JOIN protocol_rules pr
		  ON pr.tenant_id = oi.tenant_id
		 AND pr.rule_id = oi.rule_id
		WHERE oi.tenant_id=$1 AND oi.target_id=$2
		ORDER BY pr.dose_code`, fxTenant, goatID)
	if err != nil {
		fx.T.Fatalf("query generated dose codes: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var dose string
		if err := rows.Scan(&dose); err != nil {
			fx.T.Fatalf("scan generated dose code: %v", err)
		}
		out = append(out, dose)
	}
	if err := rows.Err(); err != nil {
		fx.T.Fatalf("read generated dose codes: %v", err)
	}
	sort.Strings(out)
	return out
}
