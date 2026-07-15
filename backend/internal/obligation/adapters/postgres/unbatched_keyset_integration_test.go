package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// TestListUnbatchedDueForVersionKeysetDrainsPastFirstPage guards BUG #1: the cursor placeholder in
// ListUnbatchedDueForVersionKeyset must not collide with the LIMIT placeholder. It seeds MORE than
// one page of unbatched-due rows for a single version and drains the keyset pager to exhaustion
// with a tiny page size, asserting every row is returned exactly once and page 2+ does not error.
//
// FAILING-FIRST: before the fix the cursor predicate hardcoded `oi.obligation_id > $5` while the
// cursor UUID was appended as $4 and LIMIT as $5, so the second page compared obligation_id (uuid)
// against the LIMIT integer and Postgres errored (`operator does not exist: uuid > integer`),
// aborting the read-only preflight instead of draining every row.
func TestListUnbatchedDueForVersionKeysetDrainsPastFirstPage(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protocolpg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.keyset", Name: "Keyset", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protocoldomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
		TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}

	// Seed N goats + N unbatched-due obligations (N spans several pages at pageSize below).
	const n = 7
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	dueBefore := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantIDs := map[string]bool{}
	for i := 0; i < n; i++ {
		goatID := fmt.Sprintf("10000000-0000-4000-8000-0000000%05d", i)
		if _, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
				 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
			goatID, tenantID, meshaParty, cbePark); err != nil {
			t.Fatalf("seed goat %d: %v", i, err)
		}
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: due, Status: "scheduled", IdempotencyKey: fmt.Sprintf("obl-%d", i), Sequence: 1,
		})
		if err != nil || !applied || id == "" {
			t.Fatalf("seed obligation %d: id=%q applied=%v err=%v", i, id, applied, err)
		}
		wantIDs[id] = true
	}

	// Drain the keyset pager with a page smaller than n so at least one page-2+ fetch (which
	// carries the cursor) runs. pageSize=2, n=7 -> pages of 2,2,2,1.
	const pageSize = int32(2)
	seen := map[string]int{}
	var after *domain.UnbatchedDueCursor
	pages := 0
	for {
		pages++
		if pages > 100 {
			t.Fatalf("pager did not terminate; cursor not advancing")
		}
		rows, err := repo.ListUnbatchedDueForVersionKeyset(ctx, tenantID, versionID, dueBefore, after, pageSize)
		if err != nil {
			t.Fatalf("keyset page %d error (BUG #1: cursor vs LIMIT placeholder collision): %v", pages, err)
		}
		if len(rows) == 0 {
			break
		}
		if int32(len(rows)) > pageSize {
			t.Fatalf("page %d returned %d rows, exceeds LIMIT %d", pages, len(rows), pageSize)
		}
		for _, r := range rows {
			seen[r.ObligationID]++
		}
		if int32(len(rows)) < pageSize {
			break
		}
		last := rows[len(rows)-1]
		after = &domain.UnbatchedDueCursor{ObligationID: last.ObligationID}
	}

	if len(seen) != n {
		t.Fatalf("drained %d distinct rows, want %d", len(seen), n)
	}
	for id := range wantIDs {
		switch seen[id] {
		case 0:
			t.Fatalf("obligation %s missing from keyset drain", id)
		case 1:
			// ok
		default:
			t.Fatalf("obligation %s returned %d times (duplicate across pages)", id, seen[id])
		}
	}
	if pages < 2 {
		t.Fatalf("drain completed in %d page(s); test must exercise the cursor path (>=2 pages)", pages)
	}
}
