package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestReminderCadenceShedVaccineEnrichment verifies that shed and vaccine labels are extracted from
// the underlying obligations and populated in the fire. This is the meaningful-notification-copy fix
// (docs/decisions/2026-08-02-meaningful-notification-copy.md): pushes now name the specific sheds
// and vaccines involved, not just an abstract count.
func TestReminderCadenceShedVaccineEnrichment(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 15*time.Second)

	protocolID := "86000000-0000-4000-8000-0000000f4101"
	versionID := "86000000-0000-4000-8000-0000000f4102"
	ruleID := "86000000-0000-4000-8000-0000000f4103"

	// Three DISTINCT vaccines, one per shed. Each gets its own protocol rule plus a
	// protocol_rule_dimensions row carrying the vaccine name, which is what the drive projection
	// renders as its vaccine label. Reusing one rule (as this fixture originally did) could only ever
	// produce a single vaccine label and made the 3-vaccine cap assertion unreachable.
	vaccineRuleIDs := []string{
		"86000000-0000-4000-8000-0000000f4131",
		"86000000-0000-4000-8000-0000000f4132",
		"86000000-0000-4000-8000-0000000f4133",
	}
	// Labels are deduped and sorted, then capped, so "ET+TT" is deliberately the alphabetically
	// first name: it must survive the cap for the human-readable-name assertion below to mean
	// anything.
	vaccineNames := []string{"ET+TT", "FMD", "PPR Booster"}
	vaccineProtocolIDs := []string{
		"86000000-0000-4000-8000-0000000f4141",
		"86000000-0000-4000-8000-0000000f4142",
		"86000000-0000-4000-8000-0000000f4143",
	}
	vaccineVersionIDs := []string{
		"86000000-0000-4000-8000-0000000f4151",
		"86000000-0000-4000-8000-0000000f4152",
		"86000000-0000-4000-8000-0000000f4153",
	}
	vaccineSeedObligationIDs := []string{
		"86000000-0000-4000-8000-0000000f4161",
		"86000000-0000-4000-8000-0000000f4162",
		"86000000-0000-4000-8000-0000000f4163",
	}

	// One park with three sheds and three different vaccines, to test capping behavior.
	parkMulti := "86000000-0000-4000-8000-0000000f4701"
	shedMulti1 := "86000000-0000-4000-8000-0000000f4711"
	shedMulti2 := "86000000-0000-4000-8000-0000000f4712"
	shedMulti3 := "86000000-0000-4000-8000-0000000f4713"

	batchMulti1 := "86000000-0000-4000-8000-0000000f4201"
	batchMulti2 := "86000000-0000-4000-8000-0000000f4202"
	batchMulti3 := "86000000-0000-4000-8000-0000000f4203"

	oblMulti1 := "86000000-0000-4000-8000-0000000f4301"
	oblMulti2 := "86000000-0000-4000-8000-0000000f4302"
	oblMulti3 := "86000000-0000-4000-8000-0000000f4303"

	goatMulti1 := "86000000-0000-4000-8000-0000000f4401"
	goatMulti2 := "86000000-0000-4000-8000-0000000f4402"
	goatMulti3 := "86000000-0000-4000-8000-0000000f4403"

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute) // evening: due_today rung has fired
	dueToday := dayStart.Add(10 * time.Hour)

	// Seed protocol/version/rule.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID,
		"86000000-0000-4000-8000-0000000f4001", dayStart.AddDate(0, 0, -30))

	// Seed goats.
	for _, g := range []string{goatMulti1, goatMulti2, goatMulti3} {
		seedCalendarGoat(t, ctx, pool, g)
	}

	// Seed one protocol/version/rule + named vaccine dimension per shed. Published protocol config is
	// immutable, so each extra vaccine needs its own version rather than an extra rule on versionID.
	for i, rid := range vaccineRuleIDs {
		seedVaccinationObligation(t, ctx, pool, vaccineProtocolIDs[i], vaccineVersionIDs[i], rid,
			vaccineSeedObligationIDs[i], dayStart.AddDate(0, 0, -30))
		seedProtocolRuleDimension(t, ctx, pool, vaccineVersionIDs[i], rid, "enrich-"+vaccineNames[i], vaccineNames[i])
	}
	// MULTI-SHED, MULTI-VACCINE: 3 obligations across 3 sheds with 3 different vaccines.
	// Obligation 1: ET+TT in shed 1
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, vaccineRuleIDs[0], oblMulti1,
		goatMulti1, shedMulti1, parkMulti, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchMulti1, versionID, parkMulti, shedMulti1, dueToday, oblMulti1)

	// Obligation 2: FMD in shed 2
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, vaccineRuleIDs[1], oblMulti2,
		goatMulti2, shedMulti2, parkMulti, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchMulti2, versionID, parkMulti, shedMulti2, dueToday, oblMulti2)

	// Obligation 3: PPR Booster in shed 3
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, vaccineRuleIDs[2], oblMulti3,
		goatMulti3, shedMulti3, parkMulti, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchMulti3, versionID, parkMulti, shedMulti3, dueToday, oblMulti3)

	// ---- Sweep and verify shed/vaccine enrichment. --------
	fires, err := repo.SweepReminderCadence(ctx, ports.ReminderCadenceQuery{
		TenantID: testTenantID,
		Now:      evalNow,
		Limit:    200,
	})
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}

	// Should have 1 fire (all 3 obligations collapse into one fire for the multi-park/day/slot).
	if len(fires) != 1 {
		t.Fatalf("sweep returned %d fires, want 1", len(fires))
	}

	fire := fires[0]

	// Verify shed labels are populated and capped.
	if len(fire.ShedLabels) == 0 {
		t.Fatalf("fire.ShedLabels is empty, want populated shed names")
	}
	// With 3 sheds and a cap of 2, should have 2 items: 2 shednames + "and 1 more".
	if len(fire.ShedLabels) != 2 {
		t.Fatalf("fire.ShedLabels has %d items, want 2 (capped at 2 max, plus 'and N more')", len(fire.ShedLabels))
	}
	// Second item should be "and N more".
	if !strings.HasPrefix(fire.ShedLabels[1], "and ") {
		t.Fatalf("fire.ShedLabels[1] = %q, want to start with 'and '", fire.ShedLabels[1])
	}

	// Verify vaccine labels are populated and capped.
	if len(fire.VaccineLabels) == 0 {
		t.Fatalf("fire.VaccineLabels is empty, want populated vaccine names")
	}
	// With 3 vaccines and a cap of 2, should have 2 items: 2 vaccine names + "and N more".
	if len(fire.VaccineLabels) != 2 {
		t.Fatalf("fire.VaccineLabels has %d items, want 2 (capped at 2 max, plus 'and N more')", len(fire.VaccineLabels))
	}
	// Check that we have human-readable vaccine names, not raw tokens.
	// "ET+TT" should be in there (the vaccine name on the shed-1 rule).
	hasETTT := false
	for _, label := range fire.VaccineLabels {
		if strings.Contains(label, "ET+TT") {
			hasETTT = true
			break
		}
	}
	if !hasETTT {
		t.Fatalf("fire.VaccineLabels = %v, want at least one to contain 'ET+TT' (human-readable vaccine name)", fire.VaccineLabels)
	}
}
