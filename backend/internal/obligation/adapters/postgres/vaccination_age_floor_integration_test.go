package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func seedVaccinationAgeFloor(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	goatID := "10000000-0000-4000-8000-0000000000af"
	dob := time.Date(2026, time.September, 16, 0, 0, 0, 0, biztime.DefaultLocation())
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, dob)
VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $5)`, goatID, tenantID, meshaParty, cbePark, dob); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protocolID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.age_floor", Name: "Vaccination age floor", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("create protocol: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protocolID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: dob, RuleDsl: []byte(`{"procurement_policy":{"purpose_plans":{"fattening":{"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox"],"sheep_second_wave":["Sheep Pox"]}}}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "et_tt_kid_4w", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 28, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT","name":"ET+TT"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `
WITH load AS (
  INSERT INTO procurement_loads (tenant_id, source_party_id, status, idempotency_key)
  VALUES ($1::uuid, $2::uuid, 'accepted_intake', 'vaccination-age-floor-purpose')
  RETURNING load_id
)
INSERT INTO procurement_load_goats (
  tenant_id, load_id, goat_id, purpose, selection_state, current_state,
  source_entry_state, ownership_state, health_state, warmup_started_at, intake_accepted_at
)
SELECT $1::uuid, load_id, $3::uuid, 'fattening', 'accepted', 'accepted_herd_intake',
       'accepted', 'mesha_owned', 'passed', $4, $4
FROM load`, tenantID, meshaParty, goatID, dob); err != nil {
		t.Fatalf("seed fattening purpose: %v", err)
	}
	return goatID, ruleID
}

func TestVaccinationBirthAgeFloorGuardsEveryWritePath(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	goatID, ruleID := seedVaccinationAgeFloor(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	versionID := mustVersionOf(t, ctx, pool)
	early := time.Date(2026, time.September, 24, 0, 0, 0, 0, biztime.DefaultLocation())
	floor := time.Date(2026, time.October, 14, 0, 0, 0, 0, biztime.DefaultLocation())
	newRow := func(key, status string, due time.Time) domain.NewObligation {
		return domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: status, IdempotencyKey: key,
			RuleIdentityKey: "et+tt|et_tt_kid_4w|1", Sequence: 1,
		}
	}

	if _, _, err := repo.InsertObligation(ctx, newRow("early-insert", "scheduled", early)); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early insert error=%v, want age-floor error", err)
	}
	if _, _, err := repo.InsertDeferredObligation(ctx, newRow("early-deferred", "deferred", early), "warming_hold", early); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early deferred insert error=%v, want age-floor error", err)
	}
	id, applied, err := repo.InsertObligation(ctx, newRow("valid-floor", "scheduled", floor))
	if err != nil || !applied {
		t.Fatalf("valid insert id=%q applied=%v err=%v", id, applied, err)
	}
	if _, _, err := repo.ReconcileOpenObligationForRuleIdentity(ctx, tenantID, newRow("reconcile-early", "scheduled", early), early); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early reconcile error=%v, want age-floor error", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='deferred' WHERE obligation_id=$1::uuid`, id); err != nil {
		t.Fatalf("defer valid row: %v", err)
	}
	if _, _, err := repo.ReopenDeferredObligationForGeneration(ctx, tenantID, "valid-floor", early, &domain.RecoveryReschedule{
		DueAt: early, WindowStart: early,
	}); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early recovery reopen error=%v, want age-floor error", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET due_at=$2, status='deferred' WHERE obligation_id=$1::uuid`, id, early); err != nil {
		t.Fatalf("seed legacy early deferred row: %v", err)
	}
	if _, _, err := repo.ReopenDeferredObligationForGeneration(ctx, tenantID, "valid-floor", early, nil); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("nil-reschedule recovery reopen error=%v, want age-floor error", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE obligation_instances SET status='scheduled', due_at=$2 WHERE obligation_id=$1::uuid`, id, floor); err != nil {
		t.Fatalf("restore valid row: %v", err)
	}
	if _, _, err := repo.RescheduleObligationByID(ctx, tenantID, id, "reschedule-early", nil, early, early, nil, early); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early reschedule error=%v, want age-floor error", err)
	}
	var got time.Time
	if err := pool.QueryRow(ctx, `SELECT due_at FROM obligation_instances WHERE obligation_id=$1::uuid`, id).Scan(&got); err != nil {
		t.Fatalf("read due date: %v", err)
	}
	if !got.Equal(floor) {
		t.Fatalf("persisted due=%s, want unchanged floor=%s", got, floor)
	}

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	fmdRuleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "fmd_kid_12w", Sequence: 2,
		TriggerType: "birth_age", OffsetDays: 84, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"FMD","name":"FMD"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create FMD rule: %v", err)
	}
	invalid := newRow("fmd-not-applicable", "scheduled", time.Date(2026, time.December, 9, 0, 0, 0, 0, biztime.DefaultLocation()))
	invalid.RuleID = fmdRuleID
	invalid.RuleIdentityKey = "fmd|fmd_kid_12w|2"
	invalid.Sequence = 2
	if _, _, err := repo.InsertObligation(ctx, invalid); !errors.Is(err, ports.ErrVaccinationNotApplicable) {
		t.Fatalf("fattening FMD insert error=%v, want applicability error", err)
	}

	postArrivalRuleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "goat_pox_adult_w1", Sequence: 3,
		TriggerType: "post_arrival", OffsetDays: 28, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"GOAT_POX","name":"Goat Pox"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create post-arrival rule: %v", err)
	}
	postArrival := newRow("pox-before-second-wave", "scheduled", early)
	postArrival.RuleID = postArrivalRuleID
	postArrival.RuleIdentityKey = "goat_pox|goat_pox_adult_w1|3"
	postArrival.Sequence = 3
	if _, _, err := repo.InsertObligation(ctx, postArrival); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early post-arrival insert error=%v, want rule-floor error", err)
	}

	administeredAt := time.Date(2026, time.October, 14, 9, 0, 0, 0, biztime.DefaultLocation())
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  tenant_id, obligation_id, goat_id, administered_at, status, verified_at, idempotency_key
) VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'accepted', $4, 'vaccination-floor-ettt-history')`, tenantID, id, goatID, administeredAt); err != nil {
		t.Fatalf("seed accepted ET+TT completion: %v", err)
	}
	afterPreviousRuleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "et_tt_adult_w2", Sequence: 4,
		TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 21, DueWindowDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT","name":"ET+TT"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("create after-previous rule: %v", err)
	}
	booster := newRow("ettt-booster-too-early", "scheduled", administeredAt.AddDate(0, 0, 7))
	booster.RuleID = afterPreviousRuleID
	booster.RuleIdentityKey = "et+tt|et_tt_adult_w2|4"
	booster.Sequence = 4
	if _, _, err := repo.InsertObligation(ctx, booster); !errors.Is(err, ports.ErrBeforeVaccinationAgeFloor) {
		t.Fatalf("early after-previous insert error=%v, want rule-floor error", err)
	}
}
