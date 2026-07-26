-- +goose Up
-- Restore the selected-operator set on already-migrated STG databases. Fresh
-- clean-slate databases have this in 000001, but databases that applied 000001
-- before the baseline was amended need an explicit forward migration.

ALTER TABLE public.vaccination_operator_assignment_config
  ADD COLUMN IF NOT EXISTS selected_operator_ids uuid[] DEFAULT '{}'::uuid[];

UPDATE public.vaccination_operator_assignment_config
SET selected_operator_ids = ARRAY[default_operator_id]::uuid[]
WHERE COALESCE(array_length(selected_operator_ids, 1), 0) = 0;

ALTER TABLE public.vaccination_operator_assignment_config
  ALTER COLUMN selected_operator_ids SET DEFAULT '{}'::uuid[],
  ALTER COLUMN selected_operator_ids SET NOT NULL;

-- +goose Down
ALTER TABLE public.vaccination_operator_assignment_config
  DROP COLUMN IF EXISTS selected_operator_ids;
