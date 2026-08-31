-- +goose Up
-- seed-fixture-guard:ignore: additive receivables ledger on the commercial sales table; no
-- vaccination/HRMS seed contract change.
--
-- SALES DEAL PART-PAYMENTS (maintainer request 2026-08-31, the sales twin of the feed-purchase
-- instalment ledger in 000224).
--
-- A buyer does not pay a deal in one go either: an advance when the deal is struck, more on
-- pickup, the balance later. The ledger so far kept ONE figure (sales_deals.advance_amount, the
-- sheet's single recorded advance), so a second receipt could only overwrite the first and the
-- payment history was lost.
--
-- This table is the receipts ledger: one row per amount the buyer actually handed over for one
-- deal. sales_deals.payment_received is the RUNNING TOTAL of these rows, maintained inside the
-- same transaction as each insert so the total can never drift from its own rows. It is seeded
-- from advance_amount below — the sheet's advance IS money received — and advance_amount itself
-- stays untouched as sheet-era detail. Deal status is NOT auto-derived from money: Deal Closed /
-- Advance Paid / In Discussion is the deal's lifecycle and stays a human decision.
CREATE TABLE IF NOT EXISTS public.sales_deal_payments (
  payment_id    uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id     uuid NOT NULL REFERENCES tenants (tenant_id),
  deal_id       uuid NOT NULL REFERENCES sales_deals (id) ON DELETE CASCADE,
  -- The business date the money was received, never a timestamp.
  received_on   date NOT NULL,
  amount_rupees numeric(14, 2) NOT NULL,
  -- Free-text context ("advance at deal", "on pickup", "balance by transfer"). Optional.
  note          text NOT NULL DEFAULT '',
  recorded_by   uuid,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT sales_deal_payments_pkey PRIMARY KEY (payment_id),
  CONSTRAINT sales_deal_payments_amount_check CHECK (amount_rupees > 0)
);

-- The one read path: every receipt of the deals on one ledger page, oldest first.
CREATE INDEX IF NOT EXISTS sales_deal_payments_deal_idx
  ON public.sales_deal_payments (tenant_id, deal_id, received_on, created_at);

ALTER TABLE public.sales_deals
  ADD COLUMN IF NOT EXISTS payment_received numeric(14, 2);

-- Seed the running total from the sheet's recorded advance: that money was really received, and
-- without this every sheet deal with an advance would read as fully unpaid the moment the balance
-- column appears. No instalment ROW is fabricated for it — the advance's own date was never
-- recorded, and a dated row nobody entered would be an invented fact.
UPDATE public.sales_deals
SET payment_received = advance_amount
WHERE payment_received IS NULL AND advance_amount IS NOT NULL;

COMMENT ON TABLE public.sales_deal_payments IS
  'Amounts received from the buyer against one sales deal, recorded on /sales. sales_deals.payment_received is the running total of these rows (seeded from the sheet''s advance_amount) and is updated in the same transaction as each insert.';
COMMENT ON COLUMN public.sales_deal_payments.received_on IS
  'Business date the money was received (IST), never a timestamp.';
COMMENT ON COLUMN public.sales_deals.payment_received IS
  'Running total of money received for this deal: seeded from advance_amount, advanced by sales_deal_payments rows inside the same transaction.';

-- +goose Down
ALTER TABLE public.sales_deals DROP COLUMN IF EXISTS payment_received;
DROP TABLE IF EXISTS public.sales_deal_payments;
