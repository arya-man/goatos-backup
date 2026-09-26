package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCensusBreedCorrectionStaysInsideEachAnimalsSpecies pins the 2026-09-26 audit fix. The Counts
// dashboard now offers every species' breeds (it used to offer goat breeds only, so a sheep could
// never be corrected to its own breed), which makes the write the place that keeps a goat off a
// sheep breed:
//
//   - a breed some animal in the row's species does not carry is refused, and nothing moves;
//   - a name the farm keeps under two species links each animal to ITS species' row, never to
//     whichever row happened to sort first.
func TestCensusBreedCorrectionStaysInsideEachAnimalsSpecies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedCorrectionBreeds(t, ctx, pool)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	// Deccani is a sheep breed only; Nellore is kept under BOTH species.
	exec(`INSERT INTO breeds (tenant_id, species, canonical_name, status) VALUES
($1::uuid, 'sheep', 'Deccani', 'active'),
($1::uuid, 'sheep', 'Nellore', 'active'),
($1::uuid, 'goat', 'Nellore', 'active')
ON CONFLICT (tenant_id, species, canonical_name) DO UPDATE SET status = 'active'`, ssTenant)

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, goat, "Beetal", "female")

	// A goat cannot be corrected to a sheep-only breed.
	err := correctCensus(t, ctx, repo,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "female", "breed", "Deccani", "key-goat-to-sheep-breed"))
	if !errors.Is(err, ports.ErrCensusCorrectionValue) {
		t.Fatalf("goat corrected to a sheep-only breed: err = %v, want ErrCensusCorrectionValue", err)
	}
	if breed, _ := goatBreedSex(t, ctx, pool, goat); breed != "Beetal" {
		t.Fatalf("a refused correction still wrote breed=%q", breed)
	}

	// A breed kept under both species links the goat to the GOAT row.
	if err := correctCensus(t, ctx, repo,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "female", "breed", "Nellore", "key-goat-to-nellore")); err != nil {
		t.Fatalf("correct goat to Nellore: %v", err)
	}
	var linkedSpecies string
	if err := pool.QueryRow(ctx, `SELECT b.species FROM goats g JOIN breeds b ON b.breed_id = g.breed_id WHERE g.goat_id = $1::uuid`, goat).Scan(&linkedSpecies); err != nil {
		t.Fatalf("read linked breed: %v", err)
	}
	if linkedSpecies != "goat" {
		t.Fatalf("the goat was linked to the %s Nellore row, want the goat row", linkedSpecies)
	}

	// A row holding a goat and a sheep may only take a breed BOTH species carry.
	sheep := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, sheep, "Nellore", "female")
	exec(`UPDATE goats SET species = 'sheep' WHERE goat_id = $1::uuid`, sheep)
	err = correctCensus(t, ctx, repo,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Nellore", "female", "breed", "Sirohi", "key-mixed-to-goat-breed"))
	if !errors.Is(err, ports.ErrCensusCorrectionValue) {
		t.Fatalf("mixed-species row corrected to a goat-only breed: err = %v, want ErrCensusCorrectionValue", err)
	}
	for _, id := range []string{goat, sheep} {
		if breed, _ := goatBreedSex(t, ctx, pool, id); breed != "Nellore" {
			t.Fatalf("a refused correction still moved %s to %q", id, breed)
		}
	}
}
