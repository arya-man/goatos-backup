package postgres

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

// PEN RECONCILIATION against the real schema (maintainer decision 2026-09-02).
//
// The scenarios here are the business rules, not CRUD: a scanned animal whose registered pen
// disagrees with the weighed pen raises exactly one card; a matching animal and an unknown tag
// raise none; the raise is idempotent across re-deliveries; a partition spelled "Part 1" in
// the register matches a bucket labelled "1" (the scrubbed-key rule); the completion is
// proof-gated and replay-safe; and the verdict cycle open -> pending_verification -> rework ->
// pending_verification -> completed ends with a NEW mismatch card being raisable again while a
// non-completed card blocks duplicates.

const (
	penRecOperator = "00000000-0000-4000-8000-00000000a201"
	penRecCampaign = "00000000-0000-4000-8000-00000000b201"
	penRecProof    = "00000000-0000-4000-8000-00000000c201"
)

func seedPenRecGoatWithTag(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, shedID, partition, tag string,
) {
	t.Helper()
	seedCustodianParty(t, ctx, pool)
	seedApprovalGoat(t, ctx, pool, goatID, shedID)
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		countsTenant, goatID, shedID, partition); err != nil {
		t.Fatalf("seed goat partition: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, lower($3), $3, 'active', now(), 'v1')`,
		countsTenant, goatID, tag); err != nil {
		t.Fatalf("seed goat identifier: %v", err)
	}
}

// seedPenRecBucket creates one submitted INDIVIDUAL weighing bucket at (locationID, partition)
// with one observation per scanned tag, and returns the campaign_shed_id.
func seedPenRecBucket(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, bucketID, locationID, partition string, tags []string,
) {
	t.Helper()
	// weighing_campaign_sheds_operator_park_bound refuses an operator with no ACTIVE park
	// grant, so the fixture operator is scoped to the park the bucket lives in.
	if _, err := pool.Exec(ctx, `
INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $3::uuid, 'active', now())
ON CONFLICT DO NOTHING`, countsTenant, penRecOperator, countsPark); err != nil {
		t.Fatalf("seed operator scope grant: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, mime_type, upload_state, scope_type, scope_id, subject_type, subject_id, proof_type, uploaded_by, uploaded_at)
VALUES ($1::uuid, $2::uuid, 'local', 'pen-rec-test/' || $1, 'video/mp4', 'completed', 'shed', $3::uuid, 'shed', $3::uuid, 'video', $4::uuid, now())
ON CONFLICT (proof_id) DO NOTHING`, penRecProof, countsTenant, locationID, penRecOperator); err != nil {
		t.Fatalf("seed proof artifact: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date, start_business_date, status, operator_user_id, created_by)
VALUES ($1::uuid, $2::uuid, $3::uuid, DATE '2026-09-01', DATE '2026-09-07', DATE '2026-09-01', 'in_progress', $4::uuid, $4::uuid)
ON CONFLICT (campaign_id) DO NOTHING`, penRecCampaign, countsTenant, countsPark, penRecOperator); err != nil {
		t.Fatalf("seed weighing campaign: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO weighing_campaign_sheds (campaign_shed_id, campaign_id, tenant_id, location_id, location_type, display_name, weighing_category, status, operator_user_id, park_id, partition_label)
SELECT $1::uuid, $2::uuid, $3::uuid, $4::uuid, 'shed', l.name, 'individual_animal', 'completed', $5::uuid, $6::uuid, NULLIF($7, '')
FROM locations l WHERE l.location_id = $4::uuid`,
		bucketID, penRecCampaign, countsTenant, locationID, penRecOperator, countsPark, partition); err != nil {
		t.Fatalf("seed weighing bucket: %v", err)
	}
	for i, tag := range tags {
		if _, err := pool.Exec(ctx, `
INSERT INTO weighing_observations (tenant_id, campaign_id, campaign_shed_id, scanned_identifier, weight_kg, proof_artifact_id, recorded_by, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 21.5, $5::uuid, $6::uuid, $3 || ':' || $4 || ':' || $7::text)`,
			countsTenant, penRecCampaign, bucketID, tag, penRecProof, penRecOperator, strconv.Itoa(i)); err != nil {
			t.Fatalf("seed weighing observation %s: %v", tag, err)
		}
	}
}

func raisePenRec(t *testing.T, ctx context.Context, repo *Repository, bucketID string) int {
	t.Helper()
	raised, err := repo.RaisePenReconciliationCards(ctx, domain.PenReconciliationRaiseCommand{
		TenantID:       countsTenant,
		CampaignID:     penRecCampaign,
		CampaignShedID: bucketID,
		RaisedAt:       time.Date(2026, 9, 2, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("raise pen reconciliation: %v", err)
	}
	return raised
}

// TestPenReconciliationRaiseFindsOnlyTheMismatchedAnimal is the core rule: the bucket at shed
// A scanned three tags — an animal registered in shed B (card), an animal registered in shed A
// (no card), and a tag no animal carries (no card, free-flow stays honest).
func TestPenReconciliationRaiseFindsOnlyTheMismatchedAnimal(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	strayed := "00000000-0000-4000-8000-00000000f101"
	resident := "00000000-0000-4000-8000-00000000f102"
	seedPenRecGoatWithTag(t, ctx, pool, strayed, countsShedB, "whole", "1420 1001")
	seedPenRecGoatWithTag(t, ctx, pool, resident, countsShedA, "whole", "1420 1002")
	bucket := "00000000-0000-4000-8000-00000000e101"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "", []string{"1420 1001", "1420 1002", "9999 0000"})

	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("raised = %d, want exactly the strayed animal", raised)
	}
	// Idempotent: a duplicate bus delivery inserts nothing.
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 0 {
		t.Fatalf("duplicate delivery raised = %d, want 0", raised)
	}

	page, err := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("open cards = %d, want 1", len(page.Items))
	}
	card := page.Items[0]
	if card.GoatID != strayed || card.ScannedIdentifier != "1420 1001" {
		t.Fatalf("card = %+v", card)
	}
	if card.FoundLocationID != countsShedA || card.RegisteredShedID != countsShedB {
		t.Fatalf("card pens: found=%s registered=%s", card.FoundLocationID, card.RegisteredShedID)
	}
	if card.RegisteredShedName != "CPT Shed 2" || card.FoundDisplayName != "CPT Shed 1" {
		t.Fatalf("card labels: found=%q registered=%q", card.FoundDisplayName, card.RegisteredShedName)
	}
	if card.PrimaryActionKey != "execute" {
		t.Fatalf("primary action = %q", card.PrimaryActionKey)
	}
	if page.StatusCounts.Open != 1 || page.StatusCounts.All != 1 {
		t.Fatalf("status counts = %+v", page.StatusCounts)
	}
}

// TestPenReconciliationPartitionSpellingsCompareScrubbed pins the scrubbed-key rule: the
// register writes "Part 1" while the bucket label says "1", and the two mean the SAME pen —
// comparing them raw would card every animal in a partitioned shed.
func TestPenReconciliationPartitionSpellingsCompareScrubbed(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	samePen := "00000000-0000-4000-8000-00000000f201"
	otherPen := "00000000-0000-4000-8000-00000000f202"
	seedPenRecGoatWithTag(t, ctx, pool, samePen, countsShedA, "Part 1", "1420 2001")
	seedPenRecGoatWithTag(t, ctx, pool, otherPen, countsShedA, "Part 2", "1420 2002")
	bucket := "00000000-0000-4000-8000-00000000e201"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "1", []string{"1420 2001", "1420 2002"})

	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("raised = %d, want only the Part 2 animal weighed in pen 1", raised)
	}
	page, err := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].GoatID != otherPen {
		t.Fatalf("cards = %+v, want only the cross-pen animal", page.Items)
	}
	if page.Items[0].RegisteredPartitionLabel != "Part 2" || page.Items[0].FoundPartitionLabel != "1" {
		t.Fatalf("partitions = %+v", page.Items[0])
	}
}

// TestPenReconciliationAliasBucketResolvesToThePhysicalPen pins the legacy-alias rule: STG
// still carries active alias location rows ("CPT Shed 2 1" as its own location) while the
// register puts animals on the physical shed + partition. A bucket weighed at the alias must
// compare as (physical shed, partition 1) — a raw id comparison would card every animal in
// the pen.
func TestPenReconciliationAliasBucketResolvesToThePhysicalPen(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	alias := "00000000-0000-4000-8000-00000000d401"
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'CPT-S2-1', 'CPT Shed 2 1', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, alias, countsTenant, countsPark); err != nil {
		t.Fatalf("seed alias location: %v", err)
	}

	resident := "00000000-0000-4000-8000-00000000f401"
	strayed := "00000000-0000-4000-8000-00000000f402"
	seedPenRecGoatWithTag(t, ctx, pool, resident, countsShedB, "Part 1", "1420 4001")
	seedPenRecGoatWithTag(t, ctx, pool, strayed, countsShedB, "Part 2", "1420 4002")
	bucket := "00000000-0000-4000-8000-00000000e401"
	seedPenRecBucket(t, ctx, pool, bucket, alias, "", []string{"1420 4001", "1420 4002"})

	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("raised = %d, want only the Part 2 animal scanned in the alias pen 1", raised)
	}
	page, err := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20,
	})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %v items=%d", err, len(page.Items))
	}
	if page.Items[0].GoatID != strayed {
		t.Fatalf("carded goat = %s, want the cross-pen animal", page.Items[0].GoatID)
	}
}

// TestPenReconciliationCompletionAndVerdictCycle drives one card through the whole life:
// proof-gated submit, idempotent replay, same-key/different-payload conflict, verifier rework,
// re-shoot, approve — and only a COMPLETED card frees the animal for a fresh card.
func TestPenReconciliationCompletionAndVerdictCycle(t *testing.T) {
	ctx := context.Background()
	pool := setupCountsDB(t, ctx)
	repo := NewRepository(pool, 5*time.Second)

	strayed := "00000000-0000-4000-8000-00000000f301"
	seedPenRecGoatWithTag(t, ctx, pool, strayed, countsShedB, "whole", "1420 3001")
	bucket := "00000000-0000-4000-8000-00000000e301"
	seedPenRecBucket(t, ctx, pool, bucket, countsShedA, "", []string{"1420 3001"})
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("raised = %d", raised)
	}
	page, err := repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketOpen, PageSize: 20,
	})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %v items=%d", err, len(page.Items))
	}
	cardID := page.Items[0].CardID

	complete := func(key, fingerprint, proof string) (domain.PenReconciliationCompletionResult, bool, error) {
		return repo.CompletePenReconciliationCard(ctx, domain.PenReconciliationCompletionCommand{
			TenantID: countsTenant, CardID: cardID,
			CompletedByUserID: penRecOperator,
			CompletedAt:       time.Date(2026, 9, 2, 14, 0, 0, 0, time.UTC),
			ProofRef:          proof, IdempotencyKey: key, RequestFingerprint: fingerprint,
		})
	}

	// Blank proof is refused before any state changes.
	if _, _, err := complete("key-1", "fp-1", " "); !errors.Is(err, ports.ErrPenReconciliationProofRequired) {
		t.Fatalf("blank proof err = %v", err)
	}

	result, replay, err := complete("key-1", "fp-1", "proof-return-1")
	if err != nil || replay || result.Status != domain.PenReconciliationStatusPendingVerification {
		t.Fatalf("first complete: %+v replay=%v err=%v", result, replay, err)
	}
	if result.RegisteredShedName != "CPT Shed 2" {
		t.Fatalf("registered shed name = %q", result.RegisteredShedName)
	}

	// Exact replay echoes; same key with a different payload conflicts; a fresh key against the
	// already-submitted card is not actionable.
	if _, replay, err := complete("key-1", "fp-1", "proof-return-1"); err != nil || !replay {
		t.Fatalf("replay: replay=%v err=%v", replay, err)
	}
	if _, _, err := complete("key-1", "fp-DIFFERENT", "proof-return-9"); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("conflict err = %v", err)
	}
	if _, _, err := complete("key-2", "fp-2", "proof-return-2"); !errors.Is(err, ports.ErrPenReconciliationNotActionable) {
		t.Fatalf("not-actionable err = %v", err)
	}

	// While the card is not completed, the same animal gains no second card.
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 0 {
		t.Fatalf("open card duplicated: raised = %d", raised)
	}

	// Verifier reject -> rework, and the operator re-shoots with a NEW key + proof.
	verdict := domain.PenReconciliationVerdictCommand{
		TenantID: countsTenant, CardID: cardID, VerifiedBy: penRecOperator,
		VerifiedAt: time.Date(2026, 9, 2, 15, 0, 0, 0, time.UTC),
		Reason:     "animal not visible",
	}
	if err := repo.BouncePenReconciliationForRework(ctx, verdict); err != nil {
		t.Fatalf("bounce: %v", err)
	}
	page, err = repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketRework, PageSize: 20,
	})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("rework list: %v items=%d", err, len(page.Items))
	}
	if page.Items[0].ReworkReason == nil || *page.Items[0].ReworkReason != "animal not visible" {
		t.Fatalf("rework reason = %+v", page.Items[0].ReworkReason)
	}
	if _, replay, err := complete("key-3", "fp-3", "proof-return-3"); err != nil || replay {
		t.Fatalf("re-shoot: replay=%v err=%v", replay, err)
	}

	// Approve -> completed, and the animal may be carded again by a later weighing.
	if err := repo.ApplyVerifiedPenReconciliation(ctx, verdict); err != nil {
		t.Fatalf("apply: %v", err)
	}
	page, err = repo.ListPenReconciliationCards(ctx, domain.PenReconciliationQuery{
		TenantID: countsTenant, Status: domain.PenReconciliationBucketCompleted, PageSize: 20,
	})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("completed list: %v items=%d", err, len(page.Items))
	}
	if raised := raisePenRec(t, ctx, repo, bucket); raised != 1 {
		t.Fatalf("post-completion raise = %d, want a fresh card for the still-mismatched animal", raised)
	}
}
