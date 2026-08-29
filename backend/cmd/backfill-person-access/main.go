// Command backfill-person-access migrates every person from role-derived access to
// per-person module assignment (maintainer decision 2026-08-24).
//
// It is the safety-critical half of the cutover. For each active workforce member it
// reads their CURRENT active role grants, expands them through
// permissions.AssignmentsForRoles -- the mapping proved by capability_parity_test.go to
// lose no permission on any of the 47 registered roles -- and writes the result as their
// own rows. Their effective access the morning after release is the access they had the
// night before.
//
// It also fills designation_module_defaults from the same mapping, so picking "Feed
// Director" when adding a person pre-fills exactly what that job means today.
//
// Idempotent: re-running converges on the same rows. Safe to run before the API is
// switched over, because nothing reads these tables until it is.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

func main() {
	var tenantID string
	var dryRun bool
	var overwrite bool
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID to backfill; empty means every tenant")
	flag.BoolVar(&dryRun, "dry-run", false, "report what would be written and change nothing")
	// A person whose access has already been EDITED in the new screen must not be
	// silently reverted to what their retired role meant. Re-running the backfill after
	// go-live is the obvious way to do that by accident, so it is opt-in.
	flag.BoolVar(&overwrite, "overwrite-existing", false, "replace access for people who already have rows (destroys edits made since the first backfill)")
	flag.Parse()

	if err := run(context.Background(), tenantID, dryRun, overwrite); err != nil {
		fmt.Fprintf(os.Stderr, "backfill-person-access: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, tenantID string, dryRun, overwrite bool) error {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	if err := seedDesignationDefaults(ctx, pool, dryRun); err != nil {
		return fmt.Errorf("designation defaults: %w", err)
	}
	return backfillPeople(ctx, pool, tenantID, dryRun, overwrite)
}

// seedDesignationDefaults writes what each catalog designation pre-fills. The catalog
// codes ARE the retired flat role keys, so each designation's defaults are that role's
// proven assignment set -- there is no second mapping to keep in step.
func seedDesignationDefaults(ctx context.Context, pool *pgxpool.Pool, dryRun bool) error {
	rows, err := pool.Query(ctx, `SELECT designation_code FROM designation_catalog WHERE status = 'active' ORDER BY sort_order`)
	if err != nil {
		return err
	}
	codes := make([]string, 0, 16)
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return err
		}
		codes = append(codes, code)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Resolve every designation FIRST, then write the whole catalog in two statements.
	// Nothing is written until all of them resolve, so a designation with no mapping fails
	// the run before half the catalog has been replaced.
	all := make([]map[string]any, 0, len(codes)*24)
	for _, code := range codes {
		assignments, ok := permissions.AssignmentsForRole(code)
		if !ok {
			// A catalog row with no mapping would pre-fill NOTHING and read to whoever
			// picks it as "this designation grants no access", which is indistinguishable
			// from a deliberate empty. Fail loudly instead.
			return fmt.Errorf("designation %q has no assignment mapping; either map it in capability_backfill.go or retire the catalog row", code)
		}
		// A designation pre-fills the whole job, never a narrowed one: the retired lens
		// narrowing belongs to a PERSON, and stamping it on the title would hand the next
		// Procurement Director a workspace nobody chose for them.
		assignments = permissions.FillDefaultPages(assignments)
		if dryRun {
			fmt.Printf("designation %-22s -> %d module rows\n", code, len(assignments))
			continue
		}
		for _, a := range assignments {
			caps := a.Capabilities
			if caps == nil {
				caps = []string{}
			}
			all = append(all, map[string]any{
				"designation_code": code,
				"module_key":       a.Module,
				"surface":          a.Surface,
				"capabilities":     caps,
				"pages":            orEmptyPages(a.Pages),
			})
		}
	}
	if dryRun || len(all) == 0 {
		return nil
	}
	payload, err := json.Marshal(all)
	if err != nil {
		return err
	}
	// TWO statements for the WHOLE catalog, not two per designation. jsonb_to_recordset
	// rather than unnest over parallel arrays: capabilities and pages are each a SET, so
	// the arrays are arrays-of-arrays and unnest would FLATTEN them into scalars.
	if _, err := pool.Exec(ctx,
		`DELETE FROM designation_module_defaults WHERE designation_code = ANY($1::text[])`, codes); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
		 SELECT r.designation_code, r.surface, r.module_key,
		        ARRAY(SELECT jsonb_array_elements_text(r.capabilities)),
		        ARRAY(SELECT jsonb_array_elements_text(r.pages))
		   FROM jsonb_to_recordset($1::jsonb)
		     AS r(designation_code text, module_key text, surface text, capabilities jsonb, pages jsonb)
		 ON CONFLICT (designation_code, surface, module_key)
		 DO UPDATE SET capabilities = EXCLUDED.capabilities, pages = EXCLUDED.pages`,
		payload); err != nil {
		return fmt.Errorf("designation defaults: %w", err)
	}
	return nil
}

// assignmentPayload encodes module rows for a set-based insert.
//
// jsonb_to_recordset rather than parallel arrays with unnest: capabilities and pages are
// each a SET, so the arrays are arrays-of-arrays, and unnest FLATTENS a text[][] into
// scalars -- the insert then fails 42804 or, worse, writes the wrong shape. JSON also
// avoids inventing a delimiter for a value that is a set.
func assignmentPayload(assignments []permissions.ModuleAssignment) ([]byte, error) {
	rows := make([]map[string]any, 0, len(assignments))
	for _, a := range assignments {
		caps := a.Capabilities
		if caps == nil {
			caps = []string{}
		}
		rows = append(rows, map[string]any{
			"module_key":   a.Module,
			"surface":      a.Surface,
			"capabilities": caps,
			"pages":        orEmptyPages(a.Pages),
		})
	}
	return json.Marshal(rows)
}

// orEmptyPages keeps a nil slice out of the NOT NULL pages column. A mobile row has no
// pages by construction, and a web module with no admin-web screen of its own has none
// either; both store an empty list, which reads back as "every page" for a module that
// happens to have some.
func orEmptyPages(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

type person struct {
	tenantID string
	memberID string
	name     string
	roles    []string
	parkIDs  []string
	// deptModules is what this person's DEPARTMENT grants on the phone today. Empty means
	// their bar was never department-composed (leadership, a verifier, or no department).
	deptModules []string
	tenantWide  bool
}

func backfillPeople(ctx context.Context, pool *pgxpool.Pool, tenantID string, dryRun, overwrite bool) error {
	// One set-based read of every person and their active grants. A per-person query
	// inside the loop would be the N+1 this repo bans, and the roster is small enough
	// that one pass is also simply correct.
	//
	// projection-review: membership=workforce_members, one row per active person, PK
	// workforce_member_id; group_key=(tenant_id, workforce_member_id) -- display_name is
	// functionally dependent on that PK and is in GROUP BY only to stay selectable;
	// join_cardinality=user_scope_grants is MANY per person (one row per role x scope) and
	// every aggregate over it is DISTINCT or bool_or, so the fan-out collapses to SETS and
	// cannot inflate anything -- no COUNT/SUM ranges over the joined side and no ratio or
	// cap is compared; pagination=none, one pass with no LIMIT/OFFSET, so no page boundary
	// exists for a person to fall through and each is written exactly once; scope=explicit,
	// park scope comes from grants FILTERed to scope_type='park' while a single
	// tenant-scoped grant promotes the whole person, a widening that is never inferred from
	// an empty park list.
	const q = `
		SELECT m.tenant_id::text,
		       m.workforce_member_id::text,
		       m.display_name,
		       coalesce(array_agg(DISTINCT g.role) FILTER (WHERE g.role IS NOT NULL), '{}') AS roles,
		       coalesce(array_agg(DISTINCT g.scope_id::text) FILTER (WHERE g.scope_type = 'park' AND g.scope_id IS NOT NULL), '{}') AS park_ids,
	       coalesce((SELECT array_agg(DISTINCT dmg.module_key)
	                   FROM department_module_grants dmg
	                  WHERE dmg.tenant_id = m.tenant_id
	                    AND dmg.department_id = m.department_id
	                    AND dmg.status = 'active'), '{}') AS dept_modules,
		       bool_or(g.scope_type = 'tenant') AS tenant_wide
		FROM workforce_members m
		LEFT JOIN user_scope_grants g
		       ON g.user_id = m.user_id
		      AND g.tenant_id = m.tenant_id
		      AND g.status = 'active'
		WHERE m.status = 'active'
		  AND ($1 = '' OR m.tenant_id::text = $1)
		GROUP BY m.tenant_id, m.workforce_member_id, m.display_name
		ORDER BY m.display_name`

	rows, err := pool.Query(ctx, q, tenantID)
	if err != nil {
		return fmt.Errorf("read roster: %w", err)
	}
	people := make([]person, 0, 64)
	for rows.Next() {
		var p person
		var tenantWide *bool
		if err := rows.Scan(&p.tenantID, &p.memberID, &p.name, &p.roles, &p.parkIDs, &p.deptModules, &tenantWide); err != nil {
			rows.Close()
			return err
		}
		p.tenantWide = tenantWide != nil && *tenantWide
		people = append(people, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var written, skipped, noGrants int
	for _, p := range people {
		if len(p.roles) == 0 {
			// A roster row with no active grant cannot log in today either. Writing an
			// empty access header would make them look deliberately set up with nothing;
			// leaving them absent keeps "never migrated" visible.
			noGrants++
			continue
		}
		assignments, scopeMode := shapePerson(p)
		if len(assignments) == 0 {
			return fmt.Errorf("person %s (%s) carries roles %v that map to NO modules; migrating them would remove all access",
				p.name, p.memberID, p.roles)
		}
		if dryRun {
			fmt.Printf("%-22s %-8s roles=%-52s modules=%d parks=%d\n",
				truncate(p.name, 22), scopeMode, strings.Join(sorted(p.roles), ","), len(assignments), len(p.parkIDs))
			continue
		}
		did, err := writePerson(ctx, pool, p, assignments, scopeMode, overwrite)
		if err != nil {
			return fmt.Errorf("person %s (%s): %w", p.name, p.memberID, err)
		}
		if did {
			written++
		} else {
			skipped++
		}
	}

	fmt.Printf("\npeople=%d written=%d skipped-existing=%d no-active-grant=%d\n",
		len(people), written, skipped, noGrants)
	if skipped > 0 && !overwrite {
		fmt.Printf("(%d already had access rows and were left alone; pass -overwrite-existing to replace them)\n", skipped)
	}
	return nil
}

// shapePerson turns one roster row into the access rows to write.
//
// projection-review: membership=workforce_members (one row per active person, PK
// workforce_member_id); group_key=(tenant_id, workforce_member_id) -- display_name is
// functionally dependent on that PK and rides along in GROUP BY only to be selectable;
// join_cardinality=user_scope_grants is MANY per person (one row per role x scope), and
// every aggregate over it is DISTINCT or bool_or, so the fan-out collapses to SETS and can
// never inflate a count -- no COUNT/SUM ranges over the joined side, and no ratio or cap is
// compared; pagination=none, the roster is read in ONE set-based pass with no LIMIT/OFFSET,
// so there is no page boundary a person can fall through and each person is written exactly
// once; scope=explicit -- park scope comes from grants FILTERed to scope_type='park' and a
// single tenant-scoped grant promotes the whole person to 'tenant', which is a widening that
// must not be inferred from an empty park list.
//
// The multiplicity that actually bites is one layer up and lives HERE, not in the SQL: a
// stacked person (STG has one wearing five roles) expands to many module rows that overlap,
// and mergeAssignments must union them into ONE row per (module, surface). Duplicated rows
// would be written twice and the second would win silently.
func shapePerson(p person) ([]permissions.ModuleAssignment, string) {
	// The retired admin-web lenses become this person's own ticks here, once (maintainer
	// decision 2026-08-27). FillDefaultPages then stamps the full page list on every web row
	// that was not narrowed, so the editor opens showing what the person can actually reach
	// rather than an empty grid.
	// Everything this person's roles grant, with the retired admin-web lens applied as ticks.
	//
	// The phone rows are NOT trimmed to their department. An earlier version did, to keep the
	// bar identical, and a cutover simulation against the real STG roster showed what it cost:
	// 177 permissions removed across 20 operators, `goat.read` among them -- the read a scan
	// lookup depends on. The bar is still offered from the department; what the ticks decide
	// is whether an offered module has anything this person may open. So the rows carry the
	// permissions, unchanged, and nobody loses an action.
	assignments := permissions.FillDefaultPages(
		permissions.NarrowForRetiredLenses(p.roles, permissions.AssignmentsForRoles(p.roles)),
	)
	scopeMode := "parks"
	if p.tenantWide {
		scopeMode = "tenant"
	}
	return assignments, scopeMode
}

// writePerson replaces one person's access in a single transaction. Reports false when
// the person already had rows and overwrite was not requested.
func writePerson(ctx context.Context, pool *pgxpool.Pool, p person, assignments []permissions.ModuleAssignment, scopeMode string, overwrite bool) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if !overwrite {
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM person_access WHERE tenant_id = $1 AND workforce_member_id = $2)`,
			p.tenantID, p.memberID).Scan(&exists); err != nil {
			return false, err
		}
		if exists {
			return false, nil
		}
	}

	// The designation records what the access STARTED as. A stacked person has no single
	// title, so it is left NULL rather than picking one of their roles arbitrarily -- an
	// arbitrary pick would read to the next person as a deliberate statement about the job.
	var designation *string
	if len(p.roles) == 1 {
		var known bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM designation_catalog WHERE designation_code = $1)`,
			p.roles[0]).Scan(&known); err != nil {
			return false, err
		}
		if known {
			role := p.roles[0]
			designation = &role
		}
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO person_access (tenant_id, workforce_member_id, scope_mode, designation_code, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (tenant_id, workforce_member_id)
		 DO UPDATE SET scope_mode = EXCLUDED.scope_mode,
		               designation_code = EXCLUDED.designation_code,
		               updated_at = now(),
		               row_version = person_access.row_version + 1`,
		p.tenantID, p.memberID, scopeMode, designation); err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM person_module_access WHERE tenant_id = $1 AND workforce_member_id = $2`,
		p.tenantID, p.memberID); err != nil {
		return false, err
	}
	held := make([]permissions.ModuleAssignment, 0, len(assignments))
	for _, a := range assignments {
		if len(a.Capabilities) == 0 {
			continue
		}
		held = append(held, a)
	}
	if len(held) > 0 {
		payload, err := assignmentPayload(held)
		if err != nil {
			return false, err
		}
		// ONE set-based insert per person, the same shape the live save path uses.
		if _, err := tx.Exec(ctx,
			`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, pages, updated_at)
			 SELECT $1::uuid, $2::uuid, r.surface, r.module_key,
			        ARRAY(SELECT jsonb_array_elements_text(r.capabilities)),
			        ARRAY(SELECT jsonb_array_elements_text(r.pages)),
			        now()
			   FROM jsonb_to_recordset($3::jsonb) AS r(module_key text, surface text, capabilities jsonb, pages jsonb)`,
			p.tenantID, p.memberID, payload); err != nil {
			return false, err
		}
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM person_park_scope WHERE tenant_id = $1 AND workforce_member_id = $2`,
		p.tenantID, p.memberID); err != nil {
		return false, err
	}
	// A tenant-wide person needs no park rows; listing them would go stale the moment a
	// park is added and quietly narrow someone who is supposed to see everything.
	if scopeMode == "parks" {
		parkIDs := make([]string, 0, len(p.parkIDs))
		for _, parkID := range p.parkIDs {
			if strings.TrimSpace(parkID) != "" {
				parkIDs = append(parkIDs, parkID)
			}
		}
		if len(parkIDs) > 0 {
			// ONE set-based insert, not one per park.
			if _, err := tx.Exec(ctx,
				`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id)
				 SELECT $1::uuid, $2::uuid, unnest($3::uuid[])
				 ON CONFLICT DO NOTHING`,
				p.tenantID, p.memberID, parkIDs); err != nil {
				return false, err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func sorted(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
