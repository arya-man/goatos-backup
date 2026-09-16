// Package app registers the feed SOP document contract on the SOP library service.
package app

import (
	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
)

// FeedSOPContract validates the `feed` section of a feed.direction / feed.packing /
// feed.transport version at save time (registered on sop/app via WithFormDSLContract). The
// section is REQUIRED on those codes: a version without it would leave the crew with no card,
// so it cannot be saved. Every problem names its path so the web editor can point at the field.
func FeedSOPContract(sopCode string, formDSL map[string]any, report *sopdomain.ValidationReport) {
	if !domain.IsFeedSOPCode(sopCode) {
		return
	}
	dsl, err := domain.ParseFeedSOP(formDSL)
	if err != nil {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl.feed", Code: "invalid", Message: err.Error()})
		return
	}
	for _, problem := range domain.ValidateFeedSOP(sopCode, dsl) {
		report.Valid = false
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
	// A key the schema does not know is refused at SAVE: the lenient parser drops it, and a
	// misspelt `proofs` would otherwise publish a card with no captures without a word.
	for _, key := range domain.UnknownFeedSOPKeys(formDSL) {
		report.Valid = false
		problem := "feed." + key + ": not a field of this document"
		report.Errors = append(report.Errors, sopdomain.ValidationIssue{Field: "form_dsl." + problem, Code: "invalid", Message: problem})
	}
}
