package domain

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Benchmark fills the derived comparison fields on a set of pens: the feed
// conversion ratio, the peer group each pen belongs to, that group's median gain,
// and the pen's distance from it. It also returns the response-level honesty
// counters.
//
// This is deliberately PURE and separate from SQL. The three rules below are
// business rules with real consequences, and each one is directly unit-testable
// here without a database:
//
//  1. LIKE IS COMPARED ONLY WITH LIKE. The peer key is breed + stage + gain BASIS.
//     Dropping basis from the key would rank a pen whose kids were each weighed
//     against a pen weighed as one lump — two different measurements, because a
//     shed average also moves when animals join or leave.
//  2. A PARTIAL RATION PRODUCES NO RATIO. Feed config encodes "not configured" as
//     an ABSENT rate row, so a pen missing one item has a planned total that is an
//     understatement. A conversion computed from it is not approximately right, it
//     is wrong, and it would rank that pen as unusually efficient.
//  3. A PEER GROUP OF ONE IS NOT A BENCHMARK. Its median is the pen's own number,
//     which renders as exactly 0% off the pace and reads as reassurance.
//
// The input slice is mutated in place; pens are otherwise left untouched.
func Benchmark(pens []Pen) (pensWithoutCohort, pensWithoutRation, pensWithoutGain, comparable int) {
	// Group first, so every pen sees the same membership regardless of its own
	// position in the slice.
	groups := map[string][]float64{}
	for i := range pens {
		pens[i].PeerGroupKey = peerGroupKey(pens[i])
		if pens[i].PeerGroupKey == "" || pens[i].ADGGPerDay == nil {
			continue
		}
		groups[pens[i].PeerGroupKey] = append(groups[pens[i].PeerGroupKey], *pens[i].ADGGPerDay)
	}

	medians := make(map[string]float64, len(groups))
	for key, values := range groups {
		if len(values) < MinPeerPens {
			continue
		}
		medians[key] = median(values)
	}

	for i := range pens {
		pen := &pens[i]

		pen.FeedPerKgGainKg = feedPerKgGain(*pen)

		if pen.Breed == nil || pen.Stage == nil {
			pensWithoutCohort++
		}
		if pen.FeedPlanStatus != FeedPlanResolved {
			pensWithoutRation++
		}
		if pen.ADGGPerDay == nil {
			pensWithoutGain++
		}

		pen.PeerPenCount = len(groups[pen.PeerGroupKey])
		peerMedian, ok := medians[pen.PeerGroupKey]
		if !ok || pen.ADGGPerDay == nil {
			// The key stays on the row even without a median: it tells the reader WHY
			// there is no comparison ("nothing else in this park is a Sirohi grower
			// weighed the same way"), which a blank cell does not.
			pen.PeerMedianADGGPerDay = nil
			pen.ADGVsPeerPct = nil
			continue
		}
		comparable++
		m := peerMedian
		pen.PeerMedianADGGPerDay = &m
		// A zero median has no percentage distance from it. Reporting one would be a
		// division by zero rendered as an arrow.
		if peerMedian == 0 {
			continue
		}
		// math.Abs on the denominator so that a group whose median gain is NEGATIVE
		// still reports "worse than peers" as a negative percentage. Without it the
		// sign flips and the worst pen in a shrinking cohort reads as the best.
		pct := (*pen.ADGGPerDay - peerMedian) / math.Abs(peerMedian) * 100
		pen.ADGVsPeerPct = &pct
	}
	return pensWithoutCohort, pensWithoutRation, pensWithoutGain, comparable
}

// peerGroupKey returns "" when the pen cannot be compared with anything: an
// unknown breed or stage, or no gain basis at all. An empty key never collects
// members, so unresolved pens are never silently pooled into one giant "unknown"
// group and ranked against each other.
func peerGroupKey(pen Pen) string {
	if pen.Breed == nil || pen.Stage == nil || pen.ADGBasis == "" {
		return ""
	}
	breed := strings.TrimSpace(*pen.Breed)
	stage := strings.TrimSpace(*pen.Stage)
	if breed == "" || stage == "" {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s", breed, stage, pen.ADGBasis)
}

// feedPerKgGain is kg of feed per kg of gain. See rule 2 above for why a partial
// ration is refused rather than approximated.
func feedPerKgGain(pen Pen) *float64 {
	if pen.FeedPlanStatus != FeedPlanResolved {
		return nil
	}
	if pen.PlannedFeedGPerHeadDay == nil || pen.ADGGPerDay == nil {
		return nil
	}
	// A pen that is flat or losing weight has no conversion ratio. Allowing a
	// negative one would sort it to the top of a "most efficient" ranking, and a
	// division by a near-zero gain produces a headline number in the thousands.
	if *pen.ADGGPerDay <= 0 {
		return nil
	}
	ratio := *pen.PlannedFeedGPerHeadDay / *pen.ADGGPerDay
	return &ratio
}

// median over a copied slice: the caller's group slices are shared across pens in
// the same group, so sorting in place would be a data race waiting to happen and
// would reorder another pen's view of its own group.
func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}
