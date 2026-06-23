package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	feeddomain "github.com/vgoats/goatos/backend/internal/feed/domain"
	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	feedTenant = "00000000-0000-4000-8000-000000000001"
	feedShed   = "55000000-0000-4000-8000-0000000000f1" // a shed location seeded by the test
)

// TestFeedDirectionCompletionFlow drives the feed SM-5 verify+complete path: accept completes a shed
// direction's obligation; reject leaves it open; double-submit is a no-op.
func TestFeedDirectionCompletionFlow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	if _, err := pool.Exec(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
		 VALUES ($1, $2, 'shed', 'SHED-F1', 'Feed test shed', 'active')`, feedShed, feedTenant); err != nil {
		t.Fatalf("seed shed: %v", err)
	}

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	feed := NewRepository(pool, 5*time.Second)

	// Feed protocol (category='feed') for the obligation FKs.
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: feedTenant, Code: "feed.direction.demo", Name: "Feed", Category: "feed_direction", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: feedTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: feedTenant, ProtocolVersionID: versionID, DoseCode: "ration", Sequence: 1,
		TriggerType: "calendar", Repeat: "every_n_days", CatchUp: "next_cycle",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	mkObl := func(key string, due time.Time) string {
		id, applied, err := obl.InsertObligation(ctx, obldomain.NewObligation{
			TenantID: feedTenant, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "shed", TargetID: feedShed, ScopeType: "shed", ScopeID: feedShed,
			DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("obligation %s: applied=%v err=%v", key, applied, err)
		}
		return id
	}
	obA := mkObl("feed-a", time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC))
	obB := mkObl("feed-b", time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC))

	completion := feedapp.NewCompletionService(feedapp.NewService(feed), obl)
	head := int32(40)
	mk := func(ob, key string) feeddomain.NewDirection {
		return feeddomain.NewDirection{
			TenantID: feedTenant, ObligationID: ob, ShedID: feedShed,
			QuantityFed: "12.5", QuantityUnit: "kg", HeadCount: &head,
			FedAt: time.Date(2026, 6, 23, 7, 0, 0, 0, time.UTC), IdempotencyKey: key,
		}
	}

	// Accept obA: direction accepted + obligation completed.
	ar, err := completion.Accept(ctx, feedapp.AcceptInput{Direction: mk(obA, "rec-a")})
	if err != nil || !ar.Applied || !ar.Completed {
		t.Fatalf("accept obA: %+v err=%v", ar, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obA); got != "completed" {
		t.Fatalf("obA status: want completed, got %s", got)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM feed_direction_completions WHERE tenant_id=$1 AND obligation_id=$2 AND status='accepted'`, feedTenant, obA); got != 1 {
		t.Fatalf("want 1 accepted feed direction for obA, got %d", got)
	}

	// Double-submit obA: no-op.
	if ar2, err := completion.Accept(ctx, feedapp.AcceptInput{Direction: mk(obA, "rec-a")}); err != nil || ar2.Applied {
		t.Fatalf("double-submit should be a no-op: %+v err=%v", ar2, err)
	}

	// Reject obB: obligation stays open, direction rejected.
	rr, err := completion.Reject(ctx, feedapp.RejectInput{Direction: mk(obB, "rec-b"), Reason: "blurry feed video"})
	if err != nil || !rr.Applied {
		t.Fatalf("reject obB: %+v err=%v", rr, err)
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obB); got == "completed" {
		t.Fatalf("obB must not be completed after reject")
	}
	if got := scanText(t, ctx, pool, `SELECT status FROM feed_direction_completions WHERE tenant_id=$1 AND obligation_id=$2`, feedTenant, obB); got != "rejected" {
		t.Fatalf("obB direction: want rejected, got %s", got)
	}

	// Verification queue is empty (one accepted, one rejected — none left recorded).
	queue, err := feed.ListRecordedDirections(ctx, feedTenant, 100)
	if err != nil || len(queue) != 0 {
		t.Fatalf("verification queue: want empty, got %d err=%v", len(queue), err)
	}
	// Shed history shows both.
	hist, err := feed.ListDirectionsByShed(ctx, feedTenant, feedShed, 100)
	if err != nil || len(hist) != 2 {
		t.Fatalf("shed history: want 2, got %d err=%v", len(hist), err)
	}
}

func scanText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&s); err != nil {
		t.Fatalf("scan %q: %v", sql, err)
	}
	return s
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}
