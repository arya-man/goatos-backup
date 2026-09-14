-- +goose Up
-- seed-fixture-guard:ignore: market survey tables are operational (phone-written prices); the
-- workforce_members join below only ticks per-person access rows for an existing module and
-- changes no vaccination/HRMS seed source, fixture schema, or read-model.
-- MARKET SURVEY (maintainer decision 2026-09-14).
--
-- Every morning the procurement director calls three or four markets and asks what goat and sheep
-- are fetching there today: live price, carcass price, offals price, for each species. Those
-- calls were made and forgotten. Now each city is a card on the phone for the day, the director
-- types the answers in, and the Sales vertical's Market analytics page reads them back over time.
--
-- EVERYTHING ABOUT WHAT IS ASKED IS CONFIG, not code (maintainer instruction, same day: "if I want
-- tomorrow, I will add one more question, I will remove, I will change it from kg to five hundred
-- grams"). Two config tables under Sales Config:
--
--   market_cities      the places he calls. Adding one puts a new card on the phone from that day.
--   market_questions   what he asks in every city: a label and the unit the price is quoted in.
--
-- and one fact table:
--
--   market_price_entries  one price per (city, question, business day).
--
-- THE ENTRY SNAPSHOTS THE QUESTION IT ANSWERED. A question's label and unit can be edited at any
-- time -- that is the point of making it config -- so the entry keeps `question_label` and
-- `unit_label` as they were the morning it was recorded. A price entered as ₹/kg stays ₹/kg in
-- history after the question is switched to ₹/500 g; the analytics page renders the snapshot,
-- never the live question, so a unit change starts a new series rather than silently rescaling an
-- old one. The same holds for a renamed city.
--
-- RETIRE, NEVER DELETE. A city or question that stops being asked is marked retired; its entries
-- stay readable. Reactivating it resumes the series.
--
-- ONE ROW PER (city, question, day) and the write is an UPSERT: the director may correct a figure
-- the same morning, and the row keeps who recorded it last and when.
--
-- Tenants are created only by the baseline (no seed command inserts tenants), so the six default
-- questions are seeded here for every existing tenant, the 000276 shape.

CREATE TABLE public.market_cities (
    id          uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id   uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    name        text        NOT NULL,
    sort_order  integer     NOT NULL DEFAULT 0,
    status      text        NOT NULL DEFAULT 'active',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_cities_pkey PRIMARY KEY (id),
    CONSTRAINT market_cities_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT market_cities_status_check CHECK (status IN ('active', 'retired'))
);

-- One active city per name: two "Chennai" cards would be two calls to the same market.
CREATE UNIQUE INDEX market_cities_active_name_uq
    ON public.market_cities (tenant_id, lower(btrim(name))) WHERE status = 'active';
CREATE INDEX market_cities_tenant_order_idx
    ON public.market_cities (tenant_id, status, sort_order, name);

CREATE TABLE public.market_questions (
    id          uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id   uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    label       text        NOT NULL,
    unit_label  text        NOT NULL,
    sort_order  integer     NOT NULL DEFAULT 0,
    status      text        NOT NULL DEFAULT 'active',
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_questions_pkey PRIMARY KEY (id),
    CONSTRAINT market_questions_label_not_blank CHECK (btrim(label) <> ''),
    CONSTRAINT market_questions_unit_not_blank CHECK (btrim(unit_label) <> ''),
    CONSTRAINT market_questions_status_check CHECK (status IN ('active', 'retired'))
);

CREATE UNIQUE INDEX market_questions_active_label_uq
    ON public.market_questions (tenant_id, lower(btrim(label))) WHERE status = 'active';
CREATE INDEX market_questions_tenant_order_idx
    ON public.market_questions (tenant_id, status, sort_order, label);

CREATE TABLE public.market_price_entries (
    id              uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id       uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    city_id         uuid        NOT NULL REFERENCES public.market_cities (id),
    question_id     uuid        NOT NULL REFERENCES public.market_questions (id),
    business_date   date        NOT NULL,
    price           numeric(12, 2) NOT NULL,
    -- Snapshots: the words the answer was given against, frozen at recording time.
    city_name       text        NOT NULL,
    question_label  text        NOT NULL,
    unit_label      text        NOT NULL,
    recorded_by     uuid,
    recorded_at     timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_price_entries_pkey PRIMARY KEY (id),
    CONSTRAINT market_price_entries_price_not_negative CHECK (price >= 0),
    CONSTRAINT market_price_entries_day_uq UNIQUE (tenant_id, city_id, question_id, business_date)
);

-- The analytics window read: a tenant's entries between two dates, newest first.
CREATE INDEX market_price_entries_tenant_date_idx
    ON public.market_price_entries (tenant_id, business_date DESC, city_id, question_id);

COMMENT ON TABLE public.market_cities IS
  'Market survey config (maintainer decision 2026-09-14): the cities the procurement director phones each morning for goat and sheep prices. Authored on /sales/config; each active city is one card per day on the phone.';
COMMENT ON TABLE public.market_questions IS
  'Market survey config: what is asked in every city -- a label and the unit the price is quoted in. Editable at any time; entries snapshot the label and unit they answered.';
COMMENT ON TABLE public.market_price_entries IS
  'Market survey facts: one price per (city, question, business day), with the city name, question label and unit frozen at recording time so a later config edit never rewrites history.';

-- The six questions the farm asks today, for every existing tenant. Read-only over tenants.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=read-only-select-over-tenants-to-seed-default-questions expiry=2026-12-31
INSERT INTO public.market_questions (tenant_id, label, unit_label, sort_order)
SELECT t.tenant_id, q.label, q.unit_label, q.sort_order
FROM public.tenants t
CROSS JOIN (VALUES
    ('Goat live price',    '₹/kg', 10),
    ('Sheep live price',   '₹/kg', 20),
    ('Goat carcass price', '₹/kg', 30),
    ('Sheep carcass price','₹/kg', 40),
    ('Goat offals price',  '₹/kg', 50),
    ('Sheep offals price', '₹/kg', 60)
) AS q (label, unit_label, sort_order);

-- Notification vocabulary: the morning push telling the director today's market calls are due
-- rides 'market_survey_due'. Enum widening on a table no seed path writes; the lock-timeout /
-- NOT VALID / VALIDATE shape is 000252's, 000279's, 000289's and 000301's.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text,
    'leave_request_raised'::text,
    'leave_request_decided'::text,
    'animal_purchase_decided'::text,
    'market_survey_due'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- market_reporter is a PER-PERSON authority grant (the toxin_tester shape, 000211): the named
-- person who phones the markets holds it alongside their job role. It carries
-- sales.market.read + sales.market.entry ONLY -- never the config write -- and is never anyone's
-- primary job, so no workforce_members_role_hint_check change.
INSERT INTO public.org_role_catalog (role_key, tier_code, vertical_code, is_legacy, label, created_at)
VALUES ('market_reporter', 'director', NULL, true, 'Market Reporter', now())
ON CONFLICT (role_key) DO UPDATE
SET is_legacy = true,
    label = EXCLUDED.label;

-- The Sales module gained a CONFIGURE level today (what the market is asked: cities and
-- questions), held by the CEO/CXO and the Procurement Director. Person rows DECIDE after the
-- 2026-08-24 cutover and the role map is dead data for anyone already migrated, so the two roles'
-- existing web `sales` rows get the level here -- the 000245 shape, additive only. The CEO floor
-- makes ceo_internal need no row; the Procurement Director does.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-capability-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access pma
   SET capabilities = array_append(pma.capabilities, 'configure')
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('ceo_internal', 'procurement_director')
 WHERE pma.tenant_id = m.tenant_id
   AND pma.workforce_member_id = m.workforce_member_id
   AND pma.surface = 'web'
   AND pma.module_key = 'sales'
   AND m.status = 'active'
   AND NOT ('configure' = ANY (pma.capabilities));

-- THE REPORTER'S PHONE TICK. The market_reporter grant is seeded per person by name
-- (seed-stg-login-grants perPersonGrants: today the Procurement Director), but person rows DECIDE
-- at request time for anyone already migrated, so the grant alone opens nothing until a mobile
-- `market_survey` row exists. This writes that row, once, for every migrated person holding the
-- procurement_director grant today -- the one desk the maintainer named -- and for any
-- market_reporter grant already present. It is a one-time repair for people migrated before the
-- module existed; from here on /people ticks it like any other module, and a FUTURE procurement
-- director inherits nothing from this statement because it runs once.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=one-time-mobile-tick-for-the-named-reporter expiry=2026-12-31
INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'mobile', 'market_survey', ARRAY['do']::text[], now(), '{}'::text[]
FROM public.workforce_members m
JOIN public.user_scope_grants g
  ON g.tenant_id = m.tenant_id
 AND g.user_id = m.user_id
 AND g.status = 'active'
 AND (g.valid_to IS NULL OR g.valid_to > now())
 AND g.role IN ('procurement_director', 'market_reporter')
WHERE m.status = 'active'
  AND m.user_id IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM public.person_access pa
    WHERE pa.tenant_id = m.tenant_id
      AND pa.workforce_member_id = m.workforce_member_id
  )
ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING;

-- Give the people who already hold the web Sales module the new Market analytics page tick, the
-- 000245 shape: person rows DECIDE after the 2026-08-24 cutover, and a person whose sales row
-- names specific pages would otherwise never see a page shipped after their rows were written.
-- Additive only; the CEO floor makes ceo_internal need no row.
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-page-tick-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_append(pages, 'sales-market-analytics')
 WHERE surface = 'web'
   AND module_key = 'sales'
   AND pages IS NOT NULL
   AND array_length(pages, 1) > 0
   AND NOT ('sales-market-analytics' = ANY (pages));

-- +goose Down
DELETE FROM public.person_module_access WHERE module_key = 'market_survey';
DELETE FROM public.user_scope_grants WHERE role = 'market_reporter';
DELETE FROM public.auth_pending_email_grants WHERE role = 'market_reporter';
DELETE FROM public.org_role_catalog WHERE role_key = 'market_reporter';
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=additive-page-tick-for-existing-sales-module-holders expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_remove(pages, 'sales-market-analytics')
 WHERE surface = 'web' AND module_key = 'sales' AND 'sales-market-analytics' = ANY (pages);

SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text,
    'leave_request_raised'::text,
    'leave_request_decided'::text,
    'animal_purchase_decided'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

DROP TABLE IF EXISTS public.market_price_entries;
DROP TABLE IF EXISTS public.market_questions;
DROP TABLE IF EXISTS public.market_cities;
