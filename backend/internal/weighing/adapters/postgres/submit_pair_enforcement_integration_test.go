package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// The pair rule, enforced by the SERVER.
//
// One animal = one (weight, video) pair. Every such pair must exist before a
// bucket can be submitted. The app gates on this, but a client gate is not
// enforcement: a stale build, a replayed request, a modified client, or a
// dropped capture POST all reach the server with a list the operator believes
// is complete. What the operator gets back has to say which ANIMAL is short of
// which half -- the rejection used to fall through as ErrNotFound, i.e. "this
// shed does not exist", about a shed they are standing in.
//
// Nothing here consults a roster, an expected count, or the herd register.
// Weighing has no denominator; the set under test is exactly what was scanned.

func TestSubmitIndividualScopeRejectsAnimalWithWeightButNoVideo(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const complete = "pair-complete-rfid"
	const videoless = "pair-no-video-rfid"
	const videolessProof = "00000000-0000-4000-8000-0000000094f1"

	// One fully paired animal.
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: complete, WeightKg: 18.25,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:pair-complete", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record complete observation: %v", err)
	}

	// A second animal whose weight landed but whose video never finished
	// uploading. Captured against its own proof, which is then left in the state a
	// half-finished upload actually sits in.
	insertProof(t, ctx, pool, videolessProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: videoless, WeightKg: 21.75,
		ProofArtifactID: videolessProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:pair-no-video", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record videoless observation: %v", err)
	}
	insertProof(t, ctx, pool, videolessProof, "video", "pending", "shed", repoExpectedShed, "shed", repoExpectedShed)

	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:pair-no-video", []string{complete, videoless})
	if !errors.Is(err, ports.ErrCaptureIncomplete) {
		t.Fatalf("submit err=%v, want ErrCaptureIncomplete", err)
	}
	incomplete := &ports.CaptureIncomplete{}
	if !errors.As(err, &incomplete) {
		t.Fatalf("submit err=%v does not carry *ports.CaptureIncomplete", err)
	}
	if len(incomplete.MissingVideo) != 1 || incomplete.MissingVideo[0] != videoless {
		t.Fatalf("MissingVideo=%v, want exactly [%s] — the rejection must name the animal", incomplete.MissingVideo, videoless)
	}
	if len(incomplete.MissingWeight) != 0 {
		t.Fatalf("MissingWeight=%v, want empty — both animals were weighed", incomplete.MissingWeight)
	}
	// A rejected submit must leave the bucket OPEN so the operator can go back and
	// finish the missing half. Anything terminal here would mean a half-captured
	// shed was accepted as done.
	assertScopeNotCompleted(t, ctx, pool, repoAnimalScope)
}

func TestSubmitIndividualScopeRejectsAnimalWithNoWeight(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const weighed = "pair-weighed-rfid"
	// The exact shape of tonight's defect: the phone scanned this tag and believes
	// it captured it, but the capture write never reached the server -- the outbox
	// suppressed the re-enqueue. The submit list still carries the tag.
	const unweighed = "pair-never-arrived-rfid"

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: weighed, WeightKg: 17.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:pair-weighed", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:pair-no-weight", []string{weighed, unweighed})
	if !errors.Is(err, ports.ErrCaptureIncomplete) {
		t.Fatalf("submit err=%v, want ErrCaptureIncomplete", err)
	}
	incomplete := &ports.CaptureIncomplete{}
	if !errors.As(err, &incomplete) {
		t.Fatalf("submit err=%v does not carry *ports.CaptureIncomplete", err)
	}
	if len(incomplete.MissingWeight) != 1 || incomplete.MissingWeight[0] != unweighed {
		t.Fatalf("MissingWeight=%v, want exactly [%s]", incomplete.MissingWeight, unweighed)
	}
	assertScopeNotCompleted(t, ctx, pool, repoAnimalScope)
}

// assertScopeNotCompleted fails if the bucket reached a terminal state. The pair
// rule is only enforcement if a rejected submit leaves the work still doable.
func assertScopeNotCompleted(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM weighing_campaign_sheds WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, campaignShedID).Scan(&status); err != nil {
		t.Fatalf("read scope status: %v", err)
	}
	if status != "pending" && status != domain.StatusInProgress {
		t.Fatalf("scope status=%s after a REJECTED submit — a half-captured shed must never reach a terminal state", status)
	}
}

// TestSubmitIndividualScopeAcceptsFullyPairedBuckets is the happy-path guard.
// The first real device run submitted a 5-animal bucket and a 2-animal bucket;
// neither may regress, and every event they produce must be DELIVERABLE.
func TestSubmitIndividualScopeAcceptsFullyPairedBuckets(t *testing.T) {
	for _, size := range []int{5, 2} {
		t.Run(fmt.Sprintf("%d_animals", size), func(t *testing.T) {
			pgtest.SkipIfNoDocker(t)
			ctx := context.Background()
			pool := pgtest.StartPostgres(t, ctx)
			defer pool.Close()
			seedWeighingObservationFixture(t, ctx, pool)
			repo := NewRepository(pool, 5*time.Second)

			identifiers := make([]string, 0, size)
			for i := 0; i < size; i++ {
				identifier := fmt.Sprintf("pair-happy-%d-rfid-%d", size, i)
				identifiers = append(identifiers, identifier)
				if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
					TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
					ScannedIdentifier: identifier, WeightKg: 15 + float64(i),
					ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
					IdempotencyKey: fmt.Sprintf("animal:pair-happy-%d-%d", size, i),
					RecordedBy:     repoOperator,
				}); err != nil {
					t.Fatalf("record observation %d: %v", i, err)
				}
			}

			if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
				fmt.Sprintf("submit:pair-happy-%d", size), identifiers); err != nil {
				t.Fatalf("submit fully paired bucket of %d: %v — the pair gate must not block complete work", size, err)
			}
			assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
			if got := countOutbox(t, ctx, pool, "weighing.shed_submission.completed"); got != 1 {
				t.Fatalf("completion events=%d, want 1", got)
			}
			// A row that exists but cannot validate is a silent, permanent drop.
			assertEveryOutboxEnvelopeValidates(t, ctx, pool, repoTenant)
		})
	}
}

// The lump-sum pair is (total weight + animal count) + at least one finished
// video for the shed. Weight and count are rejected upstream; an unusable video
// used to render as "request is invalid".
func TestRecordShedObservationRejectsVideoThatIsNotReady(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const pendingShedProof = "00000000-0000-4000-8000-0000000094f2"
	insertProof(t, ctx, pool, pendingShedProof, "video", "pending", "shed", repoPerShed, "shed", repoPerShed)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 240, AverageWeightKg: 24, AnimalCount: 10,
		ProofArtifactID:  pendingShedProof,
		ProofArtifactIDs: []string{pendingShedProof},
		IdempotencyKey:   "shed:pair-video-not-ready", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrProofNotReady) {
		t.Fatalf("lump-sum err=%v, want ErrProofNotReady — an operator whose video is still uploading must not be told 'request is invalid'", err)
	}
}
