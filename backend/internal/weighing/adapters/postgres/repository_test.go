package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestProgressCountsIndividualAndPerShedCompletion(t *testing.T) {
	got := progress([]domain.CampaignShed{
		{WeighingCategory: domain.CategoryIndividualAnimal, ExpectedAnimalCount: 3},
		{WeighingCategory: domain.CategoryPerShedPartition, ExpectedAnimalCount: 1, Status: "completed"},
		{WeighingCategory: domain.CategoryPerShedPartition, ExpectedAnimalCount: 1, Status: "pending"},
	}, 2, 1, 1, 1)

	if got.IndividualExpectedCount != 3 || got.IndividualCompletedCount != 2 {
		t.Fatalf("individual progress=%+v want 2/3", got)
	}
	if got.PerScopeExpectedCount != 2 || got.PerScopeCompletedCount != 1 {
		t.Fatalf("per-shed progress=%+v want 1/2", got)
	}
	if got.WrongShedCount != 1 || got.MissingCount != 1 {
		t.Fatalf("attention counts=%+v want wrong=1 missing=1", got)
	}
	if got.RemainingCount != 2 {
		t.Fatalf("remaining=%d want 2", got.RemainingCount)
	}
}

func TestProgressClampsNegativeRemaining(t *testing.T) {
	got := progress([]domain.CampaignShed{{WeighingCategory: domain.CategoryIndividualAnimal, ExpectedAnimalCount: 1}}, 2, 0, 0, 0)
	if got.RemainingCount != 0 {
		t.Fatalf("remaining=%d want clamped 0", got.RemainingCount)
	}
}
