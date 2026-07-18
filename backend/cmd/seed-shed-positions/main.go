// Command seed-shed-positions materializes shed-scoped operational manager seats
// (position_code='shed_manager', scope_type='shed', scope_id=<shed location id>)
// so the shed-wise vaccination read model can attach a Manager to each shed.
//
// SOURCE OF TRUTH = an explicit shed->manager mapping CSV plus the reviewed
// park preventive_care_manager seat. The CSV wins when present; any active shed
// absent from the CSV inherits its park's preventive_care_manager. The
// vaccination UI must never show "Manager: unassigned" merely because a source
// refresh added sheds faster than the shed-level mapping CSV was regenerated.
//
// There is no reviewed shed->manager source yet, so -generate-provisional writes
// a PROVISIONAL mapping CSV for all active sheds by round-robin across each park's
// active, NON-backup manager-tier holders (the Backup Manager slot is excluded --
// it covers a manager on leave, it does not own sheds as primary). Round-robin
// lives ONLY here, in this data-fill step that produces a reviewable artifact --
// it is never part of the seed's runtime apply path. Every generated row is
// tagged assignment_source=provisional_seed, confidence=provisional,
// needs_review=true so it is visibly traceable and replaceable by reviewed data.
// -generate-provisional writes NO database rows.
//
// Flow: `-generate-provisional shed_manager_mapping.csv` (fill + review), then
// `-mapping shed_manager_mapping.csv` (apply). -strict exits non-zero if any
// active shed is still unassigned after the park-manager fallback, provisionally
// assigned, or missing backup coverage after apply -- use it as a production
// preflight gate so real data is never accepted until every active shed has a
// manager and backup.
//
// Idempotent (deterministic v5 position UUID per shed + ON CONFLICT DO UPDATE),
// batched, tenant-scoped, gated to local/dev/test only (localtarget guard). It
// refuses to run until the shed-scope migration (000153) has landed.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTenantID = "00000000-0000-4000-8000-000000000001"

	// shedManagerPositionCode matches workforce/app.ShedManagerPositionCode --
	// the position_code ShedManager reads for a shed's operational manager seat.
	shedManagerPositionCode = "shed_manager"
	// managerBackupGroupCode matches workforce/app.ManagerBackupGroupCode -- the
	// backup group whose is_backup_slot seat covers manager-tier work. Every seeded
	// shed_manager seat carries it so its coverage resolves to the park Backup Manager.
	managerBackupGroupCode = "manager_backup"

	// provisional provenance stamped on every -generate-provisional row.
	provisionalSource     = "provisional_seed"
	provisionalSourceRef  = "no reviewed shed-vaccination-manager source exists yet"
	provisionalConfidence = "provisional"
)

// mappingCSVHeader is the canonical column order -generate-provisional writes and
// -mapping reads (extra columns are ignored; shed_code + manager_code are required).
var mappingCSVHeader = []string{
	"shed_code", "shed_name", "park_code", "manager_code", "manager_name",
	"assignment_source", "source_ref", "confidence", "needs_review",
}

// mappingEntry is one resolved assignment plus whether it is provisional or reviewed.
type mappingEntry struct {
	memberID    string
	provisional bool
}

type stats struct {
	ActiveSheds       int
	ShedsWithPark     int
	OrphanSheds       int
	ProvisionalSeats  int
	ReviewedSeats     int
	ParkFallbackSeats int
	UnmappedGaps      int
	BackupGaps        int
	SeatsInserted     int
	DistinctHolders   int
	StrictBlocked     bool // strict preflight refused to write (manager gaps, backup gaps, or provisional present)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("seed-shed-positions", flag.ContinueOnError)
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID", defaultTenantID), "tenant id")
	timeout := fs.Duration("timeout", 120*time.Second, "seed timeout")
	genProvisional := fs.String("generate-provisional", "",
		"write a PROVISIONAL shed->manager mapping CSV to this path (round-robin across each park's non-backup managers; every row tagged provisional_seed/needs_review). Writes NO database rows. Review it, then apply with -mapping.")
	mappingPath := fs.String("mapping", "",
		"CSV of shed->manager assignments to APPLY (header must include shed_code,manager_code; resolved via locations.location_code / workforce_members.display_code). CSV rows override; unlisted sheds inherit the park preventive_care_manager.")
	strict := fs.Bool("strict", false,
		"exit non-zero if any active shed is left unassigned after park preventive-care-manager fallback, assigned only provisionally, or missing backup coverage after applying -mapping. Use as a production preflight gate: production requires every active shed to resolve manager and backup.")
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
		fmt.Printf("seed built, waiting on shed-scope migration to land (missing: %s) -- no database writes were made.\n", missing)
		return nil
	}

	if *genProvisional != "" {
		return generateProvisional(ctx, pool, *tenantID, *genProvisional)
	}

	if *mappingPath == "" {
		fmt.Println("nothing to do: pass -generate-provisional <file> to author a provisional mapping, then -mapping <file> to apply it. No database rows were written.")
		return nil
	}

	st, err := applyMapping(ctx, pool, *tenantID, *mappingPath, *strict)
	if err != nil {
		return err
	}

	fmt.Printf("shed manager seats applied from %s:\n"+
		"  active_sheds=%d sheds_with_park=%d orphan_sheds=%d\n"+
		"  provisional_seats=%d reviewed_seats=%d park_fallback_seats=%d unmapped_gaps=%d\n"+
		"  backup_gaps=%d seats_inserted=%d distinct_holders=%d\n",
		*mappingPath, st.ActiveSheds, st.ShedsWithPark, st.OrphanSheds,
		st.ProvisionalSeats, st.ReviewedSeats, st.ParkFallbackSeats, st.UnmappedGaps,
		st.BackupGaps, st.SeatsInserted, st.DistinctHolders)
	fmt.Printf("PREFLIGHT: %d/%d active sheds assigned via provisional seed; %d reviewed shed mappings; %d park-manager fallback mappings; %d manager gaps; %d backup gaps.\n",
		st.ProvisionalSeats, st.ActiveSheds, st.ReviewedSeats, st.ParkFallbackSeats, st.UnmappedGaps, st.BackupGaps)
	if st.ProvisionalSeats > 0 {
		fmt.Printf("WARNING: %d assignment(s) are PROVISIONAL (needs_review=true), NOT reviewed business truth. "+
			"Replace with a reviewed shed->manager source and re-apply.\n", st.ProvisionalSeats)
	}
	if st.UnmappedGaps > 0 {
		fmt.Printf("GAP: %d active shed(s) still have NO manager assignment.\n", st.UnmappedGaps)
	}
	if st.BackupGaps > 0 {
		fmt.Printf("GAP: %d active shed(s) still have NO shed or center Backup Manager coverage.\n", st.BackupGaps)
	}
	if st.StrictBlocked {
		return fmt.Errorf("strict preflight FAILED (wrote 0 rows): %d unassigned + %d provisional (unreviewed) + %d missing-backup shed assignment(s) -- production requires every active shed to resolve a manager and Backup Manager coverage",
			st.UnmappedGaps, st.ProvisionalSeats, st.BackupGaps)
	}
	return nil
}

// checkSchemaReady verifies the workforce_positions table exists and its scope
// check constraint allows the 'shed' scope (migration 000153).
func checkSchemaReady(ctx context.Context, pool *pgxpool.Pool) (bool, string, error) {
	var missing []string

	var positionsTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.workforce_positions')::text`).Scan(&positionsTable); err != nil {
		return false, "", fmt.Errorf("check workforce_positions: %w", err)
	}
	if positionsTable == nil {
		missing = append(missing, "workforce_positions table")
		return false, strings.Join(missing, ", "), nil
	}

	var scopeDef *string
	if err := pool.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'workforce_positions_scope_check'`).Scan(&scopeDef); err != nil {
		if err == pgx.ErrNoRows {
			missing = append(missing, "workforce_positions_scope_check constraint")
			return false, strings.Join(missing, ", "), nil
		}
		return false, "", fmt.Errorf("check scope constraint: %w", err)
	}
	if scopeDef == nil || !strings.Contains(*scopeDef, "'shed'") {
		missing = append(missing, "shed scope allowed by workforce_positions_scope_check (migration 000153)")
	}

	if len(missing) > 0 {
		return false, strings.Join(missing, ", "), nil
	}
	return true, "", nil
}

// shedRow is an active shed with its park, for CSV generation.
type shedRow struct {
	shedID   string
	shedCode string
	shedName string
	parkID   string
	parkCode string
}

// managerRow is one candidate manager for a park, for CSV generation.
type managerRow struct {
	memberID   string
	memberCode string
	memberName string
}

// generateProvisional writes a provisional shed->manager mapping CSV. Round-robin
// (the one-and-only place it lives) fills each shed from its park's non-backup
// manager pool; every row is stamped provisional_seed / needs_review. No DB writes.
func generateProvisional(ctx context.Context, pool *pgxpool.Pool, tenantID, outPath string) error {
	sheds, err := loadShedsWithPark(ctx, pool, tenantID)
	if err != nil {
		return err
	}
	mgrByPark, err := loadManagerPool(ctx, pool, tenantID)
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(mappingCSVHeader); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	idx := map[string]int{}
	var filled, gaps int
	for _, s := range sheds {
		mgrs := mgrByPark[s.parkID]
		if len(mgrs) == 0 {
			// no manager to derive -- write the shed with an empty owner so the gap is visible.
			if err := w.Write([]string{
				s.shedCode, s.shedName, s.parkCode, "", "",
				"unassigned_no_manager", "park has no non-backup manager-tier holder", provisionalConfidence, "true",
			}); err != nil {
				return fmt.Errorf("write gap row: %w", err)
			}
			gaps++
			continue
		}
		m := mgrs[idx[s.parkID]%len(mgrs)]
		idx[s.parkID]++
		if err := w.Write([]string{
			s.shedCode, s.shedName, s.parkCode, m.memberCode, m.memberName,
			provisionalSource, provisionalSourceRef, provisionalConfidence, "true",
		}); err != nil {
			return fmt.Errorf("write row: %w", err)
		}
		filled++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush %s: %w", outPath, err)
	}

	fmt.Printf("wrote provisional mapping %s: %d shed(s), %d filled (provisional_seed, needs_review), %d gap(s) with no park manager.\n",
		outPath, len(sheds), filled, gaps)
	fmt.Println("NOTE: every filled row is PROVISIONAL (round-robin fill, not reviewed truth). Review/replace manager_code, set assignment_source=reviewed for confirmed rows, then apply with -mapping.")
	return nil
}

// applyMapping reads the mapping CSV and writes shed_manager seats for its rows.
// It invents nothing: only rows with a resolvable shed + manager become seats.
// Under strict, it is a true PREFLIGHT: if any active shed would be unassigned or
// only provisionally assigned, it writes NOTHING and reports StrictBlocked.
func applyMapping(ctx context.Context, pool *pgxpool.Pool, tenantID, path string, strict bool) (stats, error) {
	var st stats
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM locations
		WHERE tenant_id = $1::uuid AND location_type = 'shed' AND status = 'active'`, tenantID).Scan(&st.ActiveSheds); err != nil {
		return st, fmt.Errorf("count active sheds: %w", err)
	}
	sheds, err := loadShedsWithPark(ctx, pool, tenantID)
	if err != nil {
		return st, err
	}
	st.ShedsWithPark = len(sheds)
	st.OrphanSheds = st.ActiveSheds - st.ShedsWithPark

	mapping, err := loadMapping(ctx, pool, tenantID, path)
	if err != nil {
		return st, err
	}
	fallbackByPark, err := loadVaccinationManagerPool(ctx, pool, tenantID)
	if err != nil {
		return st, err
	}

	type seat struct {
		shedID, memberID string
	}
	var seats []seat
	holders := map[string]struct{}{}
	fallbackIdx := map[string]int{}
	for _, s := range sheds {
		entry, ok := mapping[s.shedID]
		if !ok {
			mgrs := fallbackByPark[s.parkID]
			if len(mgrs) == 0 {
				continue // real gap: no reviewed park manager to inherit.
			}
			mgr := mgrs[fallbackIdx[s.parkID]%len(mgrs)]
			fallbackIdx[s.parkID]++
			seats = append(seats, seat{s.shedID, mgr.memberID})
			holders[mgr.memberID] = struct{}{}
			st.ParkFallbackSeats++
			continue
		}
		seats = append(seats, seat{s.shedID, entry.memberID})
		holders[entry.memberID] = struct{}{}
		if entry.provisional {
			st.ProvisionalSeats++
		} else {
			st.ReviewedSeats++
		}
	}
	st.UnmappedGaps = st.ActiveSheds - len(seats)
	st.DistinctHolders = len(holders)
	st.BackupGaps, err = backupCoverageGaps(ctx, pool, tenantID)
	if err != nil {
		return st, err
	}

	// Strict preflight: refuse to write when any shed is unassigned, provisional, or lacks backup coverage.
	if strict && (st.UnmappedGaps > 0 || st.ProvisionalSeats > 0 || st.BackupGaps > 0) {
		st.StrictBlocked = true
		return st, nil
	}

	if len(seats) == 0 {
		return st, nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return st, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := batch(ctx, tx, seats, 200, func(b *pgx.Batch, s seat) {
		posID := detUUID("shed_manager_position", tenantID, s.shedID)
		b.Queue(`
			INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id,
				position_code, position_tier, is_backup_slot, backup_group_code, status, valid_from, updated_at)
			VALUES ($1,$2,$3,'shed',$4,$5,'manager',false,$6,'active',now(),now())
			ON CONFLICT (position_id) DO UPDATE SET
				workforce_member_id = EXCLUDED.workforce_member_id,
				scope_type = EXCLUDED.scope_type,
				scope_id = EXCLUDED.scope_id,
				position_code = EXCLUDED.position_code,
				position_tier = EXCLUDED.position_tier,
				is_backup_slot = EXCLUDED.is_backup_slot,
				backup_group_code = EXCLUDED.backup_group_code,
				status = 'active',
				valid_to = NULL,
				updated_at = now()`,
			posID, tenantID, s.memberID, s.shedID, shedManagerPositionCode, managerBackupGroupCode)
	}); err != nil {
		return st, fmt.Errorf("insert shed seats: %w", err)
	}
	st.SeatsInserted = len(seats)

	if err := tx.Commit(ctx); err != nil {
		return st, fmt.Errorf("commit: %w", err)
	}
	return st, nil
}

func backupCoverageGaps(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int, error) {
	var gaps int
	err := pool.QueryRow(ctx, `
		WITH active_sheds AS (
			SELECT s.location_id AS shed_id, p.location_id AS center_id
			FROM locations s
			LEFT JOIN locations p
			  ON p.tenant_id = s.tenant_id
			 AND p.location_id = s.parent_location_id
			 AND p.status = 'active'
			WHERE s.tenant_id = $1::uuid
			  AND s.location_type = 'shed'
			  AND s.status = 'active'
		)
		SELECT count(*)::int
		FROM active_sheds s
		WHERE NOT EXISTS (
			SELECT 1
			FROM workforce_positions wp
			WHERE wp.tenant_id = $1::uuid
			  AND wp.scope_type = 'shed'
			  AND wp.scope_id = s.shed_id
			  AND wp.backup_group_code = $2
			  AND wp.is_backup_slot = true
			  AND wp.status = 'active'
			  AND wp.valid_from <= now()
			  AND (wp.valid_to IS NULL OR wp.valid_to > now())
		)
		  AND NOT EXISTS (
			SELECT 1
			FROM workforce_positions wp
			WHERE wp.tenant_id = $1::uuid
			  AND wp.scope_type = 'center'
			  AND wp.scope_id = s.center_id
			  AND wp.backup_group_code = $2
			  AND wp.is_backup_slot = true
			  AND wp.status = 'active'
			  AND wp.valid_from <= now()
			  AND (wp.valid_to IS NULL OR wp.valid_to > now())
		)`, tenantID, managerBackupGroupCode).Scan(&gaps)
	if err != nil {
		return 0, fmt.Errorf("check backup coverage: %w", err)
	}
	return gaps, nil
}

func loadShedsWithPark(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]shedRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT s.location_id::text, s.location_code, s.name, p.location_id::text, p.location_code
		FROM locations s
		JOIN locations p
		  ON p.tenant_id = s.tenant_id AND p.location_id = s.parent_location_id
		 AND p.location_type = 'park' AND p.status = 'active'
		WHERE s.tenant_id = $1::uuid AND s.location_type = 'shed' AND s.status = 'active'
		ORDER BY p.location_code, s.location_code`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query sheds: %w", err)
	}
	defer rows.Close()
	var out []shedRow
	for rows.Next() {
		var s shedRow
		if err := rows.Scan(&s.shedID, &s.shedCode, &s.shedName, &s.parkID, &s.parkCode); err != nil {
			return nil, fmt.Errorf("scan shed: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// loadManagerPool returns, per park, the active, center-scoped, manager-tier,
// NON-backup-slot holders (deduped per park). The Backup Manager slot is excluded
// because it covers a manager on leave rather than owning sheds as primary.
func loadManagerPool(ctx context.Context, pool *pgxpool.Pool, tenantID string) (map[string][]managerRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT wp.scope_id::text, m.workforce_member_id::text, coalesce(m.display_code, ''), coalesce(m.display_name, '')
		FROM workforce_positions wp
		JOIN workforce_members m ON m.workforce_member_id = wp.workforce_member_id
		WHERE wp.tenant_id = $1::uuid AND wp.scope_type = 'center' AND wp.status = 'active'
		  AND wp.position_tier = 'manager' AND wp.is_backup_slot = false
		ORDER BY wp.scope_id, wp.position_code, m.workforce_member_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query manager pool: %w", err)
	}
	defer rows.Close()
	poolByPark := map[string][]managerRow{}
	seen := map[string]map[string]struct{}{}
	for rows.Next() {
		var parkID string
		var mr managerRow
		if err := rows.Scan(&parkID, &mr.memberID, &mr.memberCode, &mr.memberName); err != nil {
			return nil, fmt.Errorf("scan manager: %w", err)
		}
		if seen[parkID] == nil {
			seen[parkID] = map[string]struct{}{}
		}
		if _, dup := seen[parkID][mr.memberID]; dup {
			continue
		}
		seen[parkID][mr.memberID] = struct{}{}
		poolByPark[parkID] = append(poolByPark[parkID], mr)
	}
	return poolByPark, rows.Err()
}

// loadVaccinationManagerPool returns the deterministic park manager fallback
// used when the shed-level mapping CSV is partial. Only the explicit
// preventive_care_manager seat may be inherited; other functional managers are
// not vaccination owners.
func loadVaccinationManagerPool(ctx context.Context, pool *pgxpool.Pool, tenantID string) (map[string][]managerRow, error) {
	rows, err := pool.Query(ctx, `
		SELECT wp.scope_id::text, m.workforce_member_id::text, coalesce(m.display_code, ''), coalesce(m.display_name, '')
		FROM workforce_positions wp
		JOIN workforce_members m
		  ON m.tenant_id = wp.tenant_id
		 AND m.workforce_member_id = wp.workforce_member_id
		WHERE wp.tenant_id = $1::uuid
		  AND wp.scope_type = 'center'
		  AND wp.status = 'active'
		  AND wp.position_tier = 'manager'
		  AND wp.position_code = 'preventive_care_manager'
		  AND wp.is_backup_slot = false
		  AND wp.valid_from <= now()
		  AND (wp.valid_to IS NULL OR wp.valid_to > now())
		ORDER BY wp.scope_id, m.workforce_member_id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query vaccination manager pool: %w", err)
	}
	defer rows.Close()

	poolByPark := map[string][]managerRow{}
	seen := map[string]map[string]struct{}{}
	for rows.Next() {
		var parkID string
		var mr managerRow
		if err := rows.Scan(&parkID, &mr.memberID, &mr.memberCode, &mr.memberName); err != nil {
			return nil, fmt.Errorf("scan vaccination manager: %w", err)
		}
		if seen[parkID] == nil {
			seen[parkID] = map[string]struct{}{}
		}
		if _, dup := seen[parkID][mr.memberID]; dup {
			continue
		}
		seen[parkID][mr.memberID] = struct{}{}
		poolByPark[parkID] = append(poolByPark[parkID], mr)
	}
	return poolByPark, rows.Err()
}

// loadMapping reads the mapping CSV and resolves it to shed_id -> mappingEntry.
// Required header columns: shed_code, manager_code. Optional: assignment_source
// (rows whose value != provisional_seed count as reviewed). Rows with an empty
// manager_code are skipped (declared gaps). FAILS LOUD on any unknown code.
func loadMapping(ctx context.Context, pool *pgxpool.Pool, tenantID, path string) (map[string]mappingEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open mapping %s: %w", path, err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read mapping header from %s: %w", path, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	shedCol, okS := col["shed_code"]
	mgrCol, okM := col["manager_code"]
	if !okS || !okM {
		return nil, fmt.Errorf("mapping %s must have header columns shed_code,manager_code (got %v)", path, header)
	}
	srcCol, hasSrc := col["assignment_source"]

	shedCodeToID, err := loadCodeMap(ctx, pool, tenantID,
		`SELECT upper(location_code), location_id::text FROM locations WHERE tenant_id = $1::uuid AND location_type = 'shed' AND location_code IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("load shed codes: %w", err)
	}
	memberCodeToID, err := loadCodeMap(ctx, pool, tenantID,
		`SELECT upper(display_code), workforce_member_id::text FROM workforce_members WHERE tenant_id = $1::uuid AND display_code IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("load member codes: %w", err)
	}

	mapping := map[string]mappingEntry{}
	var unknownSheds, unknownMgrs []string
	rowNo := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read mapping %s row %d: %w", path, rowNo, err)
		}
		rowNo++
		if shedCol >= len(rec) || mgrCol >= len(rec) {
			return nil, fmt.Errorf("mapping %s row %d has too few columns", path, rowNo)
		}
		shedCode := strings.ToUpper(strings.TrimSpace(rec[shedCol]))
		mgrCode := strings.ToUpper(strings.TrimSpace(rec[mgrCol]))
		if shedCode == "" {
			continue
		}
		if mgrCode == "" {
			continue // declared gap row
		}
		shedID, ok := shedCodeToID[shedCode]
		if !ok {
			unknownSheds = append(unknownSheds, shedCode)
			continue
		}
		memberID, ok := memberCodeToID[mgrCode]
		if !ok {
			unknownMgrs = append(unknownMgrs, mgrCode)
			continue
		}
		// A row is REVIEWED business truth ONLY when it explicitly says so: every
		// signal must line up -- assignment_source=reviewed AND needs_review is
		// false AND a real source_ref is cited. Anything weaker (missing columns,
		// blank source, provisional placeholder ref, needs_review still true) stays
		// PROVISIONAL. Default toward provisional; never silently promote.
		provisional := true
		srcVal := ""
		if hasSrc && srcCol < len(rec) {
			srcVal = strings.ToLower(strings.TrimSpace(rec[srcCol]))
		}
		needsReview := true
		if nrCol, ok := col["needs_review"]; ok && nrCol < len(rec) {
			switch strings.ToLower(strings.TrimSpace(rec[nrCol])) {
			case "false", "no", "0":
				needsReview = false
			}
		}
		sourceRef := ""
		if srCol, ok := col["source_ref"]; ok && srCol < len(rec) {
			sourceRef = strings.TrimSpace(rec[srCol])
		}
		if srcVal == "reviewed" && !needsReview && sourceRef != "" && sourceRef != provisionalSourceRef {
			provisional = false
		}
		mapping[shedID] = mappingEntry{memberID: memberID, provisional: provisional}
	}
	if len(unknownSheds) > 0 || len(unknownMgrs) > 0 {
		return nil, fmt.Errorf("mapping %s has unresolvable codes (fix the source; the seed will not guess): unknown_sheds=%v unknown_managers=%v",
			path, unknownSheds, unknownMgrs)
	}
	return mapping, nil
}

func loadCodeMap(ctx context.Context, pool *pgxpool.Pool, tenantID, query string) (map[string]string, error) {
	rows, err := pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var code, id string
		if err := rows.Scan(&code, &id); err != nil {
			return nil, err
		}
		m[code] = id
	}
	return m, rows.Err()
}

func batch[T any](ctx context.Context, tx pgx.Tx, rows []T, size int, queue func(*pgx.Batch, T)) error {
	for start := 0; start < len(rows); start += size {
		end := start + size
		if end > len(rows) {
			end = len(rows)
		}
		b := &pgx.Batch{}
		for _, r := range rows[start:end] {
			queue(b, r)
		}
		br := tx.SendBatch(ctx, b)
		var firstErr error
		for range rows[start:end] {
			if _, err := br.Exec(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if cerr := br.Close(); cerr != nil && firstErr == nil {
			firstErr = cerr
		}
		if firstErr != nil {
			return firstErr
		}
	}
	return nil
}

func detUUID(kind string, parts ...string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("goatos:seed-shed-positions:"+kind+":"+strings.Join(parts, ":"))).String()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-shed-positions", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-shed-positions", env, databaseURL, "local", "dev", "test")
}
