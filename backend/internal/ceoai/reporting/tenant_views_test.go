package reporting

// D0 tenant-isolation proofs (docs/ceo-ai/plan-v3-one-brain-two-doors.md, D0):
//
//   TestAssistantRoleCannotReadPublic     - mesha_ceo_readonly reads ceo_ai.* ONLY
//   TestEveryCeoAiViewHasTenantID         - every ceo_ai view exposes tenant_id
//   TestTrustedSQL_HealthIssue_TenantScoped - the trusted $1-bound read returns
//                                           zero rows of the other tenant
//
// All three are Postgres-gated (GOATOS_RUN_POSTGRES_TESTS=1 + Docker or
// GOATOS_PGTEST_ADMIN_DSN) and skip cleanly otherwise.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const assistantRole = "mesha_ceo_readonly"

// TestEveryCeoAiViewHasTenantID: a ceo_ai view without a tenant_id column
// cannot carry the mandatory tenant predicate and would be an unscoped read
// for whoever can SELECT it. information_schema is the oracle, so a new view
// added by any later migration is covered automatically.
func TestEveryCeoAiViewHasTenantID(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	rows, err := pool.Query(ctx, `
		SELECT v.table_name
		FROM information_schema.views v
		WHERE v.table_schema = 'ceo_ai'
		  AND NOT EXISTS (
		      SELECT 1 FROM information_schema.columns c
		      WHERE c.table_schema = v.table_schema AND c.table_name = v.table_name
		        AND c.column_name = 'tenant_id')
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("query views: %v", err)
	}
	defer rows.Close()
	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		missing = append(missing, name)
	}
	if len(missing) > 0 {
		t.Fatalf("ceo_ai views without a tenant_id column (add the column append-only in a new migration): %v", missing)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.views WHERE table_schema = 'ceo_ai'`).Scan(&total); err != nil {
		t.Fatalf("count views: %v", err)
	}
	if total == 0 {
		t.Fatal("no ceo_ai views found; the migrated template is not what this test expects")
	}
}

// TestAssistantRoleCannotReadPublic proves migration 000359 removes the public.*
// read that 000031 granted, on a database that had the 000031 posture applied
// (the readonly roles are provisioned per environment, so the template may have
// been migrated without the role existing; the test creates it, re-applies the
// 000031-style grant, then runs the 000359 Up SQL verbatim and asserts the
// outcome by actually SELECTing as the role).
func TestAssistantRoleCannotReadPublic(t *testing.T) {
	ctx := context.Background()
	pool, _ := newDB(t, ctx)

	created := ensureAssistantRole(t, ctx, pool)
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// Revoke everything granted inside this clone so the role can be dropped
		// (and so a pre-existing role is left exactly as it was elsewhere).
		_, _ = pool.Exec(cctx, `DROP OWNED BY `+assistantRole)
		if created {
			_, _ = pool.Exec(cctx, `DROP ROLE IF EXISTS `+assistantRole)
		}
	})

	// Pre-D0 posture (000031 / grant-assistant-public-read.sh before this change).
	for _, stmt := range []string{
		`GRANT USAGE ON SCHEMA public TO ` + assistantRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA public TO ` + assistantRole,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO ` + assistantRole,
		`GRANT USAGE ON SCHEMA ceo_ai TO ` + assistantRole,
		`GRANT SELECT ON ALL TABLES IN SCHEMA ceo_ai TO ` + assistantRole,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("pre-D0 grant %q: %v", stmt, err)
		}
	}
	if n := publicTablesReadableBy(t, ctx, pool, assistantRole); n == 0 {
		t.Fatal("pre-D0 posture not established: role cannot read any public table before the migration")
	}

	// Apply the D0 migration Up section verbatim.
	if _, err := pool.Exec(ctx, migrationUpSQL(t, "000359_ceo_ai_readonly_role_ceo_ai_only.sql")); err != nil {
		t.Fatalf("apply 000359 up: %v", err)
	}

	// 1. Catalog view: zero public tables readable, every ceo_ai view readable.
	if n := publicTablesReadableBy(t, ctx, pool, assistantRole); n != 0 {
		t.Fatalf("after 000359 the assistant role can still SELECT %d public tables", n)
	}
	var unreadableViews int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.views
		WHERE table_schema = 'ceo_ai'
		  AND NOT has_table_privilege($1, format('%I.%I', table_schema, table_name), 'SELECT')`,
		assistantRole).Scan(&unreadableViews); err != nil {
		t.Fatalf("count unreadable views: %v", err)
	}
	if unreadableViews != 0 {
		t.Fatalf("after 000359 the assistant role lost SELECT on %d ceo_ai views", unreadableViews)
	}

	// 2. Future tables: a table created after the migration by the migration
	//    owner must NOT auto-grant to the role any more.
	if _, err := pool.Exec(ctx, `CREATE TABLE public.d0_probe_after_revoke (id int)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	var probeReadable bool
	if err := pool.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.d0_probe_after_revoke', 'SELECT')`, assistantRole).Scan(&probeReadable); err != nil {
		t.Fatalf("probe privilege: %v", err)
	}
	if probeReadable {
		t.Fatal("default privileges still grant public SELECT to the assistant role for tables created after 000359")
	}

	// 3. Behavioural: SELECT as the role. public.goats must be permission denied
	//    (SQLSTATE 42501); a ceo_ai view that itself joins public tables must
	//    still work because the view resolves with its owner's rights.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET ROLE `+assistantRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	defer func() { _, _ = conn.Exec(context.Background(), `RESET ROLE`) }()

	var n int
	err = conn.QueryRow(ctx, `SELECT count(*) FROM public.goats`).Scan(&n)
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("SELECT public.goats as %s must fail with 42501 permission denied, got err=%v", assistantRole, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ceo_ai.animal_current_scope`).Scan(&n); err != nil {
		t.Fatalf("SELECT ceo_ai.animal_current_scope as %s must still work (view runs with owner rights): %v", assistantRole, err)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ceo_ai.source_entry_health_status`).Scan(&n); err != nil {
		t.Fatalf("SELECT ceo_ai.source_entry_health_status as %s must work: %v", assistantRole, err)
	}
}

// TestTrustedSQL_HealthIssue_TenantScoped seeds two tenants with distinguishable
// source labels and health blockers, runs the trusted source-entry health read
// exactly as the assistant does (tenant bound as $1 by the executor), and
// asserts tenant A's answer carries zero rows/labels of tenant B — including
// when a park scope ($2) is supplied. It also proves the query text carries no
// tenant literal at all (ValidateTrusted) so a tenant can never be smuggled in.
func TestTrustedSQL_HealthIssue_TenantScoped(t *testing.T) {
	ctx := context.Background()
	pool, tenantA := newDB(t, ctx)
	var tenantB string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'Northwind Goat Co', 'active') RETURNING tenant_id::text`,
	).Scan(&tenantB); err != nil {
		t.Fatalf("insert tenant B: %v", err)
	}

	parkA := park(t, ctx, pool, tenantA, "Coimbatore")
	parkB := park(t, ctx, pool, tenantB, "Northwind Park")
	seedHealthIssueLoad(t, ctx, pool, tenantA, parkA, "Kumar Traders", 2)
	seedHealthIssueLoad(t, ctx, pool, tenantB, parkB, "Northwind Meats", 3)

	exec := sqlguard.NewExecutorWithPool(pool, 5*time.Second)
	run := func(tenant string, args ...any) []sqlguard.Row {
		t.Helper()
		where := "tenant_id = $1 AND health_blockers > 0"
		if len(args) > 0 {
			where += " AND park_location_id = $2"
		}
		sql := `SELECT COALESCE(source_label, 'Unknown source') AS label, CAST(health_blockers AS text) AS value, load_label AS scope
			FROM ceo_ai.source_entry_health_status WHERE ` + where + ` ORDER BY health_blockers DESC LIMIT 50`
		if err := sqlguard.ValidateTrusted(sql); err != nil {
			t.Fatalf("trusted health SQL must validate: %v", err)
		}
		rows, err := exec.ExecuteTrustedReadOnlyForTenant(ctx, tenant, sql, args...)
		if err != nil {
			t.Fatalf("execute trusted: %v", err)
		}
		return rows
	}

	assertOnly := func(rows []sqlguard.Row, wantLabel, wantValue, forbidLabel string) {
		t.Helper()
		if len(rows) != 1 {
			t.Fatalf("want exactly 1 row, got %d: %v", len(rows), rows)
		}
		if got := rows[0]["label"]; got != wantLabel {
			t.Fatalf("label=%v want %q", got, wantLabel)
		}
		if got := rows[0]["value"]; got != wantValue {
			t.Fatalf("value=%v want %q", got, wantValue)
		}
		for _, r := range rows {
			for _, v := range r {
				if s, ok := v.(string); ok && strings.Contains(s, forbidLabel) {
					t.Fatalf("other tenant's label %q leaked: %v", forbidLabel, rows)
				}
			}
		}
	}

	// As tenant A: only A's load, never Northwind.
	assertOnly(run(tenantA), "Kumar Traders", "2", "Northwind")
	assertOnly(run(tenantA, parkA), "Kumar Traders", "2", "Northwind")
	// A asking for B's park id as $2 gets nothing (the tenant predicate wins).
	if rows := run(tenantA, parkB); len(rows) != 0 {
		t.Fatalf("tenant A scoped to tenant B's park must return zero rows, got %v", rows)
	}
	// As tenant B: only B's load, never Kumar.
	assertOnly(run(tenantB), "Northwind Meats", "3", "Kumar")

	// The executor prepends the session tenant as $1: a caller cannot shift it.
	if _, err := exec.ExecuteTrustedReadOnlyForTenant(ctx, tenantA,
		`SELECT label FROM ceo_ai.source_entry_health_status WHERE tenant_id = '`+tenantB+`' LIMIT 1`); err == nil {
		t.Fatal("trusted SQL carrying a tenant literal must be rejected")
	}
}

// TestSourceEntryHealthStatusOneToManyReviews: a load re-reviewed at intake has
// TWO arrival_intake_reviews rows and N failed health checks. The rebuilt view
// (000359) must still yield exactly ONE row for that load, with health_blockers
// = N (not 2N) and park_location_id/animals_received from the LATEST review.
// The baseline view LEFT JOINed the reviews directly and doubled the row.
func TestSourceEntryHealthStatusOneToManyReviews(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	parkOld := park(t, ctx, pool, tenant, "Coimbatore")
	parkNew := park(t, ctx, pool, tenant, "Channapatna")
	loadID := seedHealthIssueLoad(t, ctx, pool, tenant, parkOld, "Kumar Traders", 3)
	// Re-review, later, at another park with a different arrived count.
	if _, err := pool.Exec(ctx,
		`INSERT INTO arrival_intake_reviews (tenant_id, load_id, park_location_id, expected_count, arrived_count, matched_count, rejected_count, status, reviewed_at, idempotency_key)
		 VALUES ($1, $2, $3, 3, 7, 0, 0, 'accepted', now() + interval '1 hour', 'air-re-' || gen_random_uuid()::text)`,
		tenant, loadID, parkNew); err != nil {
		t.Fatalf("insert re-review: %v", err)
	}

	rows, err := pool.Query(ctx,
		`SELECT health_blockers, animals_received, park_location_id::text FROM ceo_ai.source_entry_health_status WHERE tenant_id = $1`, tenant)
	if err != nil {
		t.Fatalf("query view: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var blockers int64
		var received *int32
		var parkID *string
		if err := rows.Scan(&blockers, &received, &parkID); err != nil {
			t.Fatalf("scan: %v", err)
		}
		n++
		if blockers != 3 {
			t.Fatalf("health_blockers=%d, want 3 (review fan-out must not multiply the count)", blockers)
		}
		if received == nil || *received != 7 || parkID == nil || *parkID != parkNew {
			t.Fatalf("view must carry the LATEST review (received=7, park=%s), got received=%v park=%v", parkNew, received, parkID)
		}
	}
	if n != 1 {
		t.Fatalf("one load with two reviews must be exactly one view row, got %d", n)
	}
}

// TestSourceEntryHealthStatusPageBoundary: the trusted read caps at LIMIT 50
// while the view holds the whole per-load set. With 53 blocked loads the read
// returns exactly 50 (the highest-blocker loads first) and the view still
// counts 53 — pagination is a payload cap on the consumer, never a truth cap.
func TestSourceEntryHealthStatusPageBoundary(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	parkID := park(t, ctx, pool, tenant, "Coimbatore")
	const loads = 53
	for i := 0; i < loads; i++ {
		// Load i carries i%3+1 failed checks so the ORDER BY has a real ranking.
		seedHealthIssueLoad(t, ctx, pool, tenant, parkID, "Source "+strconv.Itoa(i), i%3+1)
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ceo_ai.source_entry_health_status WHERE tenant_id = $1 AND health_blockers > 0`, tenant).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != loads {
		t.Fatalf("view must hold every load: got %d want %d", total, loads)
	}
	exec := sqlguard.NewExecutorWithPool(pool, 5*time.Second)
	rows, err := exec.ExecuteTrustedReadOnlyForTenant(ctx, tenant,
		`SELECT COALESCE(source_label, 'Unknown source') AS label, CAST(health_blockers AS text) AS value, load_label AS scope
		 FROM ceo_ai.source_entry_health_status WHERE tenant_id = $1 AND health_blockers > 0 AND park_location_id = $2
		 ORDER BY health_blockers DESC, load_label ASC LIMIT 50`, parkID)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(rows) != 50 {
		t.Fatalf("trusted read must cap at 50 rows, got %d", len(rows))
	}
	// The first page is the top of the ranking: every returned blocker count is
	// >= the smallest count left out (loads with 1 blocker are the ones cut).
	for _, r := range rows[:17] { // i%3+1 == 3 for i = 2,5,...,50: 17 loads
		if r["value"] != "3" {
			t.Fatalf("first rows must be the 3-blocker loads, got %v", r)
		}
	}
}

// seedHealthIssueLoad inserts one procurement load for the tenant from a source
// party with the given display name, an arrival review at parkID, and n failed
// source health checks (each on its own goat).
func seedHealthIssueLoad(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, sourceName string, failed int) string {
	t.Helper()
	var partyID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES (gen_random_uuid(), 'org', $1, 'active') RETURNING party_id::text`, sourceName).Scan(&partyID); err != nil {
		t.Fatalf("insert party: %v", err)
	}
	var loadID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, expected_count, purchase_date, status, idempotency_key)
		 VALUES (gen_random_uuid(), $1, $2, $3, '2026-09-01', 'health_pending', 'load-' || gen_random_uuid()::text)
		 RETURNING load_id::text`, tenant, partyID, failed).Scan(&loadID); err != nil {
		t.Fatalf("insert load: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO arrival_intake_reviews (tenant_id, load_id, park_location_id, expected_count, arrived_count, matched_count, rejected_count, status, reviewed_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, $4, 0, 0, 'pending', now(), 'air-' || gen_random_uuid()::text)`,
		tenant, loadID, parkID, failed); err != nil {
		t.Fatalf("insert arrival review: %v", err)
	}
	shedID := shed(t, ctx, pool, tenant, parkID, "Intake", nil)
	for i := 0; i < failed; i++ {
		goatID := goatWithID(t, ctx, pool, tenant, parkID, shedID, "goat", "alive", nil, nil)
		if _, err := pool.Exec(ctx,
			`INSERT INTO procurement_source_health_checks (tenant_id, goat_id, load_id, health_state, checked_at, idempotency_key)
			 VALUES ($1, $2, $3, 'failed', now(), 'hc-' || gen_random_uuid()::text)`,
			tenant, goatID, loadID); err != nil {
			t.Fatalf("insert health check: %v", err)
		}
	}
	return loadID
}

// ensureAssistantRole creates the (cluster-global, NOLOGIN) assistant role when
// the server does not have it, and reports whether this test created it.
func ensureAssistantRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, assistantRole).Scan(&exists); err != nil {
		t.Fatalf("role exists: %v", err)
	}
	if exists {
		return false
	}
	if _, err := pool.Exec(ctx, `CREATE ROLE `+assistantRole+` NOLOGIN`); err != nil {
		t.Fatalf("create role: %v", err)
	}
	return true
}

func publicTablesReadableBy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, role string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		  AND has_table_privilege($1, format('%I.%I', table_schema, table_name), 'SELECT')`, role).Scan(&n); err != nil {
		t.Fatalf("count readable public tables: %v", err)
	}
	return n
}

// migrationUpSQL returns the goose Up section of one committed migration file.
func migrationUpSQL(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("backend module root not found from %s", wd)
		}
		dir = next
	}
	raw, err := os.ReadFile(filepath.Join(dir, "migrations", "postgres", name))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	var out []string
	inUp := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "-- +goose Up"):
			inUp = true
			continue
		case strings.HasPrefix(line, "-- +goose Down"):
			inUp = false
		}
		if inUp {
			out = append(out, line)
		}
	}
	up := strings.TrimSpace(strings.Join(out, "\n"))
	if up == "" {
		t.Fatalf("migration %s has no Up section", name)
	}
	return up
}

// keep pgtest referenced for the skip contract even if helpers above move.
var _ = pgtest.Enabled
