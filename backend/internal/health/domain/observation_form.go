package domain

import "github.com/vgoats/goatos/backend/internal/health/diagnosis"

// THE FORM THE PHONE DRAWS, for ONE animal.
//
// Maintainer, 2026-09-23: nothing on the observation form should be hard-coded, and a question
// added on Health Config for a type must reach the phone on a refresh.
//
// Until now the phone had no way to LEARN the questions: the app could post answers and read them
// back, and the form itself was 1031 lines of Compose beside a field-per-question DTO -- a third
// copy of a form that also exists as a Go struct and as the authored register. Migration 000388
// made the RULES authorable and left the FORM behind.
//
// IT IS SERVED PER ANIMAL, not per type, and that is the load-bearing choice. The phone then never
// has to know the routing rules -- which stage reaches which type is the farm's authored decision
// and belongs on one side of the wire. It also moves the REFUSAL to the front: an animal whose
// stage no type covers is turned away before the operator fills anything in, rather than after
// they have walked the whole animal and pressed submit.
type ObservationForm struct {
	// GoatID and the animal's own identifiers, so the phone can show who it is holding.
	GoatID    string `json:"goat_id"`
	DisplayID string `json:"display_id,omitempty"`
	Tag       string `json:"tag,omitempty"`

	// TypeKey and TypeLabel say which rulebook this animal is judged against, because an operator
	// who sees a different form for two animals in the same pen is owed the reason.
	TypeKey   string `json:"type_key"`
	TypeLabel string `json:"type_label"`

	// RegisterVersion pins the version these questions came from. The answers are submitted
	// against it, so a publish between fetching the form and submitting it is detectable rather
	// than silently mixing one version's answers into another's rules.
	RegisterVersion string `json:"register_version"`

	// Sex and Stage are THE ANIMAL'S OWN, normalised exactly as the engine reads them (F/M, and
	// the herd register's stage). They ride the form because the phone cannot evaluate
	// `only_if_sex` / `only_if_stage` without them -- and it must evaluate them, or it shows the
	// operator a form missing the questions the server will then refuse the submit for.
	//
	// They are FACTS, never fields: the operator is not asked and cannot change them. A manager
	// who could type an animal's sex could change which half of the rulebook judges it.
	Sex   string `json:"sex,omitempty"`
	Stage string `json:"stage,omitempty"`

	// Pages are what the operator walks, in authored order.
	Pages []diagnosis.Page `json:"pages"`
}
