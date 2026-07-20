package main

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
)

func TestValidRoleAcceptsLegacyAndCompositeOrgRoles(t *testing.T) {
	for _, role := range []string{
		permissions.RoleAdmin, permissions.RoleVerifier, permissions.RoleParkHead,
		permissions.RolePCDirector, permissions.RoleOperator, permissions.RoleCEOInternal,
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalHealth),
	} {
		if !validRole(role) {
			t.Fatalf("validRole(%q)=false, want true", role)
		}
	}
	for _, role := range []string{"", "not_a_role", "manager_atlantis"} {
		if validRole(role) {
			t.Fatalf("validRole(%q)=true, want false", role)
		}
	}
}

func TestDevMemberRoleHintForCompositeOrgRoles(t *testing.T) {
	am := permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFeed)
	if got := devMemberRoleHint(am); got != "operator" {
		t.Fatalf("devMemberRoleHint(%q)=%q, want operator", am, got)
	}
	for _, tier := range []permissions.Tier{permissions.TierManager, permissions.TierHead, permissions.TierDirector} {
		role := permissions.RoleKey(tier, permissions.VerticalHealth)
		if got := devMemberRoleHint(role); got != "supervisor" {
			t.Fatalf("devMemberRoleHint(%q)=%q, want supervisor", role, got)
		}
	}
}

func TestValidateLocalTargetAllowsLocalDatabase(t *testing.T) {
	if err := validateLocalTarget("local", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"); err != nil {
		t.Fatalf("local target rejected: %v", err)
	}
}

func TestValidateLocalTargetRejectsMissingEnvironment(t *testing.T) {
	if err := validateLocalTarget("", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"); err == nil {
		t.Fatal("missing environment accepted")
	}
}

func TestValidateLocalTargetRejectsProductionLikeTarget(t *testing.T) {
	if err := validateLocalTarget("production", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"); err == nil {
		t.Fatal("production env accepted")
	}
	if err := validateLocalTarget("local", "postgres://postgres:goatos@db.prod.example.com:5432/goatos?sslmode=require"); err == nil {
		t.Fatal("production-looking database URL accepted")
	}
}

func TestValidateLocalTargetRejectsRemoteHost(t *testing.T) {
	if err := validateLocalTarget("local", "postgres://postgres:goatos@192.0.2.10:5432/goatos?sslmode=disable"); err == nil {
		t.Fatal("remote host accepted")
	}
	if err := validateLocalTarget("local", "postgres://postgres:goatos@goatos-stg.internal:5432/goatos?sslmode=disable"); err == nil {
		t.Fatal("staging-looking remote host accepted")
	}
}

func TestValidateLocalTargetRejectsCloudSQLSocketURL(t *testing.T) {
	databaseURL := "user=postgres password=goatos dbname=goatos host=/cloudsql/project:region:instance sslmode=disable"
	if err := validateLocalTarget("local", databaseURL); err == nil {
		t.Fatal("cloud sql socket database URL accepted")
	}
}

func TestResolveGrantUserIDUsesUUIDDirectly(t *testing.T) {
	got, err := resolveGrantUserID("90000000-0000-4000-8000-000000000001", "", "")
	if err != nil {
		t.Fatalf("resolveGrantUserID: %v", err)
	}
	if got != "90000000-0000-4000-8000-000000000001" {
		t.Fatalf("user_id=%s", got)
	}
}

func TestResolveGrantUserIDMapsExternalSubject(t *testing.T) {
	const issuer = "https://securetoken.google.com/goatos-dev"
	const externalSubject = "firebase-uid-abc123"
	got, err := resolveGrantUserID("", externalSubject, issuer)
	if err != nil {
		t.Fatalf("resolveGrantUserID: %v", err)
	}
	if got != platformauth.StableSubjectID(issuer, externalSubject) {
		t.Fatalf("user_id=%s", got)
	}
}

func TestResolveGrantUserIDRejectsAmbiguousOrMissingInputs(t *testing.T) {
	if _, err := resolveGrantUserID("", "", "issuer"); err == nil {
		t.Fatal("missing user inputs accepted")
	}
	if _, err := resolveGrantUserID("90000000-0000-4000-8000-000000000001", "firebase-uid", "issuer"); err == nil {
		t.Fatal("ambiguous user inputs accepted")
	}
	if _, err := resolveGrantUserID("", "firebase-uid", ""); err == nil {
		t.Fatal("external subject without issuer accepted")
	}
}

func TestValidateGrantScopeMode(t *testing.T) {
	if err := validateGrantScopeMode("00000000-0000-4000-8000-000000003001", true); err != nil {
		t.Fatalf("park-only with park id rejected: %v", err)
	}
	if err := validateGrantScopeMode("", false); err != nil {
		t.Fatalf("tenant-wide default rejected: %v", err)
	}
	if err := validateGrantScopeMode("", true); err == nil {
		t.Fatal("park-only without park id accepted")
	}
}

func TestIsLocalHostAllowsOnlyKnownLocalSocketDirs(t *testing.T) {
	allowed := []string{
		"/tmp/.s.PGSQL.5432",
		"/private/tmp/.s.PGSQL.5432",
		"/run/postgresql/.s.PGSQL.5432",
		"/var/run/postgresql/.s.PGSQL.5432",
	}
	for _, host := range allowed {
		if !isLocalHost(host) {
			t.Fatalf("local socket host rejected: %s", host)
		}
	}

	rejected := []string{
		"/cloudsql/project:region:instance",
		"/var/run/cloudsql/project:region:instance",
		"/opt/postgres/.s.PGSQL.5432",
	}
	for _, host := range rejected {
		if isLocalHost(host) {
			t.Fatalf("non-local socket host accepted: %s", host)
		}
	}
}
