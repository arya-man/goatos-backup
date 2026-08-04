package kernelstages

// Regression guard for the confirmed maintainer defect: the reminder-cadence ladder rendered
// "Vaccination due next week" / "Vaccination reminder" / "%d vaccination(s) due soon" -- no park, no
// date, nothing a farm worker could act on. These tests assert renderReminderTitle/Body always name
// the park (or the documented "this park" fallback) and the fire date, and never leak an
// engineer-facing word or a raw UUID into the rendered copy (AGENTS.md copy firewall).

import (
	"regexp"
	"strings"
	"testing"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
)

var kcBannedCopyWords = []string{"obligation", "payload", "backend", "api", "route", "module", "null"}
var kcRawUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func assertReminderCopyClean(t *testing.T, label, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	for _, banned := range kcBannedCopyWords {
		if strings.Contains(lower, banned) {
			t.Errorf("%s contains banned engineer word %q: %q", label, banned, text)
		}
	}
	if kcRawUUIDPattern.MatchString(text) {
		t.Errorf("%s leaks a raw UUID: %q", label, text)
	}
}

func TestRenderReminderCopyNamesParkAndDate(t *testing.T) {
	cases := []struct {
		name             string
		notificationType string
		leadership       bool
	}{
		{"advance_notice", "advance_notice", false},
		{"reminder", "reminder", false},
		{"due_today_operator", "due_today", false},
		{"due_today_leadership", "due_today", true},
		{"overdue", "overdue", false},
	}
	const park = "Coimbatore"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fire := calendarports.ReminderCadenceFire{
				ParkID:                     "00000000-0000-4000-8000-000000003001",
				RepresentativeObligationID: "00000000-0000-4000-8000-000000009001",
				NotificationType:           tc.notificationType,
				Slot:                       "20:30",
				FireDayIST:                 "2026-08-02",
				ObligationCount:            3,
			}
			if tc.leadership {
				fire.Slot = "20:30"
				fire.NotificationType = "due_today"
			}

			title := renderReminderTitle(fire, park)
			body := renderReminderBody(fire, park)

			if !strings.Contains(title, park) {
				t.Errorf("title must name the park: %q", title)
			}
			if !strings.Contains(body, park) {
				t.Errorf("body must name the park: %q", body)
			}
			if !strings.Contains(body, "3") {
				t.Errorf("body must carry the collapsed obligation count: %q", body)
			}
			// The pre-fix literal strings/format must never come back.
			switch tc.notificationType {
			case "advance_notice":
				if title == "Vaccination due next week" {
					t.Errorf("regressed to pre-fix abstract title: %q", title)
				}
			case "reminder":
				if title == "Vaccination reminder" {
					t.Errorf("regressed to pre-fix abstract title: %q", title)
				}
			}
			if body == "3 vaccination(s) due soon" || body == "3 vaccination(s) due today" {
				t.Errorf("regressed to pre-fix abstract body with no place/date: %q", body)
			}

			assertReminderCopyClean(t, "reminder title", title)
			assertReminderCopyClean(t, "reminder body", body)
		})
	}
}

// TestRenderReminderCopyFallsBackWithoutParkName proves an unresolved park name degrades to the
// documented neutral fallback ("this park") rather than a blank segment or a raw park id.
func TestRenderReminderCopyFallsBackWithoutParkName(t *testing.T) {
	fire := calendarports.ReminderCadenceFire{
		ParkID:           "00000000-0000-4000-8000-000000003001",
		NotificationType: "due_today",
		FireDayIST:       "2026-08-02",
		ObligationCount:  1,
	}
	title := renderReminderTitle(fire, "")
	body := renderReminderBody(fire, "")
	if !strings.Contains(title, "this park") || !strings.Contains(body, "this park") {
		t.Errorf("missing park name must fall back to the documented neutral wording; title=%q body=%q", title, body)
	}
	assertReminderCopyClean(t, "fallback title", title)
	assertReminderCopyClean(t, "fallback body", body)
}
