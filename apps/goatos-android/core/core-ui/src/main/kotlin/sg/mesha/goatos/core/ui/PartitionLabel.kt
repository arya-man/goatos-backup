package sg.mesha.goatos.core.ui

import sg.mesha.goatos.core.common.WHOLE_SHED_PARTITION
import sg.mesha.goatos.core.common.composeOperationalLocationLabel
import sg.mesha.goatos.core.common.composeOperationalLocationLabelFromComposedName

/**
 * Shed-partition display rules, shared by every surface that shows a drive's shed/partition so the
 * same label can never render two different ways.
 *
 * A drive covers either a WHOLE shed or one partition of it. The backend's raw `partition_label` is
 * not display copy: fixtures emit `whole`, bare ordinals (`1`, `2`, `3`), and already-worded labels
 * (`Part 3`, `Parts 1-3`). Blindly prefixing "Part " produced "Part whole" and "Part Parts 1-3".
 *
 * Rules:
 *  - blank or `whole` -> null. The drive covers the shed; the shed name alone is the label and no
 *    partition chip is shown.
 *  - already worded (`Part 3`, `Parts 1-3`, `भाग 2`) -> used verbatim.
 *  - anything else (`1`, `A`) -> passed to [format] so the caller supplies the localized wording.
 */
fun partitionDisplayLabel(partition: String, format: (String) -> String): String? {
    val trimmed = partition.trim()
    if (trimmed.isEmpty() || trimmed.equals(WHOLE_SHED_PARTITION, ignoreCase = true)) return null
    return if (isAlreadyWordedPartition(trimmed)) trimmed else format(trimmed)
}

/**
 * Format operational location as shed name with optional partition label.
 *
 * Rules:
 *  - null/blank shed name or null/blank/whole partition → bare shed name only, e.g. "Yashoda"
 *  - bare numeric partition → "<shed> <label>": "Castro 1", "Gandhi 2" (space only)
 *  - worded partition → "<shed> - <label>": "Godel 1 - Part 3" (space-dash-space)
 *  - if shed name is null/blank, fall back to partition label or empty string
 *
 * Never produces "Yashoda whole" — the literal string "whole" is treated as non-partitioned.
 *
 * Separator rule (maintainer decision, 2026-08-16, clarifying farm's real-world naming):
 *  - Bare numerals use SPACE because the farm's physical sheds ARE NAMED "Castro 1", "Gandhi 2", etc. —
 *    that is the real name painted on the building, not a display formatting choice.
 *  - Worded labels use " - " for visual boundary (since 75% of live shed names end in digits and
 *    "Godel 1 1" space form would be ambiguous without the dash convention distinguishing it).
 * Keep identical to oploc.Display() (Go) and lib/operational-location.ts (admin-web).
 */
fun operationalLocationLabel(shedName: String?, partitionLabel: String?): String =
    composeOperationalLocationLabel(shedName, partitionLabel)


/**
 * True when the raw label already reads as a partition phrase, so re-wording it would double the
 * noun. Matches `Part`/`Parts` as a whole leading word only — `Part 3` and `Parts 1-3` are worded,
 * a bare `3` is not.
 */
private fun isAlreadyWordedPartition(partition: String): Boolean =
    ALREADY_WORDED_PARTITION.containsMatchIn(partition)


private val ALREADY_WORDED_PARTITION = Regex("^parts?\\b", RegexOption.IGNORE_CASE)

/**
 * Formats an operational location for a caller whose SHED-NAME source may ALREADY be a composed
 * operational display — the Kotlin twin of Go's `oploc.ResolveComposedName`, and the only safe
 * entry point for a weighing bucket, whose `display_name` already carries the pen while a sibling
 * field separately says `partition_label`.
 *
 * [operationalLocationLabel] appends unconditionally, which is right when it is handed a physical
 * shed name. Handed "Mandela 1 - Part 1" and "Part 1" it renders "Mandela 1 - Part 1 - Part 1",
 * which is what an operator read on the weighing schedule on 2026-09-21.
 *
 * Prefer the backend's own `operational_location_display` whenever the payload carries it; this is
 * for the surfaces that hold only the name/label pair. It strips the partition in the exact two
 * forms [operationalLocationLabel] appends — " - Part 3" and " 1" — so composing twice is a no-op.
 * The bare-numeric arm is not padding: a park whose canonical shed carries catalog partitions
 * sends "Castro 1" beside "1", and the guard this replaces checked only the worded form.
 */
fun operationalLocationLabelFromComposedName(shedName: String?, partitionLabel: String?): String =
    composeOperationalLocationLabelFromComposedName(shedName, partitionLabel)
