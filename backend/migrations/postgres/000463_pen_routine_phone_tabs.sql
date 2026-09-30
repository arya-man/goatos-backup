-- +goose Up
-- 000463_pen_routine_phone_tabs.sql
--
-- SIMPLE TASKS ON THEIR OWN PHONE TAB (maintainer instruction 2026-10-01, docs/decisions/simple-task-phone-tabs.md):
-- "if I add one more simple task -- like fumigation, just two videos -- I should not code; I
-- should define it on the web, add an icon, choose the filters". The work itself is a pen
-- routine (000328): scope, cadence, assignee, questions, photos/videos, verifier. What a routine
-- could not say is WHERE it appears on the phone -- every routine landed in the one "Routines"
-- tab. A PHONE TAB is that placement, authored on /routines: a label, the phone module whose
-- bottom bar carries it, an icon from the closed set the app ships, and which list filters the
-- tab offers. Routines are placed on a tab by pointing at it; a routine on no tab stays in
-- Routines only.
--
-- The module, icon and filter keys are closed vocabularies owned by the Go domain
-- (penroutines/domain/tab.go), because the phone must ship an icon for every key it is sent.
--
-- seed-fixture-guard:ignore: authored placement config (like the routines themselves), never
-- seeded; no source fixture or read-model table changes.
CREATE TABLE public.pen_routine_tabs (
    tab_id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES public.tenants (tenant_id),
    tab_key     text NOT NULL CHECK (tab_key ~ '^[a-z][a-z0-9_]{1,39}$'),
    label       text NOT NULL CHECK (length(btrim(label)) BETWEEN 1 AND 24),
    module_key  text NOT NULL CHECK (length(module_key) BETWEEN 1 AND 40),
    icon_key    text NOT NULL CHECK (length(icon_key) BETWEEN 1 AND 40),
    filters     text[] NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'retired')),
    row_version integer NOT NULL DEFAULT 1 CHECK (row_version >= 1),
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT pen_routine_tabs_key_uq UNIQUE (tenant_id, tab_key)
);

-- Which tab a routine appears on. NULL = the Routines tab only. pen_routine_definitions holds
-- the authored routine estate (a handful per park), so the constraint validates instantly.
ALTER TABLE public.pen_routine_definitions ADD COLUMN tab_id uuid;
ALTER TABLE public.pen_routine_definitions
    ADD CONSTRAINT pen_routine_definitions_tab_fk FOREIGN KEY (tab_id)
    REFERENCES public.pen_routine_tabs (tab_id) NOT VALID;
ALTER TABLE public.pen_routine_definitions VALIDATE CONSTRAINT pen_routine_definitions_tab_fk;

-- +goose Down
ALTER TABLE public.pen_routine_definitions DROP CONSTRAINT IF EXISTS pen_routine_definitions_tab_fk;
ALTER TABLE public.pen_routine_definitions DROP COLUMN IF EXISTS tab_id;
DROP TABLE IF EXISTS public.pen_routine_tabs;
