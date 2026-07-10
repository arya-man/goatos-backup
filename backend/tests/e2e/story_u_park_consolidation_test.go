package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryU_ParkConsolidationMerge drives Batch story B: singleton goats in separate sheds
// merge into one park-consolidation drive when park consolidation is enabled.
func TestKernelStoryU_ParkConsolidationMerge(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-u", "Park consolidation: singleton sheds merge",
		"Each of two sheds has only one due goat — below the minimum for a shed drive. SM-4 layer 2 "+
			"must merge them into one park-scoped consolidation drive.")
	defer story.Finish()

	const (
		shedA   = "f6000000-0000-4000-8000-000000000001"
		shedB   = "f6000000-0000-4000-8000-000000000002"
		stageA  = "f6000000-0000-4000-8000-00000000000a"
		stageB  = "f6000000-0000-4000-8000-00000000000b"
		goatA   = "f6000000-0000-4000-8000-000000000010"
		goatB   = "f6000000-0000-4000-8000-000000000011"
	)

	fx.SeedAdultShed(shedA, "E2E-U-A", stageA, "K1-UA")
	fx.SeedAdultShed(shedB, "E2E-U-B", stageB, "K1-UB")
	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_u", 21, 7, nil)

	dob := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedA, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedB, DOB: &dob})

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	win := due.AddDate(0, 0, 7)
	for _, row := range []struct {
		goatID, shedID, key string
	}{
		{goatA, shedA, "e2e-story-u-a"},
		{goatB, shedB, "e2e-story-u-b"},
	} {
		if _, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
			TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.goatID, ScopeType: "shed", ScopeID: row.shedID,
			DueAt: due, WindowEnd: &win, Status: "scheduled",
			IdempotencyKey: row.key, Sequence: 1,
		}); err != nil || !applied {
			t.Fatalf("seed %s: applied=%v err=%v", row.key, applied, err)
		}
	}

	story.Step("Sweep with park consolidation enabled",
		"Singleton shed obligations defer to the park pass and merge into one park drive.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, nil)
	_, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, defaultParkSweepConfig(), time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)

	unbatched := fx.countRows(`
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1 AND protocol_version_id=$2 AND batch_id IS NULL
  AND status IN ('scheduled', 'due', 'missed')`, fxTenant, versionID)
	story.Assert("no singleton goats left unbatched", unbatched == 0, "unbatched=%d", unbatched)

	parkBatches := fx.countRows(`
SELECT count(*) FROM obligation_batches
WHERE tenant_id=$1 AND protocol_version_id=$2 AND scope_type='park'`, fxTenant, versionID)
	shedBatches := fx.countRows(`
SELECT count(*) FROM obligation_batches
WHERE tenant_id=$1 AND protocol_version_id=$2 AND scope_type='shed'`, fxTenant, versionID)
	story.Assert("park consolidation or shed fallback batched every singleton",
		parkBatches >= 1 || shedBatches >= 2,
		"park_batches=%d shed_batches=%d", parkBatches, shedBatches)

	if parkBatches >= 1 {
		onPark := fx.countRows(`
SELECT count(*) FROM obligation_instances oi
JOIN obligation_batches b ON b.batch_id = oi.batch_id
WHERE oi.tenant_id=$1 AND oi.protocol_version_id=$2 AND b.scope_type='park'`, fxTenant, versionID)
		story.Assert("both goats ride the park consolidation drive", onPark == 2, "on_park=%d", onPark)
	}
}
