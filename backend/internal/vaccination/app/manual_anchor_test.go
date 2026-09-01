package app

import (
	"context"
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestManualBlueTongueAnchorSuppressesAgeSeedAndSchedulesBoosterFromAnchor(t *testing.T) {
	ctx := context.Background()
	dob := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)
	manualFirstDose := time.Date(2026, time.August, 12, 10, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, time.August, 13, 0, 0, 0, 0, time.UTC)

	proto := &generationProtoFake{
		ruleDSL: []byte(`{"vaccine":{"code":"Blue Tongue","type":"killed","pathogen_class":"viral"},"eligibility":{"species":"sheep","lifecycle":"alive"}}`),
		rules: []protodomain.Rule{
			{
				RuleID: "blue-tongue-first", DoseCode: "blue_tongue_first", Sequence: 1,
				TriggerType: "birth_age", OffsetDays: 112, DueWindowDays: 7,
			},
			{
				RuleID: "blue-tongue-booster", DoseCode: "blue_tongue_booster", Sequence: 2,
				TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 21, DueWindowDays: 7,
			},
		},
	}
	goats := &generationGoatFake{
		list: []domain.EligibleGoat{{
			GoatID: "manual-bt-sheep", LifecycleStatus: "alive", HealthStatus: "healthy",
			Species: "sheep", Stage: "K1", DOB: &dob,
		}},
		vaccineHistory: map[string][]domain.RecentVaccineAdministration{
			"manual-bt-sheep": {{
				AdministeredAt: manualFirstDose,
				VaccineCode:    "Blue Tongue",
				VaccineType:    "killed",
				PathogenClass:  "viral",
				DoseCode:       "blue_tongue_first",
				Sequence:       1,
			}},
		},
	}
	obl := &generationObligationFake{seen: map[string]bool{}}

	result, err := NewGenerationService(proto, goats, obl).GenerateForVersion(ctx, "tenant-1", "version-1", asOf)
	if err != nil {
		t.Fatalf("generate from manual Blue Tongue anchor: %v", err)
	}
	if result.SuppressedByTrustedHistory != 1 {
		t.Fatalf("result=%#v, want the DOB/age first-dose row suppressed by the manual anchor", result)
	}
	if result.Generated != 1 || len(obl.inserted) != 1 {
		t.Fatalf("result=%#v inserted=%#v, want only the booster generated", result, obl.inserted)
	}
	got := obl.inserted[0]
	wantDue := businessDayStart(manualFirstDose).AddDate(0, 0, 21)
	if got.RuleID != "blue-tongue-booster" || !got.DueAt.Equal(wantDue) {
		t.Fatalf("inserted=%#v, want Blue Tongue booster due 21 days after manual first dose %s", got, wantDue)
	}
	if got.RepeatCycle == nil {
		t.Fatal("booster is missing repeat-cycle anchor metadata")
	}
	if want := obldomain.RepeatCycleRef("Blue Tongue", manualFirstDose, 1); got.RepeatCycle.SourceRef != want {
		t.Fatalf("repeat source ref=%q, want manual/completion anchor %q", got.RepeatCycle.SourceRef, want)
	}
}
