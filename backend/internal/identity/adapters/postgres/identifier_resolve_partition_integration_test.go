package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// /identifiers/{type}/{value}/resolve failed on EVERY call on stg: FindIdentifierMatches selected
// goatSummaryColumns() (which reads gsp.partition_label / gsp.source_shed_name) without joining
// goat_shed_partitions gsp. This pins the join and the partition half of the operational location.
func TestFindIdentifierMatchesCarriesPartition(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	f := seedRelocatePartitionFixture(t, ctx, pool)
	partitioned := seedRelocateGoat(t, ctx, pool, f.castroShed)
	seedRelocateGoatPartition(t, ctx, pool, partitioned, f.castroShed, "1", "Castro 1")
	undivided := seedRelocateGoat(t, ctx, pool, f.yashodaShed)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', 'RES-PART-1', 'RES-PART-1', 'global', true, 'active', now(), 'test_v1'),
       ($1::uuid, $3::uuid, 'animal_identifier_1', 'RES-WHOLE-1', 'RES-WHOLE-1', 'global', true, 'active', now(), 'test_v1')`,
		rpTenant, partitioned, undivided); err != nil {
		t.Fatalf("seed identifiers: %v", err)
	}

	for _, tc := range []struct {
		value, goat, label, display string
	}{
		{"RES-PART-1", partitioned, "1", "Castro 1"},
		{"RES-WHOLE-1", undivided, "", "Yashoda"},
	} {
		matches, err := repo.FindIdentifierMatches(ctx, ports.ResolveIdentifierParams{
			TenantID: rpTenant, IdentifierType: "animal_identifier_1", NormalizedValue: tc.value,
		})
		if err != nil {
			t.Fatalf("FindIdentifierMatches(%s): %v", tc.value, err)
		}
		if len(matches) != 1 || matches[0].Goat.GoatID != tc.goat {
			t.Fatalf("FindIdentifierMatches(%s) = %#v, want goat %s", tc.value, matches, tc.goat)
		}
		loc := matches[0].Goat.LocationPath
		gotLabel := ""
		if loc.PartitionLabel != nil {
			gotLabel = *loc.PartitionLabel
		}
		if gotLabel != tc.label {
			t.Fatalf("%s partition_label = %q, want %q", tc.value, gotLabel, tc.label)
		}
		if loc.OperationalLocationDisplay != tc.display {
			t.Fatalf("%s operational_location_display = %q, want %q", tc.value, loc.OperationalLocationDisplay, tc.display)
		}
	}
}
