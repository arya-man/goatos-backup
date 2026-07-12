package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryQ_SpeciesSplitBatch proves goats and sheep in the same shed never share one SM-4
// batch — species is part of the sweep group key (Batch story E / scenario 10).
func TestKernelStoryQ_SpeciesSplitBatch(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-q", "Species split: goat and sheep never same batch",
		"Two goats in one shed and two sheep in another, all due for the same rule. The sweeper must form "+
			"two separate shed drives — one per species — never mixing goat and sheep in one batch.")
	defer story.Finish()
	story.Certify("backend kernel")

	const (
		shedA   = "f2000000-0000-4000-8000-000000000001"
		shedB   = "f2000000-0000-4000-8000-000000000002"
		stageA  = "f2000000-0000-4000-8000-00000000000a"
		stageB  = "f2000000-0000-4000-8000-00000000000b"
		itemID  = "f2000000-0000-4000-8000-000000000020"
	)

	fx.SeedShed(shedA, "E2E-Q-GOAT", stageA)
	fx.SeedAdultShed(shedB, "E2E-Q-SHEEP", stageB, "K1-QS")
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_q", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: "f2000000-0000-4000-8000-000000000010", ShedID: shedA, DOB: &dob, Species: "goat"})
	fx.SeedGoat(GoatSpec{GoatID: "f2000000-0000-4000-8000-000000000011", ShedID: shedA, DOB: &dob, Species: "goat"})
	fx.SeedGoat(GoatSpec{GoatID: "f2000000-0000-4000-8000-000000000012", ShedID: shedB, DOB: &dob, Species: "sheep"})
	fx.SeedGoat(GoatSpec{GoatID: "f2000000-0000-4000-8000-000000000013", ShedID: shedB, DOB: &dob, Species: "sheep"})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-Q', 'E2E Story Q vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock goat shed",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f2000000-0000-4000-8000-000000000021", fxTenant, itemID, shedA)
	fx.exec("vaccine stock sheep shed",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 20, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`,
		"f2000000-0000-4000-8000-000000000022", fxTenant, itemID, shedB)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	res, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("four obligations generated", res.Generated == 4, "generated=%d", res.Generated)

	story.Step("Sweep separate sheds",
		"SM-4 must create two shed batches (goat shed + sheep shed), not one mixed batch.")
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1))
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("two species-specific drives formed", sweepRes.Batches == 2, "batches=%d", sweepRes.Batches)
	story.Assert("all four obligations attached across drives", sweepRes.Obligations == 4, "obligations=%d", sweepRes.Obligations)

	mixedBatches := fx.countRows(`
SELECT count(*) FROM (
  SELECT b.batch_id
  FROM obligation_instances oi
  JOIN obligation_batches b ON b.batch_id = oi.batch_id
  JOIN goats g ON g.goat_id = oi.target_id
  WHERE oi.tenant_id = $1 AND oi.protocol_version_id = $2
  GROUP BY b.batch_id
  HAVING count(DISTINCT g.species) > 1
) mixed`, fxTenant, versionID)
	story.Assert("no batch mixes goat and sheep", mixedBatches == 0, "mixed_species_batches=%d", mixedBatches)
}
