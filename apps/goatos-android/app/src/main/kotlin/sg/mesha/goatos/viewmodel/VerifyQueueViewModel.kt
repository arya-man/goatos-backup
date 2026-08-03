package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.delay
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.feature.verify.VerificationQueueRow
import sg.mesha.goatos.feature.verify.VerifyDriveClosure
import sg.mesha.goatos.feature.verify.VerifyCategoryOption
import sg.mesha.goatos.feature.verify.VerifyLocationFilterOption
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueUiState
import sg.mesha.goatos.feature.verify.VerifyScopeType
import sg.mesha.goatos.feature.verify.VerifyTone
import sg.mesha.goatos.feature.verify.VerifyModuleTab
import javax.inject.Inject

private const val VERIFY_QUEUE_PAGE_SIZE = 20

private data class VerifyQueueFlags(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val isLoadingMore: Boolean = false,
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
)

private data class VerifyCloseFlags(
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
)

/**
 * The standalone Verifier section's queue state holder (context/architecture/
 * verifier-app-and-flow.md). Offline-first (docs/decisions/android-offline-first.md): [state]
 * is fed by [VerificationRepository.observeQueue], a cache-first Room Flow re-subscribed (via
 * [flatMapLatest]) whenever the category filter changes; [refresh] drives the network side of
 * stale-while-revalidate and [loadMore] appends the next ~20-row keyset page into the SAME
 * Room-backed scope (never an in-memory-only accumulation — mobile-guard rule). Category
 * options are derived from the distinct categories the backend has actually returned for THIS
 * verifier, never a client-hardcoded category enum (verification-module-design.md §2.3
 * plug-and-play registry) — only the raw category KEY crosses this boundary; the Compose layer
 * decides how to render an unrecognized key and always owns the "All" chrome string.
 */
@HiltViewModel
class VerifyQueueViewModel @Inject constructor(
    private val repo: VerificationRepository,
    private val syncRepo: SyncRepository,
    private val analytics: AnalyticsPort,
    savedStateHandle: SavedStateHandle,
) : ViewModel() {

    private val isActionQueue: Boolean = savedStateHandle.get<Boolean>("actionMode") ?: false

    /**
     * The queue's CATEGORY -- the backend's own vocabulary and the only thing the reads accept.
     *
     * Three cases, deliberately distinct:
     *  - an explicit `?category=` (the verifier Alerts href the backend composes) is passed
     *    through VERBATIM, so a category this client has never heard of still reads its own rows;
     *  - an explicit `?module=` is mapped, and an UNRECOGNIZED module resolves to null. The old
     *    `else -> VACCINATION` fallback meant a Counts or Feed verifier opened their own tab and
     *    was shown VACCINATION proofs -- a worse failure than showing nothing;
     *  - no scoping arg at all (the action queue route) keeps the historical vaccination default,
     *    which is a route that never asked for a module rather than one that asked wrongly.
     */
    private val _selectedCategory = MutableStateFlow(
        run {
            val category = savedStateHandle.get<String>(CATEGORY_ARG)?.trim()?.lowercase()
                ?.takeIf { it.isNotBlank() }
            val moduleKey = savedStateHandle.get<String>(MODULE_ARG)?.trim()?.takeIf { it.isNotBlank() }
            when {
                category != null -> category
                moduleKey != null -> categoryForModuleKey(moduleKey)
                else -> VACCINATION_CATEGORY
            }
        },
    )
    private val _selectedParkId = MutableStateFlow<String?>(null)
    private val _selectedShedId = MutableStateFlow<String?>(null)
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)
    private val _closingBatchId = MutableStateFlow<String?>(null)
    private val _closeErrorBatchId = MutableStateFlow<String?>(null)
    private val _closeErrorMessage = MutableStateFlow<String?>(null)

    // flatMapLatest cancels the previous category's Room collection and starts a fresh one the
    // moment _selectedCategory changes (same pattern as CalendarViewModel's _selectedDay).
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedResource: StateFlow<Resource<VerificationQueueResponseDto>> =
        combine(_selectedCategory, _selectedParkId, _selectedShedId) { category, parkId, shedId ->
            Triple(category, parkId, shedId)
        }.flatMapLatest { (category, parkId, shedId) ->
            if (category != null) {
                if (isActionQueue) {
                    repo.observeActionQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_QUEUE_PAGE_SIZE)
                } else {
                    repo.observeQueue(category = category, parkId = parkId, shedId = shedId, limit = VERIFY_QUEUE_PAGE_SIZE)
                }
            } else {
                kotlinx.coroutines.flow.flowOf(Resource(data = VerificationQueueResponseDto(items = emptyList())))
            }
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    private val closeFlags: StateFlow<VerifyCloseFlags> = combine(
        _closingBatchId,
        _closeErrorBatchId,
        _closeErrorMessage,
    ) { closingBatchId, closeErrorBatchId, closeError ->
        VerifyCloseFlags(closingBatchId, closeErrorBatchId, closeError)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyCloseFlags())

    private val flags: StateFlow<VerifyQueueFlags> = combine(
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
        closeFlags,
    ) { isRefreshing, isOffline, isLoadingMore, closeFlags ->
        VerifyQueueFlags(
            isRefreshing = isRefreshing,
            isOffline = isOffline,
            isLoadingMore = isLoadingMore,
            closingBatchId = closeFlags.closingBatchId,
            closeErrorBatchId = closeFlags.closeErrorBatchId,
            closeErrorMessage = closeFlags.closeErrorMessage,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyQueueFlags())

    val state: StateFlow<VerifyQueueUiState> = combine(
        observedResource,
        _selectedCategory,
        _selectedParkId,
        _selectedShedId,
        flags,
    ) { resource, category, parkId, shedId, flags ->
        val items = resource.data?.items.orEmpty()
        VerifyQueueUiState(
            rows = items.map { it.toRow() },
            // Null for a category this client has no dedicated chrome for (counts, feed). The
            // rows still render generically; only the module-specific grouping stands down.
            selectedModule = moduleForCategory(category),
            selectedCategory = category,
            // The route named a module or category nothing here can serve. Say so instead of
            // rendering another module's queue or a bare "all caught up".
            isUnsupportedModule = category == null,
            isActionQueue = isActionQueue,
            parkOptions = locationOptions("All parks", resource.data?.filterOptions?.parks.orEmpty().map { it.id to it.label }, parkId),
            selectedParkId = parkId,
            shedOptions = locationOptions("All sheds", resource.data?.filterOptions?.sheds.orEmpty().map { it.id to it.label }, shedId),
            selectedShedId = shedId,
            isRefreshing = flags.isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = flags.isOffline,
            hasMore = !isActionQueue && resource.data?.nextCursor != null,
            isLoadingMore = flags.isLoadingMore,
            driveClosures = resource.data?.driveClosures.orEmpty()
                .filter { it.ready }
                .map {
                    VerifyDriveClosure(
                        batchId = it.batchId,
                        driveLabel = it.driveLabel,
                        batchLabel = it.batchLabel,
                        totalCount = it.totalCount,
                        approvedCount = it.approvedCount,
                        rejectedCount = it.rejectedCount,
                        pendingCount = it.pendingCount,
                        videoCount = it.videoCount,
                        approvedVideos = it.approvedVideos,
                        rejectedVideos = it.rejectedVideos,
                        pendingVideos = it.pendingVideos,
                        shedCount = it.shedCount,
                        ready = it.ready,
                    )
                },
            closingBatchId = flags.closingBatchId,
            closeErrorBatchId = flags.closeErrorBatchId,
            closeErrorMessage = flags.closeErrorMessage,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyQueueUiState())

    init {
        viewModelScope.launch {
            observedResource.collect { resource ->
                clearStaleLocationFilters(resource.data ?: return@collect)
            }
        }
        refresh()
    }

    fun onEvent(event: VerifyQueueEvent) {
        when (event) {
            is VerifyQueueEvent.SelectCategory -> {
                Unit
            }
            is VerifyQueueEvent.SelectPark -> {
                _selectedParkId.value = event.parkId
                _selectedShedId.value = null
                refresh()
            }
            is VerifyQueueEvent.SelectShed -> {
                _selectedShedId.value = event.shedId
                refresh()
            }
            is VerifyQueueEvent.SelectModule -> {
                _selectedCategory.value = categoryForModule(event.module)
                _selectedParkId.value = null
                _selectedShedId.value = null
                refresh()
            }
            is VerifyQueueEvent.OpenItem -> Unit // navigation — handled by the nav host.
            VerifyQueueEvent.Refresh -> refresh()
            VerifyQueueEvent.LoadMore -> loadMore()
            is VerifyQueueEvent.CloseDrive -> closeDrive(event.batchId)
        }
    }

    private fun refresh() = viewModelScope.launch {
        _isLoadingMore.value = false
        _isRefreshing.value = true
        try {
            val category = _selectedCategory.value ?: return@launch
            AnalyticsFunnels.trackVerifyQueueOpened(analytics, category)
            val result = if (isActionQueue) {
                repo.refreshActionQueue(
                    category = category,
                    parkId = _selectedParkId.value,
                    shedId = _selectedShedId.value,
                    limit = VERIFY_QUEUE_PAGE_SIZE,
                )
            } else {
                repo.refreshQueue(
                    category = category,
                    parkId = _selectedParkId.value,
                    shedId = _selectedShedId.value,
                    limit = VERIFY_QUEUE_PAGE_SIZE,
                )
            }
            _isOffline.value = result.isFailure
        } finally {
            _isRefreshing.value = false
        }
    }

    private fun loadMore() = viewModelScope.launch {
        val category = _selectedCategory.value ?: return@launch
        val cursor = observedResource.value.data?.nextCursor ?: return@launch
        _isLoadingMore.value = true
        val result = repo.appendQueue(
            cursor = cursor,
            category = category,
            parkId = _selectedParkId.value,
            shedId = _selectedShedId.value,
            limit = VERIFY_QUEUE_PAGE_SIZE,
        )
        _isOffline.value = result.isFailure
        _isLoadingMore.value = false
    }

    private fun closeDrive(batchId: String) = viewModelScope.launch {
        val batchId = batchId.takeIf { it.isNotBlank() } ?: return@launch
        _closingBatchId.value = batchId
        _closeErrorBatchId.value = null
        _closeErrorMessage.value = null
        when (val result = syncRepo.enqueueVerificationBatchClose(batchId)) {
            is AppResult.Ok -> {
                val error = waitForCloseSync(result.value)
                _closingBatchId.value = null
                if (error == null) {
                    repo.markVaccinationBatchClosedLocally(
                        batchId = batchId,
                        category = VACCINATION_CATEGORY,
                        parkId = _selectedParkId.value,
                        shedId = _selectedShedId.value,
                        limit = VERIFY_QUEUE_PAGE_SIZE,
                    )
                    _isOffline.value = false
                    refresh()
                } else {
                    _closeErrorBatchId.value = batchId
                    _closeErrorMessage.value = error
                }
            }
            is AppResult.Err -> {
                _closingBatchId.value = null
                _closeErrorBatchId.value = batchId
                _closeErrorMessage.value = result.message
            }
        }
    }

    private suspend fun waitForCloseSync(outboxItemId: String): String? {
        repeat(30) {
            syncRepo.triggerDrain()
            delay(250)
            when (val item = syncRepo.findOutboxItem(outboxItemId)) {
                is AppResult.Ok -> {
                    val row = item.value
                    when {
                        row?.status == SyncItemStatus.SUCCEEDED -> return null
                        row?.status == SyncItemStatus.FAILED && (row.conflict || row.isDeadLetter) ->
                            return row.lastError ?: "Backend rejected the close action."
                    }
                }
                is AppResult.Err -> Unit
            }
            delay(320)
        }
        return "Close saved locally; waiting for backend sync."
    }

    private fun clearStaleLocationFilters(data: VerificationQueueResponseDto) {
        val selectedPark = _selectedParkId.value
        if (selectedPark != null && data.filterOptions.parks.orEmpty().none { it.id == selectedPark }) {
            _selectedParkId.value = null
            _selectedShedId.value = null
            refresh()
            return
        }
        val selectedShed = _selectedShedId.value
        if (selectedShed != null && data.filterOptions.sheds.orEmpty().none { it.id == selectedShed }) {
            _selectedShedId.value = null
            refresh()
        }
    }

    /** `value = null` ("All") always leads, followed by every distinct category the backend has
     *  returned. `label = null` on the "All" entry tells the Screen to substitute its own
     *  localized chrome string; every other label is the raw backend category key, humanized
     *  client-side only as a display fallback until the backend ships a proper display label
     *  per registry entry (verification-module-design.md §2.3). */
    private fun categoryOptions(items: List<VerificationQueueItem>, selected: String?): List<VerifyCategoryOption> {
        val seen = items.map { it.category }.filter { it.isNotBlank() }.distinct().sorted()
        if (seen.isEmpty() && selected == null) return emptyList()
        val options = mutableListOf(VerifyCategoryOption(value = null, label = null))
        seen.forEach { options += VerifyCategoryOption(value = it, label = humanizeCategory(it)) }
        if (selected != null && seen.none { it == selected }) {
            options += VerifyCategoryOption(value = selected, label = humanizeCategory(selected))
        }
        return options
    }

    private fun VerifyQueueViewModel.formatCapturedAtIST(raw: String): String {
        if (raw.isBlank()) return ""
        return runCatching {
            val instant = java.time.Instant.parse(raw)
            val locale = java.util.Locale.getDefault()
            java.time.format.DateTimeFormatter
                .ofLocalizedDateTime(
                    java.time.format.FormatStyle.MEDIUM,
                    java.time.format.FormatStyle.SHORT
                )
                .withLocale(locale)
                .withZone(java.time.ZoneId.of("Asia/Kolkata"))
                .format(instant)
        }.getOrDefault(raw)
    }

    private fun VerificationQueueItem.toRow(): VerificationQueueRow {
        // Backend-owned display labels: never render raw UUIDs. Use labels when available; the
        // category-humanized name is the last-resort fallback so a non-vaccination row never
        // mislabels as "Vaccination proof".
        // Deduplicate shed name if shedLabel is already part of subjectLabel (e.g., "Godel 1 · 5 goats" + "Godel 1"
        // would render as "Godel 1 · 5 goats · Godel 1"; only use subjectLabel if shedLabel is already its prefix).
        val title = listOfNotNull(
            subjectLabel,
            shedLabel?.takeUnless { shed -> subjectLabel?.startsWith(shed) == true }
        ).joinToString(" · ").ifBlank { humanizeCategory(category) }
        val subtitle = listOfNotNull(parkLabel, operatorName, capturedAt.takeIf { it.isNotBlank() }?.let { formatCapturedAtIST(it) })
            .joinToString(" · ")
        val mediaCount = media.size
        val firstMedia = media.firstOrNull()
        val scopeType = weighingScopeType()
        return VerificationQueueRow(
            id = itemId,
            category = category,
            categoryLabel = humanizeCategory(category),
            title = title,
            subtitle = subtitle,
            scopeType = scopeType,
            shedLabel = shedLabel ?: subjectLabel.orEmpty(),
            animalLabel = when (scopeType) {
                VerifyScopeType.INDIVIDUAL -> firstMedia?.label?.takeIf { it.isNotBlank() } ?: subjectLabel.orEmpty()
                VerifyScopeType.LUMP_SUM -> ""
                VerifyScopeType.OTHER -> ""
            },
            weightLabel = firstMedia?.answer.orEmpty(),
            mediaCountLabel = when (mediaCount) {
                0 -> ""
                1 -> "1 video"
                else -> "$mediaCount videos"
            },
            parkLabel = parkLabel.orEmpty(),
            operatorLabel = operatorName.orEmpty(),
            capturedAtLabel = capturedAt,
            statusTone = statusTone(status),
        )
    }

    private fun VerificationQueueItem.weighingScopeType(): VerifyScopeType {
        if (category != WEIGHING_CATEGORY) return VerifyScopeType.OTHER
        val refType = source.refType.lowercase()
        val label = listOfNotNull(subjectLabel, shedLabel, media.firstOrNull()?.label)
            .joinToString(" ")
            .lowercase()
        return when {
            refType.contains("shed") || refType.contains("lump") || label.contains("lump") || label.contains("shed weight") ->
                VerifyScopeType.LUMP_SUM
            else ->
                VerifyScopeType.INDIVIDUAL
        }
    }
}

private const val MODULE_ARG = "module"
private const val CATEGORY_ARG = "category"
private const val VACCINATION_CATEGORY = "vaccination_proof"
private const val WEIGHING_CATEGORY = "weighing_proof"
private fun categoryForModule(module: VerifyModuleTab): String = when (module) {
    VerifyModuleTab.VACCINATION -> VACCINATION_CATEGORY
    VerifyModuleTab.WEIGHING -> WEIGHING_CATEGORY
}

/**
 * Maps the nav's MODULE key onto the verification CATEGORY. The two vocabularies differ, and an
 * unknown key returns null rather than a guess -- see [_selectedCategory].
 */
private fun categoryForModuleKey(moduleKey: String?): String? =
    when (moduleKey?.trim()?.lowercase()) {
        "vaccination" -> VACCINATION_CATEGORY
        "weighing" -> WEIGHING_CATEGORY
        else -> null
    }

/** Which module chrome (if any) this client renders for a category. Null = generic rows only. */
private fun moduleForCategory(category: String?): VerifyModuleTab? = when (category) {
    VACCINATION_CATEGORY -> VerifyModuleTab.VACCINATION
    WEIGHING_CATEGORY -> VerifyModuleTab.WEIGHING
    else -> null
}

private fun locationOptions(
    allLabel: String,
    raw: List<Pair<String, String>>,
    selected: String?,
): List<VerifyLocationFilterOption> {
    if (raw.isEmpty() && selected == null) return emptyList()
    val options = mutableListOf(VerifyLocationFilterOption(value = null, label = allLabel))
    raw.filter { (id, _) -> id.isNotBlank() }
        .distinctBy { (id, _) -> id }
        .forEach { (id, label) -> options += VerifyLocationFilterOption(value = id, label = label.ifBlank { id }) }
    return options
}

internal fun humanizeCategory(category: String): String =
    category.replace('_', ' ').replaceFirstChar { it.uppercase() }

internal fun statusTone(status: String): VerifyTone = when (status) {
    VerificationStatus.APPROVED -> VerifyTone.APPROVED
    VerificationStatus.REJECTED -> VerifyTone.REJECTED
    else -> VerifyTone.PENDING
}
