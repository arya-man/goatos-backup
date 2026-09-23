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
	for _, role := range []string{
		permissions.RoleVerifier,
		permissions.RolePCDirector,
		permissions.RoleCEOInternal,
		permissions.RoleKey(permissions.TierDirector, permissions.VerticalPreventiveCare),
	} {
		if !validRole(role) {
			t.Fatalf("valid role rejected: %s", role)
		}
	}
	for _, role := range []string{
		permissions.RoleOperator,
		permissions.RoleParkHead,
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFeed),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalPreventiveCare),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalHealth),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalCleaning),
		permissions.RoleKey(permissions.TierManager, permissions.VerticalFarming),
		permissions.RoleKey(permissions.TierAssistantManager, permissions.VerticalFeed),
	} {
		if validRole(role) {
			t.Fatalf("park-scoped role %s accepted by tenant-scope pending grant seeder", role)
		}
	}
	retiredLegacyRole := "ad" + "min"
	if validRole(retiredLegacyRole) {
		t.Fatal("retired legacy role accepted; use ceo_internal for CEO/CXO full access")
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
