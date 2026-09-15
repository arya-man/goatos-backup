// Package app registers the weighing SOP document contract on the SOP library service.
package app

import (
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// WeighingSOPContract validates the `weighing` section of a weighing.session version at save
// time (registered on sop/app via WithFormDSLContract). The section is REQUIRED: a version
// without it would leave the planner with no rules, so it cannot be saved. Every problem names
// its path so the web editor can point at the field; the first one becomes the 400 message.
func WeighingSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if sopCode != domain.SOPCodeWeighingSession {
		return
	}
	dsl, err := domain.ParseWeighingSOP(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.weighing", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateWeighingSOP(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
