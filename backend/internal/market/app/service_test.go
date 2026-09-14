package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/market/domain"
	"github.com/vgoats/goatos/backend/internal/market/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type stubRepo struct {
	cfg      domain.Config
	entries  []domain.Entry
	recorded int
}

func (s *stubRepo) GetConfig(context.Context, string) (domain.Config, error) { return s.cfg, nil }
func (s *stubRepo) SetCallTime(_ context.Context, _, _, callTime string) error {
	s.cfg.CallTime = callTime
	return nil
}
func (s *stubRepo) CreateCity(context.Context, string, string, string, domain.CityWrite) (domain.City, error) {
	return domain.City{}, nil
}
func (s *stubRepo) UpdateCity(context.Context, string, string, string, domain.CityWrite) (domain.City, error) {
	return domain.City{}, nil
}
func (s *stubRepo) CreateQuestion(context.Context, string, string, string, domain.QuestionWrite) (domain.Question, error) {
	return domain.Question{}, nil
}
func (s *stubRepo) UpdateQuestion(context.Context, string, string, string, domain.QuestionWrite) (domain.Question, error) {
	return domain.Question{}, nil
}
func (s *stubRepo) ListDayEntries(context.Context, string, string) ([]domain.Entry, error) {
	return s.entries, nil
}
func (s *stubRepo) RecordDayEntry(_ context.Context, p ports.RecordDayEntryParams) ([]domain.Entry, error) {
	s.recorded++
	for _, a := range p.Write.Answers {
		s.entries = append(s.entries, domain.Entry{CityID: p.Write.CityID, QuestionID: a.QuestionID, Price: a.Price, BusinessDate: p.Write.BusinessDate})
	}
	return s.entries, nil
}
func (s *stubRepo) ListEntriesBetween(context.Context, string, string, string) ([]domain.Entry, error) {
	return s.entries, nil
}
func (s *stubRepo) ReporterUserIDs(context.Context, string) ([]string, error) { return nil, nil }

// The configured call time gates BOTH halves (maintainer decision 2026-09-14): before it the day
// view is closed with no cards and a save is refused; from it the cards appear and a save lands.
func TestCallTimeGatesTheDayViewAndTheSave(t *testing.T) {
	repo := &stubRepo{cfg: domain.Config{
		Cities:    []domain.City{{ID: "c1", Name: "Chennai", Status: domain.StatusActive}},
		Questions: []domain.Question{{ID: "q1", Label: "Goat live price", UnitLabel: "₹/kg", Status: domain.StatusActive}},
		CallTime:  "09:30",
	}}
	ist := biztime.DefaultLocation()
	clock := time.Date(2026, 9, 14, 9, 0, 0, 0, ist)
	svc := NewService(repo).WithClock(func() time.Time { return clock })

	view, err := svc.GetDay(context.Background(), "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Open || len(view.Cards) != 0 || view.OpensAt != "09:30" || view.Pending != 1 {
		t.Fatalf("before the call time: %+v", view)
	}
	_, err = svc.RecordDay(context.Background(), "t", "u", "k1", domain.DayEntryWrite{CityID: "c1", Answers: []domain.PriceAnswer{{QuestionID: "q1", Price: 600}}})
	if !errors.Is(err, ErrNotOpenYet) || repo.recorded != 0 {
		t.Fatalf("save before the call time = %v (recorded %d), want ErrNotOpenYet and nothing written", err, repo.recorded)
	}

	clock = time.Date(2026, 9, 14, 9, 30, 0, 0, ist)
	view, _ = svc.GetDay(context.Background(), "t", "")
	if !view.Open || len(view.Cards) != 1 {
		t.Fatalf("at the call time: %+v", view)
	}
	card, err := svc.RecordDay(context.Background(), "t", "u", "k2", domain.DayEntryWrite{CityID: "c1", Answers: []domain.PriceAnswer{{QuestionID: "q1", Price: 600}}})
	if err != nil || card.Status != domain.CardDone {
		t.Fatalf("save at the call time = %+v, %v", card, err)
	}
	// Yesterday is always open, whatever the clock says.
	clock = time.Date(2026, 9, 14, 6, 0, 0, 0, ist)
	if v, _ := svc.GetDay(context.Background(), "t", "2026-09-13"); !v.Open {
		t.Fatal("yesterday closed before today's call time")
	}
	if got, err := svc.SetCallTime(context.Background(), "t", "u", "07:15"); err != nil || got != "07:15" || repo.cfg.CallTime != "07:15" {
		t.Fatalf("set call time = %q, %v", got, err)
	}
	if _, err := svc.SetCallTime(context.Background(), "t", "u", "seven"); err == nil {
		t.Fatal("accepted a non-time")
	}
}
