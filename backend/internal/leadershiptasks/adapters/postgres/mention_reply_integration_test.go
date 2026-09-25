package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// MENTIONS CAN REPLY on the real write path (maintainer decision 2026-09-25): a person a note
// mentioned may write a note back, still may not move the status, and someone never mentioned is
// refused with the note-worded ErrNotOnTask rather than the status-worded ErrNotAssignee.
func TestMentionedParticipantMayReplyButNotMoveStatusPg(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	director := domain.Actor{UserID: ltDirector, CanRaise: true}
	// A mentioned person holding no monitor authority over the desk, and a stranger.
	mentioned := domain.Actor{UserID: ltCXO2, CanAct: true}
	stranger := domain.Actor{UserID: ltDirector2, CanAct: true}

	task, err := repo.Raise(ctx, raiseParams(ltCXO, "Check the CPT borewell pump", "reply-raise-1"))
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if _, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: director, TaskID: task.TaskID,
		Comment: "@Manohar can you confirm?", MentionUserIDs: []string{ltCXO2}, IdempotencyKey: "reply-mention-1",
	}); err != nil {
		t.Fatalf("mention: %v", err)
	}

	replied, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: mentioned, TaskID: task.TaskID,
		Comment: "Confirmed, the pump was serviced on Monday.", IdempotencyKey: "reply-1",
	})
	if err != nil {
		t.Fatalf("a mentioned participant's reply must be accepted, got %v", err)
	}
	if !replied.CanComment(mentioned) {
		t.Fatal("the served task must say the mentioned participant can comment (can_comment)")
	}
	if len(replied.Notes) != 2 {
		t.Fatalf("notes after the reply = %d, want 2", len(replied.Notes))
	}

	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{
		TenantID: ltTenant, Actor: mentioned, TaskID: task.TaskID, Status: domain.StatusDone,
		RowVersion: replied.RowVersion, IdempotencyKey: "reply-status-1",
	}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("a mentioned participant moving status must be refused ErrNotAssignee, got %v", err)
	}

	if _, err := repo.SetComment(ctx, ports.CommentParams{
		TenantID: ltTenant, Actor: stranger, TaskID: task.TaskID,
		Comment: "Me too", IdempotencyKey: "reply-stranger-1",
	}); !errors.Is(err, domain.ErrNotOnTask) {
		t.Fatalf("a note from someone never on the task must be refused ErrNotOnTask, got %v", err)
	}
}
