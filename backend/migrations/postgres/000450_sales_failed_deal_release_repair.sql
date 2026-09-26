-- +goose Up
-- seed-fixture-guard:ignore: one-time outbox re-announcement for sales deals already marked Deal
-- Failed; no schema, seed contract, source fixture or read-model shape change.
--
-- RELEASE THE ANIMALS OF SALES THAT FAILED BEFORE THE RELEASE EXISTED (2026-09-26,
-- docs/decisions/sales-sop.md -> "A failed sale" -> "Existing data"). A failed sale gives its
-- tagged animals back through ONE path: sales.Repository.SetDealStatus emits
-- sales.deal.status_changed (status Deal Failed) and identity/app.SaleFailedReleaseHandler releases
-- every allocation still 'tagged' (goats back to alive in the pen they never left, one
-- goat.reinstated per animal so vaccination re-owes the work the exit cancelled, allocations kept as
-- released history with who / when / why, one goat.sale_released for the Feed Director), while
-- tasks/app.SaleStatusChangedWorkflowHandler cancels the sale's workflow. SetDealStatus emits only
-- when the status CHANGES, so a deal that was already Deal Failed before that code shipped never
-- emits it again, and Deal Failed is final -- its animals would stay 'sold' for ever. 000444 only
-- cancelled the workflow cards.
--
-- This repair does NOT duplicate the release in SQL. It re-announces the failure: ONE
-- sales.deal.status_changed outbox row per deal that is Deal Failed AND still has a 'tagged'
-- allocation, in exactly the envelope SetDealStatus writes (deal_outbox.go emitDealStatusChanged ->
-- insertSalesDealEvent: same event type, schema version, topic, aggregate/subject sales_deal,
-- payload keys). The outbox relay delivers it and the SAME consumers do the work, so vaccination,
-- feed and counts follow exactly as for a live failure. The Feed Director's goat.sale_released
-- notice is KEPT for these historical releases: the animals re-enter the live head count the
-- moment the release commits, so the feed sheet goes up for their pens and the Feed Director is
-- owed the same "these pens get animals back" notice a live failure sends.
--
--   actor     payload.actor_id = whoever last set the deal's status (its sales.deal.status_set
--             audit row), else blank -- the documented system-caller shape, where the release is
--             recorded against the person who tagged each animal. The envelope actor is
--             system_rule: the migration, not a person, is re-announcing.
--   previous  payload.previous_status is blank: the status did not change now, and no consumer
--             reads it.
--   key       '<deal>:repair-000450' -- the real key is '<deal>:<RFC3339 updated_at>', so the two
--             can never collide. event_id is derived from the same key, so a re-run conflicts on
--             outbox_messages_event_unique; the NOT EXISTS guard makes a re-run insert nothing.
--
-- Idempotent end to end: a re-run inserts nothing; the consumer's own guards (only 'tagged'
-- allocations, per-animal sale_release idempotency key) make a redelivery release nothing twice.
-- A deal that is not Deal Failed, or has nothing tagged, is never touched.
-- seed-migration-guard:ignore owner=manohark issue=sales-failed-deal-release reason=one-time-outbox-reannouncement-of-already-failed-deals expiry=2026-12-31
-- projection-review: membership=sales_deals with status 'Deal Failed' having >=1 goat_sale_allocations row in status 'tagged' (EXISTS, no fan-out); group_key=(tenant_id, deal id), one outbox row per deal; join_cardinality=sales_deals 1:0..1 park location (LIMIT 1) and 1:0..1 latest status_set audit row (LIMIT 1); pagination=none, one bounded repair; scope=tenant + deal
WITH failed AS (
  SELECT d.tenant_id, d.id AS deal_id, d.farm, d.sale_date,
         (SELECT l.location_id::text
            FROM public.locations l
           WHERE l.tenant_id = d.tenant_id AND l.location_type = 'park'
             AND upper(l.location_code) = upper(d.farm)
           LIMIT 1) AS park_id,
         COALESCE((SELECT al.actor_id::text
                     FROM public.audit_log al
                    WHERE al.tenant_id = d.tenant_id AND al.resource_type = 'sales_deal'
                      AND al.resource_id = d.id AND al.action = 'sales.deal.status_set'
                    ORDER BY al.recorded_at DESC
                    LIMIT 1), '') AS actor_id,
         d.id::text || ':repair-000450' AS repair_key,
         now() AS at
  FROM public.sales_deals d
  WHERE d.status = 'Deal Failed'
    AND EXISTS (
      SELECT 1 FROM public.goat_sale_allocations a
      WHERE a.tenant_id = d.tenant_id AND a.sales_deal_id = d.id AND a.status = 'tagged')
),
keyed AS (
  SELECT f.*,
         md5('sales.deal.status_changed:' || f.tenant_id::text || ':' || f.repair_key)::uuid AS event_id,
         to_char(f.at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS stamp,
         to_char(f.at AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD') AS business_date
  FROM failed f
)
INSERT INTO public.outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, status
)
SELECT
  k.tenant_id,
  k.event_id,
  'sales.deal.status_changed',
  '1.0.0',
  'sales_deal',
  k.deal_id,
  'sales.events',
  jsonb_build_object(
    'event_id', k.event_id::text,
    'event_type', 'sales.deal.status_changed',
    'schema_version', '1.0.0',
    'schema_ref', 'contracts/jsonschema/domain-event-envelope.schema.json',
    'aggregate_type', 'sales_deal',
    'aggregate_id', k.deal_id::text,
    'occurred_at', k.stamp,
    'recorded_at', k.stamp,
    'producer', jsonb_build_object('service', 'goatos-migration', 'module', 'sales', 'version', NULL),
    'idempotency_key', k.repair_key,
    'actor', jsonb_build_object('actor_type', 'system_rule', 'actor_id', NULL, 'actor_ref', NULL),
    'subject_type', 'sales_deal',
    'subject_id', k.deal_id::text,
    'visibility_scope', CASE WHEN k.park_id IS NULL
                             THEN jsonb_build_object('tenant_id', k.tenant_id::text)
                             ELSE jsonb_build_object('tenant_id', k.tenant_id::text, 'park_id', k.park_id) END,
    'evidence_refs', jsonb_build_array(jsonb_build_object(
      'evidence_type', 'source_record', 'evidence_id', 'sales_deal:' || k.deal_id::text)),
    'payload', jsonb_build_object(
      'tenant_id', k.tenant_id::text,
      'sales_deal_id', k.deal_id::text,
      'previous_status', '',
      'status', 'Deal Failed',
      'sale_date', to_char(k.sale_date, 'YYYY-MM-DD'),
      'actor_id', k.actor_id
    ),
    'trace_id', 'sales-deal:' || k.deal_id::text
  ),
  jsonb_build_object('actor_id', k.actor_id, 'farm', k.farm, 'business_date', k.business_date),
  'sales.deal.status_changed:' || k.repair_key,
  'pending'
FROM keyed k
WHERE NOT EXISTS (
  SELECT 1 FROM public.outbox_messages om
  WHERE om.aggregate_type = 'sales_deal' AND om.aggregate_id = k.deal_id
    AND om.event_type = 'sales.deal.status_changed'
    AND om.idempotency_key = 'sales.deal.status_changed:' || k.repair_key)
ON CONFLICT (event_id) DO NOTHING;

-- +goose Down
-- A data repair: once delivered, the released animals are back in the herd through the ordinary
-- lifecycle path, which a down migration must not undo. An undelivered re-announcement may be
-- withdrawn.
DELETE FROM public.outbox_messages
WHERE event_type = 'sales.deal.status_changed'
  AND idempotency_key LIKE 'sales.deal.status_changed:%:repair-000450'
  AND status = 'pending';
