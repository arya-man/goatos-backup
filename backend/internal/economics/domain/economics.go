// Package domain holds the Business Economics read-model types.
//
// Business Economics is a READ-ONLY reporting module under the Sales vertical:
// what an animal costs the farm per day (priced feed direction), what it gains
// per day (weighing pairs), and what animals actually sell for (the sales
// ledger through the goat_sale_allocations mapping). It mirrors the Growth
// Director module's shape and boundary on purpose: weighing stays isolated
// (this module CONSUMES weighing tables read-only and never gates any weighing
// behaviour), the sales lock stays intact (sales still reads nothing from the
// herd; THIS module joins goat_sale_allocations — the identity-owned mapping —
// to the deal row by its opaque id, exactly the read path migration 000177
// describes), and nothing here writes anything, ever.
package domain

// Actor is the authenticated caller, resolved from auth grants by the HTTP layer.
type Actor struct {
	TenantID string
	UserID   string
	Roles    []string
}

// DefaultPeriodDays is the reporting window when the caller names no dates: the
// last 90 inclusive Asia/Kolkata business days ending today. Economics needs a
// longer default than the Weights screen's 28 days because feed cost and sale
// evidence accumulate over an animal's stay, not over one weigh cycle.
const DefaultPeriodDays = 90

// MaxParks caps the unpaged park vocabulary, mirroring the weighing module's
// planner cap.
const MaxParks = 100

// MaxAnimalRows caps the per-animal economics table. The pulse and band
// figures are whole-filter aggregates computed independently of this cap, so
// truncating the table never bends a summary number.
const MaxAnimalRows = 200

// MaxSoldRows caps the sold-animal panel, newest sale first.
const MaxSoldRows = 200

// PeriodResolutionCampaignWeek discloses the window semantics: weighing data is
// selected by CAMPAIGN-WEEK OVERLAP while feed and sales rows use the exact
// day range — the same convention the Growth Director read documents.
const PeriodResolutionCampaignWeek = "campaign_week"

// Weight band labels, ascending, matching the Growth Director road-to-sale
// bands so the two screens speak the same vocabulary.
var BandLabels = []string{"<15", "15-20", "20-25", "25-30", "30-35", "35+"}

// Per-animal daily-economics signals.
const (
	// SignalEarning: the animal's daily value added exceeds its daily feed cost.
	SignalEarning = "earning"
	// SignalBurning: daily feed cost exceeds daily value added — every extra day
	// on this animal costs more than it returns at today's realized price.
	SignalBurning = "burning"
	// SignalWatch: one side of the comparison is missing (no feed cost cell, no
	// realized price, or non-positive gain), so no honest verdict exists yet.
	SignalWatch = "watch"
)

// Realized-price bases. The window's own closed weighed deals are preferred;
// when the window has none, the trailing 365 days ending at the window end are
// used and disclosed, and when even that is empty the price (and everything
// derived from it) stays null.
const (
	PriceBasisWindow       = "window"
	PriceBasisTrailingYear = "trailing_year"
	PriceBasisNone         = "none"
)

// Park is the id/name pair behind a park scope option.
type Park struct {
	ParkID string `json:"park_id"`
	Name   string `json:"name"`
}

// Period is the resolved reporting window. Start/End are INCLUSIVE Asia/Kolkata
// business dates.
type Period struct {
	Start      string `json:"start"`
	End        string `json:"end"`
	Resolution string `json:"resolution"`
}

// BusinessEconomics is the whole Sales → Economics page: the pulse tiles, the
// per-animal table, the break-even bands and the sold-animal panel. Estimate is
// ALWAYS true: feed cost is what the sheet DIRECTED priced at the latest load,
// not what was eaten; per-animal sale revenue is the deal value apportioned
// per head, not a per-animal negotiated price (no such price exists anywhere).
type BusinessEconomics struct {
	Period   Period            `json:"period"`
	Parks    []Park            `json:"parks"`
	Pulse    Pulse             `json:"pulse"`
	Animals  []AnimalEconomics `json:"animals"`
	Bands    []BandEconomics   `json:"bands"`
	Sold     []SoldAnimal      `json:"sold"`
	Estimate bool              `json:"estimate"`
}

// Pulse is the headline strip. Money figures are nullable and null never means
// zero: it means the input that would make the figure honest is missing, with
// the reason readable from the disclosure counts beside it.
type Pulse struct {
	// FeedCostPerDayRupees is the whole-scope average daily feed spend over the
	// window: every priced directed cell (normal AND experiment — trial feed is
	// real money) summed per feed day, averaged over the days that have a sheet.
	FeedCostPerDayRupees *float64 `json:"feed_cost_per_day_rupees"`
	// ValueAddedPerDayRupees prices the measured daily gain of every paired,
	// matched, live animal at the realized price per kg. Its denominator is
	// PairedAnimals, not the herd: unweighed animals add unknown value.
	ValueAddedPerDayRupees *float64 `json:"value_added_per_day_rupees"`
	// RealizedPricePerKg is closed live-animal revenue over live weight sold,
	// on the basis PriceBasis discloses. Deal figures are BOTH FARMS always:
	// the sales ledger records a farm label, not a park id, so a park filter
	// narrows animal and feed figures only.
	RealizedPricePerKg *float64 `json:"realized_price_per_kg"`
	PriceBasis         string   `json:"price_basis"`
	// MedianCostPerKgGain is the median over CostAnimals of daily feed cost
	// divided by daily gain — what one kg of live-weight gain costs to put on.
	MedianCostPerKgGain *float64 `json:"median_cost_per_kg_gain"`

	// Honest denominators.
	WeighedIdentities int `json:"weighed_identities"`
	PairedAnimals     int `json:"paired_animals"`
	CostAnimals       int `json:"cost_animals"`
	// UnpricedFeedItems counts feed items directed in the window that have NO
	// purchase on record to price them — their kg is missing from every rupee
	// figure on this page.
	UnpricedFeedItems int `json:"unpriced_feed_items"`

	// Sales side, window-scoped, closed deals, both farms.
	ClosedDeals       int     `json:"closed_deals"`
	AnimalsSold       int     `json:"animals_sold"`
	SoldRevenueRupees float64 `json:"sold_revenue_rupees"`
}

// AnimalEconomics is one live, matched, paired animal's daily economics. Rows
// exist only for animals weighed at least twice in the window whose tag
// resolves in the herd register — the trust figures on the pulse carry the
// rest. Money fields are nullable; null means "not computable", never zero.
type AnimalEconomics struct {
	TagDisplay  string `json:"tag_display"`
	DisplayID   string `json:"display_id"`
	Breed       string `json:"breed"`
	Sex         string `json:"sex"`
	Stage       string `json:"stage"`
	ShedDisplay string `json:"shed_display"`

	LatestWeightKg float64 `json:"latest_weight_kg"`
	ADGGPerDay     float64 `json:"adg_g_per_day"`
	SpanDays       int     `json:"span_days"`

	// FeedCostPerDayRupees is the animal's pen + stage + breed feed cell: the
	// priced per-head daily direction for exactly the grain the feed sheet
	// feeds this animal under, averaged over the window's sheet days.
	FeedCostPerDayRupees   *float64 `json:"feed_cost_per_day_rupees"`
	CostPerKgGainRupees    *float64 `json:"cost_per_kg_gain_rupees"`
	ValueAddedPerDayRupees *float64 `json:"value_added_per_day_rupees"`
	NetPerDayRupees        *float64 `json:"net_per_day_rupees"`
	Signal                 string   `json:"signal"`
}

// BandEconomics is the break-even read at weight-band grain: for each road-to-
// sale band, what the median animal there gains, costs and returns per day.
// SellSignal is true when the band's median net is zero or negative — at
// today's realized price, keeping the median animal of this band loses money,
// which is the sell-point conversation this page exists to start.
type BandEconomics struct {
	Band                   string   `json:"band"`
	Animals                int      `json:"animals"`
	MedianADGGPerDay       *float64 `json:"median_adg_g_per_day"`
	FeedCostPerDayRupees   *float64 `json:"feed_cost_per_day_rupees"`
	ValueAddedPerDayRupees *float64 `json:"value_added_per_day_rupees"`
	NetPerDayRupees        *float64 `json:"net_per_day_rupees"`
	SellSignal             bool     `json:"sell_signal"`
}

// SoldAnimal is one animal tagged to a sale in the window, through the
// identity-owned goat_sale_allocations mapping. ApportionedRevenueRupees is
// the deal's recorded value divided by its LIVE tagged allocations — an
// even head split, disclosed as an estimate, because no per-animal price is
// recorded anywhere. LastWeightKg is the animal's last accepted weighing by
// tag, whole history, and RealizedPerKg divides the apportioned revenue by it.
type SoldAnimal struct {
	TagNumber   string   `json:"tag_number"`
	SaleDate    string   `json:"sale_date"`
	BuyerName   string   `json:"buyer_name"`
	Farm        string   `json:"farm"`
	ShedDisplay string   `json:"shed_display"`
	DealAnimals int      `json:"deal_animals"`

	ApportionedRevenueRupees *float64 `json:"apportioned_revenue_rupees"`
	LastWeightKg             *float64 `json:"last_weight_kg"`
	RealizedPerKg            *float64 `json:"realized_per_kg"`
}
