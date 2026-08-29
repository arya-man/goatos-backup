package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestOperatorConfigReplanWatermarkTwoPhaseSucceedsAndNoOpsOnRedelivery asserts the FIXED two-phase
// watermark behavior: a delivered cascade event whose recompute succeeds leaves exactly one watermark
// row in status 'succeeded', and an at-least-once redelivery of the SAME event is an idempotent no-op
// (no second recompute, no duplicate row, status unchanged). The pending->retry half of the contract
// (a failed recompute leaves the watermark retriable) is proven by
// TestOperatorConfigReplanRetrysPendingWatermark.
func TestOperatorConfigReplanWatermarkTwoPhaseSucceedsAndNoOpsOnRedelivery(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Forward-only recompute: a planned date already in the past is never revisited, so a
	// fixed calendar date stops proving anything once the clock passes it.
	plannedDate := time.Now().UTC().AddDate(0, 0, 10).Truncate(24 * time.Hour).Add(12 * time.Hour)
	park, _, batch := seedOperatorConfigReplanFixture(t, ctx, pool, "c1", 3, plannedDate)
	repo := NewRepository(pool, 5*time.Second)

	bus := eventbus.NewInProcessBus()
	oblapp.NewOperatorConfigReplanHandler(repo).Register(bus)

	eventID := "vaccination.operator-assignment-config.capacity:test-c1:999"
	payload, _ := json.Marshal(oblapp.OperatorConfigChangePayload{ParkID: park})
	event := eventbus.Event{
		ID:         eventID,
		Type:       oblapp.EventVaccinationCapacityChanged,
		TenantID:   tenantID,
		Key:        park,
		Payload:    payload,
		OccurredAt: time.Now().In(biztime.DefaultLocation()),
	}

	// First delivery: recompute runs and supersedes the stale planned batch.
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	assertWatermark := func(when string) {
		t.Helper()
		var count int
		var status string
		if err := pool.QueryRow(ctx, `
SELECT count(*), coalesce(max(status), '')
FROM obligation_operator_config_replan_watermarks
WHERE tenant_id=$1::uuid AND event_id=$2`, tenantID, eventID).Scan(&count, &status); err != nil {
			t.Fatalf("query watermark (%s): %v", when, err)
		}
		if count != 1 {
			t.Fatalf("%s: watermark count = %d, want exactly 1", when, count)
		}
		if status != "succeeded" {
			t.Fatalf("%s: watermark status = %q, want succeeded", when, status)
		}
	}
	assertWatermark("after first delivery")

	// The recompute actually happened: the stale planned batch is superseded.
	var batchStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM obligation_batches WHERE tenant_id=$1::uuid AND batch_id=$2::uuid`, tenantID, batch).Scan(&batchStatus); err != nil {
		t.Fatalf("query batch: %v", err)
	}
	if batchStatus != "superseded" {
		t.Fatalf("batch status = %q, want superseded (recompute must have run)", batchStatus)
	}

	// Redelivery of the SAME event is an idempotent no-op: status already 'succeeded'.
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("redelivery publish: %v", err)
	}
	assertWatermark("after idempotent redelivery")
}

// TestOperatorConfigReplanWatermarkStatusColumnExists guards the migration that added the two-phase
// status column (000037): without it the pending->succeeded lifecycle above cannot exist, so a
// dropped/renamed column must fail loudly here.
func TestOperatorConfigReplanWatermarkStatusColumnExists(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	var dataType string
	if err := pool.QueryRow(ctx, `
SELECT data_type FROM information_schema.columns
WHERE table_name='obligation_operator_config_replan_watermarks' AND column_name='status'`).Scan(&dataType); err != nil {
		t.Fatalf("status column must exist on obligation_operator_config_replan_watermarks (migration 000037): %v", err)
	}
	if dataType != "text" {
		t.Fatalf("status column data_type = %q, want text", dataType)
	}
}
