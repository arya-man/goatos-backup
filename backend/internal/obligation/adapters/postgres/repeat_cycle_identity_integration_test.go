package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Repeat-cycle identity: a repeat dose is anchored to when the PREVIOUS dose was actually
// given, so its due date legitimately moves. Identity therefore has to be the immutable
// cause -- the completed dose -- and never the mutable effect. Every test below moves the
// due date between attempts, because a test that reuses one due date passes just as happily
// against the old due-date identity and would prove nothing.

type repeatFixture struct {
	repo      *Repository
	versionID string
	ruleA     string
	ruleB     string
	goat      string
}

func seedRepeatCycleFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, code string) repeatFixture {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination." + code, Name: code, Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), RuleDsl: []byte(`{}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	newRule := func(dose string, seq int32) string {
		t.Helper()
		id, err := proto.CreateRule(ctx, protodomain.NewRule{
			TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: dose, Sequence: seq,
			TriggerType: "after_previous_completion", Repeat: "every_n_days", CatchUp: "pc_approval",
			EligibilityJSON: []byte(`{}`), ProofPolicy: []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("rule %s: %v", dose, err)
		}
		return id
	}
	goat := "10000000-0000-4000-8000-0000000000f1"
	seedReserveGoats(t, ctx, pool, cbePark, cbePark, goat)
	return repeatFixture{
		repo:      repo,
		versionID: versionID,
		ruleA:     newRule("revac", 2),
		ruleB:     newRule("revac_shared", 3),
		goat:      goat,
	}
}

func (f repeatFixture) insert(t *testing.T, ctx context.Context, ruleID, key string, due time.Time, rc *domain.RepeatCycleSource) (string, bool) {
	t.Helper()
	id, applied, err := f.repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: f.versionID, RuleID: ruleID,
		TargetType: "goat", TargetID: f.goat, ScopeType: "park", ScopeID: cbePark,
		DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 2,
		RepeatCycle: rc,
	})
	if err != nil {
		// An index rejection must never reach the caller as an error: it means some other
		// writer already created this cycle, which is the outcome we wanted.
		t.Fatalf("insert %s: unexpected error %v", key, err)
	}
	return id, applied
}

func anchoredOn(obligationID string, at time.Time) *domain.RepeatCycleSource {
	return &domain.RepeatCycleSource{
		Source:             domain.RepeatCycleSourceCompletedObligation,
		SourceRef:          obligationID,
		AnchorObligationID: &obligationID,
		AnchorAt:           &at,
	}
}

func (f repeatFixture) openCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ruleID string) int {
	t.Helper()
	return countRows(t, ctx, pool, `
SELECT count(*) FROM obligation_instances
WHERE tenant_id=$1 AND rule_id=$2 AND target_id=$3
  AND status IN ('scheduled','due','in_progress','deferred')`, tenantID, ruleID, f.goat)
}

func (f repeatFixture) setStatus(t *testing.T, ctx context.Context, pool *pgxpool.Pool, obligationID, status string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status=$3, completed_at=CASE WHEN $3='completed' THEN now() ELSE completed_at END
WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, obligationID, status); err != nil {
		t.Fatalf("set status %s: %v", status, err)
	}
}

// P1: one completed dose mints exactly ONE open successor, however many times the
// completion is replayed and however far the recomputed due date has moved. This is the
// measured defect: 207 staging groups hold two rows 0-1 days apart.
func TestRepeatCycleMovedDueDateDoesNotMintASecondSuccessor(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "repeat_moved_due")

	anchor := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	completed, applied := f.insert(t, ctx, f.ruleA, "prev-dose", anchor, nil)
	if !applied {
		t.Fatal("seed previous dose: not applied")
	}
	f.setStatus(t, ctx, pool, completed, "completed")

	rc := anchoredOn(completed, anchor)
	if _, applied := f.insert(t, ctx, f.ruleA, "succ-day-1", anchor.AddDate(0, 0, 180), rc); !applied {
		t.Fatal("first successor: want applied")
	}
	// Next pass: same cause, due date moved by a day, so a DIFFERENT idempotency key. Under
	// due-date identity this is where the duplicate appeared.
	if _, applied := f.insert(t, ctx, f.ruleA, "succ-day-2", anchor.AddDate(0, 0, 181), anchoredOn(completed, anchor)); applied {
		t.Fatal("moved due date minted a second successor: want suppressed")
	}
	if got := f.openCount(t, ctx, pool, f.ruleA); got != 1 {
		t.Fatalf("open successors = %d, want 1", got)
	}
}

// P2: the anchor is consumed only while its successor is OPEN. Once that successor closes
// the anchor must free, or the animal silently stops being scheduled forever.
func TestRepeatCycleAnchorFreesOnceItsSuccessorCloses(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "repeat_anchor_frees")

	anchor := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	completed, _ := f.insert(t, ctx, f.ruleA, "prev-dose", anchor, nil)
	f.setStatus(t, ctx, pool, completed, "completed")

	succ, applied := f.insert(t, ctx, f.ruleA, "succ-1", anchor.AddDate(0, 0, 180), anchoredOn(completed, anchor))
	if !applied {
		t.Fatal("first successor: want applied")
	}
	// missed is closed history, not an open obligation. Had it stayed inside the open set,
	// the first missed dose would hold the anchor permanently -- the exact failure this
	// design exists to prevent, reintroduced through its own predicate.
	f.setStatus(t, ctx, pool, succ, "missed")
	if _, applied := f.insert(t, ctx, f.ruleA, "succ-2", anchor.AddDate(0, 0, 200), anchoredOn(completed, anchor)); !applied {
		t.Fatal("anchor stayed consumed after its successor was missed: want a new cycle")
	}
	if got := f.openCount(t, ctx, pool, f.ruleA); got != 1 {
		t.Fatalf("open successors = %d, want 1", got)
	}
}

// P3: one administration can legitimately cause work under two rules -- a combo vaccine
// drives its own revac rule and a shared-component rule. Keyed on the anchor alone the
// second would be rejected, and rejected as a hard error, since the insert guard is
// rule-scoped and would not have suppressed it.
func TestRepeatCycleOneDoseCanAnchorTwoDifferentRules(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "repeat_two_rules")

	anchor := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	completed, _ := f.insert(t, ctx, f.ruleA, "prev-dose", anchor, nil)
	f.setStatus(t, ctx, pool, completed, "completed")

	if _, applied := f.insert(t, ctx, f.ruleA, "succ-rule-a", anchor.AddDate(0, 0, 180), anchoredOn(completed, anchor)); !applied {
		t.Fatal("rule A successor: want applied")
	}
	if _, applied := f.insert(t, ctx, f.ruleB, "succ-rule-b", anchor.AddDate(0, 0, 180), anchoredOn(completed, anchor)); !applied {
		t.Fatal("rule B successor rejected: one dose must be able to cause work under two rules")
	}
}

// P4: history-driven repeats have no completed obligation to anchor to -- their cause is a
// past administration, referenced by its own immutable coordinates. The source reference
// alone must still collapse a moved due date.
func TestRepeatCycleHistorySourceDedupesWithoutAnAnchorObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "repeat_history_source")

	at := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	hist := func() *domain.RepeatCycleSource {
		return &domain.RepeatCycleSource{
			Source:    domain.RepeatCycleSourceTrustedHistory,
			SourceRef: "et_tt|2026-03-04T00:00:00Z|2",
			AnchorAt:  &at,
		}
	}
	if _, applied := f.insert(t, ctx, f.ruleA, "hist-1", at.AddDate(0, 0, 180), hist()); !applied {
		t.Fatal("first history cycle: want applied")
	}
	if _, applied := f.insert(t, ctx, f.ruleA, "hist-2", at.AddDate(0, 0, 181), hist()); applied {
		t.Fatal("moved due date minted a second history cycle: want suppressed")
	}
	if got := f.openCount(t, ctx, pool, f.ruleA); got != 1 {
		t.Fatalf("open cycles = %d, want 1", got)
	}
}

// P5: a DIFFERENT cause is different work. The dedupe must not be so eager that the next
// genuine cycle -- anchored to the dose just given -- gets swallowed.
func TestRepeatCycleDifferentCauseStillMintsNewWork(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "repeat_new_cause")

	anchor := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	first, _ := f.insert(t, ctx, f.ruleA, "dose-1", anchor, nil)
	f.setStatus(t, ctx, pool, first, "completed")
	succ, _ := f.insert(t, ctx, f.ruleA, "succ-1", anchor.AddDate(0, 0, 180), anchoredOn(first, anchor))

	// The successor is administered; it becomes the cause of the cycle after it.
	f.setStatus(t, ctx, pool, succ, "completed")
	second := anchor.AddDate(0, 0, 182)
	if _, applied := f.insert(t, ctx, f.ruleA, "succ-2", second.AddDate(0, 0, 180), anchoredOn(succ, second)); !applied {
		t.Fatal("next cycle suppressed: a different cause is different work")
	}
	if got := f.openCount(t, ctx, pool, f.ruleA); got != 1 {
		t.Fatalf("open cycles = %d, want 1", got)
	}
}

// P6: the insert guard is shared by every obligation writer. Rows that carry no repeat
// metadata must behave exactly as they did before, including keeping their due-date
// identity -- two different due dates for a non-repeat rule are two obligations.
func TestNonRepeatObligationsKeepDueDateIdentity(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepeatCycleFixture(t, ctx, pool, "non_repeat_identity")

	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if _, applied := f.insert(t, ctx, f.ruleA, "plain-1", due, nil); !applied {
		t.Fatal("first plain obligation: want applied")
	}
	if _, applied := f.insert(t, ctx, f.ruleA, "plain-2", due, nil); applied {
		t.Fatal("same due date inserted twice: the pre-existing duplicate guard regressed")
	}
	if _, applied := f.insert(t, ctx, f.ruleA, "plain-3", due.AddDate(0, 0, 30), nil); !applied {
		t.Fatal("different due date suppressed: non-repeat identity regressed")
	}
}
