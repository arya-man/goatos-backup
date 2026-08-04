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
 * True when the raw label already reads as a partition phrase, so re-wording it would double the
 * noun. Matches `Part`/`Parts` as a whole leading word only — `Part 3` and `Parts 1-3` are worded,
 * a bare `3` is not.
 */
private fun isAlreadyWordedPartition(partition: String): Boolean =
    ALREADY_WORDED_PARTITION.containsMatchIn(partition)

private const val WHOLE_SHED_PARTITION = "whole"

private val ALREADY_WORDED_PARTITION = Regex("^parts?\\b", RegexOption.IGNORE_CASE)
