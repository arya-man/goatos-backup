package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

func TestRecentVaccineAdministrationsIncludesActiveAnchorEvents(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.anchor.history", Name: "Anchor history", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"Blue Tongue","type":"killed","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "blue_tongue_first", Sequence: 1, TriggerType: "birth_age",
		EligibilityJSON: []byte(`{"vaccine":{"code":"Blue Tongue","type":"killed","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000c9"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `UPDATE goats SET species='sheep' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("set species: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b2', $1, $2, 'Blue Tongue', 'blue_tongue_first', DATE '2026-08-12',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'manual BT first dose', 'test', 'anchor-bt-aug12'
)`, impTenant, versionID, goatID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 8, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 1 {
		t.Fatalf("history=%#v, want one anchor administration", history[goatID])
	}
	got := history[goatID][0]
	if got.VaccineCode != "Blue Tongue" || got.DoseCode != "blue_tongue_first" || got.Sequence != 1 {
		t.Fatalf("anchor history=%#v, want Blue Tongue first dose sequence 1", got)
	}
	if got.AdministeredAt.Format("2006-01-02") != "2026-08-12" {
		t.Fatalf("anchor administered_at=%s, want 2026-08-12", got.AdministeredAt.Format(time.RFC3339))
	}
}

func TestRecentVaccineAdministrationsIgnoresUnresolvedAnchorForChaining(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	const goatID = "30000000-0000-4000-8000-0000000000ca"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, vaccine_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b3', $1, 'PPR', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($2::text)), 'lineage-only PPR anchor', 'test', 'anchor-ppr-unresolved'
)`, impTenant, goatID); err != nil {
		t.Fatalf("insert unresolved anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 0 {
		t.Fatalf("history=%#v, want unresolved vaccine-level anchor excluded from synthetic administrations", history[goatID])
	}
}

func TestRecentVaccineAdministrationsIncludesMatrixRuleAnchorEvents(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.matrix.anchor", Name: "Matrix anchor", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"vaccination.matrix"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000cb"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b4', $1, $2, 'PPR', 'ppr_kid', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'matrix PPR anchor', 'test', 'anchor-ppr-matrix'
)`, impTenant, versionID, goatID); err != nil {
		t.Fatalf("insert matrix anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 1 || history[goatID][0].VaccineCode != "PPR" || history[goatID][0].DoseCode != "ppr_kid" {
		t.Fatalf("history=%#v, want matrix rule PPR anchor history", history[goatID])
	}
}

func TestRecentVaccineAdministrationsIgnoresBadAnchorDoseCode(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.bad.anchor", Name: "Bad anchor", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"vaccination.matrix"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000cc"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b5', $1, $2, 'PPR', 'ppr_typo', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'bad dose code anchor', 'test', 'anchor-ppr-bad-dose'
)`, impTenant, versionID, goatID); err != nil {
		t.Fatalf("insert bad anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 0 {
		t.Fatalf("history=%#v, want bad dose-code anchor excluded", history[goatID])
	}
}

func TestRecentVaccineAdministrationsAnchorSurvivesProtocolVersionChange(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.rollover.anchor", Name: "Rollover anchor", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	v1, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		EffectiveTo:   ptrTime(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)),
		RuleDsl:       []byte(`{"vaccine":{"code":"vaccination.matrix"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v1, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age", OffsetDays: 84,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("v1 rule: %v", err)
	}
	v2, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 2, Status: "draft",
		EffectiveFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"vaccination.matrix"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("v2: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: v2, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age", OffsetDays: 84,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("v2 rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id IN ($2, $3)`, impTenant, v1, v2); err != nil {
		t.Fatalf("publish versions: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000cd"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key
) VALUES (
  '40000000-0000-4000-8000-0000000000b6', $1, $2, 'PPR', 'ppr_kid', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'PPR Sep 8 anchor', 'test', 'anchor-ppr-rollover'
)`, impTenant, v1, goatID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 1 {
		t.Fatalf("history=%#v, want one rolled-over anchor administration", history[goatID])
	}
	got := history[goatID][0]
	if got.ProtocolVersionID != v2 || got.DoseCode != "ppr_kid" || got.VaccineCode != "PPR" {
		t.Fatalf("history=%#v, want anchor resolved through current V2 rule", got)
	}
}

func TestRecentVaccineAdministrationsEnforcesAnchorAgeEligibility(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.age.anchor", Name: "Age anchor", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"vaccination.matrix"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "ppr_kid", Sequence: 1, TriggerType: "birth_age", OffsetDays: 84,
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000ce"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob=DATE '2026-08-20' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("set young dob: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_anchor_events (
  vaccination_anchor_event_id, tenant_id, protocol_version_id, vaccine_code, dose_code, anchor_date,
  scope_type, scope_payload, reason, source_system, idempotency_key, enforce_age_eligibility
) VALUES (
  '40000000-0000-4000-8000-0000000000b7', $1, $2, 'PPR', 'ppr_kid', DATE '2026-09-08',
  'animal_set', jsonb_build_object('animal_ids', jsonb_build_array($3::text)), 'underage PPR anchor', 'test', 'anchor-ppr-underage', true
)`, impTenant, versionID, goatID); err != nil {
		t.Fatalf("insert anchor: %v", err)
	}

	history, err := vacc.RecentVaccineAdministrationsForGoats(ctx, impTenant, []string{goatID}, time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("recent administrations: %v", err)
	}
	if len(history[goatID]) != 0 {
		t.Fatalf("history=%#v, want underage anchor excluded", history[goatID])
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
