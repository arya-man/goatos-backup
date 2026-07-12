package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

func newTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var tenantID string
	err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), $1, 'active') RETURNING tenant_id::text`,
		"verification-test-tenant",
	).Scan(&tenantID)
	if err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return tenantID
}

func TestCreateItemIsIdempotentOnReplay_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	in := domain.CreateItem{
		TenantID: tenantID,
		Vertical: "preventive_care",
		Module:   "vaccination",
		Category: "vaccination_proof",
		Source: domain.SourceRef{
			Module:  "vaccination",
			RefType: "sop_submission",
			RefID:   tenantID,
		},
		MediaRefs:      []string{"proof-1", "proof-2"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "vaccination:submission:sub-1",
	}

	first, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("first CreateItem: %v", err)
	}
	if !first.Created {
		t.Fatal("first CreateItem should mint a new row")
	}
	if first.Item.Status != domain.StatusPending {
		t.Fatalf("status = %s, want pending", first.Item.Status)
	}
	if len(first.Item.MediaRefs) != 2 {
		t.Fatalf("media_refs = %v, want 2 entries", first.Item.MediaRefs)
	}

	second, err := repo.CreateItem(ctx, in)
	if err != nil {
		t.Fatalf("replay CreateItem: %v", err)
	}
	if second.Created {
		t.Fatal("replay with the same idempotency key must not mint a new row")
	}
	if second.Item.ItemID != first.Item.ItemID {
		t.Fatalf("replay returned a different item id: %s vs %s", second.Item.ItemID, first.Item.ItemID)
	}

	var itemCount, outboxCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM verification_items WHERE tenant_id = $1::uuid", tenantID).Scan(&itemCount); err != nil {
		t.Fatalf("count verification_items: %v", err)
	}
	if itemCount != 1 {
		t.Fatalf("verification_items rows = %d, want 1 (idempotent)", itemCount)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2", tenantID, EventItemPending).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox_messages: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox_messages rows for %s = %d, want 1 (idempotent, one event per item)", EventItemPending, outboxCount)
	}
}

func TestRecordVerdictLifecycle_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)
	created, err := repo.CreateItem(ctx, domain.CreateItem{
		TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
		Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
		MediaRefs:      []string{"proof-1"},
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: "vaccination:submission:sub-2",
	})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}

	verifierID := tenantID // any UUID is acceptable for this fixture

	// A reject without a reason must fail at the storage layer too (defense in depth behind the
	// app-layer 422 gate) -- verification_items_reject_reason_check.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionRejected,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if err == nil {
		t.Fatal("expected the reject-without-reason CHECK constraint to reject the write")
	}

	// Stale row_version must be reported as a conflict.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion + 99,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}

	approved, err := repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if err != nil {
		t.Fatalf("RecordVerdict approve: %v", err)
	}
	if approved.Status != domain.StatusApproved {
		t.Fatalf("status = %s, want approved", approved.Status)
	}
	if approved.VerifiedBy == nil || *approved.VerifiedBy != verifierID {
		t.Fatalf("verified_by = %v, want %s", approved.VerifiedBy, verifierID)
	}
	if approved.RowVersion != created.Item.RowVersion+1 {
		t.Fatalf("row_version = %d, want %d", approved.RowVersion, created.Item.RowVersion+1)
	}

	// A stale retry against the now-superseded row_version is a conflict, not a silent no-op --
	// proves optimistic concurrency actually advanced.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: created.Item.ItemID, Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: created.Item.RowVersion,
	})
	if !errors.Is(err, ports.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict on the now-stale row_version", err)
	}

	var verdictOutboxCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM outbox_messages WHERE tenant_id = $1::uuid AND event_type = $2", tenantID, EventVerdictApproved).Scan(&verdictOutboxCount); err != nil {
		t.Fatalf("count verdict outbox_messages: %v", err)
	}
	if verdictOutboxCount != 1 {
		t.Fatalf("outbox_messages rows for %s = %d, want 1", EventVerdictApproved, verdictOutboxCount)
	}

	// 404 for an item that does not exist.
	_, err = repo.RecordVerdict(ctx, domain.Verdict{
		TenantID: tenantID, ItemID: "00000000-0000-4000-8000-999999999999", Decision: domain.DecisionApproved,
		VerifierID: verifierID, RowVersion: 1,
	})
	if !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestListQueueKeysetIsBoundedAndOrdered_RealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := NewRepository(pool, 5*time.Second)
	tenantID := newTenant(t, ctx, pool)

	const total = 25
	base := time.Now().In(biztime.DefaultLocation()).Add(-time.Hour)
	for i := 0; i < total; i++ {
		_, err := repo.CreateItem(ctx, domain.CreateItem{
			TenantID: tenantID, Vertical: "preventive_care", Module: "vaccination", Category: "vaccination_proof",
			Source:         domain.SourceRef{Module: "vaccination", RefType: "sop_submission", RefID: tenantID},
			MediaRefs:      []string{"proof-1"},
			CapturedAt:     base.Add(time.Duration(i) * time.Minute),
			IdempotencyKey: "vaccination:submission:keyset-" + strconv.Itoa(i),
		})
		if err != nil {
			t.Fatalf("seed CreateItem[%d]: %v", i, err)
		}
	}

	pageSize := 10
	seen := map[string]bool{}
	var cursor *domain.Cursor
	pages := 0
	for {
		pages++
		if pages > total { // guard against a non-terminating loop
			t.Fatal("ListQueue did not terminate within the expected number of pages")
		}
		items, err := repo.ListQueue(ctx, ports.ListQueueParams{
			TenantID: tenantID, Category: "vaccination_proof", Status: domain.StatusPending,
			Cursor: cursor, Limit: pageSize + 1, // app layer requests Limit+1 to derive next_cursor
		})
		if err != nil {
			t.Fatalf("ListQueue: %v", err)
		}
		page := items
		hasNext := len(page) > pageSize
		if hasNext {
			page = page[:pageSize]
		}
		if len(page) > pageSize {
			t.Fatalf("page returned %d rows, want <= %d (bounded keyset)", len(page), pageSize)
		}
		for _, it := range page {
			if seen[it.ItemID] {
				t.Fatalf("item %s returned twice across pages -- keyset cursor did not advance", it.ItemID)
			}
			seen[it.ItemID] = true
		}
		if !hasNext {
			break
		}
		last := page[len(page)-1]
		cursor = &domain.Cursor{CapturedAt: last.CapturedAt, ItemID: last.ItemID}
	}
	if len(seen) != total {
		t.Fatalf("total items seen across pages = %d, want %d", len(seen), total)
	}
}
