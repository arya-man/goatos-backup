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
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

// idemScopeFeedPurchaseCreate namespaces the record-purchase idempotency keys.
const idemScopeFeedPurchaseCreate = "procurement.feed_purchase.create"

// idemScopeFeedPurchasePayment namespaces the record-instalment idempotency keys.
const idemScopeFeedPurchasePayment = "procurement.feed_purchase.payment"

// feedPurchaseNaturalKeyConstraint is the unique index a duplicate load violates. Matched by NAME
// rather than by SQLSTATE 23505 alone: the table carries several checks, and reporting "batch
// already recorded" for an unrelated violation sends an operator hunting for a duplicate that does
// not exist.
const feedPurchaseNaturalKeyConstraint = "feed_purchases_natural_uq"

// feedPurchaseColumns is the single projection every purchase read uses.
//
// The arrival day is projected as COALESCE(reached_on, purchase_date) on a reached row: sheet
// history and the importer's rows are reached with no recorded arrival day, and "arrived on the
// day it was bought" is what the ledger has always counted them as.
//
// Column order here and in scanFeedPurchase must move together. pgx fails loudly on a count
// mismatch but silently mis-assigns two same-typed columns that are swapped, so any edit to one
// must be mirrored in the other.
const feedPurchaseColumns = `
	p.feed_purchase_id, p.tenant_id, p.purchase_date, p.farm_label, p.feed_item_label,
	p.batch_no, p.quantity_kg,
	p.feed_cost, p.transport_cost, p.loading_cost, p.unloading_cost, p.total_cost, p.per_kg_cost,
	p.vendor, p.payment_released, p.payment_status,
	p.delivery_status,
	CASE WHEN p.delivery_status = 'reached' THEN COALESCE(p.reached_on, p.purchase_date) END,
	p.reached_weight_kg, p.reached_by,
	p.entry_source, p.recorded_by, p.created_at`

// scanFeedPurchase reads one row of feedPurchaseColumns, in that exact order.
func scanFeedPurchase(row pgx.Row) (domain.FeedPurchase, error) {
	var (
		p            domain.FeedPurchase
		purchaseDate time.Time
		reachedOn    *time.Time
		createdAt    time.Time
	)
	err := row.Scan(
		&p.FeedPurchaseID, &p.TenantID, &purchaseDate, &p.FarmLabel, &p.FeedItemLabel,
		&p.BatchNo, &p.QuantityKg,
		&p.FeedCost, &p.TransportCost, &p.LoadingCost, &p.UnloadingCost, &p.TotalCost, &p.PerKgCost,
		&p.Vendor, &p.PaymentReleased, &p.PaymentStatus,
		&p.DeliveryStatus, &reachedOn, &p.ReachedWeightKg, &p.ReachedBy,
		&p.EntrySource, &p.RecordedBy, &createdAt,
	)
	if err != nil {
		return domain.FeedPurchase{}, err
	}
	// The purchase date is a business DATE: formatted as its calendar day, never shifted through a
	// timezone conversion. Same for the arrival day.
	p.PurchaseDate = purchaseDate.Format("2006-01-02")
	if reachedOn != nil {
		day := reachedOn.Format("2006-01-02")
		p.ReachedOn = &day
	}
	p.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	return p, nil
}

// buildFeedPurchaseFilter renders the shared WHERE clause for the page read and its whole-filter
// totals, so the rows and the header figures can never range over different predicate sets.
func buildFeedPurchaseFilter(tenantID, farm, delivery string) (string, []any) {
	where := "p.tenant_id = $1"
	args := []any{tenantID}
	if farm != "" {
		args = append(args, farm)
		where += fmt.Sprintf(" AND p.farm_label = $%d", len(args))
	}
	if delivery != "" {
		args = append(args, delivery)
		where += fmt.Sprintf(" AND p.delivery_status = $%d", len(args))
	}
	return where, args
}

// ListFeedPurchases returns one ledger page plus the whole-filter total, quantity and spend.
//
// projection-review: membership=feed_purchases at its (tenant_id, farm_label, feed_item_key,
// batch_no) natural key -- one row per purchased load, with nothing joined to it; group_key=none, the
// page and its three totals range over the SAME predicate produced by one buildFeedPurchaseFilter
// call, so numerator (page rows) and denominator (total) are the same key set by construction;
// join_cardinality=n/a, nothing is joined here; pagination=bounded LIMIT/OFFSET with the totals
// computed whole-filter, never page-local; scope=tenant_id on every branch.
func (r *Repository) ListFeedPurchases(ctx context.Context, tenantID, farm, delivery string, limit, offset int) (ports.FeedPurchasePage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit = domain.ClampFeedPurchasePageSize(limit)
	if offset < 0 {
		offset = 0
	}
	where, args := buildFeedPurchaseFilter(tenantID, farm, delivery)

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

	if err := r.attachFeedPurchasePayments(ctx, tenantID, purchases); err != nil {
		return ports.FeedPurchasePage{}, err
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
	for _, status := range domain.FeedDeliveryStatuses {
		opts.DeliveryStatuses = append(opts.DeliveryStatuses, ports.DeliveryStatusOption{Key: status, Label: domain.FeedDeliveryLabel(status)})
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

// getFeedPurchase reads one purchase inside the caller's tenant, with its instalments. Used by the
// create/payment replay paths and as every write's returned read.
func (r *Repository) getFeedPurchase(ctx context.Context, tenantID, purchaseID string) (domain.FeedPurchase, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.feed_purchases p WHERE p.tenant_id = $1 AND p.feed_purchase_id = $2`, feedPurchaseColumns)
	p, err := scanFeedPurchase(r.pool.QueryRow(ctx, query, tenantID, purchaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("get feed purchase: %w", err)
	}
	purchases := []domain.FeedPurchase{p}
	if err := r.attachFeedPurchasePayments(ctx, tenantID, purchases); err != nil {
		return domain.FeedPurchase{}, err
	}
	return purchases[0], nil
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
		write.ReachedOn, fpMoney(write.ReachedWeightKg),
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

	// DELIVERY STATE (maintainer decision 2026-09-03). A load is recorded as still on the road
	// unless the form carries the day it arrived, in which case it is reached from that day.
	// depletes_from is the first feed day this load can be drawn against: the arrival day when
	// known, else the purchase date as a placeholder the delivery write overwrites -- an
	// in-transit load contributes stock_kg = 0 (generated column), so the placeholder feeds
	// nothing until then.
	deliveryStatus := domain.FeedDeliveryPurchased
	var reachedOn *string
	var reachedBy *string
	depletesFrom := write.PurchaseDate
	if write.IsReached() {
		deliveryStatus = domain.FeedDeliveryReached
		reachedOn = &write.ReachedOn
		depletesFrom = write.ReachedOn
		if actorID != "" {
			reachedBy = &actorID
		}
	}

	var purchaseID, feedItemKey string
	err = tx.QueryRow(ctx, `
INSERT INTO public.feed_purchases (
  tenant_id, park_id, farm_label, feed_item_label, batch_no, purchase_date, quantity_kg,
  feed_cost, transport_cost, loading_cost, unloading_cost, total_cost, per_kg_cost,
  consumed_at_import_kg, depletes_from,
  vendor, payment_released, payment_status,
  delivery_status, reached_on, reached_weight_kg, reached_by,
  entry_source, recorded_by, source_ref
) VALUES (
  $1::uuid,
  (SELECT l.location_id FROM public.locations l
    WHERE l.tenant_id = $1::uuid AND l.location_type = 'park' AND upper(l.location_code) = $2 LIMIT 1),
  $2, $3, $4, $5::date, $6,
  $7, $8, $9, $10, $11, $12,
  0, $17::date,
  $13, $14, $15,
  $18, $19::date, $20, $21::uuid,
  'app', nullif($16, '')::uuid, 'app:procurement-feed-purchases'
)
RETURNING feed_purchase_id::text, feed_item_key`,
		tenantID, write.FarmLabel, catalogLabel, batchNo, write.PurchaseDate, write.QuantityKg,
		write.FeedCost, write.TransportCost, write.LoadingCost, write.UnloadingCost,
		write.TotalOrSplitSum(), write.PerKgCost(),
		write.Vendor, write.PaymentReleased, write.PaymentStatus, actorID,
		depletesFrom, deliveryStatus, reachedOn, write.ReachedWeightKg, reachedBy,
	).Scan(&purchaseID, &feedItemKey)
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
			"delivery_status": deliveryStatus,
			"reached_on":      write.ReachedOn,
			"idempotency_key": idempotencyKey,
			"operation_id":    idempotencyKey,
		},
	}); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase record: %w", err)
	}

	// A load recorded as ALREADY reached becomes stock and owes its aflatoxin test right now;
	// the event rides THIS transaction's outbox so a committed arrival always reaches the toxin
	// consumer. A load still on the road emits nothing until the delivery write flips it.
	if write.IsReached() {
		stockKg := write.QuantityKg
		if write.ReachedWeightKg != nil {
			stockKg = *write.ReachedWeightKg
		}
		if err := emitFeedPurchaseReached(ctx, tx, tenantID, actorID, idempotencyKey, feedPurchaseReachedFacts{
			PurchaseID: purchaseID, FarmLabel: write.FarmLabel, FeedItemKey: feedItemKey,
			FeedItemLabel: catalogLabel, Vendor: write.Vendor, BatchNo: batchNo,
			PurchaseDate: write.PurchaseDate, ReachedOn: write.ReachedOn, StockKg: stockKg,
		}); err != nil {
			return domain.FeedPurchase{}, err
		}
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

// attachFeedPurchasePayments loads the instalments of every purchase on one page in ONE batched
// read (`= ANY`, never a per-row query) and attaches them oldest first.
func (r *Repository) attachFeedPurchasePayments(ctx context.Context, tenantID string, purchases []domain.FeedPurchase) error {
	if len(purchases) == 0 {
		return nil
	}
	ids := make([]string, 0, len(purchases))
	index := make(map[string]int, len(purchases))
	for i, p := range purchases {
		ids = append(ids, p.FeedPurchaseID)
		index[p.FeedPurchaseID] = i
	}
	rows, err := r.pool.Query(ctx, `
SELECT payment_id::text, feed_purchase_id::text, paid_on, amount_rupees, note, created_at
FROM public.feed_purchase_payments
WHERE tenant_id = $1 AND feed_purchase_id = ANY($2::uuid[])
ORDER BY paid_on, created_at`, tenantID, ids)
	if err != nil {
		return fmt.Errorf("list feed purchase payments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			payment   domain.FeedPurchasePayment
			paidOn    time.Time
			createdAt time.Time
		)
		if err := rows.Scan(&payment.PaymentID, &payment.FeedPurchaseID, &paidOn, &payment.AmountRupees, &payment.Note, &createdAt); err != nil {
			return fmt.Errorf("list feed purchase payments scan: %w", err)
		}
		payment.PaidOn = paidOn.Format("2006-01-02")
		payment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if i, ok := index[payment.FeedPurchaseID]; ok {
			purchases[i].Payments = append(purchases[i].Payments, payment)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list feed purchase payments rows: %w", err)
	}
	return nil
}

// RecordFeedPurchasePayment records one instalment against one load.
//
// Everything money-shaped happens in ONE transaction under the purchase's row lock: the instalment
// insert, the running payment_released total, and the payment status the new total implies. The
// row lock is what makes the running total safe -- two concurrent instalments each add their own
// amount to the total the OTHER left, never both to the same stale figure.
func (r *Repository) RecordFeedPurchasePayment(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchasePaymentWrite, actorID, idempotencyKey string) (domain.FeedPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: begin feed purchase payment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Semantic fingerprint over the instalment's whole effect: same key + any different field is a
	// different request and is refused, not recorded.
	fingerprint := requestFingerprint(purchaseID, write.PaidOn, fpMoney(&write.AmountRupees), write.Note)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeFeedPurchasePayment, idempotencyKey, fingerprint)
	if err != nil {
		return domain.FeedPurchase{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase payment replay read: %w", err)
		}
		return r.getFeedPurchase(ctx, tenantID, purchaseID)
	}

	// Lock the purchase row for the whole write. Also the tenant check: an id outside the caller's
	// tenant reads as not found.
	var (
		totalCost       *float64
		paymentReleased *float64
		currentStatus   string
	)
	err = tx.QueryRow(ctx, `
SELECT total_cost, payment_released, payment_status
FROM public.feed_purchases
WHERE tenant_id = $1 AND feed_purchase_id = $2
FOR UPDATE`, tenantID, purchaseID).Scan(&totalCost, &paymentReleased, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: lock feed purchase: %w", err)
	}

	var paymentID string
	err = tx.QueryRow(ctx, `
INSERT INTO public.feed_purchase_payments (tenant_id, feed_purchase_id, paid_on, amount_rupees, note, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::date, $4, $5, nullif($6, '')::uuid)
RETURNING payment_id::text`,
		tenantID, purchaseID, write.PaidOn, write.AmountRupees, write.Note, actorID,
	).Scan(&paymentID)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: insert feed purchase payment: %w", err)
	}

	released := write.AmountRupees
	if paymentReleased != nil {
		released += *paymentReleased
	}
	newStatus := domain.DeriveFeedPaymentStatus(totalCost, released, currentStatus)
	if _, err := tx.Exec(ctx, `
UPDATE public.feed_purchases
SET payment_released = $3, payment_status = $4, updated_at = now()
WHERE tenant_id = $1 AND feed_purchase_id = $2`,
		tenantID, purchaseID, released, newStatus); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: update feed purchase payment total: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "procurement.feed_purchase.payment_record",
		ResourceType: "feed_purchase",
		ResourceID:   purchaseID,
		Metadata: map[string]any{
			"domain":           "procurement",
			"module":           "feed_purchases",
			"category":         "payment",
			"payment_id":       paymentID,
			"paid_on":          write.PaidOn,
			"amount_rupees":    write.AmountRupees,
			"payment_released": released,
			"payment_status":   newStatus,
			"idempotency_key":  idempotencyKey,
			"operation_id":     idempotencyKey,
		},
	}); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase payment: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeFeedPurchasePayment, idempotencyKey, "feed_purchase_payment", paymentID); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: complete feed purchase payment idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase payment: %w", err)
	}
	return r.getFeedPurchase(ctx, tenantID, purchaseID)
}

// SetFeedPurchasePaymentStatus sets the load's payment status directly.
//
// Naturally idempotent, so no reservation: the guarded UPDATE writes (and audits) only when the
// status actually changes, and a retry of the same change finds nothing to do.
func (r *Repository) SetFeedPurchasePaymentStatus(ctx context.Context, tenantID, purchaseID, status, actorID string) (domain.FeedPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: begin feed purchase status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var previous string
	err = tx.QueryRow(ctx, `
SELECT payment_status
FROM public.feed_purchases
WHERE tenant_id = $1 AND feed_purchase_id = $2
FOR UPDATE`, tenantID, purchaseID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: lock feed purchase status: %w", err)
	}

	if previous != status {
		if _, err := tx.Exec(ctx, `
UPDATE public.feed_purchases
SET payment_status = $3, updated_at = now()
WHERE tenant_id = $1 AND feed_purchase_id = $2`, tenantID, purchaseID, status); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: update feed purchase status: %w", err)
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorID:      actorID,
			ActorType:    "human",
			Action:       "procurement.feed_purchase.payment_status_set",
			ResourceType: "feed_purchase",
			ResourceID:   purchaseID,
			Metadata: map[string]any{
				"domain":          "procurement",
				"module":          "feed_purchases",
				"category":        "payment",
				"previous_status": previous,
				"payment_status":  status,
			},
		}); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase status: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase status: %w", err)
	}
	return r.getFeedPurchase(ctx, tenantID, purchaseID)
}

// UpdateFeedPurchase edits an already-recorded load's values.
//
// The row lock covers the whole edit so a concurrent instalment cannot interleave: the payment
// status is re-derived from the NEW landed total against the released total the lock read.
// Naturally idempotent -- writing the values the load already has changes nothing and audits
// nothing -- so no reservation is needed.
func (r *Repository) UpdateFeedPurchase(ctx context.Context, tenantID, purchaseID string, edit domain.FeedPurchaseEdit, actorID string) (domain.FeedPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: begin feed purchase edit: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanFeedPurchase(tx.QueryRow(ctx, fmt.Sprintf(
		`SELECT %s FROM public.feed_purchases p WHERE p.tenant_id = $1 AND p.feed_purchase_id = $2 FOR UPDATE`,
		feedPurchaseColumns), tenantID, purchaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: lock feed purchase edit: %w", err)
	}

	released := 0.0
	if current.PaymentReleased != nil {
		released = *current.PaymentReleased
	}
	newTotal := edit.TotalOrSplitSum()
	newStatus := domain.DeriveFeedPaymentStatus(newTotal, released, current.PaymentStatus)

	unchanged := current.PurchaseDate == edit.PurchaseDate &&
		current.QuantityKg == edit.QuantityKg &&
		eqMoney(current.FeedCost, edit.FeedCost) && eqMoney(current.TransportCost, edit.TransportCost) &&
		eqMoney(current.LoadingCost, edit.LoadingCost) && eqMoney(current.UnloadingCost, edit.UnloadingCost) &&
		eqMoney(current.TotalCost, newTotal) &&
		current.Vendor == edit.Vendor && current.PaymentStatus == newStatus
	if !unchanged {
		if _, err := tx.Exec(ctx, `
UPDATE public.feed_purchases
SET purchase_date = $3::date, quantity_kg = $4,
    feed_cost = $5, transport_cost = $6, loading_cost = $7, unloading_cost = $8,
    total_cost = $9, per_kg_cost = $10,
    vendor = $11, payment_status = $12,
    updated_at = now()
WHERE tenant_id = $1 AND feed_purchase_id = $2`,
			tenantID, purchaseID, edit.PurchaseDate, edit.QuantityKg,
			edit.FeedCost, edit.TransportCost, edit.LoadingCost, edit.UnloadingCost,
			newTotal, edit.PerKgCost(current.ReachedWeightKg), edit.Vendor, newStatus); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: update feed purchase: %w", err)
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorID:      actorID,
			ActorType:    "human",
			Action:       "procurement.feed_purchase.edit",
			ResourceType: "feed_purchase",
			ResourceID:   purchaseID,
			Metadata: map[string]any{
				"domain":            "procurement",
				"module":            "feed_purchases",
				"category":          "purchase",
				"previous_date":     current.PurchaseDate,
				"previous_quantity": current.QuantityKg,
				"previous_total":    current.TotalCost,
				"previous_vendor":   current.Vendor,
				"purchase_date":     edit.PurchaseDate,
				"quantity_kg":       edit.QuantityKg,
				"total_cost":        newTotal,
				"vendor":            edit.Vendor,
				"payment_status":    newStatus,
			},
		}); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase edit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase edit: %w", err)
	}
	return r.getFeedPurchase(ctx, tenantID, purchaseID)
}

// eqMoney compares two optional money values as stored (paisa precision).
func eqMoney(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	diff := *a - *b
	return diff < 0.005 && diff > -0.005
}

// feedPurchaseDeliveryUpdateSQL is the arrival write: the state flip, the arrival day (which
// depletes_from follows, so depletion starts the day the feed was actually there), the received
// weight, and the landed rate re-derived from it. reached_by keeps the FIRST marker.
const feedPurchaseDeliveryUpdateSQL = `
UPDATE public.feed_purchases
SET delivery_status = $3, reached_on = $4::date, reached_weight_kg = $5,
    reached_by = COALESCE(reached_by, nullif($6, '')::uuid),
    depletes_from = $4::date,
    per_kg_cost = $7,
    updated_at = now()
WHERE tenant_id = $1 AND feed_purchase_id = $2
RETURNING feed_item_key`

// RecordFeedPurchaseDelivery marks a load reached, or corrects an already-reached load's arrival
// day and received weight.
//
// Everything that turns a load into stock happens in ONE transaction under the purchase row lock:
// the state flip, depletes_from following the arrival day, the per-kg rate re-derived from the
// received weight, the audit row, and -- on the purchased -> reached transition ONLY -- the outbox
// event that gives the toxin module its test. The row lock is what makes "exactly once" hold: two
// desks marking the same load reached at the same moment serialize, and the second finds it
// already reached and emits nothing. Naturally idempotent, so no reservation: writing the values
// the load already has changes nothing and audits nothing.
func (r *Repository) RecordFeedPurchaseDelivery(ctx context.Context, tenantID, purchaseID string, write domain.FeedPurchaseDeliveryWrite, actorID string) (domain.FeedPurchase, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: begin feed purchase delivery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanFeedPurchase(tx.QueryRow(ctx, fmt.Sprintf(
		`SELECT %s FROM public.feed_purchases p WHERE p.tenant_id = $1 AND p.feed_purchase_id = $2 FOR UPDATE`,
		feedPurchaseColumns), tenantID, purchaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeedPurchase{}, ports.ErrFeedPurchaseNotFound
	}
	if err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: lock feed purchase delivery: %w", err)
	}
	// The arrival is judged against the purchase date the LOCKED row carries, not one the caller
	// remembered: a concurrent re-dating of the purchase cannot slip an arrival in before it.
	if err := write.Validate(current.PurchaseDate, biztime.BusinessDayStart(time.Now())); err != nil {
		return domain.FeedPurchase{}, err
	}

	transition := current.DeliveryStatus != domain.FeedDeliveryReached
	unchanged := !transition &&
		current.ReachedOn != nil && *current.ReachedOn == write.ReachedOn &&
		eqMoney(current.ReachedWeightKg, write.ReachedWeightKg)
	if unchanged {
		if err := tx.Commit(ctx); err != nil {
			return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase delivery noop: %w", err)
		}
		return r.getFeedPurchase(ctx, tenantID, purchaseID)
	}

	var feedItemKey string
	if err := tx.QueryRow(ctx, feedPurchaseDeliveryUpdateSQL,
		tenantID, purchaseID, domain.FeedDeliveryReached, write.ReachedOn, write.ReachedWeightKg, actorID,
		domain.DeriveFeedPerKgCost(current.TotalCost, current.QuantityKg, write.ReachedWeightKg),
	).Scan(&feedItemKey); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: update feed purchase delivery: %w", err)
	}

	action := "procurement.feed_purchase.delivery_update"
	if transition {
		action = "procurement.feed_purchase.reached"
	}
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       action,
		ResourceType: "feed_purchase",
		ResourceID:   purchaseID,
		Metadata: map[string]any{
			"domain":                  "procurement",
			"module":                  "feed_purchases",
			"category":                "delivery",
			"farm":                    current.FarmLabel,
			"feed_item":               current.FeedItemLabel,
			"batch_no":                current.BatchNo,
			"previous_status":         current.DeliveryStatus,
			"previous_reached_on":     current.ReachedOn,
			"previous_reached_weight": current.ReachedWeightKg,
			"reached_on":              write.ReachedOn,
			"reached_weight_kg":       write.ReachedWeightKg,
			"buying_weight_kg":        current.QuantityKg,
		},
	}); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: audit feed purchase delivery: %w", err)
	}

	if transition {
		stockKg := current.QuantityKg
		if write.ReachedWeightKg != nil {
			stockKg = *write.ReachedWeightKg
		}
		// Keyed on the load itself: the transition happens once per load, so the load id IS the
		// operation identity a replayed relay delivery can recognise.
		if err := emitFeedPurchaseReached(ctx, tx, tenantID, actorID, "feed-purchase-reached:"+purchaseID, feedPurchaseReachedFacts{
			PurchaseID: purchaseID, FarmLabel: current.FarmLabel, FeedItemKey: feedItemKey,
			FeedItemLabel: current.FeedItemLabel, Vendor: current.Vendor, BatchNo: current.BatchNo,
			PurchaseDate: current.PurchaseDate, ReachedOn: write.ReachedOn, StockKg: stockKg,
		}); err != nil {
			return domain.FeedPurchase{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.FeedPurchase{}, fmt.Errorf("procurement: commit feed purchase delivery: %w", err)
	}
	return r.getFeedPurchase(ctx, tenantID, purchaseID)
}

var _ ports.FeedPurchaseRepository = (*Repository)(nil)
