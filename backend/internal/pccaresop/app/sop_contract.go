// Package app registers the PC Care SOP document contract on the SOP library service.
package app

import (
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// PCCareSOPContract validates the `pc_care` section of a pc_care.tasks version at save time
// (registered on sop/app via WithFormDSLContract). The section is REQUIRED on that code: a
// version without it would leave the operators with no card, so it cannot be saved. Every
// problem names its path so the web editor can point at the field.
func PCCareSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if !domain.IsPCCareSOPCode(sopCode) {
		return
	}
	dsl, err := domain.ParsePCCareSOP(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.pc_care", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidatePCCareSOP(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
	// A key the schema does not know is refused at SAVE: the lenient parser drops it, and a
	// misspelt `proofs` would otherwise publish a card with no captures without a word.
	for _, key := range domain.UnknownPCCareSOPKeys(formDSL) {
		report.Valid = false
		problem := "pc_care." + key + ": not a field of this document"
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
