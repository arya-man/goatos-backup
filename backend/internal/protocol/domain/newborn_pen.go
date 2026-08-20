package domain

import "strings"

// NewbornPenStage is the cohort tag that marks a pen as the place newborns go (maintainer decision
// 2026-08-20).
//
// It lives here, beside IsClinicalManagementStage, because it is the same KIND of question -- "what
// does this management_stage mean" -- and because it has to be asked from two modules that must not
// import each other: Counts resolves it to decide where a birth may be placed, and Tasks resolves it
// to decide whether a kid's care workflow needs the Record shed fallback step. Two copies of the
// comparison would let the birth form and the workflow disagree about which pens are kid pens.
const NewbornPenStage = "K0"

// IsNewbornPen reports whether a pen's AUTHORED tag marks it as a kid pen.
//
// Case-insensitive and trimmed: the tag is authored data read back from animal_stage_lookup, and a
// tenant that seeded "k0" means the same pen as one that seeded "K0".
//
// It asks about the pen's CONFIGURED tag (shed_partitions.animal_stage_id, or the shed's profile for
// a shed with no pens), never about its residents. A pen kept ready for kids is normally EMPTY, so a
// resident-derived answer would fail to recognise exactly the pens this rule exists to find.
func IsNewbornPen(configuredStage string) bool {
	return strings.EqualFold(strings.TrimSpace(configuredStage), NewbornPenStage)
}
