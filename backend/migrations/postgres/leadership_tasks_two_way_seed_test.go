package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestLeadershipTasksTwoWaySeedKeepsDirectorsRaisingAndEmployeesAssignable(t *testing.T) {
	raw, err := os.ReadFile("000285_leadership_tasks_two_way_seed.sql")
	if err != nil {
		t.Fatalf("read leadership tasks seed migration: %v", err)
	}
	sql := string(raw)

	for _, needle := range []string{
		"LEFT JOIN public.user_scope_grants g",
		"ARRAY['view','oversee']::text[]",
		"'web'::text AS surface, 'leadership_tasks'::text AS module_key",
		"ARRAY['view','oversee','configure']::text[]",
		"ARRAY['view','configure']::text[]",
		"ARRAY['do']::text[]",
		"ARRAY['configure']::text[]",
		"'pc_director'",
		"'growth_director'",
		"'feed_director'",
		"'health_director'",
		"'procurement_director'",
		"'breeding_director'",
	} {
		if !strings.Contains(sql, needle) {
			t.Fatalf("leadership tasks seed migration missing %q", needle)
		}
	}
	if strings.Contains(sql, "\n  JOIN public.user_scope_grants g\n") {
		t.Fatal("leadership tasks seed must not require a role grant before seeding assignable employees")
	}
	if !strings.Contains(sql, "COALESCE(bool_or(role IN (") {
		t.Fatal("leadership tasks seed must grant configure only to raise-capable cohorts")
	}
	if !strings.Contains(sql, "PRIMARY KEY (tenant_id, workforce_member_id, surface)") ||
		!strings.Contains(sql, "PRIMARY KEY (tenant_id, workforce_member_id, surface, capability)") {
		t.Fatal("leadership tasks seed rollback tracking must be surface-scoped")
	}
	if !strings.Contains(sql, "AND c.surface = a.surface") ||
		!strings.Contains(sql, "AND a.surface = r.surface") {
		t.Fatal("leadership tasks seed down migration must remove mobile and web rows by exact surface")
	}
	if strings.Count(sql, "RETURNING tenant_id, workforce_member_id, surface, (xmax = 0) AS inserted") != 2 {
		t.Fatal("leadership tasks seed must return surface from both upsert CTEs")
	}
	doStart := strings.Index(sql, "ARRAY['do']::text[]")
	configStart := strings.Index(sql, "ARRAY['configure']::text[]")
	if doStart == -1 || configStart == -1 || doStart > configStart {
		t.Fatal("leadership tasks seed must add do separately before configure")
	}
	doCaseStart := strings.LastIndex(sql[:doStart], "WHEN COALESCE(bool_or(role IN (")
	if doCaseStart == -1 {
		t.Fatal("leadership tasks seed must keep do in an explicit role-gated case")
	}
	doBlock := sql[doCaseStart:doStart]
	if strings.Contains(doBlock, "'ceo_internal'") {
		t.Fatal("leadership tasks seed must not grant CEO pen-visit do capability")
	}
	if !strings.Contains(doBlock, "'pc_director'") || !strings.Contains(doBlock, "'feed_director'") {
		t.Fatal("leadership tasks seed must grant director cohorts the do capability for pen visits")
	}
}
