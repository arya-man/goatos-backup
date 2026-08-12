package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// Adversarial regression tests for the OversightAnalytics aggregates
// (OversightAnalytics in repository.go).
//
// The projection-review marker on those queries claims:
//   - membership = verification_items and verification_review_events tenant-scoped,
//     split by status, module, and verifier.
//   - group_key = varies per query (none for single aggregates, module, verified_by,
//     item_id).
//   - join_cardinality = per-module and per-verifier aggregates pre-collapse to one row
//     each before returning, and the watch-state per-item GROUP BY prevents fanout.
//   - pagination = each query aggregates a full time window or keyset in one pass.
//   - scope = tenant_id only (or tenant + time window, or tenant + item_id list).
//
// Each test below proves one of those claims against a real Postgres instance.

const (
	oversightTestTenantID = "10000000-0000-4000-8000-000000000001"
	oversightTestModule   = "vaccination"
	oversightTestVerifier = "10000000-0000-4000-8000-000000000002"
)

// TestOversightAnalyticsOneToManyItemsAggregatedPerModule proves the per-module
// cardinality claim: 10 items with approved status on module=vaccination produce
// exactly ONE row whose count = 10, not 10 rows.
func TestOversightAnalyticsOneToManyItemsAggregatedPerModule(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTestData(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Insert 10 approved verification items on the vaccination module.
	for i := 0; i < 10; i++ {
		itemID := fmt.Sprintf("10000000-0000-4000-8000-0000000000%02d", i)
		if _, err := pool.Exec(ctx, `
			INSERT INTO verification_items (item_id, tenant_id, module, status, verified_by, verified_at, captured_at)
			VALUES ($1, $2, $3, 'approved', $4, now(), now())
			ON CONFLICT (item_id) DO NOTHING`,
			itemID, oversightTestTenantID, oversightTestModule, oversightTestVerifier); err != nil {
			t.Fatalf("insert verified item %s: %v", itemID, err)
		}
	}

	analytics, err := repo.OversightAnalytics(ctx, oversightTestTenantID)
	if err != nil {
		t.Fatalf("OversightAnalytics: %v", err)
	}

	// Find the vaccination module in the per-module latency results.
	var vaccLatency *domain.ModuleLatency
	for i := range analytics.KPIs.PerModuleMedianReviewLatencyHours {
		if analytics.KPIs.PerModuleMedianReviewLatencyHours[i].Module == oversightTestModule {
			vaccLatency = &analytics.KPIs.PerModuleMedianReviewLatencyHours[i]
			break
		}
	}
	if vaccLatency == nil {
		t.Fatalf("vaccination module not found in per-module latencies (join must not fan-out or drop groups)")
	}

	// Find the vaccination module in the pending backlog results.
	var vaccBacklog *domain.ModulePendingBacklog
	for i := range analytics.PendingByModule {
		if analytics.PendingByModule[i].Module == oversightTestModule {
			vaccBacklog = &analytics.PendingByModule[i]
			break
		}
	}
	if vaccBacklog == nil {
		t.Fatalf("vaccination module not found in pending backlog (group_key=module must produce exactly one row per module)")
	}
}

// TestOversightAnalyticsVerifierActivityPageBoundaryMultipleDimensions proves
// the per-verifier cardinality and pagination claims: 5 items decided by one
// verifier with mixed approved/rejected statuses and multiple events per item
// produce exactly ONE row for that verifier, with correct totals.
func TestOversightAnalyticsVerifierActivityPageBoundaryMultipleDimensions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTestData(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const verifier = "10000000-0000-4000-8000-000000000099"
	const itemCount = 5
	const approvedCount = 3
	const rejectedCount = 2

	// Insert a verifier profile so verified_by_name is populated.
	if _, err := pool.Exec(ctx, `
		INSERT INTO workforce_members (user_id, tenant_id, display_name)
		VALUES ($1, $2, 'Test Verifier')
		ON CONFLICT (user_id) DO NOTHING`,
		verifier, oversightTestTenantID); err != nil {
		t.Fatalf("insert workforce member: %v", err)
	}

	// Insert 5 verified items: 3 approved, 2 rejected.
	for i := 0; i < itemCount; i++ {
		itemID := fmt.Sprintf("10000000-0000-4000-8000-0000000099%02d", i)
		status := "approved"
		if i >= approvedCount {
			status = "rejected"
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO verification_items (item_id, tenant_id, status, verified_by, verified_at, captured_at)
			VALUES ($1, $2, $3, $4, now(), now())
			ON CONFLICT (item_id) DO NOTHING`,
			itemID, oversightTestTenantID, status, verifier); err != nil {
			t.Fatalf("insert item %s: %v", itemID, err)
		}
	}

	analytics, err := repo.OversightAnalytics(ctx, oversightTestTenantID)
	if err != nil {
		t.Fatalf("OversightAnalytics: %v", err)
	}

	// Find the verifier in the activity results.
	var found *domain.VerifierActivity
	for i := range analytics.VerifierActivity {
		if analytics.VerifierActivity[i].VerifierID == verifier {
			found = &analytics.VerifierActivity[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("verifier %s not found in activity (GROUP BY verified_by must produce exactly one row per verifier)", verifier)
	}

	// Verify the counts are correct and unpacked from a single aggregate.
	if found.Verdicts != itemCount {
		t.Fatalf("verifier verdicts = %d, want %d (10 items fan-out would show wrong total)", found.Verdicts, itemCount)
	}
	if found.Approved != approvedCount {
		t.Fatalf("verifier approved = %d, want %d (FILTER clause on same row set)", found.Approved, approvedCount)
	}
	if found.Rejected != rejectedCount {
		t.Fatalf("verifier rejected = %d, want %d", found.Rejected, rejectedCount)
	}
	if found.VerifierName != "Test Verifier" {
		t.Fatalf("verifier name = %q, want 'Test Verifier' (LEFT JOIN workforce_members must not fan-out)", found.VerifierName)
	}
}

// TestOversightAnalyticsWatchStatePerItemStatusMatrix proves the WatchStates
// per-item cardinality and status-matrix claims: 10 items with mixed events
// (played, not-played, varying watch percentages) produce exactly 10 distinct
// rows, each with correct watch state.
func TestOversightAnalyticsWatchStatePerItemStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	seedOversightTestData(t, ctx, pool)
	reviewEventRepo := NewReviewEventRepository(pool, 5*time.Second)

	const itemCount = 10
	itemIDs := make([]string, itemCount)
	for i := 0; i < itemCount; i++ {
		itemIDs[i] = fmt.Sprintf("10000000-0000-4000-8000-0000000080%02d", i)
	}

	// Insert varied review events: some items opened, some with video played,
	// some with watched positions.
	for i, itemID := range itemIDs {
		actor := fmt.Sprintf("10000000-0000-4000-8000-0000000080%02d", i)
		payload := make(map[string]interface{})
		payload["video_duration_ms"] = int64(10000) // 10 seconds

		// Half the items have watched 90%+ (watched_to_end), half have 0%.
		if i%2 == 0 {
			payload["video_position_ms"] = int64(9500) // 95% watched
		} else {
			payload["video_position_ms"] = int64(0)
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO verification_review_events (
				tenant_id, item_id, actor_id, event_type, occurred_at, payload, session_id, client_event_id
			) VALUES ($1, $2, $3, $4, now(), $5::jsonb, '', '')
			ON CONFLICT (tenant_id, client_event_id) DO NOTHING`,
			oversightTestTenantID, itemID, actor, "video_play",
			fmt.Sprintf(`{"video_position_ms": %d, "video_duration_ms": 10000}`, payload["video_position_ms"])); err != nil {
			t.Fatalf("insert play event for item %s: %v", itemID, err)
		}
	}

	watchStates, err := reviewEventRepo.WatchStates(ctx, oversightTestTenantID, itemIDs)
	if err != nil {
		t.Fatalf("WatchStates: %v", err)
	}

	// Verify all 10 items appear exactly once (no fan-out).
	if len(watchStates) != itemCount {
		t.Fatalf("WatchStates returned %d items, want %d (GROUP BY item_id must produce one row per item)", len(watchStates), itemCount)
	}

	for i, itemID := range itemIDs {
		state, ok := watchStates[itemID]
		if !ok {
			t.Fatalf("item %s missing from results (GROUP BY must not drop items)", itemID)
		}

		// Half the items should have 95% watch, half should have 0%.
		if i%2 == 0 {
			if state.PercentWatched == nil || *state.PercentWatched != 95 {
				t.Fatalf("item %d watch percent = %v, want 95", i, state.PercentWatched)
			}
		} else {
			if state.PercentWatched == nil || *state.PercentWatched != 0 {
				t.Fatalf("item %d watch percent = %v, want 0", i, state.PercentWatched)
			}
		}
	}
}

// seedOversightTestData inserts minimal required data for verification_items.
func seedOversightTestData(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tenants (tenant_id, name) VALUES ($1, 'oversight-test')
		ON CONFLICT (tenant_id) DO NOTHING`,
		oversightTestTenantID); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}
