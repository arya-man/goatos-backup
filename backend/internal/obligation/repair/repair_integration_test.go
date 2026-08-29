package repair_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/obligation/repair"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const (
	tenantID   = "00000000-0000-4000-8000-000000000001"
	meshaParty = "00000000-0000-4000-8000-000000001001"
	cbePark    = "00000000-0000-4000-8000-000000003001"
	goatID     = "10000000-0000-4000-8000-0000000000a1"
)

type fixture struct {
	pool    *pgxpool.Pool
	repo    *obligationpg.Repository
	version string
	rule    string
}

// seedRepairFixture builds a repeat rule whose vaccine code matches what the repair resolves
// from eligibility_json, plus one animal.
func seedRepairFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) fixture {
	t.Helper()
	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := obligationpg.NewRepository(pool, 5*time.Second)

	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: tenantID, Code: "vaccination.repair_fixture", Name: "RepairFixture",
		Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: tenantID, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"vaccine":{"code":"ET_TT"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: tenantID, ProtocolVersionID: versionID, DoseCode: "et_tt_revac", Sequence: 2,
		TriggerType: "after_previous_completion", Repeat: "every_n_days", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"ET_TT"}}`), ProofPolicy: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
		 VALUES ($1, $2, 'alive', 'goat', $3, 'female', $4, $4)`, goatID, tenantID, meshaParty, cbePark); err != nil {
		t.Fatalf("seed goat: %v", err)
	}
	return fixture{pool: pool, repo: repo, version: versionID, rule: ruleID}
}

func (f fixture) insertOpen(t *testing.T, ctx context.Context, key string, due time.Time, status string) string {
	t.Helper()
	id, applied, err := f.repo.InsertObligation(ctx, domain.NewObligation{
		TenantID: tenantID, ProtocolVersionID: f.version, RuleID: f.rule,
		TargetType: "goat", TargetID: goatID, ScopeType: "park", ScopeID: cbePark,
		DueAt: due, Status: "scheduled", IdempotencyKey: key, Sequence: 2,
	})
	if err != nil || !applied {
		t.Fatalf("insert %s: applied=%v err=%v", key, applied, err)
	}
	if status != "scheduled" {
		if _, err := f.pool.Exec(ctx,
			`UPDATE obligation_instances SET status=$3 WHERE tenant_id=$1 AND obligation_id=$2`,
			tenantID, id, status); err != nil {
			t.Fatalf("set status: %v", err)
		}
	}
	return id
}

// seedAdministration records an accepted administration of the rule's vaccine -- the cause the
// repair reconstructs the cycle from.
func (f fixture) seedAdministration(t *testing.T, ctx context.Context, at time.Time) {
	t.Helper()
	done := f.insertOpen(t, ctx, "administered-dose", at, "scheduled")
	if _, err := f.pool.Exec(ctx,
		`UPDATE obligation_instances SET status='completed', completed_at=$3 WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, done, at); err != nil {
		t.Fatalf("complete dose: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, administered_at, status, verified_at, doses, recorded_by, idempotency_key)
VALUES ($1, $2, $3, $4, 'accepted', $4, 1, $5, $6)`,
		tenantID, done, goatID, at, meshaParty, "vc-"+done); err != nil {
		t.Fatalf("seed completion: %v", err)
	}
}

func (f fixture) statusOf(t *testing.T, ctx context.Context, id string) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(ctx,
		`SELECT status FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`, tenantID, id).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	return status
}

// P7: the repair retires the duplicate, labels the survivor with the cause generation itself
// would compute, and a second run changes nothing. Rerun safety is the whole point -- an
// operational repair that is not safe to run twice cannot be run at all.
func TestRepairRetiresDuplicateLabelsSurvivorAndIsIdempotent(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepairFixture(t, ctx, pool)

	administered := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	f.seedAdministration(t, ctx, administered)
	// The same cycle, minted twice a day apart, exactly as measured in staging.
	older := f.insertOpen(t, ctx, "cycle-day-1", administered.AddDate(0, 0, 180), "scheduled")
	newer := f.insertOpen(t, ctx, "cycle-day-2", administered.AddDate(0, 0, 181), "scheduled")

	cfg := repair.Config{TenantID: tenantID, Mode: "repeat", Limit: 100}
	got, err := repair.Run(ctx, pool, f.repo, cfg)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if got.DuplicatesRetired != 1 || got.CyclesLabelled != 1 {
		t.Fatalf("dry run: retired=%d labelled=%d, want 1 and 1", got.DuplicatesRetired, got.CyclesLabelled)
	}
	if s := f.statusOf(t, ctx, older); s != "scheduled" {
		t.Fatalf("dry run mutated a row: older status = %s", s)
	}

	cfg.Apply = true
	if got, err = repair.Run(ctx, pool, f.repo, cfg); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got.DuplicatesRetired != 1 || got.CyclesLabelled != 1 {
		t.Fatalf("apply: retired=%d labelled=%d, want 1 and 1", got.DuplicatesRetired, got.CyclesLabelled)
	}
	// The newest row survives: it holds the most recently computed due date.
	if s := f.statusOf(t, ctx, newer); s != "scheduled" {
		t.Fatalf("survivor status = %s, want scheduled", s)
	}
	if s := f.statusOf(t, ctx, older); s != "canceled" {
		t.Fatalf("duplicate status = %s, want canceled", s)
	}
	var ref string
	if err := pool.QueryRow(ctx,
		`SELECT coalesce(repeat_cycle_source_ref,'') FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, newer).Scan(&ref); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	// Generation references a cause as vaccine|administered-at|dose. Any other shape would
	// leave the legacy row invisible to the anchored insert and the duplicate would return.
	if want := "et_tt|2026-03-04T00:00:00Z|2"; ref != want {
		t.Fatalf("survivor stamped %q, want %q", ref, want)
	}

	rerun, err := repair.Run(ctx, pool, f.repo, cfg)
	if err != nil {
		t.Fatalf("rerun: %v", err)
	}
	if rerun.DuplicatesRetired != 0 || rerun.CyclesLabelled != 0 || rerun.SkippedAlreadyValid != 1 {
		t.Fatalf("rerun changed things: %+v", rerun)
	}
}

// P8: work already under way is never retired. The survivor is chosen by status first, so an
// animal being vaccinated right now does not have its obligation cancelled mid-drive just
// because a newer, stamped row exists beside it.
func TestRepairNeverRetiresWorkInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepairFixture(t, ctx, pool)

	administered := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	f.seedAdministration(t, ctx, administered)
	working := f.insertOpen(t, ctx, "cycle-being-worked", administered.AddDate(0, 0, 180), "in_progress")
	newer := f.insertOpen(t, ctx, "cycle-newer", administered.AddDate(0, 0, 181), "scheduled")

	if _, err := repair.Run(ctx, pool, f.repo, repair.Config{
		TenantID: tenantID, Mode: "repeat", Limit: 100, Apply: true,
	}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if s := f.statusOf(t, ctx, working); s != "in_progress" {
		t.Fatalf("in-progress obligation was retired: status = %s", s)
	}
	if s := f.statusOf(t, ctx, newer); s != "canceled" {
		t.Fatalf("non-survivor status = %s, want canceled", s)
	}
}

// P9: with no reconstructable cause the row is left exactly as it is and reported, never
// stamped with a guess. A wrong anchor is worse than none: it makes two genuinely different
// cycles collide.
func TestRepairLeavesRowsWithNoResolvableCauseAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	f := seedRepairFixture(t, ctx, pool)

	due := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	only := f.insertOpen(t, ctx, "cycle-no-cause", due, "scheduled")

	got, err := repair.Run(ctx, pool, f.repo, repair.Config{
		TenantID: tenantID, Mode: "repeat", Limit: 100, Apply: true,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got.SkippedNoAnchor != 1 || got.CyclesLabelled != 0 || got.DuplicatesRetired != 0 {
		t.Fatalf("counters = %+v, want one skipped-no-anchor and nothing else", got)
	}
	if s := f.statusOf(t, ctx, only); s != "scheduled" {
		t.Fatalf("row status = %s, want untouched", s)
	}
	var ref *string
	if err := pool.QueryRow(ctx,
		`SELECT repeat_cycle_source_ref FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2`,
		tenantID, only).Scan(&ref); err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	if ref != nil {
		t.Fatalf("row was stamped with a guessed cause: %q", *ref)
	}
}
