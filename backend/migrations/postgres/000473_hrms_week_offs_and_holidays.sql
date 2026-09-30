-- +goose Up
-- seed-fixture-guard:ignore: HRMS weekly offs + park holidays (roster facts HR enters), the
-- clock-in check's start date, and a system close of a waiting violation; no source fixture schema.
--
-- WEEKLY OFFS AND HOLIDAYS (maintainer decisions 2026-09-30):
--  * Each person has a FIXED weekly off (one or more weekdays), set by HR on the Timetable beside
--    their shift. The clock-in check never raises a violation on it.
--  * HR enters HOLIDAYS: a date, for every park or for chosen parks. None exist today -- the farm
--    runs every day -- but when one is entered nobody at those parks is checked that day.
--  * The check STARTS the day after it goes live (the SOP's `starts_on`), so switching it on never
--    raises a flood of old days, and it covers only people with an app login (they are the only
--    ones who CAN clock in).
--  * A waiting violation whose day later becomes a weekly off, a holiday or a day the person applied
--    for leave closes ITSELF with that reason -- the check, not HR, raised it for a day it now owes
--    nothing on.

ALTER TABLE public.workforce_member_shifts
  ADD COLUMN IF NOT EXISTS week_offs smallint[] NOT NULL DEFAULT '{}';
ALTER TABLE public.workforce_member_shifts ADD CONSTRAINT workforce_member_shifts_week_offs_check
  CHECK (week_offs <@ ARRAY[1, 2, 3, 4, 5, 6, 7]::smallint[] AND cardinality(week_offs) <= 6);

CREATE TABLE IF NOT EXISTS public.workforce_holidays (
  holiday_id   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    uuid NOT NULL,
  holiday_on   date NOT NULL,
  -- NULL = every park.
  park_id      uuid,
  label        text NOT NULL,
  status       text NOT NULL DEFAULT 'active',
  created_by   uuid NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  removed_by   uuid,
  removed_at   timestamptz,
  row_version  integer NOT NULL DEFAULT 1,
  CONSTRAINT workforce_holidays_park_fk FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT workforce_holidays_label_check CHECK (length(btrim(label)) BETWEEN 1 AND 80),
  CONSTRAINT workforce_holidays_status_check CHECK (status IN ('active', 'removed')),
  CONSTRAINT workforce_holidays_removed_check CHECK (status = 'active' OR (removed_by IS NOT NULL AND removed_at IS NOT NULL))
);
-- One active holiday per date per park (all-parks counted as its own scope).
CREATE UNIQUE INDEX IF NOT EXISTS workforce_holidays_active_uq
  ON public.workforce_holidays (tenant_id, holiday_on, COALESCE(park_id, '00000000-0000-0000-0000-000000000000'::uuid))
  WHERE status = 'active';
CREATE INDEX IF NOT EXISTS workforce_holidays_date_idx
  ON public.workforce_holidays (tenant_id, holiday_on) WHERE status = 'active';

COMMENT ON TABLE public.workforce_holidays IS
  'HRMS holidays HR enters: a date for every park (park_id NULL) or one park. The clock-in check skips them. 2026-09-30.';

-- A waiting violation the check closes itself (leave / holiday / weekly off entered later) has no
-- human decider.
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_closed_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_closed_check
  CHECK (status <> 'closed' OR (decided_at IS NOT NULL AND length(btrim(decision_note)) > 0));

-- The clock-in check starts TOMORROW (IST) on every version that has one and no start yet.
-- seed-migration-guard:ignore owner=manohark issue=hrms-attendance reason=hrms-sop-in-place-patch-no-fixture-carries-it expiry=2026-12-31
UPDATE public.sop_versions sv
SET form_dsl = jsonb_set(sv.form_dsl, '{violations,attendance,starts_on}',
      to_jsonb(to_char((now() AT TIME ZONE 'Asia/Kolkata')::date + 1, 'YYYY-MM-DD')))
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id AND sd.code = 'hrms.violations'
  AND sv.form_dsl->'violations' ? 'attendance'
  AND NOT (sv.form_dsl->'violations'->'attendance' ? 'starts_on');

-- +goose Down
UPDATE public.sop_versions sv
SET form_dsl = sv.form_dsl #- '{violations,attendance,starts_on}'
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id AND sd.code = 'hrms.violations' AND sv.form_dsl->'violations' ? 'attendance';

UPDATE public.workforce_violations SET decided_by = recorded_by WHERE status = 'closed' AND decided_by IS NULL AND recorded_by IS NOT NULL;
DELETE FROM public.workforce_violations WHERE status = 'closed' AND decided_by IS NULL;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_closed_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_closed_check
  CHECK (status <> 'closed' OR (decided_by IS NOT NULL AND decided_at IS NOT NULL AND length(btrim(decision_note)) > 0));

DROP TABLE IF EXISTS public.workforce_holidays;
ALTER TABLE public.workforce_member_shifts DROP CONSTRAINT IF EXISTS workforce_member_shifts_week_offs_check;
ALTER TABLE public.workforce_member_shifts DROP COLUMN IF EXISTS week_offs;
