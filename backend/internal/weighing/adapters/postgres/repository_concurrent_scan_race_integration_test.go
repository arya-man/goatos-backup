package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestConcurrentRecordAnimalObservationSameTagDifferentIdempotencyKeysLeavesOneOpenRow
// reproduces the check-then-act race in recordUnknownAnimalObservationTx: two
// concurrent captures of the SAME scanned tag in the SAME individual-animal
// bucket, with DIFFERENT idempotency keys (a genuine double-scan, not a
// client replay -- a replay of the SAME key short-circuits earlier via
// observationByIdemTx and never reaches this race at all).
//
// The race has TWO possible shapes, and this repository has closed both:
//
//  1. Insert-vs-insert. Both transactions' "updated" CTE sees no existing
//     open row, so both fall through to the "inserted" CTE's
//     WHERE NOT EXISTS (SELECT 1 FROM updated). The only unique index that
//     pre-dated this fix (weighing_observations_idempotency_uidx) only
//     guards a retry of the SAME key, so both inserts could commit --
//     two rows with submitted_at IS NULL for one animal, two videos
//     equally "current". Closed by migration 000073's partial unique index
//     weighing_observations_one_open_tag_uidx (tenant_id, campaign_shed_id,
//     lower(btrim(scanned_identifier))) WHERE submitted_at IS NULL: the
//     loser's INSERT trips 23505.
//
//  2. Update-vs-update. The bucket-row lock the "assigned_shed" CTE takes
//     (FOR NO KEY UPDATE, the F8 close-race fix) serialises two concurrent
//     captures of the SAME bucket. Serialising is not the same as
//     disambiguating: under plain READ COMMITTED, the loser -- once
//     unblocked -- sees the winner's now-committed row and silently takes
//     the "updated" CTE's UPDATE-in-place branch, overwriting the winner's
//     weight/proof with its own. Both callers get a 200; the winner's
//     response is now a lie about what is actually stored, and the winner is
//     never told a second capture clobbered it. No INSERT happens here, so
//     the unique index above never fires. Closed by running
//     RecordAnimalObservation at SERIALIZABLE: PostgreSQL's SSI machinery
//     sees the loser's implicit read of weighing_observations was
//     invalidated by the winner's commit and aborts it with
//     serialization_failure (40001) instead of letting it complete against
//     refreshed state. A legitimate SEQUENTIAL rescan (same tag, no time
//     overlap -- see TestRescanOfUnsubmittedTagUpdatesSameRowNoDuplicateRow)
//     never conflicts under SERIALIZABLE and keeps updating in place exactly
//     as before.
//
// 23505 (on the new index) maps to ports.ErrDuplicateScan -- a genuine
// duplicate, never retried. 40001 (serialization failure) maps to the
// DIFFERENT ports.ErrWriteConflict and is retried internally by
// RecordAnimalObservation (bounded, see recordAnimalObservationMaxSerializationRetries):
// it says nothing about duplication, only that this transaction lost a race,
// so collapsing it into ErrDuplicateScan would falsely tell an operator they
// double-scanned an animal and discard a real capture -- see
// TestConcurrentRecordAnimalObservationDifferentTagsBothSucceed below, which
// proves that specific failure mode does NOT happen.
func TestConcurrentRecordAnimalObservationSameTagDifferentIdempotencyKeysLeavesOneOpenRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	// Migration 000065 binds a bucket's operator to the bucket's park through an
	// ACTIVE user_scope_grants row, so the shared fixture's operator needs one
	// before any weighing_campaign_sheds write -- otherwise this test would fall
	// into the unrelated pre-existing 23514 failure bucket instead of exercising
	// the race under test.
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const raceTag = "concurrent-race-rfid"
	const attempts = 2
	weights := [attempts]float64{20.0, 21.0}
	keys := [attempts]string{"animal:race-a", "animal:race-b"}

	// Two independent goroutines, same tag, same bucket, DIFFERENT idempotency
	// keys -- exactly the shape a duplicate offline-retry-with-a-fresh-key or
	// two operators scanning the same animal at once produces. `start` fires
	// both goroutines from the same instant so their transactions genuinely
	// overlap, rather than one finishing before the other begins.
	var wg sync.WaitGroup
	results := make([]error, attempts)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
				TenantID:          repoTenant,
				CampaignID:        repoCampaign,
				CampaignShedID:    repoAnimalScope,
				ScannedIdentifier: raceTag,
				WeightKg:          weights[i],
				ProofArtifactID:   repoExpectedShedProof,
				ActualLocationID:  repoActualShed,
				IdempotencyKey:    keys[i],
				RecordedBy:        repoOperator,
			})
			results[i] = err
		}(i)
	}
	start.Done()
	wg.Wait()

	successes := 0
	var winner int
	var conflicts int
	for i, err := range results {
		switch {
		case err == nil:
			successes++
			winner = i
		case errors.Is(err, ports.ErrDuplicateScan):
			conflicts++
		default:
			t.Fatalf("unexpected error from concurrent capture %d: %v", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent captures = %d, want exactly 1 (the loser must be rejected as a domain conflict, not silently accepted)", successes)
	}
	if conflicts != attempts-1 {
		t.Fatalf("rejected-as-conflict concurrent captures = %d, want %d", conflicts, attempts-1)
	}

	// The data-integrity invariant under test: at most one OPEN (submitted_at
	// IS NULL) row for this tag in this bucket, AND its content must be
	// exactly the WINNING caller's own write -- never a value that came from
	// the rejected caller (that would mean the loser's data silently won even
	// though the loser was told it lost, the same corruption in a different
	// disguise).
	var openRows int
	var storedWeight float64
	var storedKey string
	if err := pool.QueryRow(ctx, `
SELECT count(*) OVER (), weight_kg::float8, idempotency_key
FROM weighing_observations
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND lower(btrim(scanned_identifier))=lower(btrim($3))
  AND submitted_at IS NULL`, repoTenant, repoAnimalScope, raceTag).Scan(&openRows, &storedWeight, &storedKey); err != nil {
		t.Fatalf("read open row for race tag: %v", err)
	}
	if openRows != 1 {
		t.Fatalf("open rows for race tag=%d, want exactly 1 (data corruption: two videos both current for one animal)", openRows)
	}
	if storedWeight != weights[winner] {
		t.Fatalf("stored weight=%v, want the winning caller's own write %v (a mismatch means the winner was told success while the loser's data actually persisted)", storedWeight, weights[winner])
	}
	if storedKey != keys[winner] {
		t.Fatalf("stored idempotency_key=%q, want the winning caller's own key %q", storedKey, keys[winner])
	}
}

// TestConcurrentRecordAnimalObservationDifferentTagsBothSucceed is the negative
// half of the race test above, and it guards the FIELD case rather than the
// pathological one: a shed is worked fast and two DIFFERENT animals are captured
// at overlapping instants in the SAME bucket.
//
// Both captures must SUCCEED. Nothing about two different animals is a duplicate.
//
// This matters because the fix runs RecordAnimalObservation at SERIALIZABLE and
// both transactions take FOR NO KEY UPDATE on the SAME bucket row, which makes
// them candidates for an SSI conflict on a shared row. If the loser aborts with
// 40001 and that is translated to ports.ErrDuplicateScan, the operator is told
// they double-scanned an animal they scanned once, and a real weighing with a
// real video is DISCARDED instead of retried -- worse than the two-open-rows bug
// the fix exists to close. 40001 means "retry", 23505 means "duplicate"; they
// must not collapse into the same caller-visible outcome.
//
// Repeated, because SSI conflicts are timing-dependent and a single green pass
// proves very little.
func TestConcurrentRecordAnimalObservationDifferentTagsBothSucceed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const rounds = 10
	for round := 0; round < rounds; round++ {
		tagA := fmt.Sprintf("field-tag-a-%d", round)
		tagB := fmt.Sprintf("field-tag-b-%d", round)
		tags := [2]string{tagA, tagB}
		keys := [2]string{
			fmt.Sprintf("animal:field-a-%d", round),
			fmt.Sprintf("animal:field-b-%d", round),
		}

		var wg sync.WaitGroup
		var start sync.WaitGroup
		start.Add(1)
		results := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				start.Wait()
				_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
					TenantID:          repoTenant,
					CampaignID:        repoCampaign,
					CampaignShedID:    repoAnimalScope,
					ScannedIdentifier: tags[i],
					WeightKg:          30.0 + float64(i),
					ProofArtifactID:   repoExpectedShedProof,
					ActualLocationID:  repoActualShed,
					IdempotencyKey:    keys[i],
					RecordedBy:        repoOperator,
				})
				results[i] = err
			}(i)
		}
		start.Done()
		wg.Wait()

		for i, err := range results {
			if err != nil {
				t.Fatalf("round %d: capture of DISTINCT animal %q failed: %v -- two different animals in one bucket are not a duplicate, and this capture would be lost in the field", round, tags[i], err)
			}
		}

		var openRows int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM weighing_observations
WHERE tenant_id = $1::uuid
  AND campaign_shed_id = $2::uuid
  AND submitted_at IS NULL
  AND lower(btrim(scanned_identifier)) IN (lower(btrim($3)), lower(btrim($4)))`,
			repoTenant, repoAnimalScope, tagA, tagB).Scan(&openRows); err != nil {
			t.Fatalf("round %d: count open rows: %v", round, err)
		}
		if openRows != 2 {
			t.Fatalf("round %d: open rows for two distinct animals = %d, want 2 (both captures must persist)", round, openRows)
		}
	}
}
