package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryW_SickMissedSweepSafety guards Bug 1 from vaccination-vertical-bugs-and-contradictions.txt:
// a goat with a MISSED obligation that becomes clinically sick must NOT be batched into an active
// shed drive with healthy shed-mates. Clinical recheck must defer or exclude missed rows at sweep time.
func TestKernelStoryW_SickMissedSweepSafety(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-w", "Clinical safety: sick + missed goat excluded from drive",
		"A goat's dose is already missed. The goat then becomes sick. The sweeper must NOT batch that "+
			"sick missed goat with healthy shed-mates — clinical defer/exclusion must win over missed catch-up batching.")
	defer story.Finish()

	const (
		shedID   = "f8000000-0000-4000-8000-000000000001"
		stageID  = "f8000000-0000-4000-8000-000000000002"
		sickID   = "f8000000-0000-4000-8000-000000000010"
		healthy1 = "f8000000-0000-4000-8000-000000000011"
		healthy2 = "f8000000-0000-4000-8000-000000000012"
		itemID   = "f8000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-W", stageID)
	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_w", 21, 14, []string{"sick", "quarantine", "icu"})

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: sickID, ShedID: shedID, DOB: &dob, Health: "sick"})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-W', 'E2E Story W vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f8000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	story.Step("Seed one missed sick goat and two healthy scheduled doses",
		"The sick goat already carries a missed obligation; healthy goats are schedulable for catch-up batching.")
	due := now.AddDate(0, 0, -10)
	win := due.AddDate(0, 0, 14)
	for _, row := range []struct {
		goatID, key, status string
	}{
		{sickID, "e2e-story-w-missed", "missed"},
		{healthy1, "e2e-story-w-h1", "scheduled"},
		{healthy2, "e2e-story-w-h2", "scheduled"},
	} {
		_, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
			TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: row.goatID, ScopeType: "shed", ScopeID: shedID,
			DueAt: due, WindowEnd: &win, Status: row.status,
			IdempotencyKey: row.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed %s: applied=%v err=%v", row.key, applied, err)
		}
	}

	story.Step("Sweep must not batch the sick missed goat",
		"Only the two healthy goats may attach to the shed drive; the sick missed goat stays off the batch.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("drive batches only healthy goats", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)

	sickBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, sickID)
	story.Assert("sick missed goat is NOT on any batch (clinical safety)", sickBatch == "", "batch_id=%q", sickBatch)
}
