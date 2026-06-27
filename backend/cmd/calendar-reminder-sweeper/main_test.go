package main

import (
	"strings"
	"testing"
)

func TestRunRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	t.Setenv("DATABASE_URL", "")
	err := run([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("run err = %v, want tenant-id required", err)
	}
}
