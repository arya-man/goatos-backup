package postgres

import (
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// mergeSameOperationalLocation collapses placement rows that are the SAME PEN, summing
// their head counts.
//
// WHY THIS IS NEEDED, and why it is not the banned name-keying. The farm's pens exist
// twice in `locations`: the canonical shed plus a legacy row literally named for the pen
// ("Castro 1"). A load tagged to the legacy row is weighed under buckets whose own
// partition_label is sometimes blank and sometimes the pen number, and BOTH compose --
// correctly, through the doubling guard -- to the same display. The chart therefore
// rendered "Castro 1 · 63" and "Castro 1 · 31" side by side: one pen, shown twice, as if
// the load sat in two places.
//
// Merging is the honest fix rather than dropping the alias row. Both rows are real
// measured buckets and both are already inside the load's blended average and its animal
// total, so suppressing one would leave the placements no longer summing to `animals` --
// breaking the exact reconciliation this list is trusted for. Summing keeps
// 63 + 31 = 94 visible against a single "Castro 1".
//
// The key is (park, composed display) and NOT the shed name: that is the whole point --
// two rows only merge when they resolve to the same OPERATIONAL LOCATION, which is the
// canonical identity the convention defines. Two genuinely different pens compose to
// different displays and never merge, and two parks that both own a "Castro" stay apart
// because the park is in the key.
func mergeSameOperationalLocation(in []domain.LoadPlacement) []domain.LoadPlacement {
	type key struct{ park, display string }
	index := make(map[key]int, len(in))
	out := make([]domain.LoadPlacement, 0, len(in))
	for _, p := range in {
		k := key{park: p.ParkName, display: p.OperationalLocationDisplay}
		if at, ok := index[k]; ok {
			out[at].Animals += p.Animals
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}
