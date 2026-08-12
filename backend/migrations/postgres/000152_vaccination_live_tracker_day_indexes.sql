-- +goose Up
-- The live drive tracker answers ONE business day and re-reads it every poll, per viewer. Its
-- predicates are now half-open timestamptz ranges instead of (<ts> AT TIME ZONE 'Asia/Kolkata')::date
-- equality, which is the only shape an index can drive; these are the indexes that shape needs.
--
-- Without them each of these append-only tables — which grow with herd x doses x years — was read in
-- full for a one-day answer, and obligation_instances was scanned tenant-wide and joined to goats,
-- partitions and the whole protocol chain before the date filter was applied at all.

-- Membership: the day's goat-targeted, still-live obligations. tenant+due_at is the entry the scoped
-- CTE now bounds on; the partial predicate mirrors the read's own dead-status exclusion exactly.
CREATE INDEX IF NOT EXISTS obligation_instances_vaccination_drive_day_idx
  ON public.obligation_instances (tenant_id, due_at)
  WHERE target_type = 'goat'
    AND status <> ALL (ARRAY['canceled'::text, 'superseded'::text, 'waived'::text]);

-- Operator of record per shed x partition for the day. vaccination_drive_assignments already had
-- (tenant_id, park_id, planned_date), whose second column the tracker never binds, so the day slice
-- was not even a scan boundary.
CREATE INDEX IF NOT EXISTS vaccination_drive_assignments_day_shed_idx
  ON public.vaccination_drive_assignments (tenant_id, planned_date, shed_id);

-- Field evidence for the day.
CREATE INDEX IF NOT EXISTS proof_artifacts_vaccination_day_idx
  ON public.proof_artifacts (tenant_id, uploaded_at)
  INCLUDE (scope_id, subject_id, uploaded_by, upload_state)
  WHERE subject_type = 'goat' AND proof_type = 'video';

-- The not-yet-uploaded arm of COALESCE(uploaded_at, created_at) reads created_at instead.
CREATE INDEX IF NOT EXISTS proof_artifacts_vaccination_created_day_idx
  ON public.proof_artifacts (tenant_id, created_at)
  WHERE subject_type = 'goat' AND proof_type = 'video' AND uploaded_at IS NULL;

CREATE INDEX IF NOT EXISTS sop_task_scan_captures_day_idx
  ON public.sop_task_scan_captures (tenant_id, captured_at);

CREATE INDEX IF NOT EXISTS sop_task_scan_attempts_day_idx
  ON public.sop_task_scan_attempts (tenant_id, captured_at);

CREATE INDEX IF NOT EXISTS vaccination_completions_day_idx
  ON public.vaccination_completions (tenant_id, administered_at);

-- The feed filters occurred_at while the existing tenant/type index is keyed on recorded_at, so the
-- seek stopped after two columns and read every 'completed' event ever recorded for the tenant.
CREATE INDEX IF NOT EXISTS obligation_status_events_day_idx
  ON public.obligation_status_events (tenant_id, event_type, occurred_at);

-- Verification block. The pending counters are a STANDING queue by design and carry no date bound at
-- all, so they need a partial index or they are a full-history scan on every 10s poll; the
-- approved/rejected counters are day-scoped through verified_at.
CREATE INDEX IF NOT EXISTS verification_items_pending_scope_idx
  ON public.verification_items (tenant_id, park_id, shed_id)
  WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS verification_items_verified_day_idx
  ON public.verification_items (tenant_id, verified_at)
  WHERE verified_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS public.verification_items_verified_day_idx;
DROP INDEX IF EXISTS public.verification_items_pending_scope_idx;
DROP INDEX IF EXISTS public.obligation_status_events_day_idx;
DROP INDEX IF EXISTS public.vaccination_completions_day_idx;
DROP INDEX IF EXISTS public.sop_task_scan_attempts_day_idx;
DROP INDEX IF EXISTS public.sop_task_scan_captures_day_idx;
DROP INDEX IF EXISTS public.proof_artifacts_vaccination_created_day_idx;
DROP INDEX IF EXISTS public.proof_artifacts_vaccination_day_idx;
DROP INDEX IF EXISTS public.vaccination_drive_assignments_day_shed_idx;
DROP INDEX IF EXISTS public.obligation_instances_vaccination_drive_day_idx;
