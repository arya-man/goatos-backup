package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// The completion writes are gated on the SHEET (found in the 2026-09-16 sweep: a distribution for
// a feed day two days ahead and a packing for a day with no sheet were both accepted -- the phone
// never offers either, so nothing was written this way in the field, but a wrong device clock or
// a crafted request could invent work nobody was directed to do).
//
//  1. no issued sheet for (park, feed day, workflow) -> ErrSheetNotIssued, nothing written;
//  2. a distribution/wastage feed day after today's business date -> ErrFeedDayNotReached;
//  3. packing is exempt from (2): a bag is packed the day BEFORE its feed day;
//  4. a service with no issue store wired (the store-only tests) still judges the seeded card.
func TestCompletionWritesAreGatedOnTheIssuedSheet(t *testing.T) {
	ctx := context.Background()
	loc := biztime.DefaultLocation()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, loc)
	issues := newFakeIssueStore()
	// Sheets exist for today (16th) and tomorrow (17th), normal workflow.
	for _, day := range []string{"2026-09-16", "2026-09-17"} {
		if _, err := issues.PersistIssue(ctx, ports.PersistIssueCommand{
			TenantID: testTenant, ParkID: testPark, FeedDay: day, Workflow: domain.WorkflowNormal,
			IssuedAt: now, Fingerprint: "fp-" + day,
		}); err != nil {
			t.Fatalf("seed sheet %s: %v", day, err)
		}
	}
	dist := &fakeDistributionStore{}
	pack := &fakePackingStore{}
	service := NewService(nil, nil).
		WithIssueStore(issues).
		WithDistributionStore(dist).
		WithDistributionVerificationEnqueuer(&fakeDistributionEnqueuer{}).
		WithPackingStore(pack).
		WithPackingVerificationEnqueuer(&nullPackingEnqueuer{}).
		WithProofValidator(&fakeProofValidator{}).
		WithNowFunc(func() time.Time { return now })

	distFor := func(day string) CompleteDistributionInput {
		in := validCompleteDistributionInput()
		d, _ := time.ParseInLocation("2006-01-02", day, loc)
		in.TargetDate = d
		return in
	}
	// (1) no sheet at all for the 20th.
	if _, err := service.CompleteDistribution(ctx, distFor("2026-09-20")); !errors.Is(err, ports.ErrSheetNotIssued) {
		t.Fatalf("distribution with no sheet: err=%v, want ErrSheetNotIssued", err)
	}
	// (2) tomorrow's sheet exists, but tomorrow has not started.
	if _, err := service.CompleteDistribution(ctx, distFor("2026-09-17")); !errors.Is(err, ports.ErrFeedDayNotReached) {
		t.Fatalf("distribution for tomorrow: err=%v, want ErrFeedDayNotReached", err)
	}
	if dist.completeCalls != 0 {
		t.Fatalf("distribution store calls=%d, want 0 -- a refused day writes nothing", dist.completeCalls)
	}
	// Today's sheet: accepted.
	if _, err := service.CompleteDistribution(ctx, distFor("2026-09-16")); err != nil {
		t.Fatalf("distribution for today: %v", err)
	}
	// (3) packing for tomorrow's sheet is the normal case and stays accepted.
	pin := CompletePackingInput{
		TenantID: testTenant, ParkID: testPark, ShedID: shedA, PartitionLabel: "1", SessionNo: 1,
		Workflow: domain.WorkflowNormal, PackingProofRef: "proof-packing-1",
		CompletedBy: "00000000-0000-4000-8000-000000000111", IdempotencyKey: "feed-packing-complete-tomorrow",
	}
	pin.TargetDate, _ = time.ParseInLocation("2006-01-02", "2026-09-17", loc)
	if _, err := service.CompletePacking(ctx, pin); err != nil {
		t.Fatalf("packing for tomorrow's sheet: %v", err)
	}
	pin.TargetDate, _ = time.ParseInLocation("2006-01-02", "2026-09-20", loc)
	pin.IdempotencyKey = "feed-packing-complete-no-sheet"
	if _, err := service.CompletePacking(ctx, pin); !errors.Is(err, ports.ErrSheetNotIssued) {
		t.Fatalf("packing with no sheet: err=%v, want ErrSheetNotIssued", err)
	}
	// (4) unwired issue store: the store-only shape keeps working.
	bare := NewService(nil, nil).WithDistributionStore(&fakeDistributionStore{}).WithDistributionVerificationEnqueuer(&fakeDistributionEnqueuer{}).WithProofValidator(&fakeProofValidator{}).
		WithNowFunc(func() time.Time { return now })
	if _, err := bare.CompleteDistribution(ctx, distFor("2026-09-16")); err != nil {
		t.Fatalf("unwired issue store: %v", err)
	}
}
