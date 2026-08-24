package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// idemScopeFeedPurchaseCreate namespaces the record-purchase idempotency keys.
const idemScopeFeedPurchaseCreate = "procurement.feed_purchase.create"

// feedPurchaseNaturalKeyConstraint is the unique index a duplicate load violates. Matched by NAME
// rather than by SQLSTATE 23505 alone: the table carries several checks, and reporting "batch
// already recorded" for an unrelated violation sends an operator hunting for a duplicate that does
// not exist.
const feedPurchaseNaturalKeyConstraint = "feed_purchases_natural_uq"

// feedPurchaseColumns is the single projection every purchase read uses.
//
// Column order here and in scanFeedPurchase must move together. pgx fails loudly on a count
// mismatch but silently mis-assigns two same-typed columns that are swapped, so any edit to one
// must be mirrored in the other.
const feedPurchaseColumns = `
	p.feed_purchase_id, p.tenant_id, p.purchase_date, p.farm_label, p.feed_item_label,
	p.batch_no, p.quantity_kg,
	p.feed_cost, p.transport_cost, p.loading_cost, p.unloading_cost, p.total_cost, p.per_kg_cost,
	p.vendor, p.payment_released, p.payment_status,
	p.entry_source, p.recorded_by, p.created_at`

// scanFeedPurchase reads one row of feedPurchaseColumns, in that exact order.
func scanFeedPurchase(row pgx.Row) (domain.FeedPurchase, error) {
	var (
		p            domain.FeedPurchase
		purchaseDate time.Time
		createdAt    time.Time
	)
	err := row.Scan(
		&p.FeedPurchaseID, &p.TenantID, &purchaseDate, &p.FarmLabel, &p.FeedItemLabel,
		&p.BatchNo, &p.QuantityKg,
		&p.FeedCost, &p.TransportCost, &p.LoadingCost, &p.UnloadingCost, &p.TotalCost, &p.PerKgCost,
		&p.Vendor, &p.PaymentReleased, &p.PaymentStatus,
		&p.EntrySource, &p.RecordedBy, &createdAt,
	)
	if err != nil {
		return domain.FeedPurchase{}, err
	}
	// The purchase date is a business DATE: formatted as its calendar day, never shifted through a
	// timezone conversion.
	p.PurchaseDate = purchaseDate.Format("2006-01-02")
	p.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return p, nil
}

// buildFeedPurchaseFilter renders the shared WHERE clause for the page read and its whole-filter
// totals, so the rows and the header figures can never range over different predicate sets.
func buildFeedPurchaseFilter(tenantID, farm string) (string, []any) {
	if farm == "" {
		return "p.tenant_id = $1", []any{tenantID}
	}
	return "p.tenant_id = $1 AND p.farm_label = $2", []any{tenantID, farm}
}

// ListFeedPurchases returns one ledger page plus the whole-filter total, quantity and spend.
//
// projection-review: membership=feed_purchases at its (tenant_id, farm_label, feed_item_key,
// batch_no) natural key -- one row per purchased load, with nothing joined to it; group_key=none, the
// page and its three totals range over the SAME predicate produced by one buildFeedPurchaseFilter
// call, so numerator (page rows) and denominator (total) are the same key set by construction;
// join_cardinality=n/a, nothing is joined here; pagination=bounded LIMIT/OFFSET with the totals
// computed whole-filter, never page-local; scope=tenant_id on every branch.
func (r *Repository) ListFeedPurchases(ctx context.Context, tenantID, farm string, limit, offset int) (ports.FeedPurchasePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit = domain.ClampFeedPurchasePageSize(limit)
	if offset < 0 {
		offset = 0
	}
	where, args := buildFeedPurchaseFilter(tenantID, farm)

	// scale-guard:ignore: bounded LIMIT/OFFSET over an authored commercial ledger, not a herd-sized
	// table. The ledger grows with the number of feed LOADS the farm buys (a few hundred rows of
	// sheet history, a handful a week), never with animal count, and the service rejects an offset
	// beyond domain.MaxFeedPurchaseOffset, so the skipped-row cost is bounded by construction. Same
	// shape and reasoning as the sales ledger and the vendor register.
	query := fmt.Sprintf(`SELECT %s FROM public.feed_purchases p WHERE %s ORDER BY p.purchase_date DESC, p.feed_purchase_id LIMIT %d OFFSET %d`, // scale-guard:ignore: bounded authored ledger pagination; see note above
		feedPurchaseColumns, where, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ports.FeedPurchasePage{}, fmt.Errorf("list feed purchases: %w", err)
	}
	defer rows.Close()

	purchases := make([]domain.FeedPurchase, 0, limit)
	for rows.Next() {
		p, err := scanFeedPurchase(rows)
		if err != nil {
			return ports.FeedPurchasePage{}, fmt.Errorf("list feed purchases scan: %w", err)
		}
		purchases = append(purchases, p)
	}
	if err := rows.Err(); err != nil {
		return ports.FeedPurchasePage{}, fmt.Errorf("list feed purchases rows: %w", err)
	}

	page := ports.FeedPurchasePage{Purchases: purchases}
	totalsQuery := fmt.Sprintf(`
SELECT count(*), COALESCE(sum(p.quantity_kg), 0), COALESCE(sum(p.total_cost), 0)
FROM public.feed_purchases p WHERE %s`, where)
	if err := r.pool.QueryRow(ctx, totalsQuery, args...).Scan(&page.Total, &page.QuantityKg, &page.SpendRupees); err != nil {
		return ports.FeedPurchasePage{}, fmt.Errorf("count feed purchases: %w", err)
	}
	return page, nil
}

// FeedPurchaseOptions returns the entry form's backend-owned vocabularies.
//
// The feed list is the ACTIVE catalog, which is exactly the set the write path accepts -- the form
// therefore cannot offer a feed whose submit would be refused.
func (r *Repository) FeedPurchaseOptions(ctx context.Context, tenantID string) (ports.FeedPurchaseOptions, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	opts := ports.FeedPurchaseOptions{
		Farms:           append([]string(nil), domain.FeedFarms...),
		PaymentStatuses: append([]string(nil), domain.FeedPaymentStatuses...),
	}

	rows, err := r.pool.Query(ctx, `
SELECT feed_item_key, feed_item_label
FROM public.feed_item_catalog
WHERE tenant_id = $1 AND status = 'active'
ORDER BY display_order, feed_item_label`, tenantID)
	if err != nil {
		return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options catalog: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ports.FeedItemOption
		if err := rows.Scan(&item.Key, &item.Label); err != nil {
			return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options catalog scan: %w", err)
		}
		opts.FeedItems = append(opts.FeedItems, item)
	}
	if err := rows.Err(); err != nil {
		return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options catalog rows: %w", err)
	}

	// Vendor suggestions: the suppliers this tenant has actually bought feed from, most recent
	// first. Bounded by LIMIT because it feeds a datalist, not a page.
	vendorRows, err := r.pool.Query(ctx, `
SELECT vendor
FROM public.feed_purchases
WHERE tenant_id = $1 AND btrim(vendor) <> ''
GROUP BY vendor
ORDER BY max(purchase_date) DESC, vendor
LIMIT 100`, tenantID)
	if err != nil {
		return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options vendors: %w", err)
	}
	defer vendorRows.Close()
	for vendorRows.Next() {
		var vendor string
		if err := vendorRows.Scan(&vendor); err != nil {
			return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options vendors scan: %w", err)
		}
		opts.Vendors = append(opts.Vendors, vendor)
	}
	if err := vendorRows.Err(); err != nil {
		return ports.FeedPurchaseOptions{}, fmt.Errorf("feed purchase options vendors rows: %w", err)
	}
	return opts, nil
}

// getFeedPurchase reads one purchase inside the caller's tenant. Used by the create replay path.
func (r *Repository) getFeedPurchase(ctx context.Context, tenantID, purchaseID string) (domain.FeedPurchase, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.feed_purchases p WHERE p.tenant_id = $1 AND p.feed_purchase_id = $2`, feedPurchaseColumns)
	p, err := scanFeedPurchase(r.pool.QueryRow(ctx, query, tenantID, purchaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("get feed purchase: %w", err)
	}
	return p, nil
}

// CreateFeedPurchase records one purchased load: idempotency reservation, catalog check, batch
// number assignment, insert and audit in ONE transaction.
//
// Everything that can decide the row's identity happens INSIDE the transaction on purpose. The
// catalog check must not be a read-then-write outside it (a feed retired in between would land a
// purchase the stock cards can never show), and the assigned batch number is taken under the same
// transaction as the insert so two concurrent submits cannot both claim max+1 -- the loser hits
// feed_purchases_natural_uq and is reported as a duplicate rather than silently overwriting.
func (r *Repository) CreateFeedPurchase(ctx context.Context, tenantID string, write domain.FeedPurchaseWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: begin create feed purchase: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Semantic fingerprint over every field that defines the purchase's effect. A replay carrying
	// the same key but ANY different field is a different request and must be refused, not
	// recorded. The batch number is included as "" when absent, so "next load" and "batch 7"
	// fingerprint differently.
	fingerprint := requestFingerprint(
		write.PurchaseDate, write.FarmLabel, write.FeedItemLabel,
		fpInt(write.BatchNo), fpMoney(&write.QuantityKg),
		fpMoney(write.FeedCost), fpMoney(write.TransportCost), fpMoney(write.LoadingCost),
		fpMoney(write.UnloadingCost), fpMoney(write.TotalCost),
		write.Vendor, fpMoney(write.PaymentReleased), write.PaymentStatus,
	)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeFeedPurchaseCreate, idempotencyKey, fingerprint)
	if err != nil {
		return domain.FeedPurchase{}, err
	}
	if !reservation.proceed {
		// Exact replay: commit the (side-effect-free) reservation read and return the original row.
		if err := tx.Commit(ctx); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase replay read: %w", err)
		}
		return r.getFeedPurchase(ctx, tenantID, reservation.resultID)
	}

	// CURRENT-CATALOG FEEDS ONLY (migration 000174, decision 2). Resolve the entered label through
	// feed_config_norm -- the same normalization the ledger's generated feed_item_key uses -- so
	// "Dry  Sorghum forage" and "Dry Sorghum Forage" are the same feed here and in the stock read.
	var catalogLabel string
	err = tx.QueryRow(ctx, `
SELECT feed_item_label
FROM public.feed_item_catalog
WHERE tenant_id = $1 AND feed_item_key = feed_config_norm($2) AND status = 'active'`,
		tenantID, write.FeedItemLabel).Scan(&catalogLabel)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedItemNotInCatalog
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: resolve feed item: %w", err)
	}

	// The CATALOG's label is stored, not the typed one: the ledger's rows must read with one
	// spelling per feed, or the stock cards group a feed against itself.
	batchNo := 0
	if write.BatchNo != nil {
		batchNo = *write.BatchNo
	} else {
		if _, err := tx.Exec(ctx, `
SELECT pg_advisory_xact_lock(hashtext($1::text || ':' || $2 || ':' || feed_config_norm($3))::bigint)`,
			tenantID, write.FarmLabel, catalogLabel); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: lock feed batch counter: %w", err)
		}
		if err := tx.QueryRow(ctx, `
SELECT COALESCE(max(batch_no), 0) + 1
FROM public.feed_purchases
WHERE tenant_id = $1 AND farm_label = $2 AND feed_item_key = feed_config_norm($3)`,
			tenantID, write.FarmLabel, catalogLabel).Scan(&batchNo); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: next feed batch no: %w", err)
		}
	}

	var purchaseID string
	err = tx.QueryRow(ctx, `
INSERT INTO public.feed_purchases (
  tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg,
  feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost,
  consumed_at_import_kg, depletes_from,
  vendor, payment_released, payment_status,
  entry_source, recorded_by, source_ref
) VALUES (
  $1::uuid,
  (SELECT l.location_id FROM public.locations l
    WHERE l.tenant_id = $1::uuid AND l.location_type = 'park' AND upper(l.location_code) = $2 LIMIT 1),
  $2, $3, $4, $5::date, $6,
  $7, $8, $9, $10, $11, $12,
  0, $5::date,
  $13, $14, $15,
  'app', nullif($16, '')::uuid, 'app:procurement-feed-purchases'
)
RETURNING feed_purchase_id::text`,
		tenantID, write.FarmLabel, catalogLabel, batchNo, write.PurchaseDate, write.QuantityKg,
		write.FeedCost, write.TransportCost, write.LoadingCost, write.UnloadingCost,
		write.TotalOrSplitSum(), write.PerKgCost(),
		write.Vendor, write.PaymentReleased, write.PaymentStatus, actorID,
	).Scan(&purchaseID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == feedPurchaseNaturalKeyConstraint {
			return domain.FeedPurchase{}, ports.ErrFeedPurchaseDuplicateBatch
		}
		return domain.FeedPurchase{}, fmt.Errorf("procurement: create feed purchase: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "procurement.feed_purchase.record",
		ResourceType: "feed_purchase",
		ResourceID:   purchaseID,
		Metadata: map[string]any{
			"domain":          "procurement",
			"module":          "feed_purchases",
			"category":        "purchase",
			"farm":            write.FarmLabel,
			"feed_item":       catalogLabel,
			"batch_no":        batchNo,
			"quantity_kg":     write.QuantityKg,
			"purchase_date":   write.PurchaseDate,
			"vendor":          write.Vendor,
			"idempotency_key": idempotencyKey,
			"operation_id":    idempotencyKey,
		},
	}); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase record: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeFeedPurchaseCreate, idempotencyKey, "feed_purchase", purchaseID); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: complete feed purchase idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: commit create feed purchase: %w", err)
	}
	return r.getFeedPurchase(ctx, tenantID, purchaseID)
}

// fpMoney renders an optional number as a stable, nil-safe fingerprint part (empty when absent, so
// "not sent" and "sent as 0" fingerprint differently).
func fpMoney(v *float64) string {
	if v == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", *v), "0"), ".")
}

// fpInt renders an optional batch number the same nil-safe way.
func fpInt(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

var _ ports.FeedPurchaseRepository = (*Repository)(nil)
