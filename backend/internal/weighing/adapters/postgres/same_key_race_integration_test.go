package postgres

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// SAME IDEMPOTENCY KEY, IN FLIGHT TWICE (Phase A E2E, 2026-09-17).
//
// A phone's outbox retries a write whose first attempt timed out on the client while it was
// still running on the server, so the SAME key arrives twice at once. Live on the throwaway
// stack every weighing write answered the racing copy with a DOMAIN refusal instead of the
// original result: lump-sum submit 409 invalid_state, removal submit 409
// fasting_already_submitted, create 409 weighing_shed_already_scheduled, publish 409
// invalid_state. Android treats a 409 as terminal, so a pen the server accepted showed as a
// failed upload. Both copies must get the one original outcome.

func raceSameKey(t *testing.T, n int, fn func() error) []error {
	t.Helper()
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = fn()
		}(i)
	}
	close(start)
	wg.Wait()
	return errs
}

func TestSameKeyLumpSumSubmitRacingReturnsTheOriginalToEveryCopy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())`,
		repoTenant, repoOperator, repoPark)
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)

	var mu sync.Mutex
	ids := map[string]int{}
	errs := raceSameKey(t, 6, func() error {
		obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
			TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
			WeightKg: 410, AnimalCount: 10, ProofArtifactID: repoShedProof,
			IdempotencyKey: "shed:same-key-race", RecordedBy: repoOperator,
		})
		if err == nil {
			mu.Lock()
			ids[obs.ObservationID]++
			mu.Unlock()
		}
		return err
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("racing copy %d: err=%v, want the original observation", i, err)
		}
	}
	if len(ids) != 1 {
		t.Fatalf("racing copies returned %d distinct observations %v, want exactly one", len(ids), ids)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed_observation_accepted"); got != 1 {
		t.Fatalf("accepted events=%d, want exactly one", got)
	}
}

func TestSameKeyScopeSubmitRacingReturnsTheOriginalToEveryCopy(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	const identifier = "same-key-race-rfid"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: identifier, WeightKg: 11.5,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:same-key-race", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}
	errs := raceSameKey(t, 6, func() error {
		return repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:same-key-race", []string{identifier})
	})
	for i, err := range errs {
		if err != nil {
			t.Errorf("racing copy %d: err=%v, want the original outcome", i, err)
		}
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed_submission.completed"); got != 1 {
		t.Fatalf("completion events=%d, want 1", got)
	}
}
