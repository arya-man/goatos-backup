package sg.mesha.goatos.feature.vendors

import androidx.compose.runtime.Immutable

/**
 * UI models of the Procurement module's Animal purchases tab (maintainer decision 2026-09-13,
 * docs/decisions/animal-purchases.md). Every visible sentence, label and chip word is BACKEND-OWNED
 * (the load/animal payload fields and the options `copy` map) and passed through verbatim; the
 * ViewModels in :app compose nothing but the number lines the backend publishes no copy for.
 */

// ---------------------------------------------------------------------------------------------
// L0: the load list
// ---------------------------------------------------------------------------------------------

@Immutable
data class AnimalPurchaseLoadCardUi(
    /** Stable list key — the load id IS this list's grain. */
    val listKey: String,
    val loadId: String,
    /** Backend-owned row title ("Load 132 · Vendor"), VERBATIM. */
    val title: String,
    /** Backend-owned line under the title, VERBATIM. */
    val summary: String,
    /** WHOLE-LOAD counts, never page sums. */
    val pending: Int,
    val accepted: Int,
    val rejected: Int,
)

@Immutable
data class AnimalPurchaseLoadsUiState(
    /** The tab's title — the backend nav label passed through, so the screen never invents one. */
    val title: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    /** Backend-owned "nothing here" copy (`loads.empty`); null before the options land. */
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
    /** Whether THIS caller may add loads (the server's `can_record`). The action hides otherwise. */
    val canRecord: Boolean = false,
    /** Backend-owned action label (`loads.add`), VERBATIM. */
    val addLabel: String = "",
)

sealed interface AnimalPurchaseLoadsEvent {
    data object Refresh : AnimalPurchaseLoadsEvent
    data class OpenLoad(val loadId: String) : AnimalPurchaseLoadsEvent
    data object AddLoad : AnimalPurchaseLoadsEvent
}

// ---------------------------------------------------------------------------------------------
// L1: add a load
// ---------------------------------------------------------------------------------------------

enum class AnimalPurchaseLoadField { LOAD_REF, VENDOR, FARM, EXPECTED_COUNT, NOTES }

@Immutable
data class AnimalPurchaseLoadCreateUiState(
    /** The backend `copy` map, read by key at render time (`load.form.title`, `load.field.*`, `load.save`). */
    val copy: Map<String, String> = emptyMap(),
    val values: Map<AnimalPurchaseLoadField, String> = emptyMap(),
    /** The ACTIVE vendor register, labels verbatim. */
    val vendors: List<VendorsOptionUi> = emptyList(),
    /** The backend farm choices, labels verbatim. */
    val farms: List<VendorsOptionUi> = emptyList(),
    /** Fields the form refused before queueing; the message is the backend's `required.hint`. */
    val fieldErrors: Set<AnimalPurchaseLoadField> = emptySet(),
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val submitInFlight: Boolean = false,
    /** Set once the server accepted the load: the screen navigates to that load and closes. */
    val createdLoadId: String? = null,
)

sealed interface AnimalPurchaseLoadCreateEvent {
    data class FieldChanged(val field: AnimalPurchaseLoadField, val value: String) : AnimalPurchaseLoadCreateEvent
    data object Submit : AnimalPurchaseLoadCreateEvent
    data object Back : AnimalPurchaseLoadCreateEvent
    /** The screen's reaction to [AnimalPurchaseLoadCreateUiState.createdLoadId]: open that load. */
    data class OpenCreatedLoad(val loadId: String) : AnimalPurchaseLoadCreateEvent
    data object DismissMessage : AnimalPurchaseLoadCreateEvent
}

// ---------------------------------------------------------------------------------------------
// L1: one load and its animals
// ---------------------------------------------------------------------------------------------

/** One not-yet-sent animal: what the person typed, labelled from the backend vocabulary. */
@Immutable
data class AnimalPurchaseQueuedAnimalUi(
    val listKey: String,
    val title: String,
    val breed: String,
    val ageWeightLine: String,
    val conditionLabel: String,
    val tempTag: String,
    /** Backend-owned waiting copy (`animal.queued`), VERBATIM. */
    val waitingLabel: String,
    /** True when the video upload gave up (every retry spent while offline); the row then
     *  offers a tap-to-retry instead of waiting forever. */
    val sendFailed: Boolean,
    /** Backend-owned failed copy (`animal.send_failed`), VERBATIM. */
    val failedLabel: String,
)

@Immutable
data class AnimalPurchaseAnimalCardUi(
    /** Stable list key — the candidate id IS this list's grain. */
    val listKey: String,
    val candidateId: String,
    /** Backend-owned row title ("Animal 7 · Female goat"), VERBATIM. */
    val title: String,
    val breed: String,
    /** "8 months · 24 kg" — the two numbers the backend publishes no line for; blank when neither. */
    val ageWeightLine: String,
    /** Backend-owned condition word, VERBATIM. */
    val conditionLabel: String,
    val tempTag: String,
    /** Backend-owned decision chip copy, VERBATIM, with its tone. */
    val decisionLabel: String,
    val decisionTone: VendorsTone,
    /** Who decided, once decided; blank while pending. */
    val decidedByName: String,
    val decisionNote: String,
    /** Signed playback link for the recorded video; blank when it cannot be served right now. */
    val mediaUrl: String,
)

@Immutable
data class AnimalPurchaseCountChipUi(val label: String, val count: Int, val tone: VendorsTone)

@Immutable
data class AnimalPurchaseLoadDetailUiState(
    /** Backend-owned load title, VERBATIM. */
    val title: String = "",
    /** Backend-owned summary line, VERBATIM. */
    val summary: String = "",
    /** Whole-load counts as chips: awaiting / accepted / rejected. */
    val countChips: List<AnimalPurchaseCountChipUi> = emptyList(),
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val canRecord: Boolean = false,
    /** Backend-owned action label (`load.animals.add`), VERBATIM. */
    val addLabel: String = "",
    /** Backend-owned section title (`load.animals.title`). */
    val animalsTitle: String = "",
    /** Animals saved on this phone that have not reached the server yet, newest last. Rendered
     *  ABOVE the server rows with a waiting chip so a save made without signal is never invisible. */
    val queuedAnimals: List<AnimalPurchaseQueuedAnimalUi> = emptyList(),
    /** Backend-owned empty copy (`load.animals.empty`). */
    val emptyMessage: String? = null,
    /** Backend-owned prefix for the decided line (`animal.decided_by`). */
    val decidedByLabel: String = "",
)

sealed interface AnimalPurchaseLoadDetailEvent {
    data object Refresh : AnimalPurchaseLoadDetailEvent
    data object Back : AnimalPurchaseLoadDetailEvent
    data object AddAnimal : AnimalPurchaseLoadDetailEvent

    /** A play/pause/fullscreen/share/failure action on a recorded video preview, for analytics. */
    data class PreviewAction(val action: String) : AnimalPurchaseLoadDetailEvent

    /** Retry a not-yet-sent animal whose video upload gave up. */
    data class RetryQueued(val listKey: String) : AnimalPurchaseLoadDetailEvent
}

// ---------------------------------------------------------------------------------------------
// L2: add an animal
// ---------------------------------------------------------------------------------------------

enum class AnimalPurchaseAnimalField { SPECIES, SEX, BREED, AGE_MONTHS, WEIGHT_KG, CONDITION, TEMP_TAG, NOTES }

/** The one video slot's state on this phone. RECORDED means a durable proof row exists. */
enum class AnimalPurchaseVideoStatus { NONE, WORKING, RECORDED, FAILED }

@Immutable
data class AnimalPurchaseAnimalCreateUiState(
    /** The backend `copy` map, read by key at render time (`animal.form.title`, `animal.field.*`, ...). */
    val copy: Map<String, String> = emptyMap(),
    val values: Map<AnimalPurchaseAnimalField, String> = emptyMap(),
    val species: List<VendorsOptionUi> = emptyList(),
    val sexes: List<VendorsOptionUi> = emptyList(),
    val conditions: List<VendorsOptionUi> = emptyList(),
    /** The herd's own breed spellings; the field stays free text. */
    val breedSuggestions: List<String> = emptyList(),
    val fieldErrors: Set<AnimalPurchaseAnimalField> = emptySet(),
    val videoStatus: AnimalPurchaseVideoStatus = AnimalPurchaseVideoStatus.NONE,
    /** Local file of the recorded clip, for the preview; blank until recorded. */
    val videoLocalUri: String = "",
    /** Stable identity of the recorded clip (its proof row id) for the preview player. */
    val videoIdentity: String = "",
    /** True when the form refused to save because no video was recorded. */
    val videoMissing: Boolean = false,
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val submitInFlight: Boolean = false,
    /** True once the server accepted the animal (or it is durably queued): the screen closes. */
    val closeAfterSave: Boolean = false,
)

sealed interface AnimalPurchaseAnimalCreateEvent {
    data class FieldChanged(val field: AnimalPurchaseAnimalField, val value: String) : AnimalPurchaseAnimalCreateEvent
    data object RecordVideo : AnimalPurchaseAnimalCreateEvent

    /** The clip is on this phone but its upload gave up; send it again. */
    data object RetryVideoUpload : AnimalPurchaseAnimalCreateEvent
    data class VideoPreviewAction(val action: String) : AnimalPurchaseAnimalCreateEvent
    data object Submit : AnimalPurchaseAnimalCreateEvent
    data object Back : AnimalPurchaseAnimalCreateEvent
    data object DismissMessage : AnimalPurchaseAnimalCreateEvent
}

/** Maps the backend's `decision_tone` (`neutral` | `ok` | `bad`) to the module's chip tone. */
fun animalPurchaseDecisionTone(wire: String): VendorsTone = when (wire.trim().lowercase()) {
    "ok" -> VendorsTone.OK
    "bad" -> VendorsTone.DANGER
    else -> VendorsTone.NEUTRAL
}
