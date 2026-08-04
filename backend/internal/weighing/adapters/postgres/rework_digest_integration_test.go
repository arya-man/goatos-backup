package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// The rework push is BATCHED PER SHED (maintainer decision 2026-08-03).
//
// A verifier who bounces five of a shed's captures used to send the operator five pushes for
// one trip back to one shed. There is no shed-level "review finished" action anywhere in the
// product, so the grouping moment is invented as a debounce over derived state: a bounce that
// has not reached the operator is exactly `verification_status='rework' AND
// rework_notified_at IS NULL`, and this sweep flushes a bucket's set as ONE digest event.
//
// These tests assert EVENT COUNTS and PAYLOAD CONTENT, not absence of error.

// recordAndRework captures a weight and then bounces it, returning the observation id.
func recordAndRework(t *testing.T, ctx context.Context, repo *Repository, tag string, weightKg float64, seq int) string {
	t.Helper()
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: tag,
		WeightKg:          weightKg, ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: fmt.Sprintf("animal:digest-%d", seq), RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation %s: %v", tag, err)
	}
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:      repoTenant,
		ObservationID: obs.ObservationID,
		RefType:       domain.VerificationRefTypeAnimal,
		Status:        domain.VerificationStatusRework,
		VerifiedBy:    repoVerifier,
		Reason:        "video too dark",
		EventID:       fmt.Sprintf("11111111-1111-4111-8111-1111111%05d", seq),
	}); err != nil {
		t.Fatalf("apply rework verdict %s: %v", tag, err)
	}
	return obs.ObservationID
}

func digestPayloads(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []domain.ReworkDigestPayload {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT payload->'payload'
FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND event_type = 'weighing.observation.rework_digest'
ORDER BY created_at`, repoTenant)
	if err != nil {
		t.Fatalf("read digest outbox: %v", err)
	}
	defer rows.Close()
	digests := []domain.ReworkDigestPayload{}
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan digest payload: %v", err)
		}
		var payload domain.ReworkDigestPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode digest payload: %v", err)
		}
		digests = append(digests, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("digest rows: %v", err)
	}
	return digests
}

// FIVE rejections in one shed produce ONE digest naming five animals -- not five digests.
// The sixth, arriving after that digest has already fired, still reaches the operator as its
// own later digest. Nothing is dropped.
func TestFiveReworksInOneShedProduceOneDigestAndALateSixthStillGetsOne(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	tags := []string{"90100700050441", "90100700050442", "90100700050443", "90100700050444", "90100700050445"}
	for i, tag := range tags {
		recordAndRework(t, ctx, repo, tag, 12.0+float64(i), i+1)
	}

	// A tick taken WHILE the verifier is still working must not fire: the quiet window has
	// not elapsed, which is what stops the storm in the first place.
	early, err := repo.SweepReworkDigests(ctx, domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now(), QuietWindow: time.Hour, MaxAge: 24 * time.Hour, NamedLimit: 3,
	})
	if err != nil {
		t.Fatalf("early sweep: %v", err)
	}
	if early.DigestsEmitted != 0 {
		t.Fatalf("sweep inside the quiet window emitted %d digests, want 0", early.DigestsEmitted)
	}

	// Once the shed goes quiet, ONE digest for the whole shed.
	quiet := domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now().Add(time.Hour), QuietWindow: time.Minute, MaxAge: 2 * time.Hour, NamedLimit: 3,
	}
	result, err := repo.SweepReworkDigests(ctx, quiet)
	if err != nil {
		t.Fatalf("flush sweep: %v", err)
	}
	if result.DigestsEmitted != 1 {
		t.Fatalf("flush emitted %d digests for one shed, want exactly 1", result.DigestsEmitted)
	}
	if result.ObservationsNamed != 5 {
		t.Fatalf("flush covered %d observations, want all 5", result.ObservationsNamed)
	}
	digests := digestPayloads(t, ctx, pool)
	if len(digests) != 1 {
		t.Fatalf("outbox holds %d rework digests, want 1 (five bounces, one shed, one push)", len(digests))
	}
	if digests[0].TotalCount != 5 {
		t.Fatalf("digest total_count=%d, want 5", digests[0].TotalCount)
	}
	// BOUNDED: it names the first few, never all fifty a shed could hold.
	if len(digests[0].Items) != 3 {
		t.Fatalf("digest names %d animals, want the NamedLimit of 3", len(digests[0].Items))
	}
	if digests[0].CampaignShedID != repoAnimalScope || digests[0].OperatorID != repoOperator {
		t.Fatalf("digest routes to shed=%q operator=%q, want %q / %q",
			digests[0].CampaignShedID, digests[0].OperatorID, repoAnimalScope, repoOperator)
	}
	if digests[0].Items[0].ScannedIdentifier == "" || digests[0].Items[0].WeightKg == 0 {
		t.Fatalf("digest item %+v carries no tag/weight; the operator cannot tell which capture to redo", digests[0].Items[0])
	}

	// IDEMPOTENT: re-running the sweep must not re-notify anyone.
	again, err := repo.SweepReworkDigests(ctx, quiet)
	if err != nil {
		t.Fatalf("repeat sweep: %v", err)
	}
	if again.DigestsEmitted != 0 {
		t.Fatalf("repeat sweep emitted %d digests, want 0 (already delivered)", again.DigestsEmitted)
	}
	if got := len(digestPayloads(t, ctx, pool)); got != 1 {
		t.Fatalf("outbox holds %d digests after a repeat sweep, want 1", got)
	}

	// A LATE SIXTH rejection, after the batch already fired, must still reach the operator.
	recordAndRework(t, ctx, repo, "90100700050446", 17.5, 6)
	late, err := repo.SweepReworkDigests(ctx, domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now().Add(2 * time.Hour), QuietWindow: time.Minute, MaxAge: 3 * time.Hour, NamedLimit: 3,
	})
	if err != nil {
		t.Fatalf("late sweep: %v", err)
	}
	if late.DigestsEmitted != 1 || late.ObservationsNamed != 1 {
		t.Fatalf("late sweep emitted %d digests / %d observations, want 1 / 1 -- a rejection arriving after the batch must NOT be swallowed",
			late.DigestsEmitted, late.ObservationsNamed)
	}
	digests = digestPayloads(t, ctx, pool)
	if len(digests) != 2 {
		t.Fatalf("outbox holds %d digests, want 2 (the batch, then the late sixth)", len(digests))
	}
	if digests[1].TotalCount != 1 || digests[1].Items[0].ScannedIdentifier != "90100700050446" {
		t.Fatalf("late digest = %+v, want it to name only the sixth animal", digests[1])
	}
}

// A verifier who keeps rejecting steadily never lets the shed go quiet. The starvation cap
// must flush anyway, so the operator is not held indefinitely.
func TestSteadyRejectionsAreFlushedByTheStarvationCap(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	recordAndRework(t, ctx, repo, "90100700050451", 12.0, 11)

	// Quiet window still open (an hour), but the oldest bounce is past the max age.
	result, err := repo.SweepReworkDigests(ctx, domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now().Add(90 * time.Minute),
		QuietWindow: 4 * time.Hour, MaxAge: time.Hour, NamedLimit: 3,
	})
	if err != nil {
		t.Fatalf("starvation sweep: %v", err)
	}
	if result.DigestsEmitted != 1 {
		t.Fatalf("starvation cap emitted %d digests, want 1 -- a steadily-rejecting verifier must not hold the operator forever", result.DigestsEmitted)
	}
}

// A re-submission that is bounced AGAIN must re-enter the digest. The stamp is cleared inside
// the verdict UPDATE itself, so the second round needs no reconciliation anywhere.
func TestSecondBounceOfTheSameCaptureReEntersTheDigest(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	observationID := recordAndRework(t, ctx, repo, "90100700050461", 12.0, 21)
	flush := domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now().Add(time.Hour), QuietWindow: time.Minute, MaxAge: 2 * time.Hour, NamedLimit: 3,
	}
	if _, err := repo.SweepReworkDigests(ctx, flush); err != nil {
		t.Fatalf("first flush: %v", err)
	}
	if got := len(digestPayloads(t, ctx, pool)); got != 1 {
		t.Fatalf("digests after first flush=%d, want 1", got)
	}

	// The verifier bounces the SAME capture a second time (a distinct verification event).
	if _, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: observationID, RefType: domain.VerificationRefTypeAnimal,
		Status: domain.VerificationStatusRework, VerifiedBy: repoVerifier, Reason: "still dark",
		EventID: "11111111-1111-4111-8111-111111100022",
	}); err != nil {
		t.Fatalf("second rework verdict: %v", err)
	}
	second, err := repo.SweepReworkDigests(ctx, domain.ReworkDigestSweepParams{
		TenantID: repoTenant, AsOf: time.Now().Add(2 * time.Hour), QuietWindow: time.Minute, MaxAge: 3 * time.Hour, NamedLimit: 3,
	})
	if err != nil {
		t.Fatalf("second flush: %v", err)
	}
	if second.DigestsEmitted != 1 {
		t.Fatalf("second bounce emitted %d digests, want 1 -- a re-rejected capture must be told to the operator again", second.DigestsEmitted)
	}
}
