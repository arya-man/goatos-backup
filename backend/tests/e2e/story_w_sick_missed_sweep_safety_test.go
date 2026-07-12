package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryW_SickMissedSweepSafety proves a generated missed dose cannot enter a drive after
// the production health-change/recheck path marks the animal clinically blocked.
func TestKernelStoryW_SickMissedSweepSafety(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-w", "Clinical safety: sick + missed goat excluded from drive",
		"SM-1 generates three real obligations. Time advances until one becomes missed; the identity "+
			"health command emits goat.health.changed, and the production recheck/sweeper path excludes that "+
			"animal while batching the two healthy shed-mates.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedID   = "f8000000-0000-4000-8000-000000000001"
		stageID  = "f8000000-0000-4000-8000-000000000002"
		sickID   = "f8000000-0000-4000-8000-000000000010"
		healthy1 = "f8000000-0000-4000-8000-000000000011"
		healthy2 = "f8000000-0000-4000-8000-000000000012"
		itemID   = "f8000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedID, "E2E-W", stageID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_w", 21, 14, []string{"sick", "quarantine", "icu"})
	now := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	sickDue := now.AddDate(0, 0, -20)
	healthyDue := now.AddDate(0, 0, -10)
	sickDOB := sickDue.AddDate(0, 0, -21)
	healthyDOB := healthyDue.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: sickID, ShedID: shedID, DOB: &sickDOB})
	fx.SeedGoat(GoatSpec{GoatID: healthy1, ShedID: shedID, DOB: &healthyDOB})
	fx.SeedGoat(GoatSpec{GoatID: healthy2, ShedID: shedID, DOB: &healthyDOB})
	fx.exec("vaccine item", `INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		VALUES ($1, $2, 'VAC-E2E-W', 'E2E Story W vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock", `INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f8000000-0000-4000-8000-000000000021", fxTenant, itemID, shedID)

	story.Step("Generate all three obligations while their windows are open",
		"Each goat.created event reaches SM-1. The older due date belongs only to the goat that will later become sick.")
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, sickID, sickDue)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, healthy1, healthyDue)
	fx.PublishGoatEvent(vaccapp.EventGoatCreated, healthy2, healthyDue)

	story.Step("Fast-forward missed detection, then apply sickness through identity",
		"MarkMissed advances to July 20, closing only the oldest window. The production health command emits and dispatches goat.health.changed.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	marked, err := sweeper.MarkMissed(fx.Ctx, fxTenant, now)
	story.Assert("exactly the old obligation became missed", err == nil && marked == 1, "marked=%d err=%v", marked, err)
	fx.ChangeGoatHealth(sickID, "sick", "story-w-sick", now)

	story.Step("Sweep batches only clinically clear goats",
		"The real SM-4 sweeper may batch the two healthy obligations but must leave the sick goat unbatched regardless of its prior missed state.")
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("drive batches only two healthy goats", sweepRes.Obligations == 2, "obligations=%d", sweepRes.Obligations)
	sickBatch := fx.scanText(`SELECT COALESCE(batch_id::text, '') FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, sickID)
	story.Assert("sick goat is not on any batch", sickBatch == "", "batch_id=%q", sickBatch)
}
