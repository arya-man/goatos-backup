package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
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
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.VendorsRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.FeedPurchaseWriteDto
import sg.mesha.goatos.feature.vendors.FeedPurchaseCardUi
import sg.mesha.goatos.feature.vendors.FeedPurchaseCreateEvent
import sg.mesha.goatos.feature.vendors.FeedPurchaseCreateUiState
import sg.mesha.goatos.feature.vendors.FeedPurchaseDetailEvent
import sg.mesha.goatos.feature.vendors.FeedPurchaseDetailUiState
import sg.mesha.goatos.feature.vendors.FeedPurchasesListEvent
import sg.mesha.goatos.feature.vendors.FeedPurchasesListUiState
import sg.mesha.goatos.feature.vendors.PurchaseField
import sg.mesha.goatos.feature.vendors.VendorsDetailRowUi
import sg.mesha.goatos.feature.vendors.VendorsDetailSectionUi
import sg.mesha.goatos.feature.vendors.VendorsFilterUi
import sg.mesha.goatos.feature.vendors.VendorsOptionUi
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.ui.Routes
import java.util.UUID
import javax.inject.Inject

/** The Feed Purchases tab (module vendors): the ledger, Room-first, narrowed by delivery state. */
@HiltViewModel
class FeedPurchasesListViewModel @Inject constructor(
    private val repository: VendorsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Scope(val delivery: String = "", val title: String = "", val refreshNonce: Int = 0)

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    init {
        viewModelScope.launch { repository.refreshFeedPurchaseOptions() }
    }

    fun bind(title: String) {
        if (scope.value.title == title) return
        scope.value = scope.value.copy(title = title)
        analytics.track(AnalyticsEventsVendors.VENDORS_PURCHASES_VIEWED)
    }

    val state: StateFlow<FeedPurchasesListUiState> = combine(
        _isRefreshing,
        scope,
        repository.observeFeedPurchaseOptions(),
        repository.feedPurchaseTotals,
    ) { refreshing, current, options, totals ->
        FeedPurchasesListUiState(
            title = current.title,
            isRefreshing = refreshing,
            // Chip labels are BACKEND words ("On the road", "Reached"); only "All" is the screen's.
            filters = listOf(VendorsFilterUi("", FILTER_ALL, current.delivery.isBlank())) +
                options?.deliveryStatuses.orEmpty().map { VendorsFilterUi(it.key, it.label, it.key == current.delivery) },
            totalsLine = if (totals.total > 0) dotJoin("${totals.total} ${if (totals.total == 1) COUNT_ONE else COUNT_MANY}", kilograms(totals.quantityKg), rupees(totals.spendRupees)) else "",
            emptyMessage = if (current.delivery.isNotBlank()) EMPTY_FILTERED else EMPTY_MESSAGE,
            canAdd = true,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedPurchasesListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<FeedPurchaseCardUi>> = scope
        .flatMapLatest { current -> repository.feedPurchases("", current.delivery).map { page -> page.map { it.toCardUi() } } }
        .cachedIn(viewModelScope)

    fun onEvent(event: FeedPurchasesListEvent) {
        when (event) {
            FeedPurchasesListEvent.Refresh -> refresh()
            is FeedPurchasesListEvent.SelectDelivery -> {
                if (scope.value.delivery != event.key) {
                    scope.value = scope.value.copy(delivery = event.key)
                    analytics.track(AnalyticsEventsVendors.VENDORS_LIST_FILTERED, mapOf(AnalyticsEvents.Params.REASON to event.key.ifBlank { "all" }))
                }
            }
            is FeedPurchasesListEvent.OpenPurchase -> analytics.track(AnalyticsEventsVendors.VENDORS_PURCHASE_OPENED)
            FeedPurchasesListEvent.AddPurchase -> analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "feed purchase list page load failed")
        analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(120)))
    }

    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching { repository.invalidateFeedPurchases("", scope.value.delivery) }
                repository.refreshFeedPurchaseOptions()
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val FILTER_ALL = "All"
        const val COUNT_ONE = "load"
        const val COUNT_MANY = "loads"
        const val EMPTY_MESSAGE = "No feed purchases recorded yet"
        const val EMPTY_FILTERED = "No loads in this view"
    }
}

internal fun FeedPurchaseDto.toCardUi(): FeedPurchaseCardUi = FeedPurchaseCardUi(
    listKey = feedPurchaseId,
    purchaseId = feedPurchaseId,
    feedItem = feedItem,
    loadLine = dotJoin(farm, if (batchNo > 0) "Load $batchNo" else ""),
    quantityLine = dotJoin(kilograms(quantityKg), rupees(totalCost)),
    metaLine = metaLine(),
    deliveryLabel = deliveryLabel(),
    deliveryTone = deliveryTone(deliveryStatus),
    paymentLabel = paymentStatus,
    paymentTone = paymentTone(paymentStatus),
)

/** The delivery word comes from the options vocabulary when the list has it; the row's own
 *  reached date is the honest fallback label until the options load. */
private fun FeedPurchaseDto.deliveryLabel(): String = when (deliveryStatus) {
    "reached" -> if (!reachedOn.isNullOrBlank()) "Reached ${farmDate(reachedOn)}" else "Reached"
    "purchased" -> "On the road"
    else -> deliveryStatus
}

/** One feed purchase's detail (L1), read-only. */
@HiltViewModel
class FeedPurchaseDetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: VendorsRepository,
) : ViewModel() {
    private val purchaseId: String = savedStateHandle.get<String>(Routes.FEED_PURCHASE_ID_ARG).orEmpty()
    private val local = MutableStateFlow(Pair(false, false)) // (refreshing, loaded)

    init {
        refresh()
    }

    val state: StateFlow<FeedPurchaseDetailUiState> = combine(repository.observeFeedPurchase(purchaseId), local) { purchase, (refreshing, loaded) ->
        if (purchase == null) {
            FeedPurchaseDetailUiState(isRefreshing = refreshing, isLoading = !loaded)
        } else {
            FeedPurchaseDetailUiState(
                title = purchase.feedItem,
                subtitle = dotJoin(purchase.farm, if (purchase.batchNo > 0) "Load ${purchase.batchNo}" else "", farmDate(purchase.purchaseDate)),
                deliveryLabel = purchase.deliveryLabel(),
                deliveryTone = deliveryTone(purchase.deliveryStatus),
                sections = purchase.sections(),
                deliveryNote = if (purchase.deliveryStatus == "purchased") ON_THE_ROAD_NOTE else "",
                isRefreshing = refreshing,
                isLoading = false,
            )
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedPurchaseDetailUiState())

    fun onEvent(event: FeedPurchaseDetailEvent) {
        if (event == FeedPurchaseDetailEvent.Refresh) refresh()
    }

    private fun refresh() {
        viewModelScope.launch {
            local.value = Pair(true, local.value.second)
            try {
                // The ledger row is cached from the list page; there is no per-purchase read on the
                // backend, so refreshing here means refreshing the list page it came from.
                repository.invalidateFeedPurchases("", "")
            } finally {
                local.value = Pair(false, true)
            }
        }
    }

    private companion object {
        const val ON_THE_ROAD_NOTE = "This load is still on the road. It is not counted as stock until it is marked reached on the web."
    }
}

internal fun FeedPurchaseDto.sections(): List<VendorsDetailSectionUi> {
    fun rows(vararg pairs: Pair<String, String?>) = pairs.mapNotNull { (label, value) -> value?.takeIf { it.isNotBlank() }?.let { VendorsDetailRowUi(label, it) } }
    val load = rows(
        "Quantity bought" to kilograms(quantityKg),
        "Vendor" to vendor,
        "Bought on" to farmDate(purchaseDate),
        "Reached on" to reachedOn?.let(::farmDate),
        "Weight received" to reachedWeightKg?.let(::kilograms),
        "Counted as stock" to stockKg?.let(::kilograms),
    )
    val money = rows(
        "Feed cost" to rupees(feedCost),
        "Transport" to rupees(transportCost),
        "Loading" to rupees(loadingCost),
        "Unloading" to rupees(unloadingCost),
        "Landed cost" to rupees(totalCost),
        "Per kg" to perKgCost?.let { "₹" + indianNumber(it, 2) },
        "Payment" to paymentStatus,
        "Paid so far" to rupees(paymentReleased),
        "Balance to pay" to rupees(paymentBalance),
    )
    return listOfNotNull(
        VendorsDetailSectionUi("The load", load).takeIf { load.isNotEmpty() },
        VendorsDetailSectionUi("The money", money).takeIf { money.isNotEmpty() },
    )
}

/** The record-purchase wizard: two steps, offline-first write with a stable client id. */
@HiltViewModel
class FeedPurchaseCreateViewModel @Inject constructor(
    private val savedStateHandle: SavedStateHandle,
    private val repository: VendorsRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val step: Int = 0,
        val values: Map<PurchaseField, String> = mapOf(PurchaseField.PURCHASE_DATE to todayIst(), PurchaseField.PAYMENT_STATUS to "Pending"),
        val fieldErrors: Map<PurchaseField, String> = emptyMap(),
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val closeAfterSave: Boolean = false,
        val submitInFlight: Boolean = false,
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())

    private val clientId: String
        get() = savedStateHandle.get<String>(KEY_CLIENT_ID) ?: UUID.randomUUID().toString().also { savedStateHandle[KEY_CLIENT_ID] = it }

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        viewModelScope.launch { repository.refreshFeedPurchaseOptions() }
    }

    val state: StateFlow<FeedPurchaseCreateUiState> = combine(local, repository.observeFeedPurchaseOptions()) { l, options ->
        val o = options ?: FeedPurchaseOptionsDto()
        FeedPurchaseCreateUiState(
            step = l.step,
            stepCount = STEP_COUNT,
            values = if (l.values[PurchaseField.FARM].isNullOrBlank() && o.farms.size == 1) l.values + (PurchaseField.FARM to o.farms.first()) else l.values,
            farms = o.farms.map { VendorsOptionUi(it, it) },
            feedItems = o.feedItems.map { VendorsOptionUi(it.label, it.label) },
            paymentStatuses = o.paymentStatuses.map { VendorsOptionUi(it, it) },
            vendorSuggestions = o.vendors,
            fieldErrors = l.fieldErrors,
            contextLine = dotJoin(l.values[PurchaseField.FARM], l.values[PurchaseField.FEED_ITEM], l.values[PurchaseField.QUANTITY_KG]?.toDoubleOrNull()?.let(::kilograms), l.values[PurchaseField.VENDOR]),
            today = todayIst(),
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            closeAfterSave = l.closeAfterSave,
            submitInFlight = l.submitInFlight,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), FeedPurchaseCreateUiState())

    fun onEvent(event: FeedPurchaseCreateEvent) {
        // A queued or accepted write is read-only: the last step stays on screen for the banner
        // and closes by itself, so an edit or a second submit in that window has nothing to land on.
        val locked = local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        if (locked && event !is FeedPurchaseCreateEvent.Back && event !is FeedPurchaseCreateEvent.RecordAnother && event !is FeedPurchaseCreateEvent.DismissMessage) return
        when (event) {
            is FeedPurchaseCreateEvent.FieldChanged -> local.update { it.copy(values = it.values + (event.field to event.value), fieldErrors = it.fieldErrors - event.field) }
            FeedPurchaseCreateEvent.Next -> next()
            FeedPurchaseCreateEvent.Previous -> local.update { it.copy(step = (it.step - 1).coerceAtLeast(0)) }
            FeedPurchaseCreateEvent.Submit -> submit()
            FeedPurchaseCreateEvent.RecordAnother -> {
                savedStateHandle[KEY_CLIENT_ID] = UUID.randomUUID().toString()
                local.value = Local()
            }
            FeedPurchaseCreateEvent.DismissMessage -> local.update { it.copy(message = null) }
            FeedPurchaseCreateEvent.Back -> Unit
        }
    }

    private fun next() {
        val errors = validate(local.value.step, local.value.values)
        if (errors.isNotEmpty()) {
            local.update { it.copy(fieldErrors = errors) }
            return
        }
        local.update { it.copy(step = (it.step + 1).coerceAtMost(STEP_COUNT - 1), fieldErrors = emptyMap()) }
    }

    private fun submit() {
        val current = local.value
        val errors = (0 until STEP_COUNT).fold(emptyMap<PurchaseField, String>()) { acc, step -> acc + validate(step, current.values) }
        if (errors.isNotEmpty()) {
            val firstStep = (0 until STEP_COUNT).first { validate(it, current.values).isNotEmpty() }
            local.update { it.copy(step = firstStep, fieldErrors = errors) }
            return
        }
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, message = null) }
            when (val result = syncRepository.enqueueFeedPurchaseCreate(clientId, current.values.toWrite())) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_PURCHASE_QUEUED)
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_SAVING) }
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "feed purchase create enqueue failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }


    /** Upgrades the banner as the queued row moves: saved, still unsent (offline wording), or rejected. */
    private fun followWrite(itemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(itemId).collect { outcome ->
                local.update {
                    when (outcome) {
                        QueuedWriteOutcome.Saved -> it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, closeAfterSave = true)
                        QueuedWriteOutcome.StillQueued -> it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED, closeAfterSave = true)
                        is QueuedWriteOutcome.Rejected -> it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED)
                    }
                }
            }
        }
    }
    private fun validate(step: Int, v: Map<PurchaseField, String>): Map<PurchaseField, String> {
        val errors = mutableMapOf<PurchaseField, String>() // mobile-guard:ignore: per-call validation result, at most one entry per form field, returned and dropped
        fun money(f: PurchaseField) {
            val raw = v[f].orEmpty().trim()
            if (raw.isNotBlank() && (raw.toDoubleOrNull() == null || raw.toDouble() < 0.0)) errors[f] = AMOUNT
        }
        when (step) {
            0 -> {
                if (v[PurchaseField.FARM].isNullOrBlank()) errors[PurchaseField.FARM] = REQUIRED
                if (v[PurchaseField.FEED_ITEM].isNullOrBlank()) errors[PurchaseField.FEED_ITEM] = REQUIRED
                if (v[PurchaseField.VENDOR].isNullOrBlank()) errors[PurchaseField.VENDOR] = REQUIRED
                val qty = v[PurchaseField.QUANTITY_KG].orEmpty().trim()
                if (qty.toDoubleOrNull() == null || qty.toDouble() <= 0.0) errors[PurchaseField.QUANTITY_KG] = MORE_THAN_ZERO
                val bought = v[PurchaseField.PURCHASE_DATE].orEmpty()
                if (bought.isBlank()) errors[PurchaseField.PURCHASE_DATE] = REQUIRED
                else if (bought > todayIst()) errors[PurchaseField.PURCHASE_DATE] = NOT_FUTURE
                val reached = v[PurchaseField.REACHED_ON].orEmpty()
                if (reached.isNotBlank()) {
                    if (reached > todayIst()) errors[PurchaseField.REACHED_ON] = NOT_FUTURE
                    else if (bought.isNotBlank() && reached < bought) errors[PurchaseField.REACHED_ON] = NOT_BEFORE_BOUGHT
                }
                val received = v[PurchaseField.REACHED_WEIGHT_KG].orEmpty().trim()
                if (received.isNotBlank()) {
                    if (received.toDoubleOrNull() == null || received.toDouble() <= 0.0) errors[PurchaseField.REACHED_WEIGHT_KG] = MORE_THAN_ZERO
                    else if (reached.isBlank()) errors[PurchaseField.REACHED_WEIGHT_KG] = NEEDS_REACHED_DATE
                }
            }
            else -> {
                listOf(PurchaseField.FEED_COST, PurchaseField.TRANSPORT_COST, PurchaseField.LOADING_COST, PurchaseField.UNLOADING_COST, PurchaseField.TOTAL_COST, PurchaseField.PAYMENT_RELEASED).forEach(::money)
                if (v[PurchaseField.PAYMENT_STATUS].isNullOrBlank()) errors[PurchaseField.PAYMENT_STATUS] = REQUIRED
            }
        }
        return errors
    }

    private fun Map<PurchaseField, String>.toWrite(): FeedPurchaseWriteDto {
        fun money(f: PurchaseField): Double? = get(f).orEmpty().trim().ifBlank { null }?.toDoubleOrNull()
        val reachedOn = get(PurchaseField.REACHED_ON).orEmpty().ifBlank { null }
        return FeedPurchaseWriteDto(
            purchaseDate = get(PurchaseField.PURCHASE_DATE).orEmpty(),
            farm = get(PurchaseField.FARM).orEmpty(),
            feedItem = get(PurchaseField.FEED_ITEM).orEmpty(),
            quantityKg = get(PurchaseField.QUANTITY_KG).orEmpty().trim().toDouble(),
            feedCost = money(PurchaseField.FEED_COST),
            transportCost = money(PurchaseField.TRANSPORT_COST),
            loadingCost = money(PurchaseField.LOADING_COST),
            unloadingCost = money(PurchaseField.UNLOADING_COST),
            totalCost = money(PurchaseField.TOTAL_COST),
            vendor = get(PurchaseField.VENDOR).orEmpty().trim(),
            paymentReleased = money(PurchaseField.PAYMENT_RELEASED),
            paymentStatus = get(PurchaseField.PAYMENT_STATUS).orEmpty(),
            reachedOn = reachedOn,
            reachedWeightKg = if (reachedOn == null) null else money(PurchaseField.REACHED_WEIGHT_KG),
        )
    }

    private companion object {
        const val KEY_CLIENT_ID = "feed_purchase_create_client_id"
        const val STEP_COUNT = 2
        const val REQUIRED = "Required"
        const val MORE_THAN_ZERO = "Must be more than zero"
        const val AMOUNT = "Enter an amount"
        const val NOT_FUTURE = "Cannot be in the future"
        const val NOT_BEFORE_BOUGHT = "Cannot be before the day it was bought"
        const val NEEDS_REACHED_DATE = "Enter the reached date first"
        const val MESSAGE_SAVING = "Saving purchase…"
        const val MESSAGE_SAVED = "Purchase saved to the ledger."
        const val MESSAGE_QUEUED = "Saved on this phone. It will reach the ledger when the phone is online."
        const val MESSAGE_NOT_SAVED = "Could not save this purchase. Try again."
    }
}
