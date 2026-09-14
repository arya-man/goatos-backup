package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// TestSeriesFromEventKeepsEveryRoundForALateBirth pins the maintainer decision of 2026-09-14: a
// colostrum series authored "from the event" runs its full count from the birth instant, so a
// kid born at 15:00 gets all ten rounds (at 19:00, 23:00, 03:00 …) instead of losing the morning
// sessions the fixed-times basis skips. Same seeded document, one schedule swapped.
func TestSeriesFromEventKeepsEveryRoundForALateBirth(t *testing.T) {
	dsl := loadSeeded(t, SOPCodeBirth)
	reg, _ := SeededTaskTypes()
	ist := biztime.DefaultLocation()
	at := time.Date(2026, 9, 13, 15, 0, 0, 0, ist)

	track, _ := dsl.Track(TemplateKeyBirthKid)
	fixed, err := CompileTrack(track, reg, CompileOptions{EventAt: at})
	if err != nil {
		t.Fatal(err)
	}
	fixedRounds := countColostrumRounds(fixed)
	if fixedRounds != 7 { // 18:30, 22:00 today + five tomorrow: the morning rounds are gone
		t.Fatalf("fixed-times basis at 15:00 = %d rounds, want 7", fixedRounds)
	}

	for i := range track.Steps {
		if track.Steps[i].Key != "colostrum_series" {
			continue
		}
		track.Steps[i].Schedule = FollowUpSchedule{
			Kind: ScheduleKindSeries, Basis: SeriesBasisFromEvent, IntervalMinutes: 240, Count: 10,
			KeyPattern: "colostrum_round_{n}", OrdinalStart: 2,
		}
	}
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("from_event series should validate: %v", problems)
	}
	got, err := CompileTrack(track, reg, CompileOptions{EventAt: at})
	if err != nil {
		t.Fatal(err)
	}
	var rounds []ActionTemplate
	for _, a := range got.Actions {
		if strings.HasPrefix(a.Key, "colostrum_round_") {
			rounds = append(rounds, a)
		}
	}
	if len(rounds) != 10 {
		t.Fatalf("from_event basis at 15:00 = %d rounds, want 10", len(rounds))
	}
	if rounds[0].Title != "2nd Colostrum" || rounds[9].Title != "11th Colostrum" {
		t.Fatalf("ordinals continue from the authored start: %q .. %q", rounds[0].Title, rounds[9].Title)
	}
	for i, r := range rounds {
		want := time.Duration(4*(i+1)) * time.Hour
		if r.Schedule.AtFixedTime || r.Schedule.Offset != want {
			t.Fatalf("round %d schedule = %+v, want relative offset %s", i+1, r.Schedule, want)
		}
	}
	if !strings.Contains(rounds[0].Detail, "19:00") || !strings.Contains(rounds[2].Detail, "day after birth") {
		t.Fatalf("rendered time/day label wrong: %q / %q", rounds[0].Detail, rounds[2].Detail)
	}
	keys := map[string]bool{}
	for _, r := range rounds {
		if keys[r.Key] {
			t.Fatalf("duplicate round key %q", r.Key)
		}
		keys[r.Key] = true
	}
}

func TestSeriesFromEventValidationNamesTheField(t *testing.T) {
	dsl := loadSeeded(t, SOPCodeBirth)
	reg, _ := SeededTaskTypes()
	track, _ := dsl.Track(TemplateKeyBirthKid)
	for i := range track.Steps {
		if track.Steps[i].Key == "colostrum_series" {
			track.Steps[i].Schedule = FollowUpSchedule{Kind: ScheduleKindSeries, Basis: SeriesBasisFromEvent, IntervalMinutes: 0, Count: 500, KeyPattern: "colostrum_{day}"}
		}
	}
	problems := ValidateFollowUp(dsl, reg)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"interval_minutes", "count", "key_pattern"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected a problem naming %s, got: %v", want, problems)
		}
	}
}

func countColostrumRounds(tpl Template) int {
	n := 0
	for _, a := range tpl.Actions {
		if strings.HasPrefix(a.Key, "colostrum_day_") {
			n++
		}
	}
	return n
}
