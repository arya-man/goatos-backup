-- +goose Up
-- seed-fixture-guard:ignore: HRMS timetable config tables (shift names, per-park shift timings,
-- per-person shift) plus per-person access ticks; no source fixture schema, no vaccination/herd
-- seed contract and no read-model change.
--
-- HRMS TIMETABLE (maintainer request 2026-09-30). People / HRMS gains a Timetable page: pick a
-- park, see everyone who works there, which shift each person is on and what that shift's
-- working hours are. HR and the CEO change it (park heads edit later, on the phone -- maintainer
-- answer the same day). Three facts, three tables:
--
--  1. WHICH SHIFTS EXIST is one list for the whole farm (Morning, General, Second). It is data,
--     not a Go constant, so a fourth shift is a row, not a deploy.
--  2. WHEN A SHIFT RUNS is per park, because it differs by park: the Morning shift starts at
--     06:00 in Coimbatore but 07:00 in Channapatna (elephant risk in the early morning). A park
--     added tomorrow has no rows and the page offers "Set time" for each shift. A time may be
--     only half-known (the Morning shift has a start and no stated end), so both ends are
--     nullable and the page says "End not set" instead of inventing one.
--  3. WHO IS ON WHICH SHIFT is one row per person carrying only the shift CODE. The hours come
--     from the person's home park, so a person moved between parks keeps their shift and reads
--     the new park's hours with no second edit.

CREATE TABLE IF NOT EXISTS public.workforce_shift_catalog (
  shift_code  text PRIMARY KEY,
  label       text NOT NULL,
  sort_order  integer NOT NULL DEFAULT 0,
  status      text NOT NULL DEFAULT 'active',
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workforce_shift_catalog_code_check CHECK (shift_code ~ '^[a-z][a-z0-9_]{0,39}$'),
  CONSTRAINT workforce_shift_catalog_label_check CHECK (length(btrim(label)) BETWEEN 1 AND 60),
  CONSTRAINT workforce_shift_catalog_status_check CHECK (status IN ('active', 'retired'))
);

COMMENT ON TABLE public.workforce_shift_catalog IS
  'The farm''s named work shifts (HRMS Timetable, 2026-09-30). Hours are per park in workforce_park_shift_timings.';

INSERT INTO public.workforce_shift_catalog (shift_code, label, sort_order)
VALUES
  ('morning', 'Morning shift', 10),
  ('general', 'General shift', 20),
  ('second',  'Second shift',  30)
ON CONFLICT (shift_code) DO NOTHING;

-- Minutes after IST midnight. A shift's END may be 1440 (midnight: the Second shift runs
-- 15:00-24:00) and may be earlier than its start (a shift that crosses midnight); it may never
-- equal its start. An end without a start is refused: "ends at 2 pm" alone is not a timing.
CREATE TABLE IF NOT EXISTS public.workforce_park_shift_timings (
  tenant_id     uuid NOT NULL,
  park_id       uuid NOT NULL,
  shift_code    text NOT NULL,
  start_minute  integer,
  end_minute    integer,
  updated_by    uuid,
  updated_at    timestamptz NOT NULL DEFAULT now(),
  row_version   integer NOT NULL DEFAULT 1,
  CONSTRAINT workforce_park_shift_timings_pkey PRIMARY KEY (tenant_id, park_id, shift_code),
  CONSTRAINT workforce_park_shift_timings_park_fk
    FOREIGN KEY (park_id) REFERENCES public.locations (location_id),
  CONSTRAINT workforce_park_shift_timings_shift_fk
    FOREIGN KEY (shift_code) REFERENCES public.workforce_shift_catalog (shift_code),
  CONSTRAINT workforce_park_shift_timings_start_check
    CHECK (start_minute IS NULL OR (start_minute >= 0 AND start_minute <= 1439)),
  CONSTRAINT workforce_park_shift_timings_end_check
    CHECK (end_minute IS NULL OR (end_minute >= 1 AND end_minute <= 1440)),
  CONSTRAINT workforce_park_shift_timings_end_needs_start_check
    CHECK (end_minute IS NULL OR start_minute IS NOT NULL),
  CONSTRAINT workforce_park_shift_timings_not_empty_check
    CHECK (end_minute IS NULL OR end_minute % 1440 <> start_minute),
  CONSTRAINT workforce_park_shift_timings_row_version_check CHECK (row_version >= 1)
);

COMMENT ON TABLE public.workforce_park_shift_timings IS
  'When each shift runs at each park (minutes after IST midnight; end may be 1440 or cross midnight). HR/CEO edit on People / HRMS > Timetable.';

-- The two parks' stated timings (maintainer message 2026-09-30). The Morning shift was given a
-- start only; its end is left unset for HR to fill on the page (maintainer answer, same day).
--   Morning  CBE 06:00, CPT 07:00 (elephant risk in the early morning)
--   General  CBE & CPT 08:30-18:00
--   Second   CBE & CPT 15:00-24:00
-- Keyed on the park's own code, so a tenant without these parks gets nothing seeded and its
-- page offers "Set time" for every shift.
-- seed-migration-guard:ignore owner=manohark issue=hrms-timetable reason=maintainer-stated-shift-timings-seeded-in-place-no-fixture-carries-them expiry=2026-12-31
INSERT INTO public.workforce_park_shift_timings (tenant_id, park_id, shift_code, start_minute, end_minute)
SELECT l.tenant_id, l.location_id, s.shift_code, s.start_minute, s.end_minute
FROM public.locations l
JOIN (VALUES
  ('CBE', 'morning', 360,  NULL::integer),
  ('CPT', 'morning', 420,  NULL::integer),
  ('CBE', 'general', 510,  1080),
  ('CPT', 'general', 510,  1080),
  ('CBE', 'second',  900,  1440),
  ('CPT', 'second',  900,  1440)
) AS s(park_code, shift_code, start_minute, end_minute)
  ON upper(l.location_code) = s.park_code
WHERE l.location_type = 'park'
  AND l.status = 'active'
ON CONFLICT (tenant_id, park_id, shift_code) DO NOTHING;

-- One shift per person. Absent row = no shift assigned yet ("Not assigned" on the page).
CREATE TABLE IF NOT EXISTS public.workforce_member_shifts (
  tenant_id            uuid NOT NULL,
  workforce_member_id  uuid NOT NULL,
  shift_code           text NOT NULL,
  updated_by           uuid,
  updated_at           timestamptz NOT NULL DEFAULT now(),
  row_version          integer NOT NULL DEFAULT 1,
  CONSTRAINT workforce_member_shifts_pkey PRIMARY KEY (tenant_id, workforce_member_id),
  CONSTRAINT workforce_member_shifts_member_fk
    FOREIGN KEY (workforce_member_id) REFERENCES public.workforce_members (workforce_member_id),
  CONSTRAINT workforce_member_shifts_shift_fk
    FOREIGN KEY (shift_code) REFERENCES public.workforce_shift_catalog (shift_code),
  CONSTRAINT workforce_member_shifts_row_version_check CHECK (row_version >= 1)
);

-- The shift filter on the page reads "everyone on the General shift" per tenant.
CREATE INDEX IF NOT EXISTS workforce_member_shifts_tenant_shift_idx
  ON public.workforce_member_shifts (tenant_id, shift_code);

COMMENT ON TABLE public.workforce_member_shifts IS
  'Which shift each person works (HRMS Timetable, 2026-09-30). Hours resolve through the person''s home park.';

-- PER-PERSON ACCESS (the 000245 / 000454 shape). A person's stored person_module_access rows
-- decide their permissions and menus; the role map in capability_backfill.go is dead data for
-- anyone the 2026-08-24 cutover migrated. HR and the CEO edit the timetable, so both get the new
-- web-only `timetable` module at configure. ADDITIVE ONLY, only for people already migrated,
-- and remembered in a ledger so Down removes exactly these rows and never a tick an admin adds
-- by hand afterwards.
CREATE TABLE IF NOT EXISTS public.person_module_access_timetable_backfill (
  tenant_id           uuid NOT NULL,
  workforce_member_id uuid NOT NULL,
  PRIMARY KEY (tenant_id, workforce_member_id)
);

WITH inserted AS (
  INSERT INTO public.person_module_access (tenant_id, workforce_member_id, surface, module_key, capabilities, updated_at, pages)
  SELECT DISTINCT m.tenant_id, m.workforce_member_id, 'web', 'timetable', ARRAY['view', 'configure']::text[], now(), '{}'::text[]
  FROM public.workforce_members m
  JOIN public.user_scope_grants g
    ON g.tenant_id = m.tenant_id
   AND g.user_id = m.user_id
   AND g.status = 'active'
   AND (g.valid_to IS NULL OR g.valid_to > now())
   AND g.role IN ('hr', 'ceo_internal')
  WHERE m.status = 'active'
    AND m.user_id IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM public.person_access pa
      WHERE pa.tenant_id = m.tenant_id
        AND pa.workforce_member_id = m.workforce_member_id
    )
  ON CONFLICT (tenant_id, workforce_member_id, surface, module_key) DO NOTHING
  RETURNING tenant_id, workforce_member_id
)
INSERT INTO public.person_module_access_timetable_backfill (tenant_id, workforce_member_id)
SELECT tenant_id, workforce_member_id FROM inserted
ON CONFLICT DO NOTHING;

-- The same tick as the HR job's default, so a person hired onto the HR designation tomorrow
-- reaches the page without somebody remembering to tick it.
CREATE TABLE IF NOT EXISTS public.designation_module_defaults_timetable_backfill (
  designation_code text PRIMARY KEY
);

WITH inserted AS (
  INSERT INTO public.designation_module_defaults (designation_code, surface, module_key, capabilities, pages)
  SELECT d.designation_code, 'web', 'timetable', ARRAY['view', 'configure']::text[], '{}'::text[]
  FROM public.designation_catalog d
  WHERE d.designation_code = 'hr'
  ON CONFLICT (designation_code, surface, module_key) DO NOTHING
  RETURNING designation_code
)
INSERT INTO public.designation_module_defaults_timetable_backfill (designation_code)
SELECT designation_code FROM inserted
ON CONFLICT DO NOTHING;

-- THE PEOPLE PAGE IS SPLIT (same request): "Clock In / Out", "Notifications" and "Vaccination
-- operators" were TABS of the one /people page and are now pages of their own in the HRMS group.
-- An EMPTY tick list already means every page of the module; a person whose People row names
-- its pages explicitly ticked `people` -- which carried all three tabs -- so the three new keys
-- are appended wherever `people` is ticked. Nobody gains a view they did not already have.
-- seed-migration-guard:ignore owner=manohark issue=hrms-timetable reason=split-people-tabs-into-pages-for-existing-people-tickers expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = pages || ARRAY(
         SELECT k FROM unnest(ARRAY['people-clock', 'people-notifications', 'people-vaccination']) AS k
         WHERE NOT (k = ANY (pages)))
 WHERE surface = 'web'
   AND module_key = 'people'
   AND pages IS NOT NULL
   AND 'people' = ANY (pages);

-- seed-migration-guard:ignore owner=manohark issue=hrms-timetable reason=split-people-tabs-into-pages-for-designation-templates expiry=2026-12-31
UPDATE public.designation_module_defaults
   SET pages = pages || ARRAY(
         SELECT k FROM unnest(ARRAY['people-clock', 'people-notifications', 'people-vaccination']) AS k
         WHERE NOT (k = ANY (pages)))
 WHERE surface = 'web'
   AND module_key = 'people'
   AND pages IS NOT NULL
   AND 'people' = ANY (pages);

-- +goose Down
-- seed-migration-guard:ignore owner=manohark issue=hrms-timetable reason=split-people-tabs-into-pages-for-existing-people-tickers expiry=2026-12-31
UPDATE public.person_module_access
   SET pages = array_remove(array_remove(array_remove(pages, 'people-clock'), 'people-notifications'), 'people-vaccination')
 WHERE surface = 'web' AND module_key = 'people';
-- seed-migration-guard:ignore owner=manohark issue=hrms-timetable reason=split-people-tabs-into-pages-for-designation-templates expiry=2026-12-31
UPDATE public.designation_module_defaults
   SET pages = array_remove(array_remove(array_remove(pages, 'people-clock'), 'people-notifications'), 'people-vaccination')
 WHERE surface = 'web' AND module_key = 'people';

DELETE FROM public.designation_module_defaults d
USING public.designation_module_defaults_timetable_backfill b
WHERE d.designation_code = b.designation_code
  AND d.surface = 'web'
  AND d.module_key = 'timetable';
DROP TABLE IF EXISTS public.designation_module_defaults_timetable_backfill;

DELETE FROM public.person_module_access p
USING public.person_module_access_timetable_backfill b
WHERE p.tenant_id = b.tenant_id
  AND p.workforce_member_id = b.workforce_member_id
  AND p.surface = 'web'
  AND p.module_key = 'timetable';
DROP TABLE IF EXISTS public.person_module_access_timetable_backfill;

DROP TABLE IF EXISTS public.workforce_member_shifts;
DROP TABLE IF EXISTS public.workforce_park_shift_timings;
DROP TABLE IF EXISTS public.workforce_shift_catalog;
