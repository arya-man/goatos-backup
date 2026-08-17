package domain

import (
	"strings"

	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// FlushingStageName is the flushing nutrition cohort.
//
// Flushing is a NUTRITION cohort: a non-pregnant female is put on extra ration to prepare her for
// breeding (see the shed-tag table in
// context/source-findings/goats-and-parks-source-findings.md).
//
// MAINTAINER DECISION 2026-08-15, SUPERSEDING the rule that a movement must never adopt it: a
// movement into a flushing pen NOW stamps Flushing, and the maintainer accepted the consequence
// explicitly -- that animal goes onto flushing ration and her vaccination schedule is re-keyed as
// a side effect of the move. The reasoning for the reversal is the same one that carried migration
// 000167 for the clinical KID pens: the pen's tag says where the animal is, and a raiser who picks
// the destination-tag side of the toggle is asking for exactly that tag.
//
// The name is kept as a constant because the WRITABLE-VOCABULARY check is now what governs it: the
// stage must be an active animal_stage_lookup row (migration 000171 lists it) or it still resolves
// to keep-current. Nothing special-cases the string any more; it is retained for the seed/migration
// reference and for the tests that pin the reversal.
//
// What did NOT change: a movement still cannot stamp a clinical STATE (bare ICU, Quarantine, sick,
// under_treatment, recovering). Those stay rejected by
// identity/adapters/postgres.resolveDestinationTag, because an animal in one of them has her
// vaccinations POSTPONED, and a placement action must never make that medical call. Flushing is a
// FEEDING decision, which the maintainer chose to let a movement make; a clinical state is not.
const FlushingStageName = "Flushing"

// Farm-worded reasons a destination pen cannot supply a tag.
//
// BACKEND OWNS THE COPY (golden frontend rule). These strings are rendered VERBATIM under the
// greyed-out "use destination tag" option on the operator's raise form, so they are written in farm
// language and carry no internal vocabulary -- no "vocabulary", no "writable", no "resolver". The
// phone must never compose its own reason from the blank stage, because a blank stage does not say
// WHY it is blank and the operator is owed that.
const (
	// StageReasonNoTag: nobody has authored a tag for this pen and nothing lives in it to copy one
	// from.
	StageReasonNoTag = "This destination has no tag set"
	// StageReasonMixed: the pen genuinely holds several cohorts, so there is no single tag to take.
	// Picking the majority would stamp a cohort on thin evidence and would answer differently
	// tomorrow as animals move in and out.
	StageReasonMixed = "This destination holds a mix of tags"
	// StageReasonNotApplicable: the pen carries a tag a movement is not allowed to apply -- today
	// that is a clinical STATE such as ICU or Quarantine, which only the health team may set on an
	// animal. Deliberately worded as who owns the decision rather than as a system limitation.
	StageReasonNotApplicable = "This destination's tag can only be set by the health team"
)

// DestinationStageResolution is the answer the raise form needs: the tag a movement into this pen
// would stamp, and -- when it would stamp none -- the farm-worded reason to show the operator.
//
// The two fields are mutually exclusive by construction: a non-empty Stage always carries a blank
// Reason and vice versa. That invariant is what lets the client use "Reason is set" as the single
// signal to grey out the destination-tag option, instead of re-deriving availability from a blank
// stage it cannot explain.
type DestinationStageResolution struct {
	// Stage is the tag to stamp, in the writable vocabulary's canonical casing. "" means PRESERVE
	// each animal's current stage.
	Stage string
	// Reason is one of the StageReason* constants when Stage is "", else "".
	Reason string
}

// Resolved reports whether a destination tag is actually available for this pen.
func (r DestinationStageResolution) Resolved() bool { return r.Stage != "" }

// ResolveShiftingDestinationStage decides the management_stage a raised shifting will apply.
//
// Maintainer decision (2026-08-03), superseding the three-mode operator chooser: the raiser no
// longer picks. A movement simply adopts the destination shed's cohort. The operator form asks
// nothing, and the raise snapshots one concrete answer.
//
// shedStages is the set of stages the destination shed's live residents currently carry (already
// stripped of clinical states by the destination catalog). writableStages is the tenant's active
// stage vocabulary from animal_stage_lookup — the stages a relocation is actually able to write.
//
// Returns the stage to stamp, or "" meaning PRESERVE each animal's current stage. The empty
// answer is not a failure mode; it is the existing, already-shipped relocation behaviour
// (identity/adapters/postgres.resolveDestinationTag treats an empty target as "keep current"),
// which is why every case this cannot answer cleanly falls back to it instead of guessing.
//
// The three fallbacks, and why each is keep-current rather than a best guess:
//
//   - MIXED shed (more than one resident cohort). A shed genuinely holding K2 and K0 has no single
//     "destination tag" to adopt. Picking the majority would stamp a cohort on as little as 41%
//     evidence (real CBE data: Gandhi 2 holds F2-Female:32, Non-Pregnant:27, Mother:12, Pregnant:8)
//     and would flip as animals move in and out, so the same move would resolve differently
//     tomorrow.
//   - EMPTY shed (no live residents). Nothing to adopt.
//   - UNWRITABLE tag. A resident cohort that is not in the active stage vocabulary cannot be
//     written by the relocation, which validates the target against animal_stage_lookup. Stamping
//     it anyway would pass the raise and then fail at the SECOND GATE — after the operator has
//     already shot the completion video and the park head has already approved. Bare ICU and
//     Quarantine sit in this class and stay there deliberately; the clinical KID pens left it in
//     migration 000167 and Flushing left it in 000171.
//
// Comparison is case-insensitive on trimmed values, and the answer is returned in the writable
// vocabulary's canonical casing so the snapshot matches what the relocation will look up.
func ResolveShiftingDestinationStage(shedStages, writableStages []string) string {
	return ResolveShiftingDestinationStageDetailed(shedStages, writableStages).Stage
}

// ResolveShiftingDestinationStageDetailed is ResolveShiftingDestinationStage plus the farm-worded
// reason behind a keep-current answer, so the raise form can grey the destination-tag option out
// and say WHY instead of offering a choice that silently does nothing.
func ResolveShiftingDestinationStageDetailed(shedStages, writableStages []string) DestinationStageResolution {
	// Collapse to the distinct non-blank cohorts present. A shed reported as ["K2", "k2", " K2 "]
	// is one cohort, not three, so normalizing before counting is what makes "exactly one" mean
	// what it says.
	var resolved string
	for _, stage := range shedStages {
		stage = strings.TrimSpace(stage)
		if stage == "" {
			continue
		}
		if resolved == "" {
			resolved = stage
			continue
		}
		if !strings.EqualFold(resolved, stage) {
			// Mixed destination: two genuinely different cohorts live here.
			return DestinationStageResolution{Reason: StageReasonMixed}
		}
	}
	if resolved == "" {
		// Empty destination shed.
		return DestinationStageResolution{Reason: StageReasonNoTag}
	}
	// Same clinical fail-closed as the authored-tag path: a shed whose residents all carry a bare
	// clinical state must not stamp it, even when the tenant lists it as a writable stage.
	if protocoldomain.IsClinicalManagementStage(resolved) {
		return DestinationStageResolution{Reason: StageReasonNotApplicable}
	}
	for _, writable := range writableStages {
		if strings.EqualFold(strings.TrimSpace(writable), resolved) {
			// Canonical casing from the vocabulary, not the resident row's free text.
			return DestinationStageResolution{Stage: strings.TrimSpace(writable)}
		}
	}
	// Resident cohort the relocation cannot write.
	return DestinationStageResolution{Reason: StageReasonNotApplicable}
}

// ResolveShiftingDestinationPenStage decides the management_stage a raised shifting will apply when
// the destination is an operational LOCATION -- which it always is.
//
// Maintainer decision 2026-08-14, superseding the resident-derived rule below for every movement:
// animals never move into a bare shed, they move into one of its pens ("Godel 1 - Part 2"), so the
// cohort a movement adopts is THAT PEN'S TAG. The previous rule answered a question about the wrong
// place -- it aggregated the whole shed's residents, so a move into Part 2 of a shed whose eight
// pens hold four different cohorts resolved to "mixed" and kept the animal's current stage, even
// though Part 2 itself is unambiguously one cohort.
//
// configuredStage is the tag AUTHORED for the destination location: the pen's own
// (shed_partitions.animal_stage_id, migration 000161), or the shed's profile for a shed with no
// pens. It is preferred over residents on purpose -- it is what somebody decided the pen is for,
// it is stable while animals move in and out, and it is exactly what the Counts Breakdown Stage
// cell shows and edits.
//
// residentStages remains the FALLBACK for a location nobody has configured yet, so this is strictly
// additive: every movement that resolved to a stage before still resolves to one now.
//
// The keep-current fallback that remains is the WRITABLE-VOCABULARY one: a tag the relocation
// cannot write (bare ICU, Quarantine) must not be stamped at raise time only to fail at the SECOND
// GATE after the operator has already shot the video and the park head has already approved.
// Flushing is no longer in that class -- maintainer decision 2026-08-15 lets a movement adopt it,
// and migration 000171 lists it as writable. See FlushingStageName.
func ResolveShiftingDestinationPenStage(configuredStage string, residentStages, writableStages []string) string {
	return ResolveShiftingDestinationPenStageDetailed(configuredStage, residentStages, writableStages).Stage
}

// ResolveShiftingDestinationPenStageDetailed is ResolveShiftingDestinationPenStage plus the
// farm-worded reason behind a keep-current answer.
//
// The reason follows the SAME precedence the stage does. An authored tag the relocation cannot
// write reports StageReasonNotApplicable and does NOT fall through to the residents: the pen has
// been given a tag, the answer to "why can't I use it" is about THAT tag, and reporting the
// residents' reason instead would tell the operator "this pen has no tag set" about a pen that
// visibly has one.
func ResolveShiftingDestinationPenStageDetailed(configuredStage string, residentStages, writableStages []string) DestinationStageResolution {
	if trimmed := strings.TrimSpace(configuredStage); trimmed != "" {
		if resolved := resolveConfiguredStage(trimmed, writableStages); resolved != "" {
			return DestinationStageResolution{Stage: resolved}
		}
		return DestinationStageResolution{Reason: StageReasonNotApplicable}
	}
	// Nothing authored for this location: fall back to what its residents carry, with every
	// fallback the resident rule already had.
	return ResolveShiftingDestinationStageDetailed(residentStages, writableStages)
}

// resolveConfiguredStage validates an authored tag against the writable vocabulary, returning "" when
// the tag is blank, names a clinical STATE, or is one the relocation cannot write.
//
// THE CLINICAL CHECK IS NOT REDUNDANT WITH THE VOCABULARY CHECK, and it is the reason this function
// consults protocol/domain at all. Bare 'ICU' and 'Quarantine' are real animal_stage_lookup rows in
// a tenant that seeds them (see migrations/postgres/stage_age_band_test.go, which seeds exactly
// those two), so the vocabulary loop below would happily resolve them -- and then
// identity/adapters/postgres.resolveDestinationTag rejects them with ErrClinicalDestinationTag at
// the SECOND GATE, after the operator has shot the completion video and the park head has approved.
// Failing here, at raise time, is what turns that into a greyed-out option on the form instead of a
// dead movement at the most expensive possible moment.
func resolveConfiguredStage(configuredStage string, writableStages []string) string {
	configuredStage = strings.TrimSpace(configuredStage)
	if configuredStage == "" || protocoldomain.IsClinicalManagementStage(configuredStage) {
		return ""
	}
	for _, writable := range writableStages {
		if strings.EqualFold(strings.TrimSpace(writable), configuredStage) {
			// Canonical casing from the vocabulary, so the snapshot matches what the relocation
			// will look up at the second gate.
			return strings.TrimSpace(writable)
		}
	}
	return ""
}
