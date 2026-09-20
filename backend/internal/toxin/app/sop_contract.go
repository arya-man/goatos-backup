package app

import (
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/domain"
)

// ToxinSOPContract validates the `toxin` section of a procurement.toxin_test version at save time
// (registered on sop/app via WithFormDSLContract). Every problem names its path so the editor can
// point at the step; the first one becomes the 400 message.
//
// This is where the medical safety of an authored procedure lives: a document that renumbers its
// steps, loses its reading step, gates a step on one that comes later, or leaves a tester with no
// instruction is REFUSED at publish, not discovered by a tester standing over a strip.
func ToxinSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if sopCode != domain.SOPCodeToxinTest {
		return
	}
	dsl, err := domain.ParseToxin(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.toxin", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateToxin(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
