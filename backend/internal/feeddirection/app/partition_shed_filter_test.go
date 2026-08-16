package app

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// ---------------------------------------------------------------------------
// PreviewQuery.PartitionLabel / PackingQuery.ShedID+PartitionLabel
//
// Both params were parsed at the HTTP boundary but never threaded through the read path -- a
// live-status poll narrowed to one shed+partition got back every pen's rows and could land its
// target row past page one. These tests prove the narrowing on both the DRAFT (live-generate) and
// FROZEN (served-issue) paths, the same two paths the shed and session filters are already proved
// on above.
// ---------------------------------------------------------------------------

// newPartitionedTestService gives shedA two operational partitions ("1" and "2") and leaves shedB
// undivided, so a partition_label filter has something real to narrow: same shed, two distinct
// grains, two distinct rows.
func newPartitionedTestService() (*Service, *fakeConfigRepo, *fakeCountsReader) {
	config := &fakeConfigRepo{
		snapshot: testSnapshot(),
		sheds: []ports.Shed{
			{ShedID: shedA, Label: "Shed A"},
			{ShedID: shedB, Label: "Shed B"},
		},
	}
	counts := &fakeCountsReader{grains: map[string][]domain.ShedGrain{
		shedA: {
			{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 10, PartitionLabel: "1"},
			{ManagementStage: "Non-Pregnant", Breed: "Beetal", HeadCount: 8, PartitionLabel: "2"},
		},
		shedB: {
			{ManagementStage: "Non-Pregnant", Breed: "Sirohi", HeadCount: 4},
		},
	}}
	return NewService(config, counts).WithNowFunc(func() time.Time { return targetDate() }), config, counts
}

// newPartitionedLifecycleService is the frozen-path twin of newLifecycleService, built on the
// partitioned fixture above so the served (issued) path can be exercised with a real shed+partition
// to narrow to, not just the draft/generate path.
func newPartitionedLifecycleService(now time.Time) (*Service, *fakeIssueStore) {
	svc, _, _ := newPartitionedTestService()
	store := newFakeIssueStore()
	sched := &fakeScheduleReader{clocks: []domain.WorkflowClock{normalClock()}, parks: []string{testPark}}
	svc = svc.WithIssueStore(store).WithScheduleReader(sched).WithClock(func() time.Time { return now })
	return svc, store
}

// TestDraftPreviewFiltersByPartitionLabel proves PreviewQuery.PartitionLabel narrows the live-
// generated (Draft) sheet to the exact shed+partition grain, mirroring TestShedFilterNarrowsToOneShed.
func TestDraftPreviewFiltersByPartitionLabel(t *testing.T) {
	t.Parallel()
	service, _, _ := newPartitionedTestService()

	all, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 50,
	})
	if err != nil {
		t.Fatalf("Preview(unfiltered): %v", err)
	}
	// shedA has 2 partitions x 2 sessions = 4 rows, shedB (undivided) has 1 x 2 sessions = 2 rows.
	if len(all.Items) != 6 {
		t.Fatalf("unfiltered rows = %d, want 6 (shedA's 2 partitions + shedB's 1)", len(all.Items))
	}

	page, err := service.Preview(context.Background(), domain.PreviewQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
		ShedID: shedA, PartitionLabel: "1", Limit: 50,
	})
	if err != nil {
		t.Fatalf("Preview(partition_label=1): %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("partition-filtered rows = %d, want 2 (shedA partition 1's 2 sessions)", len(page.Items))
	}
	for _, row := range page.Items {
		if row.ShedID != shedA || row.PartitionLabel != "1" {
			t.Fatalf("row leaked past the partition filter: shed=%q partition=%q", row.ShedID, row.PartitionLabel)
		}
	}
}

// TestFrozenPreviewFiltersByPartitionLabel proves the same narrowing on the FROZEN (served-issue)
// path -- the one live-status polling actually reads -- mirroring
// TestFrozenPackingWorklistFiltersBySession's session-filter proof for the shed/session pair.
func TestFrozenPreviewFiltersByPartitionLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := newPartitionedLifecycleService(istInstant(2026, 7, 29, 8))

	q := domain.PreviewQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Limit: 50}
	all, err := svc.Preview(ctx, q)
	if err != nil {
		t.Fatalf("Preview(unfiltered): %v", err)
	}
	if len(store.headers) == 0 {
		t.Fatal("nothing was frozen; this test would be exercising the draft path it exists to bypass")
	}
	if len(all.Items) != 6 {
		t.Fatalf("unfiltered frozen rows = %d, want 6", len(all.Items))
	}

	narrowed := q
	narrowed.ShedID = shedA
	narrowed.PartitionLabel = "2"
	page, err := svc.Preview(ctx, narrowed)
	if err != nil {
		t.Fatalf("Preview(partition_label=2): %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("frozen partition-filtered rows = %d, want 2 -- the filter was not applied to served rows", len(page.Items))
	}
	for _, row := range page.Items {
		if row.ShedID != shedA || row.PartitionLabel != "2" {
			t.Fatalf("frozen row leaked past the partition filter: shed=%q partition=%q", row.ShedID, row.PartitionLabel)
		}
	}
	if page.Summary.RowCount != 2 {
		t.Fatalf("frozen partition-filtered summary row_count = %d, want 2", page.Summary.RowCount)
	}
}

// TestDraftPackingWorklistFiltersByShedAndPartition proves PackingQuery.ShedID and
// PackingQuery.PartitionLabel narrow the live-generated packing worklist, and that the stale claim
// ("the packing worklist has no shed filter") no longer holds.
func TestDraftPackingWorklistFiltersByShedAndPartition(t *testing.T) {
	t.Parallel()
	service, _, _ := newPartitionedTestService()

	all, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(), Limit: 50,
	})
	if err != nil {
		t.Fatalf("PackingWorklist(unfiltered): %v", err)
	}
	if len(all.Items) != 6 {
		t.Fatalf("unfiltered worklist = %d lines, want 6", len(all.Items))
	}

	page, err := service.PackingWorklist(context.Background(), domain.PackingQuery{Draft: true,
		TenantID: testTenant, ParkID: testPark, TargetDate: targetDate(),
		ShedID: shedA, PartitionLabel: "1", Limit: 50,
	})
	if err != nil {
		t.Fatalf("PackingWorklist(shed_id+partition_label): %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("shed+partition-filtered worklist = %d lines, want 2", len(page.Items))
	}
	for _, row := range page.Items {
		if row.ShedID != shedA || row.PartitionLabel != "1" {
			t.Fatalf("packing row leaked past the shed/partition filter: shed=%q partition=%q", row.ShedID, row.PartitionLabel)
		}
	}
	if page.Summary.LineCount != 2 {
		t.Fatalf("shed+partition-filtered summary line_count = %d, want 2", page.Summary.LineCount)
	}
}

// TestFrozenPackingWorklistFiltersByShedAndPartition is the FROZEN twin: it proves ShedID and
// PartitionLabel narrow the served packing worklist too, not just the draft path, and that an
// unfiltered request is unchanged (still every line).
func TestFrozenPackingWorklistFiltersByShedAndPartition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, store := newPartitionedLifecycleService(istInstant(2026, 7, 29, 8))

	q := domain.PackingQuery{TenantID: testTenant, ParkID: testPark, TargetDate: feedDayTarget(), Limit: 50}
	all, err := svc.PackingWorklist(ctx, q)
	if err != nil {
		t.Fatalf("PackingWorklist(unfiltered): %v", err)
	}
	if len(store.headers) == 0 {
		t.Fatal("nothing was frozen; this test would be exercising the draft path it exists to bypass")
	}
	if len(all.Items) != 6 {
		t.Fatalf("unfiltered frozen worklist = %d lines, want 6", len(all.Items))
	}

	narrowed := q
	narrowed.ShedID = shedA
	narrowed.PartitionLabel = "1"
	page, err := svc.PackingWorklist(ctx, narrowed)
	if err != nil {
		t.Fatalf("PackingWorklist(shed_id+partition_label): %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("frozen shed+partition-filtered worklist = %d lines, want 2 -- the filter was not applied to served rows", len(page.Items))
	}
	for _, row := range page.Items {
		if row.ShedID != shedA || row.PartitionLabel != "1" {
			t.Fatalf("frozen packing row leaked past the shed/partition filter: shed=%q partition=%q", row.ShedID, row.PartitionLabel)
		}
	}
	if page.Summary.LineCount != 2 {
		t.Fatalf("frozen shed+partition-filtered summary line_count = %d, want 2", page.Summary.LineCount)
	}
	if page.Summary.LineCount >= all.Summary.LineCount {
		t.Fatalf("filtered line_count (%d) is not smaller than the day's (%d); the filter did not apply",
			page.Summary.LineCount, all.Summary.LineCount)
	}
}
