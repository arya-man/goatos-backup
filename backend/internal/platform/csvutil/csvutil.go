package csvutil

import (
	"encoding/csv"
	"strings"
	"unicode"
)

// SafeCell keeps reviewer exports safe to open in spreadsheets by prefixing
// formula-like values while preserving the raw cell text for review fidelity.
func SafeCell(value string) string {
	if value == "" {
		return ""
	}
	trimmedLeft := strings.TrimLeftFunc(value, unicode.IsSpace)
	if trimmedLeft == "" {
		return value
	}
	switch trimmedLeft[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func WriteSafeRow(writer *csv.Writer, fields []string) error {
	safe := make([]string, len(fields))
	for i, field := range fields {
		safe[i] = SafeCell(field)
	}
	return writer.Write(safe)
}
