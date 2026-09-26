package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for every read, scan, lookup and submit.

import androidx.compose.runtime.Immutable

/**
 * UI models for the park head's TAG-ONLY Sales module (maintainer decision 2026-09-11): the
 * sales at their park still owed animals, and tagging those animals from the pen -- reader or
 * hand-typed tag, a weight per animal, submit when all are tagged. Nothing else of Sales is on
 * these screens: no buyer, no money, no pipeline.
 *
 * Every business sentence is BACKEND-OWNED and rendered verbatim: `remaining`, the pen name,
 * `blocked_reason`. Field labels are app chrome, as on every other phone entry form.
 */

// ---------------------------------------------------------------------------------------------
// The queue
// ---------------------------------------------------------------------------------------------

@Immutable
data class SaleTaggingCardUi(
    val dealId: String,
    /** "Goat · Malai · CPT" */
    val title: String,
    /** "12 animals · 8 tagged" */
    val countLine: String,
    /** "Sale date 11/09/2026" */
    val metaLine: String,
    /** "4 to tag", backend `remaining`. */
    val remainingChip: String,
)

@Immutable
data class SaleTaggingListUiState(
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val rows: List<SaleTaggingCardUi> = emptyList(),
    val hasMore: Boolean = false,
    val loadingMore: Boolean = false,
    /** Null while the first page is still unknown. */
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
)

sealed interface SaleTaggingListEvent {
    data object Refresh : SaleTaggingListEvent
    data object LoadMore : SaleTaggingListEvent
    data class OpenSale(val dealId: String) : SaleTaggingListEvent
}

// ---------------------------------------------------------------------------------------------
// Tagging one sale
// ---------------------------------------------------------------------------------------------

/** One animal in the basket with the weight typed for it. */
@Immutable
data class SaleTaggingBasketAnimalUi(
    val goatId: String,
    /** The RFID/tag the park head recognises the animal by. */
    val tag: String,
    /** Backend pen name, VERBATIM. */
    val location: String,
    val weight: String,
    /** Blank until Submit finds the box empty or wrong. */
    val weightError: String,
    /** Backend farm copy when the review refused this animal; blank otherwise. */
    val blockedReason: String,
)

/** One animal a lookup found when the typed tag matched more than one, or none exactly. */
@Immutable
data class SaleTaggingMatchUi(
    val goatId: String,
    val tag: String,
    /** "G-1042 · Malai · male" */
    val detailLine: String,
    val location: String,
    val sellable: Boolean,
    val blockedReason: String,
)

/** One animal already tagged to this sale before this session, as the server has it. */
@Immutable
data class SaleTaggingDoneAnimalUi(
    val tag: String,
    val location: String,
    /** "32.5 kg · ₹5,200" or whichever half was recorded. */
    val figures: String,
)

@Immutable
data class SaleTaggingUiState(
    /** "Goat · Malai · CPT · 12 animals" */
    val saleLine: String = "",
    /** "8 of 12 tagged · 4 to tag" -- from the backend's counts, never derived past the basket. */
    val progressLine: String = "",
    val tagInput: String = "",
    /** The Bluetooth reader is switched on for the tag field. */
    val scanning: Boolean = false,
    val lookupInFlight: Boolean = false,
    /** Warn-toned line under the field; null when there is nothing to say. */
    val lookupMessage: String? = null,
    val matches: List<SaleTaggingMatchUi> = emptyList(),
    val basket: List<SaleTaggingBasketAnimalUi> = emptyList(),
    val alreadyTagged: List<SaleTaggingDoneAnimalUi> = emptyList(),
    /** "Submit 4 animals" */
    val submitLabel: String = "",
    val canSubmit: Boolean = false,
    /** Why Submit is held back, blank when it is offered. */
    val submitHint: String = "",
    val submitInFlight: Boolean = false,
    val done: Boolean = false,
    val doneLine: String = "",
    val isLoading: Boolean = true,
    val message: String? = null,
)

sealed interface SaleTaggingEvent {
    data object Back : SaleTaggingEvent
    data class TagInputChanged(val text: String) : SaleTaggingEvent
    data object ToggleScan : SaleTaggingEvent
    data object Lookup : SaleTaggingEvent
    data class AddMatch(val goatId: String) : SaleTaggingEvent
    data class Remove(val goatId: String) : SaleTaggingEvent
    data class WeightChanged(val goatId: String, val value: String) : SaleTaggingEvent
    data object Submit : SaleTaggingEvent
    data object Done : SaleTaggingEvent
    data object DismissMessage : SaleTaggingEvent
}
