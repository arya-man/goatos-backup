package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// A shed somebody is actively working must not read the same as a shed nobody
// has touched.
//
// weighing_campaign_sheds.status admits 'in_progress' (migration 000058), but no
// CAPTURE path ever wrote it — only ReopenScope and the rework/reactivate verdict
// paths did. So a bucket with scans in it reported 'pending' right up until the
// operator pressed Submit, and operator_summaries' `count(*) FILTER (WHERE
// cs.status='in_progress')` column was structurally always 0: a rendered state
// that could not occur.
//
// Maintainer decision: mark it on FIRST capture, in the capture's own transaction.

func TestFirstIndividualCaptureMovesBucketPendingToInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	assertScopeStatus(t, ctx, pool, repoAnimalScope, "pending")

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "tag-inprogress-1", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:inprogress-1", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("first individual capture: %v", err)
	}
	// THE BUG: this used to still read 'pending'.
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)

	// The capture is not the submit: the bucket is being WORKED, not finished.
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"animal:inprogress-submit", []string{"tag-inprogress-1"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
}

// The transition is a one-way edge, not a per-capture rewrite: capture 2..N must
// leave the bucket's status row exactly as capture 1 left it (same updated_at)
// and must not put a status event on the outbox.
func TestSecondIndividualCaptureDoesNotRewriteBucketStatus(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "tag-idem-1", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:idem-1", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("first capture: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	firstTouch := scopeUpdatedAt(t, ctx, pool, repoAnimalScope)
	outboxAfterFirst := weighingOutboxCount(t, ctx, pool)

	// A different animal, and then an exact idempotent replay of the first key.
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "tag-idem-2", WeightKg: 13.1,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:idem-2", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "tag-idem-1", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:idem-1", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("redelivered capture: %v", err)
	}

	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)
	if got := scopeUpdatedAt(t, ctx, pool, repoAnimalScope); !got.Equal(firstTouch) {
		t.Fatalf("bucket updated_at moved on a later capture: %s -> %s; the pending->in_progress edge must fire once, not per scan", firstTouch, got)
	}
	// Exactly one new observation event (tag-idem-2); the replay of animal:idem-1
	// short-circuits on its idempotency record, and no capture emits a
	// bucket-status event at all.
	if got, want := weighingOutboxCount(t, ctx, pool), outboxAfterFirst+1; got != want {
		t.Fatalf("weighing outbox rows=%d, want %d (one observation_accepted for the new animal, and NO bucket-status event from any capture)", got, want)
	}
}

// A terminal bucket must never be resurrected by a late capture, and the status
// write must not become a laxer second door into weighing_campaign_sheds than the
// capture statement's own gate.
func TestCaptureCannotMoveTerminalBucketToInProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	for _, terminal := range []string{"completed", "closed", "canceled"} {
		t.Run(terminal, func(t *testing.T) {
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()
			seedWeighingObservationFixture(t, ctx, pool)
			repo := NewRepository(pool, 5*time.Second)

			execWeighingTestSQL(t, ctx, pool, `
UPDATE weighing_campaign_sheds SET status=$1, completed_at=now()
WHERE tenant_id=$2::uuid AND campaign_shed_id=$3::uuid`, terminal, repoTenant, repoAnimalScope)

			_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
				TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
				ScannedIdentifier: "tag-late-" + terminal, WeightKg: 12.4,
				ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
				IdempotencyKey: "animal:late-" + terminal, RecordedBy: repoOperator,
			})
			if err == nil {
				t.Fatalf("capture into a %s bucket succeeded; it must be refused", terminal)
			}
			if !errors.Is(err, ports.ErrImmutable) && !errors.Is(err, ports.ErrNotFound) {
				t.Fatalf("capture into a %s bucket err=%v, want a typed refusal", terminal, err)
			}
			assertScopeStatus(t, ctx, pool, repoAnimalScope, terminal)
		})
	}
}

// A lump-sum bucket has NO in-progress window and must not be given one: on that
// path the capture IS the submit. One RecordShedObservation carries the total
// weight, the count and the whole video bundle, and
// weighing_shed_observations_one_open_scope_uidx admits exactly one open row per
// bucket, so there is never a second lump-sum capture to be mid-shed between.
// This test pins that pending -> completed stays a single transition, so nobody
// later "fixes" it by writing an in_progress no reader can ever observe.
func TestFirstLumpSumCaptureCompletesBucketWithNoInProgressWindow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	assertScopeStatus(t, ctx, pool, repoShedScope, "pending")
	if _, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
		IdempotencyKey: "shed:inprogress", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("lump-sum capture: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	assertWorkItemState(t, ctx, pool, repoShedScope, domain.StatusCompleted)
}

// The read model the broken write left stranded. operator_summaries' in_progress
// FILTER counted a state nothing produced, so it was always 0. Fixing the write
// has to be enough to make it report — nothing on the read side filters
// 'in_progress' out.
func TestOperatorSummariesReportInProgressMidCapture(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	before := operatorSummaryFor(t, ctx, repo, repoOperator)
	if before.CapturingCount != 0 {
		t.Fatalf("in_progress before any capture=%d, want 0", before.CapturingCount)
	}

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "tag-rollup", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:rollup", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("capture: %v", err)
	}

	// THE STRANDED COLUMN: this used to stay 0 forever.
	mid := operatorSummaryFor(t, ctx, repo, repoOperator)
	if mid.CapturingCount != 1 {
		t.Fatalf("in_progress mid-capture=%d, want 1 (the operator is working this shed right now)", mid.CapturingCount)
	}
	if mid.NotStartedCount != before.NotStartedCount-1 {
		t.Fatalf("pending mid-capture=%d, want %d (the bucket LEFT pending, it was not double-counted)", mid.NotStartedCount, before.NotStartedCount-1)
	}
	if mid.ShedCount != before.ShedCount {
		t.Fatalf("shed count changed from %d to %d; the four state counts partition the same bucket set", before.ShedCount, mid.ShedCount)
	}

	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"animal:rollup-submit", []string{"tag-rollup"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	assertWorkItemState(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
	after := operatorSummaryFor(t, ctx, repo, repoOperator)
	if after.CapturingCount != 0 {
		t.Fatalf("in_progress after submit=%d, want 0", after.CapturingCount)
	}
	if after.SubmittedCount != mid.SubmittedCount+1 {
		t.Fatalf("completed after submit=%d, want %d", after.SubmittedCount, mid.SubmittedCount+1)
	}
}

func operatorSummaryFor(t *testing.T, ctx context.Context, repo *Repository, operatorUserID string) domain.OperatorSummary {
	t.Helper()
	rows, err := repo.operatorSummaries(ctx, repoTenant, operatorUserID, "")
	if err != nil {
		t.Fatalf("operator summaries: %v", err)
	}
	for _, row := range rows {
		if row.OperatorUserID == operatorUserID {
			return row
		}
	}
	t.Fatalf("no operator summary row for %s (rows=%d)", operatorUserID, len(rows))
	return domain.OperatorSummary{}
}

func scopeUpdatedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) time.Time {
	t.Helper()
	var got time.Time
	if err := pool.QueryRow(ctx, `SELECT updated_at FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).Scan(&got); err != nil {
		t.Fatalf("read scope updated_at: %v", err)
	}
	return got
}

func assertWorkItemState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `SELECT work_state FROM weighing_work_items WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).Scan(&got); err != nil {
		t.Fatalf("read work item state: %v", err)
	}
	if got != want {
		t.Fatalf("work item state for %s=%q, want %q", campaignShedID, got, want)
	}
}

// weighingOutboxCount is the whole-tenant outbox row count. The pending ->
// in_progress edge must add nothing to it: it is a status the read models pick up
// on their next read, and a producer with no consumer is a silent drop.
func weighingOutboxCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM outbox_messages WHERE tenant_id=$1::uuid`, repoTenant).Scan(&got); err != nil {
		t.Fatalf("count outbox messages: %v", err)
	}
	return got
}
