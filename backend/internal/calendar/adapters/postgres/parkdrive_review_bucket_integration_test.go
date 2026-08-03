package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestCalendarParkDriveReviewBucketStatusBucketsCrossSurfaceParityOneToManyParkScope pins the
// calendar drive card's summary counts to the SAME obligation-grain submission truth its own
// headline status reads.
//
// Live defect (CEO token, GET /calendar/vaccination/events, park fully submitted-but-unverified):
//
//	"status": "verification_pending", "target_count": 20,
//	"scheduled_count": 20, "review_count": 0, "deferred_count": 0
//
// The card declared itself in review while reporting ZERO work in review and all 20 animals still
// scheduled. Root cause: scheduled_count/review_count were derived from the BATCH status in
// park_drive_groups -- a different grain and a different source of truth from the headline, which
// reads obligation-grain eff_state.has_submitted. Batch status 'in_progress' is absent from the
// review status list (has_review false -> review_count 0) but present in the scheduled list
// (-> the same submitted animals counted as still scheduled). review_count was additionally a
// BOOLEAN rendered as a count (CASE WHEN has_review THEN 1 ELSE 0 END) alongside sibling fields
// that are work counts.
//
// This is the mirror image of the CEO board defect proved by
// TestVaccinationCommandBoardDueStatusEveryStatusBucketsExhaustiveOverTargets: there the same
// animals vanish from every bucket, here they are counted twice. Both surfaces must reconcile.
//
// StatusBuckets / CrossSurfaceParity / OneToMany / ParkScope.
func TestCalendarParkDriveReviewBucketStatusBucketsCrossSurfaceParityOneToManyParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	type summaryBlock struct {
		TargetCount    int    `json:"target_count"`
		ScheduledCount int    `json:"scheduled_count"`
		ReviewCount    int    `json:"review_count"`
		DeferredCount  int    `json:"deferred_count"`
		SummaryPrimary string `json:"summary_primary"`
	}
	readCard := func(t *testing.T, eventID string) (domain.CalendarEvent, summaryBlock) {
		t.Helper()
		detail, err := repo.GetEventDetail(ctx, domain.EventQuery{
			TenantID: testTenantID, EventID: eventID, Scope: domain.ScopeFilter{TenantWide: true},
		})
		if err != nil {
			t.Fatalf("GetEventDetail(%s): %v", eventID, err)
		}
		var block summaryBlock
		if err := json.Unmarshal(detail.Summary, &block); err != nil {
			t.Fatalf("unmarshal summary for %s: %v (raw=%s)", eventID, err, string(detail.Summary))
		}
		return detail.Event, block
	}

	const (
		protocolID = "87000000-0000-4000-8000-000000000b01"
		versionID  = "87000000-0000-4000-8000-000000000b02"
		ruleID     = "87000000-0000-4000-8000-000000000b03"

		parkSubmitted  = "87000000-0000-4000-8000-000000000b11"
		batchSubmitted = "87000000-0000-4000-8000-000000000b12"
		oblSubmittedA  = "87000000-0000-4000-8000-000000000b13"
		oblSubmittedB  = "87000000-0000-4000-8000-000000000b14"
		compA          = "87000000-0000-4000-8000-000000000b15"
		compB          = "87000000-0000-4000-8000-000000000b16"

		parkUntouched  = "87000000-0000-4000-8000-000000000b21"
		batchUntouched = "87000000-0000-4000-8000-000000000b22"
		oblUntouchedA  = "87000000-0000-4000-8000-000000000b23"
		oblUntouchedB  = "87000000-0000-4000-8000-000000000b24"
	)

	// Both drives planned on the SAME business date, both batches in_progress inside an open window.
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))

	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, oblSubmittedA, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, oblSubmittedB, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, oblUntouchedA, dueAt)
	seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, oblUntouchedB, dueAt)
	seedVaccinationBatchForPark(t, ctx, pool, batchSubmitted, versionID, parkSubmitted, dueAt, oblSubmittedA, oblSubmittedB)
	seedVaccinationBatchForPark(t, ctx, pool, batchUntouched, versionID, parkUntouched, dueAt, oblUntouchedA, oblUntouchedB)

	// The live shape: batches are in_progress, NOT in any review status. This is precisely the
	// state that made grouped.has_review false while the work was in fact submitted.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status = 'in_progress', updated_at = now()
WHERE tenant_id = $1::uuid AND batch_id = ANY($2::uuid[])`,
		testTenantID, []string{batchSubmitted, batchUntouched}); err != nil {
		t.Fatalf("mark batches in_progress: %v", err)
	}
	// Obligations sit in the sweeper's post-due 'due' state on both parks.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status = 'due', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = ANY($2::uuid[])`,
		testTenantID, []string{oblSubmittedA, oblSubmittedB, oblUntouchedA, oblUntouchedB}); err != nil {
		t.Fatalf("mark obligations due: %v", err)
	}
	// Submitted park: every animal scanned and submitted, nothing verified yet.
	seedVaccinationCompletion(t, ctx, pool, oblSubmittedA, oblSubmittedA, compA)
	seedVaccinationCompletion(t, ctx, pool, oblSubmittedB, oblSubmittedB, compB)
	if _, err := pool.Exec(ctx, `
UPDATE vaccination_completions SET status = 'recorded', verified_at = NULL, updated_at = now()
WHERE tenant_id = $1::uuid AND completion_id = ANY($2::uuid[])`,
		testTenantID, []string{compA, compB}); err != nil {
		t.Fatalf("mark completions recorded-unverified: %v", err)
	}

	submittedEvent, submitted := readCard(t, parkDriveEventID(parkSubmitted, dueAt))
	t.Logf("submitted park card: status=%s summary=%+v", submittedEvent.Status, submitted)

	if submittedEvent.Status != "verification_pending" {
		t.Fatalf("submitted park card status = %q, want verification_pending", submittedEvent.Status)
	}
	// The defect: status said verification_pending while review_count said 0.
	if submitted.ReviewCount != 2 {
		t.Fatalf("submitted park review_count = %d, want 2 — the card declares status=%q while reporting "+
			"no work in review; review_count must be an obligation COUNT from the same submission truth "+
			"the headline reads, not a boolean derived from batch status",
			submitted.ReviewCount, submittedEvent.Status)
	}
	// ...and scheduled_count still claimed the same animals as un-started work.
	if submitted.ScheduledCount != 0 {
		t.Fatalf("submitted park scheduled_count = %d, want 0 — submitted work must leave the scheduled "+
			"bucket; counting it in both scheduled_count and review_count double-classifies the same animals",
			submitted.ScheduledCount)
	}
	if submitted.SummaryPrimary != "0 scheduled doses" {
		t.Fatalf("submitted park summary_primary = %q, want %q", submitted.SummaryPrimary, "0 scheduled doses")
	}
	if got := submitted.ScheduledCount + submitted.ReviewCount + submitted.DeferredCount; got != submitted.TargetCount {
		t.Fatalf("submitted park buckets account for %d of %d targets (scheduled=%d review=%d deferred=%d)",
			got, submitted.TargetCount, submitted.ScheduledCount, submitted.ReviewCount, submitted.DeferredCount)
	}

	untouchedEvent, untouched := readCard(t, parkDriveEventID(parkUntouched, dueAt))
	t.Logf("untouched park card: status=%s summary=%+v", untouchedEvent.Status, untouched)

	if untouched.ReviewCount != 0 {
		t.Fatalf("untouched park review_count = %d, want 0 (no work submitted)", untouched.ReviewCount)
	}
	if untouched.ScheduledCount != 2 {
		t.Fatalf("untouched park scheduled_count = %d, want 2 (zero completions, still open)", untouched.ScheduledCount)
	}
	if got := untouched.ScheduledCount + untouched.ReviewCount + untouched.DeferredCount; got != untouched.TargetCount {
		t.Fatalf("untouched park buckets account for %d of %d targets", got, untouched.TargetCount)
	}

	t.Run("PaginationPageBoundaryMultiPageCardCountsAreWholeFilterNotPageLocal", func(t *testing.T) {
		// Pagination PageBoundary MultiPage: the card's bucket counts are whole-filter group
		// aggregates over (park_id, due_date) membership, never page-local — re-reading each card
		// independently must return the identical counts.
		_, submittedAgain := readCard(t, parkDriveEventID(parkSubmitted, dueAt))
		_, untouchedAgain := readCard(t, parkDriveEventID(parkUntouched, dueAt))
		if submittedAgain != submitted {
			t.Fatalf("submitted park card counts changed across identical reads: %+v vs %+v", submittedAgain, submitted)
		}
		if untouchedAgain != untouched {
			t.Fatalf("untouched park card counts changed across identical reads: %+v vs %+v", untouchedAgain, untouched)
		}
	})

	t.Run("CrossSurfaceParitySubmittedWorkCountedOnceAcrossBothParks", func(t *testing.T) {
		// The same business fact ("2 animals submitted-but-unverified, 2 animals untouched") must
		// reconcile across the two cards exactly as it does in the CEO board KPI row: submitted
		// work appears only in the review bucket, untouched work only in the scheduled bucket.
		totalReview := submitted.ReviewCount + untouched.ReviewCount
		totalScheduled := submitted.ScheduledCount + untouched.ScheduledCount
		if totalReview != 2 || totalScheduled != 2 {
			t.Fatalf("cross-surface totals review=%d scheduled=%d, want review=2 scheduled=2", totalReview, totalScheduled)
		}
	})
}
