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

// The activity feed (migration 000349) is written by the SAME transaction as each mutation
// and read back newest first on the single-task and the paged reads alike. This walks a task
// through raise, status, comment, edit and cancel and asserts the feed after each step --
// including that an edit re-saving the same values writes NOTHING and a refused write leaves
// no row behind.
func TestLeadershipTaskActivityFeedIsWrittenInTheMutationTransaction(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	clock := time.Date(2026, 9, 18, 4, 0, 0, 0, time.UTC)
	repo := NewRepository(pool, 10*time.Second).WithClock(func() time.Time { clock = clock.Add(time.Minute); return clock })
	director := domain.Actor{UserID: ltDirector, CanRaise: true}
	cxo := domain.Actor{UserID: ltCXO, CanAct: true}

	kinds := func(task domain.Task) []string {
		out := make([]string, 0, len(task.Activity))
		for _, e := range task.Activity {
			out = append(out, e.Kind)
		}
		return out
	}
	equal := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// 1. The raise itself is the feed's first row, by the raiser, at the raise instant.
	deadline := time.Date(2026, 9, 20, 11, 30, 0, 0, time.UTC)
	params := raiseParams(ltCXO, "Check the west fence", "ev-raise-1")
	params.DeadlineAt = &deadline
	task, err := repo.Raise(ctx, params)
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	if !equal(kinds(task), []string{domain.EventCreated}) || task.Activity[0].ActorUserID != ltDirector || task.Activity[0].ActorName != "Hemant" || !task.Activity[0].OccurredAt.Equal(task.RaisedAt) {
		t.Fatalf("after raise: %+v", task.Activity)
	}

	// 2. A refused status move (stale version) writes no row.
	if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: 99, IdempotencyKey: "ev-st-stale"}); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale: %v", err)
	}
	started, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: task.RowVersion, IdempotencyKey: "ev-st-1"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !equal(kinds(started), []string{domain.EventStatusChanged, domain.EventCreated}) {
		t.Fatalf("after start: %v", kinds(started))
	}
	if e := started.Activity[0]; e.FromValue != domain.StatusOpen || e.ToValue != domain.StatusInProgress || e.ActorUserID != ltCXO || e.ActorName != "Ravi" {
		t.Fatalf("status event = %+v", e)
	}
	// An exact replay of the same move returns the task and writes no second row.
	replay, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: task.RowVersion, IdempotencyKey: "ev-st-1"})
	if err != nil || len(replay.Activity) != 2 {
		t.Fatalf("replay: %v %v", err, kinds(replay))
	}

	// 3. A note is a `commented` row that names its note.
	commented, err := repo.SetComment(ctx, ports.CommentParams{TenantID: ltTenant, Actor: cxo, TaskID: task.TaskID, Comment: "On it.", IdempotencyKey: "ev-c-1"})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if !equal(kinds(commented), []string{domain.EventCommented, domain.EventStatusChanged, domain.EventCreated}) || commented.Activity[0].NoteID != commented.Notes[0].NoteID {
		t.Fatalf("after comment: %+v", commented.Activity)
	}

	// 4. An edit writes one row PER FIELD that moved: title and deadline here, not the
	// unchanged brief.
	newDeadline := time.Date(2026, 9, 22, 11, 30, 0, 0, time.UTC)
	edited, err := repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: "Check the west fence (revised)", Body: task.Body,
		DeadlineAt: &newDeadline, RowVersion: commented.RowVersion, IdempotencyKey: "ev-e-1",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if !equal(kinds(edited), []string{domain.EventDeadlineChanged, domain.EventTitleChanged, domain.EventCommented, domain.EventStatusChanged, domain.EventCreated}) {
		t.Fatalf("after edit: %v", kinds(edited))
	}
	if e := edited.Activity[0]; e.FromValue != deadline.Format(time.RFC3339) || e.ToValue != newDeadline.Format(time.RFC3339) {
		t.Fatalf("deadline event = %+v", e)
	}
	if e := edited.Activity[1]; e.FromValue != "Check the west fence" || e.ToValue != "Check the west fence (revised)" {
		t.Fatalf("title event = %+v", e)
	}
	// Re-saving the same values is not a fact and writes nothing.
	same, err := repo.Edit(ctx, ports.EditParams{
		TenantID: ltTenant, ActorID: ltDirector, TaskID: task.TaskID, Title: edited.Title, Body: edited.Body,
		DeadlineAt: edited.DeadlineAt, RowVersion: edited.RowVersion, IdempotencyKey: "ev-e-2",
	})
	if err != nil || len(same.Activity) != 5 {
		t.Fatalf("same-value edit: %v %v", err, kinds(same))
	}

	// 5. A cancel is its own kind, carrying both ends.
	cancelled, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: director, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: same.RowVersion, IdempotencyKey: "ev-st-2"})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if e := cancelled.Activity[0]; e.Kind != domain.EventCancelled || e.FromValue != domain.StatusInProgress || e.ToValue != domain.StatusCancelled || e.ActorUserID != ltDirector {
		t.Fatalf("cancel event = %+v", e)
	}

	// 6. The paged read carries NO feed (2026-09-18): the board and the list render none of it,
	// it was 40% of the list payload, and the drawer fetches the detail row when a card opens.
	page, err := repo.ListTasks(ctx, ports.ListParams{TenantID: ltTenant, UserID: ltCXO, Scope: domain.ScopeAssignedToMe, Statuses: []string{domain.StatusCancelled}, Limit: 10})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("list: %v rows %d", err, len(page.Rows))
	}
	if len(page.Rows[0].Activity) != 0 {
		t.Fatalf("paged row carries a feed %v; the list payload must not", kinds(page.Rows[0]))
	}
	// Newest first is the stored order of the DETAIL read, not a client sort.
	for i := 1; i < len(cancelled.Activity); i++ {
		if cancelled.Activity[i].OccurredAt.After(cancelled.Activity[i-1].OccurredAt) {
			t.Fatalf("feed not newest first: %+v", cancelled.Activity)
		}
	}
}
