package main

import (
	"strings"
	"testing"
)

func TestParseFlagsRequiresTenantID(t *testing.T) {
	t.Setenv("GOATOS_TENANT_ID", "")
	_, err := parseFlags([]string{"-timeout", "1s"})
	if err == nil || !strings.Contains(err.Error(), "tenant-id is required") {
		t.Fatalf("parseFlags err = %v, want tenant-id required", err)
	}
}
