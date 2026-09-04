package postgres

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/parkscope"
	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
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

	// Every park is refused while the person holds only park roles: an operator belongs
	// to a park, and a park head covering both parks is two ticks, never tenant scope.
	if _, err = repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "tenant", HomeParkID: scopeParkA, ExpectedRowVersion: rec.RowVersion,
	}); !errors.Is(err, parkscope.ErrParkRolesNeedAPark) {
		t.Fatalf("tenant mode for an operator-only person = %v, want ErrParkRolesNeedAPark", err)
	}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after refused tenant save = %v, want %v unchanged", got, want)
	}
	// With a director role layered on, tenant mode is allowed and every role becomes one
	// tenant row; the seat is kept as the home park.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'growth_director', 'tenant', $1::uuid, 'active', now())`, scopeTenant, scopeUser); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "tenant", HomeParkID: scopeParkA, ExpectedRowVersion: rec.RowVersion,
	}); err != nil {
		t.Fatalf("save tenant: %v", err)
	}
	want = []grantRow{{"growth_director", "tenant", scopeTenant}, {"operator", "tenant", scopeTenant}}
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
VALUES ($1::uuid, $2::uuid, 'park_head', 'tenant', $1::uuid, 'active', now())`, scopeTenant, scopeUser); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	res, provisioned, err := parkscope.ReconcileUser(ctx, tx, scopeTenant, scopeUser, "")
	if err != nil || !provisioned {
		t.Fatalf("reconcile = (%v, %v, %v)", res, provisioned, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}, {"park_head", "park", scopeParkA}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after reconcile = %v, want %v", got, want)
	}
}

// A tenant-only role claimed at login by a parks-mode person is left as written and
// reported as not reconciled: the login cannot be refused, and narrowing the role is the
// defect this package prevents. The next People-screen save is where the admin decides.
func TestReconcileUserLeavesATenantOnlyRoleAlone(t *testing.T) {
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
	defer func() { _ = tx.Rollback(ctx) }()
	_, reconciled, err := parkscope.ReconcileUser(ctx, tx, scopeTenant, scopeUser, "")
	if err != nil || reconciled {
		t.Fatalf("reconcile with a claimed verifier = (reconciled=%v, %v), want left alone with no error", reconciled, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}, {"verifier", "tenant", scopeTenant}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants = %v, want %v unchanged", got, want)
	}
}

// A verifier, a director or the CEO works across every park; the editor must refuse to
// narrow them rather than lock them out or show them half the herd.
func TestSavePersonAccessRefusesParksModeForATenantOnlyRole(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'verifier', 'tenant', $1::uuid, 'active', now())`, scopeTenant, scopeUser); err != nil {
		t.Fatal(err)
	}
	repo := NewAccessRepository(pool)
	_, err := repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkA}, HomeParkID: scopeParkA,
	})
	var tenantOnly *parkscope.TenantOnlyRoleError
	if !errors.As(err, &tenantOnly) || tenantOnly.Role != "verifier" {
		t.Fatalf("narrowing a verifier to parks = %v, want TenantOnlyRoleError{verifier}", err)
	}
	// Nothing moved: the transaction rolled back the ticks too.
	mode, _, provisioned, err := repo.ResolveParkScope(ctx, scopeTenant, scopeUser)
	if err != nil || provisioned {
		t.Fatalf("ticks after refused save = (%q, provisioned=%v, %v), want none written", mode, provisioned, err)
	}
	// Tenant mode is the one that works, and every role lands tenant-wide.
	if _, err := repo.SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember, ScopeMode: "tenant",
	}); err != nil {
		t.Fatalf("tenant save: %v", err)
	}
	want := []grantRow{{"operator", "tenant", scopeTenant}, {"verifier", "tenant", scopeTenant}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants = %v, want %v", got, want)
	}
}

// Adding a director role to a parks-mode person is refused by the grant API for the same
// reason, and the admin is told to widen the person first.
func TestCreateGrantRefusesATenantOnlyRoleOnAParksPerson(t *testing.T) {
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
	_, err := NewRepository(pool, 30*time.Second).CreateGrant(ctx, ports.CreateGrantCommand{
		TenantID: scopeTenant, ActorID: scopeActor, OperatorID: scopeMember,
		Body: domain.CreateGrantRequest{Role: permissions.RolePCDirector, ScopeType: "tenant", ScopeID: scopeTenant},
	})
	if !errors.Is(err, parkscope.ErrTenantOnlyRole) {
		t.Fatalf("pc_director on a parks person = %v, want ErrTenantOnlyRole", err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after refused create = %v, want %v unchanged", got, want)
	}
}

// The login-time claim runs end to end through the real claimer: a pending tenant grant
// for a park role lands on the person's ticked park, not tenant-wide.
func TestEmailClaimLandsOnTheAuthoredScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	if _, err := NewAccessRepository(pool).SavePersonAccess(ctx, ports.SavePersonAccessCommand{
		TenantID: scopeTenant, ActorID: scopeActor, PersonID: scopeMember,
		ScopeMode: "parks", ParkIDs: []string{scopeParkB}, HomeParkID: scopeParkB,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO auth_pending_email_grants (tenant_id, email, normalized_email, role, scope_type, scope_id, status, valid_from, source)
VALUES ($1::uuid, 'scope.person@example.com', 'scope.person@example.com', 'park_head', 'tenant', $1::uuid, 'active', now(), 'test')`, scopeTenant); err != nil {
		t.Fatal(err)
	}
	claimer := permissionspg.NewPendingEmailGrantClaimer(pool, 30*time.Second)
	res, err := claimer.ClaimPendingEmailGrant(ctx, permissions.PendingEmailGrantClaim{
		TenantID: scopeTenant, UserID: scopeUser, Email: "scope.person@example.com", ExternalSubject: "firebase:scope", Issuer: "test", Source: "test",
	})
	if err != nil || !res.Matched {
		t.Fatalf("claim = (%+v, %v)", res, err)
	}
	want := []grantRow{{"operator", "park", scopeParkB}, {"park_head", "park", scopeParkB}}
	if got := activeGrants(t, ctx, pool, scopeUser); !sameRows(got, want) {
		t.Fatalf("grants after claim = %v, want %v (the pending row's tenant scope must not win)", got, want)
	}
}

// Person creation from the People screen sets the ticks, the home park and the derived
// grant in one transaction.
func TestCreatePersonAuthorsTheScopeAndDerivesTheGrant(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedScopeFixture(t, ctx, pool)
	const newUser = "94000000-0000-4000-8000-000000000077"
	repo := NewRepository(pool, 30*time.Second)
	person, err := repo.CreatePerson(ctx, ports.CreatePersonCommand{
		TenantID: scopeTenant, ActorID: scopeActor, IdempotencyKey: "create-scope-person-1",
		UserID: newUser, FirstName: "New", LastName: "Operator", DisplayName: "New Operator",
		Email: "new.operator@example.com", NormalizedEmail: "new.operator@example.com",
		Role: permissions.RoleOperator, ScopeType: "park", ScopeID: scopeParkA, RoleHint: "operator", ParkID: scopeParkA,
	})
	if err != nil {
		t.Fatalf("create person: %v", err)
	}
	want := []grantRow{{"operator", "park", scopeParkA}}
	if got := activeGrants(t, ctx, pool, newUser); !sameRows(got, want) {
		t.Fatalf("grants = %v, want %v", got, want)
	}
	rec, err := NewAccessRepository(pool).LoadPersonAccess(ctx, scopeTenant, person.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ScopeMode != "parks" || len(rec.ParkIDs) != 1 || rec.ParkIDs[0] != scopeParkA || rec.HomeParkID != scopeParkA {
		t.Fatalf("access after create = mode %q parks %v home %q, want parks [A] home A", rec.ScopeMode, rec.ParkIDs, rec.HomeParkID)
	}
}
