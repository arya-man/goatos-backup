-- +goose Up
-- MARKET SURVEY CALL TIME (maintainer decision 2026-09-14, same day as 000304): ONE configured
-- local time per tenant at which the morning market calls open. At that time -- and not before --
-- the reporter's phone shows the day's city cards AND the reminder push goes out. Before it, the
-- day view is empty with "opens at HH:MM", and a save is refused. Authored on Sales Config beside
-- the cities and questions; the code default was 08:00 IST, so every existing tenant is seeded
-- with that and nothing changes on deploy. Tenants are created only by the baseline (no seed
-- command inserts tenants), so there is no seed path to couple; a future tenant-creation path
-- must insert this row too -- the 000276 shape.
CREATE TABLE public.market_survey_config (
    tenant_id   uuid        NOT NULL REFERENCES public.tenants (tenant_id),
    -- LOCAL Asia/Kolkata wall-clock time; the business day is IST by rule.
    call_time   time        NOT NULL DEFAULT TIME '08:00',
    updated_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT market_survey_config_pkey PRIMARY KEY (tenant_id)
);

COMMENT ON TABLE public.market_survey_config IS
  'Market survey (maintainer decision 2026-09-14): the ONE local IST time each day at which the city cards appear on the phone and the reminder push is sent. Readers fall back to 08:00 when the row is missing.';

-- seed-migration-guard:ignore owner=manohark issue=market-survey reason=read-only-select-over-tenants-to-seed-the-new-config-row expiry=2026-12-31
INSERT INTO public.market_survey_config (tenant_id, call_time)
SELECT tenant_id, TIME '08:00'
FROM public.tenants
ON CONFLICT (tenant_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS public.market_survey_config;
