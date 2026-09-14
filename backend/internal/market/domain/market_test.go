package domain

import (
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func TestBuildDayCardsIsPendingUntilEveryActiveQuestionIsAnswered(t *testing.T) {
	cfg := Config{
		Cities: []City{{ID: "c1", Name: "Chennai", Status: StatusActive}, {ID: "c2", Name: "Salem", Status: StatusRetired}},
		Questions: []Question{
			{ID: "q1", Label: "Goat live price", UnitLabel: "₹/kg", Status: StatusActive},
			{ID: "q2", Label: "Sheep live price", UnitLabel: "₹/kg", Status: StatusActive},
			{ID: "q3", Label: "Old", UnitLabel: "₹/kg", Status: StatusRetired},
		},
	}
	cards := BuildDayCards(cfg, []Entry{{CityID: "c1", QuestionID: "q1", Price: 600}, {CityID: "c1", QuestionID: "q3", Price: 1}})
	if len(cards) != 1 || cards[0].City.ID != "c1" {
		t.Fatalf("cards = %+v, want the one active city", cards)
	}
	c := cards[0]
	if c.Total != 2 || c.Answered != 1 || c.Status != CardPending {
		t.Fatalf("card = answered %d/%d %s, want 1/2 pending (a retired question's entry is not on the card)", c.Answered, c.Total, c.Status)
	}
	cards = BuildDayCards(cfg, []Entry{{CityID: "c1", QuestionID: "q1", Price: 600}, {CityID: "c1", QuestionID: "q2", Price: 500}})
	if cards[0].Status != CardDone {
		t.Fatalf("fully answered card = %s, want done", cards[0].Status)
	}
}

func TestBuildAnalyticsStartsANewLineWhenTheUnitChanges(t *testing.T) {
	entries := []Entry{
		{CityID: "c1", CityName: "Chennai", QuestionID: "q1", QuestionLabel: "Goat live price", UnitLabel: "₹/kg", BusinessDate: "2026-09-12", Price: 600},
		{CityID: "c1", CityName: "Chennai", QuestionID: "q1", QuestionLabel: "Goat live price", UnitLabel: "₹/kg", BusinessDate: "2026-09-13", Price: 620},
		{CityID: "c1", CityName: "Chennai", QuestionID: "q1", QuestionLabel: "Goat live price", UnitLabel: "₹/500 g", BusinessDate: "2026-09-14", Price: 320},
	}
	a := BuildAnalytics("2026-09-12", "2026-09-14", entries)
	if len(a.Series) != 2 {
		t.Fatalf("series = %d, want 2 (one per unit)", len(a.Series))
	}
	if a.Series[0].UnitLabel != "₹/kg" || len(a.Series[0].Points) != 2 || a.Series[1].UnitLabel != "₹/500 g" || len(a.Series[1].Points) != 1 {
		t.Fatalf("series = %+v", a.Series)
	}
	if len(a.Latest) != 1 || a.Latest[0].Price != 320 || a.Latest[0].UnitLabel != "₹/500 g" || a.Latest[0].PreviousPrice == nil || *a.Latest[0].PreviousPrice != 620 {
		t.Fatalf("latest = %+v, want 320 ₹/500 g with previous 620", a.Latest)
	}
	if a.Days != 3 {
		t.Fatalf("days = %d, want 3", a.Days)
	}
}

func TestDayEntryWriteValidation(t *testing.T) {
	base := DayEntryWrite{CityID: "c1", BusinessDate: "2026-09-14", Answers: []PriceAnswer{{QuestionID: "q1", Price: 10}}}
	if err := base.Validate(); err != nil {
		t.Fatalf("valid write refused: %v", err)
	}
	cases := map[string]DayEntryWrite{
		"no answers":     {CityID: "c1", BusinessDate: "2026-09-14"},
		"bad date":       {CityID: "c1", BusinessDate: "14/09/2026", Answers: base.Answers},
		"negative":       {CityID: "c1", BusinessDate: "2026-09-14", Answers: []PriceAnswer{{QuestionID: "q1", Price: -1}}},
		"repeated":       {CityID: "c1", BusinessDate: "2026-09-14", Answers: []PriceAnswer{{QuestionID: "q1", Price: 1}, {QuestionID: "q1", Price: 2}}},
		"blank question": {CityID: "c1", BusinessDate: "2026-09-14", Answers: []PriceAnswer{{QuestionID: " ", Price: 1}}},
	}
	for name, w := range cases {
		if err := w.Validate(); err == nil {
			t.Errorf("%s: accepted, want refusal", name)
		}
	}
}

func TestQuestionWriteNormalizesAndRefusesBlankUnit(t *testing.T) {
	w := QuestionWrite{Label: "  Goat   carcass ", UnitLabel: " ₹/kg "}.Normalize()
	if w.Label != "Goat carcass" || w.UnitLabel != "₹/kg" || w.Status != StatusActive {
		t.Fatalf("normalized = %+v", w)
	}
	if err := (QuestionWrite{Label: "X"}).Normalize().Validate(); err == nil {
		t.Fatal("blank unit accepted")
	}
	if err := (QuestionWrite{Label: "X", UnitLabel: "₹/kg", Status: "gone"}).Normalize().Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
}

func TestDefaultBusinessDateIsTheISTDay(t *testing.T) {
	// 20:30 UTC on the 13th is 02:00 IST on the 14th.
	at := time.Date(2026, 9, 13, 20, 30, 0, 0, time.UTC)
	if got := DefaultBusinessDate(at); got != "2026-09-14" {
		t.Fatalf("business date = %s, want 2026-09-14", got)
	}
	_ = f
}
