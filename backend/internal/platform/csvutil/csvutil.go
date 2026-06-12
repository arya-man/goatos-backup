package csvutil

import (
	"encoding/csv"
	"strings"
)

// SafeCell keeps reviewer exports safe to open in spreadsheets by trimming
// cells and prefixing formula-like values so they are treated as text.
func SafeCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch value[0] {
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
