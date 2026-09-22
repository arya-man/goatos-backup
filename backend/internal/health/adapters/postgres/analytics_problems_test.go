package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// THE PROPERTY THE WHOLE SECTION RESTS ON: four charts that cut one total must each add back
// up to it. They only can because every arm buckets its unknowns instead of dropping them --
// a case whose animal has no breed, whose pen nobody has typed, or whose animal has no date of
// birth is counted under a named bucket on each arm.
//
// Dropping any of them would leave a chart quietly answering a smaller question than the
// headline above it, and a reader comparing two bars would be comparing them inside a total
// that is not the total on screen.
func TestHealthProblemBreakdownsEachSumToTheTotal(t *testing.T) {
	byBreed := map[string]int64{"Beetal": 12, "Barbari": 7, "unknown": 4}
	byPenType := map[string]int64{"elevated": 15, "non_elevated": 6, "unclassified": 2}
	byAge := map[string]int64{"d31_90": 9, "over_1y": 10, "unknown": 4}

	got := buildHealthProblems(byBreed, byPenType, byAge)

	if got.Total != 23 {
		t.Fatalf("total = %d, want 23 (the pen-type arm, which is complete by construction)", got.Total)
	}
	for name, buckets := range map[string][]domain.HealthAnalyticsProblemBucket{
		"by_breed":    got.ByBreed,
		"by_pen_type": got.ByPenType,
		"by_age":      got.ByAge,
	} {
		var sum int64
		for _, bucket := range buckets {
			sum += bucket.Cases
		}
		if sum != got.Total {
			t.Fatalf("%s sums to %d but the headline says %d; a breakdown that does not add up to its own total is answering a different question", name, sum, got.Total)
		}
	}
}

func TestHealthProblemsPenTypeUsesCaseSnapshotNotCurrentGoatPartition(t *testing.T) {
	if strings.Contains(healthAnalyticsProblemsSQL, "goat_shed_partitions") {
		t.Fatal("health problem pen-type breakdown must not follow the goat's current partition after the case was opened")
	}
	if !strings.Contains(healthAnalyticsProblemsSQL, "hc.partition_label") {
		t.Fatal("health problem pen-type breakdown must resolve the pen from the health_cases partition snapshot")
	}
}

// A pen type with no cases is a ZERO, never a missing bar. An empty side that vanishes makes the
// chart read as though the farm only has one kind of pen.
func TestPenTypeSpineKeepsEmptySidesAsZeros(t *testing.T) {
	got := buildHealthProblems(map[string]int64{"Beetal": 5}, map[string]int64{"elevated": 5}, map[string]int64{"over_1y": 5})

	if len(got.ByPenType) != 3 {
		t.Fatalf("pen type has %d bars, want all 3 even when two are empty", len(got.ByPenType))
	}
	if got.ByPenType[0].Key != "elevated" || got.ByPenType[0].Cases != 5 {
		t.Fatalf("first bar = %+v, want elevated with 5", got.ByPenType[0])
	}
	if got.ByPenType[1].Cases != 0 || got.ByPenType[2].Cases != 0 {
		t.Fatalf("empty sides should be zeros, got %+v", got.ByPenType)
	}
	if len(got.ByAge) != len(domain.HealthProblemAgeBandOrder) {
		t.Fatalf("age has %d bands, want the whole spine: the gap between bands is the shape being read", len(got.ByAge))
	}
}

// The breed arm is the only capped one, and the cap is taken AFTER the total. A farm with more
// breeds than the cap must still see the true count above the chart -- summing the visible bars
// to find a headline is the banned read-time rollup, one card wide.
func TestBreedCapNeverMovesTheHeadline(t *testing.T) {
	byBreed := map[string]int64{}
	var expected int64
	for i := 0; i < domain.HealthAnalyticsBreedLimit+7; i++ {
		byBreed[string(rune('A'+i))+"-breed"] = int64(i + 1)
		expected += int64(i + 1)
	}
	got := buildHealthProblems(byBreed, map[string]int64{"elevated": expected}, map[string]int64{"over_1y": expected})

	if len(got.ByBreed) != domain.HealthAnalyticsBreedLimit {
		t.Fatalf("breed bars = %d, want the cap of %d", len(got.ByBreed), domain.HealthAnalyticsBreedLimit)
	}
	if got.Total != expected {
		t.Fatalf("total = %d, want %d; the cap must not reach the headline", got.Total, expected)
	}
	var shown int64
	for _, bucket := range got.ByBreed {
		shown += bucket.Cases
	}
	if shown >= got.Total {
		t.Fatalf("the capped bars sum to %d and the total is %d; this test is not exercising the cap", shown, got.Total)
	}
	// Biggest first, so the cap keeps the busiest breeds rather than an arbitrary slice.
	for i := 1; i < len(got.ByBreed); i++ {
		if got.ByBreed[i-1].Cases < got.ByBreed[i].Cases {
			t.Fatalf("breed bars are not ordered by size: %+v", got.ByBreed)
		}
	}
}

// "Breed not recorded" is a finding, not a footnote: it sorts with the rest, so when it is the
// biggest bar the reader sees that the farm's breed data is the problem.
func TestUnknownBreedIsNamedAndSortsOnItsSize(t *testing.T) {
	got := buildHealthProblems(
		map[string]int64{"Beetal": 3, domain.HealthProblemBreedUnknown: 30},
		map[string]int64{"elevated": 33},
		map[string]int64{"over_1y": 33},
	)
	if got.ByBreed[0].Key != domain.HealthProblemBreedUnknown {
		t.Fatalf("first bar = %+v, want the unknown bucket, which is the biggest here", got.ByBreed[0])
	}
	if got.ByBreed[0].Label != domain.HealthProblemBreedUnknownLabel {
		t.Fatalf("unknown breed label = %q, want farm copy %q", got.ByBreed[0].Label, domain.HealthProblemBreedUnknownLabel)
	}
}
