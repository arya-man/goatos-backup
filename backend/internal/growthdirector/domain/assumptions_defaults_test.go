package domain

import (
	"reflect"
	"testing"
)

// ON FIRST DEPLOYMENT NOTHING CHANGES (maintainer instruction 2026-09-19): the rows migrations
// 000252/000253 seed are exactly the figures the constants carried, so a tenant that never opens
// the drawer gets the reads it had. Pinned two ways: the settings resolved from NO rows equal the
// settings resolved from the SEEDED rows, and the seeded figures are the old constants.
func TestSeededAssumptionsEqualTheOldConstants(t *testing.T) {
	seeded := []AssumptionValue{
		{Key: AssumptionSaleReadyThresholdKg, Kind: AssumptionKindNumber, Value: 35},
		{Key: AssumptionSaleReadyLowerKg, Kind: AssumptionKindNumber, Value: 30},
		{Key: AssumptionLoadAgeAlertDays, Kind: AssumptionKindNumber, Value: 90},
		{Key: AssumptionSlowGrowthTargetGDay, Kind: AssumptionKindNumber, Value: 200},
		{Key: AssumptionBadScanLossGDay, Kind: AssumptionKindNumber, Value: 300},
		{Key: AssumptionDefaultPeriodDays, Kind: AssumptionKindNumber, Value: 15},
		{Key: AssumptionWeightBandEdgesKg, Kind: AssumptionKindNumberList, Values: []float64{15, 20, 25, 30, 35}},
	}
	if got, want := SettingsFrom(seeded), SettingsFrom(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded settings %+v differ from the no-row defaults %+v", got, want)
	}
	if got := BandLabelsFor(SettingsFrom(nil).BandEdgesKg); !reflect.DeepEqual(got, []string{"<15", "15-20", "20-25", "25-30", "30-35", "35+"}) {
		t.Fatalf("default band labels = %v", got)
	}
	// The old constants, spelled out so a change to either side is a visible diff here.
	if DefaultSaleReadyThresholdKg != 35 || DefaultSaleReadyLowerKg != 30 || DefaultLoadAgeAlertDays != 90 ||
		DefaultSlowGrowthTargetGDay != 200 || DefaultBadScanLossGDay != 300 || DefaultPeriodDaysAssumption != 15 {
		t.Fatalf("a default drifted from the constant it replaced")
	}
}
