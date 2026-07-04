package main

import (
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
)

func TestNormalizeEmailsDedupesAndSorts(t *testing.T) {
	got, err := normalizeEmails([]string{" Ravi@Mesha.SG ", "abhishek@mesha.sg", "ravi@mesha.sg"})
	if err != nil {
		t.Fatalf("normalizeEmails: %v", err)
	}
	want := []string{"abhishek@mesha.sg", "ravi@mesha.sg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeEmails=%#v want %#v", got, want)
	}
}

func TestNormalizeEmailsRejectsInvalidEmail(t *testing.T) {
	if _, err := normalizeEmails([]string{"not an email"}); err == nil {
		t.Fatal("invalid email accepted")
	}
}

func TestValidRole(t *testing.T) {
	for _, role := range []string{permissions.RoleAdmin, permissions.RoleVerifier, permissions.RoleParkHead, permissions.RolePCDirector, permissions.RoleOperator, permissions.RoleCEOInternal} {
		if !validRole(role) {
			t.Fatalf("valid role rejected: %s", role)
		}
	}
	if validRole("owner") {
		t.Fatal("invalid role accepted")
	}
}

func TestValidateTargetAllowsExplicitDevCloudSQL(t *testing.T) {
	t.Setenv("GOATOS_ALLOW_DEV_CLOUDSQL_TARGET", "true")
	t.Setenv("GOATOS_DEV_CLOUDSQL_CONNECTION_NAME", "goatos-dev:asia-south1:goatos-dev-core-db")
	url := "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-dev:asia-south1:goatos-dev-core-db sslmode=disable"
	if err := validateTarget("dev", url); err != nil {
		t.Fatalf("dev Cloud SQL target rejected: %v", err)
	}
}

func TestValidateTargetRejectsUnsafeCloudSQL(t *testing.T) {
	t.Setenv("GOATOS_ALLOW_DEV_CLOUDSQL_TARGET", "true")
	t.Setenv("GOATOS_DEV_CLOUDSQL_CONNECTION_NAME", "goatos-dev:asia-south1:goatos-dev-core-db")
	url := "user=postgres password=goatos dbname=goatos host=/cloudsql/goatos-prod:asia-south1:goatos-prod-core-db sslmode=disable"
	if err := validateTarget("dev", url); err == nil {
		t.Fatal("unsafe Cloud SQL target accepted")
	}
}
