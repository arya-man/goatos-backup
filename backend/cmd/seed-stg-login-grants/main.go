// Command seed-stg-login-grants is the permanent, idempotent seeder for the 10
// canonical STG login accounts (docs/runbooks/stg-login-seed-contract.md).
//
// ROOT CAUSE THIS FIXES (do not re-diagnose, see AGENTS.md + the runbooks):
// seed-dev-email-grants only inserts a PENDING row into
// auth_pending_email_grants. That row only materializes into an active
// user_scope_grants row via the /auth/session-events claim path, which fires
// on first sign-in. Admin-web leadership (Google SSO) and mobile
// operators/director never reliably hit that path during a fresh STG seed,
// so they see 403 permission_denied / an empty bottom bar on the FIRST login
// attempt after a reseed. This command closes that gap by materializing the
// active grant directly, in the same run, for all 10 accounts — no reliance on
// a runtime claim event.
//
// For each of the 10 accounts (backend/cmd/seed-stg-login-grants/accounts.go)
// this command, idempotently:
//
//  1. Upserts the pending email grant (auth_pending_email_grants) — belt and
//     suspenders: keeps the claim path working too, and gives a visible audit
//     trail, exactly like seed-dev-email-grants already does.
//  2. Directly inserts/confirms the ACTIVE tenant-scope user_scope_grants row,
//     keyed by platformauth.StableSubjectID(issuer, firebase_uid) — the same
//     derivation the backend uses at request time (jwt.go) — so login works
//     immediately, without waiting for a claim event.
//  3. For operators/director only: binds the derived user_id onto their
//     EXISTING named workforce_members roster row (seeded by seed-roster-real
//     / seed-vaccination-cpt-operator-drive), and ensures
//     department_module_grants gives their department the vaccination
//     bottom bar. Leadership (ceo_internal) intentionally get no department:
//     bootstrap_copy.go:214 already gives them every built module without one.
//
// Safe to re-run any number of times: every write is a guarded
// INSERT/UPDATE ... ON CONFLICT or a "already active, skipping" no-op.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// defaultVaccinationModules matches the department_module_grants seeded
// elsewhere for preventive_care (vaccination + counts) so operators/director
// see the same bottom bar as any other preventive_care roster member.
var defaultVaccinationModules = []string{"vaccination", "counts"}

var departmentCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func main() {
	var tenantID string
	var authIssuer string
	var source string
	var dryRun bool
	fail := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
		os.Exit(1)
	}

	tenantID = envOrDefault("GOATOS_TENANT_ID", "")
	authIssuer = envOrDefault("GOATOS_AUTH_ISSUER", "")
	source = "stg_9_person_login_seed"
	dryRun = envOrDefault("GOATOS_SEED_STG_LOGIN_DRY_RUN", "") == "1"

	if !uuidutil.IsUUIDString(tenantID) {
		fail("invalid or missing GOATOS_TENANT_ID: %q", tenantID)
	}
	if strings.TrimSpace(authIssuer) == "" {
		fail("GOATOS_AUTH_ISSUER is required (e.g. https://securetoken.google.com/goatos-stg)")
	}
	if err := requireFirebaseUIDs(stgLoginAccounts); err != nil {
		fail("%v", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if err := validateTarget(os.Getenv("GOATOS_ENV"), databaseURL); err != nil {
		fail("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fail("open postgres pool: %v", err)
	}
	defer pool.Close()

	type result struct {
		account        Account
		userID         string
		grantActive    bool
		departmentOK   bool
		rosterBoundOK  bool
		notifyRoutesOK bool
		modulesGranted int
		err            error
	}

	results := make([]result, 0, len(stgLoginAccounts))

	for _, acct := range stgLoginAccounts {
		userID := platformauth.StableSubjectID(authIssuer, acct.FirebaseUID)
		res := result{account: acct, userID: userID}

		if dryRun {
			fmt.Printf("[dry-run] would seed %s <%s> role=%s user_id=%s department=%q\n",
				acct.DisplayName, acct.Email, acct.Role, userID, acct.DepartmentCode)
			results = append(results, res)
			continue
		}

		if err := upsertPendingEmailGrant(ctx, pool, tenantID, acct, source); err != nil {
			res.err = fmt.Errorf("pending email grant: %w", err)
			results = append(results, res)
			continue
		}

		if err := materializeTenantGrant(ctx, pool, tenantID, userID, acct.Role); err != nil {
			res.err = fmt.Errorf("materialize tenant grant: %w", err)
			results = append(results, res)
			continue
		}
		res.grantActive = true

		if acct.DepartmentCode != "" {
			if !departmentCodePattern.MatchString(acct.DepartmentCode) {
				res.err = fmt.Errorf("invalid department code %q", acct.DepartmentCode)
				results = append(results, res)
				continue
			}
			departmentID, err := lookupDepartmentID(ctx, pool, tenantID, acct.DepartmentCode)
			if err != nil {
				res.err = fmt.Errorf("lookup department: %w", err)
				results = append(results, res)
				continue
			}

			if err := bindWorkforceMember(ctx, pool, tenantID, userID, departmentID, acct); err != nil {
				res.err = fmt.Errorf("bind workforce member: %w", err)
				results = append(results, res)
				continue
			}
			res.rosterBoundOK = true
			res.departmentOK = true

			granted, err := grantDepartmentModules(ctx, pool, tenantID, departmentID, defaultVaccinationModules)
			if err != nil {
				res.err = fmt.Errorf("grant department modules: %w", err)
				results = append(results, res)
				continue
			}
			res.modulesGranted = granted
		} else {
			// Leadership/verifier accounts have no department, but the mobile
			// /app/bootstrap still hard-requires an active workforce_members
			// profile for the signed-in user (activeProfileAndGrants →
			// operator_profile_missing 403 otherwise). Admin-web tolerates a
			// missing profile; the phone does not. Create the auth profile
			// directly, mirroring the claim path's ensureWorkforceMember.
			if err := ensureAuthProfileMember(ctx, pool, tenantID, userID, acct); err != nil {
				res.err = fmt.Errorf("ensure auth profile member: %w", err)
				results = append(results, res)
				continue
			}
			res.rosterBoundOK = true
		}

		if err := ensureNotificationRouting(ctx, pool, tenantID, userID, acct); err != nil {
			res.err = fmt.Errorf("ensure notification routing: %w", err)
			results = append(results, res)
			continue
		}
		res.notifyRoutesOK = true

		results = append(results, res)
	}

	fmt.Println()
	fmt.Println("=== seed-stg-login-grants: per-account result ===")
	failed := 0
	for _, r := range results {
		if r.err != nil {
			failed++
			fmt.Printf("FAIL  %-20s <%-35s> role=%-14s %v\n", r.account.DisplayName, r.account.Email, r.account.Role, r.err)
			continue
		}
		if dryRun {
			fmt.Printf("DRY   %-20s <%-35s> role=%-14s user_id=%s\n", r.account.DisplayName, r.account.Email, r.account.Role, r.userID)
			continue
		}
		deptNote := fmt.Sprintf("no department (%s); mobile profile ensured", r.account.Role)
		if r.account.DepartmentCode != "" {
			deptNote = fmt.Sprintf("department=%s roster_bound=%v modules_granted=%d", r.account.DepartmentCode, r.rosterBoundOK, r.modulesGranted)
		}
		fmt.Printf("OK    %-20s <%-35s> role=%-14s user_id=%s grant_active=%v notify_routes=%v %s\n",
			r.account.DisplayName, r.account.Email, r.account.Role, r.userID, r.grantActive, r.notifyRoutesOK, deptNote)
	}

	fmt.Println()
	if dryRun {
		fmt.Println("seed-stg-login-grants: dry-run complete, no writes performed")
		return
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "seed-stg-login-grants: %d/%d accounts FAILED — STG seed is INCOMPLETE\n", failed, len(results))
		os.Exit(1)
	}
	fmt.Printf("seed-stg-login-grants: all %d accounts have an ACTIVE tenant grant, mobile profile, and notification routing where applicable\n", len(results))
}

func envOrDefault(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

// upsertPendingEmailGrant mirrors seed-dev-email-grants's insert exactly, so
// the runtime session-events claim path keeps working as a second, redundant
// materialization route for anyone who signs in before this command re-runs.
func upsertPendingEmailGrant(ctx context.Context, pool *pgxpool.Pool, tenantID string, acct Account, source string) error {
	email := authallow.NormalizeEmail(acct.Email)
	var pendingGrantID string
	return pool.QueryRow(ctx, `
INSERT INTO auth_pending_email_grants (
  tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source
) VALUES (
  $1, $2, $2, $3, 'tenant', $1, 'active', now(), $4
)
ON CONFLICT (tenant_id, normalized_email, role, scope_type, scope_id)
  WHERE status = 'active' AND valid_to IS NULL
DO UPDATE SET
  email = EXCLUDED.email,
  source = EXCLUDED.source,
  updated_at = now()
RETURNING pending_grant_id::text`, tenantID, email, acct.Role, source).Scan(&pendingGrantID)
}

// materializeTenantGrant is the fix: insert the ACTIVE user_scope_grants row
// directly (mirrors seed-dev-grant's insert+idempotency shape) instead of
// waiting for the claim event that admin-web/mobile logins do not reliably
// trigger during a fresh STG seed.
func materializeTenantGrant(ctx context.Context, pool *pgxpool.Pool, tenantID, userID, role string) error {
	var grantID string
	err := pool.QueryRow(ctx, `
SELECT grant_id::text
FROM user_scope_grants
WHERE tenant_id = $1
  AND user_id = $2
  AND role = $3
  AND scope_type = 'tenant'
  AND scope_id = $1
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY valid_from DESC, grant_id DESC
LIMIT 1
`, tenantID, userID, role).Scan(&grantID)
	if err == nil {
		return nil // already active — idempotent no-op
	}
	if !isNoRows(err) {
		return err
	}
	return pool.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1, $2, $3, 'tenant', $1, 'active', now())
RETURNING grant_id::text
`, tenantID, userID, role).Scan(&grantID)
}

func lookupDepartmentID(ctx context.Context, pool *pgxpool.Pool, tenantID, code string) (string, error) {
	var departmentID string
	err := pool.QueryRow(ctx, `
SELECT department_id::text FROM departments
WHERE tenant_id = $1 AND code = $2 AND status = 'active'`, tenantID, code).Scan(&departmentID)
	if isNoRows(err) {
		return "", fmt.Errorf("department %q not found for tenant %s (run the vaccination/roster seed first)", code, tenantID)
	}
	return departmentID, err
}

// bindWorkforceMember binds userID onto the operator/director's EXISTING
// named roster row (seeded by seed-roster-real), matching Account.
// RosterDisplayNameMatch case-insensitively. It never invents a roster row —
// an unmatched or ambiguous name is a loud failure, not a fabricated member,
// because roster identity/ownership must come from the reviewed HRMS source
// (see goatos-shed-positions-seed-defect invariant: no invented ownership).
//
// Per the spec: respects the workforce_members_active_user_unique_idx unique
// index on (tenant_id, user_id) WHERE status='active' — if an orphan
// "auth:"+uid placeholder member (created by seed-dev-grant's dev-only
// provisioning path) already holds this user_id, it is deactivated first so
// the real named roster row can take the binding.
func bindWorkforceMember(ctx context.Context, pool *pgxpool.Pool, tenantID, userID, departmentID string, acct Account) error {
	if strings.TrimSpace(acct.RosterDisplayNameMatch) == "" {
		return fmt.Errorf("no RosterDisplayNameMatch configured for %s", acct.DisplayName)
	}

	// Deactivate any orphan auth:<uid> placeholder member holding this
	// user_id so the unique (tenant_id, user_id) active index does not
	// collide when we bind the real named roster row below.
	if _, err := pool.Exec(ctx, `
UPDATE workforce_members
SET status = 'inactive', updated_at = now(), row_version = row_version + 1
WHERE tenant_id = $1
  AND user_id = $2
  AND status = 'active'
  AND display_code = $3`, tenantID, userID, "auth:"+userID); err != nil {
		return fmt.Errorf("deactivate orphan placeholder member: %w", err)
	}

	rows, err := pool.Query(ctx, `
SELECT workforce_member_id::text, user_id, department_id
FROM workforce_members
WHERE tenant_id = $1
  AND status = 'active'
  AND lower(display_name) = lower($2)
ORDER BY created_at ASC, workforce_member_id ASC`, tenantID, acct.RosterDisplayNameMatch)
	if err != nil {
		return err
	}
	type candidate struct {
		id         string
		userID     *string
		department *string
	}
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.userID, &c.department); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	switch len(candidates) {
	case 0:
		return fmt.Errorf("no active workforce_members row with display_name=%q for tenant %s — run seed-roster-real/seed-vaccination-cpt-operator-drive first, or fix RosterDisplayNameMatch in accounts.go", acct.RosterDisplayNameMatch, tenantID)
	case 1:
		// exactly one match, proceed below
	default:
		return fmt.Errorf("ambiguous display_name=%q for tenant %s: %d active workforce_members rows match — narrow RosterDisplayNameMatch in accounts.go", acct.RosterDisplayNameMatch, tenantID, len(candidates))
	}

	c := candidates[0]
	if c.userID != nil && *c.userID != userID {
		return fmt.Errorf("workforce_members row %s for %q is already bound to a DIFFERENT user_id (%s != %s) — resolve manually before re-seeding", c.id, acct.RosterDisplayNameMatch, *c.userID, userID)
	}

	// department_id: bind only if currently NULL, never override an existing
	// HR assignment (mirrors provisionDevDepartmentMember in seed-dev-grant).
	setDepartment := c.department == nil
	_, err = pool.Exec(ctx, `
UPDATE workforce_members
SET user_id = $2,
    department_id = CASE WHEN $4 THEN $3::uuid ELSE department_id END,
    updated_at = now(),
    row_version = row_version + 1
WHERE workforce_member_id = $1 AND tenant_id = $5`,
		c.id, userID, departmentID, setDepartment, tenantID)
	return err
}

// ensureAuthProfileMember creates the active workforce_members profile a
// no-department auth account needs for the mobile /app/bootstrap, which
// hard-requires a profile row (activeProfileAndGrants → operator_profile_missing
// 403 otherwise). These accounts have no named roster row and no department, so
// the profile is the auth:<uid> row keyed on the derived user_id, mirroring the
// runtime claim path's ensureWorkforceMember. Idempotent: the insert is a no-op
// when an active member for this user_id already exists.
func ensureAuthProfileMember(ctx context.Context, pool *pgxpool.Pool, tenantID, userID string, acct Account) error {
	displayName := acct.DisplayName
	roleHint := acct.Role
	var designation any
	if acct.Role == permissions.RoleCEOInternal {
		displayName = "CEO/CXO"
		roleHint = "cxo"
		designation = "cxo"
	}
	_, err := pool.Exec(ctx, `
INSERT INTO workforce_members (
  tenant_id, user_id, display_code, display_name, status,
  primary_role_hint, hr_designation_grade, metadata
)
SELECT $1, $2, $3, $4, 'active', $5, $6, jsonb_build_object(
  'source', 'seed_stg_login_grants_auth_profile',
  'email', $8::text,
  'role', $7::text
)
WHERE NOT EXISTS (
  SELECT 1 FROM workforce_members
  WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
)`, tenantID, userID, "auth:"+userID, displayName, roleHint, designation, acct.Role, acct.Email)
	if err != nil {
		return fmt.Errorf("insert auth workforce member for %s: %w", acct.DisplayName, err)
	}
	return nil
}

func ensureNotificationRouting(ctx context.Context, pool *pgxpool.Pool, tenantID, userID string, acct Account) error {
	if acct.Role != permissions.RoleVerifier {
		return nil
	}
	memberID, err := lookupActiveMemberIDByUser(ctx, pool, tenantID, userID)
	if err != nil {
		return err
	}
	if err := upsertPositionModuleDuty(ctx, pool, tenantID, "preventive_care_verifier", "pc.vaccination", "verify", "proof.verify"); err != nil {
		return err
	}
	parkIDs, err := lookupVaccinationParkIDs(ctx, pool, tenantID)
	if err != nil {
		return err
	}
	for _, parkID := range parkIDs {
		if err := upsertWorkforcePosition(ctx, pool, tenantID, memberID, "center", parkID, "preventive_care_verifier", "manager"); err != nil {
			return err
		}
	}
	return nil
}

func lookupActiveMemberIDByUser(ctx context.Context, pool *pgxpool.Pool, tenantID, userID string) (string, error) {
	var memberID string
	err := pool.QueryRow(ctx, `
SELECT workforce_member_id::text
FROM workforce_members
WHERE tenant_id = $1
  AND user_id = $2
  AND status = 'active'
ORDER BY created_at ASC, workforce_member_id ASC
LIMIT 1`, tenantID, userID).Scan(&memberID)
	if isNoRows(err) {
		return "", fmt.Errorf("active workforce member not found for user_id=%s", userID)
	}
	return memberID, err
}

func lookupVaccinationParkIDs(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]string, error) {
	rows, err := pool.Query(ctx, `
SELECT DISTINCT scope_id::text
FROM workforce_positions
WHERE tenant_id = $1
  AND scope_type = 'center'
  AND position_code LIKE 'vaccination_operator_%'
  AND status = 'active'
ORDER BY 1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no active vaccination operator center positions found; run roster seed first")
	}
	return ids, nil
}

func upsertWorkforcePosition(ctx context.Context, pool *pgxpool.Pool, tenantID, memberID, scopeType, scopeID, positionCode, tier string) error {
	_, err := pool.Exec(ctx, `
INSERT INTO workforce_positions (
  tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, status, valid_from
) VALUES (
  $1, $2, $3, $4, $5, $6, 'active', now()
)
ON CONFLICT (tenant_id, scope_type, scope_id, position_code)
  WHERE status = 'active'
DO UPDATE SET
  workforce_member_id = EXCLUDED.workforce_member_id,
  position_tier = EXCLUDED.position_tier,
  valid_to = NULL,
  updated_at = now(),
  row_version = workforce_positions.row_version + 1`, tenantID, memberID, scopeType, scopeID, positionCode, tier)
	return err
}

func upsertPositionModuleDuty(ctx context.Context, pool *pgxpool.Pool, tenantID, positionCode, moduleCode, dutyType, capabilityCode string) error {
	tag, err := pool.Exec(ctx, `
UPDATE position_module_duties
SET capability_code = $5,
    effective_to = NULL,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1
  AND position_code = $2
  AND module_code = $3
  AND duty_type = $4
  AND status = 'active'
  AND effective_to IS NULL`, tenantID, positionCode, moduleCode, dutyType, capabilityCode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	_, err = pool.Exec(ctx, `
INSERT INTO position_module_duties (
  tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status
) VALUES (
  $1, $2, $3, $4, $5, now(), 'active'
)`, tenantID, positionCode, moduleCode, dutyType, capabilityCode)
	return err
}

func grantDepartmentModules(ctx context.Context, pool *pgxpool.Pool, tenantID, departmentID string, moduleKeys []string) (int, error) {
	tag, err := pool.Exec(ctx, `
INSERT INTO department_module_grants (tenant_id, department_id, module_key, status)
SELECT $1::uuid, $2::uuid, key, 'active'
FROM unnest($3::text[]) AS key
ON CONFLICT (tenant_id, department_id, module_key) DO UPDATE
SET status = 'active', updated_at = now()
WHERE department_module_grants.status <> 'active'`, tenantID, departmentID, moduleKeys)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func validateTarget(env, databaseURL string) error {
	if strings.EqualFold(strings.TrimSpace(env), "stg") {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("seed-stg-login-grants", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("seed-stg-login-grants", env, databaseURL, "local", "dev", "test")
}
