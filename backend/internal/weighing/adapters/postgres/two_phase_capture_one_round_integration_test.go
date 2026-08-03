package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestTwoPhaseCaptureIsOneEvidenceRoundNotAnEdit reproduces the P1 found in the
// first real weighing device run.
//
// The Android client captures a weight in TWO phases: it posts the weight, and
// then -- once the proof video finishes uploading and the server proof id is
// known -- posts the SAME weight again carrying the proof id, under a
// `<base>:proof:<server_proof_id>` idempotency key. On a first-time capture
// those two posts are the SAME evidence round: same tag, same weight, same
// proof. Nothing was edited and nothing was reworked.
//
// Because the two posts carry DIFFERENT idempotency keys, the second one falls
// into recordUnknownAnimalObservationTx's `updated` CTE branch and is reported
// back as Superseded=true -- an EDIT. The service layer's reviseVerificationRound
// then withdraws round 1's verification item and enqueues round 2, and the write
// emits a second weighing.observation_accepted outbox event. The live run
// produced 20 verification_items (10 pending + 10 withdrawn) and 20 accepted
// events for 10 real captures.
//
// A content-identical re-post is NOT a new evidence round. It must leave
// accepted_at alone, report Superseded=false, and emit exactly one accepted
// event. A GENUINE edit (a different weight) must still advance the round.
func TestTwoPhaseCaptureIsOneEvidenceRoundNotAnEdit(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)
	const scannedTag = "901007000504392"

	base := domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          18.5,
		ProofArtifactID:   repoExpectedShedProof,
		RecordedBy:        repoOperator,
	}

	// Phase 1: weight posted under the base key.
	phaseOneCmd := base
	phaseOneCmd.IdempotencyKey = "weighing:individual:c:s:s:" + scannedTag + ":d7bc19f0"
	phaseOne, err := repo.RecordAnimalObservation(ctx, phaseOneCmd)
	if err != nil {
		t.Fatalf("phase one capture: %v", err)
	}
	if phaseOne.Superseded {
		t.Fatalf("phase one Superseded=true, want false on a brand-new capture")
	}

	// Phase 2: the SAME weight and the SAME proof, re-posted ~60ms later once
	// the proof id is known, under the `:proof:` key.
	phaseTwoCmd := base
	phaseTwoCmd.IdempotencyKey = phaseOneCmd.IdempotencyKey + ":proof:018073aa-8084-4000-8000-000000000001"
	phaseTwo, err := repo.RecordAnimalObservation(ctx, phaseTwoCmd)
	if err != nil {
		t.Fatalf("phase two capture: %v", err)
	}
	if phaseTwo.ObservationID != phaseOne.ObservationID {
		t.Fatalf("phase two observation id=%s, want the same row %s", phaseTwo.ObservationID, phaseOne.ObservationID)
	}
	// THE BUG: a content-identical re-post is reported as an edit, which makes
	// the service withdraw the pending verification item and raise a new round.
	if phaseTwo.Superseded {
		t.Fatalf("phase two Superseded=true, want false: same tag, same weight, same proof is ONE evidence round, not an edit")
	}
	// The verification item is versioned on AcceptedAt (see the service's
	// enqueueVerification). If AcceptedAt advances on a no-op re-post, the new
	// idempotency key cannot collide with the first item, so a SECOND pending
	// item is raised for the same evidence.
	if !phaseTwo.AcceptedAt.Equal(phaseOne.AcceptedAt) {
		t.Fatalf("phase two AcceptedAt=%s advanced past phase one %s; a no-op re-post must not open a new evidence round",
			phaseTwo.AcceptedAt, phaseOne.AcceptedAt)
	}
	if got := countWeighingObservationsForTag(t, ctx, pool, scannedTag); got != 1 {
		t.Fatalf("weighing_observations rows for tag=%d, want 1", got)
	}
	if got := countAcceptedOutboxEvents(t, ctx, pool, phaseOne.ObservationID); got != 1 {
		t.Fatalf("weighing.observation_accepted outbox events=%d, want exactly 1 for a two-phase capture of one animal", got)
	}

	// A GENUINE edit must still advance the round: new weight, so new evidence.
	editCmd := base
	editCmd.WeightKg = 19.4
	editCmd.IdempotencyKey = phaseOneCmd.IdempotencyKey + ":edit-1"
	edit, err := repo.RecordAnimalObservation(ctx, editCmd)
	if err != nil {
		t.Fatalf("genuine edit: %v", err)
	}
	if !edit.Superseded {
		t.Fatalf("genuine edit Superseded=false, want true: a changed weight IS a new evidence round")
	}
	if !edit.AcceptedAt.After(phaseOne.AcceptedAt) {
		t.Fatalf("genuine edit AcceptedAt=%s did not advance past %s", edit.AcceptedAt, phaseOne.AcceptedAt)
	}
	if got := countAcceptedOutboxEvents(t, ctx, pool, phaseOne.ObservationID); got != 2 {
		t.Fatalf("weighing.observation_accepted outbox events after a genuine edit=%d, want 2", got)
	}
}

func countWeighingObservationsForTag(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tag string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM weighing_observations
WHERE tenant_id=$1::uuid AND lower(btrim(scanned_identifier))=lower(btrim($2))`, repoTenant, tag).Scan(&count); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	return count
}

func countAcceptedOutboxEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool, observationID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.observation_accepted' AND aggregate_id=$2::uuid`,
		repoTenant, observationID).Scan(&count); err != nil {
		t.Fatalf("count accepted outbox events: %v", err)
	}
	return count
}
