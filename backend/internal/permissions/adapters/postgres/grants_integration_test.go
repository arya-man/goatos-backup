package postgres

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	defaultPostgresImage = "postgres:16.9-alpine"
	meshaTenant          = "00000000-0000-4000-8000-000000000001"
	activeUser           = "90000000-0000-4000-8000-000000000001"
	revokedUser          = "90000000-0000-4000-8000-000000000002"
	inactiveUser         = "90000000-0000-4000-8000-000000000003"
	expiredUser          = "90000000-0000-4000-8000-000000000004"
	futureUser           = "90000000-0000-4000-8000-000000000005"
	unionUser            = "90000000-0000-4000-8000-000000000006"
	parkScopeUser        = "90000000-0000-4000-8000-000000000007"
	emailGrantUser       = "90000000-0000-4000-8000-000000000008"
	cbeLocation          = "00000000-0000-4000-8000-000000003001"
)

func TestGrantSourceReadsOnlyLiveTenantScopeGrants(t *testing.T) {
	pgtest.SkipIfNoDocker(t)

	ctx := context.Background()
	container := fmt.Sprintf("goatos-permissions-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	waitForPostgres(t, container)
	applyMigrations(t, container)
	psql(t, container, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from, valid_to)
VALUES
  ('`+meshaTenant+`', '`+activeUser+`', 'verifier', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+revokedUser+`', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'revoked', now() - interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+inactiveUser+`', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'inactive', now() - interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+expiredUser+`', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'active', now() - interval '2 hours', now() - interval '1 hour'),
  ('`+meshaTenant+`', '`+futureUser+`', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'active', now() + interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+unionUser+`', 'operator', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+unionUser+`', 'verifier', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 hour', NULL),
  ('`+meshaTenant+`', '`+parkScopeUser+`', 'ceo_internal', 'park', '`+cbeLocation+`', 'active', now() - interval '1 hour', NULL);
`)

	pool := openPool(t, ctx, container)
	defer pool.Close()
	source := NewGrantSource(pool, 5*time.Second)

	roles, err := source.ActiveTenantRoles(ctx, activeUser, meshaTenant)
	if err != nil {
		t.Fatalf("ActiveTenantRoles(active): %v", err)
	}
	if len(roles) != 1 || roles[0] != "verifier" {
		t.Fatalf("active roles = %#v", roles)
	}
	for _, userID := range []string{revokedUser, inactiveUser, expiredUser, futureUser, parkScopeUser, "90000000-0000-4000-8000-000000000099"} {
		roles, err := source.ActiveTenantRoles(ctx, userID, meshaTenant)
		if err != nil {
			t.Fatalf("ActiveTenantRoles(%s): %v", userID, err)
		}
		if len(roles) != 0 {
			t.Fatalf("non-live user %s got roles %#v", userID, roles)
		}
	}
	grants, err := source.ActiveTenantGrants(ctx, parkScopeUser, meshaTenant)
	if err != nil {
		t.Fatalf("ActiveTenantGrants(parkScopeUser): %v", err)
	}
	if len(grants) != 1 || grants[0] != (permissions.ActiveGrant{Role: permissions.RoleCEOInternal, ScopeType: "park", ScopeID: cbeLocation}) {
		t.Fatalf("park scoped grants = %#v", grants)
	}

	roles, err = source.ActiveTenantRoles(ctx, unionUser, meshaTenant)
	if err != nil {
		t.Fatalf("ActiveTenantRoles(union): %v", err)
	}
	if len(roles) != 2 || !permissions.RolesAuthorize(roles, []string{permissions.TaskVerify}, false) {
		t.Fatalf("operator+verifier union did not authorize verifier-only permission: %#v", roles)
	}

	psql(t, container, `
INSERT INTO auth_pending_email_grants (tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source)
VALUES ('`+meshaTenant+`', 'Ravi@Mesha.SG', 'ravi@mesha.sg', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 minute', 'test');
`)
	claimer := NewPendingEmailGrantClaimer(pool, 5*time.Second)
	result, err := claimer.ClaimPendingEmailGrant(ctx, permissions.PendingEmailGrantClaim{
		TenantID:        meshaTenant,
		UserID:          emailGrantUser,
		Email:           "RAVI@MESHA.SG",
		ExternalSubject: "firebase-uid-ravi",
		Issuer:          "https://securetoken.google.com/goatos-dev",
		Source:          "admin-web",
		TraceID:         "trace-test",
	})
	if err != nil {
		t.Fatalf("ClaimPendingEmailGrant: %v", err)
	}
	if !result.Matched || len(result.InsertedGrants) != 1 || result.InsertedGrants[0].Role != permissions.RoleCEOInternal {
		t.Fatalf("claim result=%#v", result)
	}
	roles, err = source.ActiveTenantRoles(ctx, emailGrantUser, meshaTenant)
	if err != nil {
		t.Fatalf("ActiveTenantRoles(emailGrantUser): %v", err)
	}
	if len(roles) != 1 || roles[0] != permissions.RoleCEOInternal {
		t.Fatalf("email grant roles=%#v", roles)
	}
	var memberCount int
	var roleHint string
	var designationGrade *string
	if err := pool.QueryRow(ctx, `
SELECT count(*), max(primary_role_hint), max(hr_designation_grade)
FROM workforce_members
WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'`, meshaTenant, emailGrantUser).Scan(&memberCount, &roleHint, &designationGrade); err != nil {
		t.Fatalf("count claimed email workforce member rows: %v", err)
	}
	if memberCount != 1 || roleHint != "cxo" || designationGrade == nil || *designationGrade != "cxo" {
		t.Fatalf("claimed email workforce profile count=%d roleHint=%q designationGrade=%v, want one CEO/CXO profile", memberCount, roleHint, designationGrade)
	}
	result, err = claimer.ClaimPendingEmailGrant(ctx, permissions.PendingEmailGrantClaim{
		TenantID:        meshaTenant,
		UserID:          emailGrantUser,
		Email:           "ravi@mesha.sg",
		ExternalSubject: "firebase-uid-ravi",
		Issuer:          "https://securetoken.google.com/goatos-dev",
		Source:          "admin-web",
		TraceID:         "trace-test",
	})
	if err != nil {
		t.Fatalf("idempotent ClaimPendingEmailGrant: %v", err)
	}
	if len(result.InsertedGrants) != 0 || len(result.ExistingGrants) != 1 {
		t.Fatalf("idempotent claim result=%#v", result)
	}
	var grantCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM user_scope_grants
WHERE tenant_id = $1 AND user_id = $2 AND role = 'ceo_internal' AND status = 'active'`, meshaTenant, emailGrantUser).Scan(&grantCount); err != nil {
		t.Fatalf("count email grant rows: %v", err)
	}
	if grantCount != 1 {
		t.Fatalf("grantCount=%d want 1", grantCount)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM workforce_members
WHERE tenant_id = $1 AND user_id = $2 AND status = 'active'`, meshaTenant, emailGrantUser).Scan(&memberCount); err != nil {
		t.Fatalf("count idempotent email workforce member rows: %v", err)
	}
	if memberCount != 1 {
		t.Fatalf("memberCount=%d want 1", memberCount)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM audit_log
WHERE tenant_id = $1 AND actor_id = $2 AND action = 'auth.pending_email_grant_claimed'`, meshaTenant, emailGrantUser).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("auditCount=%d want 1", auditCount)
	}
}

func waitForPostgres(t *testing.T, container string) {
	t.Helper()
	for i := 0; i < 60; i++ {
		if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos").Run() == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres container did not become ready:\n%s", runOutput(t, "docker", "logs", container))
}

func applyMigrations(t *testing.T, container string) {
	t.Helper()
	root := repoRoot(t)
	migrations, err := filepath.Glob(filepath.Join(root, "backend", "migrations", "postgres", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(migrations)
	for _, migration := range migrations {
		sqlBytes, err := os.ReadFile(migration)
		if err != nil {
			t.Fatal(err)
		}
		psql(t, container, extractGooseUp(string(sqlBytes)))
	}
}

func openPool(t *testing.T, ctx context.Context, container string) *pgxpool.Pool {
	t.Helper()
	out := runOutput(t, "docker", "port", container, "5432/tcp")
	parts := strings.Split(strings.TrimSpace(out), ":")
	port := parts[len(parts)-1]
	url := "postgres://postgres:goatos@127.0.0.1:" + port + "/goatos?sslmode=disable"
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}

func psql(t *testing.T, container, sqlText string) {
	t.Helper()
	cmd := exec.Command("docker", "exec", "-i", container, "psql", "-v", "ON_ERROR_STOP=1", "-h", "127.0.0.1", "-U", "postgres", "-d", "goatos")
	cmd.Stdin = strings.NewReader(sqlText)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("psql failed: %v\n%s", err, stderr.String())
	}
}

func extractGooseUp(sqlText string) string {
	var out []string
	inUp := false
	for _, line := range strings.Split(sqlText, "\n") {
		if strings.HasPrefix(line, "-- +goose Up") {
			inUp = true
			continue
		}
		if strings.HasPrefix(line, "-- +goose Down") {
			break
		}
		if inUp {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "backend", "migrations", "postgres")); err == nil {
			return wd
		}
		next := filepath.Dir(wd)
		if next == wd {
			t.Fatal("repo root not found")
		}
		wd = next
	}
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
}

func runOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, stderr.String())
	}
	return string(out)
}
