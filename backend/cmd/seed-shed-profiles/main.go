// Command seed-shed-profiles derives the CONFIGURED operational profile of every shed into
// public.shed_profiles from canonical signals already in the database.
//
// shed_profiles is the AUTHORITATIVE source for the operational cohort a shed holds -- the shifting
// completion path (identity.resolveDestinationTag) reads the destination cohort from the active
// shed_profiles row joined through animal_stage_lookup, never from resident goats
// (domain-event-architecture shifting_completion_to_vaccination contract). A shed with no active
// profile fails a move closed, so this seed exists to give every shed a profile derived from truth:
//
//  1. OCCUPIED, homogeneous shed -> its single distinct resident management_stage.
//  2. EMPTY "<area> - Part N" shed -> the dominant cohort of its SIBLING parts in the same base
//     area (the rest of that park block), so a spare part inherits its block's purpose.
//  3. A shed named Q1/Q2/Q3 -> Quarantine.
//
// The derived cohort text is matched to animal_stage_lookup.stage_code to resolve animal_stage_id.
// A shed with no derivable signal (empty, no occupied siblings, not a Q block) is left WITHOUT a
// profile on purpose: it must be configured explicitly before animals can be shifted into it.
//
// Idempotent (upsert on the location_id primary key, row_version bumped on change), tenant-scoped,
// and gated to local/dev/test the same way the other seed commands are. It writes only
// shed_profiles.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

// deriveShedProfilesSQL upserts a profile for every shed with a derivable cohort. See the package doc
// for the three derivation rules. Read-model rebuild: safe to re-run.
//
// projection-review: membership=every shed location for the tenant (locations.location_type='shed'), with live goats (merged/exited excluded, non-blank management_stage) as the derivation signal; group_key=shed location_id (one shed_profiles row per shed) and base_area (name minus "- Part N") for sibling inheritance; join_cardinality=occupied is 1:1 per shed (GROUP BY shed_id HAVING count(DISTINCT stage)=1), area_cohort is one mode row per base_area, animal_stage_lookup is a strict 1:{0,1} stage_code->animal_stage_id lookup, and the final INSERT..SELECT is 1:1 per resolved shed so nothing fans out; pagination=none -- this is a one-shot bounded derivation over the ~150-shed config catalog run at seed/closeout, not a request path; scope=tenant_id on goats, locations, shed_profiles and animal_stage_lookup, and shed_profiles is upserted per its location_id primary key
const deriveShedProfilesSQL = `
WITH live AS (
  SELECT g.shed_id, g.management_stage
  FROM goats g
  WHERE g.tenant_id = $1::uuid AND g.merged_into_goat_id IS NULL AND g.exited_at IS NULL
    AND btrim(coalesce(g.management_stage,'')) <> ''
),
occupied AS (
  SELECT shed_id AS location_id, min(management_stage) AS stage
  FROM live GROUP BY shed_id HAVING count(DISTINCT management_stage) = 1
),
sheds AS (
  SELECT l.location_id, l.tenant_id, l.name,
         btrim(regexp_replace(l.name, '- Part [0-9]+$', '')) AS base_area
  FROM locations l WHERE l.location_type = 'shed' AND l.tenant_id = $1::uuid
),
area_cohort AS (
  SELECT s.base_area,
         (SELECT o2.stage FROM sheds s2 JOIN occupied o2 ON o2.location_id = s2.location_id
          WHERE s2.base_area = s.base_area
          GROUP BY o2.stage ORDER BY count(*) DESC, o2.stage LIMIT 1) AS stage
  FROM sheds s GROUP BY s.base_area
),
resolved AS (
  SELECT s.location_id, s.tenant_id,
         COALESCE(o.stage, ac.stage, CASE WHEN s.name ~ '^Q[0-9]+$' THEN 'Quarantine' END) AS stage
  FROM sheds s
  LEFT JOIN occupied o     ON o.location_id = s.location_id
  LEFT JOIN area_cohort ac ON ac.base_area = s.base_area
)
INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, row_version)
SELECT r.location_id, r.tenant_id, a.animal_stage_id, 1
FROM resolved r
JOIN animal_stage_lookup a ON a.tenant_id = r.tenant_id AND a.stage_code = r.stage AND a.status = 'active'
WHERE r.stage IS NOT NULL
ON CONFLICT (location_id) DO UPDATE
  SET animal_stage_id = EXCLUDED.animal_stage_id,
      row_version     = shed_profiles.row_version + CASE WHEN shed_profiles.animal_stage_id IS DISTINCT FROM EXCLUDED.animal_stage_id THEN 1 ELSE 0 END,
      updated_at      = now()`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-shed-profiles", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	if err := validateTarget(os.Getenv("GOATOS_ENV"), pgCfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	tag, err := pool.Exec(ctx, deriveShedProfilesSQL, *tenantID)
	if err != nil {
		return fmt.Errorf("derive shed_profiles: %w", err)
	}

	var totalSheds, withProfile int
	if err := pool.QueryRow(ctx,
		`SELECT (SELECT count(*) FROM locations WHERE location_type='shed' AND tenant_id=$1::uuid),
		        (SELECT count(*) FROM shed_profiles WHERE tenant_id=$1::uuid AND animal_stage_id IS NOT NULL)`,
		*tenantID).Scan(&totalSheds, &withProfile); err != nil {
		return fmt.Errorf("count coverage: %w", err)
	}
	fmt.Printf("seed-shed-profiles: upserted=%d sheds_total=%d sheds_with_profile=%d unconfigured=%d\n",
		tag.RowsAffected(), totalSheds, withProfile, totalSheds-withProfile)
	return nil
}

func validateTarget(env, databaseURL string) error {
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-shed-profiles", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-shed-profiles", env, databaseURL, "local", "dev", "test")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
