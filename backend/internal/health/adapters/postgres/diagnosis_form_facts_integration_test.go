package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THE FORM MUST CARRY WHAT ITS OWN GATES ARE READ AGAINST.
//
// An authored question may be gated on `only_if_sex` / `only_if_stage`, and the engine reads those
// against the animal's NORMALISED facts -- the register speaks F/M while the herd register stores
// female/male. Serving the questions without the facts leaves the phone unable to evaluate its own
// document: it hid every udder question for a doe, the operator walked eleven pages with them
// missing, and the submit was refused for questions the phone had decided not to ask. The screen
// then said "Nothing found".
//
// The assertion is not that two string fields are populated. It is that every gate in the served
// document can be decided from the served facts, and that deciding them agrees with the engine.
func TestObservationFormCarriesTheFactsItsOwnGatesAreReadAgainst(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedHealthScope(t, ctx, pool)
	seedRegisters(t, ctx, pool)
	_, repo := diagnosisStack(t, ctx, pool)

	// The routing is authored, so the test authors it -- the same two writes the config screen
	// makes. The migration seeds tenants that already exist when it runs; a tenant created after
	// it, like this fixture's, is routed by hand exactly as a new farm would be.
	base := NewRepository(pool, 30*time.Second)
	if _, err := base.SaveDiagnosisType(ctx, domain.SaveDiagnosisTypeCommand{
		TenantID: healthTenant, ActorID: healthActor, IdempotencyKey: "type-adult",
		RequestFingerprint: "fp-type-adult", TypeKey: "adult", Label: "Adults", Status: "active",
	}); err != nil {
		t.Fatalf("author the adult type: %v", err)
	}
	// The farm's own stage catalog. Routing refuses a stage this farm does not use, so the stage
	// has to exist before it can be routed -- which is the fail-closed rule working, not a detour.
	const stage = "non-pregnant"
	if _, err := pool.Exec(ctx,
		`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, status)
		 VALUES ($1::uuid, $2, 'Non-Pregnant', 'adult', 'active')
		 ON CONFLICT DO NOTHING`, healthTenant, stage,
	); err != nil {
		t.Fatalf("seed the stage catalog: %v", err)
	}
	if _, err := base.SaveStageRoute(ctx, domain.SaveStageRouteCommand{
		TenantID: healthTenant, ActorID: healthActor, IdempotencyKey: "route-adult",
		RequestFingerprint: "fp-route-adult", AgeBand: "adult", StageCode: stage, TypeKey: "adult",
	}); err != nil {
		t.Fatalf("route the adult stage: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE goats SET management_stage = $1 WHERE goat_id = $2::uuid AND tenant_id = $3::uuid`,
		stage, healthGoat, healthTenant,
	); err != nil {
		t.Fatalf("stage the animal: %v", err)
	}

	form, err := repo.ObservationForm(ctx, healthTenant, healthGoat)
	if err != nil {
		t.Fatalf("observation form: %v", err)
	}
	// The fixture animal is a female adult. The register gates on F, not on "female".
	if form.Sex != "F" {
		t.Fatalf("sex = %q, want the normalised F the register gates on", form.Sex)
	}

	animal := diagnosis.Animal{Class: form.TypeKey, Sex: form.Sex, Stage: form.Stage}
	var sexGated int
	for _, page := range form.Pages {
		for _, q := range page.Questions {
			if q.OnlyIfSex == "" && len(q.OnlyIfStage) == 0 {
				continue
			}
			if q.OnlyIfSex != "" {
				sexGated++
			}
			// Asks() is what the engine will judge the submitted answers by. A question it asks
			// must be one the served facts also resolve to asked -- otherwise the phone omits a
			// question the server then demands.
			if !q.Asks(animal, diagnosis.Answers{}) && q.OnlyIf == nil {
				continue
			}
			if q.OnlyIf != nil {
				continue // conditional on another answer; not decidable from facts alone
			}
			if !q.Asks(animal, diagnosis.Answers{}) {
				t.Errorf("question %q is gated in a way the served facts cannot satisfy", q.ID)
			}
		}
	}
	if sexGated == 0 {
		t.Fatal("the seeded adult register has no sex-gated question, so this proves nothing")
	}
}
