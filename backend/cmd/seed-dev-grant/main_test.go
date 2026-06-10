package main

import "testing"

func TestValidateLocalTargetAllowsLocalDatabase(t *testing.T) {
	if err := validateLocalTarget("local", "postgres://postgres:goatos@localhost:5432/goatos?sslmode=disable"); err != nil {
		t.Fatalf("local target rejected: %v", err)
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
