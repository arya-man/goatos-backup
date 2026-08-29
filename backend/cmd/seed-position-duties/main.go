// Command seed-position-duties derives the position_module_duties rows
// (migration 000157: the normalized "what work does this position do" model,
// design doc S4.6) from the live workforce_positions catalog, so the duties
// dimension is REAL and reproducible instead of ad-hoc hand-seeded rows.
//
// Derivation (no invented data -- every row is a projection of an existing
// active workforce_positions seat):
//   - module_code comes from the position_code PREFIX (see modulePrefixes).
//     The reviewed CPT vaccination roster treats Amit, Darshan, and Sagar as
//     manager-tier vaccination operators, not support-only/park-head-only
//     users. Therefore vaccination_operator_* positions are explicit
//     pc.vaccination execute duty rows.
//   - duty_type = 'execute' for surfaced vaccination_operator_* positions even
//     when their HR/title tier is manager/head; that tier does not remove them
//     from drive execution (see isInflatedTierVaccinationOperator). Every other
//     pc.vaccination seat -- preventive_care_manager, park_head, shed_manager --
//     uses 'manage' for supervisory tiers (manager | head | director | cxo),
//     same as every other module, else 'execute'. pc.vaccination is NOT
//     blanket-excluded from 'manage': reminder_cadence.go documents and
//     resolves a real manage audience for that module.
//   - capability_code is the execution permission a temporary backup grant for
//     that seat confers. Only pc.vaccination is a built + surfaced module today
//     (scope-lock), so only the preventive_care prefix carries
//     'vaccination.execute'; every other module's capability_code is NULL until
//     that module is built.
//   - duty_type = 'verify' rows are emitted for the tenant Video Verification Team
//     seat (VerifierPositionCode), one per module that routes a pending-proof
//     notification (notificationbridge.PendingNotificationDutyModules). Without these
//     the verify join in ResolveModuleDutyRecipients matches nothing and every
//     verifier push resolves to zero devices. The run ends with a closeout assertion
//     that every such module has a reachable verify duty holder.
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

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

// vaccinationExecuteCapability is the execution permission a temporary backup
// grant covering the preventive_care seat confers (design doc S4.6). It is the
// ONLY built-module capability today (scope-lock: pc.vaccination is the only
// built + surfaced module).
const vaccinationExecuteCapability = "vaccination.execute"
const weighingExecuteCapability = "weighing.execute"

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
	{prefix: "vaccination_operator", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "preventive_care", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "backup_manager", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "shed_manager", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "park_head", moduleCode: "pc.vaccination", capability: vaccinationExecuteCapability},
	{prefix: "breeding_growth_director", moduleCode: "weighing", capability: weighingExecuteCapability},
	{prefix: "weighing_operator", moduleCode: "weighing", capability: weighingExecuteCapability},
	{prefix: "health_kidding", moduleCode: "health.kidding"},
	{prefix: "feeding", moduleCode: "feed.direction"},
	{prefix: "packaging", moduleCode: "packaging"},
	{prefix: "cleaning", moduleCode: "cleaning"},
	{prefix: "farming", moduleCode: "farming"},
	{prefix: "milk", moduleCode: "milk"},
}

// VerifierPositionCode is the tenant Video Verification Team seat. The verifier is
// TENANT-level (one verifier reviews proof for every park and shed), but
// ResolveModuleDutyRecipients resolves verify duty holders at CENTER scope with the
// item's park id, so the seat is held by the same member at every center rather than
// once at tenant scope. See backend/cmd/seed-roster-real (seedVerifierSeats).
const VerifierPositionCode = "video_verifier"

// dutyTypeVerify is the duty_type ResolveModuleDutyRecipients
// (workforce/adapters/postgres/roster_repository.go) joins on to find who reviews a
// module's proofs.
//
// THIS SEEDER NEVER EMITTED IT. deriveDuties produces only 'execute' and 'manage', so
// position_module_duties held zero verify rows, so that join matched nothing, so EVERY
// verifier push -- vaccination and weighing included, not just the new modules --
// resolved to zero devices. Nothing failed: the notification tests stubbed recipient
// resolution, the consumer logged "no recipients" at WARN, and the proof sat unreviewed.
// deriveVerifierDuties below is the fix; assertVerifyDutyCoverage is what stops it
// regressing into silence again.
const dutyTypeVerify = "verify"

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
	verifyModules := verifierDutyModules()
	duties = append(duties, deriveVerifierDuties(positions, verifyModules)...)
	st.DutiesDerived = len(duties)

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
		"  duties_inserted=%d duties_already_present=%d verify_modules=%v\n",
		st.DutiesInserted, st.DutiesDerived-st.DutiesInserted, verifyModules)

	// Closeout runs LAST and is not advisory: a seed that leaves a notified module without a
	// verify duty holder has produced a notification path that is green in tests and dead in
	// the field, which is the exact failure this command exists to prevent.
	if err := assertVerifyDutyCoverage(ctx, pool, *tenantID, verifyModules); err != nil {
		return err
	}
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
		// The verifier seat is deliberately absent from modulePrefixes: its duties are
		// 'verify' rows minted by deriveVerifierDuties from the notification routing map,
		// not a module-prefix projection. Adding it to modulePrefixes would mint a bogus
		// EXECUTE duty for the verifier and break separation of duty (the person who
		// reviews the proof would carry the capability to perform the work). Skipping it
		// here (rather than letting it fall through to the unmapped branch) is what stops
		// -strict -- which BOTH real invocations use, Makefile seed-vaccination-real and
		// seed-vaccination-cpt-operator-drive -- from aborting the whole run before
		// insertDuties and leaving position_module_duties completely empty.
		if p.positionCode == VerifierPositionCode {
			continue
		}
		mp, ok := matchModule(p.positionCode)
		if !ok {
			st.UnmappedSkipped++
			st.UnmappedPrefixes[p.positionCode]++
			continue
		}
		dutyType := "execute"
		if manageTiers[p.positionTier] && !isInflatedTierVaccinationOperator(p.positionCode) {
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

// verifierDutyModules is the module_code list the verifier seat's 'verify' duty rows are
// minted for. It is the UNION of two canonical sources, and BOTH matter:
//
//   - notificationbridge.PendingNotificationDutyModules(): the exact module_code spellings
//     ("pc.vaccination", "feed.direction") the pending-proof push consumer joins on. These
//     must stay verbatim or ResolveModuleDutyRecipients resolves zero devices.
//   - verificationcatalog.All() NavigationModule keys: EVERY registered verification
//     category's module. This is what the mobile verifier's per-feature [Verify, Alerts]
//     bar is composed from (workforce verifierFeatureKeys reads these duty rows), so a
//     category registered here with no duty row is a queue the WEB lens shows and the
//     PHONE never composes a tab for. That is exactly how the 2026-08 milk gap shipped:
//     milk verification split out of Counts into its own navigation module, no
//     notification profile was declared for it (deliberately — an unclaimed module
//     notifies nobody loudly), and because THIS seeder read only the notification map,
//     no seed run could ever emit a milk verify duty.
//
// Dedupe is on the NORMALIZED key (strip "pc.", dots → underscores — the same collapse
// workforce's verifierFeatureKeys applies), preferring the notification spelling when both
// sources name the same module, so the union never emits "vaccination" beside
// "pc.vaccination".
func verifierDutyModules() []string {
	out := notificationbridge.PendingNotificationDutyModules()
	seen := make(map[string]bool, len(out))
	normalize := func(key string) string {
		key = strings.TrimPrefix(key, "pc.")
		return strings.ReplaceAll(key, ".", "_")
	}
	for _, m := range out {
		seen[normalize(m)] = true
	}
	for _, def := range verificationcatalog.All() {
		if def.NavigationModule == "" || seen[normalize(def.NavigationModule)] {
			continue
		}
		seen[normalize(def.NavigationModule)] = true
		out = append(out, def.NavigationModule)
	}
	sort.Strings(out)
	return out
}

// deriveVerifierDuties emits one 'verify' duty row per notified module for the Video
// Verification Team seat, so ResolveModuleDutyRecipients can actually find a reviewer.
//
// The module list is NOT re-typed here: it comes from
// notificationbridge.PendingNotificationDutyModules(), the same map the push consumer
// routes from. A hand-copied list would drift, and drift in this exact place is what left
// the verify join empty.
//
// capability_code stays NULL: a verify duty confers no execution capability, so a backup
// grant covering the verifier seat must not hand anyone a scanner.
func deriveVerifierDuties(positions []positionRow, modules []string) []dutyRow {
	held := false
	for _, p := range positions {
		if p.positionCode == VerifierPositionCode {
			held = true
			break
		}
	}
	if !held {
		// Do not invent a duty for a seat that does not exist. The closeout assertion
		// reports it as uncovered, which is the loud failure this seeder owes the caller.
		return nil
	}
	out := make([]dutyRow, 0, len(modules))
	for _, module := range modules {
		out = append(out, dutyRow{positionCode: VerifierPositionCode, moduleCode: module, dutyType: dutyTypeVerify})
	}
	return out
}

// assertVerifyDutyCoverage is the SEED CLOSEOUT: every module that routes a pending-proof
// push must have at least one REACHABLE verify duty holder -- an active duty row on an
// active seat held by an active member. It counts what the notification path itself
// resolves, minus the device join, so "seeded but nobody holds the seat" fails here rather
// than in the field as a silent non-delivery.
// It must count what the RUNTIME counts, not a looser superset. ResolveModuleDutyRecipients
// (workforce/adapters/postgres/roster_repository.go) resolves verify duty holders at
// scope_type='center' with the ITEM'S PARK ID, and applies the position (valid_from/valid_to)
// and duty (effective_from/effective_to) windows. A closeout that filters only
// tenant/module/duty_type/status therefore passes on a verifier seated at ONE park while
// pushes at every OTHER park still resolve to zero devices -- the exact silent
// non-delivery this assertion exists to prevent. So the check is per (module, park in scope),
// with the same scope and temporal predicates the runtime query uses. The only runtime
// predicate deliberately omitted is the device join: "seat held but phone not registered" is
// a device-enrollment state, not a seeding defect.
func assertVerifyDutyCoverage(ctx context.Context, pool *pgxpool.Pool, tenantID string, modules []string) error {
	at := time.Now().UTC()
	type gap struct{ module, park string }
	var uncovered []gap
	parksInScope := 0
	for i, module := range modules {
		rows, err := pool.Query(ctx, `
WITH parks AS (
  SELECT DISTINCT scope_id, scope_id::text AS park_id
  FROM workforce_positions
  WHERE tenant_id = $1::uuid
    AND scope_type = 'center'
    AND status = 'active'
    AND valid_from <= $4::timestamptz
    AND (valid_to IS NULL OR valid_to > $4::timestamptz)
)
SELECT COALESCE(l.location_code, parks.park_id) AS park,
       EXISTS (
         SELECT 1
         FROM workforce_positions p
         JOIN position_module_duties pmd
           ON pmd.tenant_id = p.tenant_id
          AND pmd.position_code = p.position_code
          AND pmd.module_code = $2
          AND pmd.duty_type = $3
          AND pmd.status = 'active'
          AND pmd.effective_from <= $4::timestamptz
          AND (pmd.effective_to IS NULL OR pmd.effective_to > $4::timestamptz)
         JOIN workforce_members m
           ON m.tenant_id = p.tenant_id
          AND m.workforce_member_id = p.workforce_member_id
          AND m.status = 'active'
         WHERE p.tenant_id = $1::uuid
           AND p.scope_type = 'center'
           AND p.scope_id = parks.scope_id
           AND p.status = 'active'
           AND p.valid_from <= $4::timestamptz
           AND (p.valid_to IS NULL OR p.valid_to > $4::timestamptz)
       ) AS covered
FROM parks
LEFT JOIN locations l ON l.tenant_id = $1::uuid AND l.location_id = parks.scope_id
ORDER BY 1`, tenantID, module, dutyTypeVerify, at)
		if err != nil {
			return fmt.Errorf("count verify duty holders for %s: %w", module, err)
		}
		parks := 0
		for rows.Next() {
			var park string
			var covered bool
			if err := rows.Scan(&park, &covered); err != nil {
				rows.Close()
				return fmt.Errorf("scan verify duty coverage for %s: %w", module, err)
			}
			parks++
			if !covered {
				uncovered = append(uncovered, gap{module: module, park: park})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return fmt.Errorf("count verify duty holders for %s: %w", module, err)
		}
		if i == 0 {
			parksInScope = parks
		}
		if parks == 0 {
			return fmt.Errorf(
				"seed closeout FAILED: no active center-scope workforce position exists for tenant %s, so no park can hold a "+
					"'%s' duty and every verifier push resolves to zero devices. Seed the roster (backend/cmd/seed-roster-real) first",
				tenantID, dutyTypeVerify)
		}
	}
	if len(uncovered) > 0 {
		pairs := make([]string, 0, len(uncovered))
		for _, g := range uncovered {
			pairs = append(pairs, g.module+"@"+g.park)
		}
		return fmt.Errorf(
			"seed closeout FAILED: module/park pair(s) %v route a pending-proof notification but have no active '%s' duty holder "+
				"at scope_type='center' for that park, so every verifier push there resolves to zero devices. Seed the %q seat "+
				"at every park in scope (backend/cmd/seed-roster-real) and re-run this seeder",
			pairs, dutyTypeVerify, VerifierPositionCode)
	}
	fmt.Printf("verify duty coverage OK: %d module(s) x %d center-scope park(s)\n", len(modules), parksInScope)
	return nil
}

// vaccinationExecuteOnlyPrefixes are pc.vaccination position_code prefixes that
// always carry 'execute', never 'manage', regardless of their HR/title
// position_tier:
//   - "vaccination_operator": the reviewed CPT roster gives these seats an
//     inflated HR/title tier (manager/head) that does not reflect
//     drive-execution reality (commit cae41af63).
//   - "backup_manager": a backup slot covers the absent manager's TASKS, not
//     their supervisory role -- it never gains management authority over the
//     module (see goatos-backup-manager-coverage memory / leave-coverage docs).
var vaccinationExecuteOnlyPrefixes = []string{"vaccination_operator", "backup_manager"}

// isInflatedTierVaccinationOperator reports whether positionCode is one of
// vaccinationExecuteOnlyPrefixes, the ONLY cases where a supervisory
// position_tier (manager | head | director | cxo) does not translate to a
// 'manage' duty for pc.vaccination.
//
// This is narrower than "pc.vaccination gets no manage duty": that module DOES
// have a real manage audience -- preventive_care_manager, park_head, and
// shed_manager are documented as carrying pc.vaccination 'manage' in
// backend/internal/kernelstages/reminder_cadence.go ("park_head /
// preventive_care_manager / shed_manager (manager+ tiers) carry 'manage'"), and
// reminderCadenceDutyTypes = []string{"execute", "manage"} means the reminder
// ladder actively resolves that duty. Blanket-excluding the whole module (as a
// prior revision did) silently starved that real audience and made
// tools/dev/seed-closeout.sh's assert_reminder_audience_resolves check
// unsatisfiable on any fresh database: closeout requires an ACTIVE seat holding
// BOTH pc.vaccination execute AND manage, but no seeded position could ever earn
// 'manage' for that module.
func isInflatedTierVaccinationOperator(positionCode string) bool {
	for _, prefix := range vaccinationExecuteOnlyPrefixes {
		if positionCode == prefix || strings.HasPrefix(positionCode, prefix+"_") {
			return true
		}
	}
	return false
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
