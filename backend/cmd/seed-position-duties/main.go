// Command seed-position-duties derives the position_module_duties rows
// (migration 000157: the normalized "what work does this position do" model,
// design doc S4.6) from the live workforce_positions catalog, so the duties
// dimension is REAL and reproducible instead of ad-hoc hand-seeded rows.
//
// Derivation (no invented data -- every row is a projection of an existing
// active workforce_positions seat):
//   - module_code comes from the position_code PREFIX (see modulePrefixes).
//     Backup slots usually cover the duty of whichever seat they stand in for,
//     but the reviewed CPT vaccination roster treats Backup Manager as one of
//     the three daily vaccination operators. Therefore backup_manager is an
//     explicit pc.vaccination execute duty; other backup slots are skipped.
//   - duty_type = 'execute' for surfaced vaccination operator positions even
//     when their HR/title tier is manager/head; that tier does not remove them
//     from drive execution. Other modules still use 'manage' for supervisory
//     tiers (manager | head | director | cxo), else 'execute'.
//   - capability_code is the execution permission a temporary backup grant for
//     that seat confers. Only pc.vaccination is a built + surfaced module today
//     (scope-lock), so only the preventive_care prefix carries
//     'vaccination.execute'; every other module's capability_code is NULL until
//     that module is built.
//
// It is idempotent: the unique index (tenant_id, position_code, module_code,
// duty_type, effective_from) plus INSERT ... ON CONFLICT DO NOTHING means a
// re-run inserts only rows that are missing and skips ones that already exist,
// without mutating or duplicating them. Tenant-scoped, batched, and gated to
// local/dev/test only (same guard as seed-roster-real). It refuses to run until
// both workforce_positions and position_module_duties exist. It reads only
// workforce_positions and writes only position_module_duties -- it never
// touches people, goats, protocols, sheds, or grants.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

// vaccinationExecuteCapability is the execution permission a temporary backup
// grant covering the preventive_care seat confers (design doc S4.6). It is the
// ONLY built-module capability today (scope-lock: pc.vaccination is the only
// built + surfaced module).
const vaccinationExecuteCapability = "vaccination.execute"

// modulePrefix maps a workforce_positions.position_code PREFIX to the operational
// module_code the seat works in, and the execution capability (if any) a backup
// grant for it confers. capability is "" for modules that are not built yet.
type modulePrefix struct {
	prefix     string
	moduleCode string
	capability string
}

// modulePrefixes is ordered longest-prefix-first so a more specific prefix
// (e.g. "preventive_care") always wins over a shorter accidental match.
var modulePrefixes = []modulePrefix{
	{prefix: "preventive_care", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "backup_manager", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "shed_manager", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "park_head", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "health_kidding", moduleCode: "health.kidding"},
	{prefix: "feeding", moduleCode: "feed.direction"},
	{prefix: "packaging", moduleCode: "packaging"},
	{prefix: "cleaning", moduleCode: "cleaning"},
	{prefix: "farming", moduleCode: "farming"},
	{prefix: "milk", moduleCode: "milk"},
}

// manageTiers is the set of position_tier values that MANAGE their module (as
// opposed to executing it). Matches workforce_positions_tier_check.
var manageTiers = map[string]bool{
	"manager":  true,
	"head":     true,
	"director": true,
	"cxo":      true,
}

// positionRow is one active seat read from workforce_positions.
type positionRow struct {
	positionCode string
	positionTier string
	isBackupSlot bool
}

// dutyRow is one derived position_module_duties row to insert.
type dutyRow struct {
	positionCode string
	moduleCode   string
	dutyType     string
	capability   string // "" => NULL
}

type stats struct {
	PositionCodesScanned int
	BackupSkipped        int
	UnmappedSkipped      int
	DutiesDerived        int
	DutiesInserted       int // rows newly inserted (existing skipped by ON CONFLICT)
	UnmappedPrefixes     map[string]int
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-position-duties", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	strict := fs.Bool("strict", false, "fail if any active position cannot be mapped to a built module duty")
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

	ready, missing, err := checkSchemaReady(ctx, pool)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("seed-position-duties requires schema that has not landed (missing: %s)", missing)
	}

	positions, err := loadDistinctActivePositions(ctx, pool, *tenantID)
	if err != nil {
		return fmt.Errorf("load positions: %w", err)
	}

	duties, st := deriveDuties(positions)

	fmt.Printf("derived position duties:\n"+
		"  position_codes_scanned=%d backup_skipped=%d unmapped_skipped=%d duties_derived=%d\n",
		st.PositionCodesScanned, st.BackupSkipped, st.UnmappedSkipped, st.DutiesDerived)
	if len(st.UnmappedPrefixes) > 0 {
		fmt.Printf("WARNING: %d position code(s) had no module mapping and were skipped (never guessed): %v\n",
			st.UnmappedSkipped, st.UnmappedPrefixes)
		if *strict {
			return fmt.Errorf("strict position duty seed rejected %d unmapped position code(s): %v", st.UnmappedSkipped, st.UnmappedPrefixes)
		}
	}

	inserted, err := insertDuties(ctx, pool, *tenantID, duties)
	if err != nil {
		return fmt.Errorf("insert duties: %w", err)
	}
	st.DutiesInserted = inserted

	fmt.Printf("seeded position duties:\n"+
		"  duties_inserted=%d duties_already_present=%d\n",
		st.DutiesInserted, st.DutiesDerived-st.DutiesInserted)
	return nil
}

// ---- schema readiness guard ----

func checkSchemaReady(ctx context.Context, pool *pgxpool.Pool) (bool, string, error) {
	var missing []string
	for _, table := range []string{"workforce_positions", "position_module_duties"} {
		var reg *string
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1)::text`, table).Scan(&reg); err != nil {
			return false, "", fmt.Errorf("check %s: %w", table, err)
		}
		if reg == nil {
			missing = append(missing, table+" table")
		}
	}
	if len(missing) > 0 {
		return false, strings.Join(missing, ", "), nil
	}
	return true, "", nil
}

// ---- read ----

// loadDistinctActivePositions returns the DISTINCT (position_code, tier, backup
// marker) of active seats for the tenant. DISTINCT because a position_code
// (e.g. feeding_am1) can be held at several centers -- its duty is the same
// regardless of scope, so duties are position-code-scoped, not per-seat.
func loadDistinctActivePositions(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]positionRow, error) {
	rows, err := pool.Query(ctx, `
SELECT DISTINCT position_code, position_tier, is_backup_slot
FROM workforce_positions
WHERE tenant_id = $1::uuid AND status = 'active'
ORDER BY position_code`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []positionRow
	for rows.Next() {
		var p positionRow
		if err := rows.Scan(&p.positionCode, &p.positionTier, &p.isBackupSlot); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- derivation ----

func deriveDuties(positions []positionRow) ([]dutyRow, stats) {
	st := stats{UnmappedPrefixes: map[string]int{}}
	var out []dutyRow
	for _, p := range positions {
		st.PositionCodesScanned++
		if p.isBackupSlot && p.positionCode != "backup_manager" {
			st.BackupSkipped++
			continue
		}
		mp, ok := matchModule(p.positionCode)
		if !ok {
			st.UnmappedSkipped++
			st.UnmappedPrefixes[p.positionCode]++
			continue
		}
		dutyType := "execute"
		if mp.moduleCode != "pc.vaccination" && manageTiers[p.positionTier] {
			dutyType = "manage"
		}
		out = append(out, dutyRow{
			positionCode: p.positionCode,
			moduleCode:   mp.moduleCode,
			dutyType:     dutyType,
			capability:   mp.capability,
		})
	}
	st.DutiesDerived = len(out)
	// Stable order for deterministic batching/logging.
	sort.Slice(out, func(i, j int) bool {
		if out[i].positionCode != out[j].positionCode {
			return out[i].positionCode < out[j].positionCode
		}
		return out[i].moduleCode < out[j].moduleCode
	})
	return out, st
}

// matchModule returns the module mapping for a position_code by longest matching
// prefix, or ok=false when no built module claims it (never guessed).
func matchModule(positionCode string) (modulePrefix, bool) {
	for _, mp := range modulePrefixes {
		if positionCode == mp.prefix || strings.HasPrefix(positionCode, mp.prefix+"_") {
			return mp, true
		}
	}
	return modulePrefix{}, false
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-position-duties", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-position-duties", env, databaseURL, "local", "dev", "test")
}

// ---- write ----

func insertDuties(ctx context.Context, pool *pgxpool.Pool, tenantID string, duties []dutyRow) (int, error) {
	if len(duties) == 0 {
		return 0, nil
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	inserted := 0
	const size = 200
	for start := 0; start < len(duties); start += size {
		end := start + size
		if end > len(duties) {
			end = len(duties)
		}
		b := &pgx.Batch{}
		for _, d := range duties[start:end] {
			var capability *string
			if d.capability != "" {
				c := d.capability
				capability = &c
			}
			// Idempotency is by SEMANTIC identity (tenant, position_code,
			// module_code, duty_type) among ACTIVE rows, NOT by the unique
			// index's (…, effective_from) tuple: a re-run mints a fresh
			// effective_from (column DEFAULT now()), so ON CONFLICT on that index
			// would never match and would DUPLICATE the duty. WHERE NOT EXISTS
			// skips any position_code/module/duty already live, inserting only
			// genuinely missing rows and leaving existing ones (and their
			// windows) untouched.
			b.Queue(`
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code)
SELECT $1::uuid, $2, $3, $4, $5
WHERE NOT EXISTS (
  SELECT 1 FROM position_module_duties
  WHERE tenant_id = $1::uuid AND position_code = $2 AND module_code = $3 AND duty_type = $4 AND status = 'active'
)`,
				tenantID, d.positionCode, d.moduleCode, d.dutyType, capability)
		}
		br := tx.SendBatch(ctx, b)
		var firstErr error
		for range duties[start:end] {
			tag, err := br.Exec()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			inserted += int(tag.RowsAffected())
		}
		if cerr := br.Close(); cerr != nil && firstErr == nil {
			firstErr = cerr
		}
		if firstErr != nil {
			return 0, firstErr
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return inserted, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
