-- +goose Up
-- seed-fixture-guard:ignore: one nullable column on the task engine's action rows; no
-- vaccination/HRMS seed contract, source fixture schema, or read-model change.
--
-- ANSWER-DRIVEN BRANCHES (maintainer decision 2026-09-18, SOP studio phase 2). A step authored
-- with `when_answer` runs only when an earlier question's answer satisfies the condition. The
-- condition is stamped on the action row at workflow open (answer_gate), so the branch is
-- pinned with the rest of the version the workflow started on; when the question is answered,
-- the steps on the branch not taken move to status 'skipped' -- off the path, never owed, out
-- of every count -- in the same transaction as the answer. Rows stamped before this migration
-- carry NULL and behave exactly as before.
ALTER TABLE public.workflow_actions ADD COLUMN IF NOT EXISTS answer_gate jsonb;
ALTER TABLE public.workflow_actions DROP CONSTRAINT IF EXISTS workflow_actions_status_check;
ALTER TABLE public.workflow_actions ADD CONSTRAINT workflow_actions_status_check
  CHECK (status IN ('pending', 'in_review', 'completed', 'rework', 'canceled', 'skipped'));

-- +goose Down
UPDATE public.workflow_actions SET status = 'canceled' WHERE status = 'skipped';
ALTER TABLE public.workflow_actions DROP CONSTRAINT IF EXISTS workflow_actions_status_check;
ALTER TABLE public.workflow_actions ADD CONSTRAINT workflow_actions_status_check
  CHECK (status IN ('pending', 'in_review', 'completed', 'rework', 'canceled'));
ALTER TABLE public.workflow_actions DROP COLUMN IF EXISTS answer_gate;
