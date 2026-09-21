package domain

import "math"

// Sold-weight band keys. The edges are the maintainer's own (2026-09-08: "40+, 35-40, 20-35,
// 20 below"): a weight goes in the LOWEST band whose upper edge is above it, so exactly 20 kg is
// 20-35, exactly 35 is 35-40 and exactly 40 is 40+. They are declared once, here, because the
// same four words are a database CHECK (migration 000381), a wire value and a copy key.
const (
	SoldBandUnder20     = "under_20"
	SoldBandFrom20To35  = "from_20_to_35"
	SoldBandFrom35To40  = "from_35_to_40"
	SoldBandAtOrAbove40 = "at_or_above_40"
)

// Where one animal's weight came from. The bands report all three separately because they are
// not the same kind of fact and a reader is owed the difference: a measured animal was put on a
// scale, a load-average animal is one of a load the desk weighed together, and an estimated one
// was never weighed at all.
const (
	SoldWeightMeasured    = "measured"
	SoldWeightLoadAverage = "load_average"
	SoldWeightEstimated   = "estimated"
)

// soldBandOrder is the render order, heaviest first, and the order the wire carries. All four are
// always present, zeros included: a band that vanishes when it is empty reads as a band that does
// not exist.
var soldBandOrder = []string{SoldBandAtOrAbove40, SoldBandFrom35To40, SoldBandFrom20To35, SoldBandUnder20}

// SoldWeightBandFor files one weight into its band. The ONE definition of the edges.
func SoldWeightBandFor(kg float64) string {
	switch {
	case kg < 20:
		return SoldBandUnder20
	case kg < 35:
		return SoldBandFrom20To35
	case kg < 40:
		return SoldBandFrom35To40
	default:
		return SoldBandAtOrAbove40
	}
}

// IsSoldWeightBand reports whether a stored band string is one this code knows. An unknown band
// is dropped rather than rendered, so a future database value cannot reach a screen as a fifth
// tile nothing has copy for.
func IsSoldWeightBand(band string) bool {
	for _, b := range soldBandOrder {
		if b == band {
			return true
		}
	}
	return false
}

// SoldWeightBand is one band's count, split by where the weight came from. Measured +
// LoadAverage + Estimated == Total by construction.
type SoldWeightBand struct {
	Band        string
	Total       int
	Measured    int
	LoadAverage int
	Estimated   int
}

// SoldWeightBands counts every animal sold on a closed deal, by weight.
//
// The four bands are disjoint, and Unweighed is the honest remainder: an animal on a sale whose
// weight was never recorded in any form. Bands plus Unweighed always equal Total, and Total is
// the same animal count the page's headline reports, because both range over the live lines of
// the same closed deals.
type SoldWeightBands struct {
	Total       int
	Measured    int
	LoadAverage int
	Estimated   int
	Unweighed   int
	Bands       []SoldWeightBand
}

func (b *SoldWeightBands) add(band, source string, n int) {
	if n <= 0 || !IsSoldWeightBand(band) {
		return
	}
	for i := range b.Bands {
		if b.Bands[i].Band != band {
			continue
		}
		b.Bands[i].Total += n
		switch source {
		case SoldWeightMeasured:
			b.Bands[i].Measured += n
			b.Measured += n
		case SoldWeightLoadAverage:
			b.Bands[i].LoadAverage += n
			b.LoadAverage += n
		case SoldWeightEstimated:
			b.Bands[i].Estimated += n
			b.Estimated += n
		}
		b.Total += n
		return
	}
}

// BuildSoldWeightBands bands every animal on the closed deals, best evidence first.
//
// The three sources, in the order each animal is claimed by:
//
//  1. MEASURED -- the animal was tagged to its sale and weighed then. One band per animal, its
//     own weight. `measured` is keyed by deal id because a tag names the sale, not the line.
//  2. LOAD AVERAGE -- the line's own recorded weight, spread over the animals of that line that
//     were not individually weighed. The weights already claimed in step 1 are SUBTRACTED first,
//     because the recorded total covers the whole line including them; if that subtraction leaves
//     nothing (a load total that is smaller than the animals already weighed out of it, which
//     means one of the two figures is wrong), the plain line average is used rather than a
//     negative one.
//  3. ESTIMATED -- migration 000381's assumption for a line whose recorded weight was missing or
//     was not a weight at all. Either kilograms for the whole line, or a band with no kilogram
//     where no kilogram is honestly knowable.
//
// Anything left is UNWEIGHED and is counted, never hidden: a sold animal nobody can place is a
// gap in the evidence, not an animal that was not sold.
//
// projection-review: membership=the live lines of closed deals, at LINE grain (sales_deal_lines,
// one row per product/breed slice of a sale), plus the tagged sale allocations of those same
// deals; group_key=(deal_id) for the allocations and the line's own position within its deal for
// the animals, and every animal of a line is claimed exactly once because the claims are taken in
// order out of one remaining count that only decreases; join_cardinality=no join -- allocations
// are handed in as a map keyed by deal id and attached in Go, so a deal with three lines cannot
// fan its allocations out; pagination=none, whole-filter aggregate over the same closed-deal read
// the summary uses, which is what makes Total equal the headline animals sold; scope=the caller's
// tenant and optional farm, inherited from that read.
//
// Only status='Deal Closed' deals may be passed in -- the caller owns that predicate, the same
// way BuildDealAggregates requires it, so the bands and the headline cannot range over different
// deals.
func BuildSoldWeightBands(closed []Deal, measured map[string][]float64) SoldWeightBands {
	out := SoldWeightBands{Bands: make([]SoldWeightBand, 0, len(soldBandOrder))}
	for _, band := range soldBandOrder {
		out.Bands = append(out.Bands, SoldWeightBand{Band: band})
	}

	for _, d := range closed {
		weights := measured[d.DealID]
		next := 0
		for _, l := range d.lineView() {
			if !IsLiveProduct(l.ProductType) {
				continue
			}
			animals := int(math.Round(l.Animals()))
			if animals <= 0 {
				continue
			}

			// 1. The animals of this line that were weighed one by one.
			taken := animals
			if remaining := len(weights) - next; remaining < taken {
				taken = remaining
			}
			claimedKg := 0.0
			for i := 0; i < taken; i++ {
				kg := weights[next+i]
				claimedKg += kg
				out.add(SoldWeightBandFor(kg), SoldWeightMeasured, 1)
			}
			next += taken
			rest := animals - taken
			if rest <= 0 {
				continue
			}

			// 2. The rest of the line, at the weight the desk recorded for the load.
			if recorded := l.WeightKg(); recorded > 0 {
				avg := (recorded - claimedKg) / float64(rest)
				if avg <= 0 {
					avg = recorded / float64(animals)
				}
				out.add(SoldWeightBandFor(avg), SoldWeightLoadAverage, rest)
				continue
			}

			// 3. The recorded assumption, in kilograms or as a band.
			if l.EstimatedWeightKg != nil && *l.EstimatedWeightKg > 0 {
				out.add(SoldWeightBandFor(*l.EstimatedWeightKg/float64(animals)), SoldWeightEstimated, rest)
				continue
			}
			if IsSoldWeightBand(l.EstimatedWeightBand) {
				out.add(l.EstimatedWeightBand, SoldWeightEstimated, rest)
				continue
			}

			out.Unweighed += rest
			out.Total += rest
		}

		// A deal carrying more tagged animals than its recorded count still weighed them, so they
		// are banded rather than dropped. The ledger's count and its tags disagreeing is a data
		// question; losing an animal that was demonstrably put on a scale is a reporting lie.
		for ; next < len(weights); next++ {
			out.add(SoldWeightBandFor(weights[next]), SoldWeightMeasured, 1)
		}
	}
	return out
}
