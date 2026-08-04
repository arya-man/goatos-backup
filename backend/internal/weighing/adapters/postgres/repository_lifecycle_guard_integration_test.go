package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// -----------------------------------------------------------------------------
// TASK A: closed/canceled/completed write guards
// -----------------------------------------------------------------------------

// After CloseScope, an individual bucket must reject a NEW free-flow scan: the
// bucket is 'closed', not 'pending'/'in_progress', so it must never accept
// capture again.
func TestFreeFlowScanRejectedAfterCloseScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "test-close", ClosedBy: repoOperator, IdempotencyKey: "close:animal-scope",
	}); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "closed-bucket-rfid",
		WeightKg:          12.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:closed-bucket",
		RecordedBy:        repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("scan after close err=%v, want ErrImmutable", err)
	}
}

// After CloseScope, SubmitIndividualScope must not complete/submit the closed
// bucket.
func TestSubmitIndividualScopeRejectedAfterCloseScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const scannedTag = "submit-after-close-rfid"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          12.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:pre-close-scan",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}

	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope,
		Reason: "test-close", ClosedBy: repoOperator, IdempotencyKey: "close:animal-scope-submit",
	}); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)

	err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:closed-bucket", []string{scannedTag})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("submit after close err=%v, want ErrImmutable", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusClosed)
}

// After CloseScope, a lump-sum write must NEVER resurrect the closed bucket
// into completed.
func TestLumpSumObservationRejectedAfterCloseScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.CloseScope(ctx, domain.CloseCommand{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope,
		Reason: "test-close", ClosedBy: repoOperator, IdempotencyKey: "close:shed-scope",
	}); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusClosed)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 415, AnimalCount: 1,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:after-close", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("lump-sum after close err=%v, want ErrImmutable", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, domain.StatusClosed)
}

// A lump-sum write against a canceled bucket must be rejected, never
// resurrecting it into completed.
func TestLumpSumObservationRejectedOnCanceledBucket(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	execWeighingTestSQL(t, ctx, pool, `UPDATE weighing_campaign_sheds SET status='canceled' WHERE tenant_id=$1::uuid AND campaign_shed_id=$2::uuid`, repoTenant, repoShedScope)
	repo := NewRepository(pool, 5*time.Second)

	_, err := repo.RecordShedObservation(ctx, domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 416, AnimalCount: 1,
		ProofArtifactID: repoShedProof, IdempotencyKey: "shed:canceled-bucket", RecordedBy: repoOperator,
	})
	if !errors.Is(err, ports.ErrImmutable) {
		t.Fatalf("lump-sum on canceled bucket err=%v, want ErrImmutable", err)
	}
	assertScopeStatus(t, ctx, pool, repoShedScope, "canceled")
}

// ReopenScope must allow a NEW scan again: completed -> in_progress -> scan
// accepted.
func TestReopenScopeAllowsNewScanAgain(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const scannedTag = "reopen-then-scan-rfid"
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          10.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:reopen-pre",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:reopen-flow", []string{scannedTag}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)

	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "reopen:flow", "operator-requested-recheck"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusInProgress)

	// A NEW tag (not the previously submitted one) must be freely acceptable.
	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "reopen-new-tag-rfid",
		WeightKg:          10.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:reopen-new-scan",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("scan after reopen: %v", err)
	}
}

// -----------------------------------------------------------------------------
// TASK B: duplicate RFID rule
// -----------------------------------------------------------------------------

// A scanned_identifier already captured AND SUBMITTED in an earlier round must
// be rejected on rescan after reopen, not silently update the old submitted row.
func TestDuplicateScanRejectedAfterSubmitAndReopen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const scannedTag = "abc123"
	first, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          9.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:dup-first-scan",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("record first observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:dup-flow", []string{scannedTag}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "reopen:dup-flow", "recheck"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}

	_, err = repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          9.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:dup-rescan",
		RecordedBy:        repoOperator,
	})
	if !errors.Is(err, ports.ErrDuplicateScan) {
		t.Fatalf("rescan of submitted tag err=%v, want ErrDuplicateScan", err)
	}

	// The original submitted row must be untouched (no silent update, no new row).
	var count int
	var weight float64
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int, max(weight_kg)::float8
FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND lower(btrim(scanned_identifier))=lower(btrim($4))`,
		repoTenant, repoCampaign, repoAnimalScope, scannedTag).Scan(&count, &weight); err != nil {
		t.Fatalf("read observations for tag: %v", err)
	}
	if count != 1 {
		t.Fatalf("observation rows for duplicated tag=%d, want exactly 1 (no new row inserted)", count)
	}
	if weight != first.WeightKg {
		t.Fatalf("observation weight=%v, want original %v (must not be updated by the rejected duplicate)", weight, first.WeightKg)
	}
}

// The duplicate check is case-insensitive and trimmed: "abc123" and "ABC123"
// are the same tag.
func TestDuplicateScanRejectionIsCaseInsensitiveAndTrimmed(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	if _, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "abc123",
		WeightKg:          8.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:ci-first-scan",
		RecordedBy:        repoOperator,
	}); err != nil {
		t.Fatalf("record first observation: %v", err)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:ci-flow", []string{"abc123"}); err != nil {
		t.Fatalf("submit individual scope: %v", err)
	}
	if _, err := repo.ReopenScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "reopen:ci-flow", "recheck"); err != nil {
		t.Fatalf("reopen scope: %v", err)
	}

	_, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: "  ABC123  ",
		WeightKg:          8.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:ci-rescan",
		RecordedBy:        repoOperator,
	})
	if !errors.Is(err, ports.ErrDuplicateScan) {
		t.Fatalf("case/whitespace-variant rescan err=%v, want ErrDuplicateScan", err)
	}
}

// A rescan of a tag still in the CURRENT un-submitted round updates the same
// row -- this is the existing correct behaviour, kept and made explicit here.
func TestRescanOfUnsubmittedTagUpdatesSameRowNoDuplicateRow(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const scannedTag = "unsubmitted-rescan-rfid"
	first, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          7.0,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:unsubmitted-first",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("record first observation: %v", err)
	}

	second, err := repo.RecordAnimalObservation(ctx, domain.RecordAnimalObservation{
		TenantID:          repoTenant,
		CampaignID:        repoCampaign,
		CampaignShedID:    repoAnimalScope,
		ScannedIdentifier: scannedTag,
		WeightKg:          7.5,
		ProofArtifactID:   repoExpectedShedProof,
		IdempotencyKey:    "animal:unsubmitted-second",
		RecordedBy:        repoOperator,
	})
	if err != nil {
		t.Fatalf("rescan unsubmitted tag: %v", err)
	}
	if second.ObservationID != first.ObservationID {
		t.Fatalf("rescan observation id=%s, want same row %s", second.ObservationID, first.ObservationID)
	}

	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int
FROM weighing_observations
WHERE tenant_id=$1::uuid AND campaign_id=$2::uuid AND campaign_shed_id=$3::uuid
  AND lower(btrim(scanned_identifier))=lower(btrim($4))`,
		repoTenant, repoCampaign, repoAnimalScope, scannedTag).Scan(&count); err != nil {
		t.Fatalf("read observations for tag: %v", err)
	}
	if count != 1 {
		t.Fatalf("observation rows for rescanned unsubmitted tag=%d, want exactly 1", count)
	}
}
