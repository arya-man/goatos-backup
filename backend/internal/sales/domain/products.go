package domain

import (
	"fmt"
	"sort"
	"strings"
)

// WHAT THE FARM SELLS IS DATA (maintainer instruction 2026-09-23, migration 000393).
//
// 'Sheep', 'Goat' and 'Manure' used to be constants in this package, a CHECK in the schema and an
// option list in the page contract -- three statements of one fact, so a fourth thing to sell was
// a deploy. They are now rows of sellable_product_catalog that a person edits on Configuration ->
// Items and settings, and this file is the only thing about them code still knows.
//
// What code knows is the KIND, and nothing else:
//
//	animal  carries animal counts, a breed and live weight; feeds the price-per-kg bands, the
//	        weight bands and the tag-animals gate.
//	feed    names a feed item, is priced per kilogram, and draws those kilograms out of the store.
//	other   carries weight and revenue and never an animal count -- which is exactly what manure
//	        has always done.
//
// So a farm adding "Hay" or "Straw" as another `other` product needs no developer. A new KIND
// does, and that is the honest boundary: a kind is a behaviour, not a name.
const (
	KindAnimal = "animal"
	KindFeed   = "feed"
	KindOther  = "other"
)

// The three built-in rows' codes. Code keys the Sold page's Sheep / Goat / Manure cards, so
// those numbers survive the farm renaming a product on screen. They are the codes of ROWS, not a
// vocabulary: a farm may add a fourth product and this list does not grow.
const (
	ProductCodeSheep  = "sheep"
	ProductCodeGoat   = "goat"
	ProductCodeManure = "manure"
)

// Kinds is the closed set. It is closed because each one names code that exists.
var Kinds = []string{KindAnimal, KindFeed, KindOther}

// IsKind reports whether raw is a product kind this build can act on.
func IsKind(raw string) bool {
	return raw == KindAnimal || raw == KindFeed || raw == KindOther
}

// Product is one row of the registry: something the farm sells.
type Product struct {
	// Code is the row's stable identity. A sale line is stamped with it, so the Sold page's
	// Sheep / Goat / Manure cards keep their numbers when the farm renames a product on screen.
	Code string
	// Name is the word a sale is recorded under and a reader is shown.
	Name string
	Kind string
	// Unit is what one of it is sold by: head for an animal, kg for feed and manure.
	Unit string
	// SpeciesCode narrows an animal product's breeds to that species'. Empty for the other kinds,
	// which the registry refuses to give one.
	SpeciesCode string
	SortOrder   int
}

// IsLive reports whether this product carries animal counts and live weight.
func (p Product) IsLive() bool { return p.Kind == KindAnimal }

// DrawsFeedStock reports whether selling this product takes kilograms out of the feed store.
func (p Product) DrawsFeedStock() bool { return p.Kind == KindFeed }

// ProductCatalog is a tenant's active registry, in the order the farm put it in. It is passed to
// the write path and resolved INSIDE the writing transaction, the feed_item_catalog rule: a
// product archived between the form opening and the save landing must not get through.
type ProductCatalog struct {
	ordered []Product
	byName  map[string]Product
}

// NewProductCatalog builds a catalog from registry rows. Later duplicates of a name lose to
// earlier ones; the unique index on the table means that cannot happen in practice, and choosing
// deterministically is better than choosing whichever row the scan returned last.
func NewProductCatalog(rows []Product) ProductCatalog {
	c := ProductCatalog{byName: make(map[string]Product, len(rows))}
	for _, p := range rows {
		key := productKey(p.Name)
		if key == "" {
			continue
		}
		if _, seen := c.byName[key]; seen {
			continue
		}
		c.byName[key] = p
		c.ordered = append(c.ordered, p)
	}
	sort.SliceStable(c.ordered, func(i, j int) bool {
		if c.ordered[i].SortOrder != c.ordered[j].SortOrder {
			return c.ordered[i].SortOrder < c.ordered[j].SortOrder
		}
		return c.ordered[i].Name < c.ordered[j].Name
	})
	return c
}

// productKey matches a typed product name to a registry row the way the unique index does:
// case-folded and trimmed. It is deliberately NOT feed_config_norm -- that one also strips
// punctuation and spaces, which is right for matching a feed label across two spellings in a
// sheet and wrong for deciding that two DIFFERENT products the farm deliberately named apart are
// the same row.
func productKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Lookup resolves a product NAME as entered against the active registry.
func (c ProductCatalog) Lookup(name string) (Product, bool) {
	p, ok := c.byName[productKey(name)]
	return p, ok
}

// Products is the registry in farm order.
func (c ProductCatalog) Products() []Product {
	return append([]Product(nil), c.ordered...)
}

// Names is the vocabulary a form offers, in farm order.
func (c ProductCatalog) Names() []string {
	out := make([]string, 0, len(c.ordered))
	for _, p := range c.ordered {
		out = append(out, p.Name)
	}
	return out
}

// IsEmpty reports a tenant with no active products. A write against an empty catalog is refused
// rather than waved through: an empty registry means nobody has said what this farm sells, and
// guessing 'Sheep' on its behalf is how a constant grows back.
func (c ProductCatalog) IsEmpty() bool { return len(c.ordered) == 0 }

// BuiltinKind is the kind of the three products every tenant starts with, for the ONE case where
// no registry row is at hand: a line written before migration 000393 stamped a kind, or a deal
// read without its lines, whose synthetic line is built from the deal's own columns.
//
// It is a FALLBACK and never a source. Every write stamps the kind from the registry, so this
// answers only for rows recorded before the registry existed -- all of which are one of these
// three by construction, because until 000393 nothing else could be stored.
func BuiltinKind(productType string) string {
	switch productType {
	case ProductSheep, ProductGoat:
		return KindAnimal
	case ProductManure:
		return KindOther
	}
	return ""
}

// FeedStockShortfall is one feed line selling more than the store's ledger holds.
type FeedStockShortfall struct {
	LineNo      int
	FeedItem    string
	FarmLabel   string
	RequestedKg float64
	BalanceKg   float64
}

// ErrFeedStockShort is the CONFIRMATION a short feed sale raises (maintainer decision 2026-09-23).
//
// It is not a refusal. The sale may genuinely have happened while the purchase ledger is behind --
// a load reached the farm and nobody has recorded it yet -- and refusing it outright would make
// the register lie about feed that physically left. So the desk is told what the store thinks it
// holds and, having checked, sends the same sale again with the acknowledgement.
//
// The desk is TOLD the balance here, unlike the verifier's packed-weight warning, which is told
// only a direction. The two differ because of what the reader is for: a verifier is a second
// independent reading and must not be anchored to the figure she is checking, while the person
// recording a sale is being asked whether the LEDGER is wrong, and cannot answer that without
// seeing what the ledger says.
type ErrFeedStockShort struct {
	Shortfalls []FeedStockShortfall
}

func (e ErrFeedStockShort) Error() string {
	if len(e.Shortfalls) == 1 {
		s := e.Shortfalls[0]
		return fmt.Sprintf("sales: %s %s has %.3f kg in the store and this sale takes %.3f kg",
			s.FarmLabel, s.FeedItem, s.BalanceKg, s.RequestedKg)
	}
	return fmt.Sprintf("sales: %d feed lines take more than the store holds", len(e.Shortfalls))
}
