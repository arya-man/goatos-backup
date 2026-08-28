package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workforce/domain"
	"github.com/vgoats/goatos/backend/internal/workforce/ports"
)

const (
	clockTenant  = "00000000-0000-4000-8000-000000000001"
	clockMember  = "97000000-0000-4000-8000-000000000201"
	clockActor   = "91000000-0000-4000-8000-000000000201"
	clockMember2 = "97000000-0000-4000-8000-000000000202"
	clockActor2  = "91000000-0000-4000-8000-000000000202"
)

func clockPunch(member, user, eventType, key, date string, at time.Time) ports.ClockPunchCommand {
	return ports.ClockPunchCommand{
		TenantID:          clockTenant,
		WorkforceMemberID: member,
		UserID:            user,
		EventType:         eventType,
		IdempotencyKey:    key,
		BusinessDate:      date,
		EffectiveAt:       at,
		CapturedAt:        at,
		Location:          domain.ClockLocation{Status: "captured"},
		NetworkType:       "online",
		DeviceModel:       "SM-A15",
		AppVersion:        "0.1.37",
	}
}

// TestClockPunchPairsOneDayWithDockerPostgres proves the write-path contract on
// the real database: first punch, exact replay (no second row), duplicate
// clock-in refusal, clock-out pairing with backend-owned worked_minutes, and
// the self-healing auto-close of a stale open day (D4: no invented hours).
func TestClockPunchPairsOneDayWithDockerPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	for i, m := range [][2]string{{clockMember, clockActor}, {clockMember2, clockActor2}} {
		if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'Clock Test Member', 'active', 'operator')`,
			m[0], clockTenant, m[1], fmt.Sprintf("CLOCK-%02d", i)); err != nil {
			t.Fatalf("seed workforce_members: %v", err)
		}
	}

	repo := NewRepository(pool, 5*time.Second)

	dayOne := "2026-08-27"
	in := time.Date(2026, 8, 27, 2, 42, 0, 0, time.UTC) // 08:12 IST

	// First clock-in opens the day.
	rec, err := repo.RecordClockPunch(ctx, clockPunch(clockMember, clockActor, "clock_in", "in-1", dayOne, in))
	if err != nil {
		t.Fatalf("clock_in: %v", err)
	}
	if rec.Entry.Status != "open" || rec.Replayed {
		t.Fatalf("first clock_in: want fresh open entry, got %+v", rec)
	}

	// Exact replay returns the ORIGINAL entry and writes nothing new.
	replay, err := repo.RecordClockPunch(ctx, clockPunch(clockMember, clockActor, "clock_in", "in-1", dayOne, in))
	if err != nil {
		t.Fatalf("clock_in replay: %v", err)
	}
	if !replay.Replayed || replay.Entry.ClockEntryID != rec.Entry.ClockEntryID {
		t.Fatalf("replay must return the original entry; got %+v", replay)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM workforce_clock_events WHERE tenant_id=$1 AND workforce_member_id=$2`,
		clockTenant, clockMember).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("replay must not write a second event; got %d", eventCount)
	}

	// A second clock-in with a NEW key on the same day is refused (D4).
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember, clockActor, "clock_in", "in-2", dayOne, in.Add(time.Hour))); !errors.Is(err, ports.ErrAlreadyClockedIn) {
		t.Fatalf("second clock_in: want ErrAlreadyClockedIn, got %v", err)
	}

	// TWO people share the phone's day-scoped client key (clock:<date>:in) —
	// the reservation is member-scoped server-side, so the second person's
	// punch must NOT collide with the first (2026-08-28 E2E regression).
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember2, clockActor2, "clock_in", "in-1", dayOne, in.Add(2*time.Minute))); !errors.Is(err, nil) {
		t.Fatalf("second member reusing the shared client key: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM workforce_clock_entries WHERE workforce_member_id=$1; `, clockMember2); err != nil {
		t.Fatalf("reset member2 entries: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM workforce_clock_events WHERE workforce_member_id=$1`, clockMember2); err != nil {
		t.Fatalf("reset member2 events: %v", err)
	}

	// Clock-out without a clock-in (other member) is refused.
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember2, clockActor2, "clock_out", "out-x", dayOne, in.Add(9*time.Hour))); !errors.Is(err, ports.ErrNotClockedIn) {
		t.Fatalf("clock_out without in: want ErrNotClockedIn, got %v", err)
	}

	// Clock-out closes the day with backend-owned minutes.
	out := in.Add(9*time.Hour + 29*time.Minute)
	closed, err := repo.RecordClockPunch(ctx, clockPunch(clockMember, clockActor, "clock_out", "out-1", dayOne, out))
	if err != nil {
		t.Fatalf("clock_out: %v", err)
	}
	if closed.Entry.Status != "closed" || closed.Entry.WorkedMinutes == nil || *closed.Entry.WorkedMinutes != 9*60+29 {
		t.Fatalf("clock_out: want closed 569m, got %+v", closed.Entry)
	}

	// Second clock-out refused.
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember, clockActor, "clock_out", "out-2", dayOne, out.Add(time.Minute))); !errors.Is(err, ports.ErrAlreadyClockedOut) {
		t.Fatalf("second clock_out: want ErrAlreadyClockedOut, got %v", err)
	}

	// Member 2 opens dayOne and never clocks out; their NEXT day's clock-in
	// auto-closes the stale day with NO invented hours.
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember2, clockActor2, "clock_in", "m2-in-1", dayOne, in)); err != nil {
		t.Fatalf("m2 clock_in: %v", err)
	}
	dayTwo := "2026-08-28"
	if _, err := repo.RecordClockPunch(ctx, clockPunch(clockMember2, clockActor2, "clock_in", "m2-in-2", dayTwo, in.Add(24*time.Hour))); err != nil {
		t.Fatalf("m2 next-day clock_in: %v", err)
	}
	var status string
	var worked *int
	if err := pool.QueryRow(ctx, `
SELECT status, worked_minutes FROM workforce_clock_entries
WHERE tenant_id=$1 AND workforce_member_id=$2 AND business_date=$3::date`,
		clockTenant, clockMember2, dayOne).Scan(&status, &worked); err != nil {
		t.Fatalf("read stale day: %v", err)
	}
	if status != "auto_closed" || worked != nil {
		t.Fatalf("stale open day: want auto_closed with NULL minutes, got %s %v", status, worked)
	}

	// The presence read agrees with the writes: one page, whole-roster grain.
	page, err := repo.ListClockPresence(ctx, ports.ClockPresenceParams{
		TenantID: clockTenant, BusinessDate: dayOne, Limit: 20,
	})
	if err != nil {
		t.Fatalf("ListClockPresence: %v", err)
	}
	// Member 1 closed normally; member 2's dayOne is auto_closed — BOTH land
	// in the clocked_out bucket (buckets stay disjoint at person grain).
	if page.Summary.ClockedOut != 2 || page.Summary.Working != 0 || page.Summary.NotClockedIn != 0 {
		t.Fatalf("summary=%+v want clocked_out=2 working=0 not_clocked_in=0", page.Summary)
	}
	// Only the auto-closed day is flagged.
	if page.Summary.Flagged != 1 {
		t.Fatalf("summary.flagged=%d want 1 (the auto-closed day)", page.Summary.Flagged)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("presence rows=%d want 2 (both members)", len(page.Rows))
	}
	for _, row := range page.Rows {
		if row.Entry == nil {
			t.Fatalf("both members punched on dayOne; got %+v", row)
		}
		if row.Entry.DeviceModel != "SM-A15" {
			t.Fatalf("presence row must carry the clock-in device snapshot; got %+v", row.Entry)
		}
	}

	// Detail read: both punches for member 1's day, in order.
	detail, err := repo.ClockEntryDetail(ctx, clockTenant, rec.Entry.ClockEntryID)
	if err != nil {
		t.Fatalf("ClockEntryDetail: %v", err)
	}
	if len(detail.Events) != 2 || detail.Events[0].EventType != "clock_in" || detail.Events[1].EventType != "clock_out" {
		t.Fatalf("detail events: want [clock_in clock_out], got %+v", detail.Events)
	}
}
