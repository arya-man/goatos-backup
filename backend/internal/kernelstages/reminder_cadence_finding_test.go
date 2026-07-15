package kernelstages

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// reminderFireMarkerCount counts the claimed fire markers (vaccination_reminder_cadence_fires) for the
// tenant -- the idempotency slots. A fire is claimed ONLY when it produced >=1 notification.
func reminderFireMarkerCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM vaccination_reminder_cadence_fires WHERE tenant_id = $1::uuid`, rcTenant).Scan(&count); err != nil {
		t.Fatalf("count fire markers: %v", err)
	}
	return count
}

// TestReminderCadenceP1Finding1NoRecipientRetryable is the FINDING 1 GUARD.
//
// A fire whose recipient resolution returns ZERO recipients (no operator/device assigned yet) must NOT
// burn its fire-marker/idempotency slot: it must stay unclaimed so the NEXT cadence run retries it once
// recipients exist -- and then sends EXACTLY ONCE.
//
//	run 1 (no roster)      -> 0 notifications, marker NOT claimed (retryable)
//	assign roster + device
//	run 2 (roster present) -> exactly one notification per recipient, marker now claimed
//	run 3 (same instant)   -> no duplicate
func TestReminderCadenceP1Finding1NoRecipientRetryable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 10 * time.Second}, Logger: logger}

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute) // evening, after all slots, before quiet hours
	dueToday := dayStart.Add(10 * time.Hour)               // due today -> due_today rung fires today

	// Seed the obligation + protocol, but deliberately NO workforce roster (no resolvable recipient).
	seedReminderProtocol(t, ctx, pool)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000001", "rc-f1-obl-today", dueToday)

	stage := NewReminderCadenceStage(deps, rcTenant).withClock(func() time.Time { return evalNow })

	// ---- Run 1: no recipients. The fire is found but produces zero notifications and must NOT claim. --
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("run 1 failed: %v", err)
	}
	if got := reminderNotificationCount(t, ctx, pool); got != 0 {
		t.Fatalf("run 1 produced %d notifications, want 0 (no recipients yet)", got)
	}
	if got := reminderFireMarkerCount(t, ctx, pool); got != 0 {
		t.Fatalf("run 1 CLAIMED %d fire markers — FINDING 1: a zero-recipient fire burned its idempotency slot and is now permanently lost", got)
	}
	t.Logf("FINDING 1: run 1 (no recipients) -> 0 notifications, 0 markers claimed (retryable) — OK")

	// ---- Assign the operational roster (operator + park head + PHC manager, each with a device). ------
	seedReminderRoster(t, ctx, pool)

	// ---- Run 2: recipients now exist. The previously-unclaimed fire retries and sends exactly once. ---
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("run 2 failed: %v", err)
	}
	afterAssign := reminderNotificationCount(t, ctx, pool)
	if afterAssign != 3 { // one due_today fire x 3 recipient devices
		t.Fatalf("run 2 produced %d notifications, want 3 (due_today fire x 3 recipients)", afterAssign)
	}
	if got := reminderFireMarkerCount(t, ctx, pool); got < 1 {
		t.Fatalf("run 2 claimed %d fire markers, want >=1 (the fire is now claimed exactly once)", got)
	}
	dueTodayTokens := reminderRecipientTokensForType(t, ctx, pool, "due_today")
	if len(dueTodayTokens) != 3 {
		t.Fatalf("run 2 due_today recipients = %v, want 3 seeded devices", dueTodayTokens)
	}
	t.Logf("FINDING 1: run 2 (recipients assigned) -> %d notifications, fire claimed exactly once — OK", afterAssign)

	// ---- Run 3: same instant, fire already claimed -> no duplicate. ----------------------------------
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("run 3 failed: %v", err)
	}
	if got := reminderNotificationCount(t, ctx, pool); got != afterAssign {
		t.Fatalf("run 3 DUPLICATED notifications: %d -> %d (idempotency broken)", afterAssign, got)
	}
	t.Logf("FINDING 1 GUARD: PASS — no-recipient fire deferred, retried once recipients assigned, no duplicate")
}

// TestReminderCadenceP1Finding2DrainWithRecipients is the FINDING 2 GUARD (drain half).
//
// A high-volume backlog (>200 obligations) that collapses into multiple park-day drive fires must DRAIN
// FULLY when recipients exist: every ladder slot fires, the stage converges in a handful of iterations
// (never the cap), and a second run does not duplicate.
func TestReminderCadenceP1Finding2DrainWithRecipients(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 15 * time.Second}, Logger: logger}

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute)
	dueToday := dayStart.Add(10 * time.Hour)
	duePlus3 := dayStart.AddDate(0, 0, 3).Add(10 * time.Hour)
	duePlus7 := dayStart.AddDate(0, 0, 7).Add(10 * time.Hour)

	seedReminderRoster(t, ctx, pool)
	seedReminderProtocol(t, ctx, pool)

	// Seed 250 obligations (> the 200-per-tick candidate page) across three due days. They collapse per
	// (park, due-day) into three park-day drive candidates -> three ladder fires (advance_notice,
	// reminder, due_today). Proves high event volume drains fully, not just the first page.
	const totalObligations = 250
	for i := 0; i < totalObligations; i++ {
		goatID := fmt.Sprintf("e1000000-0000-4000-9000-%012d", 1000+i)
		var due time.Time
		switch {
		case i < 240:
			due = dueToday
		case i < 245:
			due = duePlus3
		default:
			due = duePlus7
		}
		seedReminderObligation(t, ctx, pool, goatID, fmt.Sprintf("rc-f2-obl-%d", i), due)
	}

	stage := NewReminderCadenceStage(deps, rcTenant).withClock(func() time.Time { return evalNow })
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("drain run failed: %v", err)
	}

	// Every ladder slot must have fired, addressed to all three seeded recipients.
	for _, slot := range []string{"advance_notice", "reminder", "due_today"} {
		tokens := reminderRecipientTokensForType(t, ctx, pool, slot)
		if len(tokens) != 3 {
			t.Fatalf("FINDING 2: ladder slot %q got %d notifications, want 3 — a fire group STARVED despite >200 events", slot, len(tokens))
		}
	}
	total := reminderNotificationCount(t, ctx, pool)
	if total != 9 { // 3 slots x 3 recipients
		t.Fatalf("FINDING 2: total notifications = %d, want 9 (3 collapsed fire groups x 3 recipients)", total)
	}
	if markers := reminderFireMarkerCount(t, ctx, pool); markers < 3 {
		t.Fatalf("FINDING 2: claimed %d fire markers, want >=3 (one per collapsed fire group)", markers)
	}
	// Drain must converge quickly — never spin the iteration cap.
	if stage.lastRunIterations >= 50 {
		t.Fatalf("FINDING 2: drain spun %d iterations (hit cap) — not converging", stage.lastRunIterations)
	}
	if stage.lastRunIterations > 5 {
		t.Fatalf("FINDING 2: drain took %d iterations for 3 fire groups — expected a couple", stage.lastRunIterations)
	}
	t.Logf("FINDING 2 (drain): %d obligations -> 3 collapsed fires, %d notifications, drained in %d iterations — OK",
		totalObligations, total, stage.lastRunIterations)

	// Second run at the same instant must not duplicate.
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("second drain run failed: %v", err)
	}
	if got := reminderNotificationCount(t, ctx, pool); got != total {
		t.Fatalf("FINDING 2: second run DUPLICATED notifications: %d -> %d", total, got)
	}
	t.Logf("FINDING 2 GUARD (drain): PASS — >200 events drained fully to 9 notifications, no duplicate")
}

// TestReminderCadenceP1Finding2NoRecipientDeferNoSpin is the FINDING 2 GUARD (defer half).
//
// When a page contains ONLY fires with no resolvable recipients (Finding 1 refuses to claim them), the
// drain loop must terminate on NO PROGRESS after exactly one wasted sweep and defer to the next tick --
// it must NOT spin the full iteration cap.
func TestReminderCadenceP1Finding2NoRecipientDeferNoSpin(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	pool := pgtest.StartPostgres(t, ctx)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	deps := Deps{Pool: pool, PgCfg: platformpg.Config{QueryTimeout: 10 * time.Second}, Logger: logger}

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute)
	dueToday := dayStart.Add(10 * time.Hour)

	// Obligation exists (so a fire IS produced) but NO roster (so it can never be claimed right now).
	seedReminderProtocol(t, ctx, pool)
	seedReminderObligation(t, ctx, pool, "e1000000-0000-4000-9000-000000000009", "rc-f2-defer-obl", dueToday)

	stage := NewReminderCadenceStage(deps, rcTenant).withClock(func() time.Time { return evalNow })
	if err := stage.Run(ctx); err != nil {
		t.Fatalf("defer run failed: %v", err)
	}

	if got := reminderNotificationCount(t, ctx, pool); got != 0 {
		t.Fatalf("defer run produced %d notifications, want 0", got)
	}
	if got := reminderFireMarkerCount(t, ctx, pool); got != 0 {
		t.Fatalf("defer run claimed %d markers, want 0 (unclaimable fires must stay retryable)", got)
	}
	if stage.lastRunIterations != 1 {
		t.Fatalf("FINDING 2: no-recipient page ran %d iterations, want exactly 1 (one wasted sweep, not a spin to the cap)", stage.lastRunIterations)
	}
	t.Logf("FINDING 2 GUARD (defer): PASS — unclaimable fire deferred after exactly 1 sweep, no spin")
}
