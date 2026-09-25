package domain

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// SALE WITHOUT ANIMALS (maintainer decision 2026-09-25): a manure / feed / other-item sale, or an
// animal sale with no head count, can never be tagged -- the tagging confirm refuses a deal with no
// animal count -- so a workflow that stamps the tag step for it stays open and overdue forever.
// The seeded sale document conditions the three animal steps (tag, loading video, gate pass) on
// `sale_has_animals`; the opener decides the value from the deal's lines.
func compiledSaleKeys(t *testing.T, hasAnimals bool) []string {
	t.Helper()
	dsl := loadSeeded(t, "sales.deal")
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	track, ok := dsl.Track(TemplateKeySalesDeal)
	if !ok {
		t.Fatal("sale track missing")
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{
		EventAt:        time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC),
		SaleHasAnimals: hasAnimals,
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(tmpl.Actions))
	for _, a := range tmpl.Actions {
		keys = append(keys, a.Key)
	}
	return keys
}

func TestSaleWithoutAnimalsOpensOnlyThePaymentSteps(t *testing.T) {
	if got, want := compiledSaleKeys(t, false), []string{"full_payment", "collect_balance"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a sale with no live animals compiled %v, want %v -- the tag step can never finish on it", got, want)
	}
}

func TestSaleWithAnimalsKeepsTheTagLoadingAndGatePassSteps(t *testing.T) {
	want := []string{"tag_animals", "loading_video", "dispatch_note", "full_payment", "collect_balance"}
	if got := compiledSaleKeys(t, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("a sale with live animals compiled %v, want %v", got, want)
	}
}

func TestSeededSaleConditionsExactlyTheThreeAnimalSteps(t *testing.T) {
	dsl := loadSeeded(t, "sales.deal")
	track, _ := dsl.Track(TemplateKeySalesDeal)
	conditioned := map[string]bool{}
	for _, s := range track.Steps {
		if s.When == StepWhenSaleHasAnimals {
			conditioned[s.Key] = true
		}
	}
	want := map[string]bool{"tag_animals": true, "loading_video": true, "dispatch_note": true}
	if !reflect.DeepEqual(conditioned, want) {
		t.Fatalf("conditioned steps = %v, want %v", conditioned, want)
	}
}

// The condition names a fact about a SALE; on a birth or death step it would be decided by an
// opener that never looks at a sale, so it is refused rather than silently always-true.
func TestSaleHasAnimalsIsRefusedOutsideTheSaleTrack(t *testing.T) {
	reg, _ := SeededTaskTypes()
	dsl := loadSeeded(t, "counts.death")
	dsl.Tracks[0].Steps[0].When = StepWhenSaleHasAnimals
	problems := ValidateFollowUp(dsl, reg)
	found := false
	for _, p := range problems {
		if strings.Contains(p, "when") && strings.Contains(p, StepWhenSaleHasAnimals) {
			found = true
		}
	}
	if !found {
		t.Fatalf("sale_has_animals on a death step must be refused, got %v", problems)
	}
	sale := loadSeeded(t, "sales.deal")
	if problems := ValidateFollowUp(sale, reg); len(problems) > 0 {
		t.Fatalf("seeded sale document must validate: %v", problems)
	}
}

// A step that always runs but waits on a dropped step must not wait forever: the reference to a
// step the opening context left out is released, never a dangling prerequisite.
func TestAStepWaitingOnADroppedStepIsReleased(t *testing.T) {
	reg, _ := SeededTaskTypes()
	dsl := loadSeeded(t, "sales.deal")
	track, _ := dsl.Track(TemplateKeySalesDeal)
	for i := range track.Steps {
		if track.Steps[i].Key == "full_payment" {
			track.Steps[i].Requires = []string{"dispatch_note"}
		}
		if track.Steps[i].Key == "collect_balance" {
			track.Steps[i].Schedule = FollowUpSchedule{Kind: ScheduleKindAfterStep, Step: "loading_video", OffsetMinutes: 60}
			track.Steps[i].WhenAnswer = nil
		}
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC), SaleHasAnimals: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range tmpl.Actions {
		if len(a.Requires) != 0 {
			t.Fatalf("step %q still requires dropped %v", a.Key, a.Requires)
		}
		if a.Schedule.AfterStepKey != "" {
			t.Fatalf("step %q still waits on dropped step %q", a.Key, a.Schedule.AfterStepKey)
		}
	}
}
