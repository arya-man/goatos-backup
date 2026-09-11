package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationAnimalDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidateDto
import sg.mesha.goatos.core.network.dto.SaleTaggingDealDto
import sg.mesha.goatos.feature.vendors.SaleTaggingBasketAnimalUi
import sg.mesha.goatos.feature.vendors.SaleTaggingCardUi
import sg.mesha.goatos.feature.vendors.SaleTaggingDoneAnimalUi
import sg.mesha.goatos.feature.vendors.SaleTaggingEvent
import sg.mesha.goatos.feature.vendors.SaleTaggingListEvent
import sg.mesha.goatos.feature.vendors.SaleTaggingListUiState
import sg.mesha.goatos.feature.vendors.SaleTaggingMatchUi
import sg.mesha.goatos.feature.vendors.SaleTaggingRules
import sg.mesha.goatos.feature.vendors.SaleTaggingUiState
import sg.mesha.goatos.rfid.ScanSource
import sg.mesha.goatos.ui.Routes
import java.util.UUID
import javax.inject.Inject

// The park head's TAG-ONLY Sales module (maintainer decision 2026-09-11): the sales at their park
// still owed animals, and tagging those animals from the pen. Nothing else of Sales is reachable
// from these two state holders: no ledger row, no buyer, no money, no pipeline.

/** The queue (L0, `/sale-tagging`): Room-first over the cached first page, further pages live. */
@HiltViewModel
class SaleTaggingListViewModel @Inject constructor(
    private val repository: SalesRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val title: String = "",
        val refreshing: Boolean = false,
        val lastSyncedAt: Long? = null,
        /** Pages after the cached first one, appended as the person scrolls. */
        val extra: List<SaleTaggingDealDto> = emptyList(),
        /** The cursor after the LAST page held (cached or extra); null once there is no more. */
        val nextCursor: String? = null,
        val extraLoaded: Boolean = false,
        val loadingMore: Boolean = false,
        val firstRefreshDone: Boolean = false,
        val failed: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    fun bind(title: String) {
        if (local.value.title == title) return
        local.update { it.copy(title = title) }
        analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_QUEUE_VIEWED)
    }

    val state: StateFlow<SaleTaggingListUiState> = combine(local, repository.observeTaggingQueue()) { l, cached ->
        val first = cached?.deals.orEmpty()
        val rows = (first + l.extra).distinctBy { it.salesDealId }.map { it.toCardUi() }
        val cursor = if (l.extraLoaded) l.nextCursor else cached?.nextCursor
        SaleTaggingListUiState(
            title = l.title,
            isRefreshing = l.refreshing,
            lastSyncedAt = l.lastSyncedAt,
            rows = rows,
            hasMore = !cursor.isNullOrBlank(),
            loadingMore = l.loadingMore,
            emptyMessage = when {
                rows.isNotEmpty() -> null
                l.failed && cached == null -> EMPTY_FAILED
                cached != null || l.firstRefreshDone -> EMPTY_NONE
                else -> null
            },
            isErrorEmpty = l.failed && cached == null,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleTaggingListUiState())

    fun onEvent(event: SaleTaggingListEvent) {
        when (event) {
            SaleTaggingListEvent.Refresh -> refresh()
            SaleTaggingListEvent.LoadMore -> loadMore()
            is SaleTaggingListEvent.OpenSale -> analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_SALE_OPENED)
        }
    }

    private fun refresh() {
        if (local.value.refreshing) return
        viewModelScope.launch {
            local.update { it.copy(refreshing = true) }
            when (val result = repository.refreshTaggingQueue()) {
                is AppResult.Ok -> local.update {
                    it.copy(refreshing = false, lastSyncedAt = System.currentTimeMillis(), extra = emptyList(), extraLoaded = false, nextCursor = null, firstRefreshDone = true, failed = false)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale tagging queue refresh failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(refreshing = false, firstRefreshDone = true, failed = true) }
                }
            }
        }
    }

    private fun loadMore() {
        val current = state.value
        if (!current.hasMore || local.value.loadingMore) return
        val l = local.value
        viewModelScope.launch {
            local.update { it.copy(loadingMore = true) }
            // The cursor after the last page held: the cached page's until an extra page moved it.
            val cursor = if (l.extraLoaded) l.nextCursor else state.value.let { _ -> null }
            val next = cursor ?: cachedCursor() ?: return@launch local.update { it.copy(loadingMore = false) }
            when (val result = repository.taggingQueuePage(next)) {
                is AppResult.Ok -> local.update {
                    it.copy(loadingMore = false, extra = it.extra + result.value.deals, extraLoaded = true, nextCursor = result.value.nextCursor)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale tagging queue page failed") }
                    local.update { it.copy(loadingMore = false) }
                }
            }
        }
    }

    private var cachedCursorValue: String? = null

    init {
        // Keep the cached first page's cursor to hand, so LoadMore can start from it.
        viewModelScope.launch { repository.observeTaggingQueue().collect { cachedCursorValue = it?.nextCursor } }
    }

    private fun cachedCursor(): String? = cachedCursorValue

    private companion object {
        const val EMPTY_NONE = "No sales waiting to be tagged at your park"
        const val EMPTY_FAILED = "Could not load the sales. Connect and try again."
    }
}

private fun SaleTaggingDealDto.toCardUi(): SaleTaggingCardUi = SaleTaggingCardUi(
    dealId = salesDealId,
    title = dotJoin(productType, breed, farm),
    countLine = dotJoin(
        "$declaredAnimalCount ${if (declaredAnimalCount == 1) "animal" else "animals"}",
        if (alreadyTagged > 0) "$alreadyTagged tagged" else "",
    ),
    metaLine = "Sale date ${farmDate(saleDate)}",
    remainingChip = "$remaining to tag",
)

/**
 * Tagging one sale from the pen. The Bluetooth reader is a keyboard wedge and an APP-WIDE
 * SINGLETON, so it is switched on only on an explicit tap and off the moment it has done its job
 * -- on the first tag, on leaving the screen, and before submit -- exactly as the Counts form does.
 */
@HiltViewModel
class SaleTaggingViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val scanSource: ScanSource,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val dealId: String = savedStateHandle.get<String>(Routes.SALE_ID_ARG).orEmpty()

    private data class Basket(
        val dto: SaleCandidateDto,
        val weight: String = "",
        val rate: String = "",
        val weightError: String = "",
        val rateError: String = "",
        val blockedReason: String = "",
    )

    private data class Local(
        /** The sale as the queue last showed it; kept once read so finishing it (and so leaving the queue) does not blank the screen. */
        val deal: SaleTaggingDealDto? = null,
        /** The park this sale's farm code resolves to; blank until the catalog is read. */
        val parkId: String = "",
        val catalogFailed: Boolean = false,
        val alreadyTagged: List<SaleAllocationAnimalDto> = emptyList(),
        val allocationRead: Boolean = false,
        val tagInput: String = "",
        val scanning: Boolean = false,
        val lookupInFlight: Boolean = false,
        val lookupMessage: String? = null,
        val matches: List<SaleCandidateDto> = emptyList(),
        val basket: List<Basket> = emptyList(),
        val submitInFlight: Boolean = false,
        val confirmKey: String = UUID.randomUUID().toString(),
        val done: Boolean = false,
        val doneLine: String = "",
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())
    private var scanJob: Job? = null

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_TAG_ANIMALS_OPENED, mapOf(AnalyticsEvents.Params.KIND to "tag_only"))
        viewModelScope.launch { repository.observeTaggingQueue().collect { queue -> queue?.deals?.firstOrNull { it.salesDealId == dealId }?.let { deal -> local.update { it.copy(deal = deal) } } } }
        viewModelScope.launch {
            when (val result = repository.saleLocations()) {
                is AppResult.Ok -> local.update { l ->
                    // The sale's farm code IS the park's short code (CBE, CPT). The catalog is
                    // already clamped to the caller's parks on the server.
                    val park = result.value.parks.firstOrNull { it.label.equals(l.deal?.farm, ignoreCase = true) }
                        ?: result.value.parks.singleOrNull()
                    l.copy(parkId = park?.parkId.orEmpty(), catalogFailed = park == null)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale tagging locations read failed") }
                    local.update { it.copy(catalogFailed = true, message = result.message) }
                }
            }
            readAllocation()
        }
    }

    val state: StateFlow<SaleTaggingUiState> = combine(local, repository.observeTaggingQueue()) { l, _ ->
        val deal = l.deal
        val declared = deal?.declaredAnimalCount ?: 0
        val tagged = l.alreadyTagged.size.takeIf { l.allocationRead } ?: (deal?.alreadyTagged ?: 0)
        val remaining = (declared - tagged).coerceAtLeast(0)
        val basketUi = l.basket.map { b ->
            SaleTaggingBasketAnimalUi(
                goatId = b.dto.goatId,
                tag = b.dto.tagLabel(),
                location = b.dto.operationalLocationDisplay,
                weight = b.weight, rate = b.rate,
                weightError = b.weightError, rateError = b.rateError,
                blockedReason = b.blockedReason,
            )
        }
        val gate = SaleTaggingRules.submitGate(basketUi, remaining)
        SaleTaggingUiState(
            saleLine = deal?.let { dotJoin(it.productType, it.breed, it.farm, "${it.declaredAnimalCount} ${if (it.declaredAnimalCount == 1) "animal" else "animals"}") }.orEmpty(),
            progressLine = if (declared > 0) "$tagged of $declared tagged · ${if (remaining == 0) "none left" else "$remaining to tag"}" else "",
            tagInput = l.tagInput,
            scanning = l.scanning,
            lookupInFlight = l.lookupInFlight,
            lookupMessage = l.lookupMessage,
            matches = l.matches.map { c ->
                SaleTaggingMatchUi(c.goatId, c.tagLabel(), dotJoin(c.displayId, c.breed, c.sex), c.operationalLocationDisplay, c.sellable, c.blockedReason)
            },
            basket = basketUi,
            alreadyTagged = l.alreadyTagged.map { it.toDoneUi() },
            submitLabel = if (l.basket.isEmpty()) SUBMIT else "$SUBMIT ${l.basket.size} ${if (l.basket.size == 1) "animal" else "animals"}",
            canSubmit = gate.enabled && !l.done,
            submitHint = gate.hint,
            submitInFlight = l.submitInFlight,
            done = l.done,
            doneLine = l.doneLine,
            isLoading = deal == null && !l.catalogFailed && l.message == null,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleTaggingUiState())

    fun onEvent(event: SaleTaggingEvent) {
        when (event) {
            is SaleTaggingEvent.TagInputChanged -> local.update { it.copy(tagInput = event.text, lookupMessage = null) }
            SaleTaggingEvent.ToggleScan -> toggleScan()
            SaleTaggingEvent.Lookup -> lookup()
            is SaleTaggingEvent.AddMatch -> local.value.matches.firstOrNull { it.goatId == event.goatId }?.let { addToBasket(it) }
            is SaleTaggingEvent.Remove -> local.update { l -> l.copy(basket = l.basket.filterNot { it.dto.goatId == event.goatId }, message = null) }
            is SaleTaggingEvent.WeightChanged -> local.update { l ->
                l.copy(basket = l.basket.map { if (it.dto.goatId == event.goatId) it.copy(weight = event.value, weightError = "") else it }, message = null)
            }
            is SaleTaggingEvent.RateChanged -> local.update { l ->
                l.copy(basket = l.basket.map { if (it.dto.goatId == event.goatId) it.copy(rate = event.value, rateError = "") else it }, message = null)
            }
            SaleTaggingEvent.Submit -> submit()
            SaleTaggingEvent.DismissMessage -> local.update { it.copy(message = null) }
            SaleTaggingEvent.Back, SaleTaggingEvent.Done -> stopScan()
        }
    }

    private fun toggleScan() {
        if (local.value.scanning) {
            stopScan()
            return
        }
        local.update { it.copy(scanning = true, lookupMessage = null) }
        scanSource.start()
        analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_SCAN_STARTED, mapOf(AnalyticsEvents.Params.FIELD to "tag"))
        scanJob = viewModelScope.launch {
            scanSource.tags.collect { tag ->
                analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_SCAN_CAPTURED, mapOf(AnalyticsEvents.Params.FIELD to "tag"))
                local.update { it.copy(tagInput = tag, lookupMessage = null) }
                // A scanned tag is fed through the SAME path a typed one takes, then looked up at
                // once: the person scanned because they want the animal in the basket.
                stopScan()
                lookup()
            }
        }
    }

    private fun stopScan() {
        if (!local.value.scanning) return
        scanSource.stop()
        scanJob?.cancel()
        scanJob = null
        local.update { it.copy(scanning = false) }
    }

    override fun onCleared() {
        // Leaving the screen must release the reader: capture consumes hardware key events
        // app-wide while enabled, so a leaked listener would eat another screen's input.
        stopScan()
        super.onCleared()
    }

    private fun lookup() {
        val l = local.value
        val query = l.tagInput.trim()
        if (query.isEmpty() || l.lookupInFlight) return
        if (l.parkId.isBlank()) {
            local.update { it.copy(lookupMessage = NO_PARK) }
            return
        }
        if (l.basket.any { b -> b.dto.identifiers().any { it.equals(query, ignoreCase = true) } }) {
            local.update { it.copy(lookupMessage = ALREADY_IN_BASKET, tagInput = "") }
            return
        }
        local.update { it.copy(lookupInFlight = true, lookupMessage = null, matches = emptyList()) }
        viewModelScope.launch {
            when (val result = repository.saleCandidates(l.parkId, null, emptyList(), query, null)) {
                is AppResult.Ok -> {
                    val found = result.value.candidates
                    val exact = SaleTaggingRules.exactMatch(query, found) { it.identifiers() }
                    when {
                        exact != null -> {
                            local.update { it.copy(lookupInFlight = false) }
                            addToBasket(exact)
                        }
                        found.isEmpty() -> local.update { it.copy(lookupInFlight = false, lookupMessage = NO_MATCH) }
                        else -> local.update { it.copy(lookupInFlight = false, matches = found, lookupMessage = PICK_ONE) }
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale tagging lookup failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(lookupInFlight = false, lookupMessage = result.message) }
                }
            }
        }
    }

    /** Puts a resolved animal in the basket; a refused one shows its backend reason instead. */
    private fun addToBasket(candidate: SaleCandidateDto) {
        if (!candidate.sellable) {
            local.update { it.copy(lookupMessage = candidate.blockedReason.ifBlank { CANNOT_SELL }, matches = emptyList()) }
            return
        }
        local.update { l ->
            if (l.basket.any { it.dto.goatId == candidate.goatId }) {
                l.copy(lookupMessage = ALREADY_IN_BASKET, matches = emptyList(), tagInput = "")
            } else {
                l.copy(basket = l.basket + Basket(candidate), matches = emptyList(), tagInput = "", lookupMessage = null, message = null)
            }
        }
        analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_ANIMAL_ADDED)
    }

    private fun submit() {
        val l = local.value
        if (l.basket.isEmpty() || l.submitInFlight) return
        stopScan()
        val errors = l.basket.map { b ->
            b.copy(
                weightError = if (SaleTaggingRules.weightLooksValid(b.weight)) "" else WEIGHT_NEEDED,
                rateError = if (SaleTaggingRules.rateLooksValid(b.rate)) "" else RATE_NEEDED,
            )
        }
        if (errors.any { it.weightError.isNotBlank() || it.rateError.isNotBlank() }) {
            local.update { it.copy(basket = errors, message = SaleTaggingRules.HINT_FIGURES) }
            return
        }
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, message = null) }
            val ids = l.basket.map { it.dto.goatId }
            // Review first: the server re-judges every animal and names the ones it refuses. A
            // refused animal is marked in the basket for the person to remove; nothing is written.
            when (val preview = repository.previewAllocation(SaleAllocationRequestDto(dealId, ids))) {
                is AppResult.Ok -> {
                    val blocked = preview.value.blockedAnimals.associateBy { it.goatId }
                    if (blocked.isNotEmpty()) {
                        local.update { cur ->
                            cur.copy(
                                submitInFlight = false,
                                basket = cur.basket.map { b -> blocked[b.dto.goatId]?.let { b.copy(blockedReason = it.blockedReason.ifBlank { CANNOT_SELL }) } ?: b },
                                message = SaleTaggingRules.HINT_BLOCKED,
                            )
                        }
                        return@launch
                    }
                }
                is AppResult.Err -> {
                    preview.cause?.let { crashReporter.recordException(it, "sale tagging preview failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to preview.message.take(120)))
                    local.update { it.copy(submitInFlight = false, message = preview.message) }
                    return@launch
                }
            }
            val request = SaleAllocationRequestDto(
                salesDealId = dealId,
                goatIds = ids,
                animalWeightsKg = l.basket.associate { it.dto.goatId to it.weight.trim() },
                animalRatesRupees = l.basket.associate { it.dto.goatId to it.rate.trim() },
            )
            when (val result = repository.confirmAllocation(l.confirmKey, request)) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_TAGGING_SUBMITTED, mapOf(AnalyticsEvents.Params.REASON to result.value.allocated.toString()))
                    local.update {
                        it.copy(
                            submitInFlight = false,
                            done = true,
                            basket = emptyList(),
                            doneLine = "${result.value.allocated} ${if (result.value.allocated == 1) "animal" else "animals"} tagged and marked sold.",
                        )
                    }
                    readAllocation()
                    // The sale has left the queue; refresh the cached page so the list agrees.
                    repository.refreshTaggingQueue()
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale tagging confirm failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    // A refused confirm gets a FRESH key: the server may have recorded a partial
                    // state under the old one and an exact replay would return that, not retry.
                    local.update { it.copy(submitInFlight = false, confirmKey = UUID.randomUUID().toString(), message = result.message) }
                }
            }
        }
    }

    private suspend fun readAllocation() {
        when (val a = repository.saleAllocation(dealId)) {
            is AppResult.Ok -> local.update { it.copy(alreadyTagged = a.value.animals, allocationRead = true) }
            is AppResult.Err -> a.cause?.let { crashReporter.recordException(it, "sale tagging allocation read failed") }
        }
    }

    private fun SaleCandidateDto.identifiers(): List<String> = listOf(tagNumber, secondaryTagNumber, displayId).filter { it.isNotBlank() }
    private fun SaleCandidateDto.tagLabel(): String = tagNumber.ifBlank { secondaryTagNumber }.ifBlank { displayId }

    private fun SaleAllocationAnimalDto.toDoneUi() = SaleTaggingDoneAnimalUi(
        tag = tagNumber.ifBlank { "—" },
        location = operationalLocationDisplay,
        figures = dotJoin(
            weightKg.takeIf { it.isNotBlank() }?.let { "$it kg" },
            rateRupees.toDoubleOrNull()?.let { rupees(it) },
        ),
    )

    private companion object {
        const val SUBMIT = "Submit"
        const val NO_PARK = "Could not work out your park. Connect and try again."
        const val NO_MATCH = "No animal at your park matches that tag"
        const val PICK_ONE = "More than one animal matches — pick the right one"
        const val ALREADY_IN_BASKET = "That animal is already in the basket"
        const val CANNOT_SELL = "This animal cannot be sold yet"
        const val WEIGHT_NEEDED = "Enter the weight in kg"
        const val RATE_NEEDED = "Enter the rate in rupees"
    }
}
