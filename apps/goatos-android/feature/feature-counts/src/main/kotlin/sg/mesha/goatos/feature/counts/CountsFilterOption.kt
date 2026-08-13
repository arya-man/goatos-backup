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
