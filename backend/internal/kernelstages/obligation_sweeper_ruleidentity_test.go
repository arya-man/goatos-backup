package kernelstages

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Baseline fixture tenant seeded by migration 000001_phase_1_identity_foundation.sql (same one
// reminder_cadence_test.go/housekeeping_test.go reuse).
const ruleIdentityTenant = "00000000-0000-4000-8000-000000000001"

// TestBuildSweepConfigOnlyCachesCompleteRuleVaccineIdentity is the R2-05(a) guard for
// ObligationSweeperStage.buildSweepConfig's caching contract: a rule whose eligibility_json carries
// NO vaccine (a legacy, non-matrix rule) must be left OUT of SweepConfig.RuleVaccineIDs entirely --
// never cached as an empty RuleVaccineIdentity{} -- so obligationapp.SweepConfig's
// getRuleVaccineIdentity cache-miss fallback resolves it to the version-level identity instead of
// comparing/tie-checking it as a blank-code, zero-priority phantom vaccine that silently defeats cap
// detection for every real vaccine it happens to compete with. A rule whose eligibility_json DOES
// carry a vaccine (a matrix rule) must be cached with its real code/priority.
func TestBuildSweepConfigOnlyCachesCompleteRuleVaccineIdentity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: ruleIdentityTenant, Code: "vaccination.ruleidentity.cache", Name: "RuleIdentityCache",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: ruleIdentityTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	legacyRuleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: ruleIdentityTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("legacy rule: %v", err)
	}
	matrixRuleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: ruleIdentityTenant, ProtocolVersionID: versionID, DoseCode: "booster_1", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"fmd","compatibility_group":"combo-a","inventory_item_id":"item-fmd"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("matrix rule: %v", err)
	}

	stage := NewObligationSweeperStage(deps, SweeperConfig{TenantID: ruleIdentityTenant})
	cfg, err := stage.buildSweepConfig(ctx, SweeperConfig{TenantID: ruleIdentityTenant}, versionID, nil)
	if err != nil {
		t.Fatalf("buildSweepConfig: %v", err)
	}

	if id, ok := cfg.RuleVaccineIDs[legacyRuleID]; ok {
		t.Fatalf("legacy non-matrix rule must be ABSENT from RuleVaccineIDs (cache-miss fallback), got cached entry %#v", id)
	}
	id, ok := cfg.RuleVaccineIDs[matrixRuleID]
	if !ok {
		t.Fatal("matrix rule must be cached in RuleVaccineIDs, got no entry")
	}
	if id.VaccineCode != "fmd" {
		t.Fatalf("matrix rule cached vaccine code = %q, want fmd", id.VaccineCode)
	}
	if id.VaccineItemID != "item-fmd" {
		t.Fatalf("matrix rule cached vaccine item id = %q, want item-fmd", id.VaccineItemID)
	}
	ruleCfg, ok := cfg.RuleConfigs[matrixRuleID]
	if !ok {
		t.Fatal("matrix rule with per-rule inventory item must populate RuleConfigs")
	}
	if ruleCfg.VaccineItemID != "item-fmd" {
		t.Fatalf("matrix rule config vaccine item id = %q, want item-fmd", ruleCfg.VaccineItemID)
	}
}
