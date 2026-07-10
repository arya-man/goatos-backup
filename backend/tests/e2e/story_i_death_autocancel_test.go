package e2e

import (
	"fmt"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// TestKernelStoryI_DeathAutoCancel drives the real SM-3 death/exit cancellation path: a goat with
// several open (future/scheduled) vaccination obligations dies mid-course. The real
// oblapp.GoatExitedHandler (the goat.exited event consumer) must cancel every open obligation so no
// due work remains for a dead animal -- and completed history stays untouched.
//
// This uses the genuine event handler + CancelOpenForGoatAt repository method the production
// goat.exited consumer runs, not a raw UPDATE, so the story exercises the actual death-cancel kernel
// transition (idempotent under at-least-once delivery).
func TestKernelStoryI_DeathAutoCancel(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-i", "Death mid-course: future doses auto-cancelled",
		"A goat is partway through its vaccination course with several scheduled future doses and one "+
			"already-completed dose. The goat dies. The kernel's death/exit handler must cancel every open "+
			"dose so a dead animal shows no outstanding due work, while the completed dose remains as durable "+
			"history. No cancellation touches other goats.")
	defer story.Finish()

	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_i", 21, 14, nil)

	const goatDies = "e9000000-0000-4000-8000-000000000001"
	const goatLives = "e9000000-0000-4000-8000-000000000002"
	fx.SeedGoat(GoatSpec{GoatID: goatDies})
	fx.SeedGoat(GoatSpec{GoatID: goatLives})

	story.Step("Seed a goat mid-course with 3 open doses + 1 completed, plus a bystander goat",
		"goatDies has three scheduled future doses and one completed (historical) dose. A second goat "+
			"(goatLives) has its own open dose that must NOT be affected by the death.")

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// Three open (scheduled) future doses for the dying goat.
	for i := 1; i <= 3; i++ {
		due := base.AddDate(0, 0, 28*i)
		win := due.AddDate(0, 0, 14)
		_, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
			TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
			DueAt: due, WindowEnd: &win, Status: "scheduled",
			IdempotencyKey: fmt.Sprintf("e2e-story-i-open-%d", i), Sequence: int32(i),
		})
		if err != nil || !applied {
			t.Fatalf("seed open dose %d: applied=%v err=%v", i, applied, err)
		}
	}
	// One already-completed dose (history that must survive the death).
	completedDue := base.AddDate(0, 0, -28)
	completedWin := completedDue.AddDate(0, 0, 14)
	completedObl, applied, err := fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatDies, ScopeType: "park", ScopeID: fxPark,
		DueAt: completedDue, WindowEnd: &completedWin, Status: "scheduled",
		IdempotencyKey: "e2e-story-i-completed", Sequence: 0,
	})
	if err != nil || !applied {
		t.Fatalf("seed completed dose: applied=%v err=%v", applied, err)
	}
	fx.exec("mark historical dose completed",
		`UPDATE obligation_instances SET status='completed', completed_at=$3 WHERE tenant_id=$1 AND obligation_id=$2`,
		fxTenant, completedObl, completedDue.AddDate(0, 0, 2))

	// Bystander goat's open dose.
	byDue := base.AddDate(0, 0, 28)
	byWin := byDue.AddDate(0, 0, 14)
	_, _, err = fx.Obl.InsertObligation(fx.Ctx, obldomain.NewObligation{
		TenantID: fxTenant, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: goatLives, ScopeType: "park", ScopeID: fxPark,
		DueAt: byDue, WindowEnd: &byWin, Status: "scheduled",
		IdempotencyKey: "e2e-story-i-bystander", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("seed bystander dose: %v", err)
	}

	openBefore := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatDies)
	story.Assert("dying goat starts with 3 open scheduled doses", openBefore == 3, "open=%d", openBefore)

	story.Step("Goat dies: fire the real goat.exited handler (SM-3)",
		"Mark the goat dead and dispatch a goat.exited event through the real oblapp.GoatExitedHandler "+
			"-- the same consumer production runs. It must cancel every open dose for that goat.")
	fx.exec("mark goat dead", `UPDATE goats SET lifecycle_status='dead' WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, goatDies)

	exitAt := base.AddDate(0, 0, 10)
	handler := oblapp.NewGoatExitedHandler(fx.Obl)
	err = handler.HandleEvent(fx.Ctx, eventbus.Event{
		ID: "e2e-story-i-exit-1", Type: oblapp.EventGoatExited, TenantID: fxTenant, Key: goatDies, OccurredAt: exitAt,
	})
	story.Assert("goat.exited handler ran without error", err == nil, "err=%v", err)

	openAfter := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatDies)
	story.Assert("no open doses remain for the dead goat", openAfter == 0, "open=%d", openAfter)

	cancelled := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("all 3 open doses were cancelled", cancelled == 3, "cancelled=%d", cancelled)

	story.Step("Completed history survives; bystander untouched",
		"The already-completed dose stays completed (death does not rewrite history), and the second "+
			"goat's open dose is unaffected.")
	stillCompleted := fx.scanText(`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, fxTenant, completedObl)
	story.Assert("the completed dose is still completed", stillCompleted == "completed", "status=%q", stillCompleted)

	bystanderOpen := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='scheduled'`, fxTenant, goatLives)
	story.Assert("the bystander goat's open dose is untouched", bystanderOpen == 1, "open=%d", bystanderOpen)

	story.Step("Re-deliver the death event: idempotent no-op",
		"SM-3 runs under at-least-once delivery. Re-dispatching the same goat.exited event must not "+
			"error and must not re-cancel or corrupt anything.")
	err = handler.HandleEvent(fx.Ctx, eventbus.Event{
		ID: "e2e-story-i-exit-1", Type: oblapp.EventGoatExited, TenantID: fxTenant, Key: goatDies, OccurredAt: exitAt,
	})
	story.Assert("re-delivered death event is a clean no-op", err == nil, "err=%v", err)
	cancelledReplay := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND target_id=$2 AND status='canceled'`, fxTenant, goatDies)
	story.Assert("still exactly 3 cancelled doses after replay", cancelledReplay == 3, "cancelled=%d", cancelledReplay)
}
