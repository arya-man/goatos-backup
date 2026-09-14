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
    /** The served load form (PROCUREMENT SOP), in order, only the questions that currently apply;
     *  each locked question (load_ref / vendor / farm / expected_count / notes) renders its own
     *  widget over [values], every other question over [answers] / [multiAnswers]. Empty until served. */
    val questions: List<AnimalPurchaseQuestionUi> = emptyList(),
    val questionnaireVersion: Int = 0,
    val answers: Map<String, String> = emptyMap(),
    val multiAnswers: Map<String, Set<String>> = emptyMap(),
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
    /** An authored (non-locked) load question: pick-one / text / number. */
    data class AnswerChanged(val questionId: String, val value: String) : AnimalPurchaseLoadCreateEvent
    data class MultiToggled(val questionId: String, val value: String, val checked: Boolean) : AnimalPurchaseLoadCreateEvent
    data object Submit : AnimalPurchaseLoadCreateEvent
    data object Back : AnimalPurchaseLoadCreateEvent
    /** The screen's reaction to [AnimalPurchaseLoadCreateUiState.createdLoadId]: open that load. */
    data class OpenCreatedLoad(val loadId: String) : AnimalPurchaseLoadCreateEvent
    data object DismissMessage : AnimalPurchaseLoadCreateEvent
}

// ---------------------------------------------------------------------------------------------
// L1: one load and its animals
// ---------------------------------------------------------------------------------------------

/** One not-yet-sent animal: what the person answered, labelled from the backend vocabulary. */
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
    /** True when ANY capture upload gave up (every retry spent while offline); the row then
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
    /** The inspector's OWN field verdict chip (`field_verdict_label`), VERBATIM, beside the
     *  decision; blank for legacy rows and rows without one. */
    val fieldVerdictLabel: String,
    val fieldVerdictTone: VendorsTone,
    /** Who decided, once decided; blank while pending. */
    val decidedByName: String,
    val decisionNote: String,
    /** Signed link of the card's preview: the first capture of the first media slot for a
     *  questionnaire row, the single video for a legacy row; blank when none can be served. */
    val previewUrl: String,
    val previewIsPhoto: Boolean,
    /** Stable identity of the previewed capture for the preview player. */
    val previewIdentity: String,
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

    /** A recorded animal's card was tapped: open everything the inspector entered. */
    data class OpenAnimal(val candidateId: String) : AnimalPurchaseLoadDetailEvent
}

// ---------------------------------------------------------------------------------------------
// L2: one recorded animal — everything the inspector entered, section by section, with every
// photo and video (maintainer ask 2026-09-14: "when I click on it I need to see the info I entered")
// ---------------------------------------------------------------------------------------------

@Immutable
data class AnimalPurchaseAnswerRowUi(
    val questionId: String,
    /** The served question text, VERBATIM. */
    val question: String,
    /** The backend-rendered answer, VERBATIM ("Yes · left ear", "32.5 kg"). */
    val answer: String,
    /** The SOP reads this answer as a reject signal; rendered in the warning tone. */
    val attention: Boolean,
)

@Immutable
data class AnimalPurchaseAnswerSectionUi(
    /** Backend section title, VERBATIM (blank for the unheaded first page). */
    val title: String,
    val rows: List<AnimalPurchaseAnswerRowUi>,
)

@Immutable
data class AnimalPurchaseMediaItemUi(
    val proofRef: String,
    /** Absolute app API proof route; storage signing happens only when opened. */
    val url: String,
    val isPhoto: Boolean,
)

@Immutable
data class AnimalPurchaseMediaSlotUi(
    val slot: String,
    /** Backend slot title, VERBATIM ("Photo of teeth"). */
    val title: String,
    val items: List<AnimalPurchaseMediaItemUi>,
)

@Immutable
data class AnimalPurchaseAnimalDetailUiState(
    /** Backend-owned animal title ("Animal 7 · Female goat"), VERBATIM. */
    val title: String = "",
    /** Backend-owned load title, VERBATIM, as the subtitle. */
    val loadTitle: String = "",
    val decisionLabel: String = "",
    val decisionTone: VendorsTone = VendorsTone.NEUTRAL,
    val fieldVerdictLabel: String = "",
    val fieldVerdictTone: VendorsTone = VendorsTone.NEUTRAL,
    val decidedByLine: String = "",
    val decisionNote: String = "",
    val sections: List<AnimalPurchaseAnswerSectionUi> = emptyList(),
    val mediaSlots: List<AnimalPurchaseMediaSlotUi> = emptyList(),
    /** Backend copy: section titles for the two halves and the empty sentence. */
    val answersTitle: String = "",
    val mediaTitle: String = "",
    val attentionLabel: String = "",
    val emptyMessage: String? = null,
    val isRefreshing: Boolean = false,
)

sealed interface AnimalPurchaseAnimalDetailEvent {
    data object Refresh : AnimalPurchaseAnimalDetailEvent
    data object Back : AnimalPurchaseAnimalDetailEvent

    /** A play/pause/fullscreen/share/failure action on a media preview, for analytics. */
    data class PreviewAction(val action: String) : AnimalPurchaseAnimalDetailEvent
}

// ---------------------------------------------------------------------------------------------
// L2: add an animal (the served questionnaire)
// ---------------------------------------------------------------------------------------------

/** The question kinds the phone renders, one widget each; [SECTION] is a heading with no answer. */
enum class AnimalPurchaseQuestionKind { CHOICE, MULTI, TEXT, NUMBER, MEDIA, SECTION, VENDOR }

/** One capture already taken for a media question, durable on this phone. */
@Immutable
data class AnimalPurchaseCaptureUi(
    /** The proof row id — the list key and the preview identity. */
    val proofId: String,
    val localUri: String,
    val isVideo: Boolean,
    /** True when this capture's upload gave up (every retry spent); the card offers a retry. */
    val uploadFailed: Boolean,
)

/**
 * One rendered questionnaire item, SOP wording verbatim. Only APPLICABLE questions reach the
 * screen: a question whose `only_if` does not hold is absent from the list, not disabled.
 */
@Immutable
data class AnimalPurchaseQuestionUi(
    val id: String,
    val kind: AnimalPurchaseQuestionKind,
    val title: String,
    val hint: String,
    val required: Boolean,
    /** Choice/multi options, labels verbatim. */
    val options: List<VendorsOptionUi> = emptyList(),
    /** The "other" option carries free text under `<id>_other`. */
    val allowOther: Boolean = false,
    /** Media: the slot, its cap and the capture kinds it accepts. */
    val slot: String = "",
    val maxFiles: Int = 0,
    val acceptsPhoto: Boolean = false,
    val acceptsVideo: Boolean = false,
    val captures: List<AnimalPurchaseCaptureUi> = emptyList(),
    /** True while a capture for this question is in the camera. */
    val captureWorking: Boolean = false,
    /** Number: the unit beside the field and the range line under it ("0.5–300 kg"); numbers,
     *  not copy. */
    val unit: String = "",
    val rangeLine: String = "",
    /** The message shown under this question when it failed validation (the backend's
     *  `required.hint` / `animal.other.hint`, the range line, or the SERVER's own sentence). */
    val error: String? = null,
)

@Immutable
data class AnimalPurchaseAnimalCreateUiState(
    /** The backend `copy` map, read by key at render time (`animal.form.title`, `animal.media.*`, ...). */
    val copy: Map<String, String> = emptyMap(),
    /**
     * The applicable questions of the CURRENT PAGE in served order, each carrying its captures
     * and error. The questionnaire is paged by its `section` items exactly like the SOP form:
     * page 1 is everything before the first section, then one page per section.
     */
    val questions: List<AnimalPurchaseQuestionUi> = emptyList(),
    /** Pages with at least one applicable question, and this page's position among them. */
    val stepCount: Int = 0,
    val stepIndex: Int = 0,
    /** This page's heading: the form title on page 1, the section title after. */
    val pageTitle: String = "",
    val pageHint: String = "",
    val isFirstPage: Boolean = true,
    val isLastPage: Boolean = true,
    /** Choice / text / number answers and "other" texts, keyed by question id (`<id>_other`). */
    val scalarAnswers: Map<String, String> = emptyMap(),
    /** Multi answers, keyed by question id. */
    val multiAnswers: Map<String, List<String>> = emptyMap(),
    /** The question the screen should scroll to after a refused submit; bumped per request. */
    val scrollToQuestionId: String = "",
    val scrollRequest: Int = 0,
    val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
    val writeMessage: String = "",
    val submitInFlight: Boolean = false,
    /** True once the server accepted the animal (or it is durably queued): the screen closes. */
    val closeAfterSave: Boolean = false,
)

sealed interface AnimalPurchaseAnimalCreateEvent {
    data class ChoiceChanged(val questionId: String, val value: String) : AnimalPurchaseAnimalCreateEvent
    data class MultiToggled(val questionId: String, val value: String, val checked: Boolean) : AnimalPurchaseAnimalCreateEvent

    /** Text and number answers, and the "other" free text (`<id>_other`). */
    data class TextChanged(val questionId: String, val value: String) : AnimalPurchaseAnimalCreateEvent
    data class TakePhoto(val questionId: String) : AnimalPurchaseAnimalCreateEvent
    data class RecordVideo(val questionId: String) : AnimalPurchaseAnimalCreateEvent
    data class RemoveCapture(val questionId: String, val proofId: String) : AnimalPurchaseAnimalCreateEvent

    /** The capture is on this phone but its upload gave up; send it again. */
    data class RetryCapture(val questionId: String, val proofId: String) : AnimalPurchaseAnimalCreateEvent
    data class PreviewAction(val action: String) : AnimalPurchaseAnimalCreateEvent

    /** Validates ONLY this page's questions, then moves to the next page with any applicable question. */
    data object NextPage : AnimalPurchaseAnimalCreateEvent
    data object PreviousPage : AnimalPurchaseAnimalCreateEvent
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
