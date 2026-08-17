package domain

import "sort"

// BuildDealAggregates computes the closed-deal blocks of the overview -- summary, monthly series,
// price bands and buyer board -- from ONE bounded read of the ledger.
//
// It is pure Go over whole-filter rows on purpose: the ledger is an authored commercial record
// (63 sheet rows today, growing by a handful per month), so the read that feeds this is a single
// bounded tenant-scoped query, never a paginated read collapsed page-locally. Doing the rollup in
// one place also keeps the four blocks arithmetically consistent with each other -- the monthly
// revenues sum to the summary revenue by construction.
//
// projection-review: producer rows are sales_deals at ROW grain (one sheet row / one recorded
// deal; source_sales_id is a sheet reference that can repeat and is never used as a group key).
// Consumers group by month (Deal.Month), by (product_type, breed), and by buyer_name over that
// ONE table -- no joins, so no fan-out is possible. The ratio realized_price_per_kg has numerator
// (live revenue) and denominator (live weight) both ranging over the SAME key set: closed deals
// of a live product type with weight > 0 contribute to both or neither, and a zero denominator
// yields 0 rather than a division.
//
// Only status='Deal Closed' rows may be passed in; the caller owns that predicate (it is the same
// WHERE for every block, so the blocks cannot drift apart).
func BuildDealAggregates(closed []Deal) (Summary, []MonthlyRow, []PriceBand, []BuyerRow) {
	summary := Summary{}
	monthly := map[string]*MonthlyRow{}
	type bandKey struct{ productType, breed string }
	bands := map[bandKey]*PriceBand{}
	type buyerAgg struct {
		row   BuyerRow
		types map[string]struct{}
	}
	buyers := map[string]*buyerAgg{}

	// liveWeightForPrice ranges over closed live deals with weight>0 AND value>0 -- the same key
	// set the price bands use -- so the summary's realized price and the band averages agree on
	// what a priced kilogram is.
	liveWeightForPrice, liveRevenueForPrice := 0.0, 0.0

	for _, d := range closed {
		summary.Deals++
		summary.Revenue += d.SalesValue
		if d.PeriodFromCandidate(summary.PeriodFrom) {
			summary.PeriodFrom = d.SaleDate
		}
		if d.SaleDate > summary.PeriodTo {
			summary.PeriodTo = d.SaleDate
		}

		weight := 0.0
		if d.TotalWeightKg != nil {
			weight = *d.TotalWeightKg
		}
		animals := d.Animals()

		month := monthly[d.Month()]
		if month == nil {
			month = &MonthlyRow{Month: d.Month()}
			monthly[d.Month()] = month
		}

		switch d.ProductType {
		case ProductSheep:
			summary.LiveRevenue += d.SalesValue
			summary.Animals += animals
			summary.Sheep += animals
			summary.LiveWeightKg += weight
			month.SheepRevenue += d.SalesValue
			month.SheepCount += animals
		case ProductGoat:
			summary.LiveRevenue += d.SalesValue
			summary.Animals += animals
			summary.Goats += animals
			summary.LiveWeightKg += weight
			month.GoatRevenue += d.SalesValue
			month.GoatCount += animals
		case ProductManure:
			// Manure contributes weight and revenue, never animal counts.
			summary.ManureRevenue += d.SalesValue
			summary.ManureKg += weight
			month.ManureRevenue += d.SalesValue
			month.ManureKg += weight
		}

		// Price bands: live types only, and only rows where a price per kg is actually computable.
		if IsLiveProduct(d.ProductType) && weight > 0 && d.SalesValue > 0 {
			liveWeightForPrice += weight
			liveRevenueForPrice += d.SalesValue
			key := bandKey{d.ProductType, d.Breed}
			band := bands[key]
			if band == nil {
				band = &PriceBand{ProductType: d.ProductType, Breed: d.Breed}
				bands[key] = band
			}
			band.Deals++
			band.Animals += animals
			band.WeightKg += weight
			band.Revenue += d.SalesValue
			price := d.SalesValue / weight
			if band.MinPricePerKg == 0 || price < band.MinPricePerKg {
				band.MinPricePerKg = price
			}
			if price > band.MaxPricePerKg {
				band.MaxPricePerKg = price
			}
		}

		// Buyer board. Grouped by buyer_name, which is the sheet's own buyer identity -- there is
		// no buyer master to key by. Place keeps the first non-empty value recorded.
		buyer := buyers[d.BuyerName]
		if buyer == nil {
			buyer = &buyerAgg{row: BuyerRow{BuyerName: d.BuyerName}, types: map[string]struct{}{}}
			buyers[d.BuyerName] = buyer
		}
		buyer.row.Deals++
		buyer.row.Animals += animals
		buyer.row.Revenue += d.SalesValue
		buyer.types[d.ProductType] = struct{}{}
		if buyer.row.BuyerPlace == "" && d.BuyerPlace != nil {
			buyer.row.BuyerPlace = *d.BuyerPlace
		}
	}

	// realized_price_per_kg guards the zero denominator: no priced live kilograms means no
	// realized price, reported as 0 rather than NaN/Inf.
	if liveWeightForPrice > 0 {
		summary.RealizedPricePerKg = liveRevenueForPrice / liveWeightForPrice
	}

	monthlyRows := make([]MonthlyRow, 0, len(monthly))
	for _, row := range monthly {
		monthlyRows = append(monthlyRows, *row)
	}
	sort.Slice(monthlyRows, func(i, j int) bool { return monthlyRows[i].Month < monthlyRows[j].Month })

	bandRows := make([]PriceBand, 0, len(bands))
	for _, band := range bands {
		if band.WeightKg > 0 {
			band.AvgPricePerKg = band.Revenue / band.WeightKg
		}
		bandRows = append(bandRows, *band)
	}
	sort.Slice(bandRows, func(i, j int) bool {
		if bandRows[i].AvgPricePerKg != bandRows[j].AvgPricePerKg {
			return bandRows[i].AvgPricePerKg > bandRows[j].AvgPricePerKg
		}
		if bandRows[i].ProductType != bandRows[j].ProductType {
			return bandRows[i].ProductType < bandRows[j].ProductType
		}
		return bandRows[i].Breed < bandRows[j].Breed
	})

	buyerRows := make([]BuyerRow, 0, len(buyers))
	for _, buyer := range buyers {
		types := make([]string, 0, len(buyer.types))
		for t := range buyer.types {
			types = append(types, t)
		}
		sort.Strings(types)
		buyer.row.ProductTypes = types
		if summary.Revenue > 0 {
			buyer.row.SharePct = buyer.row.Revenue / summary.Revenue * 100
		}
		buyerRows = append(buyerRows, buyer.row)
	}
	sort.Slice(buyerRows, func(i, j int) bool {
		if buyerRows[i].Revenue != buyerRows[j].Revenue {
			return buyerRows[i].Revenue > buyerRows[j].Revenue
		}
		return buyerRows[i].BuyerName < buyerRows[j].BuyerName
	})
	if len(buyerRows) > MaxOverviewBuyers {
		buyerRows = buyerRows[:MaxOverviewBuyers]
	}

	return summary, monthlyRows, bandRows, buyerRows
}

// MaxOverviewBuyers bounds the buyer board. The board is a leaderboard, not the whole customer
// book; SharePct is still computed against the WHOLE-filter revenue before the cut.
const MaxOverviewBuyers = 25

// MaxPipelinePlaces bounds the top-places / top-districts lists on the pipeline panels.
const MaxPipelinePlaces = 12

// PeriodFromCandidate reports whether this deal's date should become the running period_from --
// true when nothing is set yet or this date is earlier.
func (d Deal) PeriodFromCandidate(current string) bool {
	return current == "" || d.SaleDate < current
}

// BucketWeightGap classifies one audit row's absolute video-vs-book gap into the summary. The
// three buckets are disjoint: <= 0.3 kg, (0.3, 1] kg, > 1 kg.
func (s *WeightAuditSummary) BucketWeightGap(videoKg, bookKg float64) {
	gap := videoKg - bookKg
	if gap < 0 {
		gap = -gap
	}
	s.Total++
	// The boundaries carry a float epsilon: weights are recorded to 0.1 kg, and 23 - 22.7
	// evaluates to 0.300000...07 in binary floating point. Without the epsilon a gap that IS
	// 0.3 kg on paper lands in the wrong bucket.
	const eps = 1e-9
	switch {
	case gap <= 0.3+eps:
		s.Within03Kg++
	case gap <= 1+eps:
		s.Within1Kg++
	default:
		s.Over1Kg++
	}
	if gap > s.MaxGapKg {
		s.MaxGapKg = gap
	}
}
