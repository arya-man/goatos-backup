// Command backfill-notification-unread-counters fills the stored unread counter (migration
// 000403) for notification rows written before that migration, then opens the read gate so
// GET /app/notifications stops recomputing the badge from history.
//
// Run once per environment after 000403 is applied. It is idempotent and safe under live
// traffic: every batch re-derives key state from notification_requests under the same per-member
// lock the triggers take, `-batch` keys per short transaction (bounded I/O and lock hold time).
//
// DEFAULTS TO A READ-ONLY RECONCILE: it prints members whose stored counter differs from the
// legacy COUNT and exits 1 if any do. Pass -apply to backfill (and open the gate), then run
// again without -apply to confirm zero mismatches. The reconcile mode doubles as the periodic
// drift check.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	notificationpg "github.com/vgoats/goatos/backend/internal/notificationcentre/adapters/postgres"
)

func main() {
	var (
		databaseURL = flag.String("database-url", os.Getenv("DATABASE_URL"), "target database")
		apply       = flag.Bool("apply", false, "backfill the counter and open the read gate (default: read-only reconcile)")
		batch       = flag.Int("batch", 500, "dedupe keys per transaction")
		timeout     = flag.Duration("timeout", 30*time.Minute, "overall timeout")
	)
	flag.Parse()
	if strings.TrimSpace(*databaseURL) == "" {
		fmt.Fprintln(os.Stderr, "database-url (or DATABASE_URL) is required")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	repo := notificationpg.NewRepository(pool, *timeout)

	if *apply {
		started := time.Now()
		stats, err := repo.BackfillUnreadCounters(ctx, *batch)
		if err != nil {
			fmt.Fprintf(os.Stderr, "backfill: %v (members=%d keys=%d batches=%d)\n", err, stats.Members, stats.Keys, stats.Batches)
			os.Exit(1)
		}
		fmt.Printf("backfill done in %s: members=%d keys=%d batches=%d; read gate open\n",
			time.Since(started).Round(time.Millisecond), stats.Members, stats.Keys, stats.Batches)
		return
	}
	mismatches, err := repo.ReconcileUnreadCounters(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile: %v\n", err)
		os.Exit(1)
	}
	for _, m := range mismatches {
		fmt.Printf("mismatch tenant=%s member=%s stored=%d legacy=%d\n", m.TenantID, m.MemberID, m.Stored, m.Legacy)
	}
	fmt.Printf("reconcile: %d mismatching member(s)\n", len(mismatches))
	if len(mismatches) > 0 {
		os.Exit(1)
	}
}
