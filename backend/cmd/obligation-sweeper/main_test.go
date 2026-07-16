package main

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
)

// NOTE: the vaccination shed/execution/operations projection recompute path
// (fakeVaccinationProjectionRefresher + TestScheduledSweeperRefreshesVaccination-
// ReadModelsWithOneAsOf) was removed with the 5k-50k projection cutover
// (docs/decisions/operational-kernel-5k-50k-scale-envelope.md): those three
// screens now serve canonical indexed SQL directly, so the sweeper no longer
// recomputes their read models. Calendar has since completed the same cutover
// (migration 000189): the sweeper no longer refreshes any calendar projection at
// all -- calendarService.RefreshVaccinationProjection and the --project-calendar/
// --calendar-limit/--calendar-date-from/--calendar-date-to flags are gone; the
// sweeper only queues reminders/escalations (--sweep-reminders/--sweep-escalations)
// directly against canonical state.

// TestActorIDAlwaysRequired verifies that the worker cannot start without its
// audited task-creator identity. SOP bindings are normally discovered from the
// published version/rules after flag parsing, so optional-override validation
// cannot safely determine whether task creation will be needed.
func TestActorIDAlwaysRequired(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name: "fails when no actor-id and sop-version-id is set",
			args: []string{
				"-tenant-id", "tenant-1",
				"-sop-version-id", "sop-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required for obligation-sweeper",
		},
		{
			name: "fails when no actor-id and vaccine-item-id is set",
			args: []string{
				"-tenant-id", "tenant-1",
				"-vaccine-item-id", "vaccine-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required for obligation-sweeper",
		},
		{
			name: "succeeds when actor-id is provided",
			args: []string{
				"-tenant-id", "tenant-1",
				"-actor-id", "actor-1",
				"-sop-version-id", "sop-1",
			},
			wantErr: false,
		},
		{
			name: "fails without actor before published rules are loaded",
			args: []string{
				"-tenant-id", "tenant-1",
			},
			wantErr: true,
			errMsg:  "actor-id is required for obligation-sweeper",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := parseFlags(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseFlags(%v) = nil, want error containing %q", tt.args, tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags(%v) = %v, want nil", tt.args, err)
			}
			if cfg.ActorID == "" {
				t.Fatal("config has no actor-id, want validation error")
			}
		})
	}
}

// TestTaskCreatorImplementsBatchTaskCreatorForBulkOperations verifies that
// task creation uses the BatchTaskCreator interface when available to avoid N+1.
// This is C35-004: bulk task creation still does per-batch DB writes.
func TestTaskCreatorImplementsBatchTaskCreatorForBulkOperations(t *testing.T) {
	// This test verifies that sopbridge.Bridge implements both interfaces for bulk operations
	bridge := sopbridge.New(nil, "actor-1")

	// Verify that sopbridge.Bridge implements both TaskCreator and BatchTaskCreator
	var _ app.TaskCreator = bridge
	var _ app.BatchTaskCreator = bridge

	t.Logf("sopbridge.Bridge correctly implements both TaskCreator and BatchTaskCreator for bulk operations")
}

func TestBuildSweepConfigCachesRuleVaccineIdentity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenantID = "00000000-0000-4000-8000-000000000001"
	protocolRepo := protopg.NewRepository(pool, 5*time.Second)
	protoID, err := protocolRepo.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.oneshot.ruleidentity.cache", Name: "OneShotRuleIdentityCache",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := protocolRepo.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"PPR"},"drive_policy":{"priority":2}}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	legacyRuleID, err := protocolRepo.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("legacy rule: %v", err)
	}
	matrixRuleID, err := protocolRepo.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval", DueWindowDays: 7,
		EligibilityJSON: []byte(`{"vaccine":{"code":"fmd","priority":5,"compatibility_group":"combo-a","inventory_item_id":"item-fmd"}}`),
		ProofPolicy:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("matrix rule: %v", err)
	}

	cfg, err := buildSweepConfig(ctx, protocolRepo, config{TenantID: tenantID, DosesPerGoat: 1}, versionID)
	if err != nil {
		t.Fatalf("buildSweepConfig: %v", err)
	}
	if id, ok := cfg.RuleVaccineIDs[legacyRuleID]; ok {
		t.Fatalf("legacy rule must not cache empty vaccine identity, got %#v", id)
	}
	id, ok := cfg.RuleVaccineIDs[matrixRuleID]
	if !ok {
		t.Fatal("matrix rule missing RuleVaccineIDs entry")
	}
	if id.VaccineCode != "fmd" {
		t.Fatalf("matrix vaccine code = %q, want fmd", id.VaccineCode)
	}
	if id.VaccinePriority != 5 {
		t.Fatalf("matrix vaccine priority = %d, want FMD priority 5", id.VaccinePriority)
	}
	if id.CompatibilityGrp != "combo-a" {
		t.Fatalf("matrix compatibility group = %q, want combo-a", id.CompatibilityGrp)
	}
	if id.VaccineItemID != "item-fmd" {
		t.Fatalf("matrix vaccine item id = %q, want item-fmd", id.VaccineItemID)
	}
	ruleCfg, ok := cfg.RuleConfigs[matrixRuleID]
	if !ok {
		t.Fatal("matrix rule with per-rule inventory item must populate RuleConfigs")
	}
	if ruleCfg.VaccineItemID != "item-fmd" {
		t.Fatalf("matrix rule config vaccine item id = %q, want item-fmd", ruleCfg.VaccineItemID)
	}
}
