package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// P10 (docs/perf/2026-09-24-stg-latency): each admin pen-routines read is ONE round trip. The
// routine list used to read routines, then their role holders, then the park options (3 sequential
// statements); the drawer catalog its pens, then the role holders (2); the Today table its page,
// then its summary (2). None depends on another's result, so each read is one pgx.Batch.
func TestAdminPenRoutineReadsAreOneRoundTripEach(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	seedRoutineFixture(t, ctx, pool)
	const today = "2026-09-16"
	now := istInstant(today, 7).Add(5 * time.Minute)
	writer := NewRepository(pool, 15*time.Second).WithClock(func() time.Time { return now })
	routine, err := writer.CreateRoutine(ctx, ports.WriteParams{TenantID: prTenant, ActorID: prHead, IdempotencyKey: "rt-create", TraceID: "rt"}, domain.Definition{
		ParkID: prParkCBE, Name: "Pen cleaning", Instruction: "Sweep.", ScopeKind: domain.ScopeAllPens, OccupiedOnly: true,
		CadenceKind: domain.CadenceWeekly, Weekdays: []int{1, 3}, NotifyTime: "07:00", ReviewKind: domain.ReviewVerifier,
		Evidence: evidenceOneQuestion(domain.PresenceRequired, 1), AssigneeRoles: []string{domain.RoleParkHead}, StartDate: "2026-09-01",
	})
	if err != nil {
		t.Fatalf("create routine: %v", err)
	}
	if _, err := writer.Materialize(ctx, prTenant, today, today, now); err != nil {
		t.Fatalf("materialize: %v", err)
	}

	counted, trips := pgtest.CountingPool(t, ctx, pool)
	repo := NewRepository(counted, 15*time.Second).WithClock(func() time.Time { return now })
	measure := func(name string, read func() error) {
		t.Helper()
		if err := read(); err != nil { // warm the statement cache
			t.Fatalf("%s (warm): %v", name, err)
		}
		trips.Reset()
		if err := read(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if n := trips.Trips(); n != 1 {
			t.Fatalf("%s took %d round trips, want 1:\n%s", name, n, strings.Join(trips.SQL(), "\n---\n"))
		}
	}

	var rows []ports.RoutineListRow
	var parks []ports.Park
	for _, park := range []string{"", prParkCBE} {
		measure("routine list "+park, func() error {
			var err error
			rows, parks, err = repo.ListRoutinesAndParks(ctx, ports.RoutineListParams{TenantID: prTenant, ParkID: park, Today: today})
			return err
		})
		if len(rows) != 1 || rows[0].Definition.RoutineID != routine.RoutineID || len(rows[0].Definition.People) != 2 || len(parks) != 2 {
			t.Fatalf("list %q: rows=%+v parks=%+v (want the routine with its 2 park heads, and both parks)", park, rows, parks)
		}
	}

	var pens []ports.CatalogPen
	var roles []ports.RoleHolders
	measure("catalog", func() error {
		var err error
		pens, roles, err = repo.Catalog(ctx, prTenant, prParkCBE)
		return err
	})
	if len(pens) != 4 || len(roles) != len(domain.AssignableRoles) {
		t.Fatalf("catalog pens=%+v roles=%+v", pens, roles)
	}

	var page ports.ParkPage
	measure("today table", func() error {
		var err error
		page, err = repo.ListForPark(ctx, ports.ParkListParams{TenantID: prTenant, ParkID: prParkCBE, BusinessDate: today})
		return err
	})
	if len(page.Rows) == 0 || page.Summary.Due+page.Summary.Delayed != len(page.Rows) {
		t.Fatalf("today table rows=%d summary=%+v", len(page.Rows), page.Summary)
	}
}
