package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// The maintainer's rule, on real rows:
//
//	"If there are already 5 vaccine rules in the previous version and in the new version I add a
//	 6th without touching the past 5, all 5 stay the same -- no change, no rescheduling. If I
//	 change one rule on 1 of the 5, only that 1 changes and the other 4 continue as they are."
//
// Everything below drives CarryOverUnchangedVaccinationObligations against Postgres and asserts on
// the stored rows, because the guarantee is about what survives in the database -- obligation ids,
// due dates, statuses -- not about which functions were called.

const carryOverGoat = "10000000-0000-4000-8000-0000000000d1"

type carryOverVaccine struct {
	code     string
	doseCode string
	offset   int32
	minGap   int32
}

func fiveVaccines() []carryOverVaccine {
	return []carryOverVaccine{
		{code: "ET_TT", doseCode: "et_tt_primary", offset: 28, minGap: 180},
		{code: "PPR", doseCode: "ppr_primary", offset: 112, minGap: 1095},
		{code: "GOAT_POX", doseCode: "goat_pox_primary", offset: 140, minGap: 365},
		{code: "SHEEP_POX", doseCode: "sheep_pox_primary", offset: 84, minGap: 365},
		{code: "FMD", doseCode: "fmd_primary", offset: 84, minGap: 274},
	}
}

// seedCarryOverVersion publishes one protocol version holding exactly the given vaccines, and
// returns the version id plus dose_code -> rule_id.
func seedCarryOverVersion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, protoID string, version int32, vaccines []carryOverVaccine) (string, map[string]string) {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: version, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version %d: %v", version, err)
	}
	rules := make(map[string]string, len(vaccines))
	for i, v := range vaccines {
		ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: v.doseCode, Sequence: 1,
			TriggerType: "birth_age", OffsetDays: v.offset, DueWindowDays: 7, MinGapDays: v.minGap,
			Repeat: "yearly", CatchUp: "immediate",
			EligibilityJSON: []byte(fmt.Sprintf(`{"vaccine":{"code":%q},"eligibility":{"animal_stage":"adult"}}`, v.code)),
			ProofPolicy:     []byte(`{"mode":"per_goat"}`),
			// Deliberately different between versions: sort_order is not rule content, and
			// reordering the vaccine list must not reschedule a single animal.
			SortOrder: int32(len(vaccines) - i),
		})
		if err != nil {
			t.Fatalf("create rule %s: %v", v.doseCode, err)
		}
		rules[v.doseCode] = ruleID
	}
	return versionID, rules
}

type obligationRow struct {
	id        string
	versionID string
	ruleID    string
	dueAt     time.Time
	status    string
}

func obligationsForGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) map[string]obligationRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT oi.obligation_id::text, oi.protocol_version_id::text, oi.rule_id::text, oi.due_at, oi.status, pr.dose_code
FROM obligation_instances oi
JOIN protocol_rules pr ON pr.tenant_id = oi.tenant_id AND pr.rule_id = oi.rule_id
WHERE oi.tenant_id = $1::uuid AND oi.target_id = $2::uuid AND oi.status <> 'canceled'`, tenantID, goatID)
	if err != nil {
		t.Fatalf("read obligations: %v", err)
	}
	defer rows.Close()
	out := map[string]obligationRow{}
	for rows.Next() {
		var r obligationRow
		var doseCode string
		if err := rows.Scan(&r.id, &r.versionID, &r.ruleID, &r.dueAt, &r.status, &doseCode); err != nil {
			t.Fatalf("scan obligation: %v", err)
		}
		out[doseCode] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read obligations: %v", err)
	}
	return out
}

func seedCarryOverFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, next []carryOverVaccine) (repo *Repository, v1, v2 string, before map[string]obligationRow) {
	t.Helper()
	_ = seed(t, ctx, pool)
	repo = NewRepository(pool, 5*time.Second)
	seedCapacityGoatInPark(t, ctx, pool, carryOverGoat, cbePark)

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.carryover", Name: "CarryOver", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}

	v1, v1Rules := seedCarryOverVersion(t, ctx, pool, protoID, 1, fiveVaccines())
	// Publish v1 before creating v2, otherwise there would be two draft versions for the same scope.
	if err := proto.PublishVersion(ctx, tenantID, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	for i, v := range fiveVaccines() {
		insertObligationForRule(t, ctx, repo, v1, v1Rules[v.doseCode], carryOverGoat,
			"carryover-"+v.doseCode, due.AddDate(0, 0, i))
	}
	before = obligationsForGoat(t, ctx, pool, carryOverGoat)
	if len(before) != 5 {
		t.Fatalf("seeded %d open obligations, want 5", len(before))
	}

	v2, _ = seedCarryOverVersion(t, ctx, pool, protoID, 2, next)
	return repo, v1, v2, before
}

// Adding a 6th vaccine leaves the other five where they were: same obligation id, same due date,
// same status, only the version pointer moves.
func TestPublishingAnAddedVaccineLeavesTheOtherFiveUntouched(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	sixth := append(fiveVaccines(), carryOverVaccine{code: "Z1_Z3", doseCode: "z1_z3_primary", offset: 28, minGap: 180})
	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, sixth)

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 5 {
		t.Fatalf("carried over %d obligations, want all 5 -- the rest would be cancelled as protocol_version_replaced", moved)
	}

	after := obligationsForGoat(t, ctx, pool, carryOverGoat)
	if len(after) != 5 {
		t.Fatalf("after carry-over the goat holds %d open obligations, want the same 5", len(after))
	}
	for dose, was := range before {
		now, ok := after[dose]
		if !ok {
			t.Fatalf("%s disappeared: adding a sixth vaccine destroyed work on an untouched one", dose)
		}
		if now.id != was.id {
			t.Fatalf("%s obligation id %s -> %s: the row was re-minted, so its task, batch and proof are detached", dose, was.id, now.id)
		}
		if !now.dueAt.Equal(was.dueAt) {
			t.Fatalf("%s due %s -> %s: an untouched vaccine was rescheduled", dose, was.dueAt, now.dueAt)
		}
		if now.status != was.status {
			t.Fatalf("%s status %s -> %s", dose, was.status, now.status)
		}
		if now.versionID != v2 {
			t.Fatalf("%s still points at the retired version: supersede would cancel it", dose)
		}
		if now.ruleID == was.ruleID {
			t.Fatalf("%s kept the retired version's rule id: rules are rewritten per version, so this row is now orphaned", dose)
		}
	}
}

// Editing one rule moves that rule's work and nothing else. This is the half that stops carry-over
// becoming "never reschedule anything".
func TestEditingOneVaccineLeavesTheOtherFourAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	edited := fiveVaccines()
	edited[4].minGap = 300 // FMD revaccination interval: 274 -> 300 days
	repo, v1, v2, before := seedCarryOverFixture(t, ctx, pool, edited)

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 4 {
		t.Fatalf("carried over %d, want 4 -- the edited vaccine must NOT carry over, or the edit never reaches an animal", moved)
	}

	after := obligationsForGoat(t, ctx, pool, carryOverGoat)
	for dose, was := range before {
		now := after[dose]
		if now.id != was.id || !now.dueAt.Equal(was.dueAt) {
			t.Fatalf("%s was re-minted or rescheduled by an edit to a different vaccine", dose)
		}
		if dose == "fmd_primary" {
			if now.versionID != v1 {
				t.Fatalf("the edited vaccine carried over: its new interval would never take effect")
			}
			continue
		}
		if now.versionID != v2 {
			t.Fatalf("%s did not carry over even though nothing about it changed", dose)
		}
	}
}

// Guardrail 1: republishing an identical plan is a no-op for every animal.
func TestRepublishingAnIdenticalPlanReschedulesNothing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 5 {
		t.Fatalf("carried over %d of 5 on an identical republish", moved)
	}
	after := obligationsForGoat(t, ctx, pool, carryOverGoat)
	for dose, was := range before {
		if after[dose].id != was.id || !after[dose].dueAt.Equal(was.dueAt) || after[dose].status != was.status {
			t.Fatalf("%s changed on a republish that changed nothing", dose)
		}
	}
}

// Guardrail 5: a rule with no fingerprint -- rows written before the lineage migration -- must not
// carry over. It falls back to the behaviour that shipped rather than carrying unverified content.
func TestRuleWithoutLineageDoesNotCarryOver(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	_ = seed(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	seedCapacityGoatInPark(t, ctx, pool, carryOverGoat, cbePark)

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.carryover.nolineage", Name: "CarryOverNoLineage", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}

	v1, v1Rules := seedCarryOverVersion(t, ctx, pool, protoID, 1, fiveVaccines())
	// Drop one rule's lineage row to simulate a rule written before the lineage table existed.
	if _, err := pool.Exec(ctx, `
DELETE FROM protocol_rule_lineage l
USING protocol_rules pr
WHERE l.tenant_id = $1::uuid
  AND pr.tenant_id = l.tenant_id
  AND pr.rule_id = l.rule_id
  AND pr.dose_code = 'ppr_primary'
  AND l.protocol_version_id = $2::uuid`, tenantID, v1); err != nil {
		t.Fatalf("drop the lineage row: %v", err)
	}

	// Now publish v1 (with one rule carrying no lineage)
	if err := proto.PublishVersion(ctx, tenantID, v1, nil); err != nil {
		t.Fatalf("publish v1: %v", err)
	}

	// Create and insert obligations for v1
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	for i, v := range fiveVaccines() {
		insertObligationForRule(t, ctx, repo, v1, v1Rules[v.doseCode], carryOverGoat,
			"carryover-"+v.doseCode, due.AddDate(0, 0, i))
	}

	// Create v2 with the same vaccines
	v2, _ := seedCarryOverVersion(t, ctx, pool, protoID, 2, fiveVaccines())

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 4 {
		t.Fatalf("carried over %d, want 4: a rule with no recorded content must not be assumed unchanged", moved)
	}
}

// Guardrail 4 at the SQL level: carry-over must not move a due date even when the destination key
// is already taken. obligation_instances_dup_guard spans every status, so a rebind onto an occupied
// key would raise 23505 and abort the whole generation pass for the tenant.
func TestCarryOverSkipsRowsThatWouldCollideInsteadOfFailing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())

	// Something already occupies the key PPR would rebind onto.
	var v2PPRRule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'ppr_primary'`,
		tenantID, v2).Scan(&v2PPRRule); err != nil {
		t.Fatalf("find v2 ppr rule: %v", err)
	}
	insertObligationForRule(t, ctx, repo, v2, v2PPRRule, carryOverGoat, "carryover-clash", before["ppr_primary"].dueAt)

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over must not fail on a collision, it must leave that row behind: %v", err)
	}
	if moved != 4 {
		t.Fatalf("carried over %d, want 4 with the colliding row left for the supersede path", moved)
	}
	if got := obligationsForGoat(t, ctx, pool, carryOverGoat)["ppr_primary"]; got.id == "" {
		t.Fatalf("the colliding row vanished")
	}
}

// Guardrail 7: the two sweeps take different status sets on purpose.
//
// Carry-over includes in_progress because rebinding is non-destructive: an operator part-way
// through a drive keeps the same obligation and it stays attached to the version that is now
// live. Supersede excludes in_progress because cancelling work somebody is physically doing is
// worse than the staleness it fixes -- that exclusion predates carry-over and is unchanged by it.
//
// Pinned here because the asymmetry reads like an oversight and would otherwise be "tidied up".
func TestInFlightWorkIsReboundButNeverCancelled(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())

	// An operator has started the FMD dose.
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status = 'in_progress' WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, before["fmd_primary"].id); err != nil {
		t.Fatalf("mark in_progress: %v", err)
	}

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over: %v", err)
	}
	if moved != 5 {
		t.Fatalf("carried over %d of 5: in-flight work was left behind on a retired version", moved)
	}

	after := obligationsForGoat(t, ctx, pool, carryOverGoat)
	inFlight := after["fmd_primary"]
	if inFlight.status != "in_progress" {
		t.Fatalf("in-flight status = %q, want it untouched", inFlight.status)
	}
	if inFlight.id != before["fmd_primary"].id {
		t.Fatalf("the operator's obligation was re-minted mid-drive")
	}
	if inFlight.versionID != v2 {
		t.Fatalf("in-flight work did not follow the live version")
	}

	// And the supersede sweep must not offer it up for cancellation.
	stale, err := repo.GoatsWithVaccinationObligationsOutsideVersions(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("stale sweep: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("supersede wants to cancel %d goat(s) after a clean carry-over", len(stale))
	}
}

// A repeat obligation is unique by its CAUSE, not its due date, and rebinding moves both key
// columns of that index. So two rows can share a cause at DIFFERENT due dates, pass the dup-guard
// check, and still collide -- raising 23505 and aborting the whole tenant's generation pass.
//
// The dup-guard test above cannot catch this: it collides on an identical due_at, which is the one
// case the repeat index deliberately ignores.
func TestCarryOverSkipsRepeatCauseCollisionAtADifferentDueDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())

	cause := "et_tt|2026-07-24T03:30:00Z|2"
	// The carried-over row carries a cause.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET repeat_cycle_source = 'trusted_history', repeat_cycle_source_ref = $3
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, before["et_tt_primary"].id, cause); err != nil {
		t.Fatalf("mark the cause: %v", err)
	}

	// And the destination rule already holds an open row for the SAME cause, a week later.
	var v2Rule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'et_tt_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find v2 rule: %v", err)
	}
	clash := insertObligationForRule(t, ctx, repo, v2, v2Rule, carryOverGoat, "carryover-cause-clash",
		before["et_tt_primary"].dueAt.AddDate(0, 0, 7))
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances
SET repeat_cycle_source = 'trusted_history', repeat_cycle_source_ref = $3
WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`, tenantID, clash, cause); err != nil {
		t.Fatalf("mark the clashing cause: %v", err)
	}

	moved, err := repo.CarryOverUnchangedVaccinationObligations(ctx, tenantID, []string{carryOverGoat}, []string{v2})
	if err != nil {
		t.Fatalf("carry over must skip a cause collision, not fail the tenant's whole pass: %v", err)
	}
	if moved != 4 {
		t.Fatalf("carried over %d, want 4 with the cause-colliding row left for the supersede path", moved)
	}
	// Assert on the specific row: the clash shares its dose_code, so a lookup by dose would read
	// whichever of the two the scan happened to return last.
	var version string
	if err := pool.QueryRow(ctx,
		`SELECT protocol_version_id::text FROM obligation_instances WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, before["et_tt_primary"].id).Scan(&version); err != nil {
		t.Fatalf("read the carried row: %v", err)
	}
	if version == v2 {
		t.Fatalf("the cause-colliding row was rebound anyway, which would have raised 23505 on a real index")
	}
}
