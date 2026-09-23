package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestCancelGoatDoseCTEProjectsTenantUsedByDriveGuard(t *testing.T) {
	b, err := os.ReadFile("repository.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	start := strings.Index(s, "WITH open_goat_obligations AS MATERIALIZED")
	if start < 0 {
		t.Fatal("cancel-goat-dose SQL start not found")
	}
	end := strings.Index(s[start:], "UPDATE obligation_instances oi")
	if end < 0 {
		t.Fatal("cancel-goat-dose SQL shape not found")
	}
	query := s[start : start+end]
	if !strings.Contains(query, "SELECT tenant_id, obligation_id") ||
		!strings.Contains(query, "vdam.tenant_id = oi.tenant_id") {
		t.Fatal("cancel-goat-dose CTE must project tenant_id before its drive-membership guard uses oi.tenant_id")
	}
}
