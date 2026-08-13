-- +goose Up
-- seed-fixture-guard:ignore: operational Weighing free-flow kernel work items are written by the Weighing publish transaction and the kernel worker; they do not change the Vaccination HRMS seed contract
--
-- WEIGHING PHASE 2 — the time-driven kernel.
--
-- Before this migration Weighing had NO durable work item and NO cadence: a
-- published campaign was a plan with no obligation behind it, so nothing could
-- roll forward, nothing could go delayed, nothing could escalate, and the
-- kernel worker had zero weighing awareness. Calendar and Control Tower had no
-- weighing process state to render at all.
--
-- `weighing_work_items` is the weighing equivalent of an obligation instance.
-- Grain: ONE ROW PER `weighing_campaign_sheds` BUCKET (never per animal, never
-- per campaign). One bucket has exactly one operator, so a work item has exactly
-- one owner and `UNIQUE (tenant_id, campaign_shed_id)` makes publish/republish
-- and an exact event replay create no duplicates.
--
-- Time grain is the Asia/Kolkata BUSINESS DAY (`date`), never an instant and
-- never `now ± N hours`:
--   planned_business_date — the ORIGINAL planned date. IMMUTABLE, kept for audit
--                           so a rolled-forward item still proves when it was
--                           first due.
--   due_business_date     — the CURRENT executable date. Rolls forward at the
--                           business-day boundary. Work is NEVER auto-canceled
--                           because a date passed.
--
-- work_state is the single DISJOINT read bucket dimension for Calendar/Control
-- Tower. Exactly one value per row, so summaries can be added without
-- double-counting:
--   scheduled  — open, on or before its due business date
--   delayed    — open, past its ORIGINAL planned business date
--   completed  — the bucket finished
--   closed     — leadership explicitly ended it
--   canceled   — the bucket was deselected from the campaign
-- open/executable = scheduled | delayed.
--
-- Free-flow is preserved: nothing here references goats, herd rosters, or
-- vaccination; no animal identity is required; no uniqueness is added over
-- scanned_identifier.
CREATE TABLE IF NOT EXISTS public.weighing_work_items (
  work_item_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES public.tenants(tenant_id),
  campaign_id uuid NOT NULL REFERENCES public.weighing_campaigns(campaign_id) ON DELETE CASCADE,
  campaign_shed_id uuid NOT NULL REFERENCES public.weighing_campaign_sheds(campaign_shed_id) ON DELETE CASCADE,
  park_id uuid NOT NULL REFERENCES public.locations(location_id),
  operator_user_id uuid NOT NULL,
  weighing_category text NOT NULL,
  shed_label text NOT NULL,
  -- Denormalized at publish so every cadence pass can name the physical shed in
  -- its event payload without joining the bucket table inside the claim.
  shed_location_id uuid NOT NULL REFERENCES public.locations(location_id),
  planned_business_date date NOT NULL,
  due_business_date date NOT NULL,
  work_state text NOT NULL DEFAULT 'scheduled',
  day_start_surfaced_on date,
  rolled_forward_count integer NOT NULL DEFAULT 0 CHECK (rolled_forward_count >= 0),
  last_rolled_forward_on date,
  delayed_since_business_date date,
  escalated_on date,
  terminal_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT weighing_work_items_category_check
    CHECK (weighing_category = ANY (ARRAY['individual_animal','per_shed_partition'])),
  CONSTRAINT weighing_work_items_work_state_check
    CHECK (work_state = ANY (ARRAY['scheduled','delayed','completed','closed','canceled'])),
  -- The current due date may roll FORWARD only. It can never precede the
  -- original plan, which is what makes roll-forward auditable.
  CONSTRAINT weighing_work_items_due_not_before_planned_check
    CHECK (due_business_date >= planned_business_date)
);

-- Publish idempotency: one work item per bucket, forever. Republish and an exact
-- event replay both land on ON CONFLICT DO NOTHING.
CREATE UNIQUE INDEX IF NOT EXISTS weighing_work_items_bucket_uidx
  ON public.weighing_work_items (tenant_id, campaign_shed_id);

-- Terminal-reconciliation claim path (kernel.go pass 1). That pass claims OPEN
-- work only, filtering on tenant_id + work_state alone and keyset-ordered by
-- plain work_item_id, so this PARTIAL index IS the whole ordered claim source
-- for that ONE pass: the open set is bounded by published-and-unfinished
-- buckets, never by all history. It is not the claim source for the other
-- three passes below, which range-filter a date column and therefore keyset on
-- (date, work_item_id), not on work_item_id alone.
CREATE INDEX IF NOT EXISTS weighing_work_items_open_keyset_idx
  ON public.weighing_work_items (tenant_id, work_item_id)
  WHERE work_state = ANY (ARRAY['scheduled','delayed']);

-- Roll-forward and day-start claim paths (kernel.go passes 2 and 4). Both
-- filter on (tenant_id, work_state, due_business_date) and both keyset/ORDER BY
-- on (due_business_date, work_item_id) to match this index's column order
-- exactly, so the index scan already yields sorted output and LIMIT stops
-- early instead of forcing a full Sort or falling back to the open-keyset
-- index over far more rows than the chunk size.
CREATE INDEX IF NOT EXISTS weighing_work_items_sweep_due_idx
  ON public.weighing_work_items (tenant_id, work_state, due_business_date, work_item_id);

-- Delayed/escalation claim path (kernel.go pass 3). Filters on
-- (tenant_id, work_state, planned_business_date) and keysets/ORDER BYs on
-- (planned_business_date, work_item_id) to match this index's column order.
CREATE INDEX IF NOT EXISTS weighing_work_items_sweep_planned_idx
  ON public.weighing_work_items (tenant_id, work_state, planned_business_date, work_item_id);

-- weighing_work_items_operator_due_idx was intentionally NOT added here: no
-- query in kernel.go filters or keysets on operator_user_id (day-start surfaces
-- today's open work for ALL assigned operators in one tenant-wide claim, then
-- groups by operator only for the cadence-event payload after the row is
-- already claimed). An index nothing queries is dead weight on every write to
-- this table, so it is deleted rather than kept "for later" — add it back with
-- a real query when/if a per-operator claim predicate exists.

-- Calendar day markers + Control Tower whole-filter summary.
CREATE INDEX IF NOT EXISTS weighing_work_items_campaign_state_idx
  ON public.weighing_work_items (tenant_id, campaign_id, work_state, due_business_date);

-- Terminal reconciliation reads the bucket side by (tenant, campaign_shed_id);
-- weighing_campaign_sheds already has its own PK for that join.

-- +goose Down
DROP INDEX IF EXISTS public.weighing_work_items_campaign_state_idx;
DROP INDEX IF EXISTS public.weighing_work_items_sweep_planned_idx;
DROP INDEX IF EXISTS public.weighing_work_items_sweep_due_idx;
DROP INDEX IF EXISTS public.weighing_work_items_open_keyset_idx;
DROP INDEX IF EXISTS public.weighing_work_items_bucket_uidx;
DROP TABLE IF EXISTS public.weighing_work_items;
