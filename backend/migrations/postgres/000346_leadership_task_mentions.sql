-- +goose Up
-- @-MENTIONS ON A LEADERSHIP TASK NOTE, plus the notification vocabulary the task's own
-- parties need (maintainer instruction 2026-09-18).
--
-- WHY A TABLE AND NOT A RE-PARSE
--
-- A note's body is what someone typed; who they meant is a DECISION, taken once, at write
-- time, against the roster as it stood then. Re-reading "@Ravi" out of the text later cannot
-- answer it: two active people can carry the same display name, a person can be renamed or
-- deactivated, and a regex over free text has no way to tell a mention from an email address
-- or a handle someone quoted. So the resolved targets are STORED, one row per mentioned
-- person per note, with the actor who mentioned them and when.
--
-- The client sends explicit user ids alongside the text (contracts/openapi/app-api.yaml,
-- setLeadershipTaskComment.mentions[]); the server re-validates every id under the task's row
-- lock before it writes a row here, against the SAME visibility rule that decides whether
-- that person may open the task at all. A person who cannot already read the task can never
-- be mentioned on it, so a mention can never leak a brief or a note to a stranger.
--
-- Shape and naming follow 000286_leadership_task_notes.sql: a plain tenant_id column (no FK,
-- as on the notes table), task_id and note_id FKs that CASCADE with the rows they annotate,
-- and one composite index led by (tenant_id, ...). Seed coupling note
-- (docs/runbooks/initial-seed-migration-coupling.md): this table is OPERATIONAL, born at
-- runtime when someone types a note on a phone; no seed command hand-fills it.
-- seed-fixture-guard:ignore: runtime leadership-task mention rows, not seed input.
CREATE TABLE public.leadership_task_mentions (
    tenant_id uuid NOT NULL,
    mention_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- task_id is denormalized beside note_id so "every mention of me on this task" is one
    -- index read, and so a mention row is never orphaned from the task it belongs to.
    task_id uuid NOT NULL REFERENCES public.leadership_tasks (task_id) ON DELETE CASCADE,
    note_id uuid NOT NULL REFERENCES public.leadership_task_notes (note_id) ON DELETE CASCADE,
    mentioned_user_id uuid NOT NULL,
    -- mentioned_by_user_id is the note's author: the person who took the decision above.
    mentioned_by_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- One row per person per note. A client that sends the same id twice in one comment, or
    -- retries the whole write, still owes exactly one mention.
    CONSTRAINT leadership_task_mentions_note_user_unique UNIQUE (tenant_id, note_id, mentioned_user_id)
);

CREATE INDEX leadership_task_mentions_task_idx
    ON public.leadership_task_mentions (tenant_id, task_id, created_at, mention_id);

-- "What was I mentioned in?" -- the read the mention inbox and the fan-out both want.
CREATE INDEX leadership_task_mentions_user_idx
    ON public.leadership_task_mentions (tenant_id, mentioned_user_id, created_at, mention_id);

-- PARTICIPANTS: A MENTION GRANTS READ (maintainer decision 2026-09-18, the Jira behaviour).
--
-- Without this table a mention would be a dead end -- and worse, a leak: the push names the
-- task title, and tapping it would 404 because the mentioned person is neither the raiser, the
-- assignee nor a tenant monitor. So a mention RECORDS the mentioned person as a participant on
-- that one task, and the module's read rule (domain.Task.CanRead) admits participants. The
-- recipient is then legitimately entitled to the title the push already showed them.
--
-- The grant is DELIBERATELY NARROW and per-task:
--   * only the leadership population may be mentioned in the first place (the same Oversee
--     tick plus one of the eight leadership grants the assignee picker reads, migration
--     000292), plus the task's own two parties, so a mention cannot invent access for an
--     operator;
--   * it opens the task by DIRECT OPEN only. The list scopes (For me / Raised by me / Team
--     progress) are untouched in this migration: a participant does not acquire a task
--     dashboard, and "Team progress" stays the monitor's screen;
--   * it cascades with the task, so a deleted task takes its grants with it.
CREATE TABLE public.leadership_task_participants (
    tenant_id uuid NOT NULL,
    task_id uuid NOT NULL REFERENCES public.leadership_tasks (task_id) ON DELETE CASCADE,
    user_id uuid NOT NULL,
    -- source names WHY this person may read the task. Closed vocabulary, one value today;
    -- the column exists so a later grant path (an explicit "add watcher") is readable in the
    -- row rather than inferred from its neighbours.
    source text NOT NULL DEFAULT 'mention' CHECK (btrim(source) <> '' AND source IN ('mention')),
    added_by_user_id uuid NOT NULL,
    added_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, task_id, user_id)
);

-- "Which tasks may this person open?" -- the read a participant's own screens want.
CREATE INDEX leadership_task_participants_user_idx
    ON public.leadership_task_participants (tenant_id, user_id, added_at);

-- Notification vocabulary for the three things that now reach a task's own parties: being
-- mentioned in a note, a note being posted, and the brief/deadline/attachments being updated.
-- A status change keeps riding the existing 'leadership_task_done' type, which is what the
-- consumer has queued for every status move since 2026-09-08. Enum widening on a table no
-- seed path writes; the lock-timeout / NOT VALID / VALIDATE shape is 000252's, 000289's,
-- 000306's and 000329's. The array is 000329's current list plus ours.
SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text,
    'leave_request_raised'::text,
    'leave_request_decided'::text,
    'animal_purchase_decided'::text,
    'market_survey_due'::text,
    'pen_routine_due'::text,
    'leadership_task_mentioned'::text,
    'leadership_task_commented'::text,
    'leadership_task_updated'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;

-- +goose Down
DROP TABLE IF EXISTS public.leadership_task_mentions;
DROP TABLE IF EXISTS public.leadership_task_participants;

SET lock_timeout = '5s';
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  DROP CONSTRAINT IF EXISTS notification_requests_type_check;
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  ADD CONSTRAINT notification_requests_type_check
  CHECK ((notification_type = ANY (ARRAY[
    'reminder'::text,
    'nudge'::text,
    'escalation'::text,
    'verification_pending'::text,
    'verification_approved'::text,
    'verification_closed'::text,
    'verification_withdrawn'::text,
    'rework'::text,
    'advance_notice'::text,
    'due_today'::text,
    'leadership_task_raised'::text,
    'leadership_task_done'::text,
    'obligation_missed'::text,
    'feed_low_stock'::text,
    'procurement_load_overdue'::text,
    'feed_proof_times_daily'::text,
    'feed_sale_reduce'::text,
    'feed_sale_reduce_reminder'::text,
    'pen_visit_due'::text,
    'leave_request_raised'::text,
    'leave_request_decided'::text,
    'animal_purchase_decided'::text,
    'market_survey_due'::text,
    'pen_routine_due'::text
  ]))) NOT VALID;
-- seed-migration-guard:ignore owner=manohark issue=leadership-task-mentions reason=enum-widening-on-a-table-no-seed-path-writes expiry=2026-12-31
ALTER TABLE public.notification_requests
  VALIDATE CONSTRAINT notification_requests_type_check;
RESET lock_timeout;
