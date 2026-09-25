package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCorrectCensusSliceAcceptsAConfiguredThirdGender pins OPEN UP TO NEW SPECIES (maintainer
// decision 2026-09-25) on the census correction: a gender added on Configuration > Items &
// settings is a valid correction target, and an animal already carrying it names its census row,
// while a gender the farm never configured -- or archived -- is refused before any animal moves.
func TestCorrectCensusSliceAcceptsAConfiguredThirdGender(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	repo := NewRepository(pool, 5*time.Second)
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedCorrectionBreeds(t, ctx, pool)
	if _, err := pool.Exec(ctx, `
INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order) VALUES
($1::uuid, 'castrated', 'Castrated male', 30), ($1::uuid, 'retired_code', 'Retired', 40)
ON CONFLICT DO NOTHING`, ssTenant); err != nil {
		t.Fatalf("seed genders: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE sex_lookup SET status = 'archived' WHERE tenant_id = $1::uuid AND sex_code = 'retired_code'`, ssTenant); err != nil {
		t.Fatalf("archive gender: %v", err)
	}

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, goat, "Beetal", "male")

	for _, bad := range []string{"hermaphrodite", "retired_code"} {
		_, err := repo.CorrectCensusSlice(ctx,
			censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "male", "sex", bad, "key-bad-"+bad))
		if !errors.Is(err, ports.ErrCensusCorrectionValue) {
			t.Fatalf("correct to %q: err = %v, want ErrCensusCorrectionValue", bad, err)
		}
	}
	if _, sex := goatBreedSex(t, ctx, pool, goat); sex != "male" {
		t.Fatalf("a refused correction still wrote sex=%q", sex)
	}

	if _, err := repo.CorrectCensusSlice(ctx,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "male", "sex", "castrated", "key-castrated")); err != nil {
		t.Fatalf("correct to a configured third gender: %v", err)
	}
	if _, sex := goatBreedSex(t, ctx, pool, goat); sex != "castrated" {
		t.Fatalf("sex = %q, want castrated", sex)
	}
	// The row now carries the third gender and names itself by it.
	if _, err := repo.CorrectCensusSlice(ctx,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "castrated", "breed", "Sirohi", "key-breed-on-third")); err != nil {
		t.Fatalf("correct breed on a third-gender row: %v", err)
	}
}
