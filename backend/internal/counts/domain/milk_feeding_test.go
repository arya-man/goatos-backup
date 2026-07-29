package domain

import (
	"testing"
	"time"
)

func TestMilkFeedingDefinesFourDailySessions(t *testing.T) {
	want := []struct {
		no  int
		due string
	}{{1, "08:00"}, {2, "12:00"}, {3, "16:00"}, {4, "21:00"}}
	got := MilkFeedingSessions()
	if len(got) != len(want) {
		t.Fatalf("sessions=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].SessionNo != want[i].no || got[i].DueTime != want[i].due {
			t.Fatalf("session[%d]=%+v, want %d/%s", i, got[i], want[i].no, want[i].due)
		}
	}
}

func TestMilkFeedingSessionUnlocksOnlyAtItsIndiaStartTime(t *testing.T) {
	india, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	date := "2026-07-30"
	if MilkFeedingIsAvailable(date, "12:00", time.Date(2026, 7, 30, 11, 59, 59, 0, india)) {
		t.Fatal("session was available before its start time")
	}
	if !MilkFeedingIsAvailable(date, "12:00", time.Date(2026, 7, 30, 12, 0, 0, 0, india)) {
		t.Fatal("session did not unlock at its start time")
	}
}

func TestMilkFeedingTaskUsesFarmSessionGrain(t *testing.T) {
	task := MilkFeedingTask{ParkID: "farm-cpt", FeedingDate: "2026-07-29", SessionNo: 1}
	if got := task.GrainKey(); got != "farm-cpt:2026-07-29:1" {
		t.Fatalf("grain key=%q", got)
	}
}

func TestMilkFeedingAnswersRejectBrokenConditionalCascade(t *testing.T) {
	cases := []struct {
		name string
		in   MilkFeedingAnswers
	}{
		{"attempt one above total", MilkFeedingAnswers{TotalKidsFed: 5, Attempt1NotDrinking: 6}},
		{"attempt two above attempt one", MilkFeedingAnswers{TotalKidsFed: 5, Attempt1NotDrinking: 3, Attempt2NotDrinking: 4}},
		{"missing new refusal ids", MilkFeedingAnswers{TotalKidsFed: 5, Attempt1NotDrinking: 2, Attempt2NotDrinking: 2}},
		{"udder above attempt two", MilkFeedingAnswers{TotalKidsFed: 5, Attempt1NotDrinking: 2, Attempt2NotDrinking: 2, NewRefusals: []MilkFeedingNewRefusal{{GoatID: "goat-1"}, {GoatID: "goat-2"}}, UdderMilkNotDrinking: 3}},
		{"ors above udder", MilkFeedingAnswers{TotalKidsFed: 5, Attempt1NotDrinking: 2, Attempt2NotDrinking: 2, NewRefusals: []MilkFeedingNewRefusal{{GoatID: "goat-1"}, {GoatID: "goat-2"}}, UdderMilkNotDrinking: 1, ORSNotDrinking: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.in.Validate(nil); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestMilkFeedingAnswersRequireEveryWatchlistKidAndOnlyNewRefusalIDs(t *testing.T) {
	watchlist := []MilkFeedingWatchlistKid{{GoatID: "watch-1"}, {GoatID: "watch-2"}}
	in := MilkFeedingAnswers{
		WatchlistAnswers: []MilkFeedingWatchlistAnswer{{GoatID: "watch-1", DrankMilk: false}, {GoatID: "watch-2", DrankMilk: true}},
		TotalKidsFed:     10, Attempt1NotDrinking: 3, Attempt2NotDrinking: 2,
		NewRefusals: []MilkFeedingNewRefusal{{GoatID: "new-1", Remarks: "weak"}}, UdderMilkNotDrinking: 1,
	}
	if err := in.Validate(watchlist); err != nil {
		t.Fatalf("valid cascade rejected: %v", err)
	}
	if err := (MilkFeedingAnswers{TotalKidsFed: 2, WatchlistAnswers: []MilkFeedingWatchlistAnswer{{GoatID: "watch-1", DrankMilk: true}}}).Validate(watchlist); err == nil {
		t.Fatal("missing watchlist answer was accepted")
	}
}

func TestMilkFeedingWatchlistGraduatesAfterTwoConsecutiveYes(t *testing.T) {
	current := []MilkFeedingWatchlistKid{{GoatID: "kid-1"}}
	afterFirst := ApplyMilkFeedingWatchlistAnswers(current, []MilkFeedingWatchlistAnswer{{GoatID: "kid-1", DrankMilk: true}}, nil, "2026-07-29", 1)
	if len(afterFirst) != 1 || afterFirst[0].ConsecutiveYes != 1 {
		t.Fatalf("after first yes=%+v", afterFirst)
	}
	afterSecond := ApplyMilkFeedingWatchlistAnswers(afterFirst, []MilkFeedingWatchlistAnswer{{GoatID: "kid-1", DrankMilk: true}}, nil, "2026-07-29", 2)
	if len(afterSecond) != 0 {
		t.Fatalf("kid should graduate after two consecutive yes answers: %+v", afterSecond)
	}
	reset := ApplyMilkFeedingWatchlistAnswers(afterFirst, []MilkFeedingWatchlistAnswer{{GoatID: "kid-1", DrankMilk: false}}, nil, "2026-07-29", 2)
	if len(reset) != 1 || reset[0].ConsecutiveYes != 0 {
		t.Fatalf("refusal should reset consecutive yes: %+v", reset)
	}
}

func TestMilkFeedingProofsRequireTwoDistinctVideos(t *testing.T) {
	if err := (MilkFeedingProofs{}).Validate(); err == nil {
		t.Fatal("missing proof videos accepted")
	}
	if err := (MilkFeedingProofs{CleanBottlesProofRef: "same", MixingAndFillingProofRef: "same"}).Validate(); err == nil {
		t.Fatal("one video accepted for two proof steps")
	}
	if err := (MilkFeedingProofs{CleanBottlesProofRef: "clean", MixingAndFillingProofRef: "mix"}).Validate(); err != nil {
		t.Fatalf("valid proofs rejected: %v", err)
	}
}
