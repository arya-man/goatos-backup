package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	"github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestReminderCadenceCALMain02PagedForwardProgress is the CAL-MAIN-02 GUARD.
//
// A single LIMIT-bounded sweep may not reach the due candidates when a large block of already-fired
// candidates sorts BEFORE them by (due_at, event_id). The old code applied LIMIT to a first page that
// was entirely already-fired, so the due candidates at the tail were STARVED forever: every tick
// re-read the same completed first page.
//
// The fix is keyset paging with a cursor persisted across ticks (migration 000204):
//   - each run sweeps ONE page from the persisted cursor, advances the cursor to the last candidate it
//     scanned, and wraps back to the start once the page is the tail (Exhausted);
//   - already-fired candidates are NOT excluded from the scan — the exact per-fire-key dedup
//     (firedSet + LatestDueReminderFire) simply emits no fire for them, so re-scanning is harmless.
//
// This test seeds 220 already-fired candidates that sort BEFORE 50 due candidates, drives the sweep
// repeatedly (loading/advancing/persisting/wrapping the cursor through the DB exactly like
// ReminderCadenceStage), and proves:
//   - the FIRST LIMIT-200 page produces ZERO fires (it is entirely the already-fired block) — so a
//     single run cannot reach the due ones;
//   - repeated cursor-advancing runs EVENTUALLY fire ALL 50 due candidates (no permanent starvation);
//   - the 220 already-fired candidates NEVER re-fire (exact per-fire-key dedup holds).
func TestReminderCadenceCALMain02PagedForwardProgress(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	repo := NewRepository(pool, 15*time.Second)

	protocolID := "86000000-0000-4000-8000-0000000f4101"
	versionID := "86000000-0000-4000-8000-0000000f4102"
	ruleID := "86000000-0000-4000-8000-0000000f4103"

	dayStart := biztime.BusinessDayStart(time.Now())
	evalNow := dayStart.Add(19*time.Hour + 30*time.Minute) // evening: the due_today rungs have fired
	dueToday := dayStart.Add(10 * time.Hour)               // today 10:00 IST -> a due_today fire is pending

	// Seed the shared protocol/version/rule via a throwaway obligation held OUTSIDE the sweep window.
	seedVaccinationObligation(t, ctx, pool, protocolID, versionID, ruleID,
		"86000000-0000-4000-8000-0000000f4001", dayStart.AddDate(0, 0, -30))

	// 270 goats, each in its OWN park so each is a distinct per-park catch-up drive candidate
	// ('catchup:park:<parkID>:due:<day>'). ALL are due TODAY, so they share due_at and the scan orders
	// them by event_id — i.e. by parkID hex, which increases with i. So the first 220 (i<220) sort
	// strictly before the 50 due ones (i>=220). location_code = 'TST-' || right(uuid, 4), so park and
	// shed ids must differ in their LAST 4 hex: parks pack 0x1000+i, sheds pack 0x2000+i.
	const firedCount = 220 // > the 200 page LIMIT, so page 1 is entirely already-fired
	const dueCount = 50    // the tail that must still be reached across paged runs
	const pageLimit = 200

	blockerPark := map[string]bool{}
	duePark := map[string]bool{}

	// The exact fire-day keys a candidate due today produces as of evalNow. Seeding ALL of them for a
	// park marks it fully-fired, so LatestDueReminderFire yields no fire for it (exact per-fire-key
	// dedup) — a genuine "already fired", not a coarse park-level skip.
	pendingKeys := domain.PendingLadderFireKeys(dueToday, evalNow, domain.DefaultReminderLadder())
	if len(pendingKeys) == 0 {
		t.Fatalf("precondition: a due-today candidate must have >=1 pending fire key at evalNow")
	}

	for i := 0; i < firedCount+dueCount; i++ {
		goatID := "86000000-0000-4000-8000-0000000a" + zeroPadHex(i, 4)
		parkID := "86000000-0000-4000-8000-00000000" + zeroPadHex(0x1000+i, 4)
		shedID := "86000000-0000-4000-8000-00000000" + zeroPadHex(0x2000+i, 4)
		oblID := "86000000-0000-4000-8000-0000000d" + zeroPadHex(i, 4)

		seedCalendarGoat(t, ctx, pool, goatID)
		seedVaccinationObligationForGoatAndRule(t, ctx, pool, versionID, ruleID, oblID,
			goatID, shedID, parkID, dueToday)

		if i < firedCount {
			blockerPark[parkID] = true
			// Mark EVERY pending fire key for this park as already fired, using the sweeper's exact
			// key format (park_id + ':' + fireDayKey). Only fire_key is matched by the sweep; the other
			// NOT NULL columns carry valid placeholders (notification_type must satisfy the CHECK).
			for _, k := range pendingKeys {
				fireKey := parkID + ":" + k
				if _, err := pool.Exec(ctx,
					`INSERT INTO vaccination_reminder_cadence_fires
					   (tenant_id, fire_key, park_id, fire_day, notification_type, slot, representative_calendar_event_id)
					 VALUES ($1::uuid, $2, $3::uuid, $4::date, 'due_today', 'seed', $5)
					 ON CONFLICT (tenant_id, fire_key) DO NOTHING`,
					testTenantID, fireKey, parkID, dueToday, "evt:"+fireKey); err != nil {
					t.Fatalf("mark fired key %s: %v", fireKey, err)
				}
			}
		} else {
			duePark[parkID] = true
			// No fire markers: this park still has a pending due_today fire.
		}
	}

	// Drive the sweep repeatedly, mirroring ReminderCadenceStage: load the cursor from the DB, sweep one
	// page, advance to the last scanned candidate, wrap to the start on exhaustion, persist. NOTE: this
	// test never queues/claims fires, so a re-scan after a wrap would re-fire the 50 due parks; we stop
	// after ONE full cycle (the first Exhausted page) so each candidate is scanned exactly once.
	firedDueParks := map[string]bool{}
	firedBlockerCount := 0
	pages := 0
	firstPageFireCount := -1

	const maxRuns = 20 // safety bound; one cycle over 270 candidates at page 200 needs 2 pages
	for run := 0; run < maxRuns; run++ {
		cursorDueAt, cursorEventID, err := repo.LoadReminderCadenceCursor(ctx, testTenantID)
		if err != nil {
			t.Fatalf("run %d: load cursor: %v", run, err)
		}

		fires, sweepCursor, err := repo.SweepReminderCadencePage(ctx, ports.ReminderCadenceQuery{
			TenantID:      testTenantID,
			Now:           evalNow,
			Limit:         pageLimit,
			CursorDueAt:   cursorDueAt,
			CursorEventID: cursorEventID,
		})
		if err != nil {
			t.Fatalf("run %d: sweep page: %v", run, err)
		}
		pages++
		if firstPageFireCount == -1 {
			firstPageFireCount = len(fires)
		}
		for _, f := range fires {
			switch {
			case duePark[f.ParkID]:
				firedDueParks[f.ParkID] = true
			case blockerPark[f.ParkID]:
				firedBlockerCount++
			default:
				t.Fatalf("run %d: fire for an unexpected park %s", run, f.ParkID)
			}
		}

		// Advance/wrap the cursor exactly as the stage does, and persist it through the DB.
		nextDueAt, nextEventID := cursorDueAt, cursorEventID
		if sweepCursor.EventID != "" {
			nextDueAt, nextEventID = sweepCursor.DueAt, sweepCursor.EventID
		}
		if sweepCursor.Exhausted {
			nextDueAt, nextEventID = time.Time{}, ""
		}
		if err := repo.SaveReminderCadenceCursor(ctx, testTenantID, nextDueAt, nextEventID, time.Now()); err != nil {
			t.Fatalf("run %d: save cursor: %v", run, err)
		}

		if sweepCursor.Exhausted {
			// Completed one full cycle over the candidate set.
			break
		}
		if sweepCursor.EventID == "" {
			t.Fatalf("run %d: non-exhausted page scanned no candidate (cursor stuck) — would starve", run)
		}
	}

	// The first LIMIT-200 page must be entirely the already-fired block -> zero fires. This is the
	// STARVATION condition: a single sweep cannot reach the 50 due candidates at the tail.
	if firstPageFireCount != 0 {
		t.Fatalf("CAL-MAIN-02: first page produced %d fires, want 0 (page 1 must be the already-fired block that starves the tail)",
			firstPageFireCount)
	}
	// Paging was genuinely required to reach the due candidates.
	if pages < 2 {
		t.Fatalf("CAL-MAIN-02: reached the due candidates in %d page(s); the seed must force >=2 pages to prove paging", pages)
	}
	// Every due candidate must fire across the paged runs — no permanent starvation.
	if len(firedDueParks) != dueCount {
		t.Fatalf("CAL-MAIN-02: %d/%d due candidates fired across %d pages — the tail STARVED",
			len(firedDueParks), dueCount, pages)
	}
	// Already-fired candidates must never re-fire (exact per-fire-key dedup).
	if firedBlockerCount != 0 {
		t.Fatalf("CAL-MAIN-02: %d already-fired candidates re-fired — per-fire-key dedup broken", firedBlockerCount)
	}

	t.Logf("CAL-MAIN-02 GUARD: PASS — first page fired 0 (220 already-fired block), %d due candidates all fired across %d paged runs, 0 re-fires",
		len(firedDueParks), pages)
}

// zeroPadHex formats an int as a zero-padded hex string with the given width.
// E.g., zeroPadHex(15, 2) = "0f", zeroPadHex(255, 4) = "00ff".
func zeroPadHex(i, width int) string {
	s := ""
	for i > 0 {
		s = "0123456789abcdef"[i%16:i%16+1] + s
		i /= 16
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
