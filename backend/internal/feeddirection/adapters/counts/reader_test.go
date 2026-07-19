package counts

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

const (
	testTenant = "00000000-0000-4000-8000-000000000001"
	testPark   = "00000000-0000-4000-8000-000000003001"
)

// fakeProjection serves a fixed grain set through the counts service's paging contract, including
// its clamping behaviour: a Limit above the service maximum is clamped rather than honoured, which
// is precisely how the original truncation bug hid.
type fakeProjection struct {
	total int
	// stall makes every page return zero rows while still reporting a non-zero window total -- the
	// shape that would make a naive drain loop spin or return a silent partial.
	stall bool
	// frozen makes every page return the same first rows without advancing, simulating a reader
	// whose offset is ignored.
	frozen bool

	calls int
}

func (f *fakeProjection) ProjectedShedCountsForFeed(
	_ context.Context,
	req countsdomain.FeedProjectedCountQuery,
) (countsdomain.FeedProjectedCounts, error) {
	f.calls++

	out := countsdomain.FeedProjectedCounts{
		Items:     []countsdomain.FeedProjectedCountRow{},
		TotalRows: int64(f.total),
	}
	if f.stall {
		return out, nil
	}

	offset := int(req.Offset)
	if f.frozen {
		offset = 0
	}
	end := offset + int(req.Limit)
	if end > f.total {
		end = f.total
	}
	for i := offset; i < end; i++ {
		shedID := fmt.Sprintf("shed-%04d", i)
		out.Items = append(out.Items, countsdomain.FeedProjectedCountRow{
			ShedID:             &shedID,
			ManagementStage:    "Non-Pregnant",
			Breed:              "Beetal",
			ProjectedHeadCount: 1,
		})
	}
	return out, nil
}

func request(shedIDs []string) ports.ProjectedGrainsRequest {
	return ports.ProjectedGrainsRequest{
		TenantID:   testTenant,
		ParkID:     testPark,
		TargetDate: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		ShedIDs:    shedIDs,
	}
}

func shedIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("shed-%04d", i))
	}
	return out
}

// THE REGRESSION TEST FOR THE SILENT-TRUNCATION DEFECT.
//
// The reader used to issue ONE capped read and return whatever came back. A park whose grain count
// exceeded that cap silently lost sheds -- from the sheet AND from the whole-scope total -- with no
// error anywhere, which reads as "those sheds need no feed" rather than as a failure. The drain
// must now return the COMPLETE set.
func TestGrainReadDrainsBeyondOnePageRatherThanTruncating(t *testing.T) {
	t.Parallel()

	// Deliberately more grains than one underlying projection page holds.
	total := int(projectionPageSize) + 37
	projection := &fakeProjection{total: total}
	reader := NewReader(projection)

	grains, err := reader.ProjectedGrainsForSheds(context.Background(), request(shedIDs(total)))
	if err != nil {
		t.Fatalf("ProjectedGrainsForSheds: %v", err)
	}
	if len(grains) != total {
		t.Fatalf("returned %d sheds, want %d -- the read silently truncated at the page cap", len(grains), total)
	}
	if projection.calls < 2 {
		t.Fatalf("issued %d reads for %d grains; the drain did not page", projection.calls, total)
	}
	// Still bounded: one read per projection page, NOT one per shed. A per-shed read would be the
	// N+1 fan-out the scale rules ban.
	if projection.calls > total/int(projectionPageSize)+2 {
		t.Fatalf("issued %d reads for %d grains; the drain is fanning out per shed", projection.calls, total)
	}
}

// A single page is still a single read. The drain must not cost an extra round trip on the shape
// every live park actually has.
func TestGrainReadIssuesOneReadWhenTheScopeFitsInOnePage(t *testing.T) {
	t.Parallel()

	projection := &fakeProjection{total: 78}
	reader := NewReader(projection)

	if _, err := reader.ProjectedGrainsForSheds(context.Background(), request(shedIDs(78))); err != nil {
		t.Fatalf("ProjectedGrainsForSheds: %v", err)
	}
	if projection.calls != 1 {
		t.Fatalf("issued %d reads for a single-page scope, want 1", projection.calls)
	}
}

// A projection that claims more rows than it will hand over must ERROR. Returning what was
// collected so far would be exactly the silent partial this function exists to prevent.
func TestGrainReadFailsClosedWhenTheProjectionStalls(t *testing.T) {
	t.Parallel()

	reader := NewReader(&fakeProjection{total: 500, stall: true})

	_, err := reader.ProjectedGrainsForSheds(context.Background(), request(shedIDs(500)))
	if !errors.Is(err, ports.ErrScopeTooLarge) {
		t.Fatalf("err = %v, want ErrScopeTooLarge -- a stalled drain must never return a quiet partial", err)
	}
}

// A reader that ignores the offset would loop forever. It must terminate with an error instead of
// spinning or returning a partial.
func TestGrainReadFailsClosedWhenThePageNeverAdvances(t *testing.T) {
	t.Parallel()

	projection := &fakeProjection{total: int(projectionPageSize) * (maxProjectionPages + 5), frozen: true}
	reader := NewReader(projection)

	_, err := reader.ProjectedGrainsForSheds(context.Background(), request(shedIDs(10)))
	if !errors.Is(err, ports.ErrScopeTooLarge) {
		t.Fatalf("err = %v, want ErrScopeTooLarge -- a non-advancing drain must terminate", err)
	}
	if projection.calls > maxProjectionPages {
		t.Fatalf("issued %d reads, want at most %d -- the loop is not bounded", projection.calls, maxProjectionPages)
	}
}

// An empty shed scope short-circuits: no scope, no read.
func TestGrainReadIssuesNoReadForAnEmptyScope(t *testing.T) {
	t.Parallel()

	projection := &fakeProjection{total: 10}
	reader := NewReader(projection)

	grains, err := reader.ProjectedGrainsForSheds(context.Background(), request(nil))
	if err != nil {
		t.Fatalf("ProjectedGrainsForSheds: %v", err)
	}
	if len(grains) != 0 || projection.calls != 0 {
		t.Fatalf("grains = %d, calls = %d, want 0 and 0", len(grains), projection.calls)
	}
}

// CR-04 regression: every page request must ask for the STABLE (identity-only) order, never the
// default head-count-DESC display order. The reader takes a consistent snapshot across potentially
// several OFFSET pages; sorting by a value a concurrent movement can change (current_head_count +
// pending_delta) would let a grain cross a page boundary between two reads of the same drain,
// producing a duplicate or an omission while the drain still reports success.
func TestGrainReadRequestsStableOrderOnEveryPage(t *testing.T) {
	t.Parallel()

	total := int(projectionPageSize)*2 + 5
	projection := &orderCapturingProjection{fakeProjection: fakeProjection{total: total}}
	reader := NewReader(projection)

	if _, err := reader.ProjectedGrainsForSheds(context.Background(), request(shedIDs(total))); err != nil {
		t.Fatalf("ProjectedGrainsForSheds: %v", err)
	}
	if len(projection.stableFlags) < 2 {
		t.Fatalf("expected a multi-page drain, only saw %d page(s)", len(projection.stableFlags))
	}
	for i, stable := range projection.stableFlags {
		if !stable {
			t.Fatalf("page %d requested StableOrder=false, want true on every page of a multi-page drain", i)
		}
	}
}

// orderCapturingProjection wraps fakeProjection to record whether each call asked for the stable
// order.
type orderCapturingProjection struct {
	fakeProjection
	stableFlags []bool
}

func (f *orderCapturingProjection) ProjectedShedCountsForFeed(
	ctx context.Context,
	req countsdomain.FeedProjectedCountQuery,
) (countsdomain.FeedProjectedCounts, error) {
	f.stableFlags = append(f.stableFlags, req.StableOrder)
	return f.fakeProjection.ProjectedShedCountsForFeed(ctx, req)
}
