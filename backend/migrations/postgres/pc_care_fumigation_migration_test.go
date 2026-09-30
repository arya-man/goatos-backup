package postgres

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// 000457 (maintainer instruction 2026-09-30): Fumigation. On a database that already carries the
// day-one pc_care.tasks version and people migrated onto per-person ticks, the Up path
//   - adds the seeded fumigation card IN PLACE to the stored version (and to nothing else),
//   - writes the planners' ticks for everyone already migrated, keyed on the role grant, leaving
//     an existing row exactly as it was and a person with no rows at all untouched,
//   - is a no-op when run again,
//
// and the Down path removes exactly what the Up wrote.
func TestFumigationMigrationAddsTheCardAndThePlannersTicks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec: %v\nsql: %s", err, sql)
		}
	}

	const tenant = "d0000000-0000-4000-8000-00000000f001"
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Fumigation', 'active') ON CONFLICT DO NOTHING`, tenant)

	// The day-one document for this tenant, exactly as 000386 writes it.
	raw386, err := os.ReadFile("000386_pc_care_sop.sql")
	if err != nil {
		t.Fatalf("read 000386: %v", err)
	}
	exec(migrationUp(string(raw386)))

	type person struct {
		name        string
		userID      string
		role        string
		migrated    bool   // has a person_access row (the cutover reached them)
		existingCap string // a pc_care mobile row already on the person, "" = none
	}
	people := []person{
		{"park-head", "d0000000-0000-4000-8000-00000000f101", "park_head", true, ""},
		{"park-head-already-doing", "d0000000-0000-4000-8000-00000000f102", "park_head", true, "do"},
		{"park-head-not-migrated", "d0000000-0000-4000-8000-00000000f103", "park_head", false, ""},
		{"health-director", "d0000000-0000-4000-8000-00000000f104", "health_director", true, ""},
		{"breeding-director", "d0000000-0000-4000-8000-00000000f105", "breeding_director", true, ""},
		{"pc-director", "d0000000-0000-4000-8000-00000000f106", "pc_director", true, ""},
	}
	member := map[string]string{}
	for i, p := range people {
		var id string
		if err := pool.QueryRow(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status)
VALUES ($1::uuid, $2::uuid, $3, $4, 'active') RETURNING workforce_member_id::text`,
			tenant, p.userID, "FUM"+string(rune('A'+i)), p.name).Scan(&id); err != nil {
			t.Fatalf("seed member %s: %v", p.name, err)
		}
		member[p.name] = id
		exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, $3, 'tenant', $1::uuid, 'active', now() - interval '1 day')`, tenant, p.userID, p.role)
		if p.migrated {
			exec(`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode) VALUES ($1::uuid, $2::uuid, 'tenant')`, tenant, id)
		}
		if p.existingCap != "" {
			exec(`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities)
VALUES ($1::uuid, $2::uuid, 'mobile', 'pc_care', ARRAY[$3]::text[])`, tenant, id, p.existingCap)
		}
	}

	// The tenant's root Consumables list, where Virufix is filed.
	exec(`INSERT INTO item_categories (tenant_id, name, normalized_name, item_kind, status) VALUES ($1::uuid, 'Consumables', 'consumables', 'consumable', 'active')`, tenant)

	raw, err := os.ReadFile("000457_pc_care_fumigation.sql")
	if err != nil {
		t.Fatalf("read 000457: %v", err)
	}
	up := migrationUp(string(raw))
	exec(up)

	caps := func(name, surface, module string) string {
		t.Helper()
		var got string
		err := pool.QueryRow(ctx, `
SELECT coalesce(array_to_string(capabilities, ','), '')
FROM person_module_access
WHERE tenant_id = $1::uuid AND workforce_member_id = $2::uuid AND surface = $3 AND module_key = $4`,
			tenant, member[name], surface, module).Scan(&got)
		if err != nil {
			return "<none>"
		}
		return got
	}
	assertCaps := func(stage, name, surface, module, want string) {
		t.Helper()
		if got := caps(name, surface, module); got != want {
			t.Fatalf("%s: %s %s/%s = %q, want %q", stage, name, surface, module, got, want)
		}
	}
	check := func(stage string) {
		t.Helper()
		// The card, in place on the stored version, and the version number unchanged.
		var version int
		var instruction, keys string
		if err := pool.QueryRow(ctx, `
SELECT v.version,
       v.form_dsl #>> '{pc_care,categories,fumigation,instruction}',
       (SELECT string_agg(p ->> 'key', ',' ORDER BY o)
          FROM jsonb_array_elements(v.form_dsl #> '{pc_care,categories,fumigation,proofs}') WITH ORDINALITY AS x(p, o))
FROM sop_versions v JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = 'pc_care.tasks'`, tenant).Scan(&version, &instruction, &keys); err != nil {
			t.Fatalf("%s: read pc_care.tasks: %v", stage, err)
		}
		if version != 1 || !strings.Contains(instruction, "5 ml") || keys != "mixing_video,spraying_video" {
			t.Fatalf("%s: v%d instruction=%q keys=%q, want v1 with the seeded card", stage, version, instruction, keys)
		}
		// The ticks.
		assertCaps(stage, "park-head", "mobile", "pc_fumigation", "view,configure")
		assertCaps(stage, "park-head", "mobile", "pc_care", "view")
		assertCaps(stage, "park-head", "web", "pc_fumigation", "<none>")        // park heads have no web bootstrap
		assertCaps(stage, "park-head-already-doing", "mobile", "pc_care", "do") // left exactly as it was
		assertCaps(stage, "park-head-already-doing", "mobile", "pc_fumigation", "view,configure")
		assertCaps(stage, "park-head-not-migrated", "mobile", "pc_fumigation", "<none>") // still on the role path
		assertCaps(stage, "health-director", "web", "pc_fumigation", "view,configure")
		assertCaps(stage, "health-director", "mobile", "pc_care", "view")
		assertCaps(stage, "breeding-director", "web", "pc_fumigation", "view,configure")
		assertCaps(stage, "pc-director", "mobile", "pc_fumigation", "<none>") // plans nothing
		// Virufix, once, in the Consumables list, in ml.
		var items int
		var unit, notes string
		if err := pool.QueryRow(ctx, `
SELECT count(*), min(i.base_unit), min(i.context ->> 'notes')
FROM inventory_items i JOIN item_categories c ON c.tenant_id = i.tenant_id AND c.category_id = i.category_id
WHERE i.tenant_id = $1::uuid AND i.name = 'Virufix' AND c.item_kind = 'consumable'`, tenant).Scan(&items, &unit, &notes); err != nil {
			t.Fatalf("%s: read Virufix: %v", stage, err)
		}
		if items != 1 || unit != "ml" || !strings.Contains(notes, "5 ml per litre") {
			t.Fatalf("%s: Virufix items=%d unit=%q notes=%q, want one ml item carrying the dosage", stage, items, unit, notes)
		}
	}
	check("after up")

	// The job default for a park head hired tomorrow.
	var defaults int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM designation_module_defaults WHERE designation_code = 'park_head' AND module_key = 'pc_fumigation'`).Scan(&defaults); err != nil || defaults != 1 {
		t.Fatalf("park_head default pc_fumigation rows = %d (err %v), want 1", defaults, err)
	}

	// A second run changes nothing.
	var before, after int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM person_module_access WHERE tenant_id = $1::uuid`, tenant).Scan(&before)
	exec(up)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM person_module_access WHERE tenant_id = $1::uuid`, tenant).Scan(&after)
	if before != after {
		t.Fatalf("re-running Up changed the tick count %d -> %d", before, after)
	}
	check("after a second up")

	// Down removes exactly what Up wrote.
	down := strings.SplitN(string(raw), "-- +goose Down", 2)[1]
	exec(down)
	assertCaps("after down", "park-head", "mobile", "pc_fumigation", "<none>")
	assertCaps("after down", "park-head", "mobile", "pc_care", "<none>")
	assertCaps("after down", "park-head-already-doing", "mobile", "pc_care", "do")
	var virufix int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM inventory_items WHERE tenant_id = $1::uuid AND name = 'Virufix'`, tenant).Scan(&virufix)
	if virufix != 0 {
		t.Fatalf("after down: %d Virufix items, want 0", virufix)
	}
	var hasCard bool
	if err := pool.QueryRow(ctx, `
SELECT v.form_dsl -> 'pc_care' -> 'categories' ? 'fumigation'
FROM sop_versions v JOIN sop_definitions d ON d.tenant_id = v.tenant_id AND d.sop_id = v.sop_id
WHERE v.tenant_id = $1::uuid AND d.code = 'pc_care.tasks'`, tenant).Scan(&hasCard); err != nil || hasCard {
		t.Fatalf("after down: fumigation card present=%v (err %v), want removed", hasCard, err)
	}
	// Re-apply Up so the shared template's schema is left as the migrations define it.
	exec(up)
}
