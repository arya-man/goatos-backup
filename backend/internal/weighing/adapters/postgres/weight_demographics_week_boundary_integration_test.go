package postgres

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The scanned half of the pen-week / load-week series is computed once (animal_gain_week_pen, the
// 2026-09-24 burst fix) and read by both arms. This pins its WEEK PAGE BOUNDARY: a pair is bucketed
// by its LATER weigh's Monday in Asia/Kolkata, so a weigh at Mon 13 Jul 00:10 IST (Sun 12 Jul 18:40
// UTC) closes a leg in the week of 13 Jul, not 6 Jul, and the next leg lands in the week of 20 Jul.
// Each (animal, week) is counted exactly once, and the pen row and the load row agree.
//
//	Sat 11 Jul 11:30 IST 10.0 kg -> Mon 13 Jul 00:10 IST 11.0 kg : 1000 g / 2 days = 500 g/day, week 2026-07-13
//	Mon 13 Jul 00:10 IST 11.0 kg -> Mon 20 Jul 11:30 IST 11.7 kg :  700 g / 7 days = 100 g/day, week 2026-07-20
func TestScannedPenWeekGainAtTheISTWeekPageBoundary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	const (
		boundaryShed   = "00000000-0000-4000-8000-00000000c0b1"
		boundaryBucket = "00000000-0000-4000-8000-00000000c0b2"
		boundaryTag    = "BOUNDARY-KID-1"
	)
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'Boundary Shed', $3::uuid, 'active')
ON CONFLICT (tenant_id, location_id) DO NOTHING`, boundaryShed, repoTenant, repoPark)
	seedLoadBucket(t, ctx, pool, boundaryBucket, repoCampaign, boundaryShed, "individual_animal")
	seedLoadTag(t, ctx, pool, boundaryShed, "LOAD-BOUNDARY", "Boundary Supplier")
	seedLoadIndividualWeigh(t, ctx, pool, boundaryBucket, boundaryTag, 10.0, time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC))
	seedLoadIndividualWeigh(t, ctx, pool, boundaryBucket, boundaryTag, 11.0, time.Date(2026, 7, 12, 18, 40, 0, 0, time.UTC))
	seedLoadIndividualWeigh(t, ctx, pool, boundaryBucket, boundaryTag, 11.7, time.Date(2026, 7, 20, 6, 0, 0, 0, time.UTC))

	repo := NewRepository(pool, 30*time.Second)
	demo, err := repo.GetWeightDemographics(ctx, repoTenant, []string{repoPark},
		time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC), time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
		"", "", "", "weekly_gain", nil, domain.TimeScope{})
	if err != nil {
		t.Fatalf("GetWeightDemographics: %v", err)
	}
	want := map[string]float64{"2026-07-13": 500, "2026-07-20": 100}
	pens := map[string]domain.WeightGainPenWeekBucket{}
	for _, row := range demo.GainByPenWeek {
		if row.LocationID != boundaryShed {
			continue
		}
		if _, dup := pens[row.WeekStart]; dup {
			t.Fatalf("pen week %s emitted twice: %#v", row.WeekStart, demo.GainByPenWeek)
		}
		pens[row.WeekStart] = row
	}
	if len(pens) != len(want) {
		t.Fatalf("pen weeks = %#v, want exactly %v", pens, want)
	}
	for week, g := range want {
		row, ok := pens[week]
		if !ok || row.Animals != 1 || math.Abs(row.AverageGainGPerDay-g) > 0.01 {
			t.Fatalf("pen week %s = %#v, want 1 animal at %.0f g/day", week, row, g)
		}
	}
	loads := map[string]domain.WeightGainLoadWeekBucket{}
	for _, row := range demo.GainByLoadWeek {
		if row.LoadRef == "LOAD-BOUNDARY" {
			loads[row.WeekStart] = row
		}
	}
	if len(loads) != len(want) {
		t.Fatalf("load weeks = %#v, want exactly %v", loads, want)
	}
	for week, g := range want {
		row := loads[week]
		if row.Animals != 1 || math.Abs(row.AverageGainGPerDay-g) > 0.01 || row.OwnerName != "Boundary Supplier" {
			t.Fatalf("load week %s = %#v, want the pen row (1 animal at %.0f g/day)", week, row, g)
		}
	}
}
