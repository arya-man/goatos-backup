package app

import (
	"context"
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
