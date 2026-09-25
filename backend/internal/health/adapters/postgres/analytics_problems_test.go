package postgres

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// farmPenTypes is the Pen types register as migration 000437 seeds it. The tests below read it as
// DATA, the way the read does, so none of them teaches that these two are the only kinds of pen.
var farmPenTypes = []domain.HealthPenType{
	{Key: "elevated", Name: "Elevated", Active: true},
	{Key: "non_elevated", Name: "Non-elevated", Active: true},
}

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

	got := buildHealthProblems(byBreed, byPenType, byAge, farmPenTypes)

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
	got := buildHealthProblems(map[string]int64{"Beetal": 5}, map[string]int64{"elevated": 5}, map[string]int64{"over_1y": 5}, farmPenTypes)

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
	got := buildHealthProblems(byBreed, map[string]int64{"elevated": expected}, map[string]int64{"over_1y": expected}, farmPenTypes)

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
		farmPenTypes,
	)
	if got.ByBreed[0].Key != domain.HealthProblemBreedUnknown {
		t.Fatalf("first bar = %+v, want the unknown bucket, which is the biggest here", got.ByBreed[0])
	}
	if got.ByBreed[0].Label != domain.HealthProblemBreedUnknownLabel {
		t.Fatalf("unknown breed label = %q, want farm copy %q", got.ByBreed[0].Label, domain.HealthProblemBreedUnknownLabel)
	}
}

// A pen type the FARM adds on Configuration -> Pen types appears on the chart with its own name,
// in the register's order, with no code change -- the whole point of the register (maintainer
// instruction 2026-09-25). "Not set" stays last.
func TestAFarmAddedPenTypeIsItsOwnBarInRegisterOrder(t *testing.T) {
	types := []domain.HealthPenType{
		{Key: "slatted", Name: "Slatted floor", Active: true},
		{Key: "elevated", Name: "Elevated", Active: true},
		{Key: "non_elevated", Name: "Non-elevated", Active: true},
	}
	got := buildHealthProblems(
		map[string]int64{"Beetal": 10},
		map[string]int64{"slatted": 4, "elevated": 5, "unclassified": 1},
		map[string]int64{"over_1y": 10},
		types,
	)
	want := []domain.HealthAnalyticsProblemBucket{
		{Key: "slatted", Label: "Slatted floor", Cases: 4},
		{Key: "elevated", Label: "Elevated", Cases: 5},
		{Key: "non_elevated", Label: "Non-elevated", Cases: 0},
		{Key: domain.HealthPenTypeUnclassified, Label: domain.HealthPenTypeUnclassifiedLabel, Cases: 1},
	}
	if len(got.ByPenType) != len(want) {
		t.Fatalf("pen type bars = %+v, want %+v", got.ByPenType, want)
	}
	for i := range want {
		if got.ByPenType[i] != want[i] {
			t.Fatalf("bar %d = %+v, want %+v", i, got.ByPenType[i], want[i])
		}
	}
	if got.Total != 10 {
		t.Fatalf("total = %d, want 10", got.Total)
	}
}

// An ARCHIVED type is kept off the chart while no case sits in its pens, and shown -- under its
// own name -- while some do. Configuration refuses to archive a type any pen holds, so this is
// the defensive path: the total must never lose a case whatever the register says. A key the
// register does not name at all still counts toward the total.
func TestArchivedAndUnknownPenTypesNeverLoseACase(t *testing.T) {
	types := []domain.HealthPenType{
		{Key: "elevated", Name: "Elevated", Active: true},
		{Key: "old_idle", Name: "Old idle", Active: false},
		{Key: "old_used", Name: "Old used", Active: false},
	}
	got := buildHealthProblems(
		map[string]int64{"Beetal": 9},
		map[string]int64{"elevated": 3, "old_used": 2, "stray": 4},
		map[string]int64{"over_1y": 9},
		types,
	)
	keys := []string{}
	for _, bucket := range got.ByPenType {
		keys = append(keys, bucket.Key)
		if bucket.Key == "old_used" && bucket.Label != "Old used" {
			t.Fatalf("archived type lost its name: %+v", bucket)
		}
	}
	if strings.Join(keys, ",") != "elevated,old_used,stray,unclassified" {
		t.Fatalf("pen type keys = %v, want elevated,old_used,stray,unclassified", keys)
	}
	if got.Total != 9 {
		t.Fatalf("total = %d, want 9: no case may be dropped", got.Total)
	}
}
