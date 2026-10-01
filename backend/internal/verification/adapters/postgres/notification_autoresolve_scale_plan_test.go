package postgres

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestResolveDecisionNotificationsUsesTheEventIndexAtScale is the plan proof for the statement every
// approve / reject / closeout / withdraw runs last, inside its own transaction.
//
// It shipped (000403, 2026-09-24) as one UPDATE whose WHERE said
// "calendar_event_id IN (SELECT ... FROM scope) OR ($3 <> ” AND ...)". The planner cannot probe an
// index for that: it turned the IN into a hashed SubPlan filter and SEQ-SCANNED notification_requests
// on every verdict. On stg (246k rows, 709 MB) that one statement took ~400 ms, and the verdict
// endpoint went from p50 0.10 s to 0.49 s (p99 up to 9 s) the day it deployed.
//
// ~300k notices for other items are bulk-loaded beside the decided item's few, VACUUM ANALYZEd, and
// the exact production statement is EXPLAIN (ANALYZE)'d in a rolled-back transaction under BOTH plan
// modes the API pool can land on (pgx prepares, so after five runs Postgres may cache a generic
// plan). Neither may Seq Scan notification_requests, and it must still resolve the decided item's
// notices -- a plan that touches nothing proves nothing.
func TestResolveDecisionNotificationsUsesTheEventIndexAtScale(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	const verifier = "11111111-1111-4111-8111-111111111111"

	item := createWeighingItem(t, ctx, repo, tenantID, "33333333-3333-4333-8333-333333333391", "scale-plan", nil)
	seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, verifier, "verification.item.pending:"+item.ItemID, "sent")
	seedPendingNotice(t, ctx, pool, tenantID, item.ItemID, verifier, "verification.item.pending:"+item.ItemID, "queued")
	// Decide the item first so its notices are resolvable, without running the resolve itself.
	if _, err := pool.Exec(ctx, `UPDATE verification_items SET status = 'approved', verified_at = now()
WHERE tenant_id = $1::uuid AND item_id = $2::uuid`, tenantID, item.ItemID); err != nil {
		t.Fatalf("decide item: %v", err)
	}

	// The haystack: notices for 100k other items across 40 members, most unread, as on stg.
	if _, err := pool.Exec(ctx, `
INSERT INTO notification_requests (tenant_id, calendar_event_id, target_type, target_id, notification_type, channel,
  title, body, status, idempotency_key, request_fingerprint, context)
SELECT $1::uuid, 'verification:' || it, 'verification_item', it, 'verification_pending', 'push_fcm',
  'Video to verify', '', CASE WHEN g % 5 = 0 THEN 'read' ELSE 'sent' END, 'scale-plan:' || g, 'fp',
  jsonb_build_object('member_id', md5('member|' || (g % 40)::text)::uuid::text,
                     'event_key', 'verification.item.pending:' || it::text, 'item_id', it::text,
                     'message_key', 'weighing.proof.pending.verifier')
FROM generate_series(1, 300000) AS g,
     LATERAL (SELECT md5('item|' || (g / 3)::text)::uuid AS it) x`, tenantID); err != nil {
		t.Fatalf("seed haystack: %v", err)
	}
	if _, err := pool.Exec(ctx, `VACUUM (ANALYZE) notification_requests, verification_items`); err != nil {
		t.Fatalf("vacuum analyze: %v", err)
	}

	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		plan, ms, resolved := explainResolveDecision(t, ctx, tenantID, item.ItemID, mode, pool)
		var walk func(n resolvePlanNode)
		walk = func(n resolvePlanNode) {
			if n.RelationName == "notification_requests" && strings.Contains(n.NodeType, "Seq Scan") {
				t.Errorf("%s: Seq Scan on notification_requests at ~300k rows", mode)
			}
			for _, c := range n.Plans {
				walk(c)
			}
		}
		walk(plan)
		if resolved != 2 {
			t.Errorf("%s: resolved %d notices, want the decided item's 2", mode, resolved)
		}
		if ms > 50 {
			t.Errorf("%s: execution %.1fms over the 50ms budget for one decision", mode, ms)
		}
		t.Logf("%s: %.1fms", mode, ms)
	}
}

type resolvePlanNode struct {
	NodeType     string            `json:"Node Type"`
	RelationName string            `json:"Relation Name"`
	ActualRows   float64           `json:"Actual Rows"`
	ActualLoops  float64           `json:"Actual Loops"`
	Plans        []resolvePlanNode `json:"Plans"`
}

// explainResolveDecision runs the production statement under EXPLAIN ANALYZE in a transaction it
// rolls back, so each plan mode resolves the same unread notices. jit=off as the API pools run.
func explainResolveDecision(t *testing.T, ctx context.Context, tenantID, itemID, mode string, pool *pgxpool.Pool) (resolvePlanNode, float64, int) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		t.Fatalf("jit off: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL plan_cache_mode = "+mode); err != nil {
		t.Fatalf("plan mode: %v", err)
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+sqlResolveDecisionNotifications,
		tenantID, []string{itemID}, "").Scan(&raw); err != nil {
		t.Fatalf("explain: %v", err)
	}
	var out []struct {
		Plan          resolvePlanNode `json:"Plan"`
		ExecutionTime float64         `json:"Execution Time"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out) == 0 {
		t.Fatalf("decode explain: %v", err)
	}
	// Read back, in the same transaction, what the statement actually resolved: the decided item's
	// notices and nothing of the haystack.
	var resolved int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM notification_requests
WHERE tenant_id = $1::uuid AND read_at IS NOT NULL AND status IN ('read', 'suppressed')
  AND updated_at = now()`, tenantID).Scan(&resolved); err != nil {
		t.Fatalf("count resolved: %v", err)
	}
	return out[0].Plan, out[0].ExecutionTime, resolved
}
