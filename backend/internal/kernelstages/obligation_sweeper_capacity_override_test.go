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

// TestBuildSweepConfigCapacityShotCapOverride proves the admin-editable per-animal shot-cap override
// (vaccination_capacity_config.max_shots_per_animal_per_drive, migration 000045) wins over the
// published rule_dsl drive_policy value when set, and the DSL value survives untouched when the
// override is nil (no admin override authored).
func TestBuildSweepConfigCapacityShotCapOverride(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 5 * time.Second}, Logger: logger}

	proto := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: ruleIdentityTenant, Code: "vaccination.capacityoverride", Name: "CapacityOverride",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	// DSL authors max_shots_per_animal_per_drive = 5.
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: ruleIdentityTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"fmd"},"drive_policy":{"max_shots_per_animal_per_drive":5}}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}

	stage := NewObligationSweeperStage(deps, SweeperConfig{TenantID: ruleIdentityTenant})

	// No override: DSL value (5) survives.
	cfg, err := stage.buildSweepConfig(ctx, SweeperConfig{TenantID: ruleIdentityTenant}, versionID, nil)
	if err != nil {
		t.Fatalf("buildSweepConfig (no override): %v", err)
	}
	if cfg.DrivePlanner.MaxShotsPerAnimalPerDrive != 5 {
		t.Fatalf("MaxShotsPerAnimalPerDrive (no override) = %d want 5 (DSL value)", cfg.DrivePlanner.MaxShotsPerAnimalPerDrive)
	}

	// Admin override = 2 wins over the DSL value.
	override := int32(2)
	cfg, err = stage.buildSweepConfig(ctx, SweeperConfig{TenantID: ruleIdentityTenant}, versionID, &override)
	if err != nil {
		t.Fatalf("buildSweepConfig (with override): %v", err)
	}
	if cfg.DrivePlanner.MaxShotsPerAnimalPerDrive != 2 {
		t.Fatalf("MaxShotsPerAnimalPerDrive (override=2) = %d want 2 (admin override wins)", cfg.DrivePlanner.MaxShotsPerAnimalPerDrive)
	}
}
