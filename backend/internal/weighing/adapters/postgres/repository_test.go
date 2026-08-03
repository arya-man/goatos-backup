package postgres

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// An individual_animal bucket carries NO expected animal total: weighing is
// free-flow, so whatever walks through the shed gets weighed and there is no
// roster to weigh against. progress() therefore never sums
// expected_animal_count (which the write path stores as 0) into
// IndividualExpectedCount -- it used to, back when the write path stamped a
// literal 1 per bucket, and a shed where five animals were weighed then reported
// an expectation of one.
//
// A per_shed_partition bucket is different and still counted: the unit there is
// the BUCKET (one lump-sum weight per bucket), so counting buckets is a real
// expectation, not an invented one.
func TestProgressClaimsNoIndividualAnimalExpectation(t *testing.T) {
	got := progress([]domain.CampaignShed{
		{WeighingCategory: domain.CategoryIndividualAnimal, ExpectedAnimalCount: 3},
		{WeighingCategory: domain.CategoryPerShedPartition, ExpectedAnimalCount: 1, Status: "completed"},
		{WeighingCategory: domain.CategoryPerShedPartition, ExpectedAnimalCount: 1, Status: "pending"},
	}, 2, 1, 1, 1)

	// Even when a legacy row still holds a non-zero expected_animal_count, no
	// animal expectation may be derived from it.
	if got.IndividualExpectedCount != 0 {
		t.Fatalf("individual expected=%d, want 0: free-flow weighing has no expected animal total", got.IndividualExpectedCount)
	}
	if got.IndividualCompletedCount != 2 {
		t.Fatalf("individual completed=%d want 2", got.IndividualCompletedCount)
	}
	if got.PerScopeExpectedCount != 2 || got.PerScopeCompletedCount != 1 {
		t.Fatalf("per-shed progress=%+v want 1/2", got)
	}
	if got.WrongShedCount != 1 || got.MissingCount != 1 {
		t.Fatalf("attention counts=%+v want wrong=1 missing=1", got)
	}
	// Only the lump-sum buckets can be "still open": 2 buckets - 1 completed = 1.
	// The individual side contributes nothing, because there is nothing to expect.
	if got.RemainingCount != 1 {
		t.Fatalf("remaining=%d want 1 (lump-sum buckets only)", got.RemainingCount)
	}
}

func TestProgressClampsNegativeRemaining(t *testing.T) {
	got := progress([]domain.CampaignShed{{WeighingCategory: domain.CategoryPerShedPartition, ExpectedAnimalCount: 1}}, 0, 2, 0, 0)
	if got.RemainingCount != 0 {
		t.Fatalf("remaining=%d want clamped 0", got.RemainingCount)
	}
}
