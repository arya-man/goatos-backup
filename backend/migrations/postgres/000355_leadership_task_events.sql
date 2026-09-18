-- +goose Up
-- THE ACTIVITY FEED OF A LEADERSHIP TASK (CEO instruction 2026-09-18, the Jira issue panel).
--
-- WHY A TABLE AND NOT A RE-READ OF WHAT EXISTS
--
-- The task panel must answer "who moved this, from what, to what, when" -- "Ravi Teja changed
-- the Status  Open -> Doing", "Hemant created the task", "deadline changed 17/09 -> 20/09" --
-- newest first, beside the notes. No column on leadership_tasks (000252) records the ACTOR of a
-- change: done_at and cancelled_at say when, never who, and an edit overwrites the title,
-- brief and deadline in place. Two stores already SEE every write, and neither can serve the
-- feed:
--   * audit_log rows carry actor, before-state and after-state as JSON. They are the
--     platform-wide compliance ledger, keyed for "what happened on this farm in this window",
--     not for "this one task's own story"; the feed would have to diff two JSON blobs per row
--     to learn which field moved, and a compliance ledger's retention is not the panel's.
--   * outbox_messages is a transport queue: a row exists until its consumer has taken it, and
--     it carries the new status but not the old title or old deadline.
-- So the feed is its own table: one row per fact, with the fact's two values spelled out at
-- write time by the same transaction that changed them, so a rolled-back edit leaves no
-- history and a committed one always does.
--
-- kind is a CLOSED vocabulary mirrored by the CHECK. from_value / to_value are the plain
-- stored values (a status key, an RFC3339 deadline instant, a title, a brief) and the reading
-- side composes the words ("Doing", "20/09/2026 17:00"); a row never carries a rendered label,
-- so a rewording never needs a data fix. note_id links a `commented` row to its note and
-- cascades with it.
--
-- Shape and naming follow 000286_leadership_task_notes.sql and 000346: a plain tenant_id column
-- (no FK, as on the notes table), a task_id FK that CASCADEs with the task, and one composite
-- index led by (tenant_id, task_id, ...) in the feed's own read order. Seed coupling note
-- (docs/runbooks/initial-seed-migration-coupling.md): this table is OPERATIONAL, born at
-- runtime from a task write; no seed command hand-fills it.
-- seed-fixture-guard:ignore: runtime leadership-task activity rows, not seed input.
CREATE TABLE public.leadership_task_events (
    tenant_id uuid NOT NULL,
    event_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id uuid NOT NULL REFERENCES public.leadership_tasks (task_id) ON DELETE CASCADE,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_user_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN (
        'created',
        'status_changed',
        'assignee_changed',
        'deadline_changed',
        'title_changed',
        'brief_changed',
        'commented',
        'cancelled'
    )),
    from_value text NOT NULL DEFAULT '',
    to_value text NOT NULL DEFAULT '',
    note_id uuid NULL REFERENCES public.leadership_task_notes (note_id) ON DELETE CASCADE
);

-- The feed's one read: this task's rows, newest first, event_id breaking a same-instant tie
-- the same way on every page load.
CREATE INDEX leadership_task_events_task_idx
    ON public.leadership_task_events (tenant_id, task_id, occurred_at DESC, event_id DESC);

-- BACKFILL, in this migration and not a one-off command, because the feed is READ by the
-- binary that ships with this file and every task raised before today would otherwise open
-- on an empty history. Only facts the rows already carry exactly are written:
--   * one `created` row per task from raised_at / raised_by (both NOT NULL since 000252);
--   * one `commented` row per existing note from its author and created_at (000286).
-- Nothing is invented: a done task's done_at names no actor, so no `status_changed` row is
-- guessed for it. Both statements are idempotent (NOT EXISTS on the natural key), so a re-run
-- of the Up section on a database that already has them writes nothing.
INSERT INTO public.leadership_task_events (tenant_id, task_id, occurred_at, actor_user_id, kind)
SELECT t.tenant_id, t.task_id, t.raised_at, t.raised_by, 'created'
FROM public.leadership_tasks t
WHERE NOT EXISTS (
    SELECT 1 FROM public.leadership_task_events e
    WHERE e.tenant_id = t.tenant_id AND e.task_id = t.task_id AND e.kind = 'created'
);

INSERT INTO public.leadership_task_events (tenant_id, task_id, occurred_at, actor_user_id, kind, note_id)
SELECT n.tenant_id, n.task_id, n.created_at, n.author_user_id, 'commented', n.note_id
FROM public.leadership_task_notes n
WHERE NOT EXISTS (
    SELECT 1 FROM public.leadership_task_events e
    WHERE e.tenant_id = n.tenant_id AND e.note_id = n.note_id
);

-- +goose Down
DROP TABLE IF EXISTS public.leadership_task_events;
