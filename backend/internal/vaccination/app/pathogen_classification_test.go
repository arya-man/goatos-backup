package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// reviewedVaccineClass mirrors the reviewed V1 preset matrix in
// docs/preventive-care-vaccination/vaccination-rules.md and the medical class the
// compatibility engine must derive from (type, pathogen_class). This is the
// consumer-side lock for BUG1: given correct seeded metadata, the classifier must
// produce the right same-day-compatibility / spacing class.
var reviewedVaccineClass = []struct {
	Code     string
	Type     string
	Pathogen string
	Want     vaccineImmunoClass
}{
	{"et_tt", "killed", "bacterial", immunoKilledBacterial},
	{"ppr", "live", "viral", immunoLiveViral},
	{"goat_pox", "live", "viral", immunoLiveViral},
	{"sheep_pox", "live", "viral", immunoLiveViral},
	{"fmd", "killed", "viral", immunoKilledViral},
	{"blue_tongue", "killed", "viral", immunoKilledViral},
	{"hs", "killed", "bacterial", immunoKilledBacterial},
}

// TestClassifyVaccineProducesExpectedMedicalClass proves the compatibility
// classification produces the expected medical class for every reviewed vaccine,
// and never degrades to immunoUnknown.
func TestClassifyVaccineProducesExpectedMedicalClass(t *testing.T) {
	for _, tc := range reviewedVaccineClass {
		got := classifyVaccine(tc.Type, tc.Pathogen)
		if got == immunoUnknown {
			t.Errorf("%s (%s/%s) classified as immunoUnknown — compatibility rule would be lost", tc.Code, tc.Type, tc.Pathogen)
		}
		if got != tc.Want {
			t.Errorf("%s (%s/%s) class = %d, want %d", tc.Code, tc.Type, tc.Pathogen, got, tc.Want)
		}
	}
}

// TestProtocolAndRuleLevelMetadataAgree proves the two runtime metadata entry
// points classify identically: the published-rule DSL vaccine metadata
// (protocol/rule level) and a recorded administration's metadata (execution
// level) must yield the same medical class for the same (type, pathogen_class).
// If they diverged, spacing floors computed from history would disagree with the
// spacing used when scheduling the next dose.
func TestProtocolAndRuleLevelMetadataAgree(t *testing.T) {
	for _, tc := range reviewedVaccineClass {
		fromRule := vaccineProfileFromDSL(genDSL{
			Vaccine: genVaccineMeta{
				Code:          tc.Code,
				Type:          tc.Type,
				PathogenClass: tc.Pathogen,
			},
		})
		fromAdmin := vaccineProfileFromAdministration(domain.RecentVaccineAdministration{
			VaccineCode:   tc.Code,
			VaccineType:   tc.Type,
			PathogenClass: tc.Pathogen,
		})
		if fromRule.Class != fromAdmin.Class {
			t.Errorf("%s: rule-level class %d != administration-level class %d", tc.Code, fromRule.Class, fromAdmin.Class)
		}
		if fromRule.Class != tc.Want {
			t.Errorf("%s: rule-level class %d, want %d", tc.Code, fromRule.Class, tc.Want)
		}
	}
}

// TestLeakedTypeValueInPathogenSelectsWrongClass is a regression witness for
// BUG1: a vaccine-type value ("live") wrongly stored in pathogen_class classifies
// a live viral vaccine as generic immunoLive, losing the "live viral + killed
// viral same day" and "bacterial + viral same day" compatibility. Correct
// pathogen data ("viral") yields immunoLiveViral. The two must differ, proving
// why the seeded metadata has to be correct.
func TestLeakedTypeValueInPathogenSelectsWrongClass(t *testing.T) {
	buggy := classifyVaccine("live", "live")    // pathogen carries a type value
	correct := classifyVaccine("live", "viral") // reviewed value
	if correct != immunoLiveViral {
		t.Fatalf("expected correct PPR-like classification immunoLiveViral, got %d", correct)
	}
	if buggy == correct {
		t.Fatal("leaked type-value pathogen classified the same as correct data; the bug would be invisible")
	}
}
