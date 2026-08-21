package sg.mesha.goatos.feature.pccare

// telemetry:exempt pure UI model declarations; the @HiltViewModels in :app own the pc_care_*
// AnalyticsEvents + CrashReporter wiring for every read refresh, scan, capture, and submit.

import androidx.compose.runtime.Immutable
import java.time.LocalDate

// ---------------------------------------------------------------------------
// UI models (feature-local; mapped from DTOs by the :app @HiltViewModels).
// ---------------------------------------------------------------------------

/** Tone bucket for the worklist status chip; the :app ViewModel maps backend statuses to one. */
enum class PcCareStatusTone { NEUTRAL, REVIEW, DANGER, DONE }

/**
 * One PC Care assignment card — one planner-created task on one pen for one business date.
 * Every label is composed by the :app ViewModel from backend-owned copy; the card renders
 * them verbatim (the pen display especially — never re-derived from shed + partition here).
 */
@Immutable
data class PcCareTaskCardUi(
    /** Stable list key: the task id (globally unique — a task IS the grain of this list). */
    val listKey: String,
    val taskId: String,
    val category: String,
    /** Chip copy ("Open" / "Sent for checking" / "Needs another video" / "Done"). */
    val statusLabel: String,
    val statusTone: PcCareStatusTone,
    /** Backend-composed pen display ("Castro - 2") — rendered VERBATIM. */
    val locationDisplay: String,
    val parkLabel: String,
    val dueDateLabel: String,
    /** The assigned people, comma-joined; blank when the backend sent no names. */
    val assigneeLine: String,
    /** "12 animals", or blank before any scan. */
    val animalCountLabel: String,
    /** The verifier's rejection sentence, backend-owned, rendered VERBATIM; blank unless rework. */
    val reworkReason: String = "",
    /** True while an open-for-cancel action is offered (planner monitor only). */
    val cancellable: Boolean = false,
)

@Immutable
data class PcCareWorklistUiState(
    /** The tab's title — the backend nav label passed through, so the screen never invents one. */
    val title: String = "",
    /** ISO business date currently shown. */
    val dateLabel: String = "",
    /** Today's business date (Asia/Kolkata), the date bar's upper bound. */
    val today: String = "",
    val isRefreshing: Boolean = false,
    val lastSyncedAt: Long? = null,
    val emptyMessage: String? = null,
    val isErrorEmpty: Boolean = false,
)

sealed interface PcCareWorklistEvent {
    data object Refresh : PcCareWorklistEvent
    data class SelectDate(val date: LocalDate) : PcCareWorklistEvent
    data class OpenTask(val taskId: String) : PcCareWorklistEvent
}

// ---------------------------------------------------------------------------
// Task detail (scan + slot capture + submit)
// ---------------------------------------------------------------------------

enum class PcCareSlotState {
    /** Nothing recorded for this slot yet — the Record button is live. */
    EMPTY,

    /** This phone's own capture is compressing/uploading; [PcCareSlotChipUi.statusLabel] says which. */
    WORKING,

    /** This phone's own capture reached the server — done. */
    SYNCED,

    /** A teammate recorded this slot on another phone ("Captured by X"). */
    PEER,

    /** This phone's capture terminally failed — record again. */
    FAILED,
}

/**
 * One expected proof slot on one scanned animal. Slots are PARALLEL: each chip's enabled state
 * depends ONLY on its own [state] plus the task lifecycle lock — NEVER on a sibling slot.
 */
@Immutable
data class PcCareSlotChipUi(
    val fieldKey: String,
    /** Backend-owned slot label ("Before trimming"), rendered verbatim. */
    val label: String,
    val state: PcCareSlotState,
    /** Operator copy for the current state ("Video uploading…", "Captured by Amit", "Done"). */
    val statusLabel: String,
    /** Recorder GUIDANCE only ("Record at least 10 seconds") — never a client-enforced cap. */
    val hintLabel: String = "",
    val canRecord: Boolean = false,
)

@Immutable
data class PcCareAnimalUi(
    /** Stable list key: normalized tag (unique within the task by the duplicate rule). */
    val key: String,
    /** The tag exactly as scanned. */
    val tagLabel: String,
    /** "Scanned by Amit" attribution, or "Scan is on its way" while queued locally. */
    val scannedByLine: String,
    val slots: List<PcCareSlotChipUi>,
)

@Immutable
data class PcCareTaskUiState(
    /** The category tab's backend label, passed through the route. */
    val title: String = "",
    /** Backend-composed pen display, rendered VERBATIM. */
    val locationDisplay: String = "",
    val parkLabel: String = "",
    val dateLabel: String = "",
    val assigneeLine: String = "",
    /** True once the task is sent for checking or already approved — the screen is read-only. */
    val isLocked: Boolean = false,
    /** Farm copy for the lock ("Sent for checking" / "Approved"); blank while unlocked. */
    val lockNotice: String = "",
    /** The verifier's rejection sentence, backend-owned, VERBATIM; blank unless rework. */
    val reworkReason: String = "",
    val scanInput: String = "",
    /** Transient scan notice ("Already scanned · 1234"); auto-dismissed by the ViewModel. */
    val scanNotice: String = "",
    val animals: List<PcCareAnimalUi> = emptyList(),
    val animalCountLabel: String = "",
    val submitEnabled: Boolean = false,
    /** Why submit is blocked ("2 animals still need videos"); blank when submittable. */
    val submitBlockedReason: String = "",
    val showSubmitConfirmation: Boolean = false,
    val submitInFlight: Boolean = false,
    /** True after the submit write was durably queued — the screen shows the sent state. */
    val submitQueued: Boolean = false,
    val message: String? = null,
    val isRefreshing: Boolean = false,
)

sealed interface PcCareTaskEvent {
    data class ScanInputChanged(val value: String) : PcCareTaskEvent
    data object SubmitTypedScan : PcCareTaskEvent
    data class RecordSlot(val tagKey: String, val slotFieldKey: String) : PcCareTaskEvent
    data object Submit : PcCareTaskEvent
    data object ConfirmSubmit : PcCareTaskEvent
    data object DismissSubmitConfirmation : PcCareTaskEvent
    data object Refresh : PcCareTaskEvent
    data object Back : PcCareTaskEvent
}

// ---------------------------------------------------------------------------
// Planner (CEO create wizard + monitor list)
// ---------------------------------------------------------------------------

@Immutable
data class PcCarePlanOption(val key: String, val label: String)

@Immutable
data class PcCarePlanPenUi(
    val shedId: String,
    /** Backend-composed pen display, rendered VERBATIM. */
    val locationDisplay: String,
    val partitionLabel: String,
    /** Non-empty when a live task already covers this pen — the row renders greyed out. */
    val existingTaskId: String,
)

/**
 * Wizard steps. LIST is the monitor face of a category tab; the wizard itself runs
 * DATE -> PARK -> PEN -> OPERATORS -> REVIEW with the category fixed by the launching tab.
 */
enum class PcCarePlanStep { LIST, DATE, PARK, PEN, OPERATORS, REVIEW }

/** The wizard's ordered steps, in stepper order. */
val PC_CARE_WIZARD_STEPS: List<PcCarePlanStep> = listOf(
    PcCarePlanStep.DATE,
    PcCarePlanStep.PARK,
    PcCarePlanStep.PEN,
    PcCarePlanStep.OPERATORS,
    PcCarePlanStep.REVIEW,
)

@Immutable
data class PcCarePlanUiState(
    val title: String = "Care tasks",
    val step: PcCarePlanStep = PcCarePlanStep.LIST,
    // Monitor list scope.
    val monitorCategoryKey: String = "",
    val monitorCategoryLabel: String = "",
    val monitorDate: String = "",
    val today: String = "",
    val categories: List<PcCarePlanOption> = emptyList(),
    val isRefreshing: Boolean = false,
    val emptyMessage: String? = null,
    // Create wizard.
    val parks: List<PcCarePlanOption> = emptyList(),
    val operators: List<PcCarePlanOption> = emptyList(),
    val selectedCategoryKey: String = "",
    val selectedCategoryLabel: String = "",
    val selectedDate: String = "",
    val selectedParkId: String = "",
    val selectedParkLabel: String = "",
    val pens: List<PcCarePlanPenUi> = emptyList(),
    val pensLoading: Boolean = false,
    val pensEndReached: Boolean = true,
    val selectedShedId: String = "",
    val selectedPartitionLabel: String = "",
    val selectedPenLabel: String = "",
    val selectedOperatorIds: Set<String> = emptySet(),
    val creating: Boolean = false,
    /** Non-blank once the wizard's create landed — the wizard screen pops back on it. */
    val createdTaskId: String = "",
    val message: String? = null,
)

sealed interface PcCarePlanEvent {
    data object Refresh : PcCarePlanEvent
    data class SelectMonitorDate(val date: LocalDate) : PcCarePlanEvent
    data class CancelTask(val taskId: String) : PcCarePlanEvent
    data object CloseCreate : PcCarePlanEvent
    data class SelectDate(val date: LocalDate) : PcCarePlanEvent
    data class SelectPark(val parkId: String) : PcCarePlanEvent
    data class SelectPen(val shedId: String, val partitionLabel: String) : PcCarePlanEvent
    data object LoadMorePens : PcCarePlanEvent
    data class ToggleOperator(val userId: String) : PcCarePlanEvent
    data object NextStep : PcCarePlanEvent
    data object PreviousStep : PcCarePlanEvent
    data object Create : PcCarePlanEvent
    data object DismissMessage : PcCarePlanEvent
}
