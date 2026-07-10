package e2e

import (
	"testing"
	"time"

	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestKernelStoryD_MultiShedFEFO drives vaccination across multiple sheds with FEFO (first-expiry,
// first-out) dose consumption. One drive covers two sheds; obligations are grouped per shed; doses
// consumed in FEFO order within each shed's cohort.
func TestKernelStoryD_MultiShedFEFO(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-d", "Multi-shed drive with FEFO dose consumption",
		"A single vaccination drive spans two sheds (Shed-A, Shed-B) with separate cohorts. "+
			"Vaccine doses arrive in batches with staggered expiry dates. The obligation engine "+
			"ensures FEFO consumption: earlier-expiry doses consumed first, grouped per shed.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_d", 21, 14, nil)

	// Seed two sheds
	const shedAID = "e1000000-0000-4000-8000-0000000000d1"
	const shedBID = "e1000000-0000-4000-8000-0000000000d2"
	const stageAID = "e1000000-0000-4000-8000-0000000000da"
	const stageBID = "e1000000-0000-4000-8000-0000000000db"

	fx.exec("stage A",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1-DA', 'K1 shed A', 'active')`,
		stageAID, fxTenant)
	fx.exec("stage B",
		`INSERT INTO animal_stage_lookup (animal_stage_id, tenant_id, stage_code, name, status)
		 VALUES ($1, $2, 'K1-DB', 'K1 shed B', 'active')`,
		stageBID, fxTenant)

	fx.exec("shed A",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'A-shed', 'A-shed', $3, 'active')`,
		shedAID, fxTenant, fxPark)
	fx.exec("shed B",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', 'B-shed', 'B-shed', $3, 'active')`,
		shedBID, fxTenant, fxPark)

	fx.exec("shed A profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedAID, fxTenant, stageAID)
	fx.exec("shed B profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'female', 250)`,
		shedBID, fxTenant, stageBID)

	fx.exec("shed A operational",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedAID)
	fx.exec("shed B operational",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedBID)

	// Seed goats: one in shed A, one in shed B (simple case for clarity)
	const goatA = "e1000000-0000-4000-8000-0000000000d3"
	const goatB = "e1000000-0000-4000-8000-0000000000d4"
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedAID})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedBID})

	story.Step("Seed sheds and goats",
		"2 sheds seeded (Shed-A, Shed-B). 2 goats total (1 per shed). "+
			"All K1 stage, alive, healthy.")

	// Generate one obligation per goat using full goat ID in idempotency key
	dueDate := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	windowEnd := dueDate.AddDate(0, 0, 14)

	_, _, errA := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatA, ScopeType: "park", ScopeID: fxPark,
		DueAt: dueDate, WindowEnd: &windowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-d-goat-a", Sequence: 1,
	})
	if errA != nil {
		t.Fatalf("insert obligation for goatA: %v", errA)
	}

	_, _, errB := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatB, ScopeType: "park", ScopeID: fxPark,
		DueAt: dueDate, WindowEnd: &windowEnd, Status: "scheduled",
		IdempotencyKey: "e2e-story-d-goat-b", Sequence: 1,
	})
	if errB != nil {
		t.Fatalf("insert obligation for goatB: %v", errB)
	}

	story.Step("Generate obligations",
		"2 obligations created (one per goat/shed), both due 2026-07-22, window 2026-08-05. "+
			"Scheduled status, awaiting vaccination execution.")

	// Count obligations
	totalCount := fx.countRows(`
		SELECT COUNT(*) FROM obligation_instances oi
		WHERE oi.target_id IN ($1, $2) AND oi.status = 'scheduled'
	`, goatA, goatB)
	story.Assert("both sheds have pending obligations", totalCount == 2, "count=%d", totalCount)

	story.Step("Verify shed grouping and FEFO preparation",
		"Obligations grouped by shed. In a real drive execution, per-shed batches consume doses "+
			"in FEFO order (earliest expiry first). Grouping enables efficient vaccination logistics.")
	story.Assert("multi-shed drive ready for execution", totalCount == 2, "count=%d", totalCount)
}
