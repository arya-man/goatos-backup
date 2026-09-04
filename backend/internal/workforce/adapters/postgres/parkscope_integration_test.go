package postgres

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/parkscope"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	scopeTenant = "00000000-0000-4000-8000-000000000001"
	scopeParkA  = "94000000-0000-4000-8000-00000000000a"
	scopeParkB  = "94000000-0000-4000-8000-00000000000b"
	scopeMember = "94000000-0000-4000-8000-000000000001"
	scopeUser   = "94000000-0000-4000-8000-000000000002"
	scopeActor  = "94000000-0000-4000-8000-000000000009"
)

// grantRow is (role, scope_type, scope_id) of one ACTIVE grant, the shape every assertion
// below compares against, so a test reads "these exact rows and no others".
type grantRow struct{ role, scopeType, scopeID string }

func activeGrants(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID string) []grantRow {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT role, scope_type, scope_id::text FROM user_scope_grants
WHERE tenant_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
ORDER BY role, scope_type, scope_id`, scopeTenant, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []grantRow
	for rows.Next() {
		var g grantRow
		if err := rows.Scan(&g.role, &g.scopeType, &g.scopeID); err != nil {
			t.Fatal(err)
		}
		out = append(out, g)
	}
	return out
}

func sameRows(a, b []grantRow) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Slice(a, func(i, j int) bool {
		return a[i].role+a[i].scopeType+a[i].scopeID < a[j].role+a[j].scopeType+a[j].scopeID
	})
	sort.Slice(b, func(i, j int) bool {
		return b[i].role+b[i].scopeType+b[i].scopeID < b[j].role+b[j].scopeType+b[j].scopeID
	})
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func seedScopeFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	for _, park := range []struct{ id, code, name string }{{scopeParkA, "PSA", "Park A"}, {scopeParkB, "PSB", "Park B"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $4, 'active')`, park.id, scopeTenant, park.code, park.name); err != nil {
			t.Fatalf("seed park: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'SCOPE-01', 'Scope Person', 'active', 'operator', $4::uuid)`,
		scopeMember, scopeTenant, scopeUser, scopeParkA); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	// The drift the fix replaces: grants say both parks, ticks say nothing yet, home is A.
	for _, park := range []string{scopeParkA, scopeParkB} {
		if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`, scopeTenant, scopeUser, park); err != nil {
			t.Fatalf("seed grant: %v", err)
		}
	}
}

func homePark(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var home *string
	if err := pool.QueryRow(ctx, `SELECT primary_location_id::text FROM workforce_members WHERE workforce_member_id = $1::uuid`, scopeMember).Scan(&home); err != nil {
		t.Fatal(err)
	}
	if home == nil {
		return ""
	}
	return *home
}

// The People screen is the ONE author of park scope: saving it rewrites the grant rows and
// the home park in the same transaction (maintainer decision 2026-09-04).
func TestSavePersonAccessDerivesGrantsAndHomeParkFromTheTicks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	repo := NewAccessRepository(pool)

	// Narrow to park B only: the park A grant must go, and the home park must follow.
	rec, err := repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkB}, HomeParkID: scopeParkB,
		Assignments: []permissions.ModuleAssignment{{Module: "counts", Surface: permissions.SurfaceMobile, Capabilities: []string{permissions.LevelDo}}},
	})
	if err != nil {
		t.Fatalf("save parks: %v", err)
	}
	if rec.HomeParkID != scopeParkB || homePark(t, ctx, pool) != scopeParkB {
		t.Fatalf("home park = %q (db %q), want park B", rec.HomeParkID, homePark(t, ctx, pool))
	}
	want := []grantRow{{"operator", "park", scopeParkB}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after narrowing to B = %v, want %v", got, want)
	}

	// Widen to both parks with A as home: one row per park, home back on A.
	rec, err = repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkA, scopeParkB}, HomeParkID: scopeParkA,
		ExpectedRowVersion: rec.RowVersion,
	})
	if err != nil {
		t.Fatalf("save both: %v", err)
	}
	want = []grantRow{{"operator", "park", scopeParkA}, {"operator", "park", scopeParkB}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after both parks = %v, want %v", got, want)
	}
	if homePark(t, ctx, pool) != scopeParkA {
		t.Fatalf("home park after both = %q, want park A", homePark(t, ctx, pool))
	}

	// Tenant mode: every role becomes one tenant row; the seat is kept as the home park.
	if _, err = repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "tenant", HomeParkID: scopeParkA, ExpectedRowVersion: rec.RowVersion,
	}); err != nil {
		t.Fatalf("save tenant: %v", err)
	}
	want = []grantRow{{"operator", "tenant", scopeTenant}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after tenant = %v, want %v", got, want)
	}
	var parkRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM person_park_scope WHERE workforce_member_id = $1::uuid`, scopeMember).Scan(&parkRows); err != nil {
		t.Fatal(err)
	}
	if parkRows != 0 {
		t.Fatalf("tenant mode left %d park rows", parkRows)
	}
}

// The operators grant API adds a ROLE; the person's ticks decide where it applies, and a
// body naming another park is not honoured.
func TestCreateGrantLandsTheRoleOnTheAuthoredScopeNotTheBody(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	access := NewAccessRepository(pool)
	if _, err := access.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkA}, HomeParkID: scopeParkA,
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool, 30*time.Second)
	if _, err := repo.CreateGrant(ctx, ports.CreateGrantCommand{
		TenantID: scopeTenant, ActorID: scopeActor, OperatorID: scopeMember,
		Body: domain.CreateGrantRequest{Role: permissions.RoleParkHead, ScopeType: "park", ScopeID: scopeParkB},
	}); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}, {"park_head", "park", scopeParkA}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after park_head on a park-A person = %v, want %v (the body's park B must not win)", got, want)
	}
}

// A person never set up on the People screen is set up FROM the grant request, so the
// two records agree from the first row.
func TestCreateGrantOnAnUnsetPersonAuthorsTheScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE user_scope_grants SET status = 'revoked' WHERE user_id = $1::uuid`, scopeUser); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(pool, 30*time.Second)
	if _, err := repo.CreateGrant(ctx, ports.CreateGrantCommand{
		TenantID: scopeTenant, ActorID: scopeActor, OperatorID: scopeMember,
		Body: domain.CreateGrantRequest{Role: permissions.RoleOperator, ScopeType: "park", ScopeID: scopeParkB},
	}); err != nil {
		t.Fatalf("create grant: %v", err)
	}
	want := []grantRow{{"operator", "park", scopeParkB}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants = %v, want %v", got, want)
	}
	mode, parks, provisioned, err := NewAccessRepository(pool).ResolveParkScope(ctx, scopeTenant, scopeUser)
	if err != nil || !provisioned || mode != "parks" || len(parks) != 1 || parks[0] != scopeParkB {
		t.Fatalf("ticks after grant = (%q, %v, %v, %v), want parks=[B]", mode, parks, provisioned, err)
	}
	if homePark(t, ctx, pool) != scopeParkB {
		t.Fatalf("home park = %q, want park B", homePark(t, ctx, pool))
	}
}

// A writer that only knows the user (the login-time email claim) can add a tenant row; the
// reconcile pulls it back onto the authored scope, so a login never widens a narrowed person.
func TestReconcileUserPullsAStrayTenantRowBackOntoTheTicks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	if _, err := NewAccessRepository(pool).SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkA}, HomeParkID: scopeParkA,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'verifier', 'tenant', $1::uuid, 'active', now())`, scopeTenant, scopeUser); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	res, provisioned, err := parkscope.ReconcileUser(ctx, tx, scopeTenant, scopeUser, "")
	if err != nil || !provisioned {
		t.Fatalf("reconcile = (%v, %v, %v)", res, provisioned, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}, {"verifier", "park", scopeParkA}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after reconcile = %v, want %v", got, want)
	}
}
