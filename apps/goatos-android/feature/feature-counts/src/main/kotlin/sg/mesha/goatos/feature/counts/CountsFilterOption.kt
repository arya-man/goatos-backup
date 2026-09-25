package sg.mesha.goatos.feature.counts

import androidx.compose.runtime.Immutable

/**
 * One selectable value in a Counts picker/dropdown, straight from the backend's `facets`.
 *
 * [key] is what the picker SENDS (a park/shed uuid, or the breed's own value) and [label] is only
 * ever rendered — the two are never interchangeable. [count] is the facet's own head count for
 * this value, shown next to the option so an operator can see the size of a cohort before
 * selecting it. Nothing here is invented on device: the whole vocabulary is backend-owned.
 *
 * This used to live beside the census read screen; that screen was removed from the phone
 * (maintainer decision 2026-07-30) while the birth/death/shifting capture forms kept using the
 * same backend facet vocabulary for their park/shed/breed pickers.
 */
@Immutable
data class CountsFilterOptionUi(
    val key: String,
    val label: String,
    val count: Int,
)

/**
 * Most species / gender choices the birth form places on one line before wrapping. Two and three
 * fit comfortably; a farm that configures more wraps onto a second line at the same cell width
 * rather than breaking a name mid-word.
 */
internal const val BIRTH_VOCABULARY_PER_ROW = 3

/**
 * The birth form's species or sex choices: the farm's Configuration list when the backend served one
 * (OPEN UP TO NEW SPECIES, 2026-09-25), else the built-in pair -- an older backend, or a first open
 * with no cached read yet. The labels are the farm's own names, rendered verbatim.
 */
internal fun birthVocabularyOptions(
    configured: List<CountsFilterOptionUi>,
    builtIn: List<Pair<String, String>>,
): List<Pair<String, String>> =
    if (configured.isEmpty()) builtIn else configured.map { it.key to it.label }

/**
 * Keeps [current] when the farm's list still offers it, else the list's first entry. An empty list
 * (not read yet, or an older backend) keeps [current]: the built-in default stays selected.
 */
fun keepOrFirst(current: String, offered: List<CountsFilterOptionUi>): String =
    if (offered.isEmpty() || offered.any { it.key == current }) current else offered.first().key
