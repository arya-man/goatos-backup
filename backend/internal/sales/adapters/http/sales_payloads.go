package http

import (
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// Field names here, in contracts/openapi/app-api.yaml, and in the generated TypeScript client must
// move together. A rename on one side only is the "contract lie" failure mode: the client
// compiles, renders nothing, and nothing errors.

// dealPayload is the wire shape of one ledger row.
type dealPayload struct {
	DealID   string `json:"deal_id"`
	SaleDate string `json:"sale_date"`
	Farm     string `json:"farm"`

	SourceSalesID    *int `json:"source_sales_id"`
	SourcePurchaseID *int `json:"source_purchase_id"`

	BuyerName  string  `json:"buyer_name"`
	BuyerPlace *string `json:"buyer_place"`
	// The vendor register row this sale was made to, as an opaque reference. null on the imported
	// sheet history, which predates the register (migration 000193).
	BuyerVendorID *string `json:"buyer_vendor_id"`

	ProductType string `json:"product_type"`
	Breed       string `json:"breed"`

	AnimalCount   *float64 `json:"animal_count"`
	MaleCount     *float64 `json:"male_count"`
	FemaleCount   *float64 `json:"female_count"`
	TotalWeightKg *float64 `json:"total_weight_kg"`

	AdvanceAmount *float64 `json:"advance_amount"`
	SalesValue    float64  `json:"sales_value"`
	// PaymentReceived is the running total of money the buyer has handed over (seeded from the
	// sheet's advance); PaymentBalance is BACKEND-derived (value minus received, floored at zero),
	// so no surface computes its own money figure.
	PaymentReceived *float64             `json:"payment_received"`
	PaymentBalance  float64              `json:"payment_balance"`
	Payments        []dealPaymentPayload `json:"payments"`

	Status   string  `json:"status"`
	Feedback *string `json:"feedback"`
	Comments *string `json:"comments"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// dealStatusWritePayload is the deal-status edit body.
type dealStatusWritePayload struct {
	Status string `json:"status"`
}

// dealPaymentPayload is one receipt on the wire.
type dealPaymentPayload struct {
	PaymentID    string  `json:"payment_id"`
	ReceivedOn   string  `json:"received_on"`
	AmountRupees float64 `json:"amount_rupees"`
	Note         string  `json:"note"`
	CreatedAt    string  `json:"created_at"`
}

// dealPaymentWritePayload is the record-receipt body.
type dealPaymentWritePayload struct {
	ReceivedOn   string  `json:"received_on"`
	AmountRupees float64 `json:"amount_rupees"`
	Note         string  `json:"note"`
}

func (p dealPaymentWritePayload) toDomain() domain.DealPaymentWrite {
	return domain.DealPaymentWrite{ReceivedOn: p.ReceivedOn, AmountRupees: p.AmountRupees, Note: p.Note}
}

type dealPagePayload struct {
	Deals []dealPayload `json:"deals"`
	// total is the whole-filter count, never the page length. With limit and offset echoed back,
	// the client can render "Page 2 of 3" and a working Back control without inventing its own
	// state.
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// dealWritePayload is the record-sale body.
type dealWritePayload struct {
	SaleDate      string   `json:"sale_date"`
	Farm          string   `json:"farm"`
	ProductType   string   `json:"product_type"`
	Breed         string   `json:"breed"`
	BuyerName     string   `json:"buyer_name"`
	BuyerPlace    string   `json:"buyer_place"`
	BuyerVendorID string   `json:"buyer_vendor_id"`
	AnimalCount   *float64 `json:"animal_count"`
	MaleCount     *float64 `json:"male_count"`
	FemaleCount   *float64 `json:"female_count"`
	TotalWeightKg *float64 `json:"total_weight_kg"`
	SalesValue    float64  `json:"sales_value"`
	AdvanceAmount *float64 `json:"advance_amount"`
	Comments      string   `json:"comments"`
	// Optional: blank records the default, Deal Closed. Named for an EXPECTED sale ("Advance
	// Paid", "In Discussion") whose advance is already in hand.
	Status string `json:"status"`
}

func (p dealWritePayload) toDomain() domain.DealWrite {
	return domain.DealWrite{
		SaleDate: p.SaleDate, Farm: p.Farm, ProductType: p.ProductType, Breed: p.Breed,
		BuyerName: p.BuyerName, BuyerPlace: p.BuyerPlace, BuyerVendorID: p.BuyerVendorID,
		AnimalCount: p.AnimalCount, MaleCount: p.MaleCount, FemaleCount: p.FemaleCount,
		TotalWeightKg: p.TotalWeightKg, SalesValue: p.SalesValue, AdvanceAmount: p.AdvanceAmount,
		Status:   p.Status,
		Comments: p.Comments,
	}
}

func toDealPayload(d domain.Deal) dealPayload {
	// Empty slice, never nil: a JSON null where the client expects a list is a render crash, and
	// "no receipts yet" is the normal state of sheet history.
	payments := make([]dealPaymentPayload, 0, len(d.Payments))
	for _, payment := range d.Payments {
		payments = append(payments, dealPaymentPayload{
			PaymentID: payment.PaymentID, ReceivedOn: payment.ReceivedOn,
			AmountRupees: payment.AmountRupees, Note: payment.Note, CreatedAt: payment.CreatedAt,
		})
	}
	return dealPayload{
		DealID: d.DealID, SaleDate: d.SaleDate, Farm: d.Farm,
		SourceSalesID: d.SourceSalesID, SourcePurchaseID: d.SourcePurchaseID,
		BuyerName: d.BuyerName, BuyerPlace: d.BuyerPlace, BuyerVendorID: d.BuyerVendorID,
		ProductType: d.ProductType, Breed: d.Breed,
		AnimalCount: d.AnimalCount, MaleCount: d.MaleCount, FemaleCount: d.FemaleCount,
		TotalWeightKg: d.TotalWeightKg,
		AdvanceAmount: d.AdvanceAmount, SalesValue: d.SalesValue,
		PaymentReceived: d.PaymentReceived, PaymentBalance: d.PaymentBalance(), Payments: payments,
		Status: d.Status, Feedback: d.Feedback, Comments: d.Comments,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

// overviewPayload is the whole-page contract behind GET /sales/overview.
type overviewPayload struct {
	Summary          summaryPayload           `json:"summary"`
	Monthly          []monthlyPayload         `json:"monthly"`
	PriceBands       []priceBandPayload       `json:"price_bands"`
	Buyers           []buyerPayload           `json:"buyers"`
	BuyerPipeline    buyerPipelinePayload     `json:"buyer_pipeline"`
	FPOPipeline      fpoPipelinePayload       `json:"fpo_pipeline"`
	TagRoster        tagRosterPayload         `json:"tag_roster"`
	WeightAudit      weightAuditPayload       `json:"weight_audit"`
	MarketBenchmarks []marketBenchmarkPayload `json:"market_benchmarks"`
}

type summaryPayload struct {
	Revenue            float64 `json:"revenue"`
	LiveRevenue        float64 `json:"live_revenue"`
	Deals              int     `json:"deals"`
	Animals            float64 `json:"animals"`
	Sheep              float64 `json:"sheep"`
	Goats              float64 `json:"goats"`
	LiveWeightKg       float64 `json:"live_weight_kg"`
	RealizedPricePerKg float64 `json:"realized_price_per_kg"`
	ManureKg           float64 `json:"manure_kg"`
	ManureRevenue      float64 `json:"manure_revenue"`
	PeriodFrom         string  `json:"period_from"`
	PeriodTo           string  `json:"period_to"`
}

type monthlyPayload struct {
	Month         string  `json:"month"`
	SheepRevenue  float64 `json:"sheep_revenue"`
	GoatRevenue   float64 `json:"goat_revenue"`
	ManureRevenue float64 `json:"manure_revenue"`
	SheepCount    float64 `json:"sheep_count"`
	GoatCount     float64 `json:"goat_count"`
	ManureKg      float64 `json:"manure_kg"`
}

type priceBandPayload struct {
	ProductType   string  `json:"product_type"`
	Breed         string  `json:"breed"`
	Deals         int     `json:"deals"`
	Animals       float64 `json:"animals"`
	WeightKg      float64 `json:"weight_kg"`
	Revenue       float64 `json:"revenue"`
	AvgPricePerKg float64 `json:"avg_price_per_kg"`
	MinPricePerKg float64 `json:"min_price_per_kg"`
	MaxPricePerKg float64 `json:"max_price_per_kg"`
}

type buyerPayload struct {
	BuyerName    string   `json:"buyer_name"`
	BuyerPlace   string   `json:"buyer_place"`
	ProductTypes []string `json:"product_types"`
	Deals        int      `json:"deals"`
	Animals      float64  `json:"animals"`
	Revenue      float64  `json:"revenue"`
	SharePct     float64  `json:"share_pct"`
}

type statusCountPayload struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

type placeCountPayload struct {
	Place string `json:"place"`
	Count int    `json:"count"`
}

type buyerPipelinePayload struct {
	Total     int                  `json:"total"`
	Statuses  []statusCountPayload `json:"statuses"`
	TopPlaces []placeCountPayload  `json:"top_places"`
}

type fpoPipelinePayload struct {
	Total     int                  `json:"total"`
	Statuses  []statusCountPayload `json:"statuses"`
	Districts []placeCountPayload  `json:"districts"`
}

type tagTypeCountPayload struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type tagRosterPayload struct {
	Total      int                   `json:"total"`
	SalesCount int                   `json:"sales_count"`
	ByType     []tagTypeCountPayload `json:"by_type"`
}

type weightAuditPayload struct {
	Total      int     `json:"total"`
	Within03Kg int     `json:"within_0_3_kg"`
	Within1Kg  int     `json:"within_1_kg"`
	Over1Kg    int     `json:"over_1_kg"`
	MaxGapKg   float64 `json:"max_gap_kg"`
}

type marketBenchmarkPayload struct {
	Market           *string  `json:"market"`
	Category         *string  `json:"category"`
	Breed            string   `json:"breed"`
	Source           *string  `json:"source"`
	ExFarmRate       *string  `json:"ex_farm_rate"`
	TransportRate    *string  `json:"transport_rate"`
	LandingCostPerKg *float64 `json:"landing_cost_per_kg"`
	MarketPricePerKg *float64 `json:"market_price_per_kg"`
}

func toOverviewPayload(o domain.Overview) overviewPayload {
	monthly := make([]monthlyPayload, 0, len(o.Monthly))
	for _, m := range o.Monthly {
		monthly = append(monthly, monthlyPayload{
			Month: m.Month, SheepRevenue: m.SheepRevenue, GoatRevenue: m.GoatRevenue,
			ManureRevenue: m.ManureRevenue, SheepCount: m.SheepCount, GoatCount: m.GoatCount,
			ManureKg: m.ManureKg,
		})
	}
	bands := make([]priceBandPayload, 0, len(o.PriceBands))
	for _, b := range o.PriceBands {
		bands = append(bands, priceBandPayload{
			ProductType: b.ProductType, Breed: b.Breed, Deals: b.Deals, Animals: b.Animals,
			WeightKg: b.WeightKg, Revenue: b.Revenue, AvgPricePerKg: b.AvgPricePerKg,
			MinPricePerKg: b.MinPricePerKg, MaxPricePerKg: b.MaxPricePerKg,
		})
	}
	buyers := make([]buyerPayload, 0, len(o.Buyers))
	for _, b := range o.Buyers {
		buyers = append(buyers, buyerPayload{
			BuyerName: b.BuyerName, BuyerPlace: b.BuyerPlace, ProductTypes: b.ProductTypes,
			Deals: b.Deals, Animals: b.Animals, Revenue: b.Revenue, SharePct: b.SharePct,
		})
	}
	benchmarks := make([]marketBenchmarkPayload, 0, len(o.MarketBenchmarks))
	for _, b := range o.MarketBenchmarks {
		benchmarks = append(benchmarks, marketBenchmarkPayload{
			Market: b.Market, Category: b.Category, Breed: b.Breed, Source: b.Source,
			ExFarmRate: b.ExFarmRate, TransportRate: b.TransportRate,
			LandingCostPerKg: b.LandingCostPerKg, MarketPricePerKg: b.MarketPricePerKg,
		})
	}
	return overviewPayload{
		Summary: summaryPayload{
			Revenue: o.Summary.Revenue, LiveRevenue: o.Summary.LiveRevenue, Deals: o.Summary.Deals,
			Animals: o.Summary.Animals, Sheep: o.Summary.Sheep, Goats: o.Summary.Goats,
			LiveWeightKg: o.Summary.LiveWeightKg, RealizedPricePerKg: o.Summary.RealizedPricePerKg,
			ManureKg: o.Summary.ManureKg, ManureRevenue: o.Summary.ManureRevenue,
			PeriodFrom: o.Summary.PeriodFrom, PeriodTo: o.Summary.PeriodTo,
		},
		Monthly:    monthly,
		PriceBands: bands,
		Buyers:     buyers,
		BuyerPipeline: buyerPipelinePayload{
			Total:     o.BuyerPipeline.Total,
			Statuses:  statusCounts(o.BuyerPipeline.Statuses),
			TopPlaces: placeCounts(o.BuyerPipeline.TopPlaces),
		},
		FPOPipeline: fpoPipelinePayload{
			Total:     o.FPOPipeline.Total,
			Statuses:  statusCounts(o.FPOPipeline.Statuses),
			Districts: placeCounts(o.FPOPipeline.Districts),
		},
		TagRoster: tagRosterPayload{
			Total: o.TagRoster.Total, SalesCount: o.TagRoster.SalesCount,
			ByType: tagTypeCounts(o.TagRoster.ByType),
		},
		WeightAudit: weightAuditPayload{
			Total: o.WeightAudit.Total, Within03Kg: o.WeightAudit.Within03Kg,
			Within1Kg: o.WeightAudit.Within1Kg, Over1Kg: o.WeightAudit.Over1Kg,
			MaxGapKg: o.WeightAudit.MaxGapKg,
		},
		MarketBenchmarks: benchmarks,
	}
}

func statusCounts(in []domain.StatusCount) []statusCountPayload {
	out := make([]statusCountPayload, 0, len(in))
	for _, s := range in {
		out = append(out, statusCountPayload{Status: s.Status, Count: s.Count})
	}
	return out
}

func placeCounts(in []domain.PlaceCount) []placeCountPayload {
	out := make([]placeCountPayload, 0, len(in))
	for _, p := range in {
		out = append(out, placeCountPayload{Place: p.Place, Count: p.Count})
	}
	return out
}

func tagTypeCounts(in []domain.TagTypeCount) []tagTypeCountPayload {
	out := make([]tagTypeCountPayload, 0, len(in))
	for _, t := range in {
		out = append(out, tagTypeCountPayload{Label: t.Label, Count: t.Count})
	}
	return out
}
