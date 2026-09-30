-- +goose Up
-- seed-fixture-guard:ignore: HRMS attendance violations (columns + checks on workforce_violations)
-- and an in-place patch of the HRMS SOP document; no source fixture schema.
--
-- AUTOMATIC CLOCK-IN VIOLATIONS (maintainer decisions 2026-09-30).
--
--  * A person mapped to a shift who clocks in more than the grace (15 minutes, authored on the
--    HRMS SOP) after the shift's start gets a LATE violation; one who does not clock in at all by
--    the shift's end gets a separate DID-NOT-CLOCK-IN violation. A day covered by leave the person
--    applied for (pending or approved) is never checked.
--  * An automatic violation WAITS FOR HR (status 'pending'): HR KEEPS it (it becomes 'recorded',
--    optionally with a fine HR types -- a mistake carries no money of its own) or CLOSES it with a
--    reason ('closed'). Hand-recorded and enquiry violations stay final when recorded.
--  * One per person, day and kind: the check runs every few minutes and re-reads the last two
--    days, so a replay or an overlapping tick inserts nothing twice.

ALTER TABLE public.workforce_violations
  ADD COLUMN IF NOT EXISTS attendance_kind text,
  ADD COLUMN IF NOT EXISTS shift_code      text,
  -- Backend-composed at detection ("Clocked in 07:42 · shift starts 07:00"), so no reader re-derives it.
  ADD COLUMN IF NOT EXISTS detail          text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS decided_by      uuid,
  ADD COLUMN IF NOT EXISTS decided_at      timestamptz,
  ADD COLUMN IF NOT EXISTS decision_note   text;

-- The check records a violation with no human recorder.
ALTER TABLE public.workforce_violations ALTER COLUMN recorded_by DROP NOT NULL;

ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_source_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_source_check
  CHECK (source IN ('manual', 'enquiry', 'attendance'));
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_status_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_status_check
  CHECK (status IN ('pending', 'recorded', 'withdrawn', 'closed'));
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_withdrawn_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_withdrawn_check
  CHECK (status <> 'withdrawn' OR (withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL AND length(btrim(withdraw_reason)) > 0));
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_closed_check
  CHECK (status <> 'closed' OR (decided_by IS NOT NULL AND decided_at IS NOT NULL AND length(btrim(decision_note)) > 0));
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_pending_source_check
  CHECK (status NOT IN ('pending', 'closed') OR source = 'attendance');
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_attendance_kind_check
  CHECK ((source = 'attendance') = (attendance_kind IS NOT NULL) AND (attendance_kind IS NULL OR attendance_kind IN ('late', 'absent')));
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_recorder_check
  CHECK (source = 'attendance' OR recorded_by IS NOT NULL);

-- The natural key of an automatic violation: one per person, business day and kind.
CREATE UNIQUE INDEX IF NOT EXISTS workforce_violations_attendance_uq
  ON public.workforce_violations (tenant_id, workforce_member_id, occurred_on, attendance_kind)
  WHERE source = 'attendance';
-- HR's queue: what is still waiting.
CREATE INDEX IF NOT EXISTS workforce_violations_pending_idx
  ON public.workforce_violations (tenant_id, occurred_on DESC, violation_id DESC)
  WHERE status = 'pending';

-- THE HRMS SOP gains the clock-in check IN PLACE on every version (the 000315 shape): the two
-- types are appended when absent, and the check is added where the version has none, so a
-- version HR already published keeps every type and question it had.
-- seed-migration-guard:ignore owner=manohark issue=hrms-attendance reason=hrms-sop-in-place-patch-no-fixture-carries-it expiry=2026-12-31
UPDATE public.sop_versions sv
SET form_dsl = jsonb_set(
      jsonb_set(sv.form_dsl, '{violations,violation_types}',
        COALESCE(sv.form_dsl->'violations'->'violation_types', '[]'::jsonb) || COALESCE((
          SELECT jsonb_agg(t) FROM jsonb_array_elements($types$[
  {"key": "late_clock_in", "title": "Late clock-in", "active": true},
  {"key": "did_not_clock_in", "title": "Did not clock in", "active": true}
]$types$::jsonb) t
          WHERE NOT EXISTS (
            SELECT 1 FROM jsonb_array_elements(COALESCE(sv.form_dsl->'violations'->'violation_types', '[]'::jsonb)) e
            WHERE e->>'key' = t->>'key')), '[]'::jsonb)),
      '{violations,attendance}',
      $att${"grace_minutes": 15, "late_type": "late_clock_in", "absent_type": "did_not_clock_in"}$att$::jsonb)
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id AND sd.code = 'hrms.violations'
  AND sv.form_dsl ? 'violations' AND NOT (sv.form_dsl->'violations' ? 'attendance');

-- The SOP's description said "their fines"; a type carries none (2026-09-30).
UPDATE public.sop_definitions
SET description = 'The violation types, the clock-in check, and the enquiries farm events open: who fills them, their questions and deadline.'
WHERE code = 'hrms.violations';

-- +goose Down
UPDATE public.sop_versions sv
SET form_dsl = jsonb_set(sv.form_dsl #- '{violations,attendance}', '{violations,violation_types}',
  COALESCE((SELECT jsonb_agg(t) FROM jsonb_array_elements(sv.form_dsl->'violations'->'violation_types') t
            WHERE t->>'key' NOT IN ('late_clock_in', 'did_not_clock_in')), '[]'::jsonb))
FROM public.sop_definitions sd
WHERE sd.sop_id = sv.sop_id AND sd.code = 'hrms.violations' AND sv.form_dsl ? 'violations';

DELETE FROM public.workforce_violations WHERE source = 'attendance';
DROP INDEX IF EXISTS public.workforce_violations_pending_idx;
DROP INDEX IF EXISTS public.workforce_violations_attendance_uq;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_recorder_check;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_attendance_kind_check;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_pending_source_check;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_closed_check;
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_withdrawn_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_withdrawn_check
  CHECK (status = 'recorded' OR (withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL AND length(btrim(withdraw_reason)) > 0));
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_status_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_status_check
  CHECK (status IN ('recorded', 'withdrawn'));
ALTER TABLE public.workforce_violations DROP CONSTRAINT IF EXISTS workforce_violations_source_check;
ALTER TABLE public.workforce_violations ADD CONSTRAINT workforce_violations_source_check
  CHECK (source IN ('manual', 'enquiry'));
ALTER TABLE public.workforce_violations ALTER COLUMN recorded_by SET NOT NULL;
ALTER TABLE public.workforce_violations
  DROP COLUMN IF EXISTS decision_note,
  DROP COLUMN IF EXISTS decided_at,
  DROP COLUMN IF EXISTS decided_by,
  DROP COLUMN IF EXISTS detail,
  DROP COLUMN IF EXISTS shift_code,
  DROP COLUMN IF EXISTS attendance_kind;
