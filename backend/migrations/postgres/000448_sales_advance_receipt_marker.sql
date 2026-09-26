-- +goose Up
-- seed-fixture-guard:ignore: one marker column on the commercial sales receipts ledger plus a
-- backfill of the rows 000447 inserted; no vaccination/HRMS seed contract change.
--
-- "ADVANCE RECEIVED" FOLLOWS THE RECEIPT (maintainer decision 2026-09-25).
--
-- 000447 made the advance paid at sale the deal's first receipt, so the desk now corrects or
-- removes it in the receipts list. The sale's "Advance received" (sales_deals.advance_amount) did
-- not follow: it kept the figure typed when the sale was recorded. From this release, editing the
-- advance receipt sets advance_amount to its amount and removing it clears advance_amount, in the
-- same transaction as the receipt write.
--
-- That needs to know WHICH receipt is the advance, and the note is not an identity: the desk may
-- re-word it, and may type "Advance at sale" on an ordinary receipt. is_advance is the identity.
-- The record-sale write stamps it; this backfill stamps the rows 000447 inserted, matched exactly
-- as 000447 wrote them (its note, and the deal's advance_amount as the amount, to the paisa), one per deal --
-- the earliest, which is the row 000447 or the record-sale write created.
ALTER TABLE public.sales_deal_payments
  ADD COLUMN IF NOT EXISTS is_advance boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN public.sales_deal_payments.is_advance IS
  'True on the one receipt that IS the advance paid when the sale was recorded (written by the record-sale transaction, backfilled by 000448 for 000447''s rows). Editing it sets sales_deals.advance_amount to its amount; removing it clears advance_amount.';

UPDATE public.sales_deal_payments p
SET is_advance = true
FROM (
  SELECT DISTINCT ON (a.tenant_id, a.deal_id) a.payment_id
  FROM public.sales_deal_payments a
  JOIN public.sales_deals d ON d.tenant_id = a.tenant_id AND d.id = a.deal_id
  WHERE a.note = 'Advance at sale'
    AND d.advance_amount > 0
    -- 000447 inserted advance_amount into a 2-place column, so an advance stored with float
    -- noise (55930.00000000001) lands rounded; compare at the column's own precision.
    AND abs(a.amount_rupees - d.advance_amount) < 0.005
  ORDER BY a.tenant_id, a.deal_id, a.created_at, a.payment_id
) first_advance
WHERE p.payment_id = first_advance.payment_id
  AND NOT p.is_advance;

-- At most one advance receipt per deal. The table is the per-deal receipts ledger (tens of rows
-- per deal, a few thousand in all), so a plain build is brief.
CREATE UNIQUE INDEX IF NOT EXISTS sales_deal_payments_one_advance_uq
  ON public.sales_deal_payments (tenant_id, deal_id)
  WHERE is_advance;

-- +goose Down
DROP INDEX IF EXISTS public.sales_deal_payments_one_advance_uq;
ALTER TABLE public.sales_deal_payments DROP COLUMN IF EXISTS is_advance;
