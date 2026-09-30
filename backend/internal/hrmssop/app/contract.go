// Package app holds the HRMS SOP's save-time contract (maintainer instruction 2026-09-30).
package app

import (
	"github.com/vgoats/goatos/backend/internal/hrmssop/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// SOPContract validates the `violations` section of an `hrms.violations` version at SAVE, so a
// version HR cannot run never reaches publish: an unknown field, a duplicate key, an event that
// cannot open an enquiry, a fine out of range. Every other SOP code is left alone.
func SOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if sopCode != domain.SOPCode {
		return
	}
	section, ok := formDSL[domain.Section]
	if !ok {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{
			Field: "form_dsl." + domain.Section, Code: "missing",
			Message: "This SOP must list the violation types and the enquiries.",
		})
		return
	}
	_, problems := domain.Parse(section)
	for _, problem := range problems {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + domain.Section, Code: "invalid", Message: problem})
	}
}
