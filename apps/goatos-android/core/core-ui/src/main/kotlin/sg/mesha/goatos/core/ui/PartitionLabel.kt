package sg.mesha.goatos.core.ui

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
 *  - any real partition → "<shed> - <label>": "Castro - 2", "Godel 1 - Part 3"
 *  - if shed name is null/blank, fall back to partition label or empty string
 *
 * Never produces "Yashoda whole" — the literal string "whole" is treated as non-partitioned.
 *
 * The separator is " - " for EVERY partition (maintainer decision, 2026-08-06). The old space
 * form was unreadable wherever a shed name itself ends in a digit — "Godel 1" + "1" rendered
 * "Godel 1 1", and "Godel 1" + "10" rendered "Godel 1 10" — which was 98 of 130 real destination
 * options (75%) on STG. Worded labels already used the dash, so this collapses two formats to one.
 * Keep identical to oploc.Display() (Go) and lib/operational-location.ts (admin-web).
 */
fun operationalLocationLabel(shedName: String?, partitionLabel: String?): String {
    val normalizedShed = shedName?.trim().takeIf { !it.isNullOrEmpty() } ?: ""
    val normalizedPartition = partitionLabel?.trim().takeIf { !it.isNullOrEmpty() } ?: ""

    // No partition or literal "whole" means non-partitioned
    if (normalizedPartition.isEmpty() || normalizedPartition.equals("whole", ignoreCase = true)) {
        return normalizedShed
    }

    // No shed name: return partition label alone (fallback)
    if (normalizedShed.isEmpty()) {
        return normalizedPartition
    }

    // One separator for every partition, worded or bare. isAlreadyWordedPartition is still used
    // by partitionDisplayLabel above (the standalone chip, where re-wording "Part 3" would read
    // "Part Part 3"); only this JOIN stopped branching.
    return "$normalizedShed - $normalizedPartition"
}

/**
 * True when the raw label already reads as a partition phrase, so re-wording it would double the
 * noun. Matches `Part`/`Parts` as a whole leading word only — `Part 3` and `Parts 1-3` are worded,
 * a bare `3` is not.
 */
private fun isAlreadyWordedPartition(partition: String): Boolean =
    ALREADY_WORDED_PARTITION.containsMatchIn(partition)

private const val WHOLE_SHED_PARTITION = "whole"

private val ALREADY_WORDED_PARTITION = Regex("^parts?\\b", RegexOption.IGNORE_CASE)
