package postgres

import (
	"database/sql"
	"testing"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

func TestApplyLocationPartitionNonPartitionedShedStaysBare(t *testing.T) {
	loc := domain.LocationPath{
		OperationalLocationDisplay: "Yashoda",
		ShedID:                     strPtr("shed-yashoda"),
		ShedName:                   strPtr("Yashoda"),
	}
	applyLocationPartition(&loc, nullStr(""), nullStr(""))
	if loc.OperationalLocationDisplay != "Yashoda" {
		t.Fatalf("Display = %q, want bare shed name, never a 'whole' sentinel", loc.OperationalLocationDisplay)
	}
	if loc.PartitionLabel != nil {
		t.Fatalf("PartitionLabel = %v, want nil for a non-partitioned shed", loc.PartitionLabel)
	}
	if loc.OperationalLocationDisplay != "Yashoda" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", loc.OperationalLocationDisplay, "Yashoda")
	}
}

func TestApplyLocationPartitionComposesPartitionSuffix(t *testing.T) {
	loc := domain.LocationPath{
		OperationalLocationDisplay: "Castro",
		ShedID:                     strPtr("shed-castro"),
		ShedName:                   strPtr("Castro"),
	}
	applyLocationPartition(&loc, nullStr("2"), nullStr("Castro 2"))
	if loc.OperationalLocationDisplay != "Castro 2" {
		t.Fatalf("Display = %q, want %q", loc.OperationalLocationDisplay, "Castro 2")
	}
	if loc.PartitionLabel == nil || *loc.PartitionLabel != "2" {
		t.Fatalf("PartitionLabel = %v, want \"2\"", loc.PartitionLabel)
	}
	if loc.SourceShedName == nil || *loc.SourceShedName != "Castro 2" {
		t.Fatalf("SourceShedName = %v, want %q", loc.SourceShedName, "Castro 2")
	}
	if loc.OperationalLocationDisplay != "Castro 2" {
		t.Fatalf("OperationalLocationDisplay = %q, want %q", loc.OperationalLocationDisplay, "Castro 2")
	}
}

func TestApplyLocationPartitionPartPrefixConvention(t *testing.T) {
	loc := domain.LocationPath{
		OperationalLocationDisplay: "Godel 1",
		ShedID:                     strPtr("shed-godel-1"),
		ShedName:                   strPtr("Godel 1"),
	}
	applyLocationPartition(&loc, nullStr("Part 3"), nullStr("Godel 1 - Part 3"))
	if loc.OperationalLocationDisplay != "Godel 1 - Part 3" {
		t.Fatalf("Display = %q, want %q", loc.OperationalLocationDisplay, "Godel 1 - Part 3")
	}
}

// TestApplyLocationPartitionShedlessRowUnaffected covers an animal with no shed: it must not
// crash and must not fabricate a shed-based display.
func TestApplyLocationPartitionShedlessRowUnaffected(t *testing.T) {
	loc := domain.LocationPath{OperationalLocationDisplay: "Unknown location"}
	applyLocationPartition(&loc, nullStr(""), nullStr(""))
	if loc.OperationalLocationDisplay != "Unknown location" {
		t.Fatalf("Display = %q, want unchanged %q", loc.OperationalLocationDisplay, "Unknown location")
	}
	if loc.PartitionLabel != nil {
		t.Fatalf("PartitionLabel = %v, want nil", loc.PartitionLabel)
	}
}
