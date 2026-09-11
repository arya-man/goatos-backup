package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// The assignee picker behind "+" on the phone's Tasks module lists everyone whose mobile
// Tasks row carries OVERSEE. 000285 seeded every active employee that way; 000292 takes it
// back from anyone who is not leadership (maintainer decision 2026-09-11) -- and ONLY where
// 000285 put it there, so a tick set by hand on /people survives.
func TestLeadershipTasksAssigneesNarrowToLeadershipOnly(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	const tenant = "d0000000-0000-4000-8000-000000000001"
	if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Assignee Narrowing', 'active') ON CONFLICT DO NOTHING`, tenant); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	type person struct {
		name     string
		userID   string
		role     string // "" = no active grant at all
		seededBy string // "rows" (000285 inserted the row) | "caps" (000285 widened it) | "hand" (an admin ticked it)
	}
	people := []person{
		{"operator-seeded-row", "d0000000-0000-4000-8000-000000000101", "operator", "rows"},
		{"operator-widened", "d0000000-0000-4000-8000-000000000102", "operator", "caps"},
		{"operator-hand-ticked", "d0000000-0000-4000-8000-000000000103", "operator", "hand"},
		{"no-grant-seeded-row", "d0000000-0000-4000-8000-000000000104", "", "rows"},
		{"park-head", "d0000000-0000-4000-8000-000000000105", "park_head", "rows"},
		{"director", "d0000000-0000-4000-8000-000000000106", "feed_director", "caps"},
		{"cxo", "d0000000-0000-4000-8000-000000000107", "ceo_internal", "caps"},
	}
	memberID := map[string]string{}
	for i, p := range people {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active') RETURNING workforce_member_id::text`,
			tenant, p.userID, "AN"+string(rune('A'+i)), p.name).Scan(&id); err != nil {
			t.Fatalf("seed member %s: %v", p.name, err)
		}
		memberID[p.name] = id
		if p.role != "" {
			if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, $3, 'tenant', $1::uuid, 'active', now() - interval '1 day')`, tenant, p.userID, p.role); err != nil {
				t.Fatalf("seed grant %s: %v", p.name, err)
			}
		}
		if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
VALUES ($1::uuid, $2::uuid, 'mobile', 'leadership_tasks', ARRAY['oversee','view']::text[])`, tenant, id); err != nil {
			t.Fatalf("seed tick %s: %v", p.name, err)
		}
		switch p.seededBy {
		case "rows":
			if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access_leadership_tasks_two_way_rows (tenant_id, workforce_member_id, surface)
VALUES ($1::uuid, $2::uuid, 'mobile')`, tenant, id); err != nil {
				t.Fatalf("record 000285 row %s: %v", p.name, err)
			}
		case "caps":
			if _, err := pool.Exec(ctx, `
INSERT INTO person_module_access_leadership_tasks_two_way_caps (tenant_id, workforce_member_id, surface, capability)
VALUES ($1::uuid, $2::uuid, 'mobile', 'oversee')`, tenant, id); err != nil {
				t.Fatalf("record 000285 cap %s: %v", p.name, err)
			}
		}
	}

	raw, err := os.ReadFile("000292_leadership_tasks_assignees_leadership_only.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sqlText := string(raw)
	if _, err := pool.Exec(ctx, migrationUp(sqlText)); err != nil {
		t.Fatalf("apply 000292 up: %v", err)
	}

	oversee := func(name string) bool {
		var has bool
		if err := pool.QueryRow(ctx, `
SELECT 'oversee' = ANY(capabilities) FROM person_module_access
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND surface = 'mobile' AND module_key = 'leadership_tasks'`,
			tenant, memberID[name]).Scan(&has); err != nil {
			t.Fatalf("read tick %s: %v", name, err)
		}
		return has
	}
	view := func(name string) bool {
		var has bool
		if err := pool.QueryRow(ctx, `
SELECT 'view' = ANY(capabilities) FROM person_module_access
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND surface = 'mobile' AND module_key = 'leadership_tasks'`,
			tenant, memberID[name]).Scan(&has); err != nil {
			t.Fatalf("read view %s: %v", name, err)
		}
		return has
	}

	wantAssignable := map[string]bool{
		"operator-seeded-row":  false,
		"operator-widened":     false,
		"no-grant-seeded-row":  false,
		"operator-hand-ticked": true, // an admin's own tick is not the seed going stale
		"park-head":            true,
		"director":             true,
		"cxo":                  true,
	}
	for name, want := range wantAssignable {
		if got := oversee(name); got != want {
			t.Fatalf("after up: %s assignable = %v, want %v", name, got, want)
		}
		if !view(name) {
			t.Fatalf("after up: %s lost the module itself; only oversee is withdrawn", name)
		}
	}

	var narrowed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM person_module_access_leadership_tasks_assignee_narrowing WHERE tenant_id = $1::uuid`, tenant).Scan(&narrowed); err != nil {
		t.Fatal(err)
	}
	if narrowed != 3 {
		t.Fatalf("narrowing recorded %d rows, want 3", narrowed)
	}

	// Re-running Up is a no-op: nothing left to narrow, nothing recorded twice.
	if _, err := pool.Exec(ctx, migrationUp(sqlText)); err != nil {
		t.Fatalf("re-apply 000292 up: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM person_module_access_leadership_tasks_assignee_narrowing WHERE tenant_id = $1::uuid`, tenant).Scan(&narrowed); err != nil {
		t.Fatal(err)
	}
	if narrowed != 3 {
		t.Fatalf("second up recorded %d rows, want 3", narrowed)
	}

	down := strings.TrimSpace(strings.SplitN(sqlText, "-- +goose Down", 2)[1])
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatalf("apply 000292 down: %v", err)
	}
	for name := range wantAssignable {
		if !oversee(name) {
			t.Fatalf("after down: %s should be assignable again", name)
		}
		var distinct bool
		if err := pool.QueryRow(ctx, `
SELECT public.goatos_text_array_is_distinct(capabilities) FROM person_module_access
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND surface = 'mobile' AND module_key = 'leadership_tasks'`,
			tenant, memberID[name]).Scan(&distinct); err != nil || !distinct {
			t.Fatalf("after down: %s capabilities not distinct (err=%v)", name, err)
		}
	}
}

// The static half: the migration must key leadership on the ROLE GRANT, name park heads
// alongside CXOs and directors, and narrow only rows 000285 touched.
func TestLeadershipTasksAssigneeNarrowingIsScopedToTheTwoWaySeed(t *testing.T) {
	raw, err := os.ReadFile("000292_leadership_tasks_assignees_leadership_only.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	for _, needle := range []string{
		"'ceo_internal'",
		"'park_head'",
		"'pc_director'",
		"'growth_director'",
		"'feed_director'",
		"'health_director'",
		"'procurement_director'",
		"'breeding_director'",
		"FROM public.person_module_access_leadership_tasks_two_way_rows",
		"FROM public.person_module_access_leadership_tasks_two_way_caps",
		"WHERE capability = 'oversee'",
		"array_remove(a.capabilities, 'oversee')",
		"AND a.surface = 'mobile'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("assignee narrowing migration missing %q", needle)
		}
	}
	if strings.Contains(sql, "'operator'") {
		t.Fatal("narrowing must be keyed on who IS leadership, never on an operator role list that a new field role would slip past")
	}
	if strings.Contains(sql, "DELETE FROM public.person_module_access") {
		t.Fatal("narrowing withdraws oversee only; it must not delete the module row")
	}
}
