package domain

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// PER-KIND SALE CONDITIONS (maintainer decision 2026-09-28). The maintainer's Sales SOP is two
// steps: tag the animals sold (with their weights) when the sale has animals, and enter the weight
// sold when it has none (manure, feed). A sale no step applies to opens no task at all.

func maintainerSaleTrack(t *testing.T) FollowUpTrack {
	t.Helper()
	dsl := loadSeeded(t, "sales.deal")
	track, ok := dsl.Track(TemplateKeySalesDeal)
	if !ok {
		t.Fatal("sale track missing")
	}
	var tag FollowUpStep
	for _, s := range track.Steps {
		if s.Key == "tag_animals" {
			tag = s
		}
	}
	if tag.Key == "" {
		t.Fatal("seeded tag step missing")
	}
	weight := FollowUpStep{
		Key: "weight_sold", TaskType: "record_number", Title: "Enter the weight sold",
		Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}, When: StepWhenSaleHasNoAnimals,
	}
	track.Steps = []FollowUpStep{tag, weight}
	return track
}

func compileSale(t *testing.T, track FollowUpTrack, hasAnimals bool, kinds []string) ([]string, error) {
	t.Helper()
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := CompileTrack(track, reg, CompileOptions{
		EventAt:        time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC),
		SaleHasAnimals: hasAnimals,
		SaleKinds:      kinds,
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(tmpl.Actions))
	for _, a := range tmpl.Actions {
		keys = append(keys, a.Key)
	}
	return keys, nil
}

func TestAnAnimalSaleOpensOnlyTheTagStep(t *testing.T) {
	got, err := compileSale(t, maintainerSaleTrack(t), true, []string{"animal"})
	if err != nil || !reflect.DeepEqual(got, []string{"tag_animals"}) {
		t.Fatalf("animal sale compiled %v, %v; want [tag_animals]", got, err)
	}
}

func TestAManureOrFeedSaleOpensOnlyTheWeightStep(t *testing.T) {
	for _, kinds := range [][]string{{"other"}, {"feed"}, {"feed", "other"}} {
		got, err := compileSale(t, maintainerSaleTrack(t), false, kinds)
		if err != nil || !reflect.DeepEqual(got, []string{"weight_sold"}) {
			t.Fatalf("%v sale compiled %v, %v; want [weight_sold]", kinds, got, err)
		}
	}
}

func TestTheMaintainersTwoStepSaleSOPPublishes(t *testing.T) {
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	dsl := FollowUpDSL{SchemaVersion: "goatos.sop-followup.v1", Tracks: []FollowUpTrack{maintainerSaleTrack(t)}}
	if problems := ValidateFollowUp(dsl, reg); len(problems) != 0 {
		t.Fatalf("the two-step sale SOP must publish: %v", problems)
	}
	// The tag step ALONE publishes too: a manure sale on it simply owes nothing.
	dsl.Tracks[0].Steps = dsl.Tracks[0].Steps[:1]
	if problems := ValidateFollowUp(dsl, reg); len(problems) != 0 {
		t.Fatalf("a tag-only sale SOP must publish: %v", problems)
	}
}

func TestASaleNoStepAppliesToOwesNothingRatherThanFailing(t *testing.T) {
	track := maintainerSaleTrack(t)
	track.Steps = track.Steps[:1] // tag only
	_, err := compileSale(t, track, false, []string{"other"})
	if !errors.Is(err, ErrNothingOwed) {
		t.Fatalf("manure sale on a tag-only SOP: err = %v, want ErrNothingOwed", err)
	}
	if errors.Is(err, ErrFollowUpInvalid) {
		t.Fatal("owing nothing is not a broken document")
	}
}

func TestPerKindConditionsFollowTheLineKinds(t *testing.T) {
	track := maintainerSaleTrack(t)
	track.Steps = []FollowUpStep{
		{Key: "feed_step", TaskType: "record_number", Title: "Feed", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}, When: StepWhenSaleHasFeed},
		{Key: "other_step", TaskType: "record_number", Title: "Other", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}, When: StepWhenSaleHasOther},
		{Key: "always", TaskType: "record_number", Title: "Always", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}},
	}
	cases := []struct {
		kinds []string
		want  []string
	}{
		{[]string{"feed"}, []string{"feed_step", "always"}},
		{[]string{"other"}, []string{"other_step", "always"}},
		{[]string{"animal", "feed", "other"}, []string{"feed_step", "other_step", "always"}},
		// An event written before line_kinds existed: the per-kind steps are left out.
		{nil, []string{"always"}},
	}
	for _, c := range cases {
		got, err := compileSale(t, track, true, c.kinds)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("kinds %v compiled %v, %v; want %v", c.kinds, got, err, c.want)
		}
	}
}

func TestPerKindConditionsAreRefusedOutsideASaleTrack(t *testing.T) {
	reg, err := SeededTaskTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, when := range []string{StepWhenSaleHasNoAnimals, StepWhenSaleHasFeed, StepWhenSaleHasOther} {
		dsl := FollowUpDSL{SchemaVersion: "goatos.sop-followup.v1", Tracks: []FollowUpTrack{{
			Key: "death", Module: "death", Label: "Death",
			Steps: []FollowUpStep{{Key: "a", TaskType: "record_number", Title: "A", Schedule: FollowUpSchedule{Kind: ScheduleKindImmediately}, When: when}},
		}}}
		problems := strings.Join(ValidateFollowUp(dsl, reg), "; ")
		if !strings.Contains(problems, "only applies to a sale's steps") {
			t.Fatalf("%s on a death track: problems = %q", when, problems)
		}
	}
}
