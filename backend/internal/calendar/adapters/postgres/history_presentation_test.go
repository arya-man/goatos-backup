package postgres

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
)

// historyRowScanner feeds scanCalendarEvent the row shape vaccination_history_events produces:
// the SQL title is the storage-shaped `<protocol family> <dose_code> completed`.
type historyRowScanner struct {
	eventType, title, vaccineName, doseCode string
}

func (s historyRowScanner) Scan(dest ...any) error {
	*(dest[1].(*string)) = s.eventType
	*(dest[3].(*string)) = s.title
	*(dest[23].(*pgtype.Text)) = pgtype.Text{String: s.vaccineName, Valid: true}
	*(dest[24].(*pgtype.Text)) = pgtype.Text{String: s.doseCode, Valid: true}
	return nil
}

// TestVaccinationHistoryTitleNeverShowsProtocolFamilyOrRawDoseCode reproduces the Calendar
// History defect (/calendar?status=completed rendered "Preventive Care Vaccination Matrix
// blue_tongue_first completed") through the production scan path.
func TestVaccinationHistoryTitleNeverShowsProtocolFamilyOrRawDoseCode(t *testing.T) {
	cases := []struct{ dose, want string }{
		{"blue_tongue_first", "Blue Tongue · Dose 1 given"},
		{"et_tt_adult_w2", "ET+TT · Dose 2 given"},
		{"ppr_booster", "PPR · Booster given"},
	}
	for _, tc := range cases {
		event, err := scanCalendarEvent(historyRowScanner{
			eventType:   domain.EventVaccinationHistory,
			title:       "Preventive Care Vaccination Matrix " + tc.dose + " completed",
			vaccineName: "Preventive Care Vaccination Matrix",
			doseCode:    tc.dose,
		})
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		if event.Title != tc.want {
			t.Errorf("dose %s: title = %q, want %q", tc.dose, event.Title, tc.want)
		}
		for _, banned := range []string{"Preventive Care Vaccination Matrix", tc.dose, "_"} {
			if strings.Contains(event.Title, banned) {
				t.Errorf("dose %s: title %q leaks %q", tc.dose, event.Title, banned)
			}
		}
		if len(event.VaccineLabels) != 1 || strings.Contains(event.VaccineLabels[0], "_") {
			t.Errorf("dose %s: vaccine_labels = %v, want one human label", tc.dose, event.VaccineLabels)
		}
	}
}

func TestNonHistoryEventTitleIsLeftAlone(t *testing.T) {
	event, err := scanCalendarEvent(historyRowScanner{eventType: "vaccination_drive", title: "Drive · CPT"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if event.Title != "Drive · CPT" {
		t.Fatalf("title rewritten for a non-history event: %q", event.Title)
	}
}
