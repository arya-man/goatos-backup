package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	notificationpg "github.com/vgoats/goatos/backend/internal/notificationcentre/adapters/postgres"
)

// notificationCounterBackfillBatch bounds each backfill transaction (keys per tx), which bounds
// its I/O and how long it holds a member's counter-row lock.
const notificationCounterBackfillBatch = 500

// runPostMigrationJobs runs idempotent data jobs that must follow a schema migration, so a deploy
// never leaves them to a human. Failures are logged, not fatal: every job here leaves the system
// on a correct (if slower) path until it completes on the next deploy.
//
// Notification unread counter (000403): while its gate is closed the API still recomputes the
// badge from history. The backfill re-derives every key from the rows under the triggers'
// per-member lock, 500 keys per short transaction, so it is safe under live traffic and a rerun
// is a no-op; it opens the gate when done. The reconcile that follows compares each member's
// stored number with the history COUNT inside ONE snapshot, so in-flight writes cannot produce a
// false mismatch; a real mismatch is logged at error level (the reader keeps working, the number
// is off until repaired by re-running cmd/backfill-notification-unread-counters -apply).
func runPostMigrationJobs(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) {
	repo := notificationpg.NewRepository(pool, 2*time.Minute)
	open, err := repo.CounterGateOpen(ctx)
	if err != nil {
		log.Warn("post_migrate_notification_counter_gate_unreadable", slog.String("error", err.Error()))
		return
	}
	if open {
		return
	}
	started := time.Now()
	stats, err := repo.BackfillUnreadCounters(ctx, notificationCounterBackfillBatch)
	if err != nil {
		log.Warn("post_migrate_notification_counter_backfill_failed", slog.String("error", err.Error()),
			slog.Int("members", stats.Members), slog.Int("keys", stats.Keys))
		return
	}
	log.Info("post_migrate_notification_counter_backfilled", slog.Int("members", stats.Members),
		slog.Int("keys", stats.Keys), slog.Int("batches", stats.Batches), slog.Duration("elapsed", time.Since(started)))
	mismatches, err := repo.ReconcileUnreadCounters(ctx)
	if err != nil {
		log.Warn("post_migrate_notification_counter_reconcile_failed", slog.String("error", err.Error()))
		return
	}
	for _, m := range mismatches {
		log.Error("post_migrate_notification_counter_mismatch", slog.String("tenant_id", m.TenantID),
			slog.String("member_id", m.MemberID), slog.Int("stored", m.Stored), slog.Int("legacy", m.Legacy))
	}
}
