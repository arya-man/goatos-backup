package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Adversarial regression tests for the oversight-analytics aggregates
// (Repository.OversightAnalytics and ReviewEventRepository.WatchStates).
//
// Each test attacks ONE claim in those queries' projection-review markers, and each is written so
// that removing the guarantee it names makes it fail:
//
//   - OneToMany    -- a verifier who owns a RETIRED workforce seat alongside the active one must
//     not have every verdict row counted twice by the name decoration.
//     workforce_members is unique on (tenant_id, user_id) only WHERE status='active'
//     (workforce_members_active_user_unique_idx), so the second seat is legal data.
//   - PageBoundary -- the watch aggregate must answer for the item_ids it was ASKED about and for
//     no others, so one page of the queue cannot borrow another page's telemetry.
//   - StatusMatrix -- every verification_items status must land in exactly one bucket, and a
//     withdrawn row must inflate neither the waiting count nor the reject rate.
const (
	oversightTestTenantID = "10000000-0000-4000-8000-000000000001"
	oversightTestVerifier = "10000000-0000-4000-8000-000000000002"
)

func TestOversightAnalyticsOneToManyRetiredSeatDoesNotDoubleCountVerdicts(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTenant(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	seedWorkforceSeat(t, ctx, pool, "20000000-0000-4000-8000-000000000001", "VERIF-1", "Jyothi", "active")
	seedWorkforceSeat(t, ctx, pool, "20000000-0000-4000-8000-000000000002", "VERIF-1-OLD", "Jyothi (retired seat)", "inactive")

	const approved, rejected = 3, 2
	for i := 0; i < approved+rejected; i++ {
		status := "approved"
		if i >= approved {
			status = "rejected"
		}
		seedDecidedItem(t, ctx, pool, fmt.Sprintf("30000000-0000-4000-8000-0000000000%02d", i), "vaccination", status)
	}

	analytics, err := repo.OversightAnalytics(ctx, oversightTestTenantID)
	if err != nil {
		t.Fatalf("OversightAnalytics: %v", err)
	}

	rows := 0
	for _, activity := range analytics.VerifierActivity {
		if activity.VerifierID != oversightTestVerifier {
			continue
		}
		rows++
		if activity.Verdicts != approved+rejected {
			t.Fatalf("verdicts = %d, want %d -- the retired seat fanned the verdict rows out", activity.Verdicts, approved+rejected)
		}
		if activity.Approved != approved {
			t.Fatalf("approved = %d, want %d", activity.Approved, approved)
		}
		if activity.Rejected != rejected {
			t.Fatalf("rejected = %d, want %d", activity.Rejected, rejected)
		}
		if activity.VerifierName != "Jyothi" {
			t.Fatalf("verifier name = %q, want the ACTIVE seat's name %q", activity.VerifierName, "Jyothi")
		}
	}
	if rows != 1 {
		t.Fatalf("verifier appeared in %d activity rows, want exactly 1 (GROUP BY verified_by)", rows)
	}
}

func TestOversightAnalyticsWatchStatesPageBoundaryExcludesOtherPages(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTenant(t, ctx, pool)
	repo := NewReviewEventRepository(pool, 10*time.Second)

	requested := []string{
		"40000000-0000-4000-8000-000000000001",
		"40000000-0000-4000-8000-000000000002",
	}
	// offPage belongs to a DIFFERENT page of the same queue and carries fully-watched telemetry.
	// If the aggregate is not bounded to the requested ids, it leaks in here.
	const offPage = "40000000-0000-4000-8000-000000000099"

	for _, itemID := range append(append([]string{}, requested...), offPage) {
		seedPendingItem(t, ctx, pool, itemID, "weighing")
	}
	seedWatchEvents(t, ctx, pool, requested[0], 10_000, 10_000)
	seedWatchEvents(t, ctx, pool, requested[1], 0, 10_000)
	seedWatchEvents(t, ctx, pool, offPage, 10_000, 10_000)

	states, err := repo.WatchStates(ctx, oversightTestTenantID, requested)
	if err != nil {
		t.Fatalf("WatchStates: %v", err)
	}

	if _, leaked := states[offPage]; leaked {
		t.Fatalf("item %s was not requested but appeared in the result -- the aggregate is not bounded to the page's item_ids", offPage)
	}
	if len(states) != len(requested) {
		t.Fatalf("WatchStates returned %d rows for %d requested ids (one row per item, no fan-out)", len(states), len(requested))
	}
	for _, itemID := range requested {
		if _, ok := states[itemID]; !ok {
			t.Fatalf("requested item %s is missing from the result", itemID)
		}
	}
	if got := states[requested[0]].PercentWatched; got == nil || *got != 100 {
		t.Fatalf("fully-watched item percent = %v, want 100", got)
	}
	if got := states[requested[1]].PercentWatched; got == nil || *got != 0 {
		t.Fatalf("opened-but-unplayed item percent = %v, want 0", got)
	}
}

func TestOversightAnalyticsStatusMatrixWithdrawnInflatesNothing(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTenant(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	// One row in every status the table allows. A withdrawn item is not work anyone still owes: it
	// must not count as waiting, and it must not sit in the reject-rate denominator.
	seedPendingItem(t, ctx, pool, "50000000-0000-4000-8000-000000000001", "feed")
	seedPendingItem(t, ctx, pool, "50000000-0000-4000-8000-000000000002", "feed")
	seedDecidedItem(t, ctx, pool, "50000000-0000-4000-8000-000000000003", "feed", "approved")
	seedDecidedItem(t, ctx, pool, "50000000-0000-4000-8000-000000000004", "feed", "rejected")
	seedItem(t, ctx, pool, "50000000-0000-4000-8000-000000000005", "feed", "withdrawn", false)

	analytics, err := repo.OversightAnalytics(ctx, oversightTestTenantID)
	if err != nil {
		t.Fatalf("OversightAnalytics: %v", err)
	}

	if analytics.KPIs.VideosWaiting != 2 {
		t.Fatalf("videos waiting = %d, want 2 (only pending rows; withdrawn is not waiting work)", analytics.KPIs.VideosWaiting)
	}
	backlog := 0
	for _, module := range analytics.PendingByModule {
		if module.Module == "feed" {
			backlog = module.Count
		}
	}
	if backlog != 2 {
		t.Fatalf("feed pending backlog = %d, want 2 -- the per-module bucket disagrees with the headline waiting count", backlog)
	}
	// 1 rejected of 2 decided rows. A withdrawn row in the denominator would read 1/3 instead.
	rejectRate := analytics.KPIs.RejectRateLast30d
	if rejectRate == nil {
		t.Fatalf("reject rate is nil while 2 decided rows exist")
	}
	if *rejectRate < 0.49 || *rejectRate > 0.51 {
		t.Fatalf("reject rate = %v, want ~0.5 (rejected / decided, withdrawn excluded)", *rejectRate)
	}
	if analytics.KPIs.OldestPendingAgeHours == nil {
		t.Fatalf("oldest pending age is nil while 2 items are pending")
	}
}

func seedOversightTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'oversight-test', 'active')
		ON CONFLICT (tenant_id) DO NOTHING`, oversightTestTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}

func seedWorkforceSeat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, memberID, code, name, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6)
		ON CONFLICT (workforce_member_id) DO NOTHING`,
		memberID, oversightTestTenantID, oversightTestVerifier, code, name, status); err != nil {
		t.Fatalf("seed workforce seat %s: %v", memberID, err)
	}
}

// seedItem writes one verification_items row, filling every NOT NULL column the table declares.
func seedItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, module, status string, decided bool) {
	t.Helper()
	var verifier, verifiedAt, reason any
	if decided {
		verifier = oversightTestVerifier
		verifiedAt = time.Now().UTC()
		if status == "rejected" {
			reason = "seeded rejection reason"
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO verification_items (
			item_id, tenant_id, vertical, module, category, source_module,
			source_ref_type, source_ref_id, status, verdict_reason, verified_by, verified_at,
			captured_at, idempotency_key)
		VALUES ($1::uuid, $2::uuid, 'preventive_care', $3, $3 || '_proof', $3,
			'sop_submission', gen_random_uuid(), $4, $5, $6::uuid, $7::timestamptz,
			now() - interval '2 hours', 'oversight-test:' || $1)
		ON CONFLICT (item_id) DO NOTHING`,
		itemID, oversightTestTenantID, module, status, reason, verifier, verifiedAt); err != nil {
		t.Fatalf("seed %s item %s: %v", status, itemID, err)
	}
}

func seedPendingItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, module string) {
	t.Helper()
	seedItem(t, ctx, pool, itemID, module, "pending", false)
}

func seedDecidedItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID, module, status string) {
	t.Helper()
	seedItem(t, ctx, pool, itemID, module, status, true)
}

// seedWatchEvents writes the open + play pair a real review session produces, so the aggregate sees
// the same event shape the phone and the drawer emit.
func seedWatchEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, itemID string, positionMS, durationMS int64) {
	t.Helper()
	events := []struct {
		eventType string
		payload   string
	}{
		{"item_opened", `{}`},
		{"video_play", fmt.Sprintf(`{"video_position_ms": %d, "video_duration_ms": %d}`, positionMS, durationMS)},
	}
	for _, event := range events {
		if _, err := pool.Exec(ctx, `
			INSERT INTO verification_review_events (
				tenant_id, item_id, actor_id, event_type, occurred_at, payload, session_id, client_event_id)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, now(), $5::jsonb, 'oversight-test-session', gen_random_uuid())`,
			oversightTestTenantID, itemID, oversightTestVerifier, event.eventType, event.payload); err != nil {
			t.Fatalf("seed %s event for item %s: %v", event.eventType, itemID, err)
		}
	}
}
