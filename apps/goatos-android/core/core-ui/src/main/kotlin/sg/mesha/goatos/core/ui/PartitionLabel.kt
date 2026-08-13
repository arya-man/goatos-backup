package sg.mesha.goatos.core.ui

/**
 * Exact shed display rules, shared by every surface that shows a shed so the same label can never
 * render two different ways.
 *
 * The backend's raw `partition_label` is compatibility metadata, not display copy: fixtures emit
 * `whole`, bare ordinals (`1`, `2`, `3`), and already-worded labels (`Part 3`, `Parts 1-3`).
 * Blindly prefixing "Part " produced "Part whole" and "Part Parts 1-3".
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
 *  - if a shed name exists, return that exact string. `partitionLabel` is compatibility metadata
 *    and must not be appended. The backend must send `Castro 2` or `Mandela 2 Part 1` as the shed
 *    name/display string when that is the real shed.
 *  - if shed name is null/blank, fall back to partition label or empty string
 *
 * Never produces "Yashoda whole" — the literal string "whole" is treated as non-partitioned.
 *
 * Numeric labels are exact numbered shed names. Worded labels keep the explicit separator. Keep
 * identical to oploc.Display() (Go) and lib/operational-location.ts (admin-web).
 */
fun operationalLocationLabel(shedName: String?, partitionLabel: String?): String {
    val normalizedShed = shedName?.trim().takeIf { !it.isNullOrEmpty() } ?: ""
    val normalizedPartition = partitionLabel?.trim().takeIf { !it.isNullOrEmpty() } ?: ""

    // Live identity is the exact shed name. `partitionLabel` is compatibility metadata only and
    // must never be appended to produce labels like "Castro 2 2" or "Gandhi 1 - Part 1".
    if (normalizedShed.isNotEmpty()) {
        return normalizedShed
    }

    if (!normalizedPartition.equals("whole", ignoreCase = true)) {
        return normalizedPartition
    }

    return ""
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
