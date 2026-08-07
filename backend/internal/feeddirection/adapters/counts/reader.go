// Package counts adapts backend/internal/counts' live-herd feed projection to the feed-direction
// generator's ShedCountsReader port.
//
// It exists so the generator depends on its OWN narrow port rather than on the counts module's
// query and result types. Two things follow from that: the counts module keeps sole ownership of
// the census SQL (this file issues no SQL of its own), and the generator stays unit-testable
// against a fake reader with no counts service in the picture.
package counts

import (
	"context"
	"fmt"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
)

// projectionPageSize is the size of ONE underlying projection read. It equals the counts service's
// own maximum, so this adapter can never ask for more than that service will return -- asking for
// more would be silently clamped, and a silent clamp is how a truncation bug hides.
const projectionPageSize = int32(200)

// maxProjectionPages bounds the drain loop below.
//
// It is a non-termination tripwire, not a data bound: projectionPageSize * maxProjectionPages
// (5,000 grains) is far above the filtered scope of any live park (78 grains in the largest), and
// the counts service clamps its own offset at 5,000. Reaching it means either a park has outgrown
// the premise that makes this read safe, or the loop is not advancing -- both of which must fail
// loudly rather than return whatever was collected so far.
const maxProjectionPages = 25

// Projection is the counts-side dependency: the live-herd feed projection.
//
// Declared as an interface here rather than taking *countsapp.Service so this adapter can be
// exercised without constructing the whole counts service.
type Projection interface {
	ProjectedShedCountsForFeed(ctx context.Context, req countsdomain.FeedProjectedCountQuery) (countsdomain.FeedProjectedCounts, error)
}

// Reader implements ports.ShedCountsReader over the counts projection.
type Reader struct {
	projection Projection
}

func NewReader(projection Projection) *Reader { return &Reader{projection: projection} }

var _ ports.ShedCountsReader = (*Reader)(nil)

// ProjectedGrainsForSheds returns the projected grains for the filtered shed scope, keyed by shed
// id.
//
// ONE call to the projection PER UNDERLYING PAGE, with the whole shed scope passed as a SET filter.
// Not one call per shed: that is the N+1 fan-out banned by the scale rules, and it is exactly the
// shape a raw-driver check would miss because the query lives an adapter layer below this loop. The
// loop below is bounded by the projection's own page size (typically ONE iteration for a live
// park), not by the number of sheds.
//
// COMPLETE OR ERROR. The previous version issued a single capped read and returned whatever came
// back, so a park whose grain count exceeded the cap silently lost sheds -- from the sheet and from
// the total -- with no error anywhere. That is a wrong pack weight presented as a correct one, so
// the drain below either finishes the set or fails with ports.ErrScopeTooLarge.
func (r *Reader) ProjectedGrainsForSheds(ctx context.Context, req ports.ProjectedGrainsRequest) (map[string][]domain.ShedGrain, error) {
	out := map[string][]domain.ShedGrain{}
	if len(req.ShedIDs) == 0 {
		return out, nil
	}

	parkID := req.ParkID
	var collected int64
	var offset int32

	// scale-guard:ignore: BOUNDED DRAIN of one projection result set, not a per-row or per-shed fan-out. The whole shed scope goes down as a single SET filter, so this loop issues one read per 200-grain projection page (ONE iteration for every live park today: 78 grains), never one read per shed. Forward progress is guaranteed by the monotonic offset advance below, and maxProjectionPages fails the request closed rather than letting the loop spin or return a partial set.
	for page := 0; ; page++ {
		if page >= maxProjectionPages {
			return nil, fmt.Errorf(
				"%w: projected grains for park %s exceeded %d pages of %d",
				ports.ErrScopeTooLarge, req.ParkID, maxProjectionPages, projectionPageSize)
		}

		result, err := r.projection.ProjectedShedCountsForFeed(ctx, countsdomain.FeedProjectedCountQuery{
			TenantID:   req.TenantID,
			TargetDate: req.TargetDate,
			ParkID:     &parkID,
			ShedIDs:    req.ShedIDs,
			Limit:      projectionPageSize,
			Offset:     offset,
			// A stable (identity-only) sort key, not the default head-count-DESC display order:
			// this loop takes a CONSISTENT SNAPSHOT across potentially several OFFSET pages, and a
			// movement authorized/completed between two of those reads must not shift a grain
			// across the page boundary (CR-04). See FeedProjectedCountQuery.StableOrder.
			StableOrder: true,
		})
		if err != nil {
			return nil, fmt.Errorf("feeddirection: projected shed counts: %w", err)
		}

		for _, row := range result.Items {
			if row.ShedID == nil || *row.ShedID == "" {
				// A grain with no shed cannot be fed: feeding is a shed-scoped activity and there is
				// no bag to fill without one. Dropping it here is safe because the shed scope this
				// read was filtered to came from the locations catalog, so a shed-less grain was
				// never in it.
				continue
			}
			shedID := *row.ShedID
			out[shedID] = append(out[shedID], domain.ShedGrain{
				ManagementStage: row.ManagementStage,
				Breed:           row.Breed,
				// The projection has always carried this; Feed dropped it here until 2026-08-07,
				// which is why a partly-experimental shed had no representable answer.
				PartitionLabel: row.PartitionLabel,
				// PROJECTED, not current: the feed plan must cover the animals that will be standing
				// in the shed on the target date, including the ones an approved-but-unexecuted
				// movement is bringing in.
				HeadCount:      row.ProjectedHeadCount,
				OverduePending: row.OverduePending,
			})
		}

		collected += int64(len(result.Items))
		if collected >= result.TotalRows {
			return out, nil
		}
		// A page that returned nothing while the window count still claims more rows means the read
		// cannot advance. Returning `out` here would be the silent-partial this function exists to
		// prevent, so it fails instead.
		if len(result.Items) == 0 {
			return nil, fmt.Errorf(
				"%w: projected grains for park %s stalled at %d of %d rows",
				ports.ErrScopeTooLarge, req.ParkID, collected, result.TotalRows)
		}
		offset += int32(len(result.Items))
	}
}
