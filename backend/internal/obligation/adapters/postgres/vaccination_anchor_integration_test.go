package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestCancelOpenVaccinationObligationsBeforeActiveAnchors(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, dob)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $4, DATE '2026-05-01')`,
		testGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{TenantID: tenantID, Code: "vaccination.anchor", Name: "Anchor", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	beforeID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-before", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert before: applied=%v err=%v", applied, err)
	}
	afterID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-after", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert after: applied=%v err=%v", applied, err)
	}
	sameDayID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-same-day", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert same-day: applied=%v err=%v", applied, err)
	}
	inProgressID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), Status: "in_progress", IdempotencyKey: "anchor-in-progress", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("insert in-progress: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000a1', $1, $2, 'PPR', 'ppr_kid', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'manual Sep 8 anchor', 'test', 'anchor-ppr-sep8'
)`, tenantID, versionID, testGoatID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	changed, err := repo.CancelOpenVaccinationObligationsBeforeActiveAnchors(ctx, tenantID, []string{testGoatID}, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("cancel before anchors: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed=%d, want 2", changed)
	}
	assertCanceledOnce(t, ctx, pool, "pre-anchor", beforeID)
	assertCanceledOnce(t, ctx, pool, "in-progress pre-anchor", inProgressID)
	assertUntouched(t, ctx, pool, "same-day anchor", sameDayID)
	assertUntouched(t, ctx, pool, "post-anchor", afterID)
}

func TestCancelOpenVaccinationObligationsBeforeVaccineLevelAnchor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, dob)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $4, DATE '2026-05-01')`,
		testGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{TenantID: tenantID, Code: "vaccination.anchor.vaccine", Name: "Anchor vaccine", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	kidRuleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("kid rule: %v", err)
	}
	adultRuleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "ppr_adult_w1", Sequence: 10, TriggerType: "manual_campaign",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("adult rule: %v", err)
	}
	kidID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: kidRuleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-vaccine-kid", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert kid: applied=%v err=%v", applied, err)
	}
	adultID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: adultRuleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-vaccine-adult", Sequence: 10,
	})
	if err != nil || !applied {
		t.Fatalf("insert adult: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000a5', $1, $2, 'PPR', NULL, DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'manual Sep 8 vaccine anchor', 'test', 'anchor-ppr-vaccine-sep8'
)`, tenantID, versionID, testGoatID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}

	changed, err := repo.CancelOpenVaccinationObligationsBeforeActiveAnchors(ctx, tenantID, []string{testGoatID}, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("cancel before anchors: %v", err)
	}
	if changed != 2 {
		t.Fatalf("changed=%d, want vaccine-level anchor to cancel both dose rows", changed)
	}
	assertCanceledOnce(t, ctx, pool, "kid pre-anchor", kidID)
	assertCanceledOnce(t, ctx, pool, "adult pre-anchor", adultID)
}

func TestCancelOpenVaccinationObligationsBeforeTenantAnchorResolvesScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, dob)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4, $4, DATE '2026-05-01')`,
		testGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{TenantID: tenantID, Code: "vaccination.anchor.tenant", Name: "Anchor tenant", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age", OffsetDays: 112,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	beforeID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID, TargetType: "goat", TargetID: testGoatID,
		ScopeType: "park", ScopeID: cbePark, DueAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: "anchor-tenant-before", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("insert before: applied=%v err=%v", applied, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, tenantID, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b1', $1, $2, 'PPR', 'ppr_kid', DATE '2026-09-08',
  'tenant', '{}'::jsonb, 'tenant Sep 8 anchor', 'test', 'anchor-ppr-sep8-tenant'
)`, tenantID, versionID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}
	changed, err := repo.CancelOpenVaccinationObligationsBeforeActiveAnchors(ctx, tenantID, nil, time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("cancel before tenant anchor: %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed=%d, want 1", changed)
	}
	assertCanceledOnce(t, ctx, pool, "tenant pre-anchor", beforeID)
}
