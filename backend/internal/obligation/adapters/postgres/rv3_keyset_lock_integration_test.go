package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// seedUnbatchedDue creates one draft version + rule and n alive goats each with one scheduled,
// unbatched (batch_id NULL) obligation due before dueBefore, across distinct sheds so the keyset
// ORDER BY (scope_type, scope_id, rule_id, ...) has multiple distinct groups to page through.
func seedUnbatchedDue(t *testing.T, ctx context.Context, pool *pgxpool.Pool, n int) (versionID string, dueBefore time.Time) {
	t.Helper()
	proto := protocolpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protocoldomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.keyset", Name: "Keyset", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err = proto.CreateVersion(ctx, protocoldomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
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
	repo := NewRepository(pool, 5*time.Second)
	dueBefore = time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		goatID := fmt.Sprintf("20000000-0000-4000-8000-0000000000%02x", i)
		if _, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
			goatID, tenantID, meshaParty, cbePark); err != nil {
			t.Fatalf("seed goat %d: %v", i, err)
		}
		id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
			TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
			DueAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Status: "scheduled",
			IdempotencyKey: fmt.Sprintf("obl-keyset-%d", i), Sequence: 1,
		})
		if err != nil || !applied || id == "" {
			t.Fatalf("seed obligation %d: applied=%v err=%v", i, applied, err)
		}
	}
	return versionID, dueBefore
}

// TestListUnbatchedDueForVersionKeysetPagesEveryRowOnce is the RV-02 SQL guard: the keyset scan
// must return EVERY unbatched-due candidate exactly once, in the stable ORDER BY order, across many
// small pages -- the property the preflight relies on to catch a tie beyond the first page.
func TestListUnbatchedDueForVersionKeysetPagesEveryRowOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const n = 7
	versionID, dueBefore := seedUnbatchedDue(t, ctx, pool, n)
	repo := NewRepository(pool, 5*time.Second)

	seen := make(map[string]int)
	var prev *domain.UnbatchedDueCursor
	var last domain.UnbatchedDue
	haveLast := false
	total := 0
	for page := 0; page < 100; page++ {
		rows, err := repo.ListUnbatchedDueForVersionKeyset(ctx, tenantID, versionID, dueBefore, prev, 2)
		if err != nil {
			t.Fatalf("keyset page %d: %v", page, err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			seen[r.ObligationID]++
			if haveLast && !cursorAdvanced(last, r) {
				t.Fatalf("keyset order violated: %q did not advance past %q", r.ObligationID, last.ObligationID)
			}
			last = r
			haveLast = true
			total++
		}
		if len(rows) < 2 {
			break
		}
		prev = &domain.UnbatchedDueCursor{
			ScopeType: last.ScopeType, ScopeID: last.ScopeID, RuleID: last.RuleID,
			TargetSpecies: last.TargetSpecies, TargetAnimalStage: last.TargetAnimalStage,
			DueAt: last.DueAt, ObligationID: last.ObligationID,
		}
	}
	if total != n {
		t.Fatalf("paged %d rows, want %d", total, n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("obligation %q returned %d times, want exactly 1", id, c)
		}
	}
}

func cursorAdvanced(prev, next domain.UnbatchedDue) bool {
	for _, cmp := range [][2]string{
		{prev.ScopeType, next.ScopeType}, {prev.ScopeID, next.ScopeID}, {prev.RuleID, next.RuleID},
		{prev.TargetSpecies, next.TargetSpecies}, {prev.TargetAnimalStage, next.TargetAnimalStage},
	} {
		if cmp[0] != cmp[1] {
			return cmp[0] < cmp[1]
		}
	}
	if !prev.DueAt.Equal(next.DueAt) {
		return prev.DueAt.Before(next.DueAt)
	}
	return prev.ObligationID < next.ObligationID
}

// TestLockTenantSweepSerializes is the RV-03 SQL guard: while one caller holds the per-tenant sweep
// lock, a second try must NOT acquire it; after release, a third try succeeds. This is what makes
// the sweep the single priority-ordered writer for a tenant across processes.
func TestLockTenantSweepSerializes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	acquired1, release1, err := repo.LockTenantSweep(ctx, tenantID)
	if err != nil || !acquired1 {
		t.Fatalf("first lock: acquired=%v err=%v, want true/nil", acquired1, err)
	}

	acquired2, release2, err := repo.LockTenantSweep(ctx, tenantID)
	if err != nil {
		t.Fatalf("second lock: %v", err)
	}
	if acquired2 {
		_ = release2(ctx)
		t.Fatalf("second lock acquired while first held; want acquired=false")
	}

	// A different tenant is independent and must still acquire.
	otherTenant := "00000000-0000-4000-8000-000000000002"
	acquiredOther, releaseOther, err := repo.LockTenantSweep(ctx, otherTenant)
	if err != nil || !acquiredOther {
		t.Fatalf("other-tenant lock: acquired=%v err=%v, want true/nil", acquiredOther, err)
	}
	_ = releaseOther(ctx)

	if err := release1(ctx); err != nil {
		t.Fatalf("release first: %v", err)
	}

	acquired3, release3, err := repo.LockTenantSweep(ctx, tenantID)
	if err != nil || !acquired3 {
		t.Fatalf("third lock after release: acquired=%v err=%v, want true/nil", acquired3, err)
	}
	_ = release3(ctx)
}
