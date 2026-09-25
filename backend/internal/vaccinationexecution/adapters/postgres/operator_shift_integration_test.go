package postgres

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	eventbuspublisher "github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/eventbus"
	outboxapp "github.com/vgoats/goatos/backend/internal/outbox/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

// TestOperatorShiftWritePathOnRealPostgres proves the only production write path for
// vaccination_operator_shift_config end to end on real Postgres. Before it existed a park added on
// Configuration > Items & settings could never be given vaccination operators, because the
// operator-assignment config refuses any operator without a shift row for the park.
//
// It proves, in order: a shift is set for a park operator and vaccination.roster.changed lands in
// the outbox in the same transaction; an exact replay writes nothing more; the same key with a
// different body is refused; an update re-plans; an operator whose home park is another park is
// refused with nothing written; clearing the park's default or selected operator is refused; and
// clearing an unused operator is allowed and re-plans.
func TestOperatorShiftWritePathOnRealPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const (
		tenant     = "00000000-0000-4000-8000-000000000001"
		park       = "00000000-0000-4000-8000-0000000cb001"
		otherPark  = "00000000-0000-4000-8000-0000000cb002"
		operator   = "00000000-0000-4000-8000-0000000cb011"
		operator2  = "00000000-0000-4000-8000-0000000cb012"
		operator3  = "00000000-0000-4000-8000-0000000cb013"
		foreignOp  = "00000000-0000-4000-8000-0000000cb021"
		inactiveOp = "00000000-0000-4000-8000-0000000cb022"
	)
	for _, p := range []struct{ id, code string }{{park, "PARK-SH1"}, {otherPark, "PARK-SH2"}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'park', $3, $3, 'active')
ON CONFLICT (location_id) DO NOTHING`, p.id, tenant, p.code); err != nil {
			t.Fatalf("seed park %s: %v", p.code, err)
		}
	}
	for i, m := range []struct{ id, park, status string }{
		{operator, park, "active"},
		{operator2, park, "active"},
		{operator3, park, "active"},
		{foreignOp, otherPark, "active"},
		{inactiveOp, park, "inactive"},
	} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, 'operator', $6::uuid)
ON CONFLICT (workforce_member_id) DO NOTHING`, m.id, tenant, "OP-SH-"+string(rune('A'+i)), "Shift Operator "+string(rune('A'+i)), m.status, m.park); err != nil {
			t.Fatalf("seed operator %d: %v", i, err)
		}
	}

	repo := NewRepository(pool, 5*time.Second)
	set := func(key, operatorID string, start, end int, weekOff string) (domain.OperatorShift, bool, error) {
		return repo.SetOperatorShift(ctx, ports.OperatorShiftWrite{
			TenantID:       tenant,
			IdempotencyKey: key,
			Shift: domain.OperatorShift{
				ParkID: park, OperatorID: operatorID, ShiftLabel: "am",
				ShiftStartMinute: start, ShiftEndMinute: end, WeekOffWeekday: weekOff,
			},
		})
	}
	rosterEvents := func() int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'vaccination.roster.changed' AND aggregate_id = $2::uuid
  AND headers->>'producer' = 'vaccination-execution.OperatorShift'`, tenant, park).Scan(&n); err != nil {
			t.Fatalf("count roster events: %v", err)
		}
		return n
	}
	shiftRow := func(operatorID string) (label string, start, end int, weekOff string, found bool) {
		t.Helper()
		err := pool.QueryRow(ctx, `
SELECT shift_label, shift_start_minute, shift_end_minute, COALESCE(week_off_weekday, '')
FROM vaccination_operator_shift_config
WHERE tenant_id = $1::uuid AND operator_id = $2::uuid AND park_id = $3::uuid`, tenant, operatorID, park).Scan(&label, &start, &end, &weekOff)
		if err != nil {
			return "", 0, 0, "", false
		}
		return label, start, end, weekOff, true
	}

	// 1. Set a shift for a park operator: row + one roster.changed event.
	got, replay, err := set("shift-key-0001", operator, 480, 1020, "sunday")
	if err != nil || replay {
		t.Fatalf("first set: replay=%v err=%v", replay, err)
	}
	if got.DisplayName != "Shift Operator A" {
		t.Fatalf("display name not resolved: %+v", got)
	}
	if _, start, end, weekOff, ok := shiftRow(operator); !ok || start != 480 || end != 1020 || weekOff != "sunday" {
		t.Fatalf("stored row wrong: ok=%v %d %d %q", ok, start, end, weekOff)
	}
	if n := rosterEvents(); n != 1 {
		t.Fatalf("first set must enqueue exactly one roster.changed in the same tx, got %d", n)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action = 'vaccination.operator_shift.set' AND resource_id = $2::uuid`, tenant, operator).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit row: n=%d err=%v", audits, err)
	}

	// 2. Exact replay: original result, no second event, no second audit.
	got, replay, err = set("shift-key-0001", operator, 480, 1020, "sunday")
	if err != nil || !replay || got.ShiftStartMinute != 480 {
		t.Fatalf("replay: replay=%v err=%v got=%+v", replay, err, got)
	}
	if n := rosterEvents(); n != 1 {
		t.Fatalf("a replay must not enqueue again, got %d events", n)
	}

	// 3. Same key, different body: refused, nothing changes.
	if _, _, err = set("shift-key-0001", operator, 540, 1020, "sunday"); !errors.Is(err, ports.ErrOperatorShiftIdempotencyConflict) {
		t.Fatalf("same key different body: want idempotency conflict, got %v", err)
	}
	if _, start, _, _, _ := shiftRow(operator); start != 480 {
		t.Fatalf("a refused replay must not write, start=%d", start)
	}

	// 4. Update under a new key: row replaced, another roster.changed (re-plan).
	if _, replay, err = set("shift-key-0002", operator, 540, 1080, ""); err != nil || replay {
		t.Fatalf("update: replay=%v err=%v", replay, err)
	}
	if _, start, end, weekOff, _ := shiftRow(operator); start != 540 || end != 1080 || weekOff != "" {
		t.Fatalf("update not stored: %d %d %q", start, end, weekOff)
	}
	if n := rosterEvents(); n != 2 {
		t.Fatalf("an update must re-plan, got %d events", n)
	}
	// A write that changes nothing does not re-plan.
	if _, _, err = set("shift-key-0003", operator, 540, 1080, ""); err != nil {
		t.Fatalf("no-op set: %v", err)
	}
	if n := rosterEvents(); n != 2 {
		t.Fatalf("an unchanged shift must not re-plan, got %d events", n)
	}

	// 5. Operator whose home park is another park, or who is not active: refused, nothing written,
	// and the key is not burnt (the refused transaction rolled back).
	for _, op := range []string{foreignOp, inactiveOp} {
		if _, _, err = set("shift-key-foreign-"+op[len(op)-3:], op, 480, 1020, ""); !errors.Is(err, ports.ErrOperatorNotActiveInPark) {
			t.Fatalf("operator %s: want ErrOperatorNotActiveInPark, got %v", op, err)
		}
		if _, _, _, _, ok := shiftRow(op); ok {
			t.Fatalf("operator %s: a refused operator must not get a shift row", op)
		}
	}
	var keys int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys WHERE idempotency_key LIKE '%shift-key-foreign-%'`).Scan(&keys); err != nil || keys != 0 {
		t.Fatalf("a refused write must not keep its idempotency key: n=%d err=%v", keys, err)
	}

	// 6. The park's default and selected operators cannot lose their shift.
	for _, op := range []string{operator2, operator3} {
		if _, _, err = set("shift-key-seed-"+op[len(op)-3:], op, 780, 1260, ""); err != nil {
			t.Fatalf("seed shift %s: %v", op, err)
		}
	}
	if _, err := repo.UpsertOperatorAssignmentConfig(ctx, tenant, domain.OperatorAssignmentConfig{
		ParkID: park, ActiveOperatorsPerDay: 2, DefaultOperatorID: operator, SelectedOperatorIDs: []string{operator2},
	}); err != nil {
		t.Fatalf("seed assignment config: %v", err)
	}
	eventsBeforeClear := rosterEvents()
	for _, op := range []string{operator, operator2} {
		_, err := repo.ClearOperatorShift(ctx, ports.OperatorShiftClear{TenantID: tenant, IdempotencyKey: "clear-inuse-" + op[len(op)-3:], ParkID: park, OperatorID: op})
		if !errors.Is(err, ports.ErrOperatorShiftInUse) {
			t.Fatalf("clearing assigned operator %s: want ErrOperatorShiftInUse, got %v", op, err)
		}
		if _, _, _, _, ok := shiftRow(op); !ok {
			t.Fatalf("a refused clear must keep the shift of %s", op)
		}
	}
	if n := rosterEvents(); n != eventsBeforeClear {
		t.Fatalf("a refused clear must not enqueue, got %d want %d", n, eventsBeforeClear)
	}

	// 7. Clearing an operator the assignment does not name: allowed, one event, replay is a no-op.
	replay, err = repo.ClearOperatorShift(ctx, ports.OperatorShiftClear{TenantID: tenant, IdempotencyKey: "clear-ok-0001", ParkID: park, OperatorID: operator3})
	if err != nil || replay {
		t.Fatalf("clear unused operator: replay=%v err=%v", replay, err)
	}
	if _, _, _, _, ok := shiftRow(operator3); ok {
		t.Fatalf("clear must delete the shift row")
	}
	if n := rosterEvents(); n != eventsBeforeClear+1 {
		t.Fatalf("clear must re-plan once, got %d want %d", n, eventsBeforeClear+1)
	}
	replay, err = repo.ClearOperatorShift(ctx, ports.OperatorShiftClear{TenantID: tenant, IdempotencyKey: "clear-ok-0001", ParkID: park, OperatorID: operator3})
	if err != nil || !replay {
		t.Fatalf("clear replay: replay=%v err=%v", replay, err)
	}
	if n := rosterEvents(); n != eventsBeforeClear+1 {
		t.Fatalf("a clear replay must not enqueue again, got %d", n)
	}
	// A fresh clear of a shift that no longer exists is a not-found, not a silent success.
	if _, err = repo.ClearOperatorShift(ctx, ports.OperatorShiftClear{TenantID: tenant, IdempotencyKey: "clear-ok-0002", ParkID: park, OperatorID: operator3}); !errors.Is(err, ports.ErrOperatorShiftNotFound) {
		t.Fatalf("clearing an absent shift: want ErrOperatorShiftNotFound, got %v", err)
	}

	assertOperatorShiftEventsConform(t, ctx, pool)
}

// assertOperatorShiftEventsConform checks every roster.changed this producer enqueued names the park
// as aggregate and payload, the contract OperatorConfigReplanHandler reads.
func assertOperatorShiftEventsConform(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var bad int
	if err := pool.QueryRow(ctx, `
SELECT count(*) FROM outbox_messages
WHERE event_type = 'vaccination.roster.changed'
  AND headers->>'producer' = 'vaccination-execution.OperatorShift'
  AND (payload->>'aggregate_type' <> 'park'
       OR payload->'payload'->>'park_id' IS NULL
       OR payload->>'aggregate_id' <> aggregate_id::text)`).Scan(&bad); err != nil {
		t.Fatalf("inspect roster events: %v", err)
	}
	if bad != 0 {
		t.Fatalf("%d roster.changed event(s) do not carry the park contract", bad)
	}

	// Drive the REAL schema-validating outbox relay: a non-conformant envelope surfaces as a failed
	// message, and every delivered roster.changed reaches the bus the replan consumer listens on.
	delivered := 0
	bus := eventbus.NewInProcessBus()
	bus.Subscribe("vaccination.roster.changed", eventbus.HandlerFunc(func(context.Context, eventbus.Event) error { delivered++; return nil }))
	schemaPath, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "contracts", "jsonschema", "domain-event-envelope.schema.json"))
	if err != nil {
		t.Fatalf("resolve envelope schema path: %v", err)
	}
	validator, err := outboxapp.NewEnvelopeValidator(schemaPath)
	if err != nil {
		t.Fatalf("build envelope validator: %v", err)
	}
	service := outboxapp.NewService(outboxpg.NewRepository(pool, 30*time.Second), eventbuspublisher.New(bus), validator, outboxapp.Config{
		Limit: 100, MaxAttempts: 5, LeaseTimeout: time.Minute,
	})
	result, err := service.RunUntilDrained(ctx)
	if err != nil {
		t.Fatalf("outbox relay drain: %v", err)
	}
	if result.FailedCount > 0 {
		t.Fatalf("outbox relay rejected %d message(s) as invalid envelopes", result.FailedCount)
	}
	if delivered == 0 {
		t.Fatalf("no roster.changed reached the bus")
	}
}
