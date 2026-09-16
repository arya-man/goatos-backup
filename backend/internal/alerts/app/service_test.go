package app

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/alerts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

type fakeConfig struct{}

func (fakeConfig) ListRuleConfig(context.Context, string) ([]domain.StoredRuleConfig, error) {
	return nil, nil
}
func (fakeConfig) UpsertRuleConfig(context.Context, domain.SetRuleConfig) error { return nil }

type fakeStock struct{ calls int }

func (f *fakeStock) LowStock(context.Context, string, int) ([]domain.LowStockFeed, error) {
	f.calls++
	return []domain.LowStockFeed{{ParkID: "p1", FarmLabel: "CBE", FeedItemLabel: "Concentrate", FeedItemKey: "c", BalanceKg: "10", AvgDailyKg: "9", DaysLeft: 1}}, nil
}

type fakeSheets struct{}

func (fakeSheets) PenFeedDay(context.Context, string, string, string) ([]domain.PenFeedDay, string, error) {
	return nil, "", nil
}

type fakeMoves struct{}

func (fakeMoves) PenMovements(context.Context, string, string, string, string) ([]domain.PenMovement, error) {
	return nil, nil
}

// TestLowStockRuleRunsForTodayOnly pins review finding 2026-09-16: the stock rule reads the LIVE
// balance, so a historical date must not run it (and must not claim to have checked it) --
// otherwise a purchase landing today rewrites what yesterday's page reports.
func TestLowStockRuleRunsForTodayOnly(t *testing.T) {
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, biztime.DefaultLocation())
	stock := &fakeStock{}
	svc := NewService(fakeConfig{}, fakeSheets{}, fakeMoves{}, stock, nil, nil).WithClock(func() time.Time { return now })

	past, err := svc.List(context.Background(), "t", "p1", "2026-09-10")
	if err != nil {
		t.Fatal(err)
	}
	if stock.calls != 0 || len(past.Rows) != 0 {
		t.Fatalf("a past day must not read live stock: calls=%d rows=%+v", stock.calls, past.Rows)
	}
	for _, key := range past.RulesRun {
		if key == domain.RuleFeedLowStock {
			t.Fatalf("a skipped rule must not be reported as checked: %v", past.RulesRun)
		}
	}

	today, err := svc.List(context.Background(), "t", "p1", biztime.BusinessDate(now))
	if err != nil {
		t.Fatal(err)
	}
	if stock.calls != 1 || len(today.Rows) != 1 || today.Rows[0].RuleKey != domain.RuleFeedLowStock {
		t.Fatalf("today must read live stock once and report it: calls=%d rows=%+v", stock.calls, today.Rows)
	}
}

// Each missing input must be distinguished from a completed, clean comparison.
type reviewSheets struct {
	missing string
	fail    bool
}

func (f reviewSheets) PenFeedDay(_ context.Context, _, _, day string) ([]domain.PenFeedDay, string, error) {
	if f.fail {
		return nil, "", errors.New("reader unavailable")
	}
	if f.missing == "both" || f.missing == day {
		return nil, "", nil
	}
	return []domain.PenFeedDay{{ShedID: "s", HeadCount: 10, QuantityKg: 5}}, day + "T08:00:00Z", nil
}
func TestPenFeedCheckOnlyCountsCompletedComparisons(t *testing.T) {
	for _, tc := range []struct {
		name, missing              string
		fail                       bool
		checked, skipped, degraded bool
	}{
		{name: "today missing", missing: "2026-09-10", skipped: true},
		{name: "yesterday missing", missing: "2026-09-09", skipped: true},
		{name: "both missing", missing: "both", skipped: true},
		{name: "reader failure", fail: true, degraded: true},
		{name: "both issued and unchanged", checked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 9, 16, 10, 0, 0, 0, biztime.DefaultLocation())
			svc := NewService(fakeConfig{}, reviewSheets{missing: tc.missing, fail: tc.fail}, fakeMoves{}, nil, nil, nil).WithClock(func() time.Time { return now })
			page, err := svc.List(context.Background(), "t", "p1", "2026-09-10")
			if err != nil {
				t.Fatal(err)
			}
			key := domain.RulePenFeedQuantityChange
			skipped := false
			for _, r := range page.Skipped {
				if r.Key == key {
					skipped = true
					if r.Label == "" || r.Reason == "" {
						t.Fatal("skip must explain what could not be checked")
					}
				}
			}
			if slices.Contains(page.RulesRun, key) != tc.checked || skipped != tc.skipped || slices.Contains(page.Degraded, key) != tc.degraded {
				t.Fatalf("unexpected check status: %+v", page)
			}
			if len(page.Rows) != 0 {
				t.Fatalf("unexpected alerts: %+v", page.Rows)
			}
		})
	}
}

type sharedEventFixture struct {
	calls atomic.Int32
	fail  bool
}

func (f *sharedEventFixture) ListEventRules(context.Context, string) ([]domain.EventRule, error) {
	return []domain.EventRule{{ID: "a", Kind: domain.EventAnimalAdded, Enabled: true}, {ID: "b", Kind: domain.EventAnimalAdded, Enabled: true}}, nil
}
func (f *sharedEventFixture) UpsertEventRule(context.Context, domain.SetEventRule) (domain.EventRule, error) {
	panic("unused")
}
func (f *sharedEventFixture) DeleteEventRule(context.Context, string, string) error { panic("unused") }
func (f *sharedEventFixture) Events(context.Context, string, string, domain.EventKind, string) (domain.EventPage, error) {
	f.calls.Add(1)
	if f.fail {
		return domain.EventPage{}, errors.New("unavailable")
	}
	return domain.EventPage{Rows: []domain.Event{{Key: "goat", ParkID: "p1"}}, Total: 1}, nil
}
func TestSameKindRulesShareReadAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		f := &sharedEventFixture{fail: fail}
		s := NewService(fakeConfig{}, fakeSheets{}, fakeMoves{}, nil, nil, nil).WithEvents(f, f)
		p, err := s.List(context.Background(), "t", "p1", "2026-09-10")
		if err != nil {
			t.Fatal(err)
		}
		if f.calls.Load() != 1 {
			t.Fatalf("same kind read %d times", f.calls.Load())
		}
		if fail {
			if len(p.Degraded) != 2 {
				t.Fatalf("failure must reach both rules: %+v", p)
			}
		} else {
			if len(p.Rows) != 2 || p.Rows[0].Key == p.Rows[1].Key {
				t.Fatalf("rules must keep distinct alerts: %+v", p)
			}
		}
	}
}
