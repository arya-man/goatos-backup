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
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsFunnels
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.isConnectivityFailure
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.feature.verify.VerificationQueueRow
import sg.mesha.goatos.feature.verify.VerifyDriveClosure
import sg.mesha.goatos.feature.verify.VerifyCategoryOption
import sg.mesha.goatos.feature.verify.VerifyLocationFilterOption
import sg.mesha.goatos.feature.verify.VerifyModuleTab
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueUiState
import sg.mesha.goatos.feature.verify.VerifyScopeType
import sg.mesha.goatos.feature.verify.VerifyStatusOption
import sg.mesha.goatos.feature.verify.VerifyTone
import java.time.LocalDate
import java.time.ZoneId
import javax.inject.Inject

private const val VERIFY_QUEUE_PAGE_SIZE = 20

private data class VerifyQueueFlags(
    val isRefreshing: Boolean = false,
    val isOffline: Boolean = false,
    val isLoadingMore: Boolean = false,
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
    val hasLoadedOnce: Boolean = false,
)

private data class VerifyCloseFlags(
    val closingBatchId: String? = null,
    val closeErrorBatchId: String? = null,
    val closeErrorMessage: String? = null,
)

private data class VerifyQueueScope(
    val category: String?,
    val status: String,
    val businessDate: String,
    val missedOnly: Boolean,
    val parkId: String?,
    val shedId: String?,
)

/**
 * The verifier-only workspace's reusable queue state holder (context/architecture/
 * verifier-app-and-flow.md). Offline-first (docs/decisions/android-offline-first.md): [state]
 * is fed by [VerificationRepository.observeQueue], a cache-first Room Flow re-subscribed (via
 * [flatMapLatest]) whenever the category filter changes; [refresh] drives the network side of
 * stale-while-revalidate and [loadMore] appends the next ~20-row keyset page into the SAME
 * Room-backed scope (never an in-memory-only accumulation — mobile-guard rule). Module identity
 * and page options are rendered from backend registry metadata; Android sends only the selected
 * page's raw category key back as a disjoint queue filter.
 */
@HiltViewModel
class VerifyQueueViewModel @Inject constructor(
    private val repo: VerificationRepository,
    private val syncRepo: SyncRepository,
    private val analytics: AnalyticsPort,
    savedStateHandle: SavedStateHandle,
    private val crashReporter: CrashReporter = sg.mesha.goatos.core.analytics.NoopCrashReporter(),
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
    private val _selectedStatus = MutableStateFlow(VerificationStatus.PENDING)
    private val _selectedBusinessDate = MutableStateFlow(LocalDate.now(ZoneId.of("Asia/Kolkata")).toString())
    private val _missedOnly = MutableStateFlow(false)
    private val _selectedParkId = MutableStateFlow<String?>(null)
    private val _selectedShedId = MutableStateFlow<String?>(null)
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    // Set the first time a fetch COMPLETES, whatever it returned. lastSyncedAt cannot serve this
    // -- an empty result never sets it, so the screen wedged on a spinner over a blank list --
    // and isRefreshing flips on every later refresh, which yanked already-drawn content away.
    private val _hasLoadedOnce = MutableStateFlow(false)

    private val _isLoadingMore = MutableStateFlow(false)
    private val _closingBatchId = MutableStateFlow<String?>(null)
    private val _closeErrorBatchId = MutableStateFlow<String?>(null)
    private val _closeErrorMessage = MutableStateFlow<String?>(null)

    private val selectedScope: StateFlow<VerifyQueueScope> = combine(
        combine(_selectedCategory, _selectedStatus, _selectedBusinessDate, _missedOnly) { category, status, date, missed ->
            arrayOf(category, status, date, missed.toString())
        },
        _selectedParkId,
        _selectedShedId,
    ) { primary, parkId, shedId ->
        VerifyQueueScope(
            category = primary[0],
            status = primary[1].orEmpty(),
            businessDate = primary[2].orEmpty(),
            missedOnly = primary[3].toBoolean(),
            parkId = parkId,
            shedId = shedId,
        )
    }.stateIn(
        viewModelScope,
        SharingStarted.WhileSubscribed(5_000),
        VerifyQueueScope(
            category = _selectedCategory.value,
            status = _selectedStatus.value,
            businessDate = _selectedBusinessDate.value,
            missedOnly = false,
            parkId = null,
            shedId = null,
        ),
    )

    // flatMapLatest cancels the previous filter scope's Room collection immediately.
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedResource: StateFlow<Resource<VerificationQueueResponseDto>> =
        selectedScope.flatMapLatest { scope ->
            if (scope.category == null) {
                // The route named a module or category nothing here can serve. Serving NOTHING is
                // deliberate: the old fallback showed a Counts or Feed verifier VACCINATION proofs,
                // which is a worse failure than an empty queue. `isUnsupportedModule` says so.
                flowOf(Resource(data = null))
            } else if (isActionQueue) {
                repo.observeActionQueue(category = scope.category, parkId = scope.parkId, shedId = scope.shedId, limit = VERIFY_QUEUE_PAGE_SIZE)
            } else {
                repo.observeQueue(
                    category = scope.category,
                    status = scope.status,
                    businessDate = scope.businessDate.takeUnless { scope.missedOnly },
                    missed = scope.missedOnly,
                    parkId = scope.parkId,
                    shedId = scope.shedId,
                    limit = VERIFY_QUEUE_PAGE_SIZE,
                )
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
        _hasLoadedOnce,
    ) { values ->
        VerifyQueueFlags(
            isRefreshing = values[0] as Boolean,
            isOffline = values[1] as Boolean,
            isLoadingMore = values[2] as Boolean,
            closingBatchId = (values[3] as VerifyCloseFlags).closingBatchId,
            closeErrorBatchId = (values[3] as VerifyCloseFlags).closeErrorBatchId,
            closeErrorMessage = (values[3] as VerifyCloseFlags).closeErrorMessage,
            hasLoadedOnce = values[4] as Boolean,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyQueueFlags())

    val state: StateFlow<VerifyQueueUiState> = combine(
        observedResource,
        selectedScope,
        flags,
    ) { resource, scope, flags ->
        val items = resource.data?.items.orEmpty()
        val filterOptions = resource.data?.filterOptions
        VerifyQueueUiState(
            // ONE CARD PER SHED: the backend emits one verification_item per goat, so a naive
            // items.map here regresses a 40-animal shed into 40 rows. Group first -- [toShedRow]
            // folds each group's per-animal counts into one row's progress copy, and a legacy
            // bundled item (sharing no submissionId) still yields its own singleton card.
            rows = items
                .groupBy { it.verificationGroupKey() }
                .map { (groupKey, groupItems) -> groupItems.toShedRow(groupKey) },
            moduleKey = filterOptions?.moduleKey.orEmpty(),
            moduleLabel = filterOptions?.moduleLabel.orEmpty(),
            // Null for a category this client has no dedicated chrome for (counts, feed). The
            // rows still render generically; only the module-specific grouping stands down.
            selectedModule = moduleForCategory(scope.category),
            // The route named a module or category nothing here can serve. Say so instead of
            // rendering another module's queue or a bare "all caught up".
            isUnsupportedModule = scope.category == null,
            isActionQueue = isActionQueue,
            categoryOptions = filterOptions?.pages.orEmpty().map { page ->
                VerifyCategoryOption(value = page.category, label = page.label)
            },
            selectedCategory = scope.category,
            statusOptions = filterOptions?.statuses.orEmpty().map { VerifyStatusOption(value = it.status, label = it.label) },
            selectedStatus = scope.status,
            selectedBusinessDate = filterOptions?.selectedBusinessDate ?: scope.businessDate,
            businessTimezone = filterOptions?.businessTimezone ?: "Asia/Kolkata",
            missedOnly = scope.missedOnly,
            hasMissed = filterOptions?.hasMissed ?: false,
            parkOptions = locationOptions("All parks", resource.data?.filterOptions?.parks.orEmpty().map { it.id to it.label }, scope.parkId),
            selectedParkId = scope.parkId,
            shedOptions = locationOptions("All sheds", resource.data?.filterOptions?.sheds.orEmpty().map { it.id to it.label }, scope.shedId),
            selectedShedId = scope.shedId,
            isRefreshing = flags.isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = flags.isOffline,
            hasMore = !isActionQueue && resource.data?.nextCursor != null,
            isLoadingMore = flags.isLoadingMore,
            hasLoadedOnce = flags.hasLoadedOnce,
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
                // The no-op guard comes FIRST: re-selecting the category already showing must
                // not emit a filter event, or the funnel counts taps that changed nothing.
                if (event.category == _selectedCategory.value) return
                AnalyticsFunnels.trackVerifyQueueFilterApplied(
                    analytics,
                    dimension = "category",
                    action = if (event.category != null) "set" else "cleared",
                )
                _selectedCategory.value = event.category
                _selectedParkId.value = null
                _selectedShedId.value = null
                // Reset HERE, at the scope change, not inside refresh(). The old scope's
                // marker stayed true until refresh() ran its own reset, leaving a window
                // between this scope change and refresh() completing where a recomposition
                // could show the OLD scope's confident answer (rows or "Queue clear") as if
                // it belonged to the new scope -- STALE SCOPE, see
                // VerifyQueueLoadSequenceTest's "switching scope resets the loaded marker".
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.SelectPark -> {
                _selectedParkId.value = event.parkId
                _selectedShedId.value = null
                AnalyticsFunnels.trackVerifyQueueFilterApplied(
                    analytics,
                    dimension = "park",
                    action = if (event.parkId != null) "set" else "cleared",
                )
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.SelectShed -> {
                _selectedShedId.value = event.shedId
                AnalyticsFunnels.trackVerifyQueueFilterApplied(
                    analytics,
                    dimension = "shed",
                    action = if (event.shedId != null) "set" else "cleared",
                )
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.SelectStatus -> {
                if (event.status == _selectedStatus.value && !_missedOnly.value) return
                _selectedStatus.value = event.status
                _missedOnly.value = false
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.SelectBusinessDate -> {
                if (event.businessDate.isBlank()) return
                _selectedBusinessDate.value = event.businessDate
                _missedOnly.value = false
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.SelectModule -> {
                _selectedCategory.value = categoryForModule(event.module)
                _selectedParkId.value = null
                _selectedShedId.value = null
                AnalyticsFunnels.trackVerifyQueueFilterApplied(analytics, dimension = "module", action = "set")
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            VerifyQueueEvent.ToggleMissed -> {
                _missedOnly.value = !_missedOnly.value
                if (_missedOnly.value) _selectedStatus.value = VerificationStatus.PENDING
                // See SelectCategory above: reset at the scope change, not inside refresh().
                _hasLoadedOnce.value = false
                refresh()
            }
            is VerifyQueueEvent.OpenItem -> Unit // navigation — handled by the nav host.
            VerifyQueueEvent.Refresh -> refresh()
            VerifyQueueEvent.LoadMore -> loadMore()
            is VerifyQueueEvent.CloseDrive -> closeDrive(event.batchId)
            is VerifyQueueEvent.ScrollSummary ->
                AnalyticsFunnels.trackVerifyQueueScrollSummary(analytics, event.maxScrollIndex, event.rowCount)
        }
    }

    private fun refresh() = viewModelScope.launch {
        _isLoadingMore.value = false
        _isRefreshing.value = true
        // NOTE: hasLoadedOnce is reset at the SCOPE CHANGE call sites (SelectCategory/
        // SelectPark/SelectShed/SelectStatus/SelectBusinessDate/SelectModule/ToggleMissed/
        // clearStaleLocationFilters), not here. A same-scope call (pull-to-refresh, Refresh
        // event) must never blank a legitimately-loaded/empty queue while it re-reads.
        try {
            val scope = currentScope()
            val category = scope.category ?: return@launch
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
                    status = scope.status,
                    businessDate = scope.businessDate.takeUnless { scope.missedOnly },
                    missed = scope.missedOnly,
                    parkId = scope.parkId,
                    shedId = scope.shedId,
                    limit = VERIFY_QUEUE_PAGE_SIZE,
                )
            }
            _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
        } catch (t: Throwable) {
                    // A cancelled scope is not a failure. Catching Throwable without letting
                    // CancellationException through breaks structured concurrency: rotating the
                    // screen or navigating away would be reported as an error and would publish
                    // state after the scope had already been cancelled.
                    if (t is kotlinx.coroutines.CancellationException) throw t
            // A repository throw must not escape viewModelScope.launch -- an uncaught
            // exception here takes the whole app down, not just this screen. Same defect
            // shape already fixed in SessionViewModel's dev-session bring-up (see the catch
            // there): record it and resolve to an honest offline/error state instead of
            // propagating. hasLoadedOnce still flips in `finally` below, so the screen
            // reaches a real (if degraded) state rather than wedging on the skeleton.
            runCatching { crashReporter.recordException(t, "verify queue refresh failed") }
            _isOffline.value = true
        } finally {
            _isRefreshing.value = false
            // In FINALLY, not after the result: a throw on the way here would leave this false
            // forever and wedge the screen on a spinner over a blank list.
            _hasLoadedOnce.value = true
        }
    }

    private fun loadMore() = viewModelScope.launch {
        val scope = currentScope()
        val category = scope.category ?: return@launch
        val cursor = observedResource.value.data?.nextCursor ?: return@launch
        _isLoadingMore.value = true
        val result = repo.appendQueue(
            cursor = cursor,
            category = category,
            status = scope.status,
            businessDate = scope.businessDate.takeUnless { scope.missedOnly },
            missed = scope.missedOnly,
            parkId = scope.parkId,
            shedId = scope.shedId,
            limit = VERIFY_QUEUE_PAGE_SIZE,
        )
        _isOffline.value = result.exceptionOrNull().isConnectivityFailure()
        _isLoadingMore.value = false
        AnalyticsFunnels.trackVerifyQueueLoadMore(analytics, category, observedResource.value.data?.items?.size ?: 0)
    }

    private fun currentScope() = VerifyQueueScope(
        category = _selectedCategory.value,
        status = _selectedStatus.value,
        businessDate = _selectedBusinessDate.value,
        missedOnly = _missedOnly.value,
        parkId = _selectedParkId.value,
        shedId = _selectedShedId.value,
    )

    private fun closeDrive(batchId: String) = viewModelScope.launch {
        val batchId = batchId.takeIf { it.isNotBlank() } ?: return@launch
        _closingBatchId.value = batchId
        _closeErrorBatchId.value = null
        _closeErrorMessage.value = null
        AnalyticsFunnels.trackVerifyDriveCloseAttempted(analytics, batchId)
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
                    AnalyticsFunnels.trackVerifyDriveCloseSucceeded(analytics, batchId)
                } else {
                    _closeErrorBatchId.value = batchId
                    _closeErrorMessage.value = error
                    AnalyticsFunnels.trackVerifyDriveCloseFailed(analytics, batchId, error)
                }
            }
            is AppResult.Err -> {
                _closingBatchId.value = null
                _closeErrorBatchId.value = batchId
                _closeErrorMessage.value = result.message
                result.cause?.let { error ->
                    runCatching { crashReporter.recordException(error, "verification drive close enqueue failed") }
                }
                AnalyticsFunnels.trackVerifyDriveCloseFailed(analytics, batchId, result.message)
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
            // This is a scope change (the selected park no longer exists in the backend's
            // options) -- reset at the change, same as the onEvent scope-change handlers above.
            _hasLoadedOnce.value = false
            refresh()
            return
        }
        val selectedShed = _selectedShedId.value
        if (selectedShed != null && data.filterOptions.sheds.orEmpty().none { it.id == selectedShed }) {
            _selectedShedId.value = null
            _hasLoadedOnce.value = false
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
        }.getOrElse { error ->
            // Falls back to the raw backend timestamp so the row still renders something — but a
            // malformed `capturedAt` from the backend must not vanish silently either.
            runCatching { crashReporter.recordException(error, "verification capturedAt format failed") }
            raw
        }
    }

    /**
     * One card per GROUP (shed submission), not per animal — see [verificationGroupKey]. A
     * multi-item group (the new per-goat shape) renders shed-level copy with a live
     * "N goats · M to review" progress line; a single-item group (legacy bundled item, or any
     * item with no siblings) renders exactly as the old per-item card did, so a queue that has
     * not migrated to the new shape yet is visually unchanged.
     */
    private fun List<VerificationQueueItem>.toShedRow(groupKey: String): VerificationQueueRow {
        val representative = first()
        if (size == 1) return representative.toSingleItemRow(groupKey)

        val pendingCount = count { it.status == VerificationStatus.PENDING }
        // Every item in a group shares one shed/park/operator/category — the shed submission's
        // own metadata, not any one animal's.
        val shedLabel = representative.shedLabel?.takeIf { it.isNotBlank() }
        val title = listOfNotNull(shedLabel, "$size goats · $pendingCount to review")
            .joinToString(" · ")
            .ifBlank { humanizeCategory(representative.category) }
        val subtitle = listOfNotNull(
            representative.parkLabel,
            representative.operatorName,
            representative.capturedAt.takeIf { it.isNotBlank() }?.let { formatCapturedAtIST(it) },
        ).joinToString(" · ")
        val mediaCount = sumOf { it.media.size }
        val groupStatusTone = when {
            all { it.status == VerificationStatus.APPROVED } -> VerifyTone.APPROVED
            any { it.status == VerificationStatus.REJECTED } -> VerifyTone.REJECTED
            else -> VerifyTone.PENDING
        }
        return VerificationQueueRow(
            id = groupKey,
            category = representative.category,
            categoryLabel = humanizeCategory(representative.category),
            title = title,
            subtitle = subtitle,
            scopeType = VerifyScopeType.INDIVIDUAL,
            shedLabel = shedLabel.orEmpty(),
            animalLabel = "",
            weightLabel = "",
            mediaCountLabel = when (mediaCount) {
                0 -> ""
                1 -> "1 video"
                else -> "$mediaCount videos"
            },
            parkLabel = representative.parkLabel.orEmpty(),
            operatorLabel = representative.operatorName.orEmpty(),
            capturedAtLabel = representative.capturedAt,
            statusTone = groupStatusTone,
        )
    }

    private fun VerificationQueueItem.toSingleItemRow(groupKey: String): VerificationQueueRow {
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
            id = groupKey,
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

/**
 * The shed-level grouping key shared by [VerifyQueueViewModel] (one card per shed) and
 * [VerifyDetailViewModel] (one verdict per animal inside that card).
 *
 * The backend emits one verification_item PER GOAT (`source_ref_type=vaccination_goat`), with
 * every per-goat item produced from one shed submission sharing one `source.submissionId`. A
 * legacy bundled item (`ref_type=sop_submission`, several clips already under one verdict) sets
 * no shared submissionId across other items, so it falls back to its OWN item id — a group of
 * exactly itself, which is what "render/judge as a group-of-one" means for that shape.
 */
/**
 * The shed's work, across redos — NOT one submission.
 *
 * A rejected animal is re-scanned and submitted again, and that redo is a NEW submission. Keying
 * the group on `submissionId` therefore split one shed across two cards the moment anything was
 * sent back: the original card kept its rejected animal forever and the redo appeared as a
 * separate one-goat card, so the verifier could never see the shed as a whole.
 *
 * `taskId` + `shedId` are stable across redos (both survive a new submission), so the redone
 * animal lands back on the shed it belongs to. `submissionId` remains the fallback for items that
 * carry no task/shed, and `itemId` the last resort — a group of exactly itself, which renders and
 * is judged exactly as a single item always was.
 */
internal fun VerificationQueueItem.verificationGroupKey(): String {
    val taskID = source.taskId?.takeIf { it.isNotBlank() }
    val shedID = shedId?.takeIf { it.isNotBlank() }
    if (taskID != null && shedID != null) {
        return "task:$taskID|shed:$shedID"
    }
    return source.submissionId?.takeIf { it.isNotBlank() } ?: itemId
}

internal fun statusTone(status: String): VerifyTone = when (status) {
    VerificationStatus.APPROVED -> VerifyTone.APPROVED
    VerificationStatus.REJECTED -> VerifyTone.REJECTED
    else -> VerifyTone.PENDING
}
