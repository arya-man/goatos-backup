// Package app registers the herd-operations CAPTURE CARD contract on the SOP library service:
// the `capture_card` section of a counts.birth / counts.death version (maintainer decision 4,
// 2026-09-16) is validated at save time so a card the phone could not render, or whose
// compulsory slot the server could not judge, is never published. The follow_up section of the
// same versions is validated by sop/app's own follow-up contract; this package owns only the
// capture card, and only for the counts codes.
package app

import (
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// CaptureCardCodes are the SOP codes whose versions may carry a capture card.
var CaptureCardCodes = map[string]bool{
	tasksdomain.SOPCodeBirth: true,
	tasksdomain.SOPCodeDeath: true,
}

// CaptureCardContract validates `form_dsl.capture_card` on the counts codes. An ABSENT section is
// fine (the empty card = today's form); a present one must parse and pass every rule, and every
// problem names its path so the web editor can point at the field. Other codes are untouched.
func CaptureCardContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if !CaptureCardCodes[sopCode] {
		return
	}
	for _, path := range countsdomain.UnknownCaptureCardKeys(formDSL) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + path, Code: "unknown_key", Message: path + ": not a capture card field"})
	}
	card, err := countsdomain.ParseCaptureCard(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.capture_card", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range countsdomain.ValidateCaptureCard(card) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
