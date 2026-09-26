package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// ANIMAL TYPES CANNOT BE REMOVED OUT FROM UNDER THE PRODUCT (audit 2026-09-26). Four holes, each
// driven through the real repository:
//
//  1. A stage the product names in code (K0 births, Flushing, the growth ladder) could be archived
//     or deleted whenever no live animal sat in it -- archiving K0 broke every birth.
//  2. A stage could not be marked kid or adult on screen, so one added there was unclassified.
//  3. A species, gender, stage or breed a PUBLISHED vaccination rule names could be removed, leaving
//     the rule silently matching nobody.
//  4. Renaming a breed left feed_ration_groups on the old name, so every adult of that breed lost
//     its ration and was blocked off the feed sheet.
//
// Plus: a built-in species whose is_builtin flag was never written (a farm seeded after 000346)
// is still refused, because the product list decides, not only the flag.
func TestAnimalTypesCannotBeRemovedOutFromUnderTheProduct(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	stageByCode := func(code string) domain.Row {
		t.Helper()
		page, err := repo.List(ctx, cfgTenant, domain.RegStages, ports.ListParams{Status: "all", Limit: 100})
		if err != nil {
			t.Fatalf("list stages: %v", err)
		}
		for _, r := range page.Rows {
			if domain.FieldString(r.Fields, "code") == code {
				return r
			}
		}
		t.Fatalf("stage %s not listed", code)
		return domain.Row{}
	}

	// 1. K0 and Flushing hold no animal here, and are still refused.
	exec(`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order) VALUES ($1::uuid,'K0','Newborn',1), ($1::uuid,'Flushing','Flushing',50), ($1::uuid,'Warmup','Warm-up',60)`, cfgTenant)
	for _, code := range []string{"K0", "Flushing"} {
		row := stageByCode(code)
		if !row.IsBuiltin {
			t.Fatalf("stage %s is named by the product and must read as built in", code)
		}
		if _, err := repo.SetStatus(ctx, write("archive-"+code), domain.RegStages, row.ID, domain.StatusArchived, row.RowVersion); !errors.Is(err, domain.ErrBuiltin) {
			t.Fatalf("archiving %s must be refused as built in, got %v", code, err)
		}
		if err := repo.Delete(ctx, write("delete-"+code), domain.RegStages, row.ID, row.RowVersion); !errors.Is(err, domain.ErrBuiltin) {
			t.Fatalf("deleting %s must be refused as built in, got %v", code, err)
		}
		if _, err := repo.Update(ctx, write("rename-"+code), domain.RegStages, row.ID, map[string]any{"name": code + " (renamed)"}, row.RowVersion); err != nil {
			t.Fatalf("a built-in stage may still be renamed: %v", err)
		}
	}

	// 2. Kid or adult is set, changed and cleared on screen.
	created, err := repo.Create(ctx, write("stage-m1"), domain.RegStages, map[string]any{"name": "Milk 1", "code": "M1", "age_band": "kid"})
	if err != nil {
		t.Fatalf("add a stage with an age band: %v", err)
	}
	if created.IsBuiltin || domain.FieldString(created.Fields, "age_band") != "kid" {
		t.Fatalf("a farm's own stage is not built in and keeps its band, got builtin=%v fields=%v", created.IsBuiltin, created.Fields)
	}
	var band *string
	if err := pool.QueryRow(ctx, `SELECT age_band FROM animal_stage_lookup WHERE tenant_id = $1::uuid AND stage_code = 'M1'`, cfgTenant).Scan(&band); err != nil || band == nil || *band != "kid" {
		t.Fatalf("age_band must be stored for the readers that route on it, got %v (%v)", band, err)
	}
	updated, err := repo.Update(ctx, write("stage-m1-adult"), domain.RegStages, created.ID, map[string]any{"age_band": "adult"}, created.RowVersion)
	if err != nil || domain.FieldString(updated.Fields, "age_band") != "adult" {
		t.Fatalf("change the band to adult: %v %v", err, updated.Fields)
	}
	cleared, err := repo.Update(ctx, write("stage-m1-clear"), domain.RegStages, created.ID, map[string]any{"age_band": ""}, updated.RowVersion)
	if err != nil || domain.FieldString(cleared.Fields, "age_band") != "" {
		t.Fatalf("clearing the band leaves it unclassified: %v %v", err, cleared.Fields)
	}
	if err := repo.Delete(ctx, write("stage-m1-delete"), domain.RegStages, created.ID, cleared.RowVersion); err != nil {
		t.Fatalf("a farm's own unused stage can still be deleted: %v", err)
	}

	// 3. A published vaccination rule holds the stage, species, gender and breed it names.
	exec(`INSERT INTO species_lookup (tenant_id, species_code, name, sort_order) VALUES ($1::uuid,'camel','Camel',30)`, cfgTenant)
	exec(`INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order) VALUES ($1::uuid,'wether','Wether',30)`, cfgTenant)
	breed, err := repo.Create(ctx, write("breed-dromedary"), domain.RegBreeds, map[string]any{"name": "Dromedary", "species": "camel"})
	if err != nil {
		t.Fatalf("add breed: %v", err)
	}
	seedPublishedRule(t, ctx, pool, "camel", "Warmup", "wether", "Dromedary")
	warmup := stageByCode("Warmup")
	assertInUse := func(label string, err error) {
		t.Helper()
		var inUse *ports.InUseError
		if !errors.As(err, &inUse) {
			t.Fatalf("%s named by a published vaccination rule must be refused as in use, got %v", label, err)
		}
	}
	assertInUse("stage", repo.Delete(ctx, write("delete-warmup"), domain.RegStages, warmup.ID, warmup.RowVersion))
	_, err = repo.SetStatus(ctx, write("archive-warmup"), domain.RegStages, warmup.ID, domain.StatusArchived, warmup.RowVersion)
	assertInUse("stage", err)
	assertInUse("breed", repo.Delete(ctx, write("delete-dromedary"), domain.RegBreeds, breed.ID, breed.RowVersion))
	camel, err := repo.Get(ctx, cfgTenant, domain.RegSpecies, "camel")
	if err != nil {
		t.Fatalf("get camel: %v", err)
	}
	// Camel is also held by its breed; the rule must be in the usage sentence on its own.
	usage, err := repo.Usage(ctx, cfgTenant, domain.RegSpecies, camel.ID)
	if err != nil || !usageNames(usage, "vaccination rules") {
		t.Fatalf("camel's usage must name the vaccination rule, got %+v (%v)", usage, err)
	}
	wether, err := repo.Get(ctx, cfgTenant, domain.RegSexes, "wether")
	if err != nil {
		t.Fatalf("get wether: %v", err)
	}
	assertInUse("gender", repo.Delete(ctx, write("delete-wether"), domain.RegSexes, wether.ID, wether.RowVersion))

	// 4. Renaming a breed carries its feed ration mapping.
	exec(`INSERT INTO feed_ration_groups (tenant_id, breed_label, ration_group_label) VALUES ($1::uuid, 'Dromedary', 'Large ruminant')`, cfgTenant)
	if _, err := repo.Update(ctx, write("rename-dromedary"), domain.RegBreeds, breed.ID, map[string]any{"name": "Arabian camel"}, breed.RowVersion); err != nil {
		t.Fatalf("rename breed: %v", err)
	}
	var group string
	if err := pool.QueryRow(ctx, `SELECT ration_group_label FROM feed_ration_groups WHERE tenant_id = $1::uuid AND breed_key = feed_config_norm('Arabian camel')`, cfgTenant).Scan(&group); err != nil || group != "Large ruminant" {
		t.Fatalf("the renamed breed keeps its ration group, got %q (%v)", group, err)
	}

	// A built-in species whose flag was never written is still refused.
	exec(`UPDATE species_lookup SET is_builtin = false WHERE tenant_id = $1::uuid AND species_code = 'sheep'`, cfgTenant)
	sheep, err := repo.Get(ctx, cfgTenant, domain.RegSpecies, "sheep")
	if err != nil || !sheep.IsBuiltin {
		t.Fatalf("sheep is named by the product and reads as built in even without the flag, got %v (%v)", sheep.IsBuiltin, err)
	}
	if _, err := repo.SetStatus(ctx, write("archive-sheep"), domain.RegSpecies, "sheep", domain.StatusArchived, sheep.RowVersion); !errors.Is(err, domain.ErrBuiltin) {
		t.Fatalf("archiving sheep must be refused as built in, got %v", err)
	}
}

func usageNames(u domain.Usage, noun string) bool {
	for _, c := range u.Uses {
		if c.Noun == noun && c.Count > 0 {
			return true
		}
	}
	return false
}

// seedPublishedRule writes one published vaccination protocol whose single rule dimension names the
// given species, stage, sex and breed.
func seedPublishedRule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, species, stage, sex, breed string) {
	t.Helper()
	var protocolID, versionID, ruleID string
	if err := pool.QueryRow(ctx, `INSERT INTO protocol_definitions (tenant_id, code, name, category, status) VALUES ($1::uuid, 'vaccination.cfg_test', 'Test plan', 'vaccination', 'active') RETURNING protocol_id::text`, cfgTenant).Scan(&protocolID); err != nil {
		t.Fatalf("protocol definition: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO protocol_versions (tenant_id, protocol_id, version, effective_from, status) VALUES ($1::uuid, $2::uuid, 1, DATE '2026-01-01', 'draft') RETURNING protocol_version_id::text`, cfgTenant, protocolID).Scan(&versionID); err != nil {
		t.Fatalf("protocol version: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO protocol_rules (tenant_id, protocol_version_id, dose_code, trigger_type) VALUES ($1::uuid, $2::uuid, 'test_dose', 'calendar') RETURNING rule_id::text`, cfgTenant, versionID).Scan(&ruleID); err != nil {
		t.Fatalf("protocol rule: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, species, animal_stage, sex, breed)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'vaccination', 'cfg-test', $4, $5, $6, $7)`, cfgTenant, versionID, ruleID, species, stage, sex, breed); err != nil {
		t.Fatalf("rule dimension: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE protocol_versions SET status = 'published' WHERE protocol_version_id = $1::uuid`, versionID); err != nil {
		t.Fatalf("publish: %v", err)
	}
}
