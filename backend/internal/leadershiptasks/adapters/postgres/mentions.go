package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// MENTIONS AND PARTICIPANTS (maintainer decision 2026-09-18, migration 000346).
//
// Who may be mentioned on a task is answered by ONE query, mentionableUsers, and that same
// query is what the write path re-validates against under the task's row lock. The read the
// `@` autocomplete calls and the check that admits a mention cannot disagree, because they
// are the same statement.

// ListMentionableUsers answers the `@` autocomplete for one task.
func (r *Repository) ListMentionableUsers(ctx context.Context, tenantID, taskID string) ([]domain.MentionableUser, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.mentionableUsers(ctx, r.pool, tenantID, taskID)
}

// mentionableUsers lists the task's parties plus the leadership population, deduped, with the
// relation that put each person on the list. The set is BOUNDED by the leadership tick (tens
// of people), so the write path filters the client's ids against it in Go rather than issuing
// a second round trip per id.
func (r *Repository) mentionableUsers(ctx context.Context, q querier, tenantID, taskID string) ([]domain.MentionableUser, error) {
	rows, err := q.Query(ctx, sqlListMentionableUsers,
		tenantID, taskID, permissions.SurfaceMobile, leadershipTasksModuleKey, permissions.LevelOversee, assignableLeadershipRoles)
	if err != nil {
		return nil, fmt.Errorf("leadership task: list mentionable users: %w", err)
	}
	defer rows.Close()
	out := make([]domain.MentionableUser, 0, 8)
	for rows.Next() {
		var u domain.MentionableUser
		if err := rows.Scan(&u.UserID, &u.Name, &u.Title, &u.Relation); err != nil {
			return nil, fmt.Errorf("leadership task: scan mentionable user: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// resolveMentionTargets turns the client's ids into the rows to write, under the caller's
// transaction and therefore under the task's row lock. An id that names nobody mentionable is
// REFUSED -- never dropped -- so a leader who typed a name that cannot see the task is told,
// rather than watching their note post with the mention silently missing.
func (r *Repository) resolveMentionTargets(ctx context.Context, tx pgx.Tx, tenantID, taskID string, want []string) ([]string, error) {
	if len(want) == 0 {
		return nil, nil
	}
	allowed, err := r.mentionableUsers(ctx, tx, tenantID, taskID)
	if err != nil {
		return nil, err
	}
	index := make(map[string]struct{}, len(allowed))
	for _, u := range allowed {
		index[strings.TrimSpace(u.UserID)] = struct{}{}
	}
	out := make([]string, 0, len(want))
	for _, id := range want {
		if _, ok := index[id]; !ok {
			return nil, domain.ErrMentionNotVisible
		}
		out = append(out, id)
	}
	return out, nil
}

// insertMentions stores the resolved mentions of one note and, in the SAME statement pair,
// records each mentioned person as a participant of the task -- which is what lets them open
// the task the push deep-links to. Both writes are set-based (one statement, not one per id).
func insertMentions(ctx context.Context, tx execer, tenantID, taskID, noteID, authorID string, userIDs []string, now time.Time) error {
	if len(userIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, sqlInsertMentions, tenantID, taskID, noteID, authorID, now, userIDs); err != nil {
		return fmt.Errorf("leadership task: insert mentions: %w", err)
	}
	if _, err := tx.Exec(ctx, sqlUpsertParticipants, tenantID, taskID, authorID, now, userIDs); err != nil {
		return fmt.Errorf("leadership task: upsert participants: %w", err)
	}
	return nil
}

// mentionsTo loads the stored mentions of a bounded task set in ONE query and hangs each on
// its note, so the phone can render chips over the note body.
func (r *Repository) mentionsTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	ids := taskIDsOf(tasks)
	if len(ids) == 0 {
		return nil
	}
	notes := noteIndexOf(tasks)
	if len(notes) == 0 {
		return nil
	}
	rows, err := q.Query(ctx, sqlListMentions, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list mentions: %w", err)
	}
	defer rows.Close()
	return scanMentionsInto(rows, notes)
}

// noteIndexOf is the page's note id -> note pointer, and it CLEARS each note's mentions as it
// goes. It is built AFTER the notes have landed, so a pipelined read must scan its note result
// before it calls this.
func noteIndexOf(tasks []domain.Task) map[string]*domain.Note {
	notes := make(map[string]*domain.Note, 8)
	for i := range tasks {
		for j := range tasks[i].Notes {
			tasks[i].Notes[j].Mentions = nil
			notes[tasks[i].Notes[j].NoteID] = &tasks[i].Notes[j]
		}
	}
	return notes
}

// scanMentionsInto hangs one mention result set on its notes, split out of mentionsTo so the
// pipelined page read and the single-query path share one scan. A mention whose note is not on
// this page is DROPPED rather than guessed onto another note.
func scanMentionsInto(rows pgx.Rows, notes map[string]*domain.Note) error {
	for rows.Next() {
		var noteID string
		var m domain.Mention
		if err := rows.Scan(&noteID, &m.MentionID, &m.UserID, &m.Name, &m.MentionedByUserID, &m.CreatedAt); err != nil {
			return fmt.Errorf("leadership task: mentions scan: %w", err)
		}
		m.NoteID = noteID
		m.CreatedAt = m.CreatedAt.UTC()
		if note, ok := notes[noteID]; ok {
			note.Mentions = append(note.Mentions, m)
		}
	}
	return rows.Err()
}

// participantsTo loads the mention-granted readers of a bounded task set in ONE query. They
// decide domain.Task.CanRead, so a task read without them would 404 for someone a note
// legitimately pulled onto it.
func (r *Repository) participantsTo(ctx context.Context, q querier, tenantID string, tasks []domain.Task) error {
	ids := taskIDsOf(tasks)
	if len(ids) == 0 {
		return nil
	}
	index := make(map[string]int, len(tasks))
	for i := range tasks {
		index[tasks[i].TaskID] = i
		tasks[i].ParticipantUserIDs = nil
	}
	rows, err := q.Query(ctx, sqlListParticipants, tenantID, ids)
	if err != nil {
		return fmt.Errorf("leadership task: list participants: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, userID string
		if err := rows.Scan(&taskID, &userID); err != nil {
			return fmt.Errorf("leadership task: participants scan: %w", err)
		}
		if i, ok := index[taskID]; ok {
			tasks[i].ParticipantUserIDs = append(tasks[i].ParticipantUserIDs, userID)
		}
	}
	return rows.Err()
}

func taskIDsOf(tasks []domain.Task) []string {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(tasks))
	for i := range tasks {
		ids = append(ids, tasks[i].TaskID)
	}
	return ids
}

// projection-review: membership=the UNION of the task's own two parties (one leadership_tasks row,
// so exactly one raiser row and one assignee row) and the leadership population
// (person_module_access ticked at Oversee on the mobile Tasks module, one row per ticked person
// because the leadership grant is tested with EXISTS rather than joined); group_key=user_id, and
// DISTINCT ON (user_id) ORDER BY rank keeps a party who is ALSO ticked as ONE row carrying the
// party relation, so the same person can never be listed twice; join_cardinality=workforce_members
// 1:1 on workforce_members_active_user_unique_idx, workforce_member_titles 1:1 on (tenant, member)
// PK, person_access 1:1 on (tenant, member) PK, designation_catalog 1:1 on designation_code;
// pagination=none, the set is bounded by the leadership tick plus two parties; scope=tenant_id on
// every branch and task_id on the party branch
const sqlListMentionableUsers = `
WITH parties AS (
  SELECT t.raised_by AS user_id, 'raiser'::text AS relation, 0 AS rank
  FROM public.leadership_tasks t
  WHERE t.tenant_id = $1::uuid AND t.task_id = $2::uuid
  UNION ALL
  SELECT t.assignee_user_id, 'assignee'::text, 1
  FROM public.leadership_tasks t
  WHERE t.tenant_id = $1::uuid AND t.task_id = $2::uuid
),
leadership AS (
  SELECT m.user_id, 'leadership'::text AS relation, 2 AS rank
  FROM public.person_module_access a
  JOIN public.workforce_members m
    ON m.tenant_id = a.tenant_id AND m.workforce_member_id = a.workforce_member_id AND m.status = 'active'
  WHERE a.tenant_id = $1::uuid AND a.surface = $3 AND a.module_key = $4 AND $5 = ANY(a.capabilities)
    AND m.user_id IS NOT NULL
    -- The local development login is never offered as an @mention (see sqlListAssignees).
    AND COALESCE((m.metadata->>'dev_account')::boolean, false) = false
    AND EXISTS (
      SELECT 1 FROM public.user_scope_grants g
      WHERE g.tenant_id = m.tenant_id AND g.user_id = m.user_id
        AND g.status = 'active' AND (g.valid_to IS NULL OR g.valid_to > now())
        AND g.role = ANY($6::text[])
    )
),
candidates AS (
  SELECT * FROM parties
  UNION ALL
  SELECT * FROM leadership
),
resolved AS (
  SELECT DISTINCT ON (c.user_id)
         c.user_id::text AS user_id,
         m.display_name AS name,
         COALESCE(NULLIF(btrim(wt.title), ''), dc.label, '') AS title,
         c.relation
  FROM candidates c
  JOIN public.workforce_members m
    ON m.tenant_id = $1::uuid AND m.user_id = c.user_id AND m.status = 'active'
   AND COALESCE((m.metadata->>'dev_account')::boolean, false) = false
  LEFT JOIN public.workforce_member_titles wt
    ON wt.tenant_id = m.tenant_id AND wt.workforce_member_id = m.workforce_member_id
  LEFT JOIN public.person_access pa
    ON pa.tenant_id = m.tenant_id AND pa.workforce_member_id = m.workforce_member_id
  LEFT JOIN public.designation_catalog dc
    ON dc.designation_code = pa.designation_code
  ORDER BY c.user_id, c.rank
)
SELECT user_id, name, title, relation
FROM resolved
ORDER BY title, name, user_id`

const sqlInsertMentions = `
INSERT INTO public.leadership_task_mentions (
  tenant_id, task_id, note_id, mentioned_user_id, mentioned_by_user_id, created_at
)
SELECT $1::uuid, $2::uuid, $3::uuid, u, $4::uuid, $5::timestamptz
FROM unnest($6::uuid[]) AS u
ON CONFLICT (tenant_id, note_id, mentioned_user_id) DO NOTHING`

const sqlUpsertParticipants = `
INSERT INTO public.leadership_task_participants (
  tenant_id, task_id, user_id, source, added_by_user_id, added_at
)
SELECT $1::uuid, $2::uuid, u, 'mention', $3::uuid, $4::timestamptz
FROM unnest($5::uuid[]) AS u
ON CONFLICT (tenant_id, task_id, user_id) DO NOTHING`

// projection-review: membership=leadership_task_mentions at its (tenant, note, user) unique key,
// one row per mentioned person per note; group_key=note_id for hanging each mention on its note;
// join_cardinality=workforce_members 1:1 on workforce_members_active_user_unique_idx;
// pagination=none, bounded by the page's task ids and 20 mentions per note; scope=tenant_id
const sqlListMentions = `
SELECT m.note_id::text, m.mention_id::text, m.mentioned_user_id::text,
       COALESCE(w.display_name, ''), m.mentioned_by_user_id::text, m.created_at
FROM public.leadership_task_mentions m
LEFT JOIN public.workforce_members w
       ON w.tenant_id = m.tenant_id AND w.user_id = m.mentioned_user_id AND w.status = 'active'
WHERE m.tenant_id = $1 AND m.task_id = ANY($2::uuid[])
ORDER BY m.note_id, m.created_at, m.mention_id`

// projection-review: membership=leadership_task_participants at its (tenant, task, user) PK, one
// row per person per task; group_key=task_id; join_cardinality=none; pagination=none, bounded by
// the page's task ids; scope=tenant_id
const sqlListParticipants = `
SELECT task_id::text, user_id::text
FROM public.leadership_task_participants
WHERE tenant_id = $1 AND task_id = ANY($2::uuid[])
ORDER BY task_id, user_id`
