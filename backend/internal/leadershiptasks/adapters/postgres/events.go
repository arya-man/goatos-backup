package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
)

// The activity feed (migration 000349). One row per FACT that changed, written INSIDE the
// same transaction as the change -- a rolled-back edit leaves no history, a committed one
// always does -- and read newest first for the panel's History / Comments / All tabs.
//
// A row stores the two PLAIN values of the fact (a status key, an RFC3339 instant, a title);
// domain.EventValueLabel composes the words. Nothing here decides a label.

const sqlInsertEvent = `
INSERT INTO public.leadership_task_events (
  tenant_id, task_id, occurred_at, actor_user_id, kind, from_value, to_value, note_id
) VALUES ($1::uuid, $2::uuid, $3::timestamptz, $4::uuid, $5, $6, $7, NULLIF($8, '')::uuid)`

// projection-review: membership=leadership_task_events keyed on (tenant_id, task_id) for the
// page's own task ids; group_key=none, one row per stored event, nothing aggregated;
// join_cardinality=workforce_members is LEFT JOINed 1:1 on (tenant_id, user_id) for the
// actor's name only (a person with no active roster row keeps the row and reads blank);
// pagination=none, bounded by the task set the caller already paged; scope=tenant_id on the
// event row and on the join.
const sqlListEvents = `
SELECT e.task_id::text, e.event_id::text, e.kind, e.occurred_at, e.actor_user_id::text,
       COALESCE(w.display_name, ''), e.from_value, e.to_value, COALESCE(e.note_id::text, '')
FROM public.leadership_task_events e
LEFT JOIN public.workforce_members w
       ON w.tenant_id = e.tenant_id AND w.user_id = e.actor_user_id AND w.status = 'active'
WHERE e.tenant_id = $1 AND e.task_id = ANY($2::uuid[])
ORDER BY e.task_id, e.occurred_at DESC, e.event_id DESC`

// listEventsCapPerTask bounds the activity a LIST row carries. The list is a board or a table of
// 25 rows; a task that has been edited daily for a year would otherwise ship hundreds of events
// per row. The newest 30 cover what a card or drawer opened from the list shows; the detail read
// (`getRow`) stays uncapped for the full history.
const listEventsCapPerTask = 30

// sqlListEventsCapped is sqlListEvents with a per-task cap ($3), newest first, for the list page.
const sqlListEventsCapped = `
SELECT task_id, event_id, kind, occurred_at, actor_user_id, actor_name, from_value, to_value, note_id
FROM (
  SELECT e.task_id::text AS task_id, e.event_id::text AS event_id, e.kind, e.occurred_at,
         e.actor_user_id::text AS actor_user_id, COALESCE(w.display_name, '') AS actor_name,
         e.from_value, e.to_value, COALESCE(e.note_id::text, '') AS note_id,
         row_number() OVER (PARTITION BY e.task_id ORDER BY e.occurred_at DESC, e.event_id DESC) AS rn
  FROM public.leadership_task_events e
  LEFT JOIN public.workforce_members w
         ON w.tenant_id = e.tenant_id AND w.user_id = e.actor_user_id AND w.status = 'active'
  WHERE e.tenant_id = $1 AND e.task_id = ANY($2::uuid[])
) ranked
WHERE rn <= $3
ORDER BY task_id, occurred_at DESC, event_id DESC`

// recordEvent writes one activity row inside tx.
func recordEvent(ctx context.Context, tx pgx.Tx, tenantID, taskID string, at time.Time, actorID, kind, from, to, noteID string) error {
	if _, err := tx.Exec(ctx, sqlInsertEvent, tenantID, taskID, at, actorID, kind, from, to, noteID); err != nil {
		return fmt.Errorf("leadership task: record %s event: %w", kind, err)
	}
	return nil
}

// recordEditEvents compares the brief before and after an edit and writes one row per field
// that actually moved. An edit that re-saves the same title, brief and deadline writes
// nothing: the feed lists facts, not button presses.
func recordEditEvents(ctx context.Context, tx pgx.Tx, tenantID, taskID string, at time.Time, actorID string, before, after domain.Task) error {
	if before.Title != after.Title {
		if err := recordEvent(ctx, tx, tenantID, taskID, at, actorID, domain.EventTitleChanged, before.Title, after.Title, ""); err != nil {
			return err
		}
	}
	if before.Body != after.Body {
		if err := recordEvent(ctx, tx, tenantID, taskID, at, actorID, domain.EventBriefChanged, before.Body, after.Body, ""); err != nil {
			return err
		}
	}
	if !sameInstant(before.DeadlineAt, after.DeadlineAt) {
		if err := recordEvent(ctx, tx, tenantID, taskID, at, actorID, domain.EventDeadlineChanged, rfc3339OrBlank(before.DeadlineAt), rfc3339OrBlank(after.DeadlineAt), ""); err != nil {
			return err
		}
	}
	return nil
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func rfc3339OrBlank(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// eventsTo loads the activity of a bounded task set in one query, newest first per task.
func (r *Repository) eventsTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids, index := taskIndexOf(tasks)
	rows, err := q.Query(ctx, sqlListEvents, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list events: %w", err)
	}
	defer rows.Close()
	return scanEventsInto(rows, tasks, index)
}

// scanEventsInto hangs one event result set on its tasks, split out so the pipelined page
// read and the single-query path share one scan.
func scanEventsInto(rows pgx.Rows, tasks []domain.Task, index map[string]int) error {
	for i := range tasks {
		tasks[i].Activity = nil
	}
	for rows.Next() {
		var taskID string
		var e domain.Event
		if err := rows.Scan(&taskID, &e.EventID, &e.Kind, &e.OccurredAt, &e.ActorUserID, &e.ActorName, &e.FromValue, &e.ToValue, &e.NoteID); err != nil {
			return fmt.Errorf("leadership task: events scan: %w", err)
		}
		e.OccurredAt = e.OccurredAt.UTC()
		if i, ok := index[taskID]; ok {
			tasks[i].Activity = append(tasks[i].Activity, e)
		}
	}
	return rows.Err()
}
