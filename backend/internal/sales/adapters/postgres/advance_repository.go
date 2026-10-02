package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/platform/audit"
	"github.com/vgoats/goatos/backend/internal/sales/domain"
	"github.com/vgoats/goatos/backend/internal/sales/ports"
)

// AN ADVANCE BEFORE ANYTHING IS CHOSEN (maintainer decision 2026-10-02). The two writes that finish
// an advance-only sale: adding what was sold, or -- when the sale never happens -- settling the
// money on the failed deal. See domain/advance_only.go for the rules.

const (
	// idemScopeDealLinesAdd namespaces the add-what-was-sold idempotency keys.
	idemScopeDealLinesAdd = "sales.deal.lines.add"
	// idemScopeDealSettlement namespaces the refund-or-keep idempotency keys.
	idemScopeDealSettlement = "sales.deal.advance_settlement"
)

// AddDealLines writes what was sold onto an advance-only sale, ONCE. The deal row becomes the
// rollup of the lines, exactly as a sale recorded with them would have been, and
// sales.deal.recorded is emitted HERE -- in the same transaction -- so the sale's workflow opens
// now, with the steps its lines call for, rather than when the advance was taken.
//
// Everything is checked under the deal's row lock: the sale must still be advance-only (a second
// add is refused, not merged), must not have failed, and its value must cover the money the buyer
// has already handed over.
func (r *Repository) AddDealLines(ctx context.Context, tenantID, dealID string, write domain.DealLinesWrite, rollup domain.DealRollup, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin add deal lines: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(dealID, fpLines(write.Lines))
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealLinesAdd, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit add deal lines replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, dealID)
	}

	var (
		saleDate                time.Time
		farm, buyerName, status string
		advanceOnly             bool
		received                *float64
	)
	err = tx.QueryRow(ctx, `
SELECT sale_date, farm, buyer_name, status, product_type IS NULL, payment_received
FROM public.sales_deals
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`, tenantID, dealID).Scan(&saleDate, &farm, &buyerName, &status, &advanceOnly, &received)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal for lines: %w", err)
	}
	if status == domain.StatusDealFailed {
		return domain.Deal{}, domain.ErrDealFailedIsFinal
	}
	if !advanceOnly {
		return domain.Deal{}, domain.ErrDealAlreadyHasLines
	}
	if received != nil && *received > rollup.SalesValue+0.005 {
		return domain.Deal{}, domain.ErrDealValidation{
			Field:  "lines",
			Reason: fmt.Sprintf("the buyer has already paid ₹%.2f, more than these products are worth", *received),
		}
	}

	if err := confirmProductsStillSellable(ctx, tx, tenantID, write.Lines); err != nil {
		return domain.Deal{}, err
	}
	if _, err := insertDealLines(ctx, tx, tenantID, dealID, write.Lines); err != nil {
		return domain.Deal{}, err
	}
	if _, err := tx.Exec(ctx, `
UPDATE public.sales_deals
SET product_type = $3, breed = $4, animal_count = $5, male_count = $6, female_count = $7,
    total_weight_kg = $8, sales_value = $9, updated_at = now()
WHERE tenant_id = $1 AND id = $2`,
		tenantID, dealID, rollup.ProductType, rollup.Breed, rollup.AnimalCount, rollup.MaleCount,
		rollup.FemaleCount, rollup.TotalWeightKg, rollup.SalesValue); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: write deal rollup: %w", err)
	}
	// An open sale takes nothing off the feed store; kept for symmetry with every line write, so a
	// future path that adds lines to a closed sale cannot forget the store.
	if err := syncFeedSaleDepletions(ctx, tx, tenantID, dealID); err != nil {
		return domain.Deal{}, err
	}

	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.lines_add",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":          "sales",
			"module":          "sales",
			"category":        "deal",
			"farm":            farm,
			"product_type":    rollup.ProductType,
			"line_count":      len(write.Lines),
			"sales_value":     rollup.SalesValue,
			"idempotency_key": idempotencyKey,
			"operation_id":    idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit add deal lines: %w", err)
	}

	recorded := write.AsDealWrite(rollup)
	recorded.SaleDate = saleDate.Format("2006-01-02")
	recorded.Farm = farm
	recorded.BuyerName = buyerName
	recorded.Status = status
	if err := emitSaleRecorded(ctx, tx, tenantID, actorID, idempotencyKey, dealID, recorded); err != nil {
		return domain.Deal{}, err
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealLinesAdd, idempotencyKey, "sales_deal", dealID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete add deal lines idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit add deal lines: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

// SettleDealAdvance records what became of a FAILED sale's money: refunded (in part or whole) and
// the rest kept by the farm. One row per deal; recording again replaces it, so a correction never
// stacks a second answer beside the first.
func (r *Repository) SettleDealAdvance(ctx context.Context, tenantID, dealID string, write domain.AdvanceSettlementWrite, actorID, idempotencyKey string) (domain.Deal, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: begin advance settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	fingerprint := requestFingerprint(dealID, fmt.Sprintf("%.2f", write.RefundedRupees), write.RefundedOn, write.Note)
	reservation, err := reserveIdempotency(ctx, tx, tenantID, idemScopeDealSettlement, idempotencyKey, fingerprint)
	if err != nil {
		return domain.Deal{}, err
	}
	if !reservation.proceed {
		if err := tx.Commit(ctx); err != nil {
			return domain.Deal{}, fmt.Errorf("sales: commit advance settlement replay read: %w", err)
		}
		return r.getDeal(ctx, tenantID, dealID)
	}

	var status string
	var received *float64
	err = tx.QueryRow(ctx, `
SELECT status, payment_received
FROM public.sales_deals
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`, tenantID, dealID).Scan(&status, &received)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Deal{}, ports.ErrDealNotFound
	}
	if err != nil {
		return domain.Deal{}, fmt.Errorf("sales: lock deal for settlement: %w", err)
	}
	if status != domain.StatusDealFailed {
		return domain.Deal{}, domain.ErrSettlementNeedsFailedDeal
	}
	if received == nil || *received <= 0 {
		return domain.Deal{}, domain.ErrNothingToSettle
	}
	if write.RefundedRupees > *received+0.005 {
		return domain.Deal{}, domain.ErrDealValidation{
			Field:  "refunded_rupees",
			Reason: fmt.Sprintf("cannot be more than the ₹%.2f the buyer paid", *received),
		}
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO public.sales_deal_advance_settlements
    (tenant_id, deal_id, refunded_rupees, refunded_on, note, settled_by)
VALUES ($1::uuid, $2::uuid, $3, nullif($4, '')::date, nullif($5, ''), $6)
ON CONFLICT (tenant_id, deal_id) DO UPDATE
SET refunded_rupees = EXCLUDED.refunded_rupees,
    refunded_on     = EXCLUDED.refunded_on,
    note            = EXCLUDED.note,
    settled_by      = EXCLUDED.settled_by,
    updated_at      = now()`,
		tenantID, dealID, write.RefundedRupees, write.RefundedOn, write.Note, actorID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: write advance settlement: %w", err)
	}

	outcome := domain.SettlementOutcome(domain.AdvanceSettlement{RefundedRupees: write.RefundedRupees}, *received)
	if err := audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     tenantID,
		ActorID:      actorID,
		ActorType:    "human",
		Action:       "sales.deal.advance_settle",
		ResourceType: "sales_deal",
		ResourceID:   dealID,
		Metadata: map[string]any{
			"domain":           "sales",
			"module":           "sales_deals",
			"category":         "payment",
			"outcome":          outcome,
			"refunded_rupees":  write.RefundedRupees,
			"refunded_on":      write.RefundedOn,
			"payment_received": *received,
			"idempotency_key":  idempotencyKey,
			"operation_id":     idempotencyKey,
		},
	}); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: audit advance settlement: %w", err)
	}

	if err := completeIdempotency(ctx, tx, tenantID, idemScopeDealSettlement, idempotencyKey, "sales_deal", dealID); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: complete advance settlement idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Deal{}, fmt.Errorf("sales: commit advance settlement: %w", err)
	}
	return r.getDeal(ctx, tenantID, dealID)
}

const dealSettlementsForPageSQL = `
	SELECT deal_id::text, refunded_rupees, refunded_on, coalesce(note, ''), settled_by, updated_at
	FROM public.sales_deal_advance_settlements
	WHERE tenant_id = $1 AND deal_id = ANY($2::uuid[])`

// attachDealSettlements loads the settlement of every deal on one page in ONE batched read (`= ANY`,
// never a per-row query). Only failed paid sales carry one, so most pages read nothing.
func (r *Repository) attachDealSettlements(ctx context.Context, tenantID string, deals []domain.Deal) error {
	ids := make([]string, 0, len(deals))
	index := make(map[string]int, len(deals))
	for i, d := range deals {
		if d.Status != domain.StatusDealFailed {
			continue
		}
		ids = append(ids, d.DealID)
		index[d.DealID] = i
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.pool.Query(ctx, dealSettlementsForPageSQL, tenantID, ids)
	if err != nil {
		return fmt.Errorf("list sales deal settlements: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			dealID     string
			s          domain.AdvanceSettlement
			refundedOn *time.Time
			updatedAt  time.Time
		)
		if err := rows.Scan(&dealID, &s.RefundedRupees, &refundedOn, &s.Note, &s.SettledBy, &updatedAt); err != nil {
			return fmt.Errorf("list sales deal settlements scan: %w", err)
		}
		if refundedOn != nil {
			on := refundedOn.Format("2006-01-02")
			s.RefundedOn = &on
		}
		s.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if i, ok := index[dealID]; ok {
			settled := s
			deals[i].Settlement = &settled
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list sales deal settlements rows: %w", err)
	}
	return nil
}
