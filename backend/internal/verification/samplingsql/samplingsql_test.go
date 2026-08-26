package samplingsql

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// TestDefaultsMatchTheDomain pins the literals this package restates. They are duplicated so the
// SQL layer stays dependency-free; duplicated constants that can drift are only safe with a test
// standing over them.
func TestDefaultsMatchTheDomain(t *testing.T) {
	if DefaultSamplePercent != domain.DefaultSamplePercent {
		t.Fatalf("DefaultSamplePercent = %d, domain says %d", DefaultSamplePercent, domain.DefaultSamplePercent)
	}
	if BusinessTimezone != biztime.DefaultTimezone {
		t.Fatalf("BusinessTimezone = %q, biztime says %q", BusinessTimezone, biztime.DefaultTimezone)
	}
}

// TestInSampleUsesTheCallersAlias: every reference must carry the alias, or the predicate silently
// resolves against whatever table the caller happened to join.
func TestInSampleUsesTheCallersAlias(t *testing.T) {
	sql := InSample("items")
	for _, want := range []string{
		"items.sampling_bucket", "p.tenant_id = items.tenant_id",
		"p.category = items.category", "items.captured_at AT TIME ZONE",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("InSample missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "vi.") {
		t.Fatalf("InSample leaked the conventional alias into a caller using another:\n%s", sql)
	}
}

// TestDecidedByPersonIsTheComplementOfSettledByPolicy: the two are used on opposite sides of the
// same reads (what a human did vs what the policy settled), so a change that made them overlap or
// leave a gap would double-count or lose items.
func TestDecidedByPersonIsTheComplementOfSettledByPolicy(t *testing.T) {
	if DecidedByPerson("vi") != "vi.auto_resolution IS NULL" {
		t.Fatalf("DecidedByPerson = %q", DecidedByPerson("vi"))
	}
	if SettledByPolicy("vi") != "vi.auto_resolution IS NOT NULL" {
		t.Fatalf("SettledByPolicy = %q", SettledByPolicy("vi"))
	}
}
