// Package app registers the shifting SOP document contract on the SOP library service.
package app

import (
	"github.com/vgoats/goatos/backend/internal/counts/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// ShiftingSOPContract validates the `shifting` section of a `shifting` version at save time
// (registered on sop/app via WithFormDSLContract). The section is REQUIRED on that code: a version
// without it would leave the operator with no completion card, so it cannot be saved. Every
// problem names its path so the web editor can point at the field.
func ShiftingSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if sopCode != domain.SOPCodeShifting {
		return
	}
	dsl, err := domain.ParseShiftingSOP(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.shifting", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateShiftingSOP(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
	// A key the schema does not know is refused at SAVE: the lenient parser drops it, and a
	// misspelt `proofs` would otherwise publish a card with no captures without a word.
	for _, key := range domain.UnknownShiftingSOPKeys(formDSL) {
		report.Valid = false
		problem := "shifting." + key + ": not a field of this document"
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
