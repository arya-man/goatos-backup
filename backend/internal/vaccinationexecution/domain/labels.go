package domain

import "strings"

// VaccinationDoseDisplayLabel converts backend schedule identifiers into the
// display label carried by the execution contract. Clients render this value;
// they must not interpret internal dose codes such as ET_TT_7W themselves.
func VaccinationDoseDisplayLabel(protocolName, doseCode string) string {
	protocolName = strings.TrimSpace(protocolName)
	doseCode = strings.TrimSpace(doseCode)
	if doseCode == "" {
		return protocolDisplayLabel(protocolName)
	}

	normalized := strings.ToUpper(strings.ReplaceAll(doseCode, " ", "_"))
	switch normalized {
	case "ET_TT_4W", "ET_TT_7W", "ET_TT_3W":
		return "ET+TT"
	}

	if label := matrixDoseDisplayLabel(normalized); label != "" {
		return label
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
		_ = doseSuffix
		return antigen
	}

	if protocolName == "" {
		return strings.ReplaceAll(doseCode, "_", " ")
	}
	return protocolDisplayLabel(protocolName) + " · " + strings.ReplaceAll(doseCode, "_", " ")
}

func matrixDoseDisplayLabel(code string) string {
	parts := strings.Split(code, "_")
	if len(parts) == 0 {
		return ""
	}
	for length := len(parts); length > 0; length-- {
		prefix := strings.Join(parts[:length], "_")
		antigen := vaccinationAntigenLabel(prefix)
		if antigen == "" {
			continue
		}
		rest := parts[length:]
		if len(rest) == 0 {
			return antigen
		}
		for _, part := range rest {
			switch {
			case part == "BOOSTER" || part == "REVAC":
				return antigen
			case strings.HasPrefix(part, "W") && len(part) > 1:
				return antigen
			}
		}
		return antigen
	}
	return ""
}

func protocolDisplayLabel(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.EqualFold(trimmed, "Preventive Care Vaccination Matrix") ||
		strings.EqualFold(trimmed, "Preventive Care vaccination matrix") {
		return "Vaccination"
	}
	return trimmed
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
