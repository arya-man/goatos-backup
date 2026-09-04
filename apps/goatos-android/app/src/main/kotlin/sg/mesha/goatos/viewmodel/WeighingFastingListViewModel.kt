package sg.mesha.goatos.viewmodel

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import dagger.hilt.android.qualifiers.ApplicationContext
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.weighing.WeighingFastingCard
import sg.mesha.goatos.core.data.weighing.WeighingFastingRepository
import sg.mesha.goatos.feature.weighing.WeighingFastingCardUiRow
import sg.mesha.goatos.feature.weighing.WeighingFastingTone
import sg.mesha.goatos.feature.weighing.R as WeighingR
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

/** The removal cards section on the operator's weighing list plus its Room-SSOT refresh. */
data class WeighingFastingSectionUiState(
    val cards: List<WeighingFastingCardUiRow> = emptyList(),
    val isSyncing: Boolean = false,
)

/**
 * The feed & water removal SECTION of the operator's weighing list (maintainer decision
 * 2026-09-03). Its own ViewModel beside [WeighingViewModel] on purpose: the removal cards are a
 * separate backend read with a separate Room cache, and the 4k-line work-list state holder is not
 * the place to grow another one. The screen renders Room; [refresh] only writes into it — a card
 * is read at night in a shed, where the network is worst, so a failed refresh leaves the cached
 * cards up.
 */
@HiltViewModel
class WeighingFastingListViewModel @Inject constructor(
    private val repository: WeighingFastingRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
    @ApplicationContext private val appContext: Context,
) : ViewModel() {

    private val syncing = MutableStateFlow(false)

    val state: StateFlow<WeighingFastingSectionUiState> =
        combine(repository.observeCards(), syncing.asStateFlow()) { cache, busy ->
            WeighingFastingSectionUiState(
                cards = cache.cards.map { it.toUiRow(appContext) },
                isSyncing = busy,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), WeighingFastingSectionUiState())

    fun refresh() {
        if (syncing.value) return
        syncing.value = true
        viewModelScope.launch {
            try {
                when (val result = repository.refresh()) {
                    is AppResult.Ok -> Unit
                    // Quiet by design: the cached cards stay on screen, exactly like the work
                    // list's own stale handling. A hard failure is still visible in telemetry.
                    is AppResult.Err -> {
                        crashReporter.recordException(
                            result.cause ?: IllegalStateException(result.message),
                            "weighing removal cards refresh failed",
                        )
                        analytics.track(
                            AnalyticsEventsWeighing.WEIGHING_REMOVAL_FAILURE,
                            mapOf(AnalyticsEvents.Params.REASON to result.message.take(MAX_REASON_CHARS)),
                        )
                    }
                }
            } finally {
                syncing.value = false
            }
        }
    }

    /** Card-open telemetry; navigation itself is owned by the host. */
    fun onCardOpened(row: WeighingFastingCardUiRow) {
        analytics.track(
            AnalyticsEventsWeighing.WEIGHING_REMOVAL_CARD_OPENED,
            mapOf(AnalyticsEvents.Params.STATUS to row.status),
        )
    }

    private companion object {
        const val MAX_REASON_CHARS = 96
    }
}

/** Maps one backend PER-SHED card to its list row (maintainer correction #2, 2026-09-03). Every
 *  composed string is fixed farm chrome or verbatim backend copy; the "Tonight" reading is
 *  display-only convenience, never a gate. */
internal fun WeighingFastingCard.toUiRow(context: Context): WeighingFastingCardUiRow {
    val status = dto.status.trim().lowercase()
    val tone = when (status) {
        "pending_verification" -> WeighingFastingTone.IN_REVIEW
        "rework" -> WeighingFastingTone.SENT_BACK
        "completed" -> WeighingFastingTone.DONE
        else -> WeighingFastingTone.ACTION
    }
    val statusLabel = when (tone) {
        WeighingFastingTone.ACTION -> context.getString(WeighingR.string.weighing_removal_status_open)
        WeighingFastingTone.IN_REVIEW -> context.getString(WeighingR.string.weighing_removal_status_in_review)
        WeighingFastingTone.SENT_BACK -> context.getString(WeighingR.string.weighing_removal_status_sent_back)
        WeighingFastingTone.DONE -> context.getString(WeighingR.string.weighing_removal_status_done)
    }
    val todayIst = LocalDate.now(ZoneId.of("Asia/Kolkata")).toString()
    return WeighingFastingCardUiRow(
        // FULL identity: a round's cards share the task id, so the shed id is part of the key.
        uiKey = "removal|${dto.fastingTaskId}|${dto.campaignShedId}",
        fastingTaskId = dto.fastingTaskId,
        campaignShedId = dto.campaignShedId,
        title = dto.subjectLabel,
        dateLabel = if (dto.removalBusinessDate == todayIst) {
            context.getString(WeighingR.string.weighing_removal_date_tonight)
        } else {
            farmRemovalDateLabel(dto.removalBusinessDate)
        },
        status = status,
        statusLabel = statusLabel,
        tone = tone,
        reworkReason = if (tone == WeighingFastingTone.SENT_BACK) dto.reworkReason.orEmpty() else "",
        openable = tone == WeighingFastingTone.ACTION || tone == WeighingFastingTone.SENT_BACK,
    )
}

/** Farm-readable removal-evening label ("Wed 2 Sep"); a value that fails to
 *  parse renders verbatim rather than crashing a card over a date string. */
internal fun farmRemovalDateLabel(isoDate: String): String = try {
    LocalDate.parse(isoDate).format(
        java.time.format.DateTimeFormatter.ofPattern("EEE d MMM", java.util.Locale.ENGLISH),
    )
} catch (_: java.time.format.DateTimeParseException) {
    isoDate
}
