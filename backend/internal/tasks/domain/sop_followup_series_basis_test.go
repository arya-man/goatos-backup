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

// TestSeriesNextSessionsKeepsTheCountOnTheFarmClock pins the maintainer's actual rule
// (2026-09-14, "we won't feed at midnight"): rounds stay on the farm's session times, but a
// late-born kid takes its ten rounds from the NEXT session onward, spilling into day 3, so it
// loses nothing and nothing lands at 02:00.
func TestSeriesNextSessionsKeepsTheCountOnTheFarmClock(t *testing.T) {
	dsl := loadSeeded(t, SOPCodeBirth)
	reg, _ := SeededTaskTypes()
	ist := biztime.DefaultLocation()
	track, _ := dsl.Track(TemplateKeyBirthKid)
	for i := range track.Steps {
		if track.Steps[i].Key == "colostrum_series" {
			track.Steps[i].Schedule = FollowUpSchedule{
				Kind: ScheduleKindSeries, Basis: SeriesBasisNextSessions,
				Times: []string{"07:00", "11:00", "15:00", "18:30", "22:00"}, Count: 10, PreNotifyMinute: 15,
				KeyPattern: "colostrum_day_{day}_{hhmm}", OrdinalStart: 2,
			}
		}
	}
	if problems := ValidateFollowUp(dsl, reg); len(problems) > 0 {
		t.Fatalf("next_sessions series should validate: %v", problems)
	}
	got, err := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 13, 15, 0, 0, 0, ist)})
	if err != nil {
		t.Fatal(err)
	}
	var rounds []ActionTemplate
	for _, a := range got.Actions {
		if strings.HasPrefix(a.Key, "colostrum_day_") {
			rounds = append(rounds, a)
		}
	}
	if len(rounds) != 10 {
		t.Fatalf("next_sessions at 15:00 = %d rounds, want 10", len(rounds))
	}
	want := []struct{ d, h, m int }{{0, 18, 30}, {0, 22, 0}, {1, 7, 0}, {1, 11, 0}, {1, 15, 0}, {1, 18, 30}, {1, 22, 0}, {2, 7, 0}, {2, 11, 0}, {2, 15, 0}}
	for i, w := range want {
		r := rounds[i].Schedule
		if !r.AtFixedTime || r.DayOffset != w.d || r.Hour != w.h || r.Minute != w.m {
			t.Fatalf("round %d = %+v, want day+%d %02d:%02d", i+1, r, w.d, w.h, w.m)
		}
	}
	if rounds[0].Title != "2nd Colostrum" || rounds[9].Title != "11th Colostrum" {
		t.Fatalf("ordinals: %q .. %q", rounds[0].Title, rounds[9].Title)
	}
	// An early birth gets the same ten, starting the same day.
	early, _ := CompileTrack(track, reg, CompileOptions{EventAt: time.Date(2026, 9, 13, 5, 30, 0, 0, ist)})
	n := 0
	for _, a := range early.Actions {
		if strings.HasPrefix(a.Key, "colostrum_day_") {
			n++
		}
	}
	if n != 10 {
		t.Fatalf("next_sessions at 05:30 = %d rounds, want 10", n)
	}
}
