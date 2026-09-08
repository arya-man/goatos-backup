-- +goose Up
CREATE TABLE public.leadership_task_notes (
    tenant_id uuid NOT NULL,
    note_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES public.leadership_tasks (task_id) ON DELETE CASCADE,
    author_user_id uuid NOT NULL,
    body text NOT NULL CHECK (btrim(body) <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX leadership_task_notes_task_idx
    ON public.leadership_task_notes (tenant_id, task_id, created_at, note_id);

-- +goose Down
DROP TABLE IF EXISTS public.leadership_task_notes;
