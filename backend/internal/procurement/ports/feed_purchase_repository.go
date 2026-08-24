package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/procurement/domain"
)

var (
	// ErrFeedPurchaseNotFound is returned when a purchase id does not resolve inside the caller's
	// tenant. Deliberately indistinguishable from "belongs to another tenant".
	ErrFeedPurchaseNotFound = errors.New("procurement: feed purchase not found")
	// ErrFeedItemNotInCatalog is returned when the entered feed does not resolve to an ACTIVE
	// feed_item_catalog row. This is the CURRENT-CATALOG-FEEDS-ONLY decision from migration 000174
	// enforced on the write path: the importer SKIPS such a feed, and the app REFUSES it, because a
	// feed GoatOS cannot ration has no stock card for the purchase to ever appear on.
	ErrFeedItemNotInCatalog = errors.New("procurement: feed item is not in the feed catalog")
	// ErrFeedPurchaseDuplicateBatch is returned when the write would violate
	// feed_purchases_natural_uq -- the same (farm, feed, batch number) as an existing load.
	ErrFeedPurchaseDuplicateBatch = errors.New("procurement: feed purchase batch already recorded")
)

// FeedPurchasePage is one ledger page plus the whole-filter total.
//
// Total is the count across the ENTIRE filter, never len(Purchases) -- per the operational
// read-model contract a summary is a whole-filter aggregate and pagination changes rows only.
type FeedPurchasePage struct {
	Purchases []domain.FeedPurchase
	Total     int
	// QuantityKg and SpendRupees are whole-filter aggregates over the same predicate as the page,
	// so the header figures cannot drift from the rows the filter selects.
	QuantityKg  float64
	SpendRupees float64
}

// FeedItemOption is one selectable feed in the entry form.
type FeedItemOption struct {
	Key   string
	Label string
}

// FeedPurchaseOptions is the backend-owned vocabulary the entry form renders. The client picks
// from these and composes none of its own -- an unlisted feed is rejected by the write path, so a
// client-invented option would only produce a form that fails on submit.
type FeedPurchaseOptions struct {
	Farms           []string
	FeedItems       []FeedItemOption
	PaymentStatuses []string
	// Vendors the farm has already bought feed from, most recent first. A suggestion list for the
	// vendor field, not a closed vocabulary: the ledger's vendor is free text and a new supplier
	// must be enterable on the first load bought from them.
	Vendors []string
}

// FeedPurchaseRepository is the feed-purchase ledger's persistence boundary.
type FeedPurchaseRepository interface {
	// ListFeedPurchases returns one page (newest purchase date first) plus whole-filter totals.
	ListFeedPurchases(ctx context.Context, tenantID, farm string, limit, offset int) (FeedPurchasePage, error)
	// FeedPurchaseOptions returns the entry form's backend-owned vocabularies.
	FeedPurchaseOptions(ctx context.Context, tenantID string) (FeedPurchaseOptions, error)
	// CreateFeedPurchase records one load: idempotency reservation, catalog check, batch-number
	// assignment, insert and audit in ONE transaction.
	CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error)
}
