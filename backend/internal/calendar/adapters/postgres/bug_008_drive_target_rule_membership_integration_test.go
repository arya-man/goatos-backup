package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// seedMixedVaccineProtocolVersion creates ONE published vaccination protocol version carrying TWO
// rules (the ET+TT / PPR mixed-batch shape). Both rules are inserted while the version is still a
// draft, because published protocol config is immutable and cannot be reopened.
func seedMixedVaccineProtocolVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protocolID, versionID, ruleA, ruleB string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
VALUES (
  $1::uuid, $2::uuid,
  'vaccination.calendar.bug008.p' || right(replace(($1::uuid)::text, '-', ''), 12),
  'BUG-008 Mixed Vaccine Matrix', 'vaccination', 'active'
)
ON CONFLICT (protocol_id) DO UPDATE SET status = 'active', updated_at = now()`, protocolID, testTenantID); err != nil {
		t.Fatalf("seed protocol definition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_versions (
  protocol_version_id, tenant_id, protocol_id, scope_type, scope_id, version,
  version_label, status, effective_from, effective_to, rule_dsl, proof_policy, published_at
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, 'tenant', NULL, 1,
  'BUG-008 mixed vaccine version', 'draft', DATE '2026-01-01', DATE '2028-01-01',
  '{"source":{"review_status":"approved","source_ref":"docs/preventive-care-vaccination/PRD.md","source_system":"pc","approved_by":"test","approved_at":"2026-06-27T00:00:00Z"}}'::jsonb,
  '{"required_proofs":["administration"]}'::jsonb, NULL
)
ON CONFLICT (protocol_version_id) DO NOTHING`, versionID, testTenantID, protocolID); err != nil {
		t.Fatalf("seed protocol version: %v", err)
	}
	for i, ruleID := range []string{ruleA, ruleB} {
		if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rules (
  rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type,
  offset_days, due_window_days, min_gap_days, repeat, catch_up, eligibility_json,
  proof_policy, sort_order
)
SELECT
  $1::uuid, $2::uuid, $3::uuid, $4, $5::int, 'calendar',
  0, 1, 0, 'none', 'immediate', '{}'::jsonb, '{"required_proofs":["administration"]}'::jsonb, $5::int * 10
WHERE NOT EXISTS (
  SELECT 1 FROM protocol_rules WHERE tenant_id = $2::uuid AND rule_id = $1::uuid
)`, ruleID, testTenantID, versionID, fmt.Sprintf("BUG008-R%d", i+1), i+1); err != nil {
			t.Fatalf("seed protocol rule %s: %v", ruleID, err)
		}
	}
	if _, err := pool.Exec(ctx, `
UPDATE protocol_versions
SET status = 'published', published_at = COALESCE(published_at, now()), updated_at = now()
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`, testTenantID, versionID); err != nil {
		t.Fatalf("publish mixed-vaccine version: %v", err)
	}
}

// seedDriveAssignmentForRules writes one vaccination_drive_assignments row for a batch on a given
// business day, carrying an explicit vaccine_rule_ids set (empty => legacy/unspecific row).
func seedDriveAssignmentForRules(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batchID, parkID, shedID, partition string, day time.Time, ruleIDs []string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label,
  animal_count, total_doses, vaccine_rule_ids
) VALUES (
  $1::uuid, $2::uuid, ($3::timestamptz AT TIME ZONE 'Asia/Kolkata')::date,
  $4::uuid, $5::uuid, 'Gandhi', $6, 1, 1, COALESCE($7::uuid[], ARRAY[]::uuid[])
)`, testTenantID, batchID, day, parkID, shedID, partition, ruleIDs); err != nil {
		t.Fatalf("seed drive assignment (%s, rules=%v): %v", partition, ruleIDs, err)
	}
}

// TestCalendarDriveTargetsExcludeSiblingVaccineAfterPerVaccineDateOverride is the BUG-008 red
// test.
//
// Shape: ONE mixed batch (ET+TT rule + PPR rule, one animal each) whose per-vaccine date override
// has split vaccination_drive_assignments into two rows -- ET+TT stays on day D, PPR moves to day
// D+3. The Calendar drive-target drawer for EITHER day must contain ONLY that day's own vaccine's
// animals.
//
// Before the fix, calendarDriveTargetsSQL's matched_batches CTE selected the batch on the mere
// PRESENCE of an assignment row on the queried date and carried NO rule set out of the CTE, so the
// membership predicate `oi.batch_id IN (SELECT batch_id FROM matched_batches)` admitted every
// obligation on that batch -- both days returned both animals.
func TestCalendarDriveTargetsExcludeSiblingVaccineAfterPerVaccineDateOverride(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID    = "86000000-0000-4000-8000-00000000e801"
		versionID     = "86000000-0000-4000-8000-00000000e802"
		ruleEtTt      = "86000000-0000-4000-8000-00000000e803"
		rulePPR       = "86000000-0000-4000-8000-00000000e804"
		obligationEt  = "86000000-0000-4000-8000-00000000e805"
		obligationPPR = "86000000-0000-4000-8000-00000000e806"
		animalEt      = "86000000-0000-4000-8000-00000000e807"
		animalPPR     = "86000000-0000-4000-8000-00000000e808"
		batchID       = "86000000-0000-4000-8000-00000000e809"
	)

	loc := biztime.DefaultLocation()
	stayDay := stableSameLocalDayDueAt(time.Now().In(loc))
	movedDay := stayDay.Add(3 * 24 * time.Hour)
	if stayDay.In(loc).Format("2006-01-02") == movedDay.In(loc).Format("2006-01-02") {
		t.Fatalf("test setup: stay day and moved day must differ")
	}

	// ET+TT obligation on animalEt.
	seedMixedVaccineProtocolVersion(t, ctx, pool, protocolID, versionID, ruleEtTt, rulePPR)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleEtTt, obligationEt, stayDay)
	attachObligationToGoatScope(t, ctx, pool, obligationEt, animalEt, "shed", testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalEt, testParkA, testShedA)

	// PPR obligation on animalPPR, SAME version, SAME batch -- the mixed-vaccine batch.
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, rulePPR, obligationPPR, stayDay)
	attachObligationToGoatScope(t, ctx, pool, obligationPPR, animalPPR, "shed", testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalPPR, testParkA, testShedA)

	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, stayDay, obligationEt, obligationPPR)

	// The per-vaccine date-override split: two assignment rows, one per vaccine, on two dates.
	seedDriveAssignmentForRules(t, ctx, pool, batchID, testParkA, testShedA, "Part 1", stayDay, []string{ruleEtTt})
	seedDriveAssignmentForRules(t, ctx, pool, batchID, testParkA, testShedA, "Part 1", movedDay, []string{rulePPR})

	assertDriveTargets(t, ctx, repo, parkDriveEventID(testParkA, stayDay), []string{animalEt})
	assertDriveTargets(t, ctx, repo, parkDriveEventID(testParkA, movedDay), []string{animalPPR})
}

// TestCalendarDriveTargetsKeepLegacyUnspecificAssignmentRows guards the fix's required fallback:
// a pre-split / legacy assignment row carries an EMPTY vaccine_rule_ids array, and must still
// admit EVERY obligation on its batch. A rule-intersection that treated "no rule set" as "matches
// nothing" would hide all pre-split data.
func TestCalendarDriveTargetsKeepLegacyUnspecificAssignmentRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID    = "86000000-0000-4000-8000-00000000e811"
		versionID     = "86000000-0000-4000-8000-00000000e812"
		ruleEtTt      = "86000000-0000-4000-8000-00000000e813"
		rulePPR       = "86000000-0000-4000-8000-00000000e814"
		obligationEt  = "86000000-0000-4000-8000-00000000e815"
		obligationPPR = "86000000-0000-4000-8000-00000000e816"
		animalEt      = "86000000-0000-4000-8000-00000000e817"
		animalPPR     = "86000000-0000-4000-8000-00000000e818"
		batchID       = "86000000-0000-4000-8000-00000000e819"
	)

	loc := biztime.DefaultLocation()
	day := stableSameLocalDayDueAt(time.Now().In(loc))

	seedMixedVaccineProtocolVersion(t, ctx, pool, protocolID, versionID, ruleEtTt, rulePPR)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleEtTt, obligationEt, day)
	attachObligationToGoatScope(t, ctx, pool, obligationEt, animalEt, "shed", testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalEt, testParkA, testShedA)

	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, rulePPR, obligationPPR, day)
	attachObligationToGoatScope(t, ctx, pool, obligationPPR, animalPPR, "shed", testShedA)
	setCalendarGoatCurrentShed(t, ctx, pool, animalPPR, testParkA, testShedA)

	seedVaccinationBatchForShed(t, ctx, pool, batchID, versionID, testParkA, testShedA, day, obligationEt, obligationPPR)
	// Legacy row: no vaccine_rule_ids at all.
	seedDriveAssignmentForRules(t, ctx, pool, batchID, testParkA, testShedA, "whole", day, nil)

	assertDriveTargets(t, ctx, repo, parkDriveEventID(testParkA, day), []string{animalEt, animalPPR})
}
