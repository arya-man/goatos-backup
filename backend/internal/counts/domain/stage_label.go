package domain

import "github.com/vgoats/goatos/backend/internal/platform/herdstage"

// StageDisplayLabel is the stage label a Counts reader sees, given the raw stored stage_code and
// the name the tenant configured for it in animal_stage_lookup.
//
// THE CODE IS STILL THE VALUE (maintainer decision 2026-09-10). Nothing about the stored stage
// changes: the write path stores the code, the filter sends the code, and the census editor's
// value is the code. This decides one thing only -- the words a reader is shown.
//
// ONLY THE FATTENING FAMILY IS RENAMED, and that narrowness is the decision rather than an
// oversight. The lookup carries a name for every code, so rendering names throughout would also
// turn K0/K1/K2/K3 into Newborn / Milk training / Milk drinking / Weaned kids and M0 into Mother
// newborn -- stages the farm reads, says and writes on its own sheets BY THEIR CODE. "F2" is the
// one that says nothing to a reader, and the maintainer asked for that one.
//
// The WORDS are business-managed: they come from animal_stage_lookup.name, so renaming Fattening
// is a data edit and never a deploy. Only the CHOICE of which codes show their name lives here.
// An F2 stage with no configured name falls back to its code rather than to an invented word.
// The rule itself lives in platform/herdstage beside the membership it depends on, because Sales >
// Farm born renders the same cohort and the two pages must not call it two different things.
func StageDisplayLabel(stageCode, configuredName string) string {
	return herdstage.DisplayLabel(stageCode, configuredName)
}

// IsFatteningStage reports whether a stage code belongs to the fattening family: bare "F2" and its
// sexed variants "F2-Male" / "F2-Female".
//
// Matched on the WHOLE code (case-insensitively, since the column has no CHECK constraint and the
// importer writes the source sheet cell verbatim), never on a "F2" prefix -- a prefix test would
// also claim a future "F2X" or "F2-Trial" that the farm might mean as something else entirely.
// The membership itself lives in platform/herdstage, which is also what the stage FILTER folds on
// -- the label and the filter must agree about which codes are one cohort, and two lists would be
// free to drift.
func IsFatteningStage(stageCode string) bool {
	return herdstage.IsFattening(stageCode)
}
