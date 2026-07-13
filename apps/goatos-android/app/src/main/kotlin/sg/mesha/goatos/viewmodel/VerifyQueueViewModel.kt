package sg.mesha.goatos.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
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
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.VerificationRepository
import sg.mesha.goatos.core.network.dto.VerificationQueueItem
import sg.mesha.goatos.core.network.dto.VerificationQueueResponseDto
import sg.mesha.goatos.core.network.dto.VerificationStatus
import sg.mesha.goatos.feature.verify.VerificationQueueRow
import sg.mesha.goatos.feature.verify.VerifyCategoryOption
import sg.mesha.goatos.feature.verify.VerifyQueueEvent
import sg.mesha.goatos.feature.verify.VerifyQueueUiState
import sg.mesha.goatos.feature.verify.VerifyTone
import javax.inject.Inject

private const val VERIFY_QUEUE_PAGE_SIZE = 20

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
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val _selectedCategory = MutableStateFlow<String?>(null)
    private val _isRefreshing = MutableStateFlow(false)
    private val _isOffline = MutableStateFlow(false)
    private val _isLoadingMore = MutableStateFlow(false)

    // flatMapLatest cancels the previous category's Room collection and starts a fresh one the
    // moment _selectedCategory changes (same pattern as CalendarViewModel's _selectedDay).
    @OptIn(ExperimentalCoroutinesApi::class)
    private val observedResource: StateFlow<Resource<VerificationQueueResponseDto>> =
        _selectedCategory.flatMapLatest { category ->
            repo.observeQueue(category = category, limit = VERIFY_QUEUE_PAGE_SIZE)
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), Resource(data = null))

    val state: StateFlow<VerifyQueueUiState> = combine(
        observedResource,
        _selectedCategory,
        _isRefreshing,
        _isOffline,
        _isLoadingMore,
    ) { resource, category, isRefreshing, isOffline, isLoadingMore ->
        val items = resource.data?.items.orEmpty()
        VerifyQueueUiState(
            rows = items.map { it.toRow() },
            categoryOptions = categoryOptions(items, category),
            selectedCategory = category,
            isRefreshing = isRefreshing,
            lastSyncedAt = resource.lastSyncedAt,
            isOffline = isOffline,
            hasMore = resource.data?.nextCursor != null,
            isLoadingMore = isLoadingMore,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), VerifyQueueUiState())

    init {
        refresh()
    }

    fun onEvent(event: VerifyQueueEvent) {
        when (event) {
            is VerifyQueueEvent.SelectCategory -> {
                _selectedCategory.value = event.category
                refresh()
            }
            is VerifyQueueEvent.OpenItem -> Unit // navigation — handled by the nav host.
            VerifyQueueEvent.Refresh -> refresh()
            VerifyQueueEvent.LoadMore -> loadMore()
        }
    }

    private fun refresh() = viewModelScope.launch {
        _isLoadingMore.value = false
        _isRefreshing.value = true
        val category = _selectedCategory.value
        AnalyticsFunnels.trackVerifyQueueOpened(analytics, category)
        val result = repo.refreshQueue(category = category, limit = VERIFY_QUEUE_PAGE_SIZE)
        _isOffline.value = result.isFailure
        _isRefreshing.value = false
    }

    private fun loadMore() = viewModelScope.launch {
        val cursor = observedResource.value.data?.nextCursor ?: return@launch
        _isLoadingMore.value = true
        val result = repo.appendQueue(cursor = cursor, category = _selectedCategory.value, limit = VERIFY_QUEUE_PAGE_SIZE)
        _isOffline.value = result.isFailure
        _isLoadingMore.value = false
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

    private fun VerificationQueueItem.toRow(): VerificationQueueRow {
        // Backend-owned display labels: never render raw UUIDs. Use labels when available.
        val title = shedLabel ?: shedId ?: source.module.ifBlank { category }
        val subtitle = listOfNotNull(parkLabel ?: parkId, operatorName ?: operatorId, capturedAt)
            .joinToString(" · ")
        return VerificationQueueRow(
            id = itemId,
            category = category,
            categoryLabel = humanizeCategory(category),
            title = title,
            subtitle = subtitle,
            statusTone = statusTone(status),
        )
    }
}

internal fun humanizeCategory(category: String): String =
    category.replace('_', ' ').replaceFirstChar { it.uppercase() }

internal fun statusTone(status: String): VerifyTone = when (status) {
    VerificationStatus.APPROVED -> VerifyTone.APPROVED
    VerificationStatus.REJECTED -> VerifyTone.REJECTED
    else -> VerifyTone.PENDING
}
