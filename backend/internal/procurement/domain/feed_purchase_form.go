package domain

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// THE FEED PURCHASE FORM IS AUTHORED (maintainer decision 2026-09-20: procurement SOP-driven end
// to end, "what questions, what to render, what type of details we need, feed purchase").
//
// What the Record feed purchase screens ask -- which questions, on which page, in which order,
// which are compulsory, and any question the farm adds tomorrow -- is `form_dsl.feed_purchase_form`
// of the published `procurement.feed_purchase_form` SOP. It is the vendor form's engine with a
// different profile: same pages-of-questions shape, same locked-id rule, same catalog-backed
// choices, so there is ONE implementation of those rules rather than two that drift.
//
// TYPED questions are the ledger's own columns (`purchase_date`, `farm_label`, `quantity_kg`, the
// cost split, `payment_status`, ...): their id and kind are LOCKED because the stock cards, the
// landed-rate arithmetic and the aflatoxin task all read them. Everything else is stored on the
// purchase as sop_answers with the version it was answered on.

const (
	// SOPCodeFeedPurchaseForm is the authored entry form's SOP code.
	SOPCodeFeedPurchaseForm = "procurement.feed_purchase_form"
	// FeedPurchaseFormSchemaVersion tags every feed-purchase form document.
	FeedPurchaseFormSchemaVersion = "goatos.sop-feed-purchase-form.v1"

	// Catalog kinds this form's choice questions may read. They are the ledger's own closed
	// vocabularies, filled per request, never constants in a document.
	CatalogKindFarm          = "farm"
	CatalogKindFeedItem      = "feed_item"
	CatalogKindPaymentStatus = "payment_status"
)

// FeedPurchaseCatalogKinds is the closed set of catalogs a feed-purchase question may draw from.
var FeedPurchaseCatalogKinds = []string{CatalogKindFarm, CatalogKindFeedItem, CatalogKindPaymentStatus}

// lockedFeedPurchaseQuestions are the ledger's typed columns: id -> fixed kind (+ catalog for the
// choice ones). The stock cards, the landed rate and the aflatoxin task read these, so their id
// and kind are not the author's to change; their wording, order, page and -- except for the five
// below -- their compulsory flag are.
var lockedFeedPurchaseQuestions = map[string]VendorQuestion{
	"purchase_date":     {Kind: VendorQuestionText},
	"farm_label":        {Kind: VendorQuestionChoice, Catalog: CatalogKindFarm},
	"feed_item_label":   {Kind: VendorQuestionChoice, Catalog: CatalogKindFeedItem},
	"quantity_kg":       {Kind: VendorQuestionNumber},
	"vendor":            {Kind: VendorQuestionText},
	"batch_no":          {Kind: VendorQuestionNumber},
	"feed_cost":         {Kind: VendorQuestionNumber},
	"transport_cost":    {Kind: VendorQuestionNumber},
	"loading_cost":      {Kind: VendorQuestionNumber},
	"unloading_cost":    {Kind: VendorQuestionNumber},
	"total_cost":        {Kind: VendorQuestionNumber},
	"payment_released":  {Kind: VendorQuestionNumber},
	"payment_status":    {Kind: VendorQuestionChoice, Catalog: CatalogKindPaymentStatus},
	"reached_on":        {Kind: VendorQuestionText},
	"reached_weight_kg": {Kind: VendorQuestionNumber},
}

// requiredFeedPurchaseQuestionIDs must be present AND compulsory: a purchase row cannot exist
// without them, because every downstream read (stock, days left, the landed rate, the toxin task)
// is keyed on or divided by one of them.
var requiredFeedPurchaseQuestionIDs = []string{"purchase_date", "farm_label", "feed_item_label", "quantity_kg", "vendor"}

// FeedPurchaseFormProfile is this form's shape, handed to the shared entry-form engine.
func FeedPurchaseFormProfile() EntryFormProfile {
	return EntryFormProfile{
		Section:       "feed_purchase_form",
		SchemaVersion: FeedPurchaseFormSchemaVersion,
		Locked:        lockedFeedPurchaseQuestions,
		RequiredIDs:   requiredFeedPurchaseQuestionIDs,
		CatalogKinds:  FeedPurchaseCatalogKinds,
		Noun:          "a feed purchase",
	}
}

//go:embed feedformseed/feed_purchase.json
var seededFeedPurchaseFormJSON []byte

// SeededFeedPurchaseFormJSON is the day-one document, embedded verbatim in the migration that
// publishes it as v1 (pinned by TestMigrationEmbedsTheSeededFeedPurchaseForm).
func SeededFeedPurchaseFormJSON() []byte {
	return append([]byte(nil), seededFeedPurchaseFormJSON...)
}

// SeededFeedPurchaseFormDSL parses the embedded document.
func SeededFeedPurchaseFormDSL() VendorFormDSL {
	dsl, err := ParseEntryForm(FeedPurchaseFormProfile(), map[string]any{"feed_purchase_form": json.RawMessage(seededFeedPurchaseFormJSON)})
	if err != nil {
		panic("procurement: seeded feed purchase form does not parse: " + err.Error())
	}
	return dsl
}

// ParseFeedPurchaseForm extracts form_dsl.feed_purchase_form.
func ParseFeedPurchaseForm(formDSL map[string]any) (VendorFormDSL, error) {
	return ParseEntryForm(FeedPurchaseFormProfile(), formDSL)
}

// ValidateFeedPurchaseForm names every problem by path.
func ValidateFeedPurchaseForm(dsl VendorFormDSL) []string {
	return ValidateEntryForm(FeedPurchaseFormProfile(), dsl)
}

// IsTypedFeedPurchaseQuestion reports a question the ledger stores in its own column.
func IsTypedFeedPurchaseQuestion(id string) bool {
	_, ok := lockedFeedPurchaseQuestions[id]
	return ok
}

// ApplyFeedPurchaseAnswers maps the typed answers onto the ledger's write and returns everything
// else as the extras stored with the row. An answer that is blank leaves its column alone, so an
// older client sending typed fields only still works exactly as it did.
func ApplyFeedPurchaseAnswers(write FeedPurchaseWrite, answers map[string]string) (FeedPurchaseWrite, map[string]string) {
	extras := map[string]string{}
	num := func(raw string) *float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			// exception:exempt a non-numeric answer is REFUSED earlier by ValidateVendorAnswers
			// against the form the client rendered; reaching here means the caller skipped that
			// check, and the honest answer is "no value", not a column filled from nonsense.
			return nil
		}
		return &v
	}
	for id, raw := range answers {
		value := strings.TrimSpace(raw)
		if !IsTypedFeedPurchaseQuestion(id) {
			if value != "" {
				extras[id] = value
			}
			continue
		}
		if value == "" {
			continue
		}
		switch id {
		case "purchase_date":
			write.PurchaseDate = value
		case "farm_label":
			write.FarmLabel = value
		case "feed_item_label":
			write.FeedItemLabel = value
		case "vendor":
			write.Vendor = value
		case "payment_status":
			write.PaymentStatus = value
		case "reached_on":
			write.ReachedOn = value
		case "quantity_kg":
			if n := num(value); n != nil {
				write.QuantityKg = *n
			}
		case "batch_no":
			if n := num(value); n != nil {
				b := int(*n)
				write.BatchNo = &b
			}
		case "feed_cost":
			write.FeedCost = num(value)
		case "transport_cost":
			write.TransportCost = num(value)
		case "loading_cost":
			write.LoadingCost = num(value)
		case "unloading_cost":
			write.UnloadingCost = num(value)
		case "total_cost":
			write.TotalCost = num(value)
		case "payment_released":
			write.PaymentReleased = num(value)
		case "reached_weight_kg":
			write.ReachedWeightKg = num(value)
		}
	}
	return write, extras
}

// FeedPurchaseFormCatalog builds the choice rows this form's catalog-backed questions read, from
// the ledger's OWN vocabularies -- the farms it buys for, the ACTIVE feed catalog it can ration,
// and the two payment states the sheet has always carried. Nothing here is a constant list in a
// document: a feed retired from the catalog stops being offered the moment it is retired.
func FeedPurchaseFormCatalog(farms []string, feedItems []string, paymentStatuses []string) []VendorCatalogEntry {
	out := make([]VendorCatalogEntry, 0, len(farms)+len(feedItems)+len(paymentStatuses))
	for i, f := range farms {
		out = append(out, VendorCatalogEntry{Kind: CatalogKindFarm, Value: f, Label: f, SortOrder: i, IsActive: true})
	}
	for i, f := range feedItems {
		out = append(out, VendorCatalogEntry{Kind: CatalogKindFeedItem, Value: f, Label: f, SortOrder: i, IsActive: true})
	}
	for i, p := range paymentStatuses {
		out = append(out, VendorCatalogEntry{Kind: CatalogKindPaymentStatus, Value: p, Label: p, SortOrder: i, IsActive: true})
	}
	return out
}

// feedPurchaseFormLabel is used by problem text only.
var _ = fmt.Sprintf
