package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestDriveSummaryRemainingCountAgreesWithProgressCrossSurfaceParityStatusBucketsParkScope pins
// drive_summary.remaining_count to the SAME "what is left" answer that progress_pct gives, on the
// SAME payload.
//
// Live defect (two parks, 20 animals each, every animal vaccinated and submitted, none verified):
//
//	CBE-QA  progress_pct 100  progress_completed 20  progress_total 20  remaining_count 20
//	CPT-QA  progress_pct 100  progress_completed 20  progress_total 20  remaining_count 20
//
// One object, two contradictory answers to "how much is left". Root cause: remaining_count was
// total_count - completed_count, a numerator that excludes submitted work, while progress_completed
// is FIELD WORK DONE = completed + submitted (the 2026-08-03 progress-semantics decision). Under the
// OLD "progress = verified only" rule the two agreed; after the decision they cannot. A client
// picking remaining_count for an "N left" label disagrees with the ring beside it -- the same
// cross-surface failure mode as the 200-vs-400 parity incident.
//
// remaining_count now means WORK STILL OWED BY THE OPERATOR = due + overdue + deferred, which is
// exactly total_count - completed_count - submitted_count over the five disjoint buckets. The
// outstanding video review is carried by submitted_count and the verification_pending status, never
// by inflating "remaining".
//
// CrossSurfaceParity / StatusBuckets / ParkScope / OneToMany.
func TestDriveSummaryRemainingCountAgreesWithProgressCrossSurfaceParityStatusBucketsParkScope(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 5*time.Second)

	const (
		protocolID = "88000000-0000-4000-8000-000000000101"
		versionID  = "88000000-0000-4000-8000-000000000102"
		ruleID     = "88000000-0000-4000-8000-000000000103"

		parkCBE = "88000000-0000-4000-8000-000000000111"
		parkCPT = "88000000-0000-4000-8000-000000000112"

		batchCBE = "88000000-0000-4000-8000-000000000121"
		batchCPT = "88000000-0000-4000-8000-000000000122"

		herdPerPark = 20
	)

	// Business-day grain, Asia/Kolkata. One stable same-local-day due instant for both parks.
	dueAt := stableSameLocalDayDueAt(time.Now().In(biztime.DefaultLocation()))

	// 20 obligations per park; seedAdditionalVaccinationObligation gives each obligation its own
	// goat (target_id = obligation_id), so obligation grain and animal grain are 1:1 here and the
	// progress basis is 'animals'.
	obligationsFor := func(parkIndex int) []string {
		ids := make([]string, 0, herdPerPark)
		for i := 0; i < herdPerPark; i++ {
			ids = append(ids, fmt.Sprintf("88000000-0000-4000-8000-%03d%09d", parkIndex, 200000+i))
		}
		return ids
	}
	cbeObligations := obligationsFor(1)
	cptObligations := obligationsFor(2)

	// seedVaccinationObligation creates the protocol/version/rule but leaves the obligation at
	// target_type 'tenant'; seedAdditionalVaccinationObligation re-points it at its own goat, so
	// every one of the 40 obligations is animal-targeted and the progress basis is 'animals'.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID, cbeObligations[0], dueAt)
	for _, id := range append(append([]string{}, cbeObligations...), cptObligations...) {
		seedAdditionalVaccinationObligation(t, ctx, pool, versionID, ruleID, id, dueAt)
	}
	seedProtocolRuleVaccineName(t, ctx, pool, versionID, ruleID, "PPR")
	seedVaccinationBatchForPark(t, ctx, pool, batchCBE, versionID, parkCBE, dueAt, cbeObligations...)
	seedVaccinationBatchForPark(t, ctx, pool, batchCPT, versionID, parkCPT, dueAt, cptObligations...)

	all := append(append([]string{}, cbeObligations...), cptObligations...)

	// The live shape: batches in_progress, every obligation in the sweeper's post-due 'due' state,
	// every animal scanned and submitted, NOTHING verified.
	if _, err := pool.Exec(ctx, `
UPDATE obligation_batches SET status = 'in_progress', updated_at = now()
WHERE tenant_id = $1::uuid AND batch_id = ANY($2::uuid[])`,
		testTenantID, []string{batchCBE, batchCPT}); err != nil {
		t.Fatalf("mark batches in_progress: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE obligation_instances SET status = 'due', updated_at = now()
WHERE tenant_id = $1::uuid AND obligation_id = ANY($2::uuid[])`,
		testTenantID, all); err != nil {
		t.Fatalf("mark obligations due: %v", err)
	}
	for _, id := range all {
		if _, err := pool.Exec(ctx, `
INSERT INTO vaccination_completions (
  completion_id, tenant_id, obligation_id, goat_id, administered_at, status, verified_at, idempotency_key
) VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $2::uuid, $3::timestamptz, 'recorded', NULL, $2::text || ':recorded')`,
			testTenantID, id, dueAt); err != nil {
			t.Fatalf("seed recorded completion %s: %v", id, err)
		}
	}

	resp, err := repo.ListEvents(ctx, domain.Query{
		TenantID: testTenantID, OwnerKey: domain.OwnerAll,
		DateFrom: dueAt.Add(-24 * time.Hour), DateTo: dueAt.Add(24 * time.Hour), Limit: 50,
		Scope: domain.ScopeFilter{TenantWide: true}, IncludeDriveSummary: true,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}

	byEventID := map[string]*domain.DriveSummary{}
	for i := range resp.Items {
		if resp.Items[i].EventType == domain.EventVaccinationDrive && resp.Items[i].DriveSummary != nil {
			byEventID[resp.Items[i].EventID] = resp.Items[i].DriveSummary
		}
	}

	for _, park := range []struct{ label, id string }{{"CBE", parkCBE}, {"CPT", parkCPT}} {
		eventID := parkDriveEventID(park.id, dueAt)
		s := byEventID[eventID]
		if s == nil {
			t.Fatalf("%s: missing drive_summary for event %s (have %d drive cards)", park.label, eventID, len(byEventID))
		}
		t.Logf("%s drive_summary: total=%d completed=%d submitted=%d due=%d overdue=%d deferred=%d remaining=%d progress=%d/%d (%d%%, basis=%s)",
			park.label, s.TotalCount, s.CompletedCount, s.SubmittedCount, s.DueCount, s.OverdueCount,
			s.DeferredCount, s.RemainingCount, s.ProgressCompleted, s.ProgressTotal, s.ProgressPct, s.ProgressBasis)

		// Preconditions reproducing the live state (these already hold today).
		if s.TotalCount != herdPerPark || s.SubmittedCount != herdPerPark || s.CompletedCount != 0 {
			t.Fatalf("%s: fixture did not reproduce the live state: total=%d submitted=%d completed=%d, want %d/%d/0",
				park.label, s.TotalCount, s.SubmittedCount, s.CompletedCount, herdPerPark, herdPerPark)
		}
		if s.ProgressPct != 100 {
			t.Fatalf("%s: progress_pct = %d, want 100 (field work done = completed + submitted)", park.label, s.ProgressPct)
		}

		// (a) THE DEFECT: one payload must not carry two contradictory answers to "how much is left".
		if s.RemainingCount != 0 {
			t.Errorf("%s: remaining_count = %d on a payload that also says progress_pct = %d (%d of %d done). "+
				"A client rendering \"%d left\" beside a full ring shows two different truths for the same drive. "+
				"remaining_count must exclude submitted work, exactly like the progress numerator.",
				park.label, s.RemainingCount, s.ProgressPct, s.ProgressCompleted, s.ProgressTotal, s.RemainingCount)
		}

		// (b) remaining is the open-bucket sum over the five disjoint buckets, so it can never
		// disagree with the buckets that are shipped alongside it.
		wantRemaining := s.DueCount + s.OverdueCount + s.DeferredCount
		if s.RemainingCount != wantRemaining {
			t.Errorf("%s: remaining_count = %d, want %d (due %d + overdue %d + deferred %d) -- the buckets and "+
				"remaining_count must be the same arithmetic",
				park.label, s.RemainingCount, wantRemaining, s.DueCount, s.OverdueCount, s.DeferredCount)
		}
		if s.RemainingCount != s.TotalCount-s.CompletedCount-s.SubmittedCount {
			t.Errorf("%s: remaining_count = %d, want total %d - completed %d - submitted %d = %d",
				park.label, s.RemainingCount, s.TotalCount, s.CompletedCount, s.SubmittedCount,
				s.TotalCount-s.CompletedCount-s.SubmittedCount)
		}

		// (c) The five-bucket invariant must survive the change.
		sum := s.CompletedCount + s.SubmittedCount + s.DueCount + s.OverdueCount + s.DeferredCount
		if sum != s.TotalCount {
			t.Errorf("%s: five buckets sum to %d, want total_count %d", park.label, sum, s.TotalCount)
		}

		// (d) Shed completion and the ring tell the same story as remaining_count.
		if s.ShedsCompleted != s.ShedCount {
			t.Errorf("%s: sheds_completed = %d of %d on a 100%% drive", park.label, s.ShedsCompleted, s.ShedCount)
		}
	}
}
