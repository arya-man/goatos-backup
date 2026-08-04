package postgres

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	verificationports "github.com/vgoats/goatos/backend/internal/verification/ports"
	"github.com/vgoats/goatos/backend/internal/weighing/adapters/verificationbridge"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// W-18: a verifier approves, the queue empties, and NOTHING happens.
//
// Verdicts are applied ASYNCHRONOUSLY and that is deliberate
// (backend/internal/bootstrap/api.go: the API's in-process bus does NOT receive
// verification.verdict.* -- the appliers run on the durable bus in
// cmd/outbox-relay / cmd/domain-event-consumer). The bug was never the async
// design; it was that the moment of DECIDING and the moment of APPLYING were
// indistinguishable from every surface. RecordVerdict drained the item out of the
// pending queue instantly while the observation stayed untouched, so a stopped
// relay, a lagging consumer, or a dead-lettered verdict looked exactly like
// finished work: an empty queue and no signal anywhere.
//
// These tests pin the two halves of the honest state, by VALUE:
//
//  1. verdict recorded, event NOT yet consumed -> the item is
//     domain.VerdictStateApplying and the observation is still 'pending'. There is
//     a real state a surface can render, not an absence.
//  2. the event IS consumed -> the observation becomes 'verified' AND the item
//     settles, because the applier acked.
const (
	ackVerificationTenant = repoTenant
	ackVerifier           = repoVerifier
	ackShedProof          = "00000000-0000-4000-8000-00000000940a"
)

func TestVerdictRecordedButNotConsumedLeavesAnHonestApplyingState(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, ackShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	weighingRepo := NewRepository(pool, 5*time.Second)
	verificationRepo := verificationpg.NewRepository(pool, 5*time.Second)

	obs := recordAndSubmitAckObservation(t, ctx, weighingRepo, "ack-pending-rfid", "ack:pending")
	item := enqueueWeighingVerificationItem(t, ctx, verificationRepo, obs.ObservationID, "ack:item:pending")

	// The producer declared it acks, so its items can enter the applying state at all.
	if !item.ApplierAckExpected {
		t.Fatal("weighing item was enqueued without applier_ack_expected; its decided items could never read as awaiting application")
	}
	if got := item.VerdictState(); got != verificationdomain.VerdictStateAwaitingReview {
		t.Fatalf("fresh item verdict_state=%q, want %q", got, verificationdomain.VerdictStateAwaitingReview)
	}

	// The verifier approves. NOTHING consumes the event -- exactly the relay-down case.
	decided, err := verificationRepo.RecordVerdict(ctx, verificationdomain.Verdict{
		TenantID:       ackVerificationTenant,
		ItemID:         item.ItemID,
		Decision:       verificationdomain.DecisionApproved,
		VerifierID:     ackVerifier,
		RowVersion:     item.RowVersion,
		IdempotencyKey: "verdict:ack:pending",
	})
	if err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	// The decision is real...
	if decided.Status != verificationdomain.StatusApproved {
		t.Fatalf("item status=%q, want %q", decided.Status, verificationdomain.StatusApproved)
	}
	// ...and the farm's record has NOT changed. Both of those are true at once, and
	// that is precisely the state the old contract could not express.
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusPending {
		t.Fatalf("observation verification_status=%q before the verdict is applied, want %q", got, domain.VerificationStatusPending)
	}
	if got := decided.VerdictState(); got != verificationdomain.VerdictStateApplying {
		t.Fatalf("verdict_state=%q after a verdict nothing consumed, want %q -- an unapplied verdict must be visible, not silent", got, verificationdomain.VerdictStateApplying)
	}
	if decided.AppliedAt != nil {
		t.Fatalf("applied_at=%v with no applier having run; an ack must only ever come from the applier", decided.AppliedAt)
	}

	// And the item is still THERE for the verifier to see: it left the pending
	// queue, but it did not leave the world. An empty queue must never be the only
	// feedback that a verdict was recorded.
	awaiting := listAwaitingApplication(t, ctx, verificationRepo)
	if len(awaiting) != 1 || awaiting[0].ItemID != item.ItemID {
		t.Fatalf("awaiting-application page=%d items %v, want exactly the decided item %s", len(awaiting), itemIDs(awaiting), item.ItemID)
	}
}

func TestConsumingTheVerdictAppliesItAndSettlesTheItem(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, ackShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	weighingRepo := NewRepository(pool, 5*time.Second)
	verificationRepo := verificationpg.NewRepository(pool, 5*time.Second)

	obs := recordAndSubmitAckObservation(t, ctx, weighingRepo, "ack-applied-rfid", "ack:applied")
	item := enqueueWeighingVerificationItem(t, ctx, verificationRepo, obs.ObservationID, "ack:item:applied")
	if _, err := verificationRepo.RecordVerdict(ctx, verificationdomain.Verdict{
		TenantID:       ackVerificationTenant,
		ItemID:         item.ItemID,
		Decision:       verificationdomain.DecisionApproved,
		VerifierID:     ackVerifier,
		RowVersion:     item.RowVersion,
		IdempotencyKey: "verdict:ack:applied",
	}); err != nil {
		t.Fatalf("record verdict: %v", err)
	}

	// The durable bus delivers. This is the SAME handler cmd/outbox-relay and
	// cmd/domain-event-consumer register, wired to the SAME ack bridge.
	handler := weighingapp.NewVerificationVerdictHandler(weighingRepo, nil).
		WithApplyAcker(verificationbridge.New(verificationRepo))
	if err := handler.HandleEvent(ctx, weighingVerdictEvent(t, obs.ObservationID, "22222222-2222-4222-8222-2222222222aa")); err != nil {
		t.Fatalf("handle verdict event: %v", err)
	}

	// The outcome landed on the observation -- the applier is still the ONLY writer of it.
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusVerified {
		t.Fatalf("observation verification_status=%q after the verdict was applied, want %q", got, domain.VerificationStatusVerified)
	}

	settled, err := verificationRepo.GetItem(ctx, ackVerificationTenant, item.ItemID)
	if err != nil {
		t.Fatalf("reload item: %v", err)
	}
	if got := settled.VerdictState(); got != verificationdomain.VerdictStateSettled {
		t.Fatalf("verdict_state=%q after the applier ran, want %q", got, verificationdomain.VerdictStateSettled)
	}
	if settled.AppliedAt == nil {
		t.Fatal("applied_at is nil after the applier acked")
	}
	if settled.AppliedByModule == nil || *settled.AppliedByModule != domain.VerificationModuleWeighing {
		t.Fatalf("applied_by_module=%v, want %q -- an ack must name the module that sent it", settled.AppliedByModule, domain.VerificationModuleWeighing)
	}
	// The status the verifier decided is untouched by the ack: the ack reports that
	// something happened, never what.
	if settled.Status != verificationdomain.StatusApproved {
		t.Fatalf("status=%q after ack, want %q; the ack must not rewrite the verdict", settled.Status, verificationdomain.StatusApproved)
	}
	if awaiting := listAwaitingApplication(t, ctx, verificationRepo); len(awaiting) != 0 {
		t.Fatalf("awaiting-application page still holds %v after the verdict was applied", itemIDs(awaiting))
	}

	// At-least-once redelivery must not advance applied_at to an instant at which
	// nothing was applied.
	firstAppliedAt := *settled.AppliedAt
	if err := handler.HandleEvent(ctx, weighingVerdictEvent(t, obs.ObservationID, "22222222-2222-4222-8222-2222222222aa")); err != nil {
		t.Fatalf("redeliver verdict event: %v", err)
	}
	replayed, err := verificationRepo.GetItem(ctx, ackVerificationTenant, item.ItemID)
	if err != nil {
		t.Fatalf("reload item after redelivery: %v", err)
	}
	if replayed.AppliedAt == nil || !replayed.AppliedAt.Equal(firstAppliedAt) {
		t.Fatalf("applied_at moved on redelivery: %v -> %v", firstAppliedAt, replayed.AppliedAt)
	}
}

// A verdict applied by an applier that has NO ack wired must still apply. The ack
// is a visibility signal; it is never allowed to become a correctness gate that
// can drop verdicts when it is missing.
func TestVerdictStillAppliesWhenNoAckerIsWired(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, ackShedProof, "video", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)

	weighingRepo := NewRepository(pool, 5*time.Second)
	obs := recordAndSubmitAckObservation(t, ctx, weighingRepo, "ack-none-rfid", "ack:none")

	handler := weighingapp.NewVerificationVerdictHandler(weighingRepo, nil)
	if err := handler.HandleEvent(ctx, weighingVerdictEvent(t, obs.ObservationID, "33333333-3333-4333-8333-3333333333aa")); err != nil {
		t.Fatalf("handle verdict event without acker: %v", err)
	}
	if got := readObservationVerificationStatus(t, ctx, pool, obs.ObservationID); got != domain.VerificationStatusVerified {
		t.Fatalf("observation verification_status=%q without an acker wired, want %q", got, domain.VerificationStatusVerified)
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func recordAndSubmitAckObservation(t *testing.T, ctx context.Context, repo *Repository, identifier, key string) domain.Observation {
	t.Helper()
	obs, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: identifier,
		WeightKg:          14.2, ProofArtifactID: ackShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:" + key, RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:"+key, []string{identifier}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	return obs
}

// enqueueWeighingVerificationItem raises the item through the SAME bridge the
// weighing service uses, so the applier_ack_expected declaration under test is the
// production one rather than a value the test made up.
func enqueueWeighingVerificationItem(t *testing.T, ctx context.Context, repo *verificationpg.Repository, observationID, idempotencyKey string) verificationdomain.Item {
	t.Helper()
	bridge := verificationbridge.New(repo)
	if err := bridge.EnqueueWeighingVerification(ctx, weighingapp.VerificationEnqueueRequest{
		TenantID:       repoTenant,
		ObservationID:  observationID,
		Category:       domain.VerificationRefTypeAnimal,
		MediaRefs:      []string{ackShedProof},
		OperatorID:     repoOperator,
		ShedID:         repoExpectedShed,
		CapturedAt:     time.Now().In(biztime.DefaultLocation()),
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		t.Fatalf("enqueue verification item: %v", err)
	}
	items, err := repo.ListQueue(ctx, verificationQueueParams(false))
	if err != nil {
		t.Fatalf("list verification queue: %v", err)
	}
	for _, item := range items {
		if item.Source.RefID == observationID {
			return item
		}
	}
	t.Fatalf("no verification item raised for observation %s", observationID)
	return verificationdomain.Item{}
}

func listAwaitingApplication(t *testing.T, ctx context.Context, repo *verificationpg.Repository) []verificationdomain.Item {
	t.Helper()
	items, err := repo.ListQueue(ctx, verificationQueueParams(true))
	if err != nil {
		t.Fatalf("list awaiting-application page: %v", err)
	}
	return items
}

func weighingVerdictEvent(t *testing.T, observationID, eventID string) eventbus.Event {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"verified_by": ackVerifier,
		"source": map[string]any{
			"module":   domain.VerificationModuleWeighing,
			"ref_type": domain.VerificationRefTypeAnimal,
			"ref_id":   observationID,
		},
	})
	if err != nil {
		t.Fatalf("marshal verdict payload: %v", err)
	}
	return eventbus.Event{
		ID:       eventID,
		Type:     "verification.verdict.approved",
		TenantID: repoTenant,
		Payload:  payload,
	}
}

// verificationQueueParams builds the two pages this test contrasts: the ordinary
// verifier queue (status=pending) and the awaiting-application page. They are
// deliberately the SAME read with one flag flipped -- the whole point is that a
// decided-but-unapplied item is still reachable through the queue, not lost.
func verificationQueueParams(awaitingApplication bool) verificationports.ListQueueParams {
	params := verificationports.ListQueueParams{
		TenantID: repoTenant,
		Status:   verificationdomain.StatusPending,
		Limit:    50,
	}
	if awaitingApplication {
		params.Status = ""
		params.AwaitingApplicationOnly = true
	}
	return params
}

func itemIDs(items []verificationdomain.Item) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ItemID)
	}
	return ids
}
