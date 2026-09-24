package domain

import (
	"fmt"
	"sort"
	"strings"
)

// WHAT THE FARM SELLS IS DATA (maintainer instruction 2026-09-23, migration 000402).
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
	// Aliases retain earlier names for writes queued before a rename.
	Aliases []string
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

// ProductRow is one registry row as the EDITOR sees it: the product plus the two facts only the
// editor needs -- whether it is switched off, and whether it is one of the three the farm cannot
// switch off.
type ProductRow struct {
	Product
	Status    string
	IsBuiltin bool
}

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
		for _, alias := range p.Aliases {
			c.byName[productKey(alias)] = p
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
// no registry row is at hand: a line written before migration 000402 stamped a kind, or a deal
// read without its lines, whose synthetic line is built from the deal's own columns.
//
// It is a FALLBACK and never a source. Every write stamps the kind from the registry, so this
// answers only for rows recorded before the registry existed -- all of which are one of these
// three by construction, because until 000402 nothing else could be stored.
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

// Units an item may be sold by (maintainer instruction 2026-09-23). The farm answers one plain
// question -- by the kilogram, or by number -- so the vocabulary is exactly those two answers.
const (
	UnitKg     = "kg"
	UnitNumber = "number"
)

// Units is the closed set the item form offers.
var Units = []string{UnitKg, UnitNumber}

// IsUnit reports whether raw is a unit an item may be sold by.
func IsUnit(raw string) bool { return raw == UnitKg || raw == UnitNumber }

// PricedPerUnit reports whether selling this product asks for a quantity at a rate rather than a
// negotiated lump value.
//
// Animals are the exception and it is deliberate (maintainer decision 2026-09-23): a lot of goats
// is haggled as a lot, not at a fixed rate per head, and every sale recorded so far reads that
// way. Everything else -- feed by the kilogram, sheep tags by number -- is quantity times rate.
func (p Product) PricedPerUnit() bool { return p.Kind != KindAnimal }

// MaxProductNameLength bounds an item name. It is stored on every line of every sale made under
// it, and rendered in a dropdown.
const MaxProductNameLength = 60

// ErrProductValidation is a refused item edit, naming the field a person got wrong.
type ErrProductValidation struct {
	Field  string
	Reason string
}

func (e ErrProductValidation) Error() string {
	return fmt.Sprintf("sellable product %s: %s", e.Field, e.Reason)
}

// ProductWrite is an item being added to, or edited in, the farm's registry.
type ProductWrite struct {
	// Code is empty when adding: the farm names the item, and the code is derived from that name
	// once and never changes, so renaming the item later cannot orphan the sales recorded under it.
	Code   string
	Name   string
	Kind   string
	Unit   string
	Status string
	// SpeciesCode narrows an animal item's breeds. Only the animal kind may carry one.
	SpeciesCode string
	SortOrder   int

	// Adding is set by Normalize when the caller supplied NO code, meaning this is a new item
	// rather than an edit of an existing one. It matters because the code is DERIVED from the
	// name: adding an item called "Feed" when a Feed already exists derives the same code, and
	// without this the write would land on that row and quietly rewrite it. The screen calls that
	// adding; the farm would call it losing its feed item.
	Adding bool
}

// Normalize trims the fields and, for a new item, derives its code from its name.
func (w ProductWrite) Normalize() ProductWrite {
	out := w
	out.Name = strings.Join(strings.Fields(w.Name), " ")
	out.Code = strings.TrimSpace(w.Code)
	out.Kind = strings.TrimSpace(w.Kind)
	out.Unit = strings.TrimSpace(w.Unit)
	out.Status = strings.TrimSpace(w.Status)
	out.SpeciesCode = strings.TrimSpace(w.SpeciesCode)
	if out.Status == "" {
		out.Status = StatusActive
	}
	if out.Code == "" {
		out.Adding = true
		out.Code = ProductCodeFromName(out.Name)
	}
	if out.Kind != KindAnimal {
		// A species narrows an animal's breeds and means nothing on feed or an item; the schema
		// refuses it outright, so it is dropped here rather than carried to a constraint failure.
		out.SpeciesCode = ""
	}
	return out
}

// Statuses an item row may hold. An item is ARCHIVED rather than deleted: sales recorded under it
// keep the word they were sold under, and a deleted row would leave them naming nothing.
const (
	StatusActive   = "active"
	StatusArchived = "archived"
)

// ProductCodeFromName derives a row's stable identity from the name it was created with:
// lowercase, words joined by underscores, anything else dropped. "Sheep tags" -> "sheep_tags".
//
// It is computed ONCE, when the item is added. A later rename leaves it alone, which is the whole
// point: the code is what a sale line is stamped with, so the farm may call an item whatever it
// likes tomorrow without moving yesterday's sales out of their card.
func ProductCodeFromName(name string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteRune('_')
			lastUnderscore = true
		}
	}
	code := strings.Trim(b.String(), "_")
	// The column requires a letter first, so a name of digits alone ("2026") gets a prefix rather
	// than being refused for a reason nobody typing it would understand.
	if code == "" {
		return ""
	}
	if code[0] < 'a' || code[0] > 'z' {
		code = "item_" + code
	}
	if len(code) > 40 {
		code = strings.Trim(code[:40], "_")
	}
	return code
}

// Validate checks one item edit, returning the FIRST failure with the field a person can fix.
func (w ProductWrite) Validate() error {
	if strings.EqualFold(w.Name, ProductMixed) {
		return ErrProductValidation{Field: "name", Reason: "Mixed describes a sale with several items; choose an item name"}
	}
	if w.Name == "" {
		return ErrProductValidation{Field: "name", Reason: "required"}
	}
	if len(w.Name) > MaxProductNameLength {
		return ErrProductValidation{Field: "name", Reason: "too long"}
	}
	if w.Code == "" {
		// A name of punctuation alone ("---") derives no code.
		return ErrProductValidation{Field: "name", Reason: "must contain a letter or a number"}
	}
	if !IsKind(w.Kind) {
		return ErrProductValidation{Field: "kind", Reason: "must be an animal, feed from the store, or another item"}
	}
	if !IsUnit(w.Unit) {
		return ErrProductValidation{Field: "unit", Reason: "must be sold by the kilogram or by number"}
	}
	// FEED FROM THE STORE IS WEIGHED. Selling it takes the quantity off a balance the store keeps
	// in kilograms, so an item counted by the piece would subtract 20 kilograms for 20 bags and
	// the stock the store reports would drift from the store. Refused where it is authored, so a
	// sale never has to guess what the number on the line meant.
	if w.Kind == KindFeed && w.Unit != UnitKg {
		return ErrProductValidation{Field: "unit", Reason: "feed from the store is sold by the kilogram, because its stock is kept in kilograms"}
	}
	// AN ANIMAL MUST SAY WHICH SPECIES IT IS. A sale line asks for a breed and requires one, and
	// the breeds offered are the SPECIES' breeds -- so an animal item naming no species resolves
	// to an empty breed list and can never be sold. It saved cleanly and was unsellable, with
	// nothing on the screen to say why. Refused here instead, at the moment it would be created.
	if w.Kind == KindAnimal && strings.TrimSpace(w.SpeciesCode) == "" {
		return ErrProductValidation{Field: "species_code", Reason: "is needed for an animal, so the sale can offer its breeds"}
	}
	if w.Status != StatusActive && w.Status != StatusArchived {
		return ErrProductValidation{Field: "status", Reason: "must be in use or archived"}
	}
	// SortOrder is not asked for on screen any more: zero means "put it at the end", which the
	// repository resolves. A NEGATIVE one is still nonsense and is refused.
	if w.SortOrder < 0 {
		return ErrProductValidation{Field: "sort_order", Reason: "must not be negative"}
	}
	return nil
}

// UnitWord is the unit in the words a sentence needs: "kilogram", or "item" for a counted thing.
func (p Product) UnitWord() string {
	if p.Unit == UnitNumber {
		return "item"
	}
	return "kilogram"
}
