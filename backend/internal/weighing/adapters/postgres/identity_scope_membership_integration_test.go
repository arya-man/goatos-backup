package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func TestAnimalIdentityMembershipDualRFIDReassignmentAndUntrustedTags(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoat, "G-970001", idScopePrimary, idScopeSecondary, "active")
	seedDoubleTaggedGoat(t, ctx, pool, idScopeGoatTwo, "G-970002", "rfid-primary-002", "rfid-secondary-002", "active")
	day := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	seedGrowthObservation(t, ctx, pool, idScopeSecondary, 20, day)
	assertMap := func(from, to time.Time, want map[string]string) {
		t.Helper()
		got, err := ResolveAnimalIdentityMap(ctx, pool, repoTenant, []string{repoPark}, from, to)
		if err != nil {
			t.Fatal(err)
		}
		actual := map[string]string{}
		for i, tag := range got.Tags {
			if _, exists := actual[tag]; exists {
				t.Fatalf("duplicate map tag %q", tag)
			}
			actual[tag] = got.CanonicalTags[i]
		}
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("identity map = %v, want %v", actual, want)
		}
	}
	// Scanning only the secondary must include its unscanned primary, but no unweighed goat.
	assertMap(day, day.Add(time.Hour), map[string]string{idScopePrimary: idScopePrimary, idScopeSecondary: idScopePrimary})
	assertMap(day.Add(time.Microsecond), day.Add(time.Hour), map[string]string{})
	assertMap(day.Add(-time.Hour), day, map[string]string{})
	// The schema forbids duplicate lifetime tag values. A supported reassignment updates the
	// existing identity row; the historical scan must now resolve through its current owner.
	execWeighingTestSQL(t, ctx, pool, `UPDATE goat_identifiers SET goat_id=$1::uuid WHERE tenant_id=$2::uuid AND identifier_value=$3`, idScopeGoatTwo, repoTenant, idScopeSecondary)
	assertMap(time.Time{}, time.Time{}, map[string]string{
		idScopeSecondary: "rfid-primary-002", "rfid-primary-002": "rfid-primary-002", "rfid-secondary-002": "rfid-primary-002",
	})
	// Rejected identity evidence is represented by disputed/duplicate/invalid identifier status.
	// Each must exclude the scanned tag completely, rather than merging historical observations.
	for _, status := range []string{"disputed", "duplicate", "invalid", "retired"} {
		execWeighingTestSQL(t, ctx, pool, `UPDATE goat_identifiers SET status=$1 WHERE tenant_id=$2::uuid AND identifier_value=$3`, status, repoTenant, idScopeSecondary)
		assertMap(time.Time{}, time.Time{}, map[string]string{})
	}
}
