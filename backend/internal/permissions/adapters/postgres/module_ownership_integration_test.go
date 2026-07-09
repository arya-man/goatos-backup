package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

// User identities exercised by the module-ownership read. Distinct from the
// grants_integration_test constants to avoid collisions in the same package.
const (
	moVaccUser     = "91000000-0000-4000-8000-000000000001"
	moLeadUser     = "91000000-0000-4000-8000-000000000002"
	moNoDeptUser   = "91000000-0000-4000-8000-000000000003"
	moInactiveUser = "91000000-0000-4000-8000-000000000004"
	moNoMemberUser = "91000000-0000-4000-8000-000000000005"
	moClaimUser    = "91000000-0000-4000-8000-000000000006"
	moPresetUser   = "91000000-0000-4000-8000-000000000007"
)

func TestModuleOwnershipSourceResolvesOwnedModulesByDepartment(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	container := fmt.Sprintf("goatos-module-ownership-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	waitForPostgres(t, container)
	applyMigrations(t, container) // includes 000148 + 000149 (departments + grants seeded).

	// Seed members. department_id resolves by (tenant, code) so we never hardcode
	// the gen_random_uuid() department ids.
	psql(t, container, `
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint, department_id) VALUES
  ('`+meshaTenant+`', '`+moVaccUser+`', 'MO-VACC-01', 'Vacc Operator', 'active', 'operator',
    (SELECT department_id FROM departments WHERE tenant_id='`+meshaTenant+`' AND code='vaccination')),
  ('`+meshaTenant+`', '`+moLeadUser+`', 'MO-LEAD-01', 'Leadership', 'active', 'admin',
    (SELECT department_id FROM departments WHERE tenant_id='`+meshaTenant+`' AND code='leadership')),
  ('`+meshaTenant+`', '`+moNoDeptUser+`', 'MO-NODEPT-01', 'No Department', 'active', 'operator', NULL),
  ('`+meshaTenant+`', '`+moInactiveUser+`', 'MO-INACTIVE-01', 'Inactive Member', 'inactive', 'operator',
    (SELECT department_id FROM departments WHERE tenant_id='`+meshaTenant+`' AND code='vaccination'));
`)

	pool := openPool(t, ctx, container)
	defer pool.Close()
	source := NewModuleOwnershipSource(pool, 5*time.Second)

	// Vaccination department -> exactly one owned module -> no sidebar.
	vacc, err := source.ListActiveModuleGrantsForActor(ctx, moVaccUser, meshaTenant)
	if err != nil {
		t.Fatalf("ListActiveModuleGrantsForActor(vacc): %v", err)
	}
	if got := modulesOf(vacc); len(got) != 1 || got[0] != "pc.vaccination" {
		t.Fatalf("vaccination modules = %#v, want [pc.vaccination]", got)
	}

	// Leadership department -> all four built modules (ordered vertical, module).
	lead, err := source.ListActiveModuleGrantsForActor(ctx, moLeadUser, meshaTenant)
	if err != nil {
		t.Fatalf("ListActiveModuleGrantsForActor(lead): %v", err)
	}
	wantLead := []string{"admin.audit", "admin.config", "admin.sop", "pc.vaccination"}
	if got := modulesOf(lead); !equalStrings(got, wantLead) {
		t.Fatalf("leadership modules = %#v, want %#v", got, wantLead)
	}

	// No department, inactive member, and no member at all all resolve to empty —
	// ownership never leaks across these boundaries.
	for name, userID := range map[string]string{
		"no_department": moNoDeptUser,
		"inactive":      moInactiveUser,
		"no_member":     moNoMemberUser,
	} {
		got, err := source.ListActiveModuleGrantsForActor(ctx, userID, meshaTenant)
		if err != nil {
			t.Fatalf("ListActiveModuleGrantsForActor(%s): %v", name, err)
		}
		if len(got) != 0 {
			t.Fatalf("%s owned modules = %#v, want empty", name, modulesOf(got))
		}
	}
}

func TestClaimProvisionsDepartmentMemberFromEmailGrant(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	container := fmt.Sprintf("goatos-provision-test-%d", time.Now().UnixNano())
	postgresImage := os.Getenv("GOATOS_POSTGRES_IMAGE")
	if postgresImage == "" {
		postgresImage = defaultPostgresImage
	}
	run(t, "docker", "run", "--rm", "--name", container, "-e", "POSTGRES_PASSWORD=goatos", "-e", "POSTGRES_DB=goatos", "-p", "127.0.0.1::5432", "-d", postgresImage)
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
	})
	waitForPostgres(t, container)
	applyMigrations(t, container) // 000149 seeds departments; 000150 adds department_code.

	// An approved leadership email grant carrying its HR department code, plus a
	// preset member already in the vaccination department (to prove no-override).
	psql(t, container, `
INSERT INTO auth_pending_email_grants (tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source, department_code)
VALUES ('`+meshaTenant+`', 'lead@mesha.sg', 'lead@mesha.sg', 'ceo_internal', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 minute', 'test', 'leadership'),
       ('`+meshaTenant+`', 'preset@mesha.sg', 'preset@mesha.sg', 'admin', 'tenant', '`+meshaTenant+`', 'active', now() - interval '1 minute', 'test', 'leadership');
INSERT INTO workforce_members (tenant_id, user_id, display_code, display_name, status, primary_role_hint, department_id)
VALUES ('`+meshaTenant+`', '`+moPresetUser+`', 'PRESET-01', 'Preset Member', 'active', 'admin',
  (SELECT department_id FROM departments WHERE tenant_id='`+meshaTenant+`' AND code='vaccination'));
`)

	pool := openPool(t, ctx, container)
	defer pool.Close()
	claimer := NewPendingEmailGrantClaimer(pool, 5*time.Second)
	ownership := NewModuleOwnershipSource(pool, 5*time.Second)

	claim := permissions.PendingEmailGrantClaim{
		TenantID: meshaTenant, UserID: moClaimUser, Email: "lead@mesha.sg",
		ExternalSubject: "firebase-lead", Issuer: "https://securetoken.google.com/goatos-dev",
		Source: "admin-web", TraceID: "trace-provision",
	}
	if _, err := claimer.ClaimPendingEmailGrant(ctx, claim); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// A member now exists in the leadership department and owns all four modules.
	lead, err := ownership.ListActiveModuleGrantsForActor(ctx, moClaimUser, meshaTenant)
	if err != nil {
		t.Fatalf("owned modules after claim: %v", err)
	}
	wantLead := []string{"admin.audit", "admin.config", "admin.sop", "pc.vaccination"}
	if got := modulesOf(lead); !equalStrings(got, wantLead) {
		t.Fatalf("provisioned member owned modules = %#v, want %#v", got, wantLead)
	}
	if n := activeMemberCount(t, ctx, pool, moClaimUser); n != 1 {
		t.Fatalf("provisioned member count = %d, want 1", n)
	}

	// Idempotent: a second claim provisions nothing new.
	if _, err := claimer.ClaimPendingEmailGrant(ctx, claim); err != nil {
		t.Fatalf("idempotent claim: %v", err)
	}
	if n := activeMemberCount(t, ctx, pool, moClaimUser); n != 1 {
		t.Fatalf("member count after re-claim = %d, want 1", n)
	}

	// No-override: the preset member keeps its vaccination department, not leadership.
	presetClaim := permissions.PendingEmailGrantClaim{
		TenantID: meshaTenant, UserID: moPresetUser, Email: "preset@mesha.sg",
		ExternalSubject: "firebase-preset", Issuer: "https://securetoken.google.com/goatos-dev",
		Source: "admin-web", TraceID: "trace-preset",
	}
	if _, err := claimer.ClaimPendingEmailGrant(ctx, presetClaim); err != nil {
		t.Fatalf("preset claim: %v", err)
	}
	preset, err := ownership.ListActiveModuleGrantsForActor(ctx, moPresetUser, meshaTenant)
	if err != nil {
		t.Fatalf("preset owned modules: %v", err)
	}
	if got := modulesOf(preset); len(got) != 1 || got[0] != "pc.vaccination" {
		t.Fatalf("preset member modules = %#v, want [pc.vaccination] (department not overridden)", got)
	}
}

func activeMemberCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_members WHERE tenant_id=$1 AND user_id=$2 AND status='active'`, meshaTenant, userID).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	return n
}

// ownedByUser is a no-op placeholder kept for readability of the flow above.
func ownedByUser(t *testing.T, ctx context.Context, src *ModuleOwnershipSource) bool { return false }

func modulesOf(mods []permissions.OwnedModule) []string {
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		out = append(out, m.Module)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
