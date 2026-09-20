package app

import (
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// VendorFormSOPContract validates the `vendor_form` section of a vendor SOP version at save
// time (registered on sop/app via WithFormDSLContract). Every problem names its path so the web
// editor can point at the question; the first one becomes the 400 message. A document the
// register could not run -- a locked id with the wrong kind, a compulsory identity question made
// optional, an only_if pointing forward -- is never saved.
func VendorFormSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	// BOTH vendor documents are checked by the same rules (2026-09-20 split): the register they
	// write is one table, so a supply form that drops an identity question breaks the same row a
	// sales one would.
	if sopCode != domain.SOPCodeVendor && sopCode != domain.SOPCodeProcurementVendor {
		return
	}
	dsl, err := domain.ParseVendorForm(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.vendor_form", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateVendorForm(dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
