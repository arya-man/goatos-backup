package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// A routine is for ONE person (maintainer decision 2026-09-26: "pick people, like Tasks ... it
// will go to one only"). On real SQL:
//   - the chosen person, and nobody else holding the same role, is owed it (the second CBE park
//     head and the other park's head see nothing);
//   - a park head cannot be given the OTHER park's routine (refused, nothing written);
//   - the server derives the roles through which the person may do it at that park;
//   - a person who later loses the role stops being owed it, and the work does NOT fall to
//     another holder -- the routine raises nothing and is named in RoutinesWithoutAssignee.
func TestPenRoutineOnePersonAssigneeTwoParkHeadsParkScopeRevokedGrantPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)

	const today = "2026-09-16"
	now := istInstant(today, 9)
	repo := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: prTenant, ActorID: prCXO, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	def := func(name, park, person string, pens []domain.PenRef) domain.Definition {
		return domain.Definition{
			ParkID: park, Name: name, ScopeKind: domain.ScopeSelectedPens, Pens: pens,
			CadenceKind: domain.CadenceDaily, StartDate: today, NotifyTime: "07:00", ReviewKind: domain.ReviewNone,
			Evidence: evidenceOneQuestion(domain.PresenceOff, 0), AssigneeUserID: person,
		}
	}
	castro2 := []domain.PenRef{{ShedID: prShedCastro, Partition: "2"}}

	// The CPT park head cannot be given a CBE routine: refused, nothing written.
	if _, err := repo.CreateRoutine(ctx, write("wrong-park"), def("Trough", prParkCBE, prCPTHead, castro2)); !errors.Is(err, domain.ErrNotAssignable) {
		t.Fatalf("CPT head on a CBE routine err = %v, want ErrNotAssignable", err)
	}
	var written int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pen_routine_definitions WHERE tenant_id = $1::uuid`, prTenant).Scan(&written); err != nil || written != 0 {
		t.Fatalf("a refused routine wrote %d rows / %v", written, err)
	}

	head, err := repo.CreateRoutine(ctx, write("head"), def("Trough", prParkCBE, prHead, castro2))
	if err != nil {
		t.Fatalf("create for the CBE head: %v", err)
	}
	if head.AssigneeUserID != prHead || len(head.AssigneeRoles) != 1 || head.AssigneeRoles[0] != domain.RoleParkHead || len(head.People) != 1 || head.People[0].UserID != prHead {
		t.Fatalf("routine for the CBE head = %+v, want exactly that person via park_head", head)
	}
	director, err := repo.CreateRoutine(ctx, write("director"), def("Vaccine fridge", prParkCPT, prPCDirector, []domain.PenRef{{ShedID: prShedCPT}}))
	if err != nil {
		t.Fatalf("create for the PC director at CPT: %v", err)
	}
	if len(director.People) != 1 || director.People[0].UserID != prPCDirector {
		t.Fatalf("director routine people = %+v", director.People)
	}

	result, err := repo.Materialize(ctx, prTenant, today, today, now)
	if err != nil || result.Created != 2 {
		t.Fatalf("materialize = %+v / %v, want 2", result, err)
	}
	owed := func(user string) int {
		t.Helper()
		page, err := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: user, Limit: 50})
		if err != nil {
			t.Fatalf("list %s: %v", user, err)
		}
		return len(page.Rows)
	}
	// ONE person each: the chosen one sees it; every other holder of the same role does not.
	for user, want := range map[string]int{prHead: 1, prSecond: 0, prCPTHead: 0, prPCDirector: 1, prCXO: 0} {
		if got := owed(user); got != want {
			t.Fatalf("%s is owed %d routine checks, want %d", user, got, want)
		}
	}
	// The second CBE park head cannot submit the head's check either.
	page, _ := repo.ListMine(ctx, ports.ListParams{TenantID: prTenant, UserID: prHead, Limit: 50})
	task := page.Rows[0]
	if _, err := repo.Submit(ctx, ports.SubmitParams{TenantID: prTenant, Actor: domain.Actor{UserID: prSecond}, TaskID: task.TaskID, Answers: rawAnswers(t, map[string]any{"cleaned": "yes"}), RowVersion: task.RowVersion, IdempotencyKey: "second-on-head"}); !errors.Is(err, domain.ErrNotAssignee) {
		t.Fatalf("second head submitting the head's check err = %v, want ErrNotAssignee", err)
	}

	// The head loses the role: the routine is owed by nobody (not by the second head), and the
	// next day raises nothing for it, naming it as a routine without an assignee.
	if _, err := pool.Exec(ctx, `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, prTenant, prHead); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got := owed(prSecond); got != 0 {
		t.Fatalf("after the head lost the role the second head is owed %d, want 0", got)
	}
	next, err := repo.Materialize(ctx, prTenant, "2026-09-17", "2026-09-17", now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("materialize next day: %v", err)
	}
	named := false
	for _, ref := range next.RoutinesWithoutAssignee {
		named = named || ref.RoutineID == head.RoutineID
	}
	if !named || next.Created != 1 {
		t.Fatalf("next day = %+v, want only the director's check and the head's routine named as unassigned", next)
	}
}
