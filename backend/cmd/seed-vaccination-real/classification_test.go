package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// reviewedClassification is the independent, doc-anchored authority for the two
// medical axes of every approved vaccine family. Source: the V1 preset table in
// docs/preventive-care-vaccination/vaccination-rules.md. It is intentionally
// declared here (not derived from the seed map) so the test fails if the seed map
// silently drifts from the reviewed matrix.
var reviewedClassification = map[string]struct{ Type, Pathogen string }{
	"ET+TT":       {Type: "killed", Pathogen: "bacterial"},
	"PPR":         {Type: "live", Pathogen: "viral"},
	"Blue tongue": {Type: "killed", Pathogen: "viral"},
	"FMD":         {Type: "killed", Pathogen: "viral"},
	"HS":          {Type: "killed", Pathogen: "bacterial"},
	"Goat Pox":    {Type: "live", Pathogen: "viral"},
	"Sheep Pox":   {Type: "live", Pathogen: "viral"},
}

// TestSeedVaccineClassificationMatchesReviewedAuthority proves every approved
// seeded vaccine carries the reviewed type and pathogen class, and that the
// pathogen field never contains a vaccine-type value (BUG1).
func TestSeedVaccineClassificationMatchesReviewedAuthority(t *testing.T) {
	if len(vaccines) != len(reviewedClassification) {
		t.Fatalf("seed defines %d vaccines, reviewed authority has %d", len(vaccines), len(reviewedClassification))
	}
	forbidden := map[string]struct{}{"live": {}, "killed": {}, "toxoid": {}, "combo": {}}
	for name, want := range reviewedClassification {
		def, ok := vaccines[name]
		if !ok {
			t.Fatalf("reviewed vaccine %q missing from seed map", name)
		}
		if def.Type != want.Type {
			t.Errorf("%s type = %q, reviewed authority = %q", name, def.Type, want.Type)
		}
		if def.Pathogen != want.Pathogen {
			t.Errorf("%s pathogen_class = %q, reviewed authority = %q", name, def.Pathogen, want.Pathogen)
		}
		if _, bad := forbidden[strings.ToLower(def.Pathogen)]; bad {
			t.Errorf("%s pathogen_class %q is a vaccine-TYPE value; the two axes must stay separate", name, def.Pathogen)
		}
	}
}

// TestSeededVaccineClassificationsValidate proves the seed's own guard accepts the
// reviewed matrix.
func TestSeededVaccineClassificationsValidate(t *testing.T) {
	if err := validateVaccineClassifications(); err != nil {
		t.Fatalf("reviewed seed classifications must validate, got: %v", err)
	}
}

// TestValidateVaccineClassificationsRejectsMalformed proves malformed seed
// metadata is rejected, in particular a vaccine-type value leaking into the
// pathogen_class field.
func TestValidateVaccineClassificationsRejectsMalformed(t *testing.T) {
	cases := map[string]vaccineDef{
		"type value in pathogen (live)":   {Code: "x", Type: "live", Pathogen: "live"},
		"type value in pathogen (killed)": {Code: "x", Type: "killed", Pathogen: "killed"},
		"type value in pathogen (toxoid)": {Code: "x", Type: "toxoid", Pathogen: "toxoid"},
		"combo in pathogen":               {Code: "x", Type: "killed", Pathogen: "combo"},
		"empty pathogen":                  {Code: "x", Type: "live", Pathogen: ""},
		"unknown pathogen":                {Code: "x", Type: "live", Pathogen: "fungal"},
		"invalid type":                    {Code: "x", Type: "viral", Pathogen: "viral"},
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateVaccineClassificationsIn(map[string]vaccineDef{"BAD": bad})
			if err == nil {
				t.Fatalf("expected rejection of malformed classification %+v", bad)
			}
		})
	}
}

// TestETTTIsKilledBacterialBooster locks ET+TT to the wiki-approved
// classification: vaccine_type=killed, pathogen_class=bacterial, and a booster
// course (two birth-age doses at 4 and 7 weeks). "Booster" is the course, never
// the vaccine type.
func TestETTTIsKilledBacterialBooster(t *testing.T) {
	def, ok := vaccines["ET+TT"]
	if !ok {
		t.Fatal("ET+TT missing from seed map")
	}
	if def.Type != "killed" {
		t.Errorf("ET+TT vaccine_type = %q, want killed", def.Type)
	}
	if def.Pathogen != "bacterial" {
		t.Errorf("ET+TT pathogen_class = %q, want bacterial", def.Pathogen)
	}

	raw, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix rule_dsl: %v", err)
	}
	var payload struct {
		Rows []struct {
			Vaccine struct {
				Code          string `json:"code"`
				Type          string `json:"type"`
				PathogenClass string `json:"pathogen_class"`
				CourseType    string `json:"course_type"`
			} `json:"vaccine"`
		} `json:"matrix_rows"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal matrix: %v", err)
	}
	var found bool
	for _, row := range payload.Rows {
		if !strings.EqualFold(row.Vaccine.Code, "ET_TT") && !strings.EqualFold(row.Vaccine.Code, "ET+TT") {
			continue
		}
		found = true
		if row.Vaccine.Type != "killed" || row.Vaccine.PathogenClass != "bacterial" || row.Vaccine.CourseType != "booster" {
			t.Errorf("ET+TT matrix row = type %q / pathogen %q / course %q; want killed/bacterial/booster",
				row.Vaccine.Type, row.Vaccine.PathogenClass, row.Vaccine.CourseType)
		}
	}
	if !found {
		t.Fatal("ET+TT row not present in built matrix")
	}
}

// TestSeedMatrixBuildIsDeterministic proves the published vaccination matrix
// config is byte-identical on replay, so re-running the source seed republishes
// the same content and stays idempotent (no spurious new version).
func TestSeedMatrixBuildIsDeterministic(t *testing.T) {
	a, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build 1: %v", err)
	}
	b, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build 2: %v", err)
	}
	if a != b {
		t.Error("vaccination matrix rule_dsl is not deterministic across builds; seed replay would churn config versions")
	}
}

// TestBuiltMatrixStoresTypeAndPathogenSeparately parses the published matrix
// rule_dsl and proves each vaccine row carries a valid, separate type and
// pathogen_class — the metadata the protocol layer stores and the compatibility
// engine consumes.
func TestBuiltMatrixStoresTypeAndPathogenSeparately(t *testing.T) {
	raw, err := vaccinationMatrixRuleDSL()
	if err != nil {
		t.Fatalf("build matrix rule_dsl: %v", err)
	}
	var payload struct {
		Rows []struct {
			Vaccine struct {
				Code          string `json:"code"`
				Type          string `json:"type"`
				PathogenClass string `json:"pathogen_class"`
			} `json:"vaccine"`
		} `json:"matrix_rows"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal matrix: %v", err)
	}
	if len(payload.Rows) == 0 {
		t.Fatal("matrix produced no vaccine rows")
	}
	types := map[string]struct{}{"live": {}, "killed": {}, "toxoid": {}}
	pathogens := map[string]struct{}{"bacterial": {}, "viral": {}}
	for _, row := range payload.Rows {
		v := row.Vaccine
		if _, ok := types[strings.ToLower(v.Type)]; !ok {
			t.Errorf("row %s type %q not a valid immunological type", v.Code, v.Type)
		}
		if _, ok := pathogens[strings.ToLower(v.PathogenClass)]; !ok {
			t.Errorf("row %s pathogen_class %q not a valid pathogen class", v.Code, v.PathogenClass)
		}
		if strings.EqualFold(v.Type, v.PathogenClass) {
			t.Errorf("row %s type and pathogen_class both %q — axes conflated", v.Code, v.Type)
		}
	}
}
