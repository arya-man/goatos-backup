package main

import "testing"

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
}

func TestIsLocalHostAllowsOnlyKnownLocalSocketDirs(t *testing.T) {
	allowed := []string{
		"/tmp/.s.PGSQL.5432",
		"/private/tmp/.s.PGSQL.5432",
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
