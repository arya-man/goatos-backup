package domain

import "strings"

// FlushingStageName is the one cohort a shifting must never adopt from its destination.
//
// Flushing is a NUTRITION cohort, not a placement cohort: a non-pregnant female is put on extra
// ration to prepare her for breeding (see the shed-tag table in
// context/source-findings/goats-and-parks-source-findings.md). Moving an animal INTO a flushing
// shed is a placement decision; it is not a decision to start flushing that animal, and stamping
// the tag would silently re-key both her feed ration and her vaccination schedule. So the animal
// keeps whatever stage she already had and the flushing decision stays with the workflow that
// owns it.
//
// Named explicitly rather than relied upon implicitly: "Flushing" happens not to exist in
// animal_stage_lookup today, so it would already fall out of the writable-vocabulary check below.
// That is an accident of the current seed, not the rule. Naming it here keeps the behaviour
// correct if someone later adds Flushing to the stage lookup.
const FlushingStageName = "Flushing"

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
//     already shot the completion video and the park head has already approved. Real sheds sit in
//     this class today (ICU-Kid, ICU-Non-Pregnant, Quarantine kids), so resolving to keep-current
//     here is what keeps a legitimate movement from dying at the most expensive possible moment.
//
// Comparison is case-insensitive on trimmed values, and the answer is returned in the writable
// vocabulary's canonical casing so the snapshot matches what the relocation will look up.
func ResolveShiftingDestinationStage(shedStages, writableStages []string) string {
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
			return ""
		}
	}
	if resolved == "" {
		// Empty destination shed.
		return ""
	}
	if strings.EqualFold(resolved, FlushingStageName) {
		return ""
	}
	for _, writable := range writableStages {
		if strings.EqualFold(strings.TrimSpace(writable), resolved) {
			// Canonical casing from the vocabulary, not the resident row's free text.
			return strings.TrimSpace(writable)
		}
	}
	// Resident cohort that the relocation cannot write.
	return ""
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
// The keep-current fallbacks are unchanged and still apply to the pen's own tag: FLUSHING is a
// nutrition cohort owned by its own workflow, and a tag the relocation cannot write (ICU-Kid,
// Quarantine kids) must not be stamped at raise time only to fail at the SECOND GATE after the
// operator has already shot the video and the park head has already approved.
func ResolveShiftingDestinationPenStage(configuredStage string, residentStages, writableStages []string) string {
	if resolved := resolveConfiguredStage(configuredStage, writableStages); resolved != "" {
		return resolved
	}
	// Nothing authored for this location: fall back to what its residents carry, with every
	// fallback the resident rule already had.
	return ResolveShiftingDestinationStage(residentStages, writableStages)
}

// resolveConfiguredStage validates an authored tag against the writable vocabulary, returning "" for
// the same three reasons the resident rule returns "": blank, flushing, or unwritable.
func resolveConfiguredStage(configuredStage string, writableStages []string) string {
	configuredStage = strings.TrimSpace(configuredStage)
	if configuredStage == "" || strings.EqualFold(configuredStage, FlushingStageName) {
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
