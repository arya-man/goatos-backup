package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.data.CaptureDraftRepository
import sg.mesha.goatos.core.data.CaptureFlow
import sg.mesha.goatos.core.data.ShiftingPendingRepository
import sg.mesha.goatos.core.network.dto.CountsShiftingPendingExecutionItemDto
import sg.mesha.goatos.feature.counts.ShiftingPendingEvent
import sg.mesha.goatos.feature.counts.ShiftingPendingRowUi
import sg.mesha.goatos.feature.counts.ShiftingPendingStatusUi
import sg.mesha.goatos.feature.counts.ShiftingStateTone
import sg.mesha.goatos.feature.counts.ShiftingPreviousDateUi
import sg.mesha.goatos.feature.counts.ShiftingPendingUiState
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale
import javax.inject.Inject

/** Offline-first, date/status-scoped renderer for backend-owned Shifting Actions history. */
@HiltViewModel
class ShiftingPendingViewModel @Inject constructor(
    private val repo: ShiftingPendingRepository,
    private val drafts: CaptureDraftRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val today: LocalDate get() = LocalDate.now(IST)
    private val _selection = MutableStateFlow(Selection(today.toString(), STATUS_ALL))
    private val _isOffline = MutableStateFlow(false)
    private val _lastSyncedAt = MutableStateFlow<Long?>(null)

    /**
     * The paged Actions rows, decorated with each task's local evidence progress.
     *
     * The progress comes from ONE bounded Room observation combined with the page — never a
     * per-row lookup, which would be an N+1 read behind a list (see
     * docs/decisions/mobile-data-fetch-anti-patterns.md).
     */
    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<ShiftingPendingRowUi>> = combine(
        _selection
            .flatMapLatest { repo.pending(date = it.dateIso, status = it.status) }
            .map { page -> page.map { it.toRowUi() } }
            .cachedIn(viewModelScope),
        drafts.observeProgress(CaptureFlow.SHIFTING),
    ) { page, progress ->
        page.map { row -> row.withEvidenceProgress(progress[row.shiftingEventId] ?: 0) }
    }

    val state: StateFlow<ShiftingPendingUiState> = combine(
        _selection, _isOffline, _lastSyncedAt, repo.actionsMeta,
    ) { selection, isOffline, lastSyncedAt, meta ->
        val date = LocalDate.parse(selection.dateIso)
        ShiftingPendingUiState(
            dateIso = selection.dateIso,
            dateLabel = if (date == today) "Today · ${date.format(DATE_LABEL)}" else date.format(DATE_LABEL),
            isToday = date == today,
            statuses = STATUSES.map { (key, label) -> ShiftingPendingStatusUi(key, label, key == selection.status, when(key) {
                "all" -> meta.counts.all; "pending" -> meta.counts.pending; "authorized" -> meta.counts.authorized
                "rework" -> meta.counts.rework; else -> meta.counts.completed
            }) },
            previousDates = if (meta.date == selection.dateIso) meta.previousDates.mapNotNull { item ->
                runCatching { LocalDate.parse(item.date) }.getOrNull()?.let { ShiftingPreviousDateUi(item.date, it.format(DATE_LABEL), item.actionCount) }
            } else emptyList(),
            emptyMessage = if (isOffline) OFFLINE_EMPTY else EMPTY_MESSAGE,
            isErrorEmpty = isOffline,
            lastSyncedAt = lastSyncedAt,
            isOffline = isOffline,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), ShiftingPendingUiState())

    init { analytics.track(AnalyticsEvents.COUNTS_SHIFTING_PENDING_VIEWED) }

    fun onRowsLoadFailed(error: Throwable) {
        _isOffline.value = true
        crashReporter.recordException(error, "shifting actions page load failed")
    }

    fun onRowsLoaded() {
        _isOffline.value = false
        _lastSyncedAt.value = System.currentTimeMillis()
    }

    fun onEvent(event: ShiftingPendingEvent) {
        when (event) {
            ShiftingPendingEvent.Refresh -> _selection.value = _selection.value.copy()
            ShiftingPendingEvent.PrevDay -> selectDate(LocalDate.parse(_selection.value.dateIso).minusDays(1))
            ShiftingPendingEvent.NextDay -> selectDate(LocalDate.parse(_selection.value.dateIso).plusDays(1))
            ShiftingPendingEvent.Today -> selectDate(today)
            is ShiftingPendingEvent.SelectDate -> runCatching { LocalDate.parse(event.dateIso) }.getOrNull()?.let(::selectDate)
            is ShiftingPendingEvent.OpenPreviousDate -> selectDate(LocalDate.parse(event.dateIso))
            is ShiftingPendingEvent.SelectStatus -> if (STATUSES.any { it.first == event.status }) {
                _selection.value = _selection.value.copy(status = event.status)
            }
            is ShiftingPendingEvent.OpenMovement, ShiftingPendingEvent.Raise, ShiftingPendingEvent.Back -> Unit
        }
    }

    /**
     * Fills in "videos recorded / videos required" for a task still awaiting the operator. The
     * requirement is the movement's own: a high-priority move embeds feed packing and feeding, so it
     * needs three live videos where a low-priority move needs one
     * (docs/decisions/shifting-verification.md).
     */
    private fun ShiftingPendingRowUi.withEvidenceProgress(capturedCount: Int): ShiftingPendingRowUi {
        val required = if (priority.equals("high", ignoreCase = true)) HIGH_PRIORITY_VIDEOS else 1
        return copy(videosRequired = required, videosCaptured = minOf(capturedCount, required))
    }

    private fun selectDate(date: LocalDate) {
        _selection.value = _selection.value.copy(dateIso = minOf(date, today).toString())
    }

    private fun CountsShiftingPendingExecutionItemDto.toRowUi() = ShiftingPendingRowUi(
        shiftingEventId = shiftingEventId,
        // Prefer the backend-composed operational-location label so a Castro 1 -> Castro 2 move
        // reads as "Castro - 1" -> "Castro - 2" instead of "Castro" -> "Castro". Shed name remains
        // the fallback for an older server that does not send the composed field yet.
        sourceLabel = sourceOperationalLocationDisplay?.takeIf(String::isNotBlank)
            ?: (sourceShedName ?: sourceParkName)?.takeIf(String::isNotBlank) ?: UNKNOWN_LOCATION,
        destinationLabel = destinationOperationalLocationDisplay.takeIf(String::isNotBlank)
            ?: destinationShedName.takeIf(String::isNotBlank)
            ?: destinationParkName.takeIf(String::isNotBlank) ?: UNKNOWN_LOCATION,
        priority = priority.titleCase(),
        category = category.titleCase(),
        animalCount = animalCount,
        approvedAtLabel = approvedAtIst?.take(10).orEmpty(),
        primaryActionKey = primaryActionKey,
        actionStateLabel = when {
            eventStatus == "pending" -> "Awaiting Park Head approval"
            eventStatus == "authorized" -> "Approved"
            eventStatus == "applied" && primaryActionKey == "execute" -> "Evidence rework"
            eventStatus == "applied" -> "Completed"
            else -> "Action required"
        },
        // Waiting is for a row the operator cannot clear by themselves: an unapproved movement
        // (someone else must act first) or one whose evidence a verifier sent back. Both need to be
        // read at a glance, and the unapproved one carries the whole explanation for why its card
        // does not respond to a tap.
        actionStateTone = when {
            eventStatus == "pending" -> ShiftingStateTone.Waiting
            eventStatus == "applied" && primaryActionKey == "execute" -> ShiftingStateTone.Waiting
            eventStatus == "applied" -> ShiftingStateTone.Done
            else -> ShiftingStateTone.Neutral
        },
    )

    private fun String.titleCase() = if (isEmpty()) this else replaceFirstChar {
        if (it.isLowerCase()) it.titlecase(Locale.getDefault()) else it.toString()
    }

    private data class Selection(val dateIso: String, val status: String)

    private companion object {
        /** Shifting + feed packing + feed given (docs/decisions/shifting-verification.md). */
        const val HIGH_PRIORITY_VIDEOS = 3
        val IST: ZoneId = ZoneId.of("Asia/Kolkata")
        val DATE_LABEL: DateTimeFormatter = DateTimeFormatter.ofPattern("d MMM", Locale.ENGLISH)
        const val STATUS_ALL = "all"
        val STATUSES = listOf(
            "all" to "All", "pending" to "Pending", "authorized" to "Approved",
            "rework" to "Rework", "completed" to "Completed",
        )
        const val EMPTY_MESSAGE = "No shifting actions for this date and status."
        const val OFFLINE_EMPTY = "Couldn't refresh Shifting Actions. Cached actions will appear when available."
        const val UNKNOWN_LOCATION = "—"
    }
}
