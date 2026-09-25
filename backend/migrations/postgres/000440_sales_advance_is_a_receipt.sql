-- +goose Up
-- seed-fixture-guard:ignore: one-time data repair on the commercial sales receipts ledger; no
-- vaccination/HRMS seed contract change.
--
-- THE ADVANCE IS THE SALE'S FIRST RECEIPT (2026-09-25).
--
-- Until now the advance paid when a sale was recorded went ONLY into sales_deals.payment_received
-- (000227 seeded the running total from it, and the record-sale write did the same). No
-- sales_deal_payments row carried it, so the advance never appeared in the receipts list, could
-- not be corrected or removed, and the desk -- seeing no receipt -- re-entered it. A CBE deal of
-- 02/09/2026 worth 1,97,415 read 3,94,830 received.
--
-- From this release the record-sale write inserts the advance as a receipt in its own transaction.
-- This migration gives every EXISTING advance the same row, once, so payment_received equals the
-- sum of the listed receipts afterwards.
--
-- It supersedes the 000227 stance that no dated row should be fabricated for a sheet advance: the
-- row is dated by the SALE date (capped at today for an expected sale dated ahead), which is the
-- only date the ledger has for it, and its note says what it is.
--
-- Narrowed to the deals whose unexplained money IS the advance: payment_received minus the sum of
-- the existing receipts equals advance_amount within one rupee of rounding. A deal whose total
-- disagrees for any other reason is left alone for a person to look at. Re-running is a no-op: an
-- inserted row closes the gap it matched, and a deal that already carries an advance receipt is
-- skipped outright.
INSERT INTO public.sales_deal_payments (tenant_id, deal_id, received_on, amount_rupees, note)
SELECT d.tenant_id,
       d.id,
       LEAST(d.sale_date, (now() AT TIME ZONE 'Asia/Kolkata')::date),
       d.advance_amount,
       'Advance at sale'
FROM public.sales_deals d
LEFT JOIN LATERAL (
  SELECT COALESCE(sum(p.amount_rupees), 0) AS listed
  FROM public.sales_deal_payments p
  WHERE p.tenant_id = d.tenant_id AND p.deal_id = d.id
) r ON true
WHERE d.advance_amount > 0
  AND d.payment_received IS NOT NULL
  AND abs((d.payment_received - r.listed) - d.advance_amount) <= 1
  AND NOT EXISTS (
    SELECT 1 FROM public.sales_deal_payments a
    WHERE a.tenant_id = d.tenant_id AND a.deal_id = d.id AND a.note = 'Advance at sale'
  );

-- +goose Down
-- The rows are indistinguishable from an advance receipt recorded by the app after this release,
-- so they are not removed: deleting them would drop money the buyer really handed over from the
-- receipts list while payment_received still counts it.
SELECT 1;
