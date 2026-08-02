package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestConcurrentRecordAnimalObservationSameTagDifferentIdempotencyKeysLeavesOneOpenRow
// reproduces the check-then-act race in recordUnknownAnimalObservationTx.
//
// Under READ COMMITTED, two concurrent captures of the SAME scanned tag in the
// SAME individual-animal bucket, with DIFFERENT idempotency keys, each run the
// "updated" CTE (which sees no existing open row for the tag) and then the
// "inserted" CTE (WHERE NOT EXISTS (SELECT 1 FROM updated)). Before the fix,
// the only unique index guarding the table is
// weighing_observations_idempotency_uidx (tenant_id, idempotency_key), which
// does not stop two DIFFERENT keys from both inserting. Both transactions can
// commit, leaving two rows with submitted_at IS NULL for the same tag/bucket --
// two videos are equally "current" proof for one animal.
//
// After the fix (partial unique index on
// (tenant_id, campaign_shed_id, lower(btrim(scanned_identifier))) WHERE
// submitted_at IS NULL), the loser's INSERT trips 23505, which the repository
// must translate into a domain conflict (ports.ErrDuplicateScan) rather than
// leaking a raw pgx/Postgres error. Exactly one row must be left open.
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

	// Two independent goroutines, same tag, same bucket, DIFFERENT idempotency
	// keys -- exactly the shape a duplicate offline-retry-with-a-fresh-key or
	// two operators scanning the same animal at once produces.
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
				WeightKg:          20.0 + float64(i),
				ProofArtifactID:   repoExpectedShedProof,
				ActualLocationID:  repoActualShed,
				IdempotencyKey:    "animal:race-" + [2]string{"a", "b"}[i],
				RecordedBy:        repoOperator,
			})
			results[i] = err
		}(i)
	}
	start.Done()
	wg.Wait()

	successes := 0
	var conflicts int
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ports.ErrDuplicateScan):
			conflicts++
		default:
			t.Fatalf("unexpected error from concurrent capture: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent captures = %d, want exactly 1 (the loser must be rejected as a domain conflict, not silently accepted)", successes)
	}
	if conflicts != attempts-1 {
		t.Fatalf("rejected-as-conflict concurrent captures = %d, want %d", conflicts, attempts-1)
	}

	// The data-integrity invariant under test: at most one OPEN (submitted_at IS
	// NULL) row for this tag in this bucket. Two open rows means two videos are
	// simultaneously "current" proof for the same animal -- the corruption this
	// fix exists to prevent.
	var openRows int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM weighing_observations
WHERE tenant_id=$1::uuid
  AND campaign_shed_id=$2::uuid
  AND lower(btrim(scanned_identifier))=lower(btrim($3))
  AND submitted_at IS NULL`, repoTenant, repoAnimalScope, raceTag).Scan(&openRows); err != nil {
		t.Fatalf("count open rows for race tag: %v", err)
	}
	if openRows != 1 {
		t.Fatalf("open rows for race tag=%d, want exactly 1 (data corruption: two videos both current for one animal)", openRows)
	}
}
