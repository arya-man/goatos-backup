package main

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestPostMigrationJobsOpenTheNotificationCounterGate: a deploy that applies 000403 over existing
// notification rows must backfill the counter and open the gate without a human step, and a
// second deploy must be a no-op.
func TestPostMigrationJobsOpenTheNotificationCounterGate(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	const tenant = "00000000-0000-4000-8000-00000000d001"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Post migrate', 'active')`, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context)
SELECT $1::uuid, 'pm', 'tenant', 'reminder', 'push_fcm', 'T', '', 'sent', 'pm:'||g, 'fp',
  jsonb_build_object('member_id', 'member-'||(g % 3), 'event_key', 'k'||(g % 7))
FROM generate_series(1, 60) g`, tenant); err != nil {
		t.Fatal(err)
	}
	// As if these rows predated 000403.
	if _, err := pool.Exec(ctx, `
DELETE FROM notification_member_unread_keys;
DELETE FROM notification_member_unread_counts;
UPDATE notification_unread_counter_state SET backfill_complete_at = NULL`); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	runPostMigrationJobs(ctx, pool, log)
	var open bool
	var total int
	if err := pool.QueryRow(ctx, `SELECT backfill_complete_at IS NOT NULL, (SELECT COALESCE(sum(unread_count),0) FROM notification_member_unread_counts) FROM notification_unread_counter_state`).Scan(&open, &total); err != nil {
		t.Fatal(err)
	}
	// 3 members x 7 keys, but only keys that occur for each member: g%3 and g%7 over 1..60 cover all 21 pairs.
	if !open || total != 21 {
		t.Fatalf("after post-migrate: gate open=%v total unread=%d, want true/21", open, total)
	}
	runPostMigrationJobs(ctx, pool, log) // second deploy: gate already open, no-op
}
