package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// @-MENTIONS AGAINST A REAL POSTGRES (maintainer decision 2026-09-18).
//
// The fixture seeds only EXTERNAL facts -- it reuses seedLeadershipFixture's tenant, roster,
// grants and ticks and nothing else. Every task row, note, mention row, participant grant and
// outbox message below is produced by the repository under test.
//
// The population the fixture gives us, and why each person is the case they are:
//
//	ltDirector   park_head grant, NO Tasks tick -> mentionable ONLY as the task's raiser
//	ltCXO        ceo_internal + tick            -> the assignee
//	ltCXO2       ceo_internal + tick            -> leadership, party to nothing
//	ltDirector2  tick but NO leadership grant   -> the NON-VISIBLE person, party to nothing
func TestLeadershipTaskMentionsPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	director := domain.Actor{UserID: ltDirector, CanRaise: true}

	task, err := repo.Raise(ctx, raiseParams(ltCXO, "Fix the CPT water line", "mention-raise-1"))
	if err != nil {
		t.Fatalf("raise: %v", err)
	}

	// 1. THE MENTIONABLE LIST IS THE PARTIES PLUS THE LEADERSHIP POPULATION -- and nothing
	// wider. A ticked person with no leadership grant is NOT on it.
	users, err := repo.ListMentionableUsers(ctx, ltTenant, task.TaskID)
	if err != nil {
		t.Fatalf("list mentionable users: %v", err)
	}
	relation := map[string]string{}
	for _, u := range users {
		relation[u.UserID] = u.Relation
		if u.Name == "" {
			t.Fatalf("a listed person must carry a name the picker can show: %+v", u)
		}
	}
	if relation[ltDirector] != domain.MentionRelationRaiser {
		t.Fatalf("the raiser must be nameable in the thread about their own ask; relations=%v", relation)
	}
	if relation[ltCXO] != domain.MentionRelationAssignee {
		t.Fatalf("the assignee must be nameable; relations=%v", relation)
	}
	if relation[ltCXO2] != domain.MentionRelationLeadership {
		t.Fatalf("a ticked leader must be nameable; relations=%v", relation)
	}
	if _, present := relation[ltDirector2]; present {
		t.Fatalf("a ticked person with NO leadership grant must never be mentionable; relations=%v", relation)
	}

	// 2. THE PERMISSION RULE. Mentioning the non-visible person is REFUSED, and the refusal
	// writes NOTHING -- no note, no mention row, no participant grant.
	_, err = repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: director, TaskID: task.TaskID,
		Comment: "Taking a look now", MentionUserIDs: []string{ltDirector2},
		IdempotencyKey: "mention-refused-1",
	})
	if !errors.Is(err, domain.ErrMentionNotVisible) {
		t.Fatalf("mentioning a person who cannot see the task must be refused, got %v", err)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM leadership_task_notes WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, ltTenant, task.TaskID); n != 0 {
		t.Fatalf("the refused comment wrote %d notes, want 0 -- the whole write must roll back", n)
	}
	if n := countRows(t, ctx, pool, `SELECT count(*) FROM leadership_task_participants WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, ltTenant, task.TaskID); n != 0 {
		t.Fatalf("the refused comment granted %d participants, want 0", n)
	}

	// 3. A GOOD MENTION IS STORED STRUCTURALLY, the body stays the plain text that was typed,
	// and the mentioned person becomes a participant who may OPEN the task.
	after, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: director, TaskID: task.TaskID,
		Comment: "@Manohar please confirm the vendor rate", MentionUserIDs: []string{ltCXO2},
		IdempotencyKey: "mention-ok-1",
	})
	if err != nil {
		t.Fatalf("set comment with a mention: %v", err)
	}
	if len(after.Notes) != 1 {
		t.Fatalf("notes = %+v, want exactly one", after.Notes)
	}
	note := after.Notes[0]
	if note.Body != "@Manohar please confirm the vendor rate" {
		t.Fatalf("the note body must stay the plain text that was typed, got %q", note.Body)
	}
	if len(note.Mentions) != 1 || note.Mentions[0].UserID != ltCXO2 || note.Mentions[0].Name != "Manohar" {
		t.Fatalf("the note must carry its resolved mention as a chip; mentions=%+v", note.Mentions)
	}
	if note.Mentions[0].MentionedByUserID != ltDirector {
		t.Fatalf("the mention must record WHO named them, got %q", note.Mentions[0].MentionedByUserID)
	}

	// A MENTION GRANTS READ: the mentioned leader can open the task; the person who was never
	// named still cannot.
	mentioned := domain.Actor{UserID: ltCXO2}
	stranger := domain.Actor{UserID: ltDirector2}
	reread, err := repo.GetTask(ctx, ltTenant, task.TaskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if !reread.IsParticipant(mentioned) || !reread.CanRead(mentioned) {
		t.Fatalf("a mentioned person must be able to open the task the push points at; participants=%v", reread.ParticipantUserIDs)
	}
	if stranger.UserID == "" || reread.CanRead(stranger) {
		t.Fatalf("a person who is neither party, monitor nor mentioned must NOT be able to read the task")
	}

	// 4. THE OUTBOX ANNOUNCES THE NOTE, carrying the ALREADY VALIDATED mention list, so the
	// notifier never has to re-decide who may hear about the task.
	if n := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages
		  WHERE tenant_id=$1::uuid AND aggregate_id=$2::uuid AND event_type='leadership_task.commented'
		    AND payload->'payload'->>'note_id' = $3
		    AND payload->'payload'->'mentioned_user_ids' ? $4`,
		ltTenant, task.TaskID, note.NoteID, ltCXO2); n != 1 {
		t.Fatalf("leadership_task.commented rows naming the mention = %d, want 1", n)
	}

	// 5. AN EXACT REPLAY IS A NO-OP: one note, one mention, one participant, one event.
	if _, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: director, TaskID: task.TaskID,
		Comment: "@Manohar please confirm the vendor rate", MentionUserIDs: []string{ltCXO2},
		IdempotencyKey: "mention-ok-1",
	}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	for _, check := range []struct {
		label string
		query string
	}{
		{"notes", `SELECT count(*) FROM leadership_task_notes WHERE tenant_id=$1::uuid AND task_id=$2::uuid`},
		{"mentions", `SELECT count(*) FROM leadership_task_mentions WHERE tenant_id=$1::uuid AND task_id=$2::uuid`},
		{"participants", `SELECT count(*) FROM leadership_task_participants WHERE tenant_id=$1::uuid AND task_id=$2::uuid`},
		{"commented events", `SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND aggregate_id=$2::uuid AND event_type='leadership_task.commented'`},
	} {
		if n := countRows(t, ctx, pool, check.query, ltTenant, task.TaskID); n != 1 {
			t.Fatalf("after the replay, %s = %d, want 1", check.label, n)
		}
	}

	// ...and the SAME key with a DIFFERENT mention list is a conflict, never a silent drop of
	// the people the second send named.
	if _, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: director, TaskID: task.TaskID,
		Comment: "@Manohar please confirm the vendor rate", MentionUserIDs: []string{ltCXO},
		IdempotencyKey: "mention-ok-1",
	}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key, different people named: %v", err)
	}

	// 6. AN EDIT ANNOUNCES ITSELF so the other party hears the ask changed.
	edited, err := repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID,
		Title: "Fix the CPT water line today", Body: "Please look at this.",
		RowVersion: after.RowVersion, IdempotencyKey: "mention-edit-1",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if edited.Title != "Fix the CPT water line today" {
		t.Fatalf("edited title = %q", edited.Title)
	}
	if n := countRows(t, ctx, pool,
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1::uuid AND aggregate_id=$2::uuid AND event_type='leadership_task.updated'`,
		ltTenant, task.TaskID); n != 1 {
		t.Fatalf("leadership_task.updated rows = %d, want 1", n)
	}
}

func countRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return n
}
