-- +goose Up
-- seed-fixture-guard:ignore: operational membership written only by the scheduler
-- (visit_shot_lock upsert / drive-date replan) and cascade-deleted with its parent
-- assignment or obligation. It is never authored as seed data and never rebuilt by
-- seed closeout -- a fresh seed populates it through the normal sweeper producer
-- path -- so it carries no seed-data contract and needs no fixture/manifest/runbook
-- companions.
-- Exact drive membership: which obligations (one goat x one rule) a drive
-- assignment row actually covers. Without this, a split shed/partition row
-- ("200 on Jul 24, 124 on Jul 25") only stores counts, so a death/sale/cull
-- cannot be attributed to the exact assignment row. Operational membership
-- written by the scheduler; NOT a seeded catalog and NOT a rebuildable
-- projection.
CREATE TABLE IF NOT EXISTS public.vaccination_drive_assignment_members (
  tenant_id uuid NOT NULL,
  assignment_id uuid NOT NULL,
  obligation_id uuid NOT NULL,
  goat_id uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT vaccination_drive_assignment_members_pkey PRIMARY KEY (assignment_id, obligation_id),
  CONSTRAINT vaccination_drive_assignment_members_tenant_obligation_uq UNIQUE (tenant_id, obligation_id),
  CONSTRAINT vaccination_drive_assignment_members_assignment_fk FOREIGN KEY (assignment_id)
    REFERENCES public.vaccination_drive_assignments(assignment_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignment_members_obligation_tenant_fk FOREIGN KEY (tenant_id, obligation_id)
    REFERENCES public.obligation_instances(tenant_id, obligation_id) ON DELETE CASCADE,
  CONSTRAINT vaccination_drive_assignment_members_goat_tenant_fk FOREIGN KEY (tenant_id, goat_id)
    REFERENCES public.goats(tenant_id, goat_id)
);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignment_members_tenant_goat_idx
  ON public.vaccination_drive_assignment_members (tenant_id, goat_id);

CREATE INDEX IF NOT EXISTS vaccination_drive_assignment_members_tenant_assignment_idx
  ON public.vaccination_drive_assignment_members (tenant_id, assignment_id);

-- +goose Down
DROP TABLE IF EXISTS public.vaccination_drive_assignment_members;
