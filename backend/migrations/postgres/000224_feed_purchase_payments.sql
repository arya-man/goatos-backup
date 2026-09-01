-- +goose Up
-- seed-fixture-guard:ignore: additive installment ledger on the commercial feed-purchase table; no
-- vaccination/HRMS seed contract change.
--
-- FEED PURCHASE PART-PAYMENTS (maintainer request 2026-08-29).
--
-- The farm does not pay a feed load in one go: some money is released at delivery and the balance
-- later, sometimes in several instalments. The ledger so far kept ONE number
-- (feed_purchases.payment_released) and ONE state (payment_status), so a second instalment could
-- only overwrite the first and the payment history was lost.
--
-- This table is the instalment ledger: one row per amount actually handed to the vendor for one
-- purchased load. feed_purchases.payment_released stays, as the RUNNING TOTAL of these rows for
-- app-recorded payments (sheet history keeps whatever single figure the sheet carried, with no
-- instalment rows), maintained inside the same transaction as the instalment insert so the total
-- can never drift from its own rows. payment_status keeps the sheet's two-word vocabulary
-- (Paid / Pending); a partly-paid load is Pending with money shown against it, never a third word.
CREATE TABLE IF NOT EXISTS public.feed_purchase_payments (
  payment_id       uuid DEFAULT gen_random_uuid() NOT NULL,
  tenant_id        uuid NOT NULL REFERENCES tenants (tenant_id),
  feed_purchase_id uuid NOT NULL REFERENCES feed_purchases (feed_purchase_id) ON DELETE CASCADE,
  -- The business date the money was handed over, never a timestamp.
  paid_on          date NOT NULL,
  amount_rupees    numeric(14, 2) NOT NULL,
  -- Free-text context ("advance at loading", "balance after weighbridge"). Optional.
  note             text NOT NULL DEFAULT '',
  recorded_by      uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT feed_purchase_payments_pkey PRIMARY KEY (payment_id),
  CONSTRAINT feed_purchase_payments_amount_check CHECK (amount_rupees > 0)
);

-- The one read path: every instalment of the purchases on one ledger page, oldest first.
CREATE INDEX IF NOT EXISTS feed_purchase_payments_purchase_idx
  ON public.feed_purchase_payments (tenant_id, feed_purchase_id, paid_on, created_at);

COMMENT ON TABLE public.feed_purchase_payments IS
  'Instalments paid against one feed purchase, recorded on /procurement/feed-purchases. feed_purchases.payment_released is the running total of these rows for app-recorded payments and is updated in the same transaction as each insert.';
COMMENT ON COLUMN public.feed_purchase_payments.paid_on IS
  'Business date the money was handed over (IST), never a timestamp.';

-- +goose Down
DROP TABLE IF EXISTS public.feed_purchase_payments;
