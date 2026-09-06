package domain

// TRANSITIONAL: the split-concentrate stock merge (maintainer decision 2026-09-06).
//
// The farm used to buy FOUR in-house concentrates -- adult goat, adult sheep,
// kids goat, kids sheep -- and now buys TWO, one adult and one kids, each fed
// to both species. The switch is a purchase decision, not a data migration, so
// for as long as the old sacks last the store holds up to three feeds that are
// one feed operationally, and the Stock tab showed them as three cards with
// three unrelated days-left figures: on 2026-09-06 CBE read "2 days left" for
// adult goat beside "166 days left" for the merged adult, and neither number
// was the farm's runway (the true answer was 12).
//
// StockFamilyMerge folds each retired split feed into its successor for the
// STOCK CARDS and the LOW-STOCK ALERT, on both sides of the division: the
// family's stock is the sum of its members' balances, and the family's burn
// rate is the family's kg per calendar day.
//
// WHY THE RATE IS PER FAMILY-DAY AND NOT A SUM OF THE MEMBERS' RATES: the feeds
// SUBSTITUTE for each other while the ration grid switches over, so on any one
// day the pens eat one member or another. On 2026-09-05 and -06 Channapatna fed
// 84 kg of the merged adult feed INSTEAD of the sheep feed; adding each member's
// own 3-day average gave 184.9 kg/day against a true family draw of 128.2 --
// 40% high, and a days-left a third short. Re-grouping consumption to the family
// BEFORE averaging counts each day once and needs no substitution rule.
//
// THIS IS TEMPORARY AND SELF-EXPIRING. Once every member's stock reaches zero
// and the ration grid names only the successors, each family is a single feed,
// the merge changes nothing, and this file plus its two query parameters can be
// deleted with no visible effect. It is deliberately a small Go table rather
// than a schema change or a catalog column, so that revert is a deletion rather
// than a second migration. A permanent successor/substitution model belongs on
// feed_item_catalog and is NOT what this is.
//
// Canonical prose, including the revert recipe and the numbers above:
// docs/decisions/feed-stock-transitional-concentrate-merge.md
type StockFamilyMergeRow struct {
	// MemberKey is the retired split feed whose leftover stock is folded in.
	MemberKey string
	// FamilyKey is the successor feed, and becomes the card's feed_item_key.
	FamilyKey string
	// FamilyLabel is the card's title. It is carried here rather than read from
	// feed_item_catalog so a family reads correctly even at a farm that has not
	// bought the successor yet -- the members alone must still title the card
	// with the feed the farm now buys.
	FamilyLabel string
}

// StockFamilyMerge is the whole mapping: four split feeds into two.
var StockFamilyMerge = []StockFamilyMergeRow{
	{MemberKey: "mesha_adult_concentrate_goat", FamilyKey: "mesha_adult_concentrate", FamilyLabel: "Mesha Adult Concentrate"},
	{MemberKey: "mesha_adult_concentrate_sheep", FamilyKey: "mesha_adult_concentrate", FamilyLabel: "Mesha Adult Concentrate"},
	{MemberKey: "mesha_kids_goat_concentrate", FamilyKey: "mesha_kids_concentrate", FamilyLabel: "Mesha Kids Concentrate"},
	{MemberKey: "mesha_kids_sheep_concentrate", FamilyKey: "mesha_kids_concentrate", FamilyLabel: "Mesha Kids Concentrate"},
}

// StockFamilyMergeArrays flattens the mapping into the three parallel text
// arrays the stock queries bind. Parallel arrays are built here, from one row
// slice, precisely so they cannot drift out of step -- the grain rule's
// same-key requirement applied to a parameter.
//
// An EMPTY mapping is the pre-merge behaviour exactly: every item is its own
// family, so the queries reduce to the per-item shape they had before. That is
// what makes the revert a deletion and what the merge tests assert against.
func StockFamilyMergeArrays() (members, families, labels []string) {
	members = make([]string, 0, len(StockFamilyMerge))
	families = make([]string, 0, len(StockFamilyMerge))
	labels = make([]string, 0, len(StockFamilyMerge))
	for _, row := range StockFamilyMerge {
		members = append(members, row.MemberKey)
		families = append(families, row.FamilyKey)
		labels = append(labels, row.FamilyLabel)
	}
	return members, families, labels
}
