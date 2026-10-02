package domain

import (
	"regexp"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// isoDateSegment matches a "·"-separated segment that is exactly an ISO business date.
var isoDateSegment = regexp.MustCompile(`(^|·\s*)(\d{4}-\d{2}-\d{2})(\s*(?:·|$))`)

// DisplaySubjectLabel renders a stored subject label for a reader. Producers compose the label at
// enqueue time and every current producer writes its dates as DD/MM/YYYY
// (biztime.FarmDateFromBusinessDate); items enqueued before that change still carry an ISO
// "· 2026-09-16 ·" segment in the stored row (pr294 C7: "6th Colostrum · Kid G-005346 ·
// 2026-09-16"). The stored row is history and is left as written; the read renders the farm date.
// Only a whole segment that is a date is rewritten -- an ISO date inside another word is not.
func DisplaySubjectLabel(label string) string {
	if label == "" {
		return label
	}
	return isoDateSegment.ReplaceAllStringFunc(label, func(match string) string {
		parts := isoDateSegment.FindStringSubmatch(match)
		return parts[1] + biztime.FarmDateFromBusinessDate(parts[2]) + parts[3]
	})
}

// DisplaySubjectLabelPtr is DisplaySubjectLabel for an optional label.
func DisplaySubjectLabelPtr(label *string) *string {
	if label == nil {
		return nil
	}
	out := DisplaySubjectLabel(*label)
	return &out
}
