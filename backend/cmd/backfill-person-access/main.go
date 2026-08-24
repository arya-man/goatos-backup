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

	for _, code := range codes {
		assignments, ok := permissions.AssignmentsForRole(code)
		if !ok {
			// A catalog row with no mapping would pre-fill NOTHING and read to whoever
			// picks it as "this designation grants no access", which is indistinguishable
			// from a deliberate empty. Fail loudly instead.
			return fmt.Errorf("designation %q has no assignment mapping; either map it in capability_backfill.go or retire the catalog row", code)
		}
		if dryRun {
			fmt.Printf("designation %-22s -> %d module rows\n", code, len(assignments))
			continue
		}
		if _, err := pool.Exec(ctx, `DELETE FROM designation_module_defaults WHERE designation_code = $1`, code); err != nil {
			return err
		}
		for _, a := range assignments {
			if _, err := pool.Exec(ctx,
				`INSERT INTO designation_module_defaults (designation_code, surface, module_key, capabilities)
				 VALUES ($1, $2, $3, $4)
				 ON CONFLICT (designation_code, surface, module_key)
				 DO UPDATE SET capabilities = EXCLUDED.capabilities`,
				code, a.Surface, a.Module, a.Capabilities); err != nil {
				return fmt.Errorf("designation %s/%s/%s: %w", code, a.Surface, a.Module, err)
			}
		}
	}
	return nil
}

type person struct {
	tenantID   string
	memberID   string
	name       string
	roles      []string
	parkIDs    []string
	tenantWide bool
}

func backfillPeople(ctx context.Context, pool *pgxpool.Pool, tenantID string, dryRun, overwrite bool) error {
	// One set-based read of every person and their active grants. A per-person query
	// inside the loop would be the N+1 this repo bans, and the roster is small enough
	// that one pass is also simply correct.
	const q = `
		SELECT m.tenant_id::text,
		       m.workforce_member_id::text,
		       m.display_name,
		       coalesce(array_agg(DISTINCT g.role) FILTER (WHERE g.role IS NOT NULL), '{}') AS roles,
		       coalesce(array_agg(DISTINCT g.scope_id::text) FILTER (WHERE g.scope_type = 'park' AND g.scope_id IS NOT NULL), '{}') AS park_ids,
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
		if err := rows.Scan(&p.tenantID, &p.memberID, &p.name, &p.roles, &p.parkIDs, &tenantWide); err != nil {
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
		assignments := permissions.AssignmentsForRoles(p.roles)
		if len(assignments) == 0 {
			return fmt.Errorf("person %s (%s) carries roles %v that map to NO modules; migrating them would remove all access",
				p.name, p.memberID, p.roles)
		}
		scopeMode := "parks"
		if p.tenantWide {
			scopeMode = "tenant"
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
	for _, a := range assignments {
		if len(a.Capabilities) == 0 {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at)
			 VALUES ($1, $2, $3, $4, $5, now())`,
			p.tenantID, p.memberID, a.Surface, a.Module, a.Capabilities); err != nil {
			return false, fmt.Errorf("%s/%s: %w", a.Surface, a.Module, err)
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
		for _, parkID := range p.parkIDs {
			if strings.TrimSpace(parkID) == "" {
				continue
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO person_park_scope (tenant_id, workforce_member_id, park_id)
				 VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
				p.tenantID, p.memberID, parkID); err != nil {
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
