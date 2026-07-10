package e2e

import (
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// TestKernelStoryP_DeathCancelsScheduledDeferredMissed extends Story I's death/exit coverage with the
// edge dataset Story I did not seed: a goat that dies while its open obligations span EVERY open
// status the kernel recognizes, not just plain 'scheduled' rows -- one obligation already attached to
// a planned drive batch, one 'deferred' (mid-quarantine when death occurs), one 'missed' (window
// already crossed), plus a 'completed' historical dose that must survive. Per
// CancelOpenForGoatAt's own doc comment (backend/internal/obligation/adapters/postgres/repository.go),
// the real SM-3 death handler cancels
// status IN ('scheduled','due','in_progress','deferred','missed') -- this story is the first to prove
// the deferred/missed/batched cases, since Story I only ever seeded 'scheduled' rows.
//
// This also asserts the batch-repair side effect (the batch's estimated_targets is reduced when a
// batched obligation is cancelled) and the durable outbox row the kernel's golden rule requires
// (event -> transaction -> audit + outbox), which Story I did not check either.
func TestKernelStoryP_DeathCancelsScheduledDeferredMissed(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-p", "Death mid-course: scheduled + deferred + missed + batched all cancel forever",
		"A goat dies while its open vaccination doses are spread across every open status the kernel "+
			"recognizes: one still attached to a planned drive batch, one deferred (it was mid-quarantine "+
			"when the goat died), and one already missed (its window had crossed with nobody acting). The "+
			"real SM-3 death handler must cancel all three forever -- not just the plainly-scheduled one "+
			"-- release the batch's reservation count, write a durable outbox event, leave the animal's "+
			"completed history alone, and be a clean no-op on redelivery.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_p", 21, 14, nil)

	const goatDies = "ec000000-0000-4000-8000-000000000001"
	const goatLives = "ec000000-0000-4000-8000-000000000002"
	fx.SeedGoat(GoatSpec{GoatID: goatDies})
	fx.SeedGoat(GoatSpec{GoatID: goatLives})

	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	story.Step("Seed one drive batch and four obligations spanning every open status, plus a bystander",
		"goatDies gets: (1) a scheduled dose attached to a planned drive batch, (2) a deferred dose "+
			"(mid-quarantine), (3) a missed dose (window closed with no action), and (4) a completed "+
			"historical dose. A second goat (goatLives) has its own open dose that must not be touched.")

	// A planned drive batch this goat's dose 1 will be attached to.
	const batchID = "ec000000-0000-4000-8000-0000000000ba"
	fx.exec("planned drive batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, estimated_targets)
		 VALUES ($1, $2, $3, 'park', $4, 'planned', 2)`, batchID, fxTenant, versionID, fxPark)

	// (1) Scheduled, attached to the batch.
	due1 := base.AddDate(0, 0, 28)
	win1 := due1.AddDate(0, 0, 14)
	obl1, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
		DueAt: due1, WindowEnd: &win1, Status: "scheduled",
		IdempotencyKey: "e2e-story-p-scheduled", Sequence: 1,
	})
	if err != nil || !applied {
		t.Fatalf("seed scheduled dose: applied=%v err=%v", applied, err)
	}
	fx.exec("attach dose 1 to the batch", `UPDATE obligation_instances SET batch_id=$3 WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl1, batchID)

	// (2) Deferred (mid-quarantine at time of death).
	due2 := base.AddDate(0, 0, 56)
	win2 := due2.AddDate(0, 0, 14)
	obl2, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
		DueAt: due2, WindowEnd: &win2, Status: "scheduled",
		IdempotencyKey: "e2e-story-p-deferred", Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("seed deferred dose: applied=%v err=%v", applied, err)
	}
	fx.exec("flip dose 2 to deferred", `UPDATE obligation_instances SET status='deferred' WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl2)

	// (3) Missed (window already crossed).
	due3 := base.AddDate(0, 0, -30)
	win3 := due3.AddDate(0, 0, 14)
	obl3, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
		DueAt: due3, WindowEnd: &win3, Status: "scheduled",
		IdempotencyKey: "e2e-story-p-missed", Sequence: 3,
	})
	if err != nil || !applied {
		t.Fatalf("seed missed dose: applied=%v err=%v", applied, err)
	}
	fx.exec("flip dose 3 to missed", `UPDATE obligation_instances SET status='missed' WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl3)

	// (4) Completed history that must survive death.
	due4 := base.AddDate(0, 0, -60)
	win4 := due4.AddDate(0, 0, 14)
	obl4, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
		DueAt: due4, WindowEnd: &win4, Status: "scheduled",
		IdempotencyKey: "e2e-story-p-completed", Sequence: 0,
	})
	if err != nil || !applied {
		t.Fatalf("seed completed dose: applied=%v err=%v", applied, err)
	}
	fx.exec("flip dose 4 to completed", `UPDATE obligation_instances SET status='completed', completed_at=$3 WHERE tenant_id=$1 AND obligation_id=$2`,
		fxTenant, obl4, due4.AddDate(0, 0, 2))

	// Bystander's own open dose.
	byDue := base.AddDate(0, 0, 28)
	byWin := byDue.AddDate(0, 0, 14)
	_, _, err = fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatLives, ScopeType: "park", ScopeID: fxPark,
		DueAt: byDue, WindowEnd: &byWin, Status: "scheduled",
		IdempotencyKey: "e2e-story-p-bystander", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("seed bystander dose: %v", err)
	}

	openBefore := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','deferred','missed')`, fxTenant, goatDies)
	story.Assert("dying goat starts with 3 open doses across scheduled/deferred/missed", openBefore == 3, "open=%d", openBefore)
	targetsBefore := fx.countRows(`SELECT estimated_targets FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID)
	story.Assert("the batch starts with estimated_targets=2", targetsBefore == 2, "estimated_targets=%d", targetsBefore)

	story.Step("Goat dies: fire the real goat.exited handler (SM-3)",
		"Mark the goat dead and dispatch a goat.exited event through the real oblapp.GoatExitedHandler. "+
			"It must cancel all three open doses regardless of which open status they are in.")
	fx.exec("mark goat dead", `UPDATE goats SET lifecycle_status='dead' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatDies)

	exitAt := base.AddDate(0, 0, 61)
	handler := oblapp.NewGoatExitedHandler(fx.Obl)
	err = handler.HandleEvent(fx.Ctx, eventbus.Event{
		ID: "e2e-story-p-exit-1", Type: oblapp.EventGoatExited, TenantID: fxTenant, Key: goatDies, OccurredAt: exitAt,
	})
	story.Assert("goat.exited handler ran without error", err == nil, "err=%v", err)

	story.Step("All three open statuses (scheduled/deferred/missed) are cancelled forever",
		"Not just the batched scheduled dose -- the deferred (mid-quarantine) and missed doses must also "+
			"be cancelled. This is the gap Story I's death coverage did not exercise.")
	openAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status IN ('scheduled','deferred','missed')`, fxTenant, goatDies)
	story.Assert("no open doses of any status remain for the dead goat", openAfter == 0, "open=%d", openAfter)

	cancelled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("all 3 open doses (scheduled+deferred+missed) were cancelled", cancelled == 3, "cancelled=%d", cancelled)

	scheduledStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl1)
	story.Assert("the batched scheduled dose is cancelled", scheduledStatus == "canceled", "status=%q", scheduledStatus)
	deferredStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl2)
	story.Assert("the deferred (mid-quarantine) dose is cancelled", deferredStatus == "canceled", "status=%q", deferredStatus)
	missedStatus := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl3)
	story.Assert("the missed dose is cancelled", missedStatus == "canceled", "status=%q", missedStatus)

	story.Step("Completed history survives; the batch's reservation count is repaired; bystander untouched",
		"Death never rewrites the completed dose. The drive batch that lost obligation 1 has its "+
			"estimated_targets reduced by the cancellation repair. The second goat's own open dose is "+
			"unaffected.")
	stillCompleted := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, obl4)
	story.Assert("the completed historical dose is still completed", stillCompleted == "completed", "status=%q", stillCompleted)

	targetsAfter := fx.countRows(`SELECT estimated_targets FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID)
	story.Assert("the batch's estimated_targets dropped by 1 (repair for the 1 cancelled obligation it held)", targetsAfter == 1, "estimated_targets=%d", targetsAfter)

	bystanderOpen := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatLives)
	story.Assert("the bystander goat's open dose is untouched", bystanderOpen == 1, "open=%d", bystanderOpen)

	story.Step("A durable outbox event was recorded for the cancellation (audit + outbox kernel step)",
		"Each cancelled obligation writes an outbox_messages row (event_type='goat.obligations_canceled') "+
			"in the same transaction as the status change -- the kernel's golden rule step downstream "+
			"consumers (batch repair, future notification adapters) rely on, not just a status flip.")
	outboxCount := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.obligations_canceled' AND aggregate_id IN ($2,$3,$4)`,
		fxTenant, obl1, obl2, obl3)
	story.Assert("an outbox row exists for each of the 3 cancelled obligations", outboxCount == 3, "outbox_rows=%d", outboxCount)

	story.Step("Re-deliver the death event: idempotent no-op",
		"SM-3 runs under at-least-once delivery. Re-dispatching the same goat.exited event must not error "+
			"and must not re-cancel, double-write outbox rows, or corrupt the batch repair counters.")
	err = handler.HandleEvent(fx.Ctx, eventbus.Event{
		ID: "e2e-story-p-exit-1", Type: oblapp.EventGoatExited, TenantID: fxTenant, Key: goatDies, OccurredAt: exitAt,
	})
	story.Assert("re-delivered death event is a clean no-op", err == nil, "err=%v", err)

	cancelledReplay := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("still exactly 3 cancelled doses after replay", cancelledReplay == 3, "cancelled=%d", cancelledReplay)
	targetsReplay := fx.countRows(`SELECT estimated_targets FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2`, fxTenant, batchID)
	story.Assert("the batch's estimated_targets is unchanged by the replay", targetsReplay == 1, "estimated_targets=%d", targetsReplay)
	outboxReplay := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='goat.obligations_canceled' AND aggregate_id IN ($2,$3,$4)`,
		fxTenant, obl1, obl2, obl3)
	story.Assert("no duplicate outbox rows were written by the replay", outboxReplay == 3, "outbox_rows=%d", outboxReplay)
}
