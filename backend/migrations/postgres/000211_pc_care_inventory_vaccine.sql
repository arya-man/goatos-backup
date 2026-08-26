-- +goose Up
-- seed-fixture-guard:ignore: adds an operational PC director task category; no vaccination HRMS seed rows are changed
--
-- inventory_vaccine is a PC director task created by the kernel seven days
-- before scheduled vaccination work. It is task-level fridge proof, not
-- per-animal care proof, so it stores media on pc_care_task_proofs.

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming', 'inventory_vaccine')
  );

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_tenant_task_uq UNIQUE (tenant_id, task_id);

CREATE TABLE IF NOT EXISTS public.pc_care_task_proofs (
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL,
  slot_key text NOT NULL,
  proof_ref text NOT NULL,
  captured_by uuid NOT NULL,
  captured_at timestamp with time zone DEFAULT now() NOT NULL,
  idempotency_key text NOT NULL,
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT pc_care_task_proofs_pkey PRIMARY KEY (tenant_id, task_id, slot_key),
  CONSTRAINT pc_care_task_proofs_task_fk
    FOREIGN KEY (tenant_id, task_id)
    REFERENCES public.pc_care_tasks (tenant_id, task_id)
    ON DELETE CASCADE,
  CONSTRAINT pc_care_task_proofs_slot_check CHECK (slot_key IN ('stock_fridge_photo', 'stock_fridge_video')),
  CONSTRAINT pc_care_task_proofs_proof_ref_check CHECK (btrim(proof_ref) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS pc_care_task_proofs_idempotency_uq
  ON public.pc_care_task_proofs (tenant_id, idempotency_key);

CREATE INDEX IF NOT EXISTS pc_care_task_proofs_task_idx
  ON public.pc_care_task_proofs (tenant_id, task_id);

CREATE TABLE IF NOT EXISTS public.pc_care_task_inventory_requirements (
  tenant_id uuid NOT NULL,
  task_id uuid NOT NULL,
  vaccine_label text NOT NULL,
  required_doses integer NOT NULL,
  source_batch_ids uuid[] NOT NULL DEFAULT '{}',
  created_at timestamp with time zone DEFAULT now() NOT NULL,
  updated_at timestamp with time zone DEFAULT now() NOT NULL,
  CONSTRAINT pc_care_task_inventory_requirements_pkey PRIMARY KEY (tenant_id, task_id, vaccine_label),
  CONSTRAINT pc_care_task_inventory_requirements_task_fk
    FOREIGN KEY (tenant_id, task_id)
    REFERENCES public.pc_care_tasks (tenant_id, task_id)
    ON DELETE CASCADE,
  CONSTRAINT pc_care_task_inventory_requirements_label_check CHECK (btrim(vaccine_label) <> ''),
  CONSTRAINT pc_care_task_inventory_requirements_doses_check CHECK (required_doses >= 0)
);

CREATE INDEX IF NOT EXISTS pc_care_task_inventory_requirements_task_idx
  ON public.pc_care_task_inventory_requirements (tenant_id, task_id);

-- +goose Down
DROP TABLE IF EXISTS public.pc_care_task_inventory_requirements;
DROP TABLE IF EXISTS public.pc_care_task_proofs;

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_category_check;

ALTER TABLE public.pc_care_tasks
  ADD CONSTRAINT pc_care_tasks_category_check CHECK (
    category IN ('deworming', 'ticks_removal', 'hoof_trimming', 'hair_trimming')
  );

ALTER TABLE public.pc_care_tasks
  DROP CONSTRAINT IF EXISTS pc_care_tasks_tenant_task_uq;
