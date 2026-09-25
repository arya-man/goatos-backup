package http

import (
	"github.com/vgoats/goatos/backend/internal/sales/domain"
)

// Field names here, in contracts/openapi/app-api.yaml, and in the generated TypeScript client must
// move together. A rename on one side only is the "contract lie" failure mode: the client
// compiles, renders nothing, and nothing errors.

// salesOptionsPayload is GET /sales/options: the record-sale vocabularies, backend-owned so the
// phone renders the same farms, products, breeds and statuses the web drawer does.
type salesOptionsPayload struct {
	Farms        []string `json:"farms"`
	ProductTypes []string `json:"product_types"`
	// Products is the same registry with the KIND on each, which is what a form needs to decide
	// whether to ask for a head count or for kilograms at a rate. ProductTypes above is the bare
	// name list a client written before the registry still reads.
	Products []salesProductOptionPayload `json:"products"`
	// Breeds keyed by product type, in offer order.
	Breeds   map[string][]string        `json:"breeds"`
	Statuses []salesStatusOptionPayload `json:"statuses"`
	// DefaultStatus is what a blank status resolves to on the write.
	DefaultStatus string `json:"default_status"`
	// MaxSaleDateDaysAhead is how far past today a sale date may sit (the web caps at 60).
	MaxSaleDateDaysAhead int `json:"max_sale_date_days_ahead"`
}

type salesStatusOptionPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Tone  string `json:"tone"`
}

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

	Status string `json:"status"`
	// StatusOptions is what the status editor may offer for THIS deal: every status for a live
	// deal, none for a failed one (Deal Failed is final, maintainer decision 2026-09-25).
	StatusOptions []string `json:"status_options"`
	Feedback      *string  `json:"feedback"`
	Comments      *string  `json:"comments"`

	// Lines are what was sold, in entry order (migration 000296). product_type / breed / the
	// counts / total_weight_kg / sales_value above are their ROLLUP ("Mixed" when the lines
	// disagree on product or breed).
	Lines []dealLinePayload `json:"lines"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// sellableProductPagePayload is GET /sales/products: the farm's registry as its EDITOR sees it,
// with the two closed vocabularies its form offers. The vocabularies ride WITH the rows so the
// editor never writes its own copy of them -- what a person may choose is the backend's answer.
type sellableProductPagePayload struct {
	Products []sellableProductPayload       `json:"products"`
	Kinds    []sellableProductChoicePayload `json:"kinds"`
	Units    []sellableProductChoicePayload `json:"units"`
	// FeedItems is the farm's CONFIGURED feed list (feed_item_catalog, the same rows the ration
	// grid and the feed purchases are authored against). An item of the feed kind must BE one of
	// these -- a feed is not a name somebody types, and inventing one here would make a sale draw
	// on a store that does not exist.
	FeedItems []string `json:"feed_items"`
	// Species is what an ANIMAL item may be sold as, and it rides with the list for the same
	// reason: an animal item that names no species resolves to NO breeds, and since a sale's
	// breed is required, that item can never be sold. It looked saved and was unsellable.
	Species []string `json:"species"`
}

// sellableProductChoicePayload is one option of a closed vocabulary, with the words for it.
type sellableProductChoicePayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// sellableProductPayload is one registry row in its editor.
type sellableProductPayload struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Kind string `json:"kind"`
	Unit string `json:"unit"`
	// SpeciesCode narrows an animal item's breeds; empty on everything else.
	SpeciesCode string `json:"species_code"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
	// IsBuiltin rows may be renamed and reordered but never switched off or changed in kind.
	IsBuiltin bool `json:"is_builtin"`
	// PricedPerUnit says whether SELLING this asks for a quantity at a rate. Composed here rather
	// than derived by a client from the kind, so both surfaces agree on what a sale of it asks.
	PricedPerUnit bool `json:"priced_per_unit"`
}

// sellableProductWritePayload is the add/edit body. The code is absent when adding -- it is
// derived from the name, once -- and present when editing, which is what keeps a rename from
// orphaning the sales already recorded under the item.
type sellableProductWritePayload struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Unit        string `json:"unit"`
	SpeciesCode string `json:"species_code"`
	SortOrder   int    `json:"sort_order"`
	Status      string `json:"status"`
}

func (p sellableProductWritePayload) toDomain() domain.ProductWrite {
	return domain.ProductWrite{
		Code: p.Code, Name: p.Name, Kind: p.Kind, Unit: p.Unit,
		SpeciesCode: p.SpeciesCode, SortOrder: p.SortOrder, Status: p.Status,
	}
}

func toSellableProductPayload(row domain.ProductRow) sellableProductPayload {
	return sellableProductPayload{
		Code: row.Code, Name: row.Name, Kind: row.Kind, Unit: row.Unit,
		SpeciesCode: row.SpeciesCode, SortOrder: row.SortOrder,
		Status: row.Status, IsBuiltin: row.IsBuiltin,
		PricedPerUnit: row.PricedPerUnit(),
	}
}

// The two closed vocabularies the item form offers, in farm words. The HINT is what makes the
// choice answerable by someone who does not know the word "kind": it says what picking it DOES.
func sellableProductKindPayloads() []sellableProductChoicePayload {
	return []sellableProductChoicePayload{
		{Key: domain.KindAnimal, Label: "Animals", Hint: "Sold as a lot: how many, their weight, and the price you agreed."},
		{Key: domain.KindFeed, Label: "Feed from the store", Hint: "Picked from the feed list, and the kilograms sold come off the store."},
		{Key: domain.KindOther, Label: "Something else", Hint: "Anything else the farm sells, such as manure or tags. Sold at a rate per unit."},
	}
}

func sellableProductUnitPayloads() []sellableProductChoicePayload {
	return []sellableProductChoicePayload{
		{Key: domain.UnitKg, Label: "Kilograms", Hint: "Sold by weight."},
		{Key: domain.UnitNumber, Label: "Number", Hint: "Sold by the piece: 200 tags, 12 animals."},
	}
}

// salesProductOptionPayload is one row of the farm's sellable-product registry as a form sees it.
type salesProductOptionPayload struct {
	Name string `json:"name"`
	Code string `json:"code"`
	Kind string `json:"kind"`
	Unit string `json:"unit"`
	// PricedPerUnit says whether selling this asks for a quantity at a rate rather than a
	// negotiated lump value. Composed HERE so neither surface re-derives it from the kind: a form
	// that decided for itself would start asking a new kind of item the wrong questions.
	PricedPerUnit bool `json:"priced_per_unit"`
}

// dealLinePayload is one product/breed line of a deal on the wire.
type dealLinePayload struct {
	LineID string `json:"line_id"`
	LineNo int    `json:"line_no"`
	// ProductType is the name the line was sold under; ProductKind is what it DOES (animal, feed,
	// other), which is what a renderer needs to decide whether to show a head count or kilograms.
	// Both are stamped from the registry at write time (migration 000422), so a product renamed
	// since does not change what this sale says it was.
	ProductType string `json:"product_type"`
	ProductCode string `json:"product_code"`
	ProductKind string `json:"product_kind"`
	// Breed is the line's VARIANT: a breed for an animal line, the feed item for a feed line.
	Breed string `json:"breed"`
	// Quantity at RatePerUnit, for a line priced by the unit rather than as a lump.
	Quantity      *float64 `json:"quantity"`
	Unit          string   `json:"unit"`
	RatePerUnit   *float64 `json:"rate_per_unit"`
	AnimalCount   *float64 `json:"animal_count"`
	MaleCount     *float64 `json:"male_count"`
	FemaleCount   *float64 `json:"female_count"`
	TotalWeightKg *float64 `json:"total_weight_kg"`
	SalesValue    float64  `json:"sales_value"`
}

// dealLineWritePayload is one line of the record-sale body.
type dealLineWritePayload struct {
	ProductType string `json:"product_type"`
	Breed       string `json:"breed"`
	// How much, and at what rate. A feed line needs both; the value is then COMPUTED from them, so
	// a client sending all three has its sales_value replaced rather than trusted. There is
	// deliberately no product_kind here: a body that could name its own kind could sell a goat as
	// feed and draw it out of the store.
	Quantity      *float64 `json:"quantity"`
	RatePerUnit   *float64 `json:"rate_per_unit"`
	AnimalCount   *float64 `json:"animal_count"`
	MaleCount     *float64 `json:"male_count"`
	FemaleCount   *float64 `json:"female_count"`
	TotalWeightKg *float64 `json:"total_weight_kg"`
	SalesValue    float64  `json:"sales_value"`
}

// dealStatusWritePayload is the deal-status edit body.
type dealStatusWritePayload struct {
	Status string `json:"status"`
	// The desk having seen what the store holds and closed the sale anyway. Closing an expected
	// sale is when its feed finally leaves, so it is weighed against the store exactly as
	// recording one is, and answered the same way.
	StockShortfallAcknowledged bool `json:"stock_shortfall_acknowledged"`
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
//
// Either `lines` (one per product/breed, the 2026-09-12 shape) or the legacy single-product
// fields; when lines are sent the deal-level product/breed/counts/value are ignored and
// recomputed as the rollup of the lines.
type dealWritePayload struct {
	SaleDate      string                 `json:"sale_date"`
	Farm          string                 `json:"farm"`
	Lines         []dealLineWritePayload `json:"lines"`
	ProductType   string                 `json:"product_type"`
	Breed         string                 `json:"breed"`
	BuyerName     string                 `json:"buyer_name"`
	BuyerPlace    string                 `json:"buyer_place"`
	BuyerVendorID string                 `json:"buyer_vendor_id"`
	AnimalCount   *float64               `json:"animal_count"`
	MaleCount     *float64               `json:"male_count"`
	FemaleCount   *float64               `json:"female_count"`
	TotalWeightKg *float64               `json:"total_weight_kg"`
	SalesValue    float64                `json:"sales_value"`
	AdvanceAmount *float64               `json:"advance_amount"`
	Comments      string                 `json:"comments"`
	// Optional: blank records the default, Deal Closed. Named for an EXPECTED sale ("Advance
	// Paid", "In Discussion") whose advance is already in hand.
	Status string `json:"status"`
	// The desk having seen what the store thinks it holds and said the sale is right anyway
	// (maintainer decision 2026-09-23). Only ever true because a person ticked it after being
	// shown the balance.
	StockShortfallAcknowledged bool `json:"stock_shortfall_acknowledged"`
}

func (p dealWritePayload) toDomain() domain.DealWrite {
	lines := make([]domain.DealLineWrite, 0, len(p.Lines))
	for _, l := range p.Lines {
		lines = append(lines, domain.DealLineWrite{
			ProductType: l.ProductType, Breed: l.Breed,
			Quantity: l.Quantity, RatePerUnit: l.RatePerUnit,
			AnimalCount: l.AnimalCount, MaleCount: l.MaleCount, FemaleCount: l.FemaleCount,
			TotalWeightKg: l.TotalWeightKg, SalesValue: l.SalesValue,
		})
	}
	return domain.DealWrite{
		SaleDate: p.SaleDate, Farm: p.Farm, Lines: lines, ProductType: p.ProductType, Breed: p.Breed,
		BuyerName: p.BuyerName, BuyerPlace: p.BuyerPlace, BuyerVendorID: p.BuyerVendorID,
		AnimalCount: p.AnimalCount, MaleCount: p.MaleCount, FemaleCount: p.FemaleCount,
		TotalWeightKg: p.TotalWeightKg, SalesValue: p.SalesValue, AdvanceAmount: p.AdvanceAmount,
		Status:                     p.Status,
		Comments:                   p.Comments,
		StockShortfallAcknowledged: p.StockShortfallAcknowledged,
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
	lines := make([]dealLinePayload, 0, len(d.Lines))
	for _, line := range d.Lines {
		lines = append(lines, dealLinePayload{
			LineID: line.LineID, LineNo: line.LineNo,
			ProductType: line.ProductType, ProductCode: line.Code(), ProductKind: line.Kind(),
			Breed:    line.Breed,
			Quantity: line.Quantity, Unit: line.Unit, RatePerUnit: line.RatePerUnit,
			AnimalCount: line.AnimalCount, MaleCount: line.MaleCount, FemaleCount: line.FemaleCount,
			TotalWeightKg: line.TotalWeightKg, SalesValue: line.SalesValue,
		})
	}
	return dealPayload{
		DealID: d.DealID, SaleDate: d.SaleDate, Farm: d.Farm, Lines: lines,
		SourceSalesID: d.SourceSalesID, SourcePurchaseID: d.SourcePurchaseID,
		BuyerName: d.BuyerName, BuyerPlace: d.BuyerPlace, BuyerVendorID: d.BuyerVendorID,
		ProductType: d.ProductType, Breed: d.Breed,
		AnimalCount: d.AnimalCount, MaleCount: d.MaleCount, FemaleCount: d.FemaleCount,
		TotalWeightKg: d.TotalWeightKg,
		AdvanceAmount: d.AdvanceAmount, SalesValue: d.SalesValue,
		PaymentReceived: d.PaymentReceived, PaymentBalance: d.PaymentBalance(), Payments: payments,
		Status: d.Status, Feedback: d.Feedback, Comments: d.Comments,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
		StatusOptions: domain.NextStatuses(d.Status),
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
	// sold_weight_bands (maintainer decisions 2026-09-08 and 2026-09-21): every animal sold on a
	// closed deal, by weight -- measured at tagging, spread from the load's own recorded weight,
	// or from a recorded assumption. Disjoint bands plus the unweighed remainder; they sum to
	// total, and total is the same animal count summary.animals reports.
	SoldWeightBands soldWeightBandsPayload `json:"sold_weight_bands"`
	FarmValuation   farmValuationPayload   `json:"farm_valuation"`
}

type farmValuationPayload struct {
	TotalValueRupees float64                      `json:"total_value_rupees"`
	TotalMeatKg      float64                      `json:"total_meat_kg"`
	TotalAnimals     int                          `json:"total_animals"`
	ValuedAnimals    int                          `json:"valued_animals"`
	ExcludedAnimals  int                          `json:"excluded_animals"`
	NotValued        []farmValuationNotValued     `json:"not_valued"`
	Buckets          []farmValuationBucketPayload `json:"buckets"`
}

type farmValuationNotValued struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type farmValuationBucketPayload struct {
	Bucket         string  `json:"bucket"`
	Label          string  `json:"label"`
	AnimalCount    int     `json:"animal_count"`
	WeightKg       float64 `json:"weight_kg"`
	PricePerKg     float64 `json:"price_per_kg"`
	MeatKg         float64 `json:"meat_kg"`
	ValueRupees    float64 `json:"value_rupees"`
	ActualWeight   bool    `json:"actual_weight"`
	WeighedAnimals int     `json:"weighed_animals"`
	// By recorded sex; disjoint, and they sum to animal_count.
	MaleCount       int `json:"male_count"`
	FemaleCount     int `json:"female_count"`
	SexMissingCount int `json:"sex_missing_count"`
}

type soldWeightBandsPayload struct {
	Total int `json:"total"`
	// The three provenances, whole-register. They sum to Total minus Unweighed.
	Measured    int `json:"measured"`
	LoadAverage int `json:"load_average"`
	Estimated   int `json:"estimated"`
	Unweighed   int `json:"unweighed"`
	// Always four, heaviest first, zeros included -- a band that disappears when it is empty
	// reads as a band that does not exist.
	Bands []soldWeightBandPayload `json:"bands"`
}

type soldWeightBandPayload struct {
	Band        string `json:"band"`
	Total       int    `json:"total"`
	Measured    int    `json:"measured"`
	LoadAverage int    `json:"load_average"`
	Estimated   int    `json:"estimated"`
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
	// The non-animal halves of the same revenue, in buckets DISJOINT from the live ones above:
	// animals + manure + feed + other sum to Revenue. They are carried on the wire because a
	// figure a client cannot read is a figure the farm cannot see -- these were computed and then
	// dropped here, so a feed sale reached the screen as revenue with no kilograms behind it.
	FeedKg       float64 `json:"feed_kg"`
	FeedRevenue  float64 `json:"feed_revenue"`
	OtherKg      float64 `json:"other_kg"`
	OtherRevenue float64 `json:"other_revenue"`
	PeriodFrom   string  `json:"period_from"`
	PeriodTo     string  `json:"period_to"`
}

type monthlyPayload struct {
	Revenue       float64 `json:"revenue"`
	LiveRevenue   float64 `json:"live_revenue"`
	Animals       float64 `json:"animals"`
	FeedRevenue   float64 `json:"feed_revenue"`
	FeedKg        float64 `json:"feed_kg"`
	OtherRevenue  float64 `json:"other_revenue"`
	OtherKg       float64 `json:"other_kg"`
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
			Revenue: m.Revenue, LiveRevenue: m.LiveRevenue, Animals: m.Animals, FeedRevenue: m.FeedRevenue, FeedKg: m.FeedKg, OtherRevenue: m.OtherRevenue, OtherKg: m.OtherKg,
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
	valuationBuckets := make([]farmValuationBucketPayload, 0, len(o.FarmValuation.Buckets))
	for _, b := range o.FarmValuation.Buckets {
		valuationBuckets = append(valuationBuckets, farmValuationBucketPayload{
			Bucket: b.Bucket, Label: b.Label, AnimalCount: b.AnimalCount,
			WeightKg: b.WeightKg, PricePerKg: b.PricePerKg, MeatKg: b.MeatKg,
			ValueRupees: b.ValueRupees, ActualWeight: b.ActualWeight,
			WeighedAnimals: b.WeighedAnimals,
			MaleCount:      b.MaleCount, FemaleCount: b.FemaleCount, SexMissingCount: b.SexMissingCount,
		})
	}
	notValued := make([]farmValuationNotValued, 0, len(o.FarmValuation.NotValued))
	for _, item := range o.FarmValuation.NotValued {
		notValued = append(notValued, farmValuationNotValued{Label: item.Label, Count: item.Count})
	}
	return overviewPayload{
		Summary: summaryPayload{
			Revenue: o.Summary.Revenue, LiveRevenue: o.Summary.LiveRevenue, Deals: o.Summary.Deals,
			Animals: o.Summary.Animals, Sheep: o.Summary.Sheep, Goats: o.Summary.Goats,
			LiveWeightKg: o.Summary.LiveWeightKg, RealizedPricePerKg: o.Summary.RealizedPricePerKg,
			ManureKg: o.Summary.ManureKg, ManureRevenue: o.Summary.ManureRevenue,
			FeedKg: o.Summary.FeedKg, FeedRevenue: o.Summary.FeedRevenue,
			OtherKg: o.Summary.OtherKg, OtherRevenue: o.Summary.OtherRevenue,
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
		SoldWeightBands: soldWeightBandsPayload{
			Total:       o.SoldWeightBands.Total,
			Measured:    o.SoldWeightBands.Measured,
			LoadAverage: o.SoldWeightBands.LoadAverage,
			Estimated:   o.SoldWeightBands.Estimated,
			Unweighed:   o.SoldWeightBands.Unweighed,
			Bands:       soldWeightBands(o.SoldWeightBands.Bands),
		},
		FarmValuation: farmValuationPayload{
			TotalValueRupees: o.FarmValuation.TotalValueRupees,
			TotalMeatKg:      o.FarmValuation.TotalMeatKg,
			TotalAnimals:     o.FarmValuation.TotalAnimals,
			ValuedAnimals:    o.FarmValuation.ValuedAnimals,
			ExcludedAnimals:  o.FarmValuation.ExcludedAnimals,
			NotValued:        notValued,
			Buckets:          valuationBuckets,
		},
	}
}

func soldWeightBands(in []domain.SoldWeightBand) []soldWeightBandPayload {
	out := make([]soldWeightBandPayload, 0, len(in))
	for _, b := range in {
		out = append(out, soldWeightBandPayload{
			Band: b.Band, Total: b.Total, Measured: b.Measured,
			LoadAverage: b.LoadAverage, Estimated: b.Estimated,
		})
	}
	return out
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
