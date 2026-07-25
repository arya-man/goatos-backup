package domain

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func istDate(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, biztime.DefaultLocation())
}

func TestLatestDueReminderFireAdvanceNotice(t *testing.T) {
	due := istDate(2026, 7, 20, 10, 0) // due July 20
	now := istDate(2026, 7, 13, 9, 30) // exactly D-7 at 09:30, after the 08:00 slot
	fire, ok := LatestDueReminderFire(due, now, DefaultReminderLadder(), func(string) bool { return false })
	if !ok {
		t.Fatalf("expected a due fire at D-7")
	}
	if fire.Type != ReminderTypeAdvanceNotice || fire.OffsetDays != -7 || fire.Slot != "08:00" {
		t.Fatalf("fire = %#v, want advance_notice at D-7 08:00", fire)
	}
}

func TestDefaultReminderLadderUsesDailyVaccinationSlots(t *testing.T) {
	ladder := DefaultReminderLadder()
	if len(ladder) != 3 {
		t.Fatalf("DefaultReminderLadder returned %d steps, want 3", len(ladder))
	}
	wantSlots := [][]string{
		{"08:00"},
		{"08:00", "13:00", "19:30"},
		{"08:00", "13:00", "19:30"},
	}
	for i, want := range wantSlots {
		got := ladder[i].Slots
		if len(got) != len(want) {
			t.Fatalf("step %d slots = %v, want %v", i, got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("step %d slots = %v, want %v", i, got, want)
			}
		}
	}
}

func TestLatestDueReminderFireNoneBeforeWindow(t *testing.T) {
	due := istDate(2026, 7, 20, 10, 0)
	now := istDate(2026, 7, 12, 23, 0) // D-8, before the ladder starts
	_, ok := LatestDueReminderFire(due, now, DefaultReminderLadder(), func(string) bool { return false })
	if ok {
		t.Fatalf("expected no due fire before D-7")
	}
}

func TestLatestDueReminderFirePicksLatestOnCatchUp(t *testing.T) {
	due := istDate(2026, 7, 20, 10, 0)
	// Now is D-1 at 19:45: D-1 08:00, 13:00, and 19:30 are all past due, but nothing has fired
	// yet (fired always false) -- the sweeper should pick the LATEST (19:30), not burst all three.
	now := istDate(2026, 7, 19, 19, 45)
	fire, ok := LatestDueReminderFire(due, now, DefaultReminderLadder(), func(string) bool { return false })
	if !ok {
		t.Fatalf("expected a due fire")
	}
	if fire.Type != ReminderTypeReminder || fire.OffsetDays != -1 || fire.Slot != "19:30" {
		t.Fatalf("fire = %#v, want reminder D-1 19:30 (latest, not the earlier slots)", fire)
	}
}

func TestLatestDueReminderFireSkipsAlreadyFired(t *testing.T) {
	due := istDate(2026, 7, 20, 10, 0)
	now := istDate(2026, 7, 19, 19, 45)
	firedKeys := map[string]bool{
		"2026-07-19:reminder:19:30": true,
	}
	fire, ok := LatestDueReminderFire(due, now, DefaultReminderLadder(), func(k string) bool { return firedKeys[k] })
	if !ok {
		t.Fatalf("expected a due fire (13:00 still pending)")
	}
	if fire.Slot != "13:00" {
		t.Fatalf("fire = %#v, want the still-pending 13:00 slot", fire)
	}
}

func TestLatestDueReminderFireDueToday(t *testing.T) {
	due := istDate(2026, 7, 20, 10, 0)
	now := istDate(2026, 7, 20, 13, 30)
	fire, ok := LatestDueReminderFire(due, now, DefaultReminderLadder(), func(string) bool { return false })
	if !ok {
		t.Fatalf("expected a due_today fire")
	}
	if fire.Type != ReminderTypeDueToday || fire.OffsetDays != 0 || fire.Slot != "13:00" || fire.Priority != ReminderPriorityHigh {
		t.Fatalf("fire = %#v, want due_today D-0 13:00 high", fire)
	}
}

func TestFireDayKeyCollapsesAcrossDifferentDueDates(t *testing.T) {
	// Two obligations with DIFFERENT due dates can both land a "reminder" fire on the SAME
	// calendar day+slot (one at its D-6, another at its D-1) -- vaccination-notification-rules.md
	// §3's collapse rule keys on the fire day, not the obligation's own due date.
	dueA := istDate(2026, 7, 25, 10, 0) // D-6 lands on July 19
	dueB := istDate(2026, 7, 20, 10, 0) // D-1 lands on July 19
	now := istDate(2026, 7, 19, 8, 30)

	fireA, okA := LatestDueReminderFire(dueA, now, DefaultReminderLadder(), func(string) bool { return false })
	fireB, okB := LatestDueReminderFire(dueB, now, DefaultReminderLadder(), func(string) bool { return false })
	if !okA || !okB {
		t.Fatalf("expected both obligations to have a due fire: okA=%v okB=%v", okA, okB)
	}
	if fireA.FireDayKey != fireB.FireDayKey {
		t.Fatalf("fire day keys = %q vs %q, want equal (same collapse batch)", fireA.FireDayKey, fireB.FireDayKey)
	}
}

func TestIsQuietHoursISTWrapsMidnight(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"22:00 is quiet", istDate(2026, 7, 19, 22, 0), true},
		{"06:59 is quiet", istDate(2026, 7, 20, 6, 59), true},
		{"07:00 is allowed", istDate(2026, 7, 20, 7, 0), false},
		{"20:59 is allowed", istDate(2026, 7, 19, 20, 59), false},
		{"noon is allowed", istDate(2026, 7, 19, 12, 0), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := IsQuietHoursIST(tc.now, DefaultQuietHoursStartIST, DefaultQuietHoursEndIST)
			if err != nil {
				t.Fatalf("IsQuietHoursIST: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsQuietHoursIST(%s) = %v, want %v", tc.now.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}
