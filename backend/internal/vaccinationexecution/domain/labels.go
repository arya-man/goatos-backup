package domain

import "strings"

// VaccinationDoseDisplayLabel converts backend schedule identifiers into the
// display label carried by the execution contract. Clients render this value;
// they must not interpret internal dose codes such as ET_TT_7W themselves.
func VaccinationDoseDisplayLabel(protocolName, doseCode string) string {
	protocolName = strings.TrimSpace(protocolName)
	doseCode = strings.TrimSpace(doseCode)
	if doseCode == "" {
		return protocolName
	}

	normalized := strings.ToUpper(strings.ReplaceAll(doseCode, " ", "_"))
	switch normalized {
	case "ET_TT_4W":
		return "ET+TT · First dose"
	case "ET_TT_7W", "ET_TT_3W":
		return "ET+TT · Booster"
	}

	antigenCode := normalized
	doseSuffix := ""
	for _, suffix := range []string{"_BOOSTER", "_FIRST"} {
		if strings.HasSuffix(antigenCode, suffix) {
			antigenCode = strings.TrimSuffix(antigenCode, suffix)
			doseSuffix = strings.TrimPrefix(suffix, "_")
			break
		}
	}
	if antigen := vaccinationAntigenLabel(antigenCode); antigen != "" {
		if doseSuffix == "BOOSTER" {
			return antigen + " · Booster"
		}
		return antigen
	}

	if protocolName == "" {
		return strings.ReplaceAll(doseCode, "_", " ")
	}
	return protocolName + " · " + doseCode
}

func vaccinationAntigenLabel(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "HS":
		return "HS"
	case "PPR":
		return "PPR"
	case "FMD":
		return "FMD"
	case "ET_TT":
		return "ET+TT"
	case "SHEEP_POX":
		return "Sheep Pox"
	case "BLUE_TONGUE":
		return "Blue Tongue"
	case "GOAT_POX":
		return "Goat Pox"
	default:
		return ""
	}
}
