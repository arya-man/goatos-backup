package sg.mesha.goatos.core.common

/**
 * The operational-location display rule, in the one module every layer can reach.
 *
 * It lives in core-common rather than core-ui because the DATA layer needs it too: a weighing
 * repository mapping a payload into a cached row is composing the same label a screen renders, and
 * when the two layers each kept their own copy they drifted — the data layer's guarded against a
 * doubled pen and the UI layer's did not, which is how "Mandela 1 - Part 1 - Part 1" reached an
 * operator's weighing schedule on 2026-09-21. core-ui re-exports these under their historical
 * names so existing call sites are unchanged.
 *
 * Keep identical to Go's `oploc` and admin-web's `lib/operational-location.ts`; the same pen must
 * never read two ways across surfaces.
 */

/** The stored marker for "not partitioned". A matching key only — it must never reach a screen. */
const val WHOLE_SHED_PARTITION: String = "whole"

/**
 * Composes a PHYSICAL shed name and its partition into the user-facing location.
 *
 *  - no partition (null, blank, or `whole`) -> bare shed name: "Yashoda"
 *  - bare numeral -> "<shed> <label>": "Castro 1" (the name painted on the building)
 *  - worded label -> "<shed> - <label>": "Godel 1 - Part 3" (visual boundary)
 *
 * If the shed name already ends with the partition ("Godel 1" + "1"), it is returned unchanged
 * rather than doubled, matching admin-web. If the name you hold may carry the pen in another form,
 * use [composeOperationalLocationLabelFromComposedName].
 */
fun composeOperationalLocationLabel(shedName: String?, partitionLabel: String?): String {
    val shed = shedName?.trim().orEmpty()
    val partition = partitionLabel?.trim().orEmpty()
    if (partition.isEmpty() || partition.equals(WHOLE_SHED_PARTITION, ignoreCase = true)) return shed
    if (shed.isEmpty()) return partition
    if (alreadyEndsWithPartition(shed, partition)) return shed
    val separator = if (partition.all { it.isDigit() }) " " else " - "
    return "$shed$separator$partition"
}

/**
 * True when the shed name already ends with the partition it would be given, so appending would
 * double the pen ("Godel 1" + "1", "Mandela 1 - Part 1" + "Part 1"). Twin of admin-web's
 * `alreadyEndsWithPartition` in lib/operational-location.ts.
 */
private fun alreadyEndsWithPartition(shed: String, partition: String): Boolean {
    val whitespace = Regex("\\s+")
    val normalizedShed = shed.trim().replace(whitespace, " ").lowercase()
    val normalizedPartition = partition.trim().replace(whitespace, " ").lowercase()
    if (normalizedPartition.isEmpty()) return false
    if (normalizedPartition.all { it.isDigit() }) {
        return normalizedShed.endsWith(" $normalizedPartition") ||
            normalizedShed.endsWith(" - part $normalizedPartition") ||
            normalizedShed.endsWith(" part $normalizedPartition")
    }
    return normalizedShed.endsWith(" - $normalizedPartition") || normalizedShed.endsWith(" $normalizedPartition")
}

/**
 * Composes for a caller whose shed-NAME source may ALREADY be a composed operational display — a
 * weighing bucket's `display_name`, or a synthetic per-partition location's own `name`, both of
 * which read "Godel 2 - Part 1" while a sibling field separately says "Part 1".
 *
 * Prefer the backend's own `operational_location_display` whenever the payload carries it; this is
 * for surfaces holding only the name/label pair. It strips the partition in the exact two forms
 * [composeOperationalLocationLabel] appends, so composing an already-composed answer is a no-op.
 */
fun composeOperationalLocationLabelFromComposedName(composedName: String?, partitionLabel: String?): String {
    val name = composedName?.trim().orEmpty()
    val partition = partitionLabel?.trim().orEmpty()
    if (partition.isEmpty() || partition.equals(WHOLE_SHED_PARTITION, ignoreCase = true)) return name
    if (name.isEmpty()) return partition
    for (suffix in listOf(" - $partition", " $partition")) {
        if (name.endsWith(suffix, ignoreCase = true)) {
            val parent = name.removeSuffix(suffix).trim()
            if (parent.isNotEmpty()) return composeOperationalLocationLabel(parent, partition)
        }
    }
    return composeOperationalLocationLabel(name, partition)
}
