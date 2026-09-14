package app

import (
	"github.com/vgoats/goatos/backend/internal/animalpurchase/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// InspectionSOPContract validates the `inspection` section of a procurement.animal_purchase
// version at save time (registered on sop/app via WithFormDSLContract). Every problem names its
// path so the web editor can point at the field; the first one becomes the 400 message.
func InspectionSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if sopCode != domain.SOPCodeAnimalPurchase {
		return
	}
	dsl, err := domain.ParseInspection(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.inspection", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateInspection(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
