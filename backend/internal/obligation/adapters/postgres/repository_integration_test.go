package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	tenantID   = "00000000-0000-4000-8000-000000000001"
	meshaParty = "00000000-0000-4000-8000-000000001001"
	cbePark    = "00000000-0000-4000-8000-000000003001"
	testGoatID = "10000000-0000-4000-8000-0000000000aa"
)

// seed creates a goat target + a draft protocol version + a rule + one obligation, and returns
// the obligation id. It exercises the protocol and obligation repos along the way.
func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (obligationID string) {
	t.Helper()

	// Goat target (raw insert; goats are owned by the identity module).
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, identity_state, custodian_party_id, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'clean', $3, $4, $4)`,
		testGoatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.test", Name: "Test", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "phc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("seed rule: %v", err)
	}

	repo := NewRepository(pool, 5*time.Second)
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil || !applied || id == "" {
		t.Fatalf("seed obligation: id=%q applied=%v err=%v", id, applied, err)
	}
	return id
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestObligationInsertIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	// Re-insert with the SAME idempotency key -> replay no-op.
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: mustVersionOf(t, ctx, pool), RuleID: mustRuleOf(t, ctx, pool),
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
		IdempotencyKey: "obl-1", Sequence: 1,
	})
	if err != nil {
		t.Fatalf("replay insert: %v", err)
	}
	if applied || id != "" {
		t.Fatalf("expected replay no-op, got id=%q applied=%v", id, applied)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "obl-1"); got != 1 {
		t.Fatalf("expected exactly 1 obligation row, got %d", got)
	}
	_ = obligationID
}

func TestStatusEventReserveBeforeInsertDedup(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	ev := domain.NewStatusEvent{
		TenantID: tenantID, ObligationID: obligationID, EventType: "became_due",
		OccurredAt: time.Now().UTC(), Payload: []byte(`{"k":1}`),
		IdempotencyKey: "evt-1", Scope: "obligation.status_event", RequestHash: "h1",
	}

	id1, applied1, err := repo.RecordStatusEvent(ctx, ev)
	if err != nil || !applied1 || id1 == "" {
		t.Fatalf("first record: id=%q applied=%v err=%v", id1, applied1, err)
	}
	id2, applied2, err := repo.RecordStatusEvent(ctx, ev)
	if err != nil {
		t.Fatalf("retry record: %v", err)
	}
	if applied2 || id2 != "" {
		t.Fatalf("expected retry dedup (applied=false), got id=%q applied=%v", id2, applied2)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "evt-1"); got != 1 {
		t.Fatalf("expected exactly 1 status event, got %d", got)
	}
}

func TestStatusEventConcurrentDedup(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	obligationID := seed(t, ctx, pool)

	repo := NewRepository(pool, 5*time.Second)
	const n = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	appliedCount := 0
	errs := make([]error, 0)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, applied, err := repo.RecordStatusEvent(ctx, domain.NewStatusEvent{
				TenantID: tenantID, ObligationID: obligationID, EventType: "became_due",
				OccurredAt: time.Now().UTC(), Payload: []byte(`{}`),
				IdempotencyKey: "evt-concurrent", Scope: "obligation.status_event", RequestHash: "h",
			})
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			if applied {
				appliedCount++
			}
		}()
	}
	wg.Wait()

	if len(errs) != 0 {
		t.Fatalf("concurrent record errors: %v", errs)
	}
	if appliedCount != 1 {
		t.Fatalf("expected exactly 1 applied insert under concurrency, got %d", appliedCount)
	}
	if got := countRows(t, ctx, pool, `SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, "evt-concurrent"); got != 1 {
		t.Fatalf("expected exactly 1 status event row under concurrency, got %d", got)
	}
}

// mustVersionOf / mustRuleOf re-read the seeded ids for the replay test.
func mustVersionOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT protocol_version_id::text FROM protocol_versions WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, tenantID).Scan(&id); err != nil {
		t.Fatalf("read version: %v", err)
	}
	return id
}

func mustRuleOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT rule_id::text FROM protocol_rules WHERE tenant_id=$1 ORDER BY created_at LIMIT 1`, tenantID).Scan(&id); err != nil {
		t.Fatalf("read rule: %v", err)
	}
	return id
}
