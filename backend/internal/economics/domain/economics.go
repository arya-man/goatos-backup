// Package domain holds the Business Economics read-model types.
//
// Business Economics is a READ-ONLY reporting module under the Sales vertical:
// what an animal costs the farm per day (priced feed direction), what it gains
// per day (weighing pairs), and what a kg of that gain sells for (the sales
// ledger, at DEAL grain). It mirrors the Growth Director module's shape and
// boundary on purpose: weighing stays isolated (this module CONSUMES weighing
// tables read-only and never gates any weighing behaviour), the sales lock
// stays intact (it reads sales_deals only — never joined to a herd table, and
// since the sold panel was dropped it does not read the goat_sale_allocations
// mapping either), and nothing here writes anything, ever.
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

// MaxGroupRows caps the shed and breed tables. The pulse and band figures are
// whole-filter aggregates computed independently of this cap, so truncating a
// table never bends a summary number. Sheds and breeds are naturally bounded
// (a farm has tens of pens and a handful of breeds); the cap is a backstop.
const MaxGroupRows = 200

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
// per-SHED table, the per-BREED comparison and the break-even bands. The grain
// is the pen and the breed, never the individual animal (maintainer decision
// 2026-08-26): a per-animal list is 300 rows nobody acts on, while a pen and a
// breed are things the farm can actually change. Estimate is ALWAYS true: feed cost
// is what the sheet DIRECTED priced at the latest load, not what was eaten.
//
// There is deliberately NO per-animal sale panel (maintainer decision
// 2026-08-25). No per-animal sale price is recorded anywhere, so such a panel
// could only show the deal value split evenly across its animals — a number the
// farm never negotiated. The realized price per kg on the pulse is the honest
// deal-grain figure, and the Sales page owns the deals themselves.
type BusinessEconomics struct {
	Period   Period            `json:"period"`
	Parks    []Park            `json:"parks"`
	Pulse    Pulse             `json:"pulse"`
	Sheds    []ShedEconomics  `json:"sheds"`
	Breeds   []BreedEconomics `json:"breeds"`
	Bands    []BandEconomics  `json:"bands"`
	Estimate bool             `json:"estimate"`
}

// Pulse is the headline strip. Money figures are nullable and null never means
// zero: it means the input that would make the figure honest is missing, with
// the reason readable from the disclosure counts beside it.
type Pulse struct {
	// FeedCostPerDayRupees and ValueAddedPerDayRupees COVER THE SAME ANIMALS —
	// the PricedAnimals set — so that subtracting one from the other is a true
	// statement and NetPerDayRupees is that subtraction (maintainer decision
	// 2026-08-26).
	//
	// This is the correction of a real defect: the feed figure used to be the
	// WHOLE FARM (1,649 animals) while the value figure covered only the
	// animals weighed twice (309). Side by side they invited a subtraction that
	// read as "the farm loses ₹39k a day" when the truth was "most of the herd
	// has not been weighed". Two figures a reader will subtract must range over
	// one set; the whole-farm number is still published, as FarmFeedCostPerDayRupees,
	// where it is labelled as covering a different (larger) population.
	//
	// FeedCostPerDayRupees sums each priced animal's own daily ration cost.
	FeedCostPerDayRupees *float64 `json:"feed_cost_per_day_rupees"`
	// ValueAddedPerDayRupees prices those same animals' measured daily gain at
	// the realized price per kg. An animal that did not measurably grow
	// contributes ZERO here while still contributing its full cost above —
	// which is exactly the business fact the page exists to show, and why this
	// is not filtered to growers only.
	ValueAddedPerDayRupees *float64 `json:"value_added_per_day_rupees"`
	// NetPerDayRupees is ValueAdded − FeedCost over that one shared set.
	NetPerDayRupees *float64 `json:"net_per_day_rupees"`
	// FarmFeedCostPerDayRupees is the WHOLE-SCOPE feed spend on the LATEST sheet
	// day (FarmFeedDay), not a window average: spend on the live herd climbed
	// ₹27,648 -> ₹64,676 across one window's sheet days, so an average described
	// no day that ever happened and understated the current rate by ₹10,000.
	// It covers FarmAnimals, NOT the tile set above, and must always be rendered
	// with that population and that day named.
	FarmFeedCostPerDayRupees *float64 `json:"farm_feed_cost_per_day_rupees"`
	// FarmFeedDay is the business date FarmFeedCostPerDayRupees describes.
	FarmFeedDay string `json:"farm_feed_day"`
	// FarmUnpricedKg is feed directed on that day that NO purchase can price, in
	// kg. It is the honest size of what the rupee figure is missing (on the live
	// herd, two concentrates with no purchase rows at all). Never estimated at
	// another item's rate: inventing a price would make the total look complete
	// when it is not.
	FarmUnpricedKg *float64 `json:"farm_unpriced_kg"`
	// FarmAnimals is the live animal count in scope — the population the farm
	// feed figure covers.
	FarmAnimals int `json:"farm_animals"`
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
	// PricedAnimals is the shared denominator of the two comparable tiles:
	// paired animals whose pen+stage+breed ration cell resolved AND priced.
	PricedAnimals int `json:"priced_animals"`
	// CostAnimals is the narrower set behind MedianCostPerKgGain: priced AND
	// measurably growing, since a flat animal has no cost-per-kg.
	CostAnimals int `json:"cost_animals"`
	// UnpricedFeedItems counts feed items directed in the window that have NO
	// purchase on record to price them — their kg is missing from every rupee
	// figure on this page.
	UnpricedFeedItems int `json:"unpriced_feed_items"`

	// Sales side, window-scoped, closed deals, both farms.
	ClosedDeals       int     `json:"closed_deals"`
	AnimalsSold       int     `json:"animals_sold"`
	SoldRevenueRupees float64 `json:"sold_revenue_rupees"`
}

// GroupEconomics is the shared shape of the two comparison tables. Every money
// figure is PER HEAD PER DAY over the animals behind it, never a group total:
// only the weighed animals of a pen are in scope, so a "pen total" would
// understate a pen where few animals were weighed, while a per-head figure
// compares honestly across pens of any size.
//
// The figures are MEANS over that group, and deliberately consistent with each
// other: ValueAddedPerDayRupees is ADGGPerDay priced, so the two always agree.
// Money fields are nullable; null means "not computable", never zero.
type GroupEconomics struct {
	// Animals is the denominator: paired animals in this group whose ration cell
	// resolved and priced.
	Animals int `json:"animals"`
	// ADGGPerDay is the mean measured daily gain. Animals scored flat by the 3%
	// scale-noise floor are IN this mean at 0 — a pen that is not growing must
	// read as not growing.
	ADGGPerDay             *float64 `json:"adg_g_per_day"`
	FeedCostPerDayRupees   *float64 `json:"feed_cost_per_day_rupees"`
	ValueAddedPerDayRupees *float64 `json:"value_added_per_day_rupees"`
	NetPerDayRupees        *float64 `json:"net_per_day_rupees"`
	CostPerKgGainRupees    *float64 `json:"cost_per_kg_gain_rupees"`
	Signal                 string   `json:"signal"`
}

// ShedEconomics is one operational location — a PEN where the shed is divided,
// the shed itself where it is not. The pen is the grain the farm actually feeds
// (one bag per pen), so it is the grain a cost decision is made at.
type ShedEconomics struct {
	LocationID string `json:"location_id"`
	// PartitionLabel is the pen within LocationID, blank for an undivided shed.
	// It is half of this row's identity, not decoration: a partitioned shed
	// returns several rows under ONE location_id.
	PartitionLabel string `json:"partition_label"`
	// ShedDisplay is the backend-composed park-prefixed shed + pen label.
	ShedDisplay string `json:"shed_display"`
	GroupEconomics
}

// BreedEconomics is one breed across the whole selected scope: what a head of
// that breed eats a day against what its measured growth returns. This is the
// buy/keep question — which breed pays for its feed — so it is deliberately
// scope-wide rather than per pen.
type BreedEconomics struct {
	Breed string `json:"breed"`
	GroupEconomics
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
