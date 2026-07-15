package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestReminderCadenceP1Finding3TargetTypeResolves is the FINDING 3 GUARD.
//
// The notification must store a (target_type, target_id) pair that resolves to the SAME real entity the
// fire is about -- never a batch/park id mislabeled as an "obligation". For each drive shape the fire's
// representative id is a DIFFERENT kind of entity, so the stored type must track the real id type:
//
//	SINGLE   (one batch alone in a park-day)   -> type 'batch',      id resolves in obligation_batches
//	CATCH-UP (unbatched obligation drive)      -> type 'catchup',    id resolves in locations (the park)
//	GROUPED  (multi-batch park-day drive)      -> type 'park_drive', id resolves in locations (the park)
func TestReminderCadenceP1Finding3TargetTypeResolves(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 15*time.Second)

	protocolID := "86000000-0000-4000-8000-0000000f3101"
	versionID := "86000000-0000-4000-8000-0000000f3102"
	ruleID := "86000000-0000-4000-8000-0000000f3103"

	// Distinct parks/sheds so each drive is its own park-day group (no cross-collapse).
	parkC := "86000000-0000-4000-8000-0000000f3701"
	shedC1 := "86000000-0000-4000-8000-0000000f3711"
	shedC2 := "86000000-0000-4000-8000-0000000f3712"

	batchA := "86000000-0000-4000-8000-0000000f3201"
	batchC1 := "86000000-0000-4000-8000-0000000f3221"
	batchC2 := "86000000-0000-4000-8000-0000000f3222"

	oblBatchA := "86000000-0000-4000-8000-0000000f3301"
	oblCatchupB := "86000000-0000-4000-8000-0000000f3302"
	oblGroupedC1 := "86000000-0000-4000-8000-0000000f3303"
	oblGroupedC2 := "86000000-0000-4000-8000-0000000f3304"

	goatCatchupB := "86000000-0000-4000-8000-0000000f3401"
	goatC1 := "86000000-0000-4000-8000-0000000f3402"
	goatC2 := "86000000-0000-4000-8000-0000000f3403"

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute) // evening: due_today rung has fired
	dueToday := dayStart.Add(10 * time.Hour)

	// Seed the shared protocol/version/rule via a throwaway tenant-scoped obligation held OUTSIDE the
	// sweep window (30 days ago) so it never appears as a candidate (park_id IS NULL + out of window).
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID,
		"86000000-0000-4000-8000-0000000f3001", dayStart.AddDate(0, 0, -30))

	// Goats must exist before an obligation can target them (target_id FK-validated per tenant).
	goatBatchA := "86000000-0000-4000-8000-0000000f3400"
	for _, g := range []string{goatBatchA, goatCatchupB, goatC1, goatC2} {
		seedCalendarGoat(t, ctx, pool, g)
	}

	// SINGLE (batch): parkA/shedA, one obligation attached to one batch -> park-drive drive_count=1 backed
	// by that batch -> source_target_type 'batch', source_target_id = the batch id.
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, ruleID, oblBatchA,
		goatBatchA, testShedA, testParkA, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchA, versionID, testParkA, testShedA, dueToday, oblBatchA)

	// CATCH-UP: parkB/shedB, an UNBATCHED shed-scoped obligation -> catch-up drive -> park-drive
	// drive_count=1 backed by catchup -> source_target_type 'catchup', source_target_id = the park id.
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, ruleID, oblCatchupB,
		goatCatchupB, testShedB, testParkB, dueToday)

	// GROUPED: parkC with TWO batches in two sheds, same day -> park-drive drive_count=2 ->
	// source_target_type 'park_drive', source_target_id = the park id.
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, ruleID, oblGroupedC1,
		goatC1, shedC1, parkC, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchC1, versionID, parkC, shedC1, dueToday, oblGroupedC1)
	seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, ruleID, oblGroupedC2,
		goatC2, shedC2, parkC, dueToday)
	seedVaccinationBatchForShed(t, ctx, pool, batchC2, versionID, parkC, shedC2, dueToday, oblGroupedC2)

	// ---- Sweep and queue every fire with a hand-built recipient (no workforce seeding needed). --------
	fires, err := repo.SweepReminderCadence(ctx, ports.ReminderCadenceQuery{
		TenantID: testTenantID,
		Now:      evalNow,
		Limit:    200,
	})
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}
	if len(fires) < 3 {
		t.Fatalf("sweep returned %d fires, want >=3 (batch, catchup, grouped park-drives)", len(fires))
	}

	inputs := make([]ports.ReminderCadenceFireInput, 0, len(fires))
	for _, f := range fires {
		inputs = append(inputs, ports.ReminderCadenceFireInput{
			Fire:  f,
			Title: "Finding-3 reminder",
			Body:  "target-type resolution",
			Context: map[string]string{"test": "finding3"},
			Recipients: []ports.NotificationRecipient{{
				MemberID:  "86000000-0000-4000-8000-0000000f3500",
				DeviceID:  "finding3-device-1",
				FCMToken:  "finding3-fcm-1",
				RoleLabel: "operator",
			}},
		})
	}
	if _, err := repo.QueueReminderCadenceBatch(ctx, ports.QueueReminderCadenceBatch{
		TenantID: testTenantID,
		Channel:  "push_fcm",
		TraceID:  "finding3",
		Fires:    inputs,
	}); err != nil {
		t.Fatalf("queue failed: %v", err)
	}

	// Map each seeded park to its fire's representative calendar event id (the parkdrive event id).
	eventIDByPark := map[string]string{}
	for _, f := range fires {
		eventIDByPark[f.ParkID] = f.RepresentativeCalendarEventID
	}

	// SINGLE (batch): type must be 'batch' and the id must resolve to a real obligation_batches row.
	assertNotificationTargetResolves(t, ctx, pool, eventIDByPark[testParkA], "batch",
		"SELECT count(*) FROM obligation_batches WHERE tenant_id = $1::uuid AND batch_id = $2::uuid")

	// CATCH-UP: type must be 'catchup' and the id must resolve to the park's locations row.
	assertNotificationTargetResolves(t, ctx, pool, eventIDByPark[testParkB], "catchup",
		"SELECT count(*) FROM locations WHERE tenant_id = $1::uuid AND location_id = $2::uuid")

	// GROUPED: type must be 'park_drive' and the id must resolve to the park's locations row.
	assertNotificationTargetResolves(t, ctx, pool, eventIDByPark[parkC], "park_drive",
		"SELECT count(*) FROM locations WHERE tenant_id = $1::uuid AND location_id = $2::uuid")

	t.Logf("FINDING 3 GUARD: PASS — batch->'batch', catchup->'catchup', grouped->'park_drive', each id resolves to its real entity")
}

// assertNotificationTargetResolves reads the (target_type, target_id) the reminder cadence stored for a
// calendar event and asserts (a) the type equals wantType (NOT the old hardcoded 'obligation'), and
// (b) target_id resolves to exactly one real row via resolveSQL ($1=tenant, $2=target_id).
func assertNotificationTargetResolves(t *testing.T, ctx context.Context, pool *pgxpool.Pool, calendarEventID, wantType, resolveSQL string) {
	t.Helper()
	if calendarEventID == "" {
		t.Fatalf("no fire produced for the %q scenario (calendar_event_id empty)", wantType)
	}
	var gotType, targetID string
	err := pool.QueryRow(ctx, `
SELECT target_type, target_id::text
FROM notification_requests
WHERE tenant_id = $1::uuid AND calendar_event_id = $2
LIMIT 1`, testTenantID, calendarEventID).Scan(&gotType, &targetID)
	if err != nil {
		t.Fatalf("query notification for %s: %v", calendarEventID, err)
	}
	if gotType != wantType {
		t.Fatalf("FINDING 3: event %s stored target_type=%q, want %q (wrong record type)", calendarEventID, gotType, wantType)
	}
	if targetID == "" {
		t.Fatalf("FINDING 3: event %s (type %q) stored an EMPTY target_id — cannot resolve to a real entity", calendarEventID, wantType)
	}
	var n int
	if err := pool.QueryRow(ctx, resolveSQL, testTenantID, targetID).Scan(&n); err != nil {
		t.Fatalf("resolve target_id %s (type %q): %v", targetID, wantType, err)
	}
	if n != 1 {
		t.Fatalf("FINDING 3: target (%q, %s) resolves to %d rows, want exactly 1 (nonexistent/ambiguous entity)", wantType, targetID, n)
	}
	t.Logf("FINDING 3: event %s -> (type=%q, id=%s) resolves to 1 real entity — OK", calendarEventID, wantType, targetID)
}
