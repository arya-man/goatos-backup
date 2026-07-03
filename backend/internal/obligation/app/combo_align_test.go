package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

func TestPickComboAlignDateWithinWindow(t *testing.T) {
	d1 := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)
	batches := []domain.ComboDriveBatch{
		{BatchID: "b1", PlannedDate: &d1},
		{BatchID: "b2", PlannedDate: &d2},
	}
	got := pickComboAlignDate(batches, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 7*24*time.Hour)
	if got == nil || !got.Equal(d2) {
		t.Fatalf("aligned=%v want %s", got, d2)
	}
}

func TestPickComboAlignDateRejectsWideSpread(t *testing.T) {
	d1 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	batches := []domain.ComboDriveBatch{
		{BatchID: "b1", PlannedDate: &d1},
		{BatchID: "b2", PlannedDate: &d2},
	}
	if got := pickComboAlignDate(batches, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 7*24*time.Hour); got != nil {
		t.Fatalf("expected nil align when spread > window, got %v", got)
	}
}
