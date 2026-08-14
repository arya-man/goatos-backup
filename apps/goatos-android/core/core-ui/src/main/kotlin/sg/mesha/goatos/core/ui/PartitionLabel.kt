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
 *  - bare numeric partition → "<shed> <label>": "Castro 1", "Gandhi 2" (space only)
 *  - worded partition → "<shed> - <label>": "Godel 1 - Part 3" (space-dash-space)
 *  - if shed name is null/blank, fall back to partition label or empty string
 *
 * Never produces "Yashoda whole" — the literal string "whole" is treated as non-partitioned.
 *
 * Separator rule (maintainer decision, 2026-08-14):
 *  - Bare numerals use SPACE (matches farm's physical shed names like "Castro 1" painted on buildings).
 *  - Worded labels use " - " for visual boundary, since many shed names end in digits and
 *    "Godel 1 1" (space) could be confused with "Godel 1 - Part 1" (dash).
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

    // Select separator based on partition format: bare numerals use space; worded labels use dash.
    // isAlreadyWordedPartition is still used by partitionDisplayLabel above (the standalone chip,
    // where re-wording "Part 3" would read "Part Part 3"); only this JOIN checks the format.
    val separator = if (isBarNumericPartition(normalizedPartition)) " " else " - "
    return "$normalizedShed$separator$normalizedPartition"
}

/**
 * True when the partition label is a bare ordinal (e.g., "1", "42") with no "Part" prefix or other wording.
 * Used to determine the separator in operationalLocationLabel: bare numerics join with space,
 * worded labels join with " - ".
 */
private fun isBarNumericPartition(label: String): Boolean {
    val trimmed = label.trim()
    if (trimmed.isEmpty()) return false
    return trimmed.all { it.isDigit() }
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
