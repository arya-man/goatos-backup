package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// Proofs for the census-slice correction, run against the REAL schema.
//
// The whole risk in this write is its PREDICATE: it must match one census row and nothing else in
// the pen. A fake repository cannot see SQL, so a missing `sex` or `breed` clause would pass every
// fake-backed test while silently correcting animals the operator never saw.

func censusCorrectionCmd(shedID, partition, stage, breed, sex, field, value, key string) ports.CorrectCensusSliceCommand {
	cmd := ports.CorrectCensusSliceCommand{
		TenantID:             ssTenant,
		ActorID:              ssActor,
		ClientIdempotencyKey: key,
		StoredIdempotencyKey: key,
		IdempotencyScope:     "correctCensusSlice",
		RequestHash:          "hash:" + key,
		TraceID:              "trace-" + key,
		ShedID:               shedID,
		ManagementStage:      stage,
		Breed:                breed,
		Sex:                  sex,
		Field:                field,
		Value:                value,
		Reason:               "intake recorded the wrong value",
		OccurredAt:           time.Date(2026, time.August, 14, 9, 0, 0, 0, time.UTC),
	}
	if partition != "" {
		label := partition
		cmd.PartitionLabel = &label
	}
	return cmd
}

// setGoatBreedSex puts a seeded animal into a known census slice. The stage fixture's helper does
// not set these, and the whole point of this file is that they participate in the predicate.
func setGoatBreedSex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, breed, sex string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
UPDATE goats SET breed = $3, sex = $4 WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		ssTenant, goatID, breed, sex); err != nil {
		t.Fatalf("set goat breed/sex: %v", err)
	}
}

// seedCorrectionBreeds gives the tenant a real breed catalog: the correction validates against it,
// so a test running without one would only ever prove the fail-closed path.
func seedCorrectionBreeds(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO breeds (species, canonical_name, status)
VALUES ('goat', 'Beetal', 'active'), ('goat', 'Sirohi', 'active')
ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("seed breeds: %v", err)
	}
}

func goatBreedSex(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) (string, string) {
	t.Helper()
	var breed, sex string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(breed, ''), COALESCE(sex, '') FROM goats WHERE tenant_id=$1::uuid AND goat_id=$2::uuid`,
		ssTenant, goatID).Scan(&breed, &sex); err != nil {
		t.Fatalf("read goat: %v", err)
	}
	return breed, sex
}

// TestCorrectCensusSliceTouchesOnlyThatRow is the predicate proof, and it is the reason this test
// exists at all: one pen holds several breeds and both sexes, and correcting one census row must
// leave every other row in that pen exactly as it was.
//
// The sibling that matters most is the SAME BREED, OTHER SEX animal. A correction written as
// "this breed in this pen" -- the obvious shape, and the wrong one -- would take it too.
func TestCorrectCensusSliceTouchesOnlyThatRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedCorrectionBreeds(t, ctx, pool)

	target := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	otherSex := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	otherStage := seedStageGoat(t, ctx, pool, f.castroShed, "1", "Mother", "adult")
	otherPen := seedStageGoat(t, ctx, pool, f.castroShed, "Part 2", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, target, "Beetal", "female")
	setGoatBreedSex(t, ctx, pool, otherSex, "Beetal", "male")
	setGoatBreedSex(t, ctx, pool, otherStage, "Beetal", "female")
	setGoatBreedSex(t, ctx, pool, otherPen, "Beetal", "female")

	result, err := repo.CorrectCensusSlice(ctx,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "female", "breed", "Sirohi", "key-breed"))
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	if result.Corrected != 1 || result.TotalLive != 1 {
		t.Fatalf("corrected=%d total=%d, want 1/1 -- the write reached beyond the row",
			result.Corrected, result.TotalLive)
	}

	if breed, _ := goatBreedSex(t, ctx, pool, target); breed != "Sirohi" {
		t.Fatalf("target breed = %q, want Sirohi", breed)
	}
	for name, id := range map[string]string{
		"same breed, other SEX":   otherSex,
		"same breed, other STAGE": otherStage,
		"same breed, other PEN":   otherPen,
	} {
		if breed, _ := goatBreedSex(t, ctx, pool, id); breed != "Beetal" {
			t.Fatalf("%s was corrected too (breed=%q) -- that clause is missing from the predicate", name, breed)
		}
	}
}

// TestCorrectCensusSliceRejectsAnUnknownBreed pins the fail-closed path. Without the catalog check
// the UPDATE would accept the value and set breed_id to NULL through its own subquery, leaving the
// text and the id disagreeing for every reader that joins through breed_id.
func TestCorrectCensusSliceRejectsAnUnknownBreed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedCorrectionBreeds(t, ctx, pool)

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, goat, "Beetal", "female")

	_, err := repo.CorrectCensusSlice(ctx,
		censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "female", "breed", "Not A Breed", "key-unknown"))
	if !errors.Is(err, ports.ErrCensusCorrectionValue) {
		t.Fatalf("err = %v, want ErrCensusCorrectionValue", err)
	}
	if breed, _ := goatBreedSex(t, ctx, pool, goat); breed != "Beetal" {
		t.Fatalf("a REFUSED correction still wrote breed=%q", breed)
	}
}

// TestCorrectCensusSliceCorrectsSexAndReplaysOnce covers the other field and the idempotency
// contract in one pass: an exact replay must report the first result without correcting again.
func TestCorrectCensusSliceCorrectsSexAndReplaysOnce(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool, repo := startCorrectionWriteDB(t, ctx)
	defer pool.Close()
	f := seedShedStageFixture(t, ctx, pool)
	seedStageVocabulary(t, ctx, pool)
	seedCorrectionBreeds(t, ctx, pool)

	goat := seedStageGoat(t, ctx, pool, f.castroShed, "1", "K2", "kid")
	setGoatBreedSex(t, ctx, pool, goat, "Beetal", "female")

	cmd := censusCorrectionCmd(f.castroShed, "1", "K2", "Beetal", "female", "sex", "male", "key-sex")
	first, err := repo.CorrectCensusSlice(ctx, cmd)
	if err != nil {
		t.Fatalf("correct sex: %v", err)
	}
	if first.Corrected != 1 {
		t.Fatalf("corrected=%d, want 1", first.Corrected)
	}
	if _, sex := goatBreedSex(t, ctx, pool, goat); sex != "male" {
		t.Fatalf("sex = %q, want male", sex)
	}

	// The animal no longer matches the slice, so a NON-idempotent re-run would fail with an empty
	// scope. The replay must return the original result instead.
	replay, err := repo.CorrectCensusSlice(ctx, cmd)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.Corrected != first.Corrected || replay.Field != "sex" {
		t.Fatalf("replay = %+v, want the original result", replay)
	}
}
