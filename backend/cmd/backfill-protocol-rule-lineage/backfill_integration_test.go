package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The backfill decides which of an animal's existing obligations may be labelled with a rule
// identity, and the label asserts an invariant: one open obligation per (animal, identity,
// sequence). Getting that decision wrong is not a cosmetic bug -- an over-eager label makes the
// unique index reject a legitimate row, and a missing one leaves work invisible to reconciliation,
// which is how the same dose gets booked twice.
//
// So it is exercised along the dimensions that actually change the answer: how many rows share a
// grouping key, which statuses count, whether due dates matter, and how many rows are in play.

const (
	bfTenant = "00000000-0000-4000-8000-000000000001"
	bfParty  = "00000000-0000-4000-8000-000000001001"
	bfPark   = "00000000-0000-4000-8000-000000003001"
	bfGoat   = "22000000-0000-4000-8000-0000000000b1"
)

func bfSeed(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (versionID string, ruleA, ruleB string) {
	t.Helper()
	// tenant, party and park come with the pgtest template; only the animal is ours.
	mustExec(t, ctx, pool, `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex, current_location_id, park_id)
		VALUES ($1,$2,'alive','goat',$3,'female',$4,$4) ON CONFLICT DO NOTHING`, bfGoat, bfTenant, bfParty, bfPark)

	var protocolID string
	mustQuery(t, ctx, pool, &protocolID, `INSERT INTO protocol_definitions (tenant_id, code, name, category, status)
		VALUES ($1,'vaccination.bf','BF','vaccination','draft') RETURNING protocol_id::text`, bfTenant)
	mustQuery(t, ctx, pool, &versionID, `INSERT INTO protocol_versions (tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl, proof_policy)
		VALUES ($1,$2,'tenant',1,'draft',now(),'{}','{}') RETURNING protocol_version_id::text`, bfTenant, protocolID)
	mustQuery(t, ctx, pool, &ruleA, `INSERT INTO protocol_rules (tenant_id, protocol_version_id, dose_code, "sequence", trigger_type)
		VALUES ($1,$2,'a_primary',1,'birth_age') RETURNING rule_id::text`, bfTenant, versionID)
	mustQuery(t, ctx, pool, &ruleB, `INSERT INTO protocol_rules (tenant_id, protocol_version_id, dose_code, "sequence", trigger_type)
		VALUES ($1,$2,'b_primary',1,'birth_age') RETURNING rule_id::text`, bfTenant, versionID)
	mustExec(t, ctx, pool, `INSERT INTO protocol_rule_lineage (tenant_id, protocol_version_id, rule_id, identity_key, content_fingerprint)
		VALUES ($1,$2,$3,'ident-a','fp-a'), ($1,$2,$4,'ident-b','fp-b')`, bfTenant, versionID, ruleA, ruleB)
	return versionID, ruleA, ruleB
}

func bfObligation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID, ruleID, status string, due time.Time, seq int32, key string) string {
	t.Helper()
	var id string
	mustQuery(t, ctx, pool, &id, `INSERT INTO obligation_instances
		(tenant_id, protocol_version_id, rule_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, "sequence")
		VALUES ($1,$2,$3,'goat',$4,'park',$5,$6,$7,$8,$9) RETURNING obligation_id::text`,
		bfTenant, versionID, ruleID, bfGoat, bfPark, due, status, key, seq)
	return id
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec: %v", err)
	}
}

func mustQuery(t *testing.T, ctx context.Context, pool *pgxpool.Pool, out *string, sql string, args ...any) {
	t.Helper()
	if err := pool.QueryRow(ctx, sql, args...).Scan(out); err != nil {
		t.Fatalf("query: %v", err)
	}
}

func identityOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) *string {
	t.Helper()
	var got *string
	if err := pool.QueryRow(ctx, `SELECT rule_identity_key FROM obligation_instances WHERE obligation_id = $1::uuid`, id).Scan(&got); err != nil {
		t.Fatalf("read identity: %v", err)
	}
	return got
}

// OneToMany: two open rows under ONE identity cannot both be labelled, and labelling either would
// assert an invariant the data contradicts. Neither is stamped, and a DIFFERENT identity on the
// same animal is unaffected -- the ambiguity is per grouping key, not per animal.
func TestStampSkipsOneToManyIdentityAndLeavesOtherIdentitiesAlone(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	version, ruleA, ruleB := bfSeed(t, ctx, pool)

	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	dupA1 := bfObligation(t, ctx, pool, version, ruleA, "scheduled", due, 1, "bf-a1")
	dupA2 := bfObligation(t, ctx, pool, version, ruleA, "scheduled", due.AddDate(0, 0, 3), 1, "bf-a2")
	soloB := bfObligation(t, ctx, pool, version, ruleB, "scheduled", due, 1, "bf-b1")

	if _, err := stampObligationIdentities(ctx, pool, bfTenant); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if identityOf(t, ctx, pool, dupA1) != nil || identityOf(t, ctx, pool, dupA2) != nil {
		t.Fatal("an ambiguous pair was labelled; the unique index would reject one of them")
	}
	if got := identityOf(t, ctx, pool, soloB); got == nil || *got != "ident-b" {
		t.Fatalf("the unambiguous row was not labelled: %v", got)
	}

	groups, err := countAmbiguousIdentities(ctx, pool, bfTenant)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if groups != 1 {
		t.Fatalf("reported %d ambiguous group(s), want exactly the one that exists", groups)
	}
}

// StatusMatrix: every status, one pass. Open work is labelled; terminal work is history and must be
// left alone -- and a terminal row must NOT make an open row look ambiguous, or a goat with a long
// completed history could never be labelled at all.
func TestStampStatusMatrixLabelsOpenWorkAndIgnoresTerminalRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	version, ruleA, _ := bfSeed(t, ctx, pool)

	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	terminal := map[string]string{}
	for i, status := range []string{"completed", "canceled", "missed"} {
		terminal[status] = bfObligation(t, ctx, pool, version, ruleA, status, due.AddDate(0, 0, i+1), 1, fmt.Sprintf("bf-term-%s", status))
	}
	open := bfObligation(t, ctx, pool, version, ruleA, "scheduled", due, 1, "bf-open")

	if _, err := stampObligationIdentities(ctx, pool, bfTenant); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if got := identityOf(t, ctx, pool, open); got == nil || *got != "ident-a" {
		t.Fatalf("open row not labelled beside terminal history: %v", got)
	}
	for status, id := range terminal {
		if identityOf(t, ctx, pool, id) != nil {
			t.Fatalf("%s row was labelled; terminal work is history and must not be touched", status)
		}
	}
}

// DateShift: identity is not a function of the due date. Rows a year apart under one identity are
// still the same ambiguity, and moving a date must not make an unlabelled row suddenly labellable.
func TestStampDateShiftDoesNotChangeWhatIsAmbiguous(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	version, ruleA, _ := bfSeed(t, ctx, pool)

	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	first := bfObligation(t, ctx, pool, version, ruleA, "scheduled", due, 1, "bf-d1")
	second := bfObligation(t, ctx, pool, version, ruleA, "scheduled", due.AddDate(1, 0, 0), 1, "bf-d2")

	if _, err := stampObligationIdentities(ctx, pool, bfTenant); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if identityOf(t, ctx, pool, first) != nil || identityOf(t, ctx, pool, second) != nil {
		t.Fatal("a year between two rows does not make them different work")
	}

	// Close one, and the survivor becomes unambiguous.
	mustExec(t, ctx, pool, `UPDATE obligation_instances SET status='completed' WHERE obligation_id=$1::uuid`, second)
	if _, err := stampObligationIdentities(ctx, pool, bfTenant); err != nil {
		t.Fatalf("re-stamp: %v", err)
	}
	if got := identityOf(t, ctx, pool, first); got == nil || *got != "ident-a" {
		t.Fatalf("survivor not labelled after the duplicate closed: %v", got)
	}
}

// PageBoundary / MultiPage: the stamp is one set-based statement, so a run that spans many rows must
// label all of them and count them exactly -- no silent truncation at some internal batch edge.
func TestStampMultiPageLabelsEveryRowAndCountsThemExactly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	version, ruleA, _ := bfSeed(t, ctx, pool)

	const rows = 250
	due := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	for i := 0; i < rows; i++ {
		// Distinct sequence per row: same rule, different dose slot, so each is its own identity
		// grouping key and none of them is ambiguous.
		bfObligation(t, ctx, pool, version, ruleA, "scheduled", due.AddDate(0, 0, i), int32(i+1), fmt.Sprintf("bf-page-%d", i))
	}

	tag, err := stampObligationIdentities(ctx, pool, bfTenant)
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if tag.RowsAffected() != rows {
		t.Fatalf("stamped %d row(s), want %d -- a partial pass reports success while leaving work invisible to reconciliation", tag.RowsAffected(), rows)
	}
	var unlabelled int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM obligation_instances WHERE tenant_id=$1::uuid AND rule_identity_key IS NULL AND status='scheduled'`, bfTenant).Scan(&unlabelled); err != nil {
		t.Fatalf("count: %v", err)
	}
	if unlabelled != 0 {
		t.Fatalf("%d open row(s) left unlabelled", unlabelled)
	}

	// Idempotent: a second pass has nothing left to do.
	again, err := stampObligationIdentities(ctx, pool, bfTenant)
	if err != nil {
		t.Fatalf("re-stamp: %v", err)
	}
	if again.RowsAffected() != 0 {
		t.Fatalf("re-run touched %d row(s), want 0", again.RowsAffected())
	}
}
