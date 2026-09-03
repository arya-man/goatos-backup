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
	// farm and delivery are already-normalized filters; "" means unfiltered on that dimension.
	ListFeedPurchases(ctx context.Context, tenantID, farm, delivery string, limit, offset int) (FeedPurchasePage, error)
	// FeedPurchaseOptions returns the entry form's backend-owned vocabularies.
	FeedPurchaseOptions(ctx context.Context, tenantID string) (FeedPurchaseOptions, error)
	// CreateFeedPurchase records one load: idempotency reservation, catalog check, batch-number
	// assignment, insert and audit in ONE transaction.
	CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error)
	// RecordFeedPurchasePayment records one instalment against one load and, in the SAME
	// transaction, advances the load's running payment_released total and re-derives its payment
	// status from the landed cost. Returns the updated purchase with its instalments.
	RecordFeedPurchasePayment(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchasePaymentWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error)
	// UpdateFeedPurchase edits an already-recorded load's values (date, quantity, costs, vendor)
	// under the purchase row lock, re-deriving the landed total, the per-kg rate and the payment
	// status in the SAME transaction. Naturally idempotent: writing the values a load already has
	// changes nothing and audits nothing.
	UpdateFeedPurchase(ctx context.Context, tenantID, purchaseID string, edit domain.FeedPurchaseEdit, actorID string) (domain.FeedPurchase, error)
	// RecordFeedPurchaseDelivery marks a load reached, or corrects an already-reached load's
	// arrival day and received weight, under the purchase row lock. The FIRST transition from
	// purchased to reached is the moment the load becomes stock (depletes_from follows reached_on)
	// and emits procurement.feed_purchase.reached in the SAME transaction, so the toxin test is
	// born exactly once per load. A later correction moves the figures and emits nothing.
	// Naturally idempotent: writing the values a load already has changes nothing.
	RecordFeedPurchaseDelivery(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchaseDeliveryWrite, actorID string) (domain.FeedPurchase, error)
	// SetFeedPurchasePaymentStatus sets the load's payment status directly (the edit control for a
	// status recorded wrong, or a load settled outside the instalment ledger). status must already
	// be a canonical vocabulary word.
	SetFeedPurchasePaymentStatus(ctx context.Context, tenantID, purchaseID, status, actorID string) (domain.FeedPurchase, error)
}
