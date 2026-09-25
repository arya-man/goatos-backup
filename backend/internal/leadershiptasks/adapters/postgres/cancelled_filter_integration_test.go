package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// CANCELLED TASKS ARE LISTED (maintainer decision 2026-09-25): on every scope, each filter chip's
// count equals the rows that filter lists -- including the new `cancelled` chip, which the Team
// progress row query used to hide behind its own `status <> 'cancelled'` while the status counts
// still reported a cancelled bucket.
func TestEveryFilterChipCountsWhatItListsIncludingCancelledPg(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedLeadershipFixture(t, ctx, pool)
	repo := NewRepository(pool, 10*time.Second)
	raiserActor := domain.Actor{UserID: ltDirector, CanRaise: true}
	assigneeActor := domain.Actor{UserID: ltCXO, CanAct: true}

	for i, status := range []string{domain.StatusOpen, domain.StatusInProgress, domain.StatusDone, domain.StatusCancelled, domain.StatusCancelled} {
		task, err := repo.Raise(ctx, raiseAs(ltDirector, ltCXO, fmt.Sprintf("Cancelled chip ask %d", i), fmt.Sprintf("cc-%d", i), nil))
		if err != nil {
			t.Fatalf("raise %d: %v", i, err)
		}
		switch status {
		case domain.StatusInProgress, domain.StatusDone:
			moved, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: assigneeActor, TaskID: task.TaskID, Status: domain.StatusInProgress, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("cc-mid-%d", i)})
			if err != nil {
				t.Fatal(err)
			}
			if status == domain.StatusDone {
				if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: assigneeActor, TaskID: task.TaskID, Status: domain.StatusDone, RowVersion: moved.RowVersion, IdempotencyKey: fmt.Sprintf("cc-done-%d", i)}); err != nil {
					t.Fatal(err)
				}
			}
		case domain.StatusCancelled:
			if _, err := repo.ChangeStatus(ctx, ports.StatusParams{TenantID: ltTenant, Actor: raiserActor, TaskID: task.TaskID, Status: domain.StatusCancelled, RowVersion: task.RowVersion, IdempotencyKey: fmt.Sprintf("cc-cancel-%d", i)}); err != nil {
				t.Fatal(err)
			}
		}
	}

	now := time.Now()
	for _, sc := range []struct{ scope, user string }{
		{domain.ScopeTeamProgress, ltCXO2}, {domain.ScopeAssignedByMe, ltDirector}, {domain.ScopeAssignedToMe, ltCXO},
	} {
		for _, key := range domain.FilterKeys {
			if key == domain.FilterOverdue {
				continue // counted separately against the farm clock; no deadlines in this fixture
			}
			page, err := repo.ListTasks(ctx, ports.ListParams{
				TenantID: ltTenant, UserID: sc.user, Scope: sc.scope, Statuses: domain.StatusesForFilter(key),
				OverdueAt: now, Limit: 50,
			})
			if err != nil {
				t.Fatalf("%s/%s: %v", sc.scope, key, err)
			}
			if got, want := len(page.Rows), domain.FilterCount(key, page.StatusCounts); got != want {
				t.Fatalf("%s filter=%s lists %d rows but its chip counts %d", sc.scope, key, got, want)
			}
			if key == domain.FilterCancelled && len(page.Rows) != 2 {
				t.Fatalf("%s filter=cancelled lists %d rows, want the 2 cancelled tasks", sc.scope, len(page.Rows))
			}
		}
	}
}
