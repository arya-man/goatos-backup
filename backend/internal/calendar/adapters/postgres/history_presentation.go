package postgres

import (
	"strings"

	"github.com/vgoats/goatos/backend/internal/calendar/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

// presentVaccinationHistoryEvent replaces the storage-shaped title of an accepted-dose history
// event with farm copy. The canonical read composes `<protocol family> <dose_code> completed`
// in SQL ("Preventive Care Vaccination Matrix blue_tongue_first completed"), which leaks the
// protocol family name and a raw config token onto the Calendar -- both banned visible copy
// (ui-vaccine-labels rule). The human label comes from the ONE vaccine display mapper in
// vaccination/domain, the same one the reminder cadence uses, so the Calendar and the
// notifications name a dose the same way. The raw vaccine_name/dose_code fields stay on the
// wire as identifiers; vaccine_labels carries the human label for the drawer chips.
func presentVaccinationHistoryEvent(event *domain.CalendarEvent) {
	if event == nil || event.EventType != domain.EventVaccinationHistory {
		return
	}
	vaccineName, doseCode := "", ""
	if event.VaccineName != nil {
		vaccineName = strings.TrimSpace(*event.VaccineName)
	}
	if event.DoseCode != nil {
		doseCode = strings.TrimSpace(*event.DoseCode)
	}
	label := strings.TrimSpace(vaccinationdomain.DoseQualifiedDisplayLabel(vaccineName, doseCode))
	if label == "" {
		label = "Vaccination"
	}
	event.Title = label + " given"
	if len(event.VaccineLabels) == 0 {
		event.VaccineLabels = []string{label}
	}
}
