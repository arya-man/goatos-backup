package postgres

import (
	"context"
	"testing"
	"time"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// TestSM7BoosterSchedulesNextDose drives SM-7: a published series with an age-triggered dose 1 and
// an after_previous_completion dose 2 (offset 21d, min-gap 28d). SM-1 schedules dose 1 only.
// Accepting dose 1 at T schedules dose 2 due T+28 (the min-gap dominates the 21d offset). The
// booster is idempotent on replay.
func TestSM7BoosterSchedulesNextDose(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: impTenant, Code: "vaccination.booster", Name: "Booster", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	ruleDSL := []byte(`{"eligibility":{"stage":"K1"},` +
		`"source":{"source_system":"phc","source_ref":"PHC §6","review_status":"approved","approved_by":"Reviewer"}}`)
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: impTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: ruleDSL, ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	// Dose 1: age-triggered. Dose 2: after_previous_completion, offset 21d but min-gap 28d.
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", OffsetDays: 21, Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule1: %v", err)
	}
	if _, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: impTenant, ProtocolVersionID: versionID, DoseCode: "booster", Sequence: 2,
		TriggerType: "after_previous_completion", OffsetDays: 21, MinGapDays: 28, Repeat: "none",
		CatchUp: "phc_approval", EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	}); err != nil {
		t.Fatalf("rule2: %v", err)
	}
	if err := proto.PublishVersion(ctx, impTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	const g = "30000000-0000-4000-8000-0000000000e1"
	seedGenGoat(t, ctx, pool, g, "alive")

	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	asOf := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	// SM-1 schedules dose 1 only; dose 2 (after_previous_completion) is deferred to SM-7.
	if res, err := gen.GenerateForVersion(ctx, impTenant, versionID, asOf); err != nil || res.Generated != 1 {
		t.Fatalf("generate: res=%+v err=%v", res, err)
	}
	ob1 := scanText(t, ctx, pool, `SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND "sequence"=1`, impTenant, g)

	booster := vaccapp.NewBoosterService(proto, obl)
	completion := vaccapp.NewCompletionService(vaccapp.NewService(vacc), obl, nil).WithBooster(booster)

	administered := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	doses := int32(1)
	ar, err := completion.Accept(ctx, vaccapp.AcceptInput{
		Completion: vaccdomain.NewCompletion{
			TenantID: impTenant, ObligationID: ob1, GoatID: g, Doses: &doses, RouteSite: "SC",
			AdministeredAt: administered, IdempotencyKey: "rec-boost-g",
		},
		ProtocolVersionID: versionID, ScopeType: "park", ScopeID: impCbe, RuleSequence: 1,
	})
	if err != nil || !ar.Applied || !ar.Completed || !ar.NextScheduled {
		t.Fatalf("accept dose1: %+v err=%v", ar, err)
	}

	// Dose 2 obligation exists, scheduled, due = administered + 28 (min-gap dominates the 21d offset).
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND "sequence"=2`, impTenant, g); got != 1 {
		t.Fatalf("want 1 dose-2 obligation, got %d", got)
	}
	if due := scanText(t, ctx, pool, `SELECT due_at::date::text FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND "sequence"=2`, impTenant, g); due != "2026-07-21" {
		t.Fatalf("dose-2 due: want 2026-07-21 (administered + 28d), got %s", due)
	}
	if st := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND "sequence"=2`, impTenant, g); st != "scheduled" {
		t.Fatalf("dose-2 status: want scheduled, got %s", st)
	}

	// Booster is idempotent: a direct replay schedules nothing new.
	scheduled, err := booster.ScheduleNextDose(ctx, vaccapp.ScheduleNextInput{
		TenantID: impTenant, ProtocolVersionID: versionID, GoatID: g,
		ScopeType: "park", ScopeID: impCbe, PrevSequence: 1, AdministeredAt: administered,
	})
	if err != nil {
		t.Fatalf("replay booster: %v", err)
	}
	if scheduled {
		t.Fatalf("booster replay should be a no-op")
	}
	if got := countRowsVacc(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND "sequence"=2`, impTenant, g); got != 1 {
		t.Fatalf("booster replay created a duplicate dose-2, count=%d", got)
	}
}
