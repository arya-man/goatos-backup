package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// THE CLOSURE LOOP: a weighing task that is genuinely finished must SAY so.
//
// Before verified_closure.go these tests could not be written, because there was
// no normal completion path at all: a bucket whose every animal had been weighed,
// submitted and verified sat at 'completed' — the same status as one still
// waiting on the verifier — and the only door to a terminal state was
// CloseScope, which demands a leader and a reason.
//
// Every assertion below is on a VALUE, never on "no error": the point is what
// the record says afterwards.

// ---------------------------------------------------------------------------
// small readers, all on the real columns
// ---------------------------------------------------------------------------

func readShedClosure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) (status, closureKind string, closedBy, closeReason *string, closedAt *time.Time) {
	t.Helper()
	if err := pool.QueryRow(ctx, `
SELECT status, COALESCE(closure_kind,''), closed_by::text, close_reason, closed_at
FROM weighing_campaign_sheds
WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, campaignShedID).
		Scan(&status, &closureKind, &closedBy, &closeReason, &closedAt); err != nil {
		t.Fatalf("read shed closure: %v", err)
	}
	return status, closureKind, closedBy, closeReason, closedAt
}

func readCampaignClosureKind(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	var kind string
	if err := pool.QueryRow(ctx, `
SELECT COALESCE(closure_kind,'') FROM weighing_campaigns
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid`, repoTenant, repoCampaign).Scan(&kind); err != nil {
		t.Fatalf("read campaign closure kind: %v", err)
	}
	return kind
}

// submitVerifiedShedBucket drives the REAL operator path for the lump-sum bucket:
// record the shed proof (which IS the submission) and nothing else. It returns the
// observation id the verifier will decide on.
func submitVerifiedShedBucket(t *testing.T, ctx context.Context, repo *Repository, proofID, idem string) string {
	t.Helper()
	obs, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		WeightKg: 410, AnimalCount: 40, ProofArtifactID: proofID,
		IdempotencyKey: idem, RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	return obs.ObservationID
}

func approveShedObservation(t *testing.T, ctx context.Context, repo *Repository, observationID, proofID, eventID string) domain.VerificationVerdictResult {
	t.Helper()
	result, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID:        repoTenant,
		ObservationID:   observationID,
		RefType:         domain.VerificationRefTypeShed,
		Status:          domain.VerificationStatusVerified,
		VerifiedBy:      repoVerifier,
		EvidenceProofID: proofID,
		EventID:         eventID,
	})
	if err != nil {
		t.Fatalf("apply approved verdict: %v", err)
	}
	return result
}

// cancelOtherBucket takes the fixture's OTHER bucket out of the picture so the
// campaign cascade can be observed on the bucket under test. 'canceled' is
// retracted work and is the one status that legitimately never holds a task open.
func cancelBucket(t *testing.T, ctx context.Context, pool *pgxpool.Pool, campaignShedID string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool,
		`UPDATE weighing_campaign_sheds SET status='canceled' WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`,
		repoTenant, campaignShedID)
}

// ---------------------------------------------------------------------------
// 1. THE NORMAL COMPLETION PATH
// ---------------------------------------------------------------------------

// When the LAST submitted item in a bucket is verified, the bucket reaches the
// terminal "done properly" state on the verdict's own transaction — no leader, no
// reason, and recorded as a DIFFERENT kind of ending than a leadership close.
// vcAllParks is the leadership view: authorized in this test's park, no assignee
// filter, which is what every unfiltered call below used to mean.
var vcAllParks = ports.CampaignAccess{AuthorizedParkIDs: []string{repoPark}}

func TestLastVerifiedItemClosesShedAsVerifiedWithNoActorAndNoReason(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obsID := submitVerifiedShedBucket(t, ctx, repo, repoShedProof, "shed:closure-happy")
	// A lump-sum proof IS the submission, so the bucket is already 'completed'
	// and merely WAITING on the verifier. That waiting state is precisely what
	// used to be indistinguishable from a finished one.
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusCompleted)
	if kind := readCampaignClosureKind(t, ctx, pool); kind != "" {
		t.Fatalf("campaign closure_kind=%q before any verdict, want empty", kind)
	}

	result := approveShedObservation(t, ctx, repo, obsID, repoShedProof, "22222222-2222-4222-8222-000000000001")

	if !result.ShedClosed {
		t.Fatal("verdict result ShedClosed=false; the last verified item must close the bucket")
	}
	status, kind, closedBy, closeReason, closedAt := readShedClosure(t, ctx, pool, repoShedScope)
	if status != domain.StatusClosed {
		t.Fatalf("shed status=%q after last verification, want %q", status, domain.StatusClosed)
	}
	if kind != domain.ClosureKindVerified {
		t.Fatalf("shed closure_kind=%q, want %q — a normal completion must not read as a leadership close", kind, domain.ClosureKindVerified)
	}
	if closedBy != nil {
		t.Fatalf("shed closed_by=%q; nobody ended this work, so no actor may be recorded", *closedBy)
	}
	if closeReason != nil {
		t.Fatalf("shed close_reason=%q; nothing was cut short, so there is no reason to record", *closeReason)
	}
	if closedAt == nil || closedAt.IsZero() {
		t.Fatal("shed closed_at not recorded on the verified closure")
	}

	if got := countOutbox(t, ctx, pool, "weighing.shed.verified_closed"); got != 1 {
		t.Fatalf("weighing.shed.verified_closed outbox rows=%d, want 1", got)
	}
	// The exception event must NOT fire: downstream distinguishes the two.
	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 0 {
		t.Fatalf("weighing.shed.closed outbox rows=%d on a normal completion, want 0", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_verified_closed"); got != 1 {
		t.Fatalf("weighing.scope_verified_closed audit rows=%d, want 1", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_closed"); got != 0 {
		t.Fatalf("weighing.scope_closed audit rows=%d on a normal completion, want 0", got)
	}

	var payloadKind string
	var payloadVerified int
	if err := pool.QueryRow(ctx, `
SELECT payload->'payload'->>'closure_kind', (payload->'payload'->>'verified_count')::int
FROM outbox_messages
WHERE tenant_id=$1::uuid AND event_type='weighing.shed.verified_closed'`, repoTenant).
		Scan(&payloadKind, &payloadVerified); err != nil {
		t.Fatalf("read verified-closure event payload: %v", err)
	}
	if payloadKind != domain.ClosureKindVerified || payloadVerified != 1 {
		t.Fatalf("verified-closure payload kind=%q verified_count=%d, want %q/1", payloadKind, payloadVerified, domain.ClosureKindVerified)
	}
}

// ---------------------------------------------------------------------------
// 2. ONE UNVERIFIED ITEM HOLDS THE BUCKET OPEN
// ---------------------------------------------------------------------------

// Closure is "every SUBMITTED item is verified", so a bucket carrying a second
// piece of evidence nobody has looked at must NOT close on the first approval.
// This is the same predicate CloseScope's gate uses, and it is never a comparison
// against an expected animal count.
func TestShedWithOneUnverifiedItemDoesNotClose(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// Two individual animals weighed into ONE bucket, then submitted together.
	first, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "closure-rfid-1", WeightKg: 12.4,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:closure-1", RecordedBy: repoOperator,
	})
	if err != nil {
		t.Fatalf("record first observation: %v", err)
	}
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		ScannedIdentifier: "closure-rfid-2", WeightKg: 13.1,
		ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
		IdempotencyKey: "animal:closure-2", RecordedBy: repoOperator,
	}); err != nil {
		t.Fatalf("record second observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:closure-partial", []string{"closure-rfid-1", "closure-rfid-2"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	result, err := repo.ApplyVerificationVerdict(ctx, domain.VerificationVerdict{
		TenantID: repoTenant, ObservationID: first.ObservationID,
		RefType: domain.VerificationRefTypeAnimal, Status: domain.VerificationStatusVerified,
		VerifiedBy: repoVerifier, EvidenceProofID: repoExpectedShedProof,
		EventID: "22222222-2222-4222-8222-000000000002",
	})
	if err != nil {
		t.Fatalf("apply approved verdict: %v", err)
	}

	if result.ShedClosed {
		t.Fatal("verdict result ShedClosed=true with a second item still unverified")
	}
	status, kind, _, _, _ := readShedClosure(t, ctx, pool, repoAnimalScope)
	if status != domain.StatusCompleted {
		t.Fatalf("shed status=%q with one item unverified, want %q", status, domain.StatusCompleted)
	}
	if kind != "" {
		t.Fatalf("shed closure_kind=%q with one item unverified, want empty", kind)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.verified_closed"); got != 0 {
		t.Fatalf("weighing.shed.verified_closed outbox rows=%d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// 3. THE CASCADE: THE TASK ENDS WHEN ITS LAST SHED DOES
// ---------------------------------------------------------------------------

func TestCampaignClosesVerifiedWhenItsLastShedDoes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// The fixture holds TWO buckets. While the individual one is still live the
	// task is NOT finished, however settled the lump-sum one becomes.
	obsID := submitVerifiedShedBucket(t, ctx, repo, repoShedProof, "shed:cascade")
	result := approveShedObservation(t, ctx, repo, obsID, repoShedProof, "22222222-2222-4222-8222-000000000003")
	if !result.ShedClosed {
		t.Fatal("lump-sum bucket did not close on its last verification")
	}
	if result.CampaignClosed {
		t.Fatal("campaign closed while its other bucket was still live")
	}
	assertCampaignStatus(t, ctx, pool, "published")
	if got := countOutbox(t, ctx, pool, "weighing.campaign.verified_closed"); got != 0 {
		t.Fatalf("campaign verified-closed events=%d while a bucket is live, want 0", got)
	}

	// Retract the remaining bucket, then re-run a verdict so the cascade is
	// re-evaluated. A rework bounces the shed proof back and a fresh approval
	// settles it again — the real path, not a hand-written status update.
	cancelBucket(t, ctx, pool, repoAnimalScope)
	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoShedScope, repoVerifier, "reopen:cascade", "re-shoot"); err != nil {
		t.Fatalf("reopen bucket: %v", err)
	}
	if kind := readCampaignClosureKind(t, ctx, pool); kind != "" {
		t.Fatalf("campaign closure_kind=%q after reopen, want cleared", kind)
	}
	secondObs := submitVerifiedShedBucket(t, ctx, repo, repoShedProofTwo, "shed:cascade-2")
	final := approveShedObservation(t, ctx, repo, secondObs, repoShedProofTwo, "22222222-2222-4222-8222-000000000004")

	if !final.CampaignClosed {
		t.Fatal("campaign did not close when its last non-terminal bucket settled")
	}
	assertCampaignStatus(t, ctx, pool, domain.StatusClosed)
	if kind := readCampaignClosureKind(t, ctx, pool); kind != domain.ClosureKindVerified {
		t.Fatalf("campaign closure_kind=%q, want %q", kind, domain.ClosureKindVerified)
	}
	if got := countOutbox(t, ctx, pool, "weighing.campaign.verified_closed"); got != 1 {
		t.Fatalf("weighing.campaign.verified_closed outbox rows=%d, want 1", got)
	}
	if got := countOutbox(t, ctx, pool, "weighing.campaign.closed"); got != 0 {
		t.Fatalf("weighing.campaign.closed outbox rows=%d on a normal completion, want 0", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.campaign_verified_closed"); got != 1 {
		t.Fatalf("weighing.campaign_verified_closed audit rows=%d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// 4. REDELIVERY IS A NO-OP
// ---------------------------------------------------------------------------

// The verdict arrives over an at-least-once durable bus. The same verdict
// delivered twice must not close twice, must not emit twice, and must not move
// closed_at.
func TestRedeliveredVerdictDoesNotCloseOrEmitTwice(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obsID := submitVerifiedShedBucket(t, ctx, repo, repoShedProof, "shed:redelivery")
	first := approveShedObservation(t, ctx, repo, obsID, repoShedProof, "22222222-2222-4222-8222-000000000005")
	if !first.ShedClosed {
		t.Fatal("first delivery did not close the bucket")
	}
	_, _, _, _, firstClosedAt := readShedClosure(t, ctx, pool, repoShedScope)

	replay := approveShedObservation(t, ctx, repo, obsID, repoShedProof, "22222222-2222-4222-8222-000000000005")

	// The replay REPORTS the original outcome (it is read back from the
	// idempotency snapshot) and performs none of it again.
	if !replay.ShedClosed {
		t.Fatal("replay lost the recorded outcome; the snapshot must replay the original result")
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.verified_closed"); got != 1 {
		t.Fatalf("weighing.shed.verified_closed outbox rows=%d after redelivery, want 1", got)
	}
	if got := countAudit(t, ctx, pool, "weighing.scope_verified_closed"); got != 1 {
		t.Fatalf("weighing.scope_verified_closed audit rows=%d after redelivery, want 1", got)
	}
	_, kind, _, _, replayClosedAt := readShedClosure(t, ctx, pool, repoShedScope)
	if kind != domain.ClosureKindVerified {
		t.Fatalf("shed closure_kind=%q after redelivery, want %q", kind, domain.ClosureKindVerified)
	}
	if !replayClosedAt.Equal(*firstClosedAt) {
		t.Fatalf("redelivery moved closed_at from %s to %s", firstClosedAt, replayClosedAt)
	}
}

// ---------------------------------------------------------------------------
// 5. THE EXCEPTION PATH SURVIVES AND STAYS DISTINGUISHABLE
// ---------------------------------------------------------------------------

// A leader ending work early is a DIFFERENT act, and the record must keep saying
// so: reason still mandatory, own audit action, own event, and a closure_kind
// that is not 'verified'.
func TestReasonRequiredExceptionCloseStillWorksAndStaysDistinguishable(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// CloseScope on a bucket with no submitted evidence: the gate is satisfied
	// (nothing is pending) and the leader's reason is what justifies ending it.
	closed, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "shed emptied early", ClosedBy: repoVerifier, IdempotencyKey: "close:exception-path",
	})
	if err != nil {
		t.Fatalf("close scope: %v", err)
	}
	if closed.Status != domain.StatusClosed {
		t.Fatalf("close result status=%q, want %q", closed.Status, domain.StatusClosed)
	}
	status, kind, closedBy, closeReason, _ := readShedClosure(t, ctx, pool, repoAnimalScope)
	if status != domain.StatusClosed {
		t.Fatalf("exception-closed shed status=%q, want %q", status, domain.StatusClosed)
	}
	if kind != domain.ClosureKindEarly {
		t.Fatalf("exception-closed shed closure_kind=%q, want %q", kind, domain.ClosureKindEarly)
	}
	if closedBy == nil || *closedBy != repoVerifier {
		t.Fatalf("exception-closed shed closed_by=%v, want the leader who ended it", closedBy)
	}
	if closeReason == nil || *closeReason != "shed emptied early" {
		t.Fatalf("exception-closed shed close_reason=%v, want the leader's reason", closeReason)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.closed"); got != 1 {
		t.Fatalf("weighing.shed.closed outbox rows=%d, want 1", got)
	}
	if got := countOutbox(t, ctx, pool, "weighing.shed.verified_closed"); got != 0 {
		t.Fatalf("weighing.shed.verified_closed outbox rows=%d on an early close, want 0", got)
	}

	// There is no third kind: abandon was deleted (the vocabulary is close or
	// reopen only), so a leader ending work early is ALWAYS ClosureKindEarly and
	// 'verified' stays reserved for the completion path no human performs.
}

// ---------------------------------------------------------------------------
// 6. APPROVAL IS VISIBLE ON THE READ
// ---------------------------------------------------------------------------

// Rejection had rework_count and waiting had pending_verification_count, but
// approval had NO field on any weighing read — `grep -c verification_status` on
// the weighing HTTP handler returned 0. verified_count is its counterpart, and
// closure_kind tells the surface HOW the bucket ended.
func TestBucketReadExposesVerifiedCountAndClosureKind(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	obsID := submitVerifiedShedBucket(t, ctx, repo, repoShedProof, "shed:read-model")

	page, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 20, vcAllParks)
	if err != nil {
		t.Fatalf("list campaign sheds before verdict: %v", err)
	}
	before := findShed(t, page.Items, repoShedScope)
	if before.VerifiedCount != 0 || before.PendingVerificationCount != 1 {
		t.Fatalf("before verdict verified=%d pending=%d, want 0/1", before.VerifiedCount, before.PendingVerificationCount)
	}
	if before.ClosureKind != "" {
		t.Fatalf("before verdict closure_kind=%q, want empty", before.ClosureKind)
	}

	approveShedObservation(t, ctx, repo, obsID, repoShedProof, "22222222-2222-4222-8222-000000000006")

	page, err = repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 20, vcAllParks)
	if err != nil {
		t.Fatalf("list campaign sheds after verdict: %v", err)
	}
	after := findShed(t, page.Items, repoShedScope)
	if after.VerifiedCount != 1 {
		t.Fatalf("after verdict verified_count=%d, want 1 — approval must be visible", after.VerifiedCount)
	}
	if after.PendingVerificationCount != 0 || after.ReworkCount != 0 {
		t.Fatalf("after verdict pending=%d rework=%d, want 0/0", after.PendingVerificationCount, after.ReworkCount)
	}
	if after.ClosureKind != domain.ClosureKindVerified {
		t.Fatalf("after verdict closure_kind=%q, want %q", after.ClosureKind, domain.ClosureKindVerified)
	}
	if after.Status != domain.StatusClosed {
		t.Fatalf("after verdict status=%q, want %q", after.Status, domain.StatusClosed)
	}
}

func findShed(t *testing.T, items []domain.CampaignShed, campaignShedID string) domain.CampaignShed {
	t.Helper()
	for _, item := range items {
		if item.CampaignShedID == campaignShedID {
			return item
		}
	}
	t.Fatalf("bucket %s not present in the read model", campaignShedID)
	return domain.CampaignShed{}
}

// ---------------------------------------------------------------------------
// 7. PROJECTION GRAIN PROOF FOR THE NEW CLOSURE FACTS
// ---------------------------------------------------------------------------

// closure_kind and verified_count are read through the SHARED bucket fragment
// (readyToCloseCountsSQL), which every weighing bucket surface selects. That
// fragment reaches weighing_observations and weighing_shed_observations through
// correlated subqueries, so the four ways a projection normally breaks all have
// to be shown NOT to break here:
//
//	CARDINALITY  a bucket holding several observations must still be ONE row —
//	             the fragment must not fan the bucket out per observation.
//	PAGE         the facts must be identical whether a bucket arrives on page one
//	             or page two; keyset pages must not drop or duplicate a bucket.
//	SCOPE        the operator filter must narrow the rows, not the facts.
//	STATUS       every terminal kind must report its own closure_kind, and the
//	             three verdict counts must partition the bucket's submitted work.
func TestCampaignShedProjectionClosureFactsOneToManyPageBoundaryScopeHierarchyStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	// CARDINALITY: THREE observations in ONE individual bucket.
	for i, tag := range []string{"grain-rfid-1", "grain-rfid-2", "grain-rfid-3"} {
		if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
			TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
			ScannedIdentifier: tag, WeightKg: float64(12 + i),
			ProofArtifactID: repoExpectedShedProof, ActualLocationID: repoExpectedShed,
			IdempotencyKey: "animal:grain-" + tag, RecordedBy: repoOperator,
		}); err != nil {
			t.Fatalf("record observation %s: %v", tag, err)
		}
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator,
		"submit:grain", []string{"grain-rfid-1", "grain-rfid-2", "grain-rfid-3"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	// The lump-sum bucket is submitted and then fully verified, so the two
	// buckets end in DIFFERENT terminal kinds.
	shedObs := submitVerifiedShedBucket(t, ctx, repo, repoShedProof, "shed:grain")
	approveShedObservation(t, ctx, repo, shedObs, repoShedProof, "22222222-2222-4222-8222-000000000007")
	// The individual bucket is ended by a leader instead — the exception path.
	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "operator reassigned", ClosedBy: repoVerifier, IdempotencyKey: "close:grain",
	}); err != nil {
		t.Fatalf("close individual bucket: %v", err)
	}

	whole, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 20, vcAllParks)
	if err != nil {
		t.Fatalf("list campaign sheds: %v", err)
	}
	if len(whole.Items) != 2 || whole.TotalCount != 2 {
		t.Fatalf("bucket rows=%d total=%d, want 2/2 — three observations must not fan one bucket into three rows",
			len(whole.Items), whole.TotalCount)
	}

	// STATUS MATRIX: each terminal kind reports itself, and the verdict counts
	// partition the bucket's submitted work.
	individual := findShed(t, whole.Items, repoAnimalScope)
	if individual.Status != domain.StatusClosed || individual.ClosureKind != domain.ClosureKindAbandoned {
		t.Fatalf("individual bucket status=%q closure_kind=%q, want closed/%s",
			individual.Status, individual.ClosureKind, domain.ClosureKindAbandoned)
	}
	if individual.AnimalsSubmittedCount != 3 {
		t.Fatalf("individual bucket animals_submitted=%d, want 3", individual.AnimalsSubmittedCount)
	}
	if individual.VerifiedCount != 0 || individual.PendingVerificationCount != 3 {
		t.Fatalf("individual bucket verified=%d pending=%d, want 0/3 — an abandon accepts nothing",
			individual.VerifiedCount, individual.PendingVerificationCount)
	}
	lump := findShed(t, whole.Items, repoShedScope)
	if lump.Status != domain.StatusClosed || lump.ClosureKind != domain.ClosureKindVerified {
		t.Fatalf("lump-sum bucket status=%q closure_kind=%q, want closed/%s",
			lump.Status, lump.ClosureKind, domain.ClosureKindVerified)
	}
	if lump.VerifiedCount != 1 || lump.PendingVerificationCount != 0 || lump.ReworkCount != 0 {
		t.Fatalf("lump-sum bucket verified=%d pending=%d rework=%d, want 1/0/0",
			lump.VerifiedCount, lump.PendingVerificationCount, lump.ReworkCount)
	}

	// PAGE BOUNDARY: one bucket per page, and every fact identical to the
	// whole-set read.
	firstPage, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 1, vcAllParks)
	if err != nil {
		t.Fatalf("list campaign sheds page 1: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("page 1 items=%d cursor=%q, want 1 item and a cursor", len(firstPage.Items), firstPage.NextCursor)
	}
	secondPage, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, firstPage.NextCursor, 1, vcAllParks)
	if err != nil {
		t.Fatalf("list campaign sheds page 2: %v", err)
	}
	if len(secondPage.Items) != 1 {
		t.Fatalf("page 2 items=%d, want 1", len(secondPage.Items))
	}
	paged := append(append([]domain.CampaignShed{}, firstPage.Items...), secondPage.Items...)
	if len(paged) != 2 || paged[0].CampaignShedID == paged[1].CampaignShedID {
		t.Fatalf("paging returned %d rows with ids %s/%s, want the two distinct buckets",
			len(paged), paged[0].CampaignShedID, paged[1].CampaignShedID)
	}
	pagedLump := findShed(t, paged, repoShedScope)
	if pagedLump.ClosureKind != lump.ClosureKind || pagedLump.VerifiedCount != lump.VerifiedCount {
		t.Fatalf("paged bucket closure_kind=%q verified=%d, want %q/%d — a fact must not change with the page",
			pagedLump.ClosureKind, pagedLump.VerifiedCount, lump.ClosureKind, lump.VerifiedCount)
	}

	// SCOPE HIERARCHY: the operator filter narrows the ROWS. Both buckets belong
	// to repoOperator, so another operator in the same park sees none of them.
	mine, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 20, ports.CampaignAccess{AuthorizedParkIDs: []string{repoPark}, AssigneeUserID: repoOperator})
	if err != nil {
		t.Fatalf("list campaign sheds for assigned operator: %v", err)
	}
	if len(mine.Items) != 2 {
		t.Fatalf("assigned operator sees %d buckets, want 2", len(mine.Items))
	}
	if mineLump := findShed(t, mine.Items, repoShedScope); mineLump.VerifiedCount != lump.VerifiedCount {
		t.Fatalf("operator-scoped verified_count=%d, want %d — scope narrows rows, never facts",
			mineLump.VerifiedCount, lump.VerifiedCount)
	}
	others, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 20, ports.CampaignAccess{AuthorizedParkIDs: []string{repoPark}, AssigneeUserID: repoOtherOp})
	if err != nil {
		t.Fatalf("list campaign sheds for another operator: %v", err)
	}
	if len(others.Items) != 0 {
		t.Fatalf("unassigned operator sees %d buckets, want 0", len(others.Items))
	}
}
