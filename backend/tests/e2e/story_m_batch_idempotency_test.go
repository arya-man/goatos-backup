package e2e

import (
	"errors"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/vaccination/ports"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestKernelStoryM_BatchIdempotency drives the drive-submission idempotency contract through the
// REAL RecordCompletion write path, covering the three cases the platform mandates for every write
// (see AGENTS.md idempotency rule): first call applies; exact replay (same key + same payload)
// returns the original result with no duplicate side effect; same key + DIFFERENT payload is rejected
// (ErrIdempotencyConflict) with no new side effect. A re-submitted drive must never double-complete
// a goat or double-consume a dose.
func TestKernelStoryM_BatchIdempotency(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-m", "Batch idempotency: re-submit a drive, no duplicate completions",
		"A drive submission carries an idempotency key per goat dose. The kernel must make re-submission "+
			"safe: the first submit records the completion; an exact replay of the same submit returns the "+
			"original completion without creating a duplicate; a replay that reuses the key with a different "+
			"payload is rejected outright. Flaky networks and retried mobile submits can never double-count "+
			"a vaccination.")
	defer story.Finish()

	const (
		shedID     = "ed000000-0000-4000-8000-000000000001"
		stageID    = "ed000000-0000-4000-8000-000000000002"
		operatorID = "ed000000-0000-4000-8000-000000000003"
		parkHeadID = "ed000000-0000-4000-8000-000000000004"
		verifierID = "ed000000-0000-4000-8000-000000000005"
		goatID     = "ed000000-0000-4000-8000-000000000006"
		itemID     = "ed000000-0000-4000-8000-000000000008"
		lotID      = "ed000000-0000-4000-8000-000000000009"
	)

	story.Step("Seed a real one-shed drive (generate + sweep)",
		"One shed/workforce/protocol/goat/stock topology, generated and swept into a single drive batch "+
			"-- the same real path Story C uses.")
	fx.SeedShed(shedID, "E2E-M", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)
	versionID, _ := fx.PublishSimpleProtocol("vaccination.e2e.story_m", 21, 0, nil)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-M', 'E2E Story M vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, shedID)

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	if _, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now); err != nil {
		t.Fatalf("generate: %v", err)
	}
	sweeper := oblapp.NewSweeperService(fx.Obl, nil, fx.Inv)
	if _, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{VaccineItemID: itemID, DosesPerGoat: 1}, now.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	oblID := fx.scanText(`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2`, fxTenant, goatID)

	svc := vaccapp.NewService(fx.Vacc)
	doses := int32(1)
	batch, lot := batchID, lotID
	const key = "e2e-story-m-submit"
	submit := func(routeSite string) (string, bool, error) {
		return svc.RecordCompletion(fx.Ctx, vaccdomain.NewCompletion{
			TenantID: fxTenant, ObligationID: oblID, GoatID: goatID, BatchID: &batch,
			VaccineInventoryLotID: &lot, Doses: &doses, RouteSite: routeSite, AdministeredAt: now,
			ColdChainVerified: true, Status: "recorded", IdempotencyKey: key,
		})
	}

	story.Step("First submit applies",
		"The drive's dose submission records one completion under its idempotency key.")
	cid1, applied1, err := submit("SC")
	story.Assert("first submit recorded a completion", err == nil && applied1 && cid1 != "", "cid=%q applied=%v err=%v", cid1, applied1, err)
	afterFirst := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND idempotency_key=$2`, fxTenant, key)
	story.Assert("exactly one completion row exists", afterFirst == 1, "rows=%d", afterFirst)

	story.Step("Exact replay is a durable no-op",
		"Re-submitting the identical payload under the same key must NOT create a second completion "+
			"(applied=false), and the completion count stays at one.")
	_, applied2, err := submit("SC")
	story.Assert("exact replay ran without error", err == nil, "err=%v", err)
	story.Assert("exact replay did not apply a new side effect (applied=false)", !applied2, "applied=%v", applied2)
	afterReplay := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND idempotency_key=$2`, fxTenant, key)
	story.Assert("still exactly one completion row after exact replay", afterReplay == 1, "rows=%d", afterReplay)

	story.Step("Same key, DIFFERENT payload is rejected",
		"Reusing the key with a changed payload (route site SC → IM) must be rejected with an "+
			"idempotency conflict and must NOT create or mutate any completion.")
	_, applied3, err := submit("IM")
	story.Assert("same-key-different-payload is rejected with ErrIdempotencyConflict", errors.Is(err, ports.ErrIdempotencyConflict), "err=%v", err)
	story.Assert("the rejected replay applied nothing", !applied3, "applied=%v", applied3)
	afterConflict := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND idempotency_key=$2`, fxTenant, key)
	story.Assert("still exactly one completion row after the rejected replay", afterConflict == 1, "rows=%d", afterConflict)
	routeStored := fx.scanText(`SELECT route_site FROM vaccination_completions WHERE tenant_id=$1 AND idempotency_key=$2`, fxTenant, key)
	story.Assert("the original payload is intact (route unchanged by the rejected replay)", routeStored == "SC", "route_site=%q", routeStored)

	story.Step("No duplicate obligation completion downstream",
		"The obligation still maps to exactly one recorded completion -- the drive cannot double-complete "+
			"the goat no matter how many times it is re-submitted.")
	oblCompletions := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, oblID)
	story.Assert("exactly one completion for the obligation", oblCompletions == 1, "rows=%d", oblCompletions)
}
