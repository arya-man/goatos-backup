package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A PEN VISIT CANNOT BE FILMED BEFORE ITS DAY, on the real write path (maintainer decision
// 2026-09-25). The work was done on day D; the visit is planned for D+1. A submit whose SERVER
// business date is still D is refused under the row lock and writes nothing; on D+1 it lands.
func TestPenVisitSubmitIsRefusedBeforeItsPlannedDayPg(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedPenVisitFixture(t, ctx, pool)

	const source = "2026-09-06"
	const planned = "2026-09-07"
	now := istInstant(source, 22) // late on the work day itself
	repo := NewRepository(pool, 10*time.Second).WithClock(func() time.Time { return now })
	seedWork(t, ctx, pool, "early-vacc", pvParkCBE, pvShedCastro, "2", "vaccination", "vaccination_proof", istInstant(source, 10))
	if _, _, err := repo.Materialize(ctx, pvTenant, source, planned, now); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	page, err := repo.ListMine(ctx, ports.ListParams{TenantID: pvTenant, UserID: pvDinakar, States: domain.StatesForFilter(domain.FilterToDo), Limit: 20})
	if err != nil || len(page.Rows) != 1 {
		t.Fatalf("list: %d rows err %v", len(page.Rows), err)
	}
	visit := page.Rows[0]
	if visit.PlannedDate != planned {
		t.Fatalf("planned = %s, want %s", visit.PlannedDate, planned)
	}
	actor := domain.Actor{UserID: pvDinakar}

	_, err = repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: visit.TaskID, ProofRef: pvProof, RowVersion: visit.RowVersion, IdempotencyKey: "early-1"})
	var notOpen *domain.NotOpenYetError
	if !errors.Is(err, domain.ErrNotOpenYet) || !errors.As(err, &notOpen) || notOpen.Message() != "This visit opens on 07/09/2026." {
		t.Fatalf("submit on the work day: err = %v, want \"This visit opens on 07/09/2026.\"", err)
	}
	after, err := repo.GetTask(ctx, pvTenant, visit.TaskID)
	if err != nil || after.Status != domain.StatusOpen || after.ProofRef != nil {
		t.Fatalf("a refused early submit must write nothing: %+v err %v", after, err)
	}

	now = istInstant(planned, 8)
	submitted, err := repo.Submit(ctx, ports.SubmitParams{TenantID: pvTenant, Actor: actor, TaskID: visit.TaskID, ProofRef: pvProof, RowVersion: visit.RowVersion, IdempotencyKey: "on-day-1"})
	if err != nil || submitted.Status != domain.StatusPendingVerification {
		t.Fatalf("submit on the planned day: %+v err %v", submitted.Status, err)
	}
}
