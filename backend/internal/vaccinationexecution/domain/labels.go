package domain

import vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"

// VaccinationDoseDisplayLabel converts backend schedule identifiers into the
// display label carried by the execution contract. Clients render this value;
// they must not interpret internal dose codes such as ET_TT_7W themselves.
//
// The canonical implementation lives in vaccination/domain.DoseDisplayLabel so
// every module (execution, calendar, process-integrity) shares one vaccine
// label source. This wrapper preserves the execution-package call site.
func VaccinationDoseDisplayLabel(protocolName, doseCode string) string {
	return vaccinationdomain.DoseDisplayLabel(protocolName, doseCode)
}
