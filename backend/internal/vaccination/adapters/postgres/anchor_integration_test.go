package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestCreateAnchorValidatesAndIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.create.anchor", Name: "Create anchor", Category: "vaccination", Status: "draft"})
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
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","type":"live","pathogen_class":"viral"},"eligibility":{"species":["goat","sheep"]}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000cf"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'RFID-PPR-001', 'RFID-PPR-001', 'global', true, 'active', now(), 'test_v1')`, impTenant, goatID); err != nil {
		t.Fatalf("identifier: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"animal_ids": []string{goatID}})
	cmd := domain.AnchorCommand{
		TenantID: impTenant, VaccineCode: "PPR", DoseCode: "ppr_kid",
		AnchorDate: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		Scope:      domain.AnchorScope{Type: "animal_set", Payload: payload},
		Reason:     "Sep 8 PPR drive", SourceSystem: "test", IdempotencyKey: "create-anchor-ppr-1", RequestHash: strings.Repeat("a", 64),
		SuppressBeforeAnchor: true, ChainFutureFromAnchor: true, EnforceAgeEligibility: true,
	}
	preview, err := vacc.CreateAnchor(ctx, cmd)
	if err != nil {
		t.Fatalf("create anchor: %v", err)
	}
	if !preview.Applied || preview.AnchorEventID == "" || preview.EligibleAnimals != 1 || preview.EligibleSample[0].Identifier != "RFID-PPR-001" {
		t.Fatalf("preview=%+v", preview)
	}
	replay, err := vacc.CreateAnchor(ctx, cmd)
	if err != nil {
		t.Fatalf("replay anchor: %v", err)
	}
	if replay.Applied || replay.AnchorEventID != preview.AnchorEventID {
		t.Fatalf("replay=%+v first=%+v", replay, preview)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM vaccination_anchor_events WHERE tenant_id=$1 AND idempotency_key='create-anchor-ppr-1'`, impTenant).Scan(&count); err != nil {
		t.Fatalf("count anchors: %v", err)
	}
	if count != 1 {
		t.Fatalf("anchor rows=%d, want 1", count)
	}
}

func TestCreateAnchorAcceptsActiveMatrixDoseAliasAsCanonicalRule(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	vacc := NewRepository(pool, 5*time.Second)
	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{TenantID: impTenant, Code: "vaccination.anchor.alias", Name: "Anchor alias", Category: "vaccination", Status: "draft"})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl: []byte(`{"vaccine":{"code":"vaccination.matrix"},"matrix_rows":[{"vaccine":{"code":"HS"},"schedule":[` +
			`{"dose_code":"hs_kid_16w","source_dose_code":"hs_kid_16w","sequence":9,"trigger_type":"birth_age","offset_days":84}` +
			`]}]}`),
		ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "hs_kid_12w", Sequence: 9, TriggerType: "birth_age", OffsetDays: 84,
		EligibilityJSON: []byte(`{"vaccine":{"code":"HS","type":"killed","pathogen_class":"bacterial"},"eligibility":{"species":["goat","sheep"]}}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status='published' WHERE tenant_id=$1 AND protocol_version_id=$2`, impTenant, versionID); err != nil {
		t.Fatalf("publish version: %v", err)
	}
	const goatID = "30000000-0000-4000-8000-0000000000d0"
	seedGenGoatWithStage(t, ctx, pool, goatID, "alive", "K1")
	if _, err := pool.Exec(ctx, `UPDATE goats SET dob=DATE '2026-01-01' WHERE tenant_id=$1 AND goat_id=$2`, impTenant, goatID); err != nil {
		t.Fatalf("set dob: %v", err)
	}
	payload, _ := json.Marshal(map[string]any{"animal_ids": []string{goatID}})
	cmd := domain.AnchorCommand{
		TenantID: impTenant, VaccineCode: "HS", DoseCode: "hs_kid_16w",
		AnchorDate: time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		Scope:      domain.AnchorScope{Type: "animal_set", Payload: payload},
		Reason:     "HS alias drive", SourceSystem: "test", IdempotencyKey: "create-anchor-hs-alias", RequestHash: strings.Repeat("b", 64),
		SuppressBeforeAnchor: true, ChainFutureFromAnchor: true, EnforceAgeEligibility: true,
	}
	preview, err := vacc.CreateAnchor(ctx, cmd)
	if err != nil {
		t.Fatalf("create alias anchor: %v", err)
	}
	if preview.DoseCode != "hs_kid_12w" || preview.EligibleAnimals != 1 {
		t.Fatalf("preview=%+v, want canonical hs_kid_12w with one eligible animal", preview)
	}
	var storedDose string
	if err := pool.QueryRow(ctx, `SELECT dose_code FROM vaccination_anchor_events WHERE tenant_id=$1 AND idempotency_key='create-anchor-hs-alias'`, impTenant).Scan(&storedDose); err != nil {
		t.Fatalf("stored dose: %v", err)
	}
	if storedDose != "hs_kid_12w" {
		t.Fatalf("stored dose=%q, want canonical hs_kid_12w", storedDose)
	}

	cmd.IdempotencyKey = "create-anchor-hs-bad"
	cmd.RequestHash = strings.Repeat("c", 64)
	cmd.DoseCode = "hs_kid_20w"
	if _, err := vacc.CreateAnchor(ctx, cmd); !errors.Is(err, domain.ErrInvalidAnchor) {
		t.Fatalf("bad alias err=%v, want invalid anchor", err)
	}
}

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
