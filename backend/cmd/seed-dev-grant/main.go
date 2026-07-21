package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
)

// departmentCodePattern mirrors departments_code_check.
var departmentCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// moduleKeyPattern mirrors department_module_grants_module_key_check (mig 000002).
var moduleKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// defaultDevModules is what -modules grants when the flag is not passed. It stays
// "vaccination" so the documented invocation in docs/runbooks/android-dev-device.md
// (-department preventive_care, no -modules) keeps producing a WORKING vaccination
// operator: without a department_module_grants row, ListGrantedModuleKeys returns
// empty and /app/bootstrap serves an empty bottom bar.
const defaultDevModules = "vaccination"

func main() {
	var tenantID string
	var userID string
	var externalSubject string
	var authIssuer string
	var role string
	var department string
	var modules string
	var parkID string
	var parkOnly bool
	flag.StringVar(&tenantID, "tenant-id", "", "tenant UUID for the tenant-scope grant")
	flag.StringVar(&userID, "user-id", "", "user UUID for the grant")
	flag.StringVar(&externalSubject, "external-subject", "", "non-UUID IdP subject to map into the grant user UUID")
	flag.StringVar(&authIssuer, "auth-issuer", os.Getenv("GOATOS_AUTH_ISSUER"), "issuer used when mapping an external IdP subject")
	flag.StringVar(&role, "role", "", "required role: a flat legacy role (verifier, park_head, pc_director, operator, ceo_internal) or a composite tier x vertical org role key such as manager_feed, director_health, am_preventive_care (see context/architecture/org-role-model.md and permissions.RoleKey)")
	flag.StringVar(&department, "department", "", "optional HR department code; provisions/attaches a workforce_member so department-driven nav works for this dev identity")
	flag.StringVar(&modules, "modules", defaultDevModules, "comma-separated module keys granted to -department (e.g. \"vaccination,counts\"); requires -department, since department_module_grants is department-scoped. Pass \"\" to skip module granting. Keys are matched against moduleNavRegistry in backend/internal/workforce/app/bootstrap_copy.go -- an unknown key is stored and simply contributes no nav")
	flag.StringVar(&parkID, "park-id", "", "optional park location UUID; when set, also seeds a scope_type='park' grant row for role (use -park-only to omit the tenant-wide grant)")
	flag.BoolVar(&parkOnly, "park-only", false, "seed only the park-scoped grant; requires -park-id and enables honest cross-park authorization tests")
	flag.Parse()

	department = strings.TrimSpace(department)
	if department != "" && !departmentCodePattern.MatchString(department) {
		fail("invalid department code: %q (must match ^[a-z][a-z0-9_]*$)", department)
	}

	moduleKeys, err := parseModuleKeys(modules)
	if err != nil {
		fail("%v", err)
	}
	// Module grants hang off the department, not the user. Failing loudly here beats
	// silently dropping them and leaving the dev identity with an empty bottom bar.
	if department == "" && len(moduleKeys) > 0 && modules != defaultDevModules {
		fail("-modules requires -department (department_module_grants is department-scoped)")
	}

	userID, err = resolveGrantUserID(userID, externalSubject, authIssuer)
	if err != nil {
		fail("%v", err)
	}
	if !isUUID(tenantID) {
		fail("invalid or missing tenant id: %q", tenantID)
	}
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if err := validateLocalTarget(os.Getenv("GOATOS_ENV"), databaseURL); err != nil {
		fail("%v", err)
	}
	if !validRole(role) {
		fail("invalid or missing role: %q", role)
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !isUUID(parkID) {
		fail("invalid -park-id: %q", parkID)
	}
	if err := validateGrantScopeMode(parkID, parkOnly); err != nil {
		fail("%v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		fail("open postgres pool: %v", err)
	}
	defer pool.Close()

	if !parkOnly {
		var grantID string
		existing := false
		if err := pool.QueryRow(ctx, `
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
`, tenantID, userID, role).Scan(&grantID); err == nil {
			fmt.Printf("dev grant already active %s for user %s role %s tenant %s\n", grantID, userID, role, tenantID)
			existing = true
		} else if !isNoRows(err) {
			fail("query dev grant: %v", err)
		}

		if !existing {
			if err := pool.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1, $2, $3, 'tenant', $1, 'active', now())
RETURNING grant_id::text
`, tenantID, userID, role).Scan(&grantID); err != nil {
				fail("insert local dev grant: %v", err)
			}
			fmt.Printf("seeded tenant grant %s for user %s role %s tenant %s\n", grantID, userID, role, tenantID)
		}
	}

	if parkID != "" {
		if err := seedParkScopedGrant(ctx, pool, tenantID, userID, role, parkID); err != nil {
			fail("seed park-scoped grant: %v", err)
		}
	}

	if department != "" {
		if err := provisionDevDepartmentMember(ctx, pool, tenantID, userID, role, department); err != nil {
			fail("provision dev department member: %v", err)
		}
		fmt.Printf("dev workforce_member for user %s attached to department %s\n", userID, department)

		// Seed coupling for mig 000002 (docs/runbooks/initial-seed-migration-coupling.md):
		// attaching a member to a department is only half the job now. Nav is composed from
		// department_module_grants, so without these rows the identity signs in to an empty
		// bottom bar even though the grant + member rows look correct.
		if len(moduleKeys) > 0 {
			granted, err := grantDevDepartmentModules(ctx, pool, tenantID, department, moduleKeys)
			if err != nil {
				fail("grant dev department modules: %v", err)
			}
			fmt.Printf("department %s granted modules %s (%d new)\n",
				department, strings.Join(moduleKeys, ","), granted)
		}
	}
}

func validateGrantScopeMode(parkID string, parkOnly bool) error {
	if parkOnly && strings.TrimSpace(parkID) == "" {
		return errors.New("-park-only requires -park-id")
	}
	return nil
}

// parseModuleKeys splits and validates the -modules list. Duplicates are collapsed so
// a sloppy "vaccination,vaccination" is not an error; order is preserved for readable output.
func parseModuleKeys(raw string) ([]string, error) {
	out := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	for _, part := range strings.Split(raw, ",") {
		key := strings.TrimSpace(part)
		if key == "" {
			continue
		}
		if !moduleKeyPattern.MatchString(key) {
			return nil, fmt.Errorf("invalid module key: %q (must match ^[a-z][a-z0-9_]*$)", key)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

// grantDevDepartmentModules upserts department_module_grants rows for the department's
// module keys and returns how many were newly inserted. Idempotent by the table's
// (tenant_id, department_id, module_key) unique constraint: a re-run reactivates a row
// that was set inactive rather than duplicating it, which matches how re-running the
// whole dev-grant seed is expected to behave.
func grantDevDepartmentModules(ctx context.Context, pool *pgxpool.Pool, tenantID, departmentCode string, moduleKeys []string) (int, error) {
	var departmentID string
	err := pool.QueryRow(ctx, `
SELECT department_id::text FROM departments
WHERE tenant_id = $1 AND code = $2 AND status = 'active'`, tenantID, departmentCode).Scan(&departmentID)
	if isNoRows(err) {
		return 0, fmt.Errorf("department %q not found for tenant %s (run migrations/seed first)", departmentCode, tenantID)
	}
	if err != nil {
		return 0, err
	}

	// One set-based statement over the key array rather than a query per key.
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

// provisionDevDepartmentMember mirrors the sign-in email-claim provisioning for
// the dev identity (which is seeded directly, not via the email-claim path):
// resolve the department by (tenant, code), then upsert a workforce_member for
// the user, filling a NULL department_id but never overriding an HR assignment.
func provisionDevDepartmentMember(ctx context.Context, pool *pgxpool.Pool, tenantID, userID, role, departmentCode string) error {
	var departmentID string
	err := pool.QueryRow(ctx, `
SELECT department_id::text FROM departments
WHERE tenant_id = $1 AND code = $2 AND status = 'active'`, tenantID, departmentCode).Scan(&departmentID)
	if isNoRows(err) {
		return fmt.Errorf("department %q not found for tenant %s (run migrations/seed first)", departmentCode, tenantID)
	}
	if err != nil {
		return err
	}

	var memberID string
	var existingDepartment *string
	err = pool.QueryRow(ctx, `
SELECT workforce_member_id::text, department_id::text
FROM workforce_members
WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'
ORDER BY created_at DESC, workforce_member_id DESC
LIMIT 1`, tenantID, userID).Scan(&memberID, &existingDepartment)
	if err == nil {
		if existingDepartment != nil {
			return nil
		}
		_, err = pool.Exec(ctx, `
UPDATE workforce_members
SET department_id = $3, updated_at = now(), row_version = row_version + 1
WHERE workforce_member_id = $1 AND tenant_id = $2 AND department_id IS NULL`, memberID, tenantID, departmentID)
		return err
	}
	if !isNoRows(err) {
		return err
	}

	_, err = pool.Exec(ctx, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint, department_id)
VALUES ($1, $2, $3, $4, 'active', $5, $6)`,
		tenantID, userID, "auth:"+userID, "dev-"+role, devMemberRoleHint(role), departmentID)
	return err
}

func devMemberRoleHint(role string) string {
	switch role {
	case permissions.RoleOperator:
		return "operator"
	case permissions.RoleParkHead:
		return "park_head"
	case permissions.RoleVerifier:
		return "verifier"
	case permissions.RoleCEOInternal:
		return "cxo"
	case permissions.RolePCDirector:
		return "supervisor"
	default:
		// Composite tier x vertical org role keys (e.g. "manager_feed",
		// "director_health") are not individually listed in
		// workforce_members_role_hint_check -- that CHECK stays a small,
		// display-only HR enum (see migrations/postgres/000178_org_role_catalog.sql's
		// comment on why it is intentionally out of scope for the FK swap).
		// Map by tier instead: ground tiers (Assistant Manager) hint
		// "operator", everything above ground hints "supervisor".
		if tier, _, ok := permissions.ParseRoleKey(role); ok {
			if tier == permissions.TierAssistantManager {
				return "operator"
			}
			return "supervisor"
		}
		return "other"
	}
}

func validateLocalTarget(env, databaseURL string) error {
	return localtarget.ValidateLocalDatabaseTarget("seed-dev-grant", env, databaseURL, "local", "dev", "test")
}

func isLocalHost(host string) bool {
	return localtarget.IsLocalHost(host)
}

func resolveGrantUserID(userID, externalSubject, authIssuer string) (string, error) {
	userID = strings.TrimSpace(userID)
	externalSubject = strings.TrimSpace(externalSubject)
	authIssuer = strings.TrimSpace(authIssuer)
	if userID != "" && externalSubject != "" {
		return "", errors.New("set either -user-id or -external-subject, not both")
	}
	if userID == "" {
		if externalSubject == "" {
			return "", errors.New("missing -user-id or -external-subject")
		}
		if authIssuer == "" {
			return "", errors.New("GOATOS_AUTH_ISSUER or -auth-issuer is required with -external-subject")
		}
		userID = platformauth.StableSubjectID(authIssuer, externalSubject)
	}
	if !isUUID(userID) {
		return "", fmt.Errorf("invalid user id %q", userID)
	}
	return userID, nil
}

func isUUID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
	}
	return true
}

// validRole accepts the flat legacy roles AND any composite tier x vertical
// org role key (e.g. "manager_feed") -- permissions.IsKnownRole is the single
// source of truth so a new vertical/tier does not require touching this CLI.
func validRole(role string) bool {
	return permissions.IsKnownRole(role)
}

// seedParkScopedGrant adds (idempotently) a scope_type='park' grant row for
// role, in addition to the tenant-scope grant seeded above. Most permission
// gating (RolesAuthorize via httpmiddleware.routeRoles) only reads
// tenant-scope grants; this park-scoped row exists for handlers/modules that
// hard-filter by park (see internal/calendar/adapters/http/handler.go
// calendarScope and permissions.ScopeIDsForPermission) -- it lets a developer
// locally exercise "this role can act in park A but not park B".
func seedParkScopedGrant(ctx context.Context, pool *pgxpool.Pool, tenantID, userID, role, parkID string) error {
	var grantID string
	err := pool.QueryRow(ctx, `
SELECT grant_id::text
FROM user_scope_grants
WHERE tenant_id = $1
  AND user_id = $2
  AND role = $3
  AND scope_type = 'park'
  AND scope_id = $4
  AND status = 'active'
  AND valid_from <= now()
  AND (valid_to IS NULL OR valid_to > now())
ORDER BY valid_from DESC, grant_id DESC
LIMIT 1
`, tenantID, userID, role, parkID).Scan(&grantID)
	if err == nil {
		fmt.Printf("dev park-scoped grant already active %s for user %s role %s park %s\n", grantID, userID, role, parkID)
		return nil
	}
	if !isNoRows(err) {
		return err
	}
	if err := pool.QueryRow(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1, $2, $3, 'park', $4, 'active', now())
RETURNING grant_id::text
`, tenantID, userID, role, parkID).Scan(&grantID); err != nil {
		return err
	}
	fmt.Printf("seeded park-scoped grant %s for user %s role %s park %s\n", grantID, userID, role, parkID)
	return nil
}

func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
