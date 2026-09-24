package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// P10 (docs/perf/2026-09-24-stg-latency): the leadership gallery is one page of buckets plus
// their evidence. It used to be three sequential statements (page, per-animal evidence, whole-pen
// evidence), each a network round trip; the two evidence reads only depend on the page, so they
// ride ONE pgx.Batch: two round trips per page however many buckets it holds. The whole-pen
// {ref: kind} map must also stay complete when it is a primary-key lookup (it seq-scanned
// proof_artifacts once per lump row: stg 600 ms p95).
func TestListLeadershipShedsIsTwoRoundTripsWithCompleteLumpKinds(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	seedLeadershipObservations(t, ctx, pool, time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC), 3)
	for _, id := range []string{slotPenVideoFour} {
		insertProof(t, ctx, pool, id, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	insertProof(t, ctx, pool, slotPenPhotoProof, "photo", "completed", "shed", repoPerShed, "shed", repoPerShed)
	insertProof(t, ctx, pool, slotPenPhotoProof2, "photo", "completed", "shed", repoPerShed, "shed", repoPerShed)
	writer := NewRepository(pool, 5*time.Second)
	cmd := domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 412, IdempotencyKey: "shed:rt", RecordedBy: repoOperator,
		Proofs:  domain.LumpSumProofRefs{"pen_video": {repoShedProof, repoShedProofTwo, repoShedProofThree, slotPenVideoFour}, "scale_photo": {slotPenPhotoProof, slotPenPhotoProof2}},
		Answers: domain.SOPAnswers{},
	}
	if _, err := writer.RecordShedObservation(ctx, judgedShed(t, twoSlotRules(), cmd)); err != nil {
		t.Fatalf("record whole-pen evidence: %v", err)
	}

	counted, trips := pgtest.CountingPool(t, ctx, pool)
	repo := NewRepository(counted, 5*time.Second)
	// Warm once so statement preparation is not what is being counted.
	if _, err := repo.ListLeadershipSheds(ctx, repoTenant, nil, "", 0, 0); err != nil {
		t.Fatalf("warm gallery page: %v", err)
	}
	trips.Reset()
	page, err := repo.ListLeadershipSheds(ctx, repoTenant, nil, "", 0, 0)
	if err != nil {
		t.Fatalf("gallery page: %v", err)
	}
	if got := trips.Trips(); got > 2 {
		t.Fatalf("gallery page took %d round trips, want at most 2 (page, then one batch):\n%s", got, strings.Join(trips.SQL(), "\n---\n"))
	}
	var lump, individual *domain.LeadershipShedVideos
	for i := range page.Items {
		switch page.Items[i].CampaignShedID {
		case repoShedScope:
			lump = &page.Items[i]
		case repoAnimalScope:
			individual = &page.Items[i]
		}
	}
	if individual == nil || len(individual.Individual) != 3 {
		t.Fatalf("individual bucket = %+v, want its 3 observations", individual)
	}
	if lump == nil || lump.LumpSum == nil {
		t.Fatalf("whole-pen bucket missing its evidence: %+v", lump)
	}
	for _, ref := range []string{repoShedProof, repoShedProofTwo, repoShedProofThree, slotPenVideoFour, slotPenPhotoProof, slotPenPhotoProof2} {
		if lump.LumpSum.ProofKinds[ref] == "" {
			t.Fatalf("lump kinds %v missing %s", lump.LumpSum.ProofKinds, ref)
		}
	}
	if lump.LumpSum.ProofKinds[slotPenPhotoProof2] != "photo" || lump.LumpSum.ProofKinds[slotPenVideoFour] != "video" {
		t.Fatalf("lump kinds = %v", lump.LumpSum.ProofKinds)
	}
}

// The whole-pen {ref: kind} subquery runs once per whole-pen row. `proof_id = X OR proof_id IN
// (SELECT ...)` cannot use the proof_artifacts key (OCI clone: 18.7 ms seq scan of 44k rows per
// row); ANY(array_append(ARRAY(...), primary)) is a key probe with the identical result.
func TestShedProofKindsSubqueryUsesPrimaryKeyLookup(t *testing.T) {
	if !strings.Contains(shedProofKindsSQL, "pa.proof_id = ANY(array_append(ARRAY(") {
		t.Fatalf("shedProofKindsSQL must be an ANY(uuid[]) PK lookup:\n%s", shedProofKindsSQL)
	}
	if strings.Contains(shedProofKindsSQL, "OR pa.proof_id IN") {
		t.Fatalf("shedProofKindsSQL still ORs an IN-subquery against the key:\n%s", shedProofKindsSQL)
	}
}
