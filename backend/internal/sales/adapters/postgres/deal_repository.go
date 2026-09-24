package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// idemScopeDealCreate namespaces the record-sale idempotency keys.
const idemScopeDealCreate = "sales.deal.create"

// dealColumns is the single projection every deal read uses.
//
// Column order here and in scanDeal must move together. pgx fails loudly on a count mismatch but
// silently mis-assigns two same-typed columns that are swapped, so any edit to one must be
// mirrored in the other.
const dealColumns = `
		d.id, d.tenant_id, d.sale_date, d.farm,
		d.source_sales_id, d.source_purchase_id, d.source_row_no,
		d.buyer_name, d.buyer_place, d.buyer_vendor_id, d.product_type, d.breed,
		d.animal_count, d.male_count, d.female_count, d.total_weight_kg,
		d.advance_amount, d.sales_value, d.payment_received, d.status, d.feedback, d.comments,
		d.created_at, d.updated_at`

const dealPaymentsForPageSQL = `
	SELECT payment_id::text, deal_id::text, received_on, amount_rupees, note, created_at
	FROM public.sales_deal_payments
	WHERE tenant_id = $1 AND deal_id = ANY($2::uuid[])
	ORDER BY received_on, created_at`

// EVERY COLUMN THE WRITE STAMPS IS READ BACK. The line carries what it was sold AS since 000402 --
// the product's code and kind, and for a per-unit item its quantity, unit and rate -- and leaving
// those out of this SELECT did not fail anywhere: the row was written correctly, and every reader
// got a line whose kind was blank and whose quantity was nil. A feed sale then folded into the
// `other` bucket at zero kilograms, so the feature wrote perfect rows and reported nothing.
const dealLinesForPageSQL = `
	SELECT line_id::text, deal_id::text, line_no, product_type,
	       coalesce(product_code, ''), coalesce(product_kind, ''), breed,
	       quantity, coalesce(unit, ''), rate_per_unit,
	       animal_count, male_count, female_count, total_weight_kg, sales_value,
	       estimated_weight_kg, coalesce(estimated_weight_band, ''), coalesce(weight_estimate_basis, '')
	FROM public.sales_deal_lines
	WHERE tenant_id = $1 AND deal_id = ANY($2::uuid[])
	ORDER BY deal_id, line_no`

// scanDeal reads one row of dealColumns, in that exact order.
func scanDeal(row pgx.Row) (domain.Deal, error) {
	var (
		d          domain.Deal
		saleDate   time.Time
		srcSales   *int32
		srcPur     *int32
		srcRow     *int32
		createdAt  time.Time
		updatedAt  time.Time
		salesValue float64
	)
	err := row.Scan(
		&d.DealID, &d.TenantID, &saleDate, &d.Farm,
		&srcSales, &srcPur, &srcRow,
		&d.BuyerName, &d.BuyerPlace, &d.BuyerVendorID, &d.ProductType, &d.Breed,
		&d.AnimalCount, &d.MaleCount, &d.FemaleCount, &d.TotalWeightKg,
		&d.AdvanceAmount, &salesValue, &d.PaymentReceived, &d.Status, &d.Feedback, &d.Comments,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return domain.Deal{}, err
	}
	// The sale date is a business DATE: formatted as its calendar day, never shifted through a
	// timezone conversion.
	d.SaleDate = saleDate.Format("2006-01-02")
	d.SalesValue = salesValue
	d.SourceSalesID = int32Ptr(srcSales)
	d.SourcePurchaseID = int32Ptr(srcPur)
	d.SourceRowNo = int32Ptr(srcRow)
	d.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	d.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return d, nil
}

func int32Ptr(v *int32) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// buildDealFilter renders the shared WHERE clause for the page read, its total, and the overview's
// deal read, so the three can never range over different predicate sets.
func buildDealFilter(tenantID, farm string) (string, []any) {
	if farm == "" {
		return "d.tenant_id = $1", []any{tenantID}
	}
	return "d.tenant_id = $1 AND d.farm = $2", []any{tenantID, farm}
}

// ListDeals returns one ledger page (all statuses) plus the whole-filter total.
func (r *Repository) ListDeals(ctx context.Context, tenantID, farm string, limit, offset int) (ports.DealPage, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	limit = domain.ClampDealPageSize(limit)
	if offset < 0 {
		offset = 0
	}
	where, args := buildDealFilter(tenantID, farm)

	// scale-guard:ignore: bounded LIMIT/OFFSET over an authored commercial ledger, not a herd-sized
	// table. The ledger grows with the number of DEALS the farm closes (63 sheet rows today, a
	// handful per month), never with animal count, and the service rejects offset beyond
	// domain.MaxDealOffset, so the skipped-row cost is bounded by construction. Same reasoning and
	// shape as the procurement vendor register.
	query := fmt.Sprintf(`SELECT %s FROM public.sales_deals d WHERE %s ORDER BY d.sale_date DESC, d.id LIMIT %d OFFSET %d`, // scale-guard:ignore: bounded authored ledger pagination; see note above
		dealColumns, where, limit, offset)

	boundList := sqlbind.MustBind(query, args...)
	rows, err := r.pool.Query(ctx, boundList.SQL(), boundList.Args()...)
	if err != nil {
		return ports.DealPage{}, fmt.Errorf("list sales deals: %w", err)
	}
	defer rows.Close()

	deals := make([]domain.Deal, 0, limit)
	for rows.Next() {
		d, err := scanDeal(rows)
		if err != nil {
			return ports.DealPage{}, fmt.Errorf("list sales deals scan: %w", err)
		}
		deals = append(deals, d)
	}
	if err := rows.Err(); err != nil {
		return ports.DealPage{}, fmt.Errorf("list sales deals rows: %w", err)
	}

	if err := r.attachDealPayments(ctx, tenantID, deals); err != nil {
		return ports.DealPage{}, err
	}
	if err := r.attachDealLines(ctx, tenantID, deals); err != nil {
		return ports.DealPage{}, err
	}

	page := ports.DealPage{Deals: deals}
	// Whole-filter total over the SAME predicates, built from the same buildDealFilter call so
	// the list and its total cannot drift.
	countQuery := fmt.Sprintf(`SELECT count(*) FROM public.sales_deals d WHERE %s`, where)
	boundCount := sqlbind.MustBind(countQuery, args...)
	if err := r.pool.QueryRow(ctx, boundCount.SQL(), boundCount.Args()...).Scan(&page.Total); err != nil {
		return ports.DealPage{}, fmt.Errorf("count sales deals: %w", err)
	}
	return page, nil
}

// getDeal reads one deal inside the caller's tenant. Used by the create replay path.
func (r *Repository) getDeal(ctx context.Context, tenantID, dealID string) (domain.Deal, error) {
	query := fmt.Sprintf(`SELECT %s FROM public.sales_deals d WHERE d.tenant_id = $1 AND d.id = $2`, dealColumns)
	boundDeal := sqlbind.MustBind(query, tenantID, dealID)
	d, err := scanDeal(r.pool.QueryRow(ctx, boundDeal.SQL(), boundDeal.Args()...))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("get sales deal: %w", err)
	}
	deals := []domain.Deal{d}
	if err := r.attachDealPayments(ctx, tenantID, deals); err != nil {
		return domain.Deal{}, err
	}
	if err := r.attachDealLines(ctx, tenantID, deals); err != nil {
		return domain.Deal{}, err
	}
	return deals[0], nil
}

// attachDealPayments loads the receipts of every deal on one page in ONE batched read (`= ANY`,
// never a per-row query) and attaches them oldest first.
func (r *Repository) attachDealPayments(ctx context.Context, tenantID string, deals []domain.Deal) error {
	if len(deals) == 0 {
		return nil
	}
	ids := make([]string, 0, len(deals))
	index := make(map[string]int, len(deals))
	for i, d := range deals {
		ids = append(ids, d.DealID)
		index[d.DealID] = i
	}
	rows, err := r.pool.Query(ctx, dealPaymentsForPageSQL, tenantID, ids)
	if err != nil {
		return fmt.Errorf("list sales deal payments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			payment    domain.DealPayment
			receivedOn time.Time
			createdAt  time.Time
		)
		if err := rows.Scan(&payment.PaymentID, &payment.DealID, &receivedOn, &payment.AmountRupees, &payment.Note, &createdAt); err != nil {
			return fmt.Errorf("list sales deal payments scan: %w", err)
		}
		payment.ReceivedOn = receivedOn.Format("2006-01-02")
		payment.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		if i, ok := index[payment.DealID]; ok {
			deals[i].Payments = append(deals[i].Payments, payment)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list sales deal payments rows: %w", err)
	}
	return nil
}

// attachDealLines loads the product/breed lines of every deal on one page in ONE batched read
// (`= ANY`, never a per-row query) and attaches them in entry order. Migration 000296 backfilled
// one line per pre-existing deal, so every deal comes back with at least one.
func (r *Repository) attachDealLines(ctx context.Context, tenantID string, deals []domain.Deal) error {
	if len(deals) == 0 {
		return nil
	}
	ids := make([]string, 0, len(deals))
	index := make(map[string]int, len(deals))
	for i, d := range deals {
		ids = append(ids, d.DealID)
		index[d.DealID] = i
	}
	rows, err := r.pool.Query(ctx, dealLinesForPageSQL, tenantID, ids)
	if err != nil {
		return fmt.Errorf("list sales deal lines: %w", err)
	}
	defer rows.Close()
	return attachDealLineRows(rows, deals, index, "list sales deal lines")
}

// attachDealLineRows scans rows of the dealLines*SQL projection and hangs each line off its own
// deal (by id), in row order. A line whose deal is not in `index` is skipped.
func attachDealLineRows(rows pgx.Rows, deals []domain.Deal, index map[string]int, label string) error {
	for rows.Next() {
		var (
			line   domain.DealLine
			dealID string
		)
		if err := rows.Scan(&line.LineID, &dealID, &line.LineNo, &line.ProductType,
			&line.ProductCode, &line.ProductKind, &line.Breed,
			&line.Quantity, &line.Unit, &line.RatePerUnit,
			&line.AnimalCount, &line.MaleCount, &line.FemaleCount, &line.TotalWeightKg, &line.SalesValue,
			&line.EstimatedWeightKg, &line.EstimatedWeightBand, &line.WeightEstimateBasis); err != nil {
			return fmt.Errorf("%s scan: %w", label, err)
		}
		if i, ok := index[dealID]; ok {
			deals[i].Lines = append(deals[i].Lines, line)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%s rows: %w", label, err)
	}
	return nil
}

// SetDealStatus sets a deal's lifecycle status directly -- the edit that closes an expected sale
// on the day the animals actually leave, or marks one failed.
//
// Naturally idempotent, so no reservation: the guarded UPDATE writes (and audits) only when the
// status actually changes, and a retry of the same change finds nothing to do.
func (r *Repository) SetDealStatus(ctx context.Context, tenantID, dealID, status, actorID string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin deal status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var previous string
	err = tx.QueryRow(ctx, `
SELECT status
FROM public.sales_deals
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`, tenantID, dealID).Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal status: %w", err)
	}

	if previous != status {
		if _, err := tx.Exec(ctx, `
UPDATE public.sales_deals
SET status = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2`, tenantID, dealID, status); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: update deal status: %w", err)
		}
		// Closing a sale takes its feed off the store; failing or reopening one gives it back.
		if err := syncFeedSaleDepletions(ctx, tx, tenantID, dealID); err != nil {
			return domain.Deal{}, err
		}
		if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
			TenantID:     tenantID,
			ActorID:      actorID,
			ActorType:    "human",
			Action:       "sales.deal.status_set",
			ResourceType: "sales_deal",
			ResourceID:   dealID,
			Metadata: map[string]any{
				"domain":          "sales",
				"module":          "sales_deals",
				"category":        "deal",
				"previous_status": previous,
				"status":          status,
			},
		}); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: audit deal status: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit deal status: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// idemScopeDealPayment namespaces the record-receipt idempotency keys.
const idemScopeDealPayment = "sales.deal.payment"

const (
	// idemScopeDealPaymentUpdate namespaces receipt edits. It is separate from add-payment so a
	// retry of an edit cannot collide with the original insert key.
	idemScopeDealPaymentUpdate = "sales.deal.payment.update"
	// idemScopeDealPaymentDelete namespaces receipt removals.
	idemScopeDealPaymentDelete = "sales.deal.payment.delete"
)

// RecordDealPayment records one receipt against one deal.
//
// Same shape as the feed-purchase instalment write: everything money-shaped happens in ONE
// transaction under the deal's row lock -- the receipt insert and the running payment_received
// total -- so two concurrent receipts each add their own amount to the total the OTHER left.
// Deal status is deliberately NOT derived from money: the lifecycle stays a human decision.
func (r *Repository) RecordDealPayment(ctx context.Context, tenantID, dealID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin deal payment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(dealID, write.ReceivedOn, fmt.Sprintf("%.2f", write.AmountRupees), write.Note)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealPayment, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit deal payment replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, dealID)
	}

	var received *float64
	err = tx.QueryRow(ctx, `
SELECT payment_received
FROM public.sales_deals
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`, tenantID, dealID).Scan(&received)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal payment: %w", err)
	}

	var paymentID string
	err = tx.QueryRow(ctx, `
INSERT INTO public.sales_deal_payments (tenant_id, deal_id, received_on, amount_rupees, note, recorded_by)
VALUES ($1::uuid, $2::uuid, $3::date, $4, $5, nullif($6, '')::uuid)
RETURNING payment_id::text`,
		tenantID, dealID, write.ReceivedOn, write.AmountRupees, write.Note, actorID,
	).Scan(&paymentID)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: insert deal payment: %w", err)
	}

	total := write.AmountRupees
	if received != nil {
		total += *received
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.sales_deals
SET payment_received = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2`, tenantID, dealID, total); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: update deal payment total: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.payment_record",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":           "sales",
			"module":           "sales_deals",
			"category":         "payment",
			"payment_id":       paymentID,
			"received_on":      write.ReceivedOn,
			"amount_rupees":    write.AmountRupees,
			"payment_received": total,
			"idempotency_key":  idempotencyKey,
			"operation_id":     idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit deal payment: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealPayment, idempotencyKey, "sales_deal_payment", paymentID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete deal payment idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit deal payment: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// UpdateDealPayment edits one receipt and applies only the old/new amount delta to the deal's
// running total.
func (r *Repository) UpdateDealPayment(ctx context.Context, tenantID, dealID, paymentID string, write domain.DealPaymentWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin deal payment update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(dealID, paymentID, write.ReceivedOn, fmt.Sprintf("%.2f", write.AmountRupees), write.Note)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealPaymentUpdate, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit deal payment update replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, dealID)
	}

	var received *float64
	var oldAmount float64
	var oldReceivedOn time.Time
	var oldNote string
	err = tx.QueryRow(ctx, `
SELECT d.payment_received, p.amount_rupees, p.received_on, p.note
FROM public.sales_deals d
JOIN public.sales_deal_payments p
  ON p.tenant_id = d.tenant_id AND p.deal_id = d.id
WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid AND p.payment_id = $3::uuid
FOR UPDATE OF d, p`, tenantID, dealID, paymentID).Scan(&received, &oldAmount, &oldReceivedOn, &oldNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealPaymentNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal payment update: %w", err)
	}

	if _, err := tx.Exec(ctx, `
UPDATE public.sales_deal_payments
SET received_on = $4::date, amount_rupees = $5, note = $6
WHERE tenant_id = $1::uuid AND deal_id = $2::uuid AND payment_id = $3::uuid`,
		tenantID, dealID, paymentID, write.ReceivedOn, write.AmountRupees, write.Note); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: update deal payment row: %w", err)
	}

	total := write.AmountRupees - oldAmount
	if received != nil {
		total += *received
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.sales_deals
SET payment_received = $3, updated_at = now()
WHERE tenant_id = $1::uuid AND id = $2::uuid`, tenantID, dealID, total); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: update deal payment total after edit: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.payment_update",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":                 "sales",
			"module":                 "sales_deals",
			"category":               "payment",
			"payment_id":             paymentID,
			"previous_received_on":   oldReceivedOn.Format("2006-01-02"),
			"previous_amount_rupees": oldAmount,
			"previous_note":          oldNote,
			"received_on":            write.ReceivedOn,
			"amount_rupees":          write.AmountRupees,
			"note":                   write.Note,
			"payment_received":       total,
			"idempotency_key":        idempotencyKey,
			"operation_id":           idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit deal payment update: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealPaymentUpdate, idempotencyKey, "sales_deal", dealID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete deal payment update idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit deal payment update: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// DeleteDealPayment removes one receipt and subtracts exactly that receipt's amount from the deal's
// running total.
func (r *Repository) DeleteDealPayment(ctx context.Context, tenantID, dealID, paymentID string, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin deal payment delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(dealID, paymentID)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealPaymentDelete, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit deal payment delete replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, dealID)
	}

	var received *float64
	var oldAmount float64
	var oldReceivedOn time.Time
	var oldNote string
	err = tx.QueryRow(ctx, `
SELECT d.payment_received, p.amount_rupees, p.received_on, p.note
FROM public.sales_deals d
JOIN public.sales_deal_payments p
  ON p.tenant_id = d.tenant_id AND p.deal_id = d.id
WHERE d.tenant_id = $1::uuid AND d.id = $2::uuid AND p.payment_id = $3::uuid
FOR UPDATE OF d, p`, tenantID, dealID, paymentID).Scan(&received, &oldAmount, &oldReceivedOn, &oldNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealPaymentNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal payment delete: %w", err)
	}

	if _, err := tx.Exec(ctx, `
DELETE FROM public.sales_deal_payments
WHERE tenant_id = $1::uuid AND deal_id = $2::uuid AND payment_id = $3::uuid`,
		tenantID, dealID, paymentID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: delete deal payment row: %w", err)
	}

	total := -oldAmount
	if received != nil {
		total += *received
	}
	if total < 0 {
		total = 0
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.sales_deals
SET payment_received = $3, updated_at = now()
WHERE tenant_id = $1::uuid AND id = $2::uuid`, tenantID, dealID, total); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: update deal payment total after delete: %w", err)
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.payment_delete",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":                "sales",
			"module":                "sales_deals",
			"category":              "payment",
			"payment_id":            paymentID,
			"removed_received_on":   oldReceivedOn.Format("2006-01-02"),
			"removed_amount_rupees": oldAmount,
			"removed_note":          oldNote,
			"payment_received":      total,
			"idempotency_key":       idempotencyKey,
			"operation_id":          idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit deal payment delete: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealPaymentDelete, idempotencyKey, "sales_deal", dealID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete deal payment delete idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit deal payment delete: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// CreateDeal records a sale: idempotency reservation, insert, and audit in ONE transaction.
//
// The caller has normalized and validated the write; the enums the CHECK constraints enforce were
// already rejected with field-specific messages at the domain layer.
func (r *Repository) CreateDeal(ctx context.Context, tenantID string, write domain.DealWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin create deal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Semantic fingerprint over every field that defines the sale's effect. A replay carrying the
	// same key but ANY different field is a different request and must be refused, not recorded.
	fingerprint := requestFingerprint(
		write.SaleDate, write.Farm, write.ProductType, write.Breed,
		write.BuyerName, write.BuyerPlace, write.BuyerVendorID,
		fpFloat(write.AnimalCount), fpFloat(write.MaleCount), fpFloat(write.FemaleCount),
		fpFloat(write.TotalWeightKg), fmt.Sprintf("%.4f", write.SalesValue), fpFloat(write.AdvanceAmount),
		write.Comments, write.Status,
		fpLines(write.Lines),
	)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealCreate, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		// Exact replay: commit the (side-effect-free) reservation read and return the original row.
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, reservation.resultID)
	}

	// The registry is re-read UNDER this transaction (migration 000402). The service already
	// validated against it, but a product archived in between would otherwise be recorded as sold
	// -- and, for a feed product, would draw stock the farm has stopped selling.
	if err := confirmProductsStillSellable(ctx, tx, tenantID, write.Lines); err != nil {
		return domain.Deal{}, err
	}

	// buyer_vendor_id goes through nullif(btrim(...)) like the other optional text, NOT a bare
	// $6::uuid: an empty string is not a uuid and Postgres rejects it outright (22P02), so a blank
	// would 500 instead of storing the NULL the column exists to hold for pre-register history.
	// The "every app-recorded sale names a vendor" rule is enforced in domain.DealWrite.Validate,
	// which is where a refusal can name the field and reach the operator.
	var dealID string
	err = tx.QueryRow(ctx, `
		INSERT INTO public.sales_deals (
			tenant_id, sale_date, farm, buyer_name, buyer_place, buyer_vendor_id,
			product_type, breed, animal_count, male_count, female_count,
			total_weight_kg, advance_amount, sales_value, comments,
			payment_received, status
		) VALUES (
			$1, $2::date, $3, $4, nullif(btrim($5), ''), nullif(btrim($6), '')::uuid,
			$7, $8, $9, $10, $11,
			$12, $13, $14, nullif(btrim($15), ''),
			-- The advance IS money received: seed the running total the receipts ledger advances,
			-- exactly as migration 000227 seeded sheet history, so a fresh deal's balance is honest.
			$13,
			-- Blank means the sheet's default: a recorded sale is a closed deal unless the desk
			-- says otherwise (an EXPECTED sale with an advance is 'Advance Paid').
			COALESCE(nullif($16, ''), 'Deal Closed')
		)
		RETURNING id::text`,
		tenantID, write.SaleDate, write.Farm, write.BuyerName, write.BuyerPlace, write.BuyerVendorID,
		write.ProductType, write.Breed, write.AnimalCount, write.MaleCount, write.FemaleCount,
		write.TotalWeightKg, write.AdvanceAmount, write.SalesValue, write.Comments, write.Status,
	).Scan(&dealID)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: create deal: %w", err)
	}

	// The lines land in the SAME transaction as the deal row they roll up into (migration 000296):
	// one set-based insert over UNNEST, never a per-line round trip.
	if _, err := insertDealLines(ctx, tx, tenantID, dealID, write.Lines); err != nil {
		return domain.Deal{}, err
	}

	// Feed sold off the store leaves it here, in this transaction -- if the sale is closed.
	if err := syncFeedSaleDepletions(ctx, tx, tenantID, dealID); err != nil {
		return domain.Deal{}, err
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.record",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":          "sales",
			"module":          "sales",
			"category":        "deal",
			"farm":            write.Farm,
			"product_type":    write.ProductType,
			"line_count":      len(write.Lines),
			"sale_date":       write.SaleDate,
			"idempotency_key": idempotencyKey,
			"operation_id":    idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit deal record: %w", err)
	}

	// The sale's WORK opens from this event (SALES SOP, 2026-09-19): same transaction as the row,
	// so a recorded sale always has its steps and a rolled-back one never does.
	if err := emitSaleRecorded(ctx, tx, tenantID, actorID, idempotencyKey, dealID, write); err != nil {
		return domain.Deal{}, err
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealCreate, idempotencyKey, "sales_deal", dealID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete deal idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit create deal: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// fpFloat renders an optional number as a stable, nil-safe fingerprint part (empty when absent, so
// "not sent" and "sent as 0" fingerprint differently).
func fpFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", *v), "0"), ".")
}

// insertDealLinesSQL writes a deal's lines in entry order with ONE set-based insert, returning
// each line's id by its own line number so a feed line's stock depletion can name it.
const insertDealLinesSQL = `
INSERT INTO public.sales_deal_lines (
	tenant_id, deal_id, line_no, product_type, product_code, product_kind, breed,
	quantity, unit, rate_per_unit,
	animal_count, male_count, female_count, total_weight_kg, sales_value
)
SELECT $1::uuid, $2::uuid, u.line_no, u.product_type, u.product_code, u.product_kind, u.breed,
       u.quantity, u.unit, u.rate_per_unit,
       u.animal_count, u.male_count, u.female_count, u.total_weight_kg, u.sales_value
FROM unnest(
	$3::int[], $4::text[], $5::text[], $6::text[], $7::text[],
	$8::numeric[], $9::text[], $10::numeric[],
	$11::numeric[], $12::numeric[], $13::numeric[], $14::numeric[], $15::numeric[]
) AS u(line_no, product_type, product_code, product_kind, breed,
       quantity, unit, rate_per_unit,
       animal_count, male_count, female_count, total_weight_kg, sales_value)
RETURNING line_no, line_id::text`

// farmParkIDSQL resolves a deal's farm label to its park, the way the feed purchase importer does.
const farmParkIDSQL = `
SELECT location_id::text
FROM public.locations
WHERE tenant_id = $1 AND location_type = 'park' AND upper(location_code) = upper($2)`

// deleteFeedSaleDepletionsSQL clears a deal's ledger rows before they are rewritten.
const deleteFeedSaleDepletionsSQL = `
DELETE FROM public.feed_sale_depletions
WHERE tenant_id = $1 AND deal_id = $2`

// dealStockFactsSQL is the three facts a depletion needs from the deal itself.
const dealStockFactsSQL = `
SELECT status, farm, sale_date::text
FROM public.sales_deals
WHERE tenant_id = $1 AND id = $2`

// insertFeedSaleDepletionsSQL takes the sold kilograms off the feed store, one row per feed line,
// reading the deal's OWN lines rather than a list handed in from Go -- so the ledger can never
// describe a sale the lines do not, whichever path wrote them.
//
// The producer's unique columns are (tenant_id, deal_id, line_no) and this reads by
// (tenant_id, deal_id, product_kind): every surviving row is still one line, so two feed lines
// naming the SAME feed write two ledger rows and the store is drawn twice -- which is correct,
// they are two sales of it. The DELETE above ranges over the identical key set, so a rewrite can
// neither leave a stale row behind nor take another deal's rows with it.
//
// projection-review: membership=sales_deal_lines at (tenant_id, line_id), narrowed to this deal and product_kind=feed; group_key=none, the insert is row-for-row and nothing is aggregated; join_cardinality=no joins, the SELECT reads one table; pagination=none, a deal's lines are at most MaxDealLines and every one must deplete; scope=tenant_id and deal_id
const insertFeedSaleDepletionsSQL = `
INSERT INTO public.feed_sale_depletions (
	tenant_id, deal_id, line_id, park_id, farm_label, feed_item_label, feed_day, quantity_kg
)
SELECT $1::uuid, $2::uuid, l.line_id, nullif(btrim($3), '')::uuid, $4, l.breed,
       $5::date, COALESCE(l.quantity, 0)
FROM public.sales_deal_lines l
WHERE l.tenant_id = $1 AND l.deal_id = $2 AND l.product_kind = $6`

// insertDealLines writes a deal's lines in entry order with one UNNEST insert, each stamped with
// the registry product it was sold under (migration 000402).
//
// It returns the new line ids by 1-based position, and the caller uses them only to CHECK that
// every line it wrote came back. The feed depletion reads the deal's own lines rather than taking
// a list from here, so the ledger cannot describe a sale the lines do not.
func insertDealLines(ctx context.Context, tx pgx.Tx, tenantID, dealID string, lines []domain.DealLineWrite) ([]string, error) {
	if len(lines) == 0 {
		return nil, nil
	}
	n := len(lines)
	lineNos := make([]int32, n)
	products := make([]string, n)
	codes := make([]string, n)
	kinds := make([]string, n)
	breeds := make([]string, n)
	quantities := make([]*float64, n)
	units := make([]*string, n)
	rates := make([]*float64, n)
	animals := make([]*float64, n)
	males := make([]*float64, n)
	females := make([]*float64, n)
	weights := make([]*float64, n)
	values := make([]float64, n)
	for i, l := range lines {
		lineNos[i] = int32(i + 1)
		products[i] = l.ProductType
		codes[i], kinds[i], _ = l.ResolvedProduct()
		breeds[i] = l.Breed
		quantities[i] = l.Quantity
		rates[i] = l.RatePerUnit
		// The unit is stored only on a line that actually carries a quantity; a lump-value line
		// has no unit to state, and writing one would say it was priced by something it was not.
		if l.Quantity != nil {
			if _, _, unit := l.ResolvedProduct(); unit != "" {
				u := unit
				units[i] = &u
			}
		}
		animals[i] = l.AnimalCount
		males[i] = l.MaleCount
		females[i] = l.FemaleCount
		weights[i] = l.TotalWeightKg
		values[i] = l.SalesValue
	}
	rows, err := tx.Query(ctx, insertDealLinesSQL,

		tenantID, dealID, lineNos, products, codes, kinds, breeds,
		quantities, units, rates,
		animals, males, females, weights, values,
	)
	if err != nil {
		return nil, fmt.Errorf("sales: create deal lines: %w", err)
	}
	defer rows.Close()
	// Keyed by the line's own number rather than by scan order: RETURNING makes no promise about
	// the order rows come back in.
	ids := make([]string, n)
	for rows.Next() {
		var no int32
		var id string
		if err := rows.Scan(&no, &id); err != nil {
			return nil, fmt.Errorf("sales: scan created deal line: %w", err)
		}
		if no < 1 || int(no) > n {
			return nil, fmt.Errorf("sales: created deal line %d outside the %d written", no, n)
		}
		ids[int(no)-1] = id
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sales: created deal line rows: %w", err)
	}
	for i, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("sales: created deal line %d came back without an id", i+1)
		}
	}
	return ids, nil
}

// syncFeedSaleDepletions makes the feed store agree with what this deal currently SAYS.
//
// It rewrites the deal's ledger rows from its own lines, in the transaction that changed the deal,
// and it is driven by the deal's STATUS: feed leaves the store when a sale is closed, and a sale
// still in discussion -- or one that failed -- has moved no feed at all. Depleting on the mere
// recording of an expected sale would take two tonnes off a store that still physically holds
// them, and the shortage would surface days later as a low-stock alert nobody could explain.
//
// It is a DELETE-then-INSERT rather than a diff, so it is idempotent and reaches the right answer
// from any previous state: a deal closed, reopened and closed again ends with exactly one set of
// rows, and a deal that fails after being closed gives its feed back.
//
// The park is resolved from the deal's farm by location_code the way the feed purchase importer
// does. A farm whose park row does not resolve still writes the ledger row with a NULL park and
// its farm label -- the column is nullable for exactly this reason (000174) -- rather than losing
// the fact that the feed left.
func syncFeedSaleDepletions(ctx context.Context, tx pgx.Tx, tenantID, dealID string) error {
	if _, err := tx.Exec(ctx, deleteFeedSaleDepletionsSQL, tenantID, dealID); err != nil {
		return fmt.Errorf("sales: clear feed sale depletion: %w", err)
	}

	var status, farm, saleDate string
	if err := tx.QueryRow(ctx, dealStockFactsSQL, tenantID, dealID).Scan(&status, &farm, &saleDate); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ports.ErrDealNotFound
		}
		return fmt.Errorf("sales: read deal for feed depletion: %w", err)
	}
	if status != domain.StatusDealClosed {
		return nil
	}

	var parkID *string
	var resolved string
	switch err := tx.QueryRow(ctx, farmParkIDSQL, tenantID, farm).Scan(&resolved); {
	case err == nil:
		parkID = &resolved
	case errors.Is(err, pgx.ErrNoRows):
		// Left NULL deliberately; see the doc comment.
	default:
		return fmt.Errorf("sales: resolve farm park for feed sale: %w", err)
	}

	if _, err := tx.Exec(ctx, insertFeedSaleDepletionsSQL,
		tenantID, dealID, derefOrEmpty(parkID), farm, saleDate, domain.KindFeed,
	); err != nil {
		return fmt.Errorf("sales: record feed sale depletion: %w", err)
	}
	return nil
}

func derefOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// fpLines renders the lines as one stable fingerprint part, so a replay that changes any line --
// or their order -- is a different request.
func fpLines(lines []domain.DealLineWrite) string {
	parts := make([]string, 0, len(lines))
	for _, l := range lines {
		parts = append(parts, strings.Join([]string{
			l.ProductType, l.Breed, fpFloat(l.AnimalCount), fpFloat(l.MaleCount), fpFloat(l.FemaleCount),
			fpFloat(l.TotalWeightKg), fmt.Sprintf("%.4f", l.SalesValue),
			// The quantity and rate define what a feed line TAKES OFF THE STORE, so a replay
			// changing either is a different request and must be refused rather than recorded.
			fpFloat(l.Quantity), fpFloat(l.RatePerUnit),
		}, "|"))
	}
	return strings.Join(parts, ";")
}
