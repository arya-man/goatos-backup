package postgres

import (
	"context"
	"testing"
	"time"

	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// MAINTAINER DECISION 2026-09-25, proved on the production path against the database: a
// disease authored and published in Health Config joins the cause-of-death list, a death
// can be recorded under it, and once it is retired a NEW death is refused while the death
// already filed under it keeps its record and its name.
func TestHealthConfigDiseaseBecomesACauseOfDeath(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedHealthScope(t, ctx, pool)
	stockMedicineForTest(t, ctx, pool, "Meloxicam Paracetamol")

	repo := NewRepository(pool, 10*time.Second)
	deathCauses := healthapp.NewDeathCauseCatalogService().WithTenantSource(repo)
	// The same wiring bootstrap uses: a Health Config write drops the tenant's cached list.
	config := healthapp.NewConfigService(repo).WithMedicineCatalog(repo).
		WithRulebookChanged(deathCauses.Invalidate)

	offered := func() map[string]domain.DeathCauseOption {
		t.Helper()
		catalog, err := deathCauses.Catalog(ctx, healthTenant)
		if err != nil {
			t.Fatalf("death causes: %v", err)
		}
		out := map[string]domain.DeathCauseOption{}
		for _, o := range catalog.Options {
			out[o.Key] = o
		}
		return out
	}

	// Before: the built-in register only.
	before := offered()
	if _, ok := before["MASTITIS"]; !ok {
		t.Fatalf("built-in MASTITIS missing before any authoring")
	}
	if _, ok := before["snake_bite"]; ok {
		t.Fatalf("snake_bite offered before anyone authored it")
	}

	// A bare draft is not yet a disease the farm treats: not offered, not accepted.
	created, err := config.CreateDisease(ctx, createDiseaseCmd("Snake bite", "snake-bite"))
	if err != nil {
		t.Fatalf("create disease: %v", err)
	}
	if _, ok := offered()["snake_bite"]; ok {
		t.Fatalf("an unpublished draft disease is offered as a cause of death")
	}
	if err := deathCauses.ValidateCause(ctx, healthTenant, "snake_bite", domain.DeathCauseKindDiseaseKey); err == nil {
		t.Fatalf("an unpublished draft disease was accepted on a death")
	}

	// Author and publish the course -- the Health Config write path.
	if _, err := config.SaveDraft(ctx, domain.SaveDraftCommand{
		TenantID: healthTenant, ActorID: healthActor,
		DiseaseKey: "snake_bite", AgeBand: domain.AgeBandAdult,
		Protocol:       completeCourse("Snake bite"),
		IdempotencyKey: "save-snake", RequestFingerprint: "fp-save-snake",
	}); err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if _, err := config.PublishDraft(ctx, domain.ProtocolVersionCommand{
		TenantID: healthTenant, ActorID: healthActor,
		ProtocolVersionID: created.DraftVersionIDs[domain.AgeBandAdult],
		IdempotencyKey:    "publish-snake", RequestFingerprint: "fp-publish-snake",
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	option, ok := offered()["snake_bite"]
	if !ok {
		t.Fatalf("a published Health Config disease is not offered as a cause of death")
	}
	if option.Kind != domain.DeathCauseKindDiseaseKey || option.Label != "Snake bite" {
		t.Fatalf("option = %+v, want kind disease_key labelled Snake bite", option)
	}
	// The check Counts runs when the death is raised.
	if err := deathCauses.ValidateCause(ctx, healthTenant, option.Key, option.Kind); err != nil {
		t.Fatalf("the death form's own option was refused: %v", err)
	}
	// The death is approved: the approved-death consumer files the cause.
	if err := repo.CloseForApprovedDeath(ctx, healthTenant, healthGoat,
		domain.DeathCause{Key: option.Key, Kind: option.Kind}); err != nil {
		t.Fatalf("record the approved death: %v", err)
	}
	assertHealthCount(t, ctx, pool, "death filed under the Health Config disease",
		`SELECT count(*)::int FROM health_death_causes WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND cause_key='snake_bite' AND cause_kind='disease_key'`,
		1, healthTenant, healthGoat)

	// A published diagnosis register the farm authored contributes its problem rules too,
	// and its field actions do not.
	if _, err := pool.Exec(ctx, `
INSERT INTO health_diagnosis_register_versions
  (tenant_id, animal_class, version, status, register_label, document, content_hash, published_at)
VALUES ($1::uuid, 'adult', 99, 'published', 'adult-authored-test',
        '{"register_version":"adult-authored-test","rules":[{"id":"SNAKE_ENVENOMATION"},{"id":"GATE_CHECK","kind":"field"}]}'::jsonb,
        'hash-test', now())
ON CONFLICT (tenant_id, animal_class) WHERE status = 'published'
DO UPDATE SET document = EXCLUDED.document, register_label = EXCLUDED.register_label`, healthTenant); err != nil {
		t.Fatalf("seed an authored register: %v", err)
	}
	deathCauses.Invalidate(healthTenant)
	withRegister := offered()
	if o := withRegister["SNAKE_ENVENOMATION"]; o.Kind != domain.DeathCauseKindRegisterRule {
		t.Fatalf("an authored register diagnosis is not offered: %+v", o)
	}
	if _, ok := withRegister["GATE_CHECK"]; ok {
		t.Fatalf("a field action from an authored register is offered as a cause of death")
	}

	// RETIRED: every version of the disease leaves service.
	if _, err := pool.Exec(ctx, `
UPDATE health_protocol_versions SET status='retired'
WHERE tenant_id=$1::uuid AND disease_key='snake_bite' AND status='published'`, healthTenant); err != nil {
		t.Fatalf("retire: %v", err)
	}
	deathCauses.Invalidate(healthTenant)
	if _, ok := offered()["snake_bite"]; ok {
		t.Fatalf("a retired disease is still offered to a new death")
	}
	if err := deathCauses.ValidateCause(ctx, healthTenant, "snake_bite", domain.DeathCauseKindDiseaseKey); err == nil {
		t.Fatalf("a retired disease was accepted on a NEW death")
	}
	// ...while the death already filed under it is untouched and still reads by name.
	assertHealthCount(t, ctx, pool, "the recorded death after the retirement",
		`SELECT count(*)::int FROM health_death_causes WHERE tenant_id=$1::uuid AND goat_id=$2::uuid AND cause_key='snake_bite'`,
		1, healthTenant, healthGoat)
	if label := deathCauses.LabelDeathCause(ctx, healthTenant, "snake_bite"); label != "Snake bite" {
		t.Fatalf("history label = %q, want Snake bite", label)
	}
}
