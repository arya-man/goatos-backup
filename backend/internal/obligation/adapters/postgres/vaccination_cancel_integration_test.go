package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// vaxCancelFixture is a goat whose vaccination protocol carries TWO draft versions, each with a
// rule. The second version is load-bearing: a single-version setup cannot tell a correct selector
// apart from a broken one that cancels the wrong version or ignores the effective-version list, so
// every selectivity assertion below leans on version v2 obligations staying untouched.
type vaxCancelFixture struct {
	v1, r1, v2, r2 string
}

func seedVaxCancelFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) vaxCancelFixture {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
			 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`,
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
	mkVersion := func(v int) (versionID, ruleID string) {
		versionID, err := proto.CreateVersion(ctx, protocoldomain.NewVersion{
			TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: int32(v), Status: "draft",
			EffectiveFrom: time.Date(2026, 6, v, 0, 0, 0, 0, time.UTC),
			RuleDsl:       []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("seed version %d: %v", v, err)
		}
		ruleID, err = proto.CreateRule(ctx, protocoldomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "primary", Sequence: 1,
			TriggerType: "birth_age", Repeat: "none", CatchUp: "pc_approval",
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("seed rule %d: %v", v, err)
		}
		// Retire it before the next version is drafted. One draft per plan is a database rule
		// now, and this fixture wants two VERSIONS, not two drafts. Retired rather than
		// published because two published versions cannot share an effective range either --
		// and what these tests exercise is cancelling a goat's work ACROSS versions, which
		// does not care which non-draft state each version ended in.
		if _, err := pool.Exec(ctx, `
UPDATE protocol_versions SET status = 'retired'
WHERE tenant_id = $1::uuid AND protocol_version_id = $2::uuid`, tenantID, versionID); err != nil {
			t.Fatalf("retire version %d: %v", v, err)
		}
		return versionID, ruleID
	}
	f := vaxCancelFixture{}
	f.v1, f.r1 = mkVersion(1)
	f.v2, f.r2 = mkVersion(2)
	return f
}

func insertOpenObl(t *testing.T, ctx context.Context, pool *pgxpool.Pool, repo *Repository, versionID, ruleID, idem string, seq int) string {
	t.Helper()
	id, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: testGoatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: time.Date(2026, 9, seq, 0, 0, 0, 0, time.UTC), Status: "scheduled", IdempotencyKey: idem, Sequence: int32(seq),
	})
	if err != nil || !applied {
		t.Fatalf("insert %s: applied=%v err=%v", idem, applied, err)
	}
	return id
}

func canceledEventCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID string) int {
	t.Helper()
	return countRows(t, ctx, pool,
		`SELECT count(*) FROM obligation_status_events WHERE tenant_id=$1 AND obligation_id=$2 AND event_type='canceled'`,
		tenantID, obligationID)
}

// canceledOutboxCount counts the lifecycle outbox rows emitted for one obligation's cancellation.
// insertObligationLifecycleOutbox keys each row idempotency_key = "goat.obligations_canceled:<id>",
// exactly one per aggregate, so this proves per-obligation cardinality rather than a bulk >= 1.
func canceledOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID string) int {
	t.Helper()
	return countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, "goat.obligations_canceled:"+obligationID)
}

// assertCanceledOnce asserts an obligation is canceled with exactly one status-event and one outbox
// row — the invariant that the bulk status-event insert and the per-obligation outbox loop must hold.
func assertCanceledOnce(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, id string) {
	t.Helper()
	if got := scanStatus(t, ctx, pool, id); got != "canceled" {
		t.Fatalf("%s: want canceled, got %s", label, got)
	}
	if got := canceledEventCount(t, ctx, pool, id); got != 1 {
		t.Fatalf("%s: want exactly 1 canceled status-event, got %d", label, got)
	}
	if got := canceledOutboxCount(t, ctx, pool, id); got != 1 {
		t.Fatalf("%s: want exactly 1 canceled outbox row, got %d", label, got)
	}
}

// assertUntouched asserts an obligation on a preserved version was not cancelled and emitted no
// cancellation side effects — the selectivity half that a single-version fixture cannot express.
func assertUntouched(t *testing.T, ctx context.Context, pool *pgxpool.Pool, label, id string) {
	t.Helper()
	if got := scanStatus(t, ctx, pool, id); got != "scheduled" {
		t.Fatalf("%s: want scheduled (preserved), got %s", label, got)
	}
	if got := canceledEventCount(t, ctx, pool, id); got != 0 {
		t.Fatalf("%s: preserved obligation must have 0 canceled events, got %d", label, got)
	}
	if got := canceledOutboxCount(t, ctx, pool, id); got != 0 {
		t.Fatalf("%s: preserved obligation must have 0 canceled outbox rows, got %d", label, got)
	}
}

// TestCancelOpenVaccinationObligationsForGoatVersion covers the bulk cancel path in
// recordCanceledObligationRows via CancelOpenVaccinationObligationsForGoatVersion. It asserts three
// distinct properties the sibling TestSM3CancelOpenForGoat (which hits CancelOpenForGoat, a
// different method) never touches:
//
//   - liveness: the multi-row bulk status-event insert executes (regression guard for the invalid
//     ON CONFLICT (idempotency_key) target that raised SQLSTATE 42P10 on a partitioned table);
//   - selectivity: only obligations on the named version are cancelled — an obligation on another
//     version of the same vaccination protocol is preserved;
//   - idempotent replay: a second call cancels nothing and writes no duplicate status-event/outbox.
func TestCancelOpenVaccinationObligationsForGoatVersion(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedVaxCancelFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	a1 := insertOpenObl(t, ctx, pool, repo, f.v1, f.r1, "obl-v1-a", 1)
	a2 := insertOpenObl(t, ctx, pool, repo, f.v1, f.r1, "obl-v1-b", 2) // second v1 row => multi-row bulk insert
	keep := insertOpenObl(t, ctx, pool, repo, f.v2, f.r2, "obl-v2", 3) // other version => must survive

	n, err := repo.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, testGoatID, f.v1, "ineligible_after_recheck", time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("cancel by version: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 cancelled (both v1 obligations), got %d", n)
	}
	assertCanceledOnce(t, ctx, pool, "obl-v1-a", a1)
	assertCanceledOnce(t, ctx, pool, "obl-v1-b", a2)
	assertUntouched(t, ctx, pool, "obl-v2", keep)

	// Replay: rows are already canceled so the UPDATE ... RETURNING selects nothing, the bulk insert
	// and outbox loop are skipped, and the earlier side-effect counts are unchanged.
	n2, err := repo.CancelOpenVaccinationObligationsForGoatVersion(ctx, tenantID, testGoatID, f.v1, "ineligible_after_recheck", time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("replay cancel by version: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("replay should cancel 0, got %d", n2)
	}
	assertCanceledOnce(t, ctx, pool, "obl-v1-a (post-replay)", a1)
	assertCanceledOnce(t, ctx, pool, "obl-v1-b (post-replay)", a2)
	assertUntouched(t, ctx, pool, "obl-v2 (post-replay)", keep)
}

// TestCancelOpenVaccinationObligationsForGoatExceptVersions covers the same bulk path via the
// except-versions method, with the same liveness + selectivity + replay guarantees. Here selectivity
// means: passing the effective-version set {v2} cancels only the non-effective v1 obligations and
// preserves the v2 obligation — a broken filter that ignores the effective list would cancel keep.
func TestCancelOpenVaccinationObligationsForGoatExceptVersions(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedVaxCancelFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	a1 := insertOpenObl(t, ctx, pool, repo, f.v1, f.r1, "obl-v1-a", 1)
	a2 := insertOpenObl(t, ctx, pool, repo, f.v1, f.r1, "obl-v1-b", 2)
	keep := insertOpenObl(t, ctx, pool, repo, f.v2, f.r2, "obl-v2", 3)

	// v2 is effective; every open obligation NOT on v2 (both v1 rows) must be cancelled.
	n, err := repo.CancelOpenVaccinationObligationsForGoatExceptVersions(ctx, tenantID, testGoatID, []string{f.v2}, "version_no_longer_effective", time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("cancel except-versions: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 cancelled (both v1 obligations), got %d", n)
	}
	assertCanceledOnce(t, ctx, pool, "obl-v1-a", a1)
	assertCanceledOnce(t, ctx, pool, "obl-v1-b", a2)
	assertUntouched(t, ctx, pool, "obl-v2 (effective)", keep)

	// Replay is idempotent and touches no side-effect counts.
	n2, err := repo.CancelOpenVaccinationObligationsForGoatExceptVersions(ctx, tenantID, testGoatID, []string{f.v2}, "version_no_longer_effective", time.Now().In(biztime.DefaultLocation()))
	if err != nil {
		t.Fatalf("replay cancel except-versions: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("replay should cancel 0, got %d", n2)
	}
	assertCanceledOnce(t, ctx, pool, "obl-v1-a (post-replay)", a1)
	assertCanceledOnce(t, ctx, pool, "obl-v1-b (post-replay)", a2)
	assertUntouched(t, ctx, pool, "obl-v2 (post-replay)", keep)
}
