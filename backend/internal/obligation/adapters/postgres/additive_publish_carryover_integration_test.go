package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
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

// The defect this whole design turns on: a rule's content can be unchanged while the date the
// animal owes it moves, because the due date is computed from the ANIMAL's history too -- a dose
// recorded late, a correction applied afterwards.
//
// Generation used to answer that by inserting, since its key includes the due date, and the animal
// ended up owing the same dose twice. Reconciliation moves the existing row instead.
func TestReconcileMovesTheExistingWorkInsteadOfBookingTheDoseTwice(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]

	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET rule_identity_key = $3 WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id, identity); err != nil {
		t.Fatalf("stamp identity: %v", err)
	}

	var v2Rule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find v2 rule: %v", err)
	}

	// Generation now wants this dose a week later, under the new version.
	movedDue := original.dueAt.AddDate(0, 0, 7)
	ref, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: movedDue, Status: "scheduled", IdempotencyKey: "generation-owns-this-key",
		Sequence: 1, RuleIdentityKey: identity,
	}, movedDue)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !found {
		t.Fatal("reconcile found nothing, so generation would insert a SECOND open row for a dose the animal already owes")
	}
	if ref.ObligationID != original.id {
		t.Fatalf("obligation id %s -> %s: the row was replaced, detaching its task, batch and proof", original.id, ref.ObligationID)
	}

	open := obligationsForGoat(t, ctx, pool, carryOverGoat)
	if len(open) != 5 {
		t.Fatalf("the goat now holds %d open obligations, want the same 5 -- a duplicate dose was booked", len(open))
	}

	var (
		gotDue       time.Time
		gotVersion   string
		gotKey       string
		rowVersion   int
		statusEvents int
		count        int
	)
	if err := pool.QueryRow(ctx, `
SELECT due_at, protocol_version_id::text, idempotency_key, row_version,
       (SELECT count(*) FROM obligation_status_events e
         WHERE e.tenant_id = $1::uuid AND e.obligation_id = $2::uuid
           AND e.event_type = 'scheduled'
           AND e.payload->>'reason' = 'rule_identity_reconciled'),
       (SELECT count(*) FROM obligation_instances d
         WHERE d.tenant_id = $1::uuid AND d.target_id = $3::uuid AND d.rule_identity_key = $4
           AND d.status IN ('scheduled','due','in_progress','deferred'))
FROM obligation_instances WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id, carryOverGoat, identity).Scan(&gotDue, &gotVersion, &gotKey, &rowVersion, &statusEvents, &count); err != nil {
		t.Fatalf("read reconciled row: %v", err)
	}
	if !gotDue.Equal(movedDue) {
		t.Fatalf("due %s, want it moved to %s -- a stale date is the other half of this bug", gotDue, movedDue)
	}
	if gotVersion != v2 {
		t.Fatalf("version did not follow the reconcile")
	}
	if gotKey != "generation-owns-this-key" {
		t.Fatalf("idempotency key = %q, want the one generation owns, or the row is unaddressable", gotKey)
	}
	if count != 1 {
		t.Fatalf("%d open obligations under one identity, want exactly 1", count)
	}

	_, found, err = repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: movedDue, Status: "scheduled", IdempotencyKey: "generation-owns-this-key",
		Sequence: 1, RuleIdentityKey: identity,
	}, movedDue)
	if err != nil {
		t.Fatalf("exact replay reconcile: %v", err)
	}
	if !found {
		t.Fatal("exact replay lost the reconciled row")
	}
	var replayRowVersion, replayStatusEvents int
	if err := pool.QueryRow(ctx, `
SELECT row_version,
       (SELECT count(*) FROM obligation_status_events e
         WHERE e.tenant_id = $1::uuid AND e.obligation_id = $2::uuid
           AND e.event_type = 'scheduled'
           AND e.payload->>'reason' = 'rule_identity_reconciled')
FROM obligation_instances WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id).Scan(&replayRowVersion, &replayStatusEvents); err != nil {
		t.Fatalf("read exact replay row: %v", err)
	}
	if replayRowVersion != rowVersion {
		t.Fatalf("exact replay row_version = %d, want unchanged %d", replayRowVersion, rowVersion)
	}
	if replayStatusEvents != statusEvents {
		t.Fatalf("exact replay wrote another reconcile event: %d -> %d", statusEvents, replayStatusEvents)
	}
}

// in_progress work is not moved underneath the operator running it, but it still counts as found
// so generation does not insert a second row beside it.
func TestReconcileLeavesInFlightWorkOnItsDateButStillClaimsIt(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET rule_identity_key = $3, status = 'in_progress' WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id, identity); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	var v2Rule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find rule: %v", err)
	}

	_, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: original.dueAt.AddDate(0, 0, 7), Status: "scheduled", IdempotencyKey: "k",
		Sequence: 1, RuleIdentityKey: identity,
	}, original.dueAt)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !found {
		t.Fatal("in-flight work was not claimed, so generation would book the dose a second time")
	}
	var due time.Time
	if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id).Scan(&due); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !due.Equal(original.dueAt) {
		t.Fatalf("the operator's in-flight dose was moved from %s to %s underneath them", original.dueAt, due)
	}
}

// An in-flight dose keeps its DATE but must still take the new ADDRESS. Everything generation does
// after reconciling -- defer, reopen, realign, cancel-by-key -- addresses the row by the key it
// just computed, so a row still holding the key it was minted under is unreachable and those
// follow-ups fail with "not found", taking the whole goat's pass down.
func TestReconcileGivesInFlightWorkTheNewAddressEvenThoughItKeepsItsDate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET rule_identity_key = $3, status = 'in_progress' WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id, identity); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	var v2Rule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find rule: %v", err)
	}

	if _, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: original.dueAt.AddDate(0, 0, 7), Status: "scheduled",
		IdempotencyKey: "the-key-generation-now-owns", Sequence: 1, RuleIdentityKey: identity,
	}, original.dueAt); err != nil || !found {
		t.Fatalf("reconcile in-flight: found=%v err=%v", found, err)
	}

	var (
		due     time.Time
		key     string
		version string
	)
	if err := pool.QueryRow(ctx,
		`SELECT due_at, idempotency_key, protocol_version_id::text FROM obligation_instances WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id).Scan(&due, &key, &version); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !due.Equal(original.dueAt) {
		t.Fatalf("the operator's in-flight dose moved from %s to %s underneath them", original.dueAt, due)
	}
	if key != "the-key-generation-now-owns" {
		t.Fatalf("idempotency key = %q: every key-addressed follow-up on this row would fail with not-found", key)
	}
	if version != v2 {
		t.Fatalf("in-flight row did not follow the live version")
	}
}

// Work that predates the identity column is invisible to the unique index, so inserting beside it
// double-books. A single such row is adopted -- labelled and reconciled -- bringing it under the
// invariant instead of leaving it outside.
func TestReconcileAdoptsASingleUnlabelledObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]

	// Lineage exists for the rule; the OBLIGATION carries no label, as every pre-migration row does.
	var v1Rule, v2Rule string
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM protocol_rules WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid AND dose_code='fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find v2 rule: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		tenantID, original.id).Scan(&v1Rule); err != nil {
		t.Fatalf("read rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_rule_lineage SET identity_key = $2 WHERE tenant_id = $1::uuid AND rule_id IN ($3::uuid, $4::uuid)`,
		tenantID, identity, v1Rule, v2Rule); err != nil {
		t.Fatalf("align lineage identity: %v", err)
	}

	moved := original.dueAt.AddDate(0, 0, 5)
	ref, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: moved, Status: "scheduled", IdempotencyKey: "adopted", Sequence: 1, RuleIdentityKey: identity,
	}, moved)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !found {
		t.Fatal("pre-label work was not adopted, so generation would insert beside it and book the dose twice")
	}
	if ref.ObligationID != original.id {
		t.Fatalf("adopted the wrong row: %s", ref.ObligationID)
	}
	var label *string
	var due time.Time
	if err := pool.QueryRow(ctx, `SELECT rule_identity_key, due_at FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		tenantID, original.id).Scan(&label, &due); err != nil {
		t.Fatalf("read: %v", err)
	}
	if label == nil || *label != identity {
		t.Fatalf("adopted row left unlabelled, so it stays outside the unique index: %v", label)
	}
	if !due.Equal(moved) {
		t.Fatalf("adopted row was not reconciled to the new date")
	}
}

// Two unlabelled open rows for one identity is the 561-group case from real staging data. Adopting
// one means guessing which scheduled vaccination to keep; inserting means booking a third. The pass
// fails for THIS animal, loudly, and every other animal in the run is unaffected.
func TestReconcileRefusesToGuessBetweenTwoUnlabelledObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]

	var v1Rule, v2Rule string
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM protocol_rules WHERE tenant_id=$1::uuid AND protocol_version_id=$2::uuid AND dose_code='fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find v2 rule: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		tenantID, original.id).Scan(&v1Rule); err != nil {
		t.Fatalf("read rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_rule_lineage SET identity_key = $2 WHERE tenant_id = $1::uuid AND rule_id IN ($3::uuid, $4::uuid)`,
		tenantID, identity, v1Rule, v2Rule); err != nil {
		t.Fatalf("align lineage: %v", err)
	}
	// A second, unlabelled open row for the same dose a week later: the duplicate.
	insertObligationForRule(t, ctx, repo, before["fmd_primary"].versionID, v1Rule, carryOverGoat,
		"pre-existing-duplicate", original.dueAt.AddDate(0, 0, 7))

	_, _, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: original.dueAt.AddDate(0, 0, 9), Status: "scheduled", IdempotencyKey: "k",
		Sequence: 1, RuleIdentityKey: identity,
	}, original.dueAt)
	if !errors.Is(err, ports.ErrAmbiguousOpenWork) {
		t.Fatalf("err = %v, want ErrAmbiguousOpenWork -- silently picking or inserting decides somebody's medical work", err)
	}
	if !strings.Contains(err.Error(), original.id) {
		t.Fatalf("the error must name the obligations a human has to look at: %v", err)
	}
}

// When the due date cannot move -- because obligation_instances_dup_guard already holds the key it
// would move to, typically this obligation's own canceled twin -- the ADDRESS must still move.
// Everything generation does next addresses the row by the key it computed, and a row left holding
// its old key is unreachable: defer, reopen and cancel-by-key all fail the animal.
func TestReconcileStillMovesTheAddressWhenTheDateIsBlocked(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo, _, v2, before := seedCarryOverFixture(t, ctx, pool, fiveVaccines())
	identity := "3:fmd|11:fmd_primary|1:1"
	original := before["fmd_primary"]
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET rule_identity_key = $3 WHERE tenant_id = $1::uuid AND obligation_id = $2::uuid`,
		tenantID, original.id, identity); err != nil {
		t.Fatalf("stamp: %v", err)
	}

	var v2Rule string
	if err := pool.QueryRow(ctx,
		`SELECT rule_id::text FROM protocol_rules WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid AND dose_code = 'fmd_primary'`,
		tenantID, v2).Scan(&v2Rule); err != nil {
		t.Fatalf("find rule: %v", err)
	}

	// A CANCELED twin already occupies the key the reconcile would move onto. The duplicate guard
	// spans terminal rows, so the date cannot move there.
	movedDue := original.dueAt.AddDate(0, 0, 4)
	blocker := insertObligationForRule(t, ctx, repo, v2, v2Rule, carryOverGoat, "blocking-twin", movedDue)
	if _, err := pool.Exec(ctx,
		`UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		tenantID, blocker); err != nil {
		t.Fatalf("cancel the twin: %v", err)
	}

	ref, found, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: v2, RuleID: v2Rule,
		TargetType: "goat", TargetID: carryOverGoat, ScopeType: "park", ScopeID: cbePark,
		DueAt: movedDue, Status: "scheduled", IdempotencyKey: "key-generation-now-owns",
		Sequence: 1, RuleIdentityKey: identity,
	}, movedDue)
	if err != nil {
		t.Fatalf("a blocked date must not fail the animal: %v", err)
	}
	if !found {
		t.Fatal("the obligation was not claimed, so generation would insert a duplicate beside it")
	}
	if !ref.DateBlocked {
		t.Fatal("DateBlocked not reported, so a stale date would be invisible")
	}
	if ref.IdempotencyKey == "" {
		t.Fatal("the ref carries no key, so the caller cannot address the row at all")
	}

	var storedKey string
	var storedDue time.Time
	if err := pool.QueryRow(ctx,
		`SELECT idempotency_key, due_at FROM obligation_instances WHERE tenant_id=$1::uuid AND obligation_id=$2::uuid`,
		tenantID, original.id).Scan(&storedKey, &storedDue); err != nil {
		t.Fatalf("read: %v", err)
	}
	if storedKey != ref.IdempotencyKey {
		t.Fatalf("ref key %q does not match the stored key %q: follow-ups would address the wrong thing", ref.IdempotencyKey, storedKey)
	}
	if storedKey == "carryover-fmd_primary" {
		t.Fatal("the row kept the key it was minted under; every key-addressed follow-up on it fails")
	}
	if !storedDue.Equal(original.dueAt) {
		t.Fatalf("the date moved after all (%s), which the duplicate guard should have prevented", storedDue)
	}
}
