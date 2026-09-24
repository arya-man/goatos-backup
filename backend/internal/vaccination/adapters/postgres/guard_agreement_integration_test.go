package postgres

import (
	"context"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestGenerationAgreesWithPersistenceGuard runs real generation through the real obligation
// repository for the standard purpose/anchor fixtures. Any dose generation proposes but the
// persistence write guard refuses shows up as GuardRejected; the two must agree (zero).
func TestGenerationAgreesWithPersistenceGuard(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.guard.agreement", Name: "Guard agreement", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{},"procurement_policy":{"purpose_plans":{
		"breeding":{"first_wave":["ET+TT"]},
		"fattening":{"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]}}}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	vaccine := func(code, name, typ, class string) []byte {
		return []byte(`{"vaccine":{"code":"` + code + `","name":"` + name + `","type":"` + typ + `","pathogen_class":"` + class + `"}}`)
	}
	rules := []protodomain.NewRule{
		{DoseCode: "et_tt_w1", Sequence: 1, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: vaccine("ET_TT", "ET+TT", "killed", "bacterial")},
		{DoseCode: "ppr_w1", Sequence: 2, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: vaccine("PPR", "PPR", "live", "viral")},
		{DoseCode: "goat_pox_w2", Sequence: 3, TriggerType: "post_arrival", OffsetDays: 28, DueWindowDays: 7, EligibilityJSON: vaccine("GOAT_POX", "Goat Pox", "live", "viral")},
		{DoseCode: "sheep_pox_w2", Sequence: 4, TriggerType: "post_arrival", OffsetDays: 28, DueWindowDays: 7, EligibilityJSON: vaccine("SHEEP_POX", "Sheep Pox", "live", "viral")},
		{DoseCode: "fmd_w1", Sequence: 5, TriggerType: "post_arrival", OffsetDays: 0, DueWindowDays: 7, EligibilityJSON: vaccine("FMD", "FMD", "killed", "viral")},
		{DoseCode: "bt_kid_1", Sequence: 6, TriggerType: "birth_age", OffsetDays: 90, DueWindowDays: 7, EligibilityJSON: vaccine("BT", "Blue Tongue", "killed", "viral")},
		{DoseCode: "bt_kid_2", Sequence: 7, TriggerType: "after_previous_completion", OffsetDays: 21, DueWindowDays: 7, EligibilityJSON: vaccine("BT", "Blue Tongue", "killed", "viral")},
	}
	for _, rule := range rules {
		rule.TenantID, rule.ProtocolVersionID = impTenant, versionID
		rule.Repeat, rule.CatchUp, rule.ProofPolicy = "none", "immediate", []byte(`{}`)
		if _, err := proto.CreateRule(ctx, rule); err != nil {
			t.Fatalf("rule %s: %v", rule.DoseCode, err)
		}
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	entry := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	const (
		breedingGoat   = "30000000-0000-4000-8000-00000000a901"
		fatteningGoat  = "30000000-0000-4000-8000-00000000a902"
		fatteningSheep = "30000000-0000-4000-8000-00000000a903"
		btAnchorKid    = "30000000-0000-4000-8000-00000000a904"
		adultNoDOB     = "30000000-0000-4000-8000-00000000a905"
		adultWithDOB   = "30000000-0000-4000-8000-00000000a906"
	)
	for i, fixture := range []struct{ goat, purpose, species string }{
		{breedingGoat, "breeding", "goat"},
		{fatteningGoat, "fattening", "goat"},
		{fatteningSheep, "fattening", "sheep"},
	} {
		seedGenAdultProcuredGoat(t, ctx, pool, fixture.goat, "alive", entry)
		if _, err := pool.Exec(ctx, `UPDATE goats SET species=$3 WHERE tenant_id=$1 AND goat_id=$2`, impTenant, fixture.goat, fixture.species); err != nil {
			t.Fatalf("species %s: %v", fixture.goat, err)
		}
		load := "30000000-0000-4000-8000-00000000b90" + string(rune('1'+i))
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, expected_count, status, idempotency_key)
VALUES ($1, $2, $3, 1, 'source_warmup', $4)`, load, impTenant, impParty, "guard-agreement:"+load); err != nil {
			t.Fatalf("load %s: %v", load, err)
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO procurement_load_goats (tenant_id, load_id, goat_id, purpose, selection_state, current_state,
  source_entry_state, ownership_state, health_state, intake_accepted_at)
VALUES ($1, $2, $3, $4, 'accepted_herd_intake', 'accepted_herd_intake', 'accepted', 'mesha_owned', 'passed', $5::timestamptz)`,
			impTenant, load, fixture.goat, fixture.purpose, entry); err != nil {
			t.Fatalf("load goat %s: %v", fixture.goat, err)
		}
	}
	// BT follow-up after an animal-set anchor of the first BT dose (DOB 2026-05-01 + 90d <= anchor).
	seedGenGoat(t, ctx, pool, btAnchorKid, "alive")
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b7', $1, $2, 'BT', 'bt_kid_1', DATE '2026-08-01',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'BT dose 1 anchor', 'test', 'guard-agreement-bt'
)`, impTenant, versionID, btAnchorKid); err != nil {
		t.Fatalf("anchor: %v", err)
	}
	// Adult catch-up with and without a DOB (no procurement row: unspecified purpose).
	seedGenGoatWithStage(t, ctx, pool, adultNoDOB, "alive", "adult")
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob=NULL WHERE tenant_id=$1 AND goat_id=$2`, impTenant, adultNoDOB); err != nil {
		t.Fatalf("clear dob: %v", err)
	}
	seedGenGoatWithStage(t, ctx, pool, adultWithDOB, "alive", "adult")
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob=DATE '2025-01-01' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, adultWithDOB); err != nil {
		t.Fatalf("set dob: %v", err)
	}

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	for _, goat := range []string{breedingGoat, fatteningGoat, fatteningSheep, btAnchorKid, adultNoDOB, adultWithDOB} {
		if res, err := gen.GenerateForGoat(ctx, impTenant, goat, asOf); err != nil || res.GuardRejected != 0 || res.FailedGoats != 0 {
			t.Errorf("goat %s: res=%#v err=%v", goat, res, err)
		}
	}
	for pass := 1; pass <= 2; pass++ {
		res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf)
		if err != nil {
			t.Fatalf("pass %d generate: %v (res=%#v)", pass, err, res)
		}
		if res.GuardRejected != 0 || res.FailedGoats != 0 {
			t.Fatalf("pass %d res=%#v, want generation and persistence guard to agree (0 rejected, 0 failed)", pass, res)
		}
		if pass == 1 && res.Generated+res.Reconciled == 0 {
			t.Fatalf("pass 1 res=%#v, want generated work", res)
		}
	}

	count := func(goat, dose string) int {
		return countRowsVacc(t, ctx, pool, `
SELECT count(*) FROM obligation_instances oi JOIN protocol_rules pr ON pr.tenant_id=oi.tenant_id AND pr.rule_id=oi.rule_id
WHERE oi.tenant_id=$1 AND oi.target_id=$2 AND pr.dose_code=$3 AND oi.status IN ('scheduled','due','deferred')`, impTenant, goat, dose)
	}
	for _, excluded := range []struct{ goat, dose string }{
		{breedingGoat, "fmd_w1"}, {breedingGoat, "ppr_w1"},
		{fatteningGoat, "fmd_w1"}, {fatteningGoat, "sheep_pox_w2"}, {fatteningGoat, "goat_pox_w2"},
		{fatteningSheep, "fmd_w1"}, {fatteningSheep, "goat_pox_w2"}, {fatteningSheep, "sheep_pox_w2"},
	} {
		if n := count(excluded.goat, excluded.dose); n != 0 {
			t.Fatalf("goat %s dose %s open rows=%d, want 0 (excluded or first wave incomplete)", excluded.goat, excluded.dose, n)
		}
	}
	if n := count(fatteningGoat, "et_tt_w1") + count(fatteningGoat, "ppr_w1"); n != 2 {
		t.Fatalf("fattening goat first-wave rows=%d, want ET+TT and PPR", n)
	}
	if n := count(btAnchorKid, "bt_kid_2"); n != 1 {
		t.Fatalf("BT follow-up rows=%d, want 1 chained from the animal-set anchor", n)
	}
	if n := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND schedule_basis='anchor_missing_catch_up'`, impTenant, adultNoDOB); n == 0 {
		t.Fatal("adult without DOB has no anchor_missing_catch_up row")
	}
	if n := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND schedule_basis='anchor_missing_catch_up'`, impTenant, adultWithDOB); n != 0 {
		t.Fatalf("adult with DOB has %d catch-up rows, want 0", n)
	}
}
