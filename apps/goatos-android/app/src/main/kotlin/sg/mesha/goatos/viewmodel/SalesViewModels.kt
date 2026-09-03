package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import androidx.paging.PagingData
import androidx.paging.cachedIn
import androidx.paging.map
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
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
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidateDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SaleShedGroupDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.feature.vendors.SaleBuyerListState
import sg.mesha.goatos.feature.vendors.SaleBuyerOptionUi
import sg.mesha.goatos.feature.vendors.SaleCandidateUi
import sg.mesha.goatos.feature.vendors.SaleCardUi
import sg.mesha.goatos.feature.vendors.SaleCreateEvent
import sg.mesha.goatos.feature.vendors.SaleCreateUiState
import sg.mesha.goatos.feature.vendors.SaleDetailEvent
import sg.mesha.goatos.feature.vendors.SaleDetailUiState
import sg.mesha.goatos.feature.vendors.SaleField
import sg.mesha.goatos.feature.vendors.SaleShedGroupUi
import sg.mesha.goatos.feature.vendors.SaleTagAnimalsEvent
import sg.mesha.goatos.feature.vendors.SaleTagAnimalsUiState
import sg.mesha.goatos.feature.vendors.SaleTagStep
import sg.mesha.goatos.feature.vendors.SalesListEvent
import sg.mesha.goatos.feature.vendors.SalesListUiState
import sg.mesha.goatos.feature.vendors.VendorsDetailRowUi
import sg.mesha.goatos.feature.vendors.VendorsDetailSectionUi
import sg.mesha.goatos.feature.vendors.VendorsFilterUi
import sg.mesha.goatos.feature.vendors.VendorsOptionUi
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.ui.Routes
import java.time.LocalDate
import java.util.UUID
import javax.inject.Inject

/** Sales tab (Procurement module, maintainer instruction 2026-09-04): the ledger, Room-first, by farm. */
@HiltViewModel
class SalesListViewModel @Inject constructor(
    private val repository: SalesRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Scope(val farm: String = "", val title: String = "", val refreshNonce: Int = 0)

    private val scope = MutableStateFlow(Scope())
    private val _isRefreshing = MutableStateFlow(false)

    init {
        viewModelScope.launch { repository.refreshOptions() }
    }

    fun bind(title: String) {
        if (scope.value.title == title) return
        scope.value = scope.value.copy(title = title)
        analytics.track(AnalyticsEventsVendors.VENDORS_SALES_VIEWED)
    }

    val state: StateFlow<SalesListUiState> = combine(_isRefreshing, scope, repository.observeOptions(), repository.dealTotals) { refreshing, current, options, totals ->
        SalesListUiState(
            title = current.title,
            isRefreshing = refreshing,
            filters = listOf(VendorsFilterUi("", FILTER_ALL, current.farm.isBlank())) +
                options?.farms.orEmpty().map { VendorsFilterUi(it, it, it == current.farm) },
            countLine = if (totals.total > 0) "${totals.total} ${if (totals.total == 1) COUNT_ONE else COUNT_MANY}" else "",
            emptyMessage = if (current.farm.isNotBlank()) EMPTY_FILTERED else EMPTY_MESSAGE,
            canAdd = true,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SalesListUiState())

    @OptIn(ExperimentalCoroutinesApi::class)
    val rows: Flow<PagingData<SaleCardUi>> = scope
        .flatMapLatest { current -> repository.deals(current.farm).map { page -> page.map { it.toCardUi() } } }
        .cachedIn(viewModelScope)

    fun onEvent(event: SalesListEvent) {
        when (event) {
            SalesListEvent.Refresh -> refresh()
            is SalesListEvent.SelectFarm -> {
                if (scope.value.farm != event.key) {
                    scope.value = scope.value.copy(farm = event.key)
                    analytics.track(AnalyticsEventsVendors.VENDORS_LIST_FILTERED, mapOf(AnalyticsEvents.Params.REASON to event.key.ifBlank { "all" }))
                }
            }
            is SalesListEvent.OpenSale -> analytics.track(AnalyticsEventsVendors.VENDORS_SALE_OPENED)
            SalesListEvent.AddSale -> analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        }
    }

    fun onRowsLoadFailed(error: Throwable) {
        crashReporter.recordException(error, "sales list page load failed")
        analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to (error.message ?: "unknown").take(120)))
    }

    private fun refresh() {
        viewModelScope.launch {
            _isRefreshing.value = true
            try {
                // exception:exempt local cache-marker delete; a failure just leaves the TTL skip
                runCatching { repository.invalidateDeals(scope.value.farm) }
                repository.refreshOptions()
                scope.value = scope.value.let { it.copy(refreshNonce = it.refreshNonce + 1) }
            } finally {
                _isRefreshing.value = false
            }
        }
    }

    private companion object {
        const val FILTER_ALL = "All"
        const val COUNT_ONE = "sale"
        const val COUNT_MANY = "sales"
        const val EMPTY_MESSAGE = "No sales recorded yet"
        const val EMPTY_FILTERED = "No sales at this farm"
    }
}

/** Status tones follow the backend's sales_deal_statuses group (ok / dng / info / warn). */
internal fun saleStatusTone(status: String, options: SalesOptionsDto?): VendorsTone {
    val tone = options?.statuses?.firstOrNull { it.key == status }?.tone
        ?: when (status) {
            "Deal Closed" -> "ok"
            "Deal Failed" -> "dng"
            "In Discussion" -> "info"
            "Advance Paid" -> "warn"
            else -> ""
        }
    return when (tone) {
        "ok" -> VendorsTone.OK
        "dng" -> VendorsTone.DANGER
        "info" -> VendorsTone.INFO
        "warn" -> VendorsTone.WARN
        else -> VendorsTone.NEUTRAL
    }
}

internal fun animalsLine(count: Double?): String = when {
    count == null -> ""
    count == 1.0 -> "1 animal"
    else -> "${indianNumber(count, 0)} animals"
}

internal fun SalesDealDto.toCardUi(options: SalesOptionsDto? = null): SaleCardUi = SaleCardUi(
    listKey = dealId,
    dealId = dealId,
    buyer = buyerName,
    productLine = dotJoin(productType, breed, farm),
    valueLine = dotJoin(animalsLine(animalCount), kilograms(totalWeightKg), rupees(salesValue)),
    // Sheet-imported deals carry paise dust (₹0.18 on a fully paid sale); under a rupee reads as paid.
    metaLine = dotJoin("Sold ${farmDate(saleDate)}", if (paymentBalance >= 1.0) "Balance ${rupees(paymentBalance)}" else "Fully paid"),
    statusLabel = status,
    statusTone = saleStatusTone(status, options),
)

internal fun SalesDealDto.sections(): List<VendorsDetailSectionUi> {
    fun rows(vararg pairs: Pair<String, String?>) = pairs.mapNotNull { (label, value) -> value?.takeIf { it.isNotBlank() }?.let { VendorsDetailRowUi(label, it) } }
    val sale = rows(
        "Sold on" to farmDate(saleDate),
        "Farm" to farm,
        "Product" to productType,
        "Breed" to breed,
        "Animals" to animalCount?.let { indianNumber(it, 0) },
        "Total weight" to totalWeightKg?.let(::kilograms),
    )
    val buyer = rows("Buyer" to buyerName, "Place" to buyerPlace)
    val money = rows(
        "Sale value" to rupees(salesValue),
        "Advance" to advanceAmount?.let(::rupees),
        "Received so far" to paymentReceived?.let(::rupees),
        "Balance" to rupees(paymentBalance),
    )
    val notes = rows("Comments" to comments, "Feedback" to feedback)
    return listOfNotNull(
        VendorsDetailSectionUi("The sale", sale).takeIf { sale.isNotEmpty() },
        VendorsDetailSectionUi("The buyer", buyer).takeIf { buyer.isNotEmpty() },
        VendorsDetailSectionUi("The money", money).takeIf { money.isNotEmpty() },
        VendorsDetailSectionUi("Notes", notes).takeIf { notes.isNotEmpty() },
    )
}

internal fun SaleShedGroupDto.toUi(): SaleShedGroupUi = SaleShedGroupUi(
    location = dotJoin(parkName, operationalLocationDisplay),
    animals = animals,
    tags = tagNumbers.joinToString(", "),
)

/** One sale (L1), read-only, plus what is tagged to it and the tag-animals entry. */
@HiltViewModel
class SaleDetailViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val dealId: String = savedStateHandle.get<String>(Routes.SALE_ID_ARG).orEmpty()

    private data class Local(
        val refreshing: Boolean = false,
        val loaded: Boolean = false,
        val taggedLine: String = "",
        val taggedGroups: List<SaleShedGroupUi> = emptyList(),
        val allocationRead: Boolean = false,
        val allocated: Int = 0,
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())

    init {
        refresh()
    }

    val state: StateFlow<SaleDetailUiState> = combine(repository.observeDeal(dealId), repository.observeOptions(), local) { deal, options, l ->
        if (deal == null) {
            SaleDetailUiState(isRefreshing = l.refreshing, isLoading = !l.loaded, message = l.message)
        } else {
            val declared = deal.animalCount?.toInt() ?: 0
            val complete = declared > 0 && l.allocated >= declared
            val live = deal.productType != "Manure" && deal.status != "Deal Failed" && !complete
            SaleDetailUiState(
                title = deal.buyerName,
                subtitle = dotJoin(deal.productType, deal.breed, deal.farm, farmDate(deal.saleDate)),
                statusLabel = deal.status,
                statusTone = saleStatusTone(deal.status, options),
                sections = deal.sections(),
                taggedLine = l.taggedLine,
                taggedGroups = l.taggedGroups,
                canTagAnimals = live,
                tagDisabledReason = when {
                    deal.productType == "Manure" -> TAG_MANURE
                    deal.status == "Deal Failed" -> TAG_FAILED
                    complete -> "All $declared ${if (declared == 1) "animal is" else "animals are"} tagged. Tag more on the web if the count changes."
                    else -> ""
                },
                isRefreshing = l.refreshing,
                isLoading = false,
                message = l.message,
            )
        }
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleDetailUiState())

    fun onEvent(event: SaleDetailEvent) {
        when (event) {
            SaleDetailEvent.Refresh -> refresh()
            SaleDetailEvent.TagAnimals -> analytics.track(AnalyticsEventsVendors.VENDORS_TAG_ANIMALS_OPENED)
            SaleDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
            SaleDetailEvent.Back -> Unit
        }
    }

    private fun refresh() {
        viewModelScope.launch {
            local.update { it.copy(refreshing = true) }
            try {
                // The ledger row is cached from the list page (there is no per-deal read on the
                // backend); refreshing means refreshing the ledger it came from.
                repository.invalidateDeals("")
                when (val result = repository.saleAllocation(dealId)) {
                    is AppResult.Ok -> {
                        val a = result.value
                        val declared = state.value.sections.firstOrNull { it.title == "The sale" }?.rows?.firstOrNull { it.label == "Animals" }?.value
                        local.update {
                            it.copy(
                                taggedLine = when {
                                    a.allocated == 0 -> ""
                                    declared != null -> "${a.allocated} of $declared tagged"
                                    else -> "${a.allocated} tagged"
                                },
                                taggedGroups = a.shedGroups.map { g -> g.toUi() },
                                allocationRead = true,
                                allocated = a.allocated,
                            )
                        }
                    }
                    is AppResult.Err -> {
                        // Offline or not allowed to read allocations: the sale still renders; only
                        // the tagged line stays blank. Not an error banner -- nothing the person did.
                        result.cause?.let { crashReporter.recordException(it, "sale allocation read failed") }
                    }
                }
            } finally {
                local.update { it.copy(refreshing = false, loaded = true) }
            }
        }
    }

    private companion object {
        const val TAG_MANURE = "A manure sale has no animals to tag."
        const val TAG_FAILED = "A failed deal has no animals to tag."
    }
}

/** The record-sale wizard: three steps, offline-first write with a stable client id. */
@HiltViewModel
class SaleCreateViewModel @Inject constructor(
    private val savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private data class Local(
        val step: Int = 0,
        val values: Map<SaleField, String> = mapOf(SaleField.SALE_DATE to todayIst()),
        val buyerSearch: String = "",
        val fieldErrors: Map<SaleField, String> = emptyMap(),
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val submitInFlight: Boolean = false,
        val message: String? = null,
        val buyersRefreshed: Boolean = false,
    )

    private val local = MutableStateFlow(Local())

    private val clientId: String
        get() = savedStateHandle.get<String>(KEY_CLIENT_ID) ?: UUID.randomUUID().toString().also { savedStateHandle[KEY_CLIENT_ID] = it }

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_ADD_OPENED)
        viewModelScope.launch {
            repository.refreshOptions()
            repository.refreshVendorOptions()
            local.update { it.copy(buyersRefreshed = true) }
        }
    }

    val state: StateFlow<SaleCreateUiState> = combine(local, repository.observeOptions(), repository.observeVendorOptions()) { l, options, vendors ->
        val o = options ?: SalesOptionsDto()
        val values = if (l.values[SaleField.STATUS].isNullOrBlank() && o.defaultStatus.isNotBlank()) l.values + (SaleField.STATUS to o.defaultStatus) else l.values
        val product = values[SaleField.PRODUCT_TYPE].orEmpty()
        val search = l.buyerSearch.trim().lowercase()
        val pickedId = values[SaleField.BUYER_VENDOR_ID].orEmpty()
        val buyers = vendors?.vendors.orEmpty()
            .filter { v ->
                v.vendorId == pickedId ||
                    search.length < 2 ||
                    listOf(v.businessName, v.city, v.state).any { it.lowercase().contains(search) }
            }
            .take(if (search.length < 2) BUYER_LIST_PREVIEW else BUYER_LIST_MAX)
            .map { SaleBuyerOptionUi(it.vendorId, it.businessName, listOf(it.city, it.state).filter { s -> s.isNotBlank() }.joinToString(", ")) }
        SaleCreateUiState(
            step = l.step,
            stepCount = STEP_COUNT,
            values = values,
            farms = o.farms.map { VendorsOptionUi(it, it) },
            productTypes = o.productTypes.map { VendorsOptionUi(it, it) },
            breeds = o.breeds[product].orEmpty().map { VendorsOptionUi(it, it) },
            statuses = o.statuses.map { VendorsOptionUi(it.key, it.label) },
            buyerSearch = l.buyerSearch,
            buyers = buyers,
            buyerListState = when {
                vendors == null && !l.buyersRefreshed -> SaleBuyerListState.LOADING
                vendors == null -> SaleBuyerListState.UNAVAILABLE
                vendors.vendors.isEmpty() -> SaleBuyerListState.EMPTY
                else -> SaleBuyerListState.READY
            },
            buyersTruncated = vendors?.truncated == true,
            fieldErrors = l.fieldErrors,
            contextLine = dotJoin(values[SaleField.FARM], product, values[SaleField.BREED], values[SaleField.BUYER_NAME], values[SaleField.SALES_VALUE]?.toDoubleOrNull()?.let(::rupees)),
            today = todayIst(),
            maxDate = LocalDate.now(VENDORS_IST).plusDays(o.maxSaleDateDaysAhead.toLong()).toString(),
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            submitInFlight = l.submitInFlight,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleCreateUiState())

    fun onEvent(event: SaleCreateEvent) {
        when (event) {
            is SaleCreateEvent.FieldChanged -> local.update { l ->
                var values = l.values + (event.field to event.value)
                // A product change invalidates the breed: the breed list belongs to the product.
                if (event.field == SaleField.PRODUCT_TYPE && l.values[SaleField.PRODUCT_TYPE] != event.value) values = values - SaleField.BREED
                l.copy(values = values, fieldErrors = l.fieldErrors - event.field)
            }
            is SaleCreateEvent.BuyerSearchChanged -> local.update { it.copy(buyerSearch = event.text) }
            is SaleCreateEvent.BuyerPicked -> pickBuyer(event.vendorId)
            SaleCreateEvent.Next -> next()
            SaleCreateEvent.Previous -> local.update { it.copy(step = (it.step - 1).coerceAtLeast(0)) }
            SaleCreateEvent.Submit -> submit()
            SaleCreateEvent.RecordAnother -> {
                savedStateHandle[KEY_CLIENT_ID] = UUID.randomUUID().toString()
                local.value = Local(buyersRefreshed = true)
            }
            SaleCreateEvent.DismissMessage -> local.update { it.copy(message = null) }
            SaleCreateEvent.Back -> Unit
        }
    }

    private fun pickBuyer(vendorId: String) {
        val picked = state.value.buyers.firstOrNull { it.vendorId == vendorId } ?: return
        local.update { l ->
            val values = l.values.toMutableMap()
            val previous = l.values[SaleField.BUYER_VENDOR_ID]
            values[SaleField.BUYER_VENDOR_ID] = vendorId
            // Prefill name and place from the vendor, but never overwrite a name the person typed
            // for a different buyer than the one previously picked.
            val previousName = state.value.buyers.firstOrNull { it.vendorId == previous }?.name
            if (l.values[SaleField.BUYER_NAME].isNullOrBlank() || l.values[SaleField.BUYER_NAME] == previousName) values[SaleField.BUYER_NAME] = picked.name
            val previousPlace = state.value.buyers.firstOrNull { it.vendorId == previous }?.place
            if (l.values[SaleField.BUYER_PLACE].isNullOrBlank() || l.values[SaleField.BUYER_PLACE] == previousPlace) values[SaleField.BUYER_PLACE] = picked.place
            l.copy(values = values, fieldErrors = l.fieldErrors - SaleField.BUYER_VENDOR_ID - SaleField.BUYER_NAME)
        }
    }

    private fun next() {
        val errors = validate(local.value.step, state.value.values)
        if (errors.isNotEmpty()) {
            local.update { it.copy(fieldErrors = errors) }
            return
        }
        local.update { it.copy(step = (it.step + 1).coerceAtMost(STEP_COUNT - 1), fieldErrors = emptyMap()) }
    }

    private fun submit() {
        val values = state.value.values
        val errors = (0 until STEP_COUNT).fold(emptyMap<SaleField, String>()) { acc, step -> acc + validate(step, values) }
        if (errors.isNotEmpty()) {
            val firstStep = (0 until STEP_COUNT).first { validate(it, values).isNotEmpty() }
            local.update { it.copy(step = firstStep, fieldErrors = errors) }
            return
        }
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, message = null) }
            when (val result = syncRepository.enqueueSalesDealCreate(clientId, values.toWrite())) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_SALE_QUEUED)
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED) }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale create enqueue failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_NOT_SAVED) }
                }
            }
        }
    }

    private fun validate(step: Int, v: Map<SaleField, String>): Map<SaleField, String> {
        val errors = mutableMapOf<SaleField, String>() // mobile-guard:ignore: per-call validation result, at most one entry per form field, returned and dropped
        fun nonNegative(f: SaleField, whole: Boolean = false) {
            val raw = v[f].orEmpty().trim()
            if (raw.isBlank()) return
            val n = raw.toDoubleOrNull()
            if (n == null || n < 0.0) errors[f] = if (whole) WHOLE_NUMBER else AMOUNT
            else if (whole && n != Math.floor(n)) errors[f] = WHOLE_NUMBER
        }
        when (step) {
            0 -> {
                val date = v[SaleField.SALE_DATE].orEmpty()
                if (date.isBlank()) errors[SaleField.SALE_DATE] = REQUIRED
                else if (date > state.value.maxDate) errors[SaleField.SALE_DATE] = TOO_FAR
                if (v[SaleField.FARM].isNullOrBlank()) errors[SaleField.FARM] = REQUIRED
                if (v[SaleField.PRODUCT_TYPE].isNullOrBlank()) errors[SaleField.PRODUCT_TYPE] = REQUIRED
                if (v[SaleField.BREED].isNullOrBlank()) errors[SaleField.BREED] = REQUIRED
                nonNegative(SaleField.ANIMAL_COUNT, whole = true)
                nonNegative(SaleField.TOTAL_WEIGHT_KG)
            }
            1 -> {
                if (v[SaleField.BUYER_VENDOR_ID].isNullOrBlank()) errors[SaleField.BUYER_VENDOR_ID] = PICK_BUYER
                val name = v[SaleField.BUYER_NAME].orEmpty().trim()
                if (name.isBlank()) errors[SaleField.BUYER_NAME] = REQUIRED
                else if (name.length > MAX_SHORT) errors[SaleField.BUYER_NAME] = TOO_LONG
                if (v[SaleField.BUYER_PLACE].orEmpty().trim().length > MAX_SHORT) errors[SaleField.BUYER_PLACE] = TOO_LONG
            }
            else -> {
                val value = v[SaleField.SALES_VALUE].orEmpty().trim()
                if (value.toDoubleOrNull() == null || value.toDouble() <= 0.0) errors[SaleField.SALES_VALUE] = MORE_THAN_ZERO
                nonNegative(SaleField.ADVANCE_AMOUNT)
                val advance = v[SaleField.ADVANCE_AMOUNT].orEmpty().trim().toDoubleOrNull()
                if (advance != null && value.toDoubleOrNull() != null && advance > value.toDouble()) errors[SaleField.ADVANCE_AMOUNT] = ADVANCE_OVER
                if (v[SaleField.STATUS].isNullOrBlank()) errors[SaleField.STATUS] = REQUIRED
                if (v[SaleField.COMMENTS].orEmpty().length > MAX_COMMENTS) errors[SaleField.COMMENTS] = TOO_LONG
            }
        }
        return errors
    }

    private fun Map<SaleField, String>.toWrite(): SalesDealWriteDto {
        fun number(f: SaleField): Double? = get(f).orEmpty().trim().ifBlank { null }?.toDoubleOrNull()
        return SalesDealWriteDto(
            saleDate = get(SaleField.SALE_DATE).orEmpty(),
            farm = get(SaleField.FARM).orEmpty(),
            productType = get(SaleField.PRODUCT_TYPE).orEmpty(),
            breed = get(SaleField.BREED).orEmpty(),
            buyerName = get(SaleField.BUYER_NAME).orEmpty().trim(),
            buyerPlace = get(SaleField.BUYER_PLACE).orEmpty().trim(),
            buyerVendorId = get(SaleField.BUYER_VENDOR_ID).orEmpty(),
            animalCount = number(SaleField.ANIMAL_COUNT),
            totalWeightKg = number(SaleField.TOTAL_WEIGHT_KG),
            salesValue = get(SaleField.SALES_VALUE).orEmpty().trim().toDouble(),
            advanceAmount = number(SaleField.ADVANCE_AMOUNT),
            comments = get(SaleField.COMMENTS).orEmpty().trim(),
            status = get(SaleField.STATUS).orEmpty(),
        )
    }

    private companion object {
        const val KEY_CLIENT_ID = "sale_create_client_id"
        const val STEP_COUNT = 3
        const val BUYER_LIST_PREVIEW = 8
        const val BUYER_LIST_MAX = 20
        const val MAX_SHORT = 160
        const val MAX_COMMENTS = 2000
        const val REQUIRED = "Required"
        const val PICK_BUYER = "Pick the buyer from the vendor register"
        const val MORE_THAN_ZERO = "Must be more than zero"
        const val AMOUNT = "Enter an amount, zero or more"
        const val WHOLE_NUMBER = "Whole number, zero or more"
        const val ADVANCE_OVER = "Cannot be more than the sale value"
        const val TOO_FAR = "Too far ahead — within 60 days of today"
        const val TOO_LONG = "Too long"
        const val MESSAGE_QUEUED = "Sale saved. It will reach the ledger when the phone is online."
        const val MESSAGE_NOT_SAVED = "Could not save this sale. Try again."
    }
}

/** Tag animals to a sale: pick (park → pen → search → tick) → review (server preview) → confirm. */
@HiltViewModel
class SaleTagAnimalsViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val dealId: String = savedStateHandle.get<String>(Routes.SALE_ID_ARG).orEmpty()

    private data class Local(
        val step: SaleTagStep = SaleTagStep.PICK,
        val locations: SaleLocationsDto? = null,
        val locationsFailed: Boolean = false,
        val parkId: String = "",
        val penKey: String = "",
        val search: String = "",
        val candidates: List<SaleCandidateDto> = emptyList(),
        val nextCursor: String? = null,
        val loading: Boolean = false,
        val loadFailed: Boolean = false,
        /** Picked animals across parks/pens/searches, by goat id; the DTO is kept for review. */
        val picked: Map<String, SaleCandidateDto> = emptyMap(),
        val reviewGroups: List<SaleShedGroupUi> = emptyList(),
        val reviewBlocked: List<SaleCandidateDto> = emptyList(),
        val reviewLine: String = "",
        val reviewInFlight: Boolean = false,
        val confirmInFlight: Boolean = false,
        val confirmKey: String = UUID.randomUUID().toString(),
        val doneLine: String = "",
        val alreadyTagged: Int = 0,
        val message: String? = null,
    )

    private val local = MutableStateFlow(Local())
    private var loadJob: Job? = null

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_TAG_ANIMALS_OPENED)
        viewModelScope.launch {
            when (val result = repository.saleLocations()) {
                is AppResult.Ok -> {
                    val first = result.value.parks.firstOrNull()?.parkId.orEmpty()
                    local.update { it.copy(locations = result.value, parkId = first) }
                    if (first.isNotBlank()) loadCandidates(reset = true)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale locations read failed") }
                    local.update { it.copy(locationsFailed = true, message = result.message) }
                }
            }
            when (val a = repository.saleAllocation(dealId)) {
                is AppResult.Ok -> local.update { it.copy(alreadyTagged = a.value.allocated) }
                is AppResult.Err -> Unit
            }
        }
    }

    val state: StateFlow<SaleTagAnimalsUiState> = combine(local, repository.observeDeal(dealId)) { l, deal ->
        val declared = deal?.animalCount?.toInt()?.takeIf { it > 0 }
        val remaining = declared?.let { it - l.alreadyTagged - l.picked.size }
        SaleTagAnimalsUiState(
            step = l.step,
            saleLine = deal?.let { dotJoin(it.buyerName, it.productType, it.breed, animalsLine(it.animalCount)) }.orEmpty(),
            parks = l.locations?.parks.orEmpty().map { VendorsOptionUi(it.parkId, it.label) },
            selectedParkId = l.parkId,
            pens = l.locations?.locations.orEmpty().filter { it.parkId == l.parkId }.map { VendorsOptionUi(penKey(it.shedId, it.partitionLabel), it.operationalLocationDisplay) },
            selectedPenKey = l.penKey,
            search = l.search,
            candidates = l.candidates.map { c ->
                SaleCandidateUi(
                    goatId = c.goatId,
                    tag = c.tagNumber.ifBlank { c.secondaryTagNumber }.ifBlank { c.displayId },
                    detailLine = dotJoin(c.displayId, c.breed, c.sex),
                    location = c.operationalLocationDisplay,
                    selected = l.picked.containsKey(c.goatId),
                    sellable = c.sellable,
                    blockedReason = c.blockedReason,
                )
            },
            candidatesLoading = l.loading,
            hasMore = !l.nextCursor.isNullOrBlank(),
            candidatesEmptyMessage = when {
                l.locationsFailed -> EMPTY_LOCATIONS_FAILED
                l.loadFailed -> EMPTY_LOAD_FAILED
                l.locations != null && l.parkId.isBlank() -> EMPTY_NO_PARK
                l.locations != null && !l.loading -> if (l.search.isNotBlank()) EMPTY_NO_MATCH else EMPTY_NONE
                else -> ""
            },
            selectedCount = l.picked.size,
            pickLine = when {
                declared == null -> ""
                remaining != null && remaining <= 0 -> "All $declared picked"
                else -> "$remaining still to pick"
            },
            reviewGroups = l.reviewGroups,
            reviewBlocked = l.reviewBlocked.map { c ->
                SaleCandidateUi(c.goatId, c.tagNumber.ifBlank { c.displayId }, dotJoin(c.displayId, c.breed, c.sex), c.operationalLocationDisplay, false, false, c.blockedReason)
            },
            reviewLine = l.reviewLine,
            reviewInFlight = l.reviewInFlight,
            confirmInFlight = l.confirmInFlight,
            doneLine = l.doneLine,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleTagAnimalsUiState())

    fun onEvent(event: SaleTagAnimalsEvent) {
        when (event) {
            is SaleTagAnimalsEvent.SelectPark -> {
                if (local.value.parkId != event.parkId) {
                    local.update { it.copy(parkId = event.parkId, penKey = "", message = null) }
                    loadCandidates(reset = true)
                }
            }
            is SaleTagAnimalsEvent.SelectPen -> {
                local.update { it.copy(penKey = event.penKey, message = null) }
                loadCandidates(reset = true)
            }
            is SaleTagAnimalsEvent.SearchChanged -> {
                local.update { it.copy(search = event.text) }
                loadCandidates(reset = true, debounce = true)
            }
            is SaleTagAnimalsEvent.ToggleAnimal -> local.update { l ->
                val c = l.candidates.firstOrNull { it.goatId == event.goatId } ?: l.picked[event.goatId] ?: return@update l
                if (!c.sellable) l else l.copy(picked = if (l.picked.containsKey(c.goatId)) l.picked - c.goatId else l.picked + (c.goatId to c))
            }
            SaleTagAnimalsEvent.LoadMore -> loadCandidates(reset = false)
            SaleTagAnimalsEvent.Review -> review()
            SaleTagAnimalsEvent.BackToPick -> local.update { it.copy(step = SaleTagStep.PICK, message = null) }
            SaleTagAnimalsEvent.Confirm -> confirm()
            SaleTagAnimalsEvent.DismissMessage -> local.update { it.copy(message = null) }
            SaleTagAnimalsEvent.Back, SaleTagAnimalsEvent.Done -> Unit
        }
    }

    private fun loadCandidates(reset: Boolean, debounce: Boolean = false) {
        loadJob?.cancel()
        loadJob = viewModelScope.launch {
            if (debounce) kotlinx.coroutines.delay(SEARCH_DEBOUNCE_MS)
            val l = local.value
            if (l.parkId.isBlank()) return@launch
            val cursor = if (reset) null else l.nextCursor
            if (!reset && cursor.isNullOrBlank()) return@launch
            local.update { it.copy(loading = true, loadFailed = false, candidates = if (reset) emptyList() else it.candidates) }
            val (shedId, partition) = splitPenKey(l.penKey)
            when (val result = repository.saleCandidates(l.parkId, shedId, listOfNotNull(partition), l.search.trim(), cursor)) {
                is AppResult.Ok -> local.update {
                    it.copy(
                        loading = false,
                        candidates = (if (reset) emptyList() else it.candidates) + result.value.candidates.filter { c -> (if (reset) emptyList() else it.candidates).none { x -> x.goatId == c.goatId } },
                        nextCursor = result.value.nextCursor,
                    )
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale candidates read failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(loading = false, loadFailed = true, message = result.message) }
                }
            }
        }
    }

    private fun review() {
        val l = local.value
        if (l.picked.isEmpty() || l.reviewInFlight) return
        viewModelScope.launch {
            local.update { it.copy(reviewInFlight = true, message = null) }
            when (val result = repository.previewAllocation(SaleAllocationRequestDto(dealId, l.picked.keys.toList()))) {
                is AppResult.Ok -> {
                    val p = result.value
                    local.update {
                        it.copy(
                            reviewInFlight = false,
                            step = SaleTagStep.REVIEW,
                            reviewGroups = p.shedGroups.map { g -> g.toUi() },
                            reviewBlocked = p.blockedAnimals,
                            reviewLine = buildString {
                                append("${p.sellable} ${if (p.sellable == 1) "animal" else "animals"} will be marked sold")
                                if (p.blocked > 0) append(" · ${p.blocked} cannot be")
                                if (p.declaredAnimalCount > 0) append(" · sale declares ${p.declaredAnimalCount}")
                                if (p.alreadyTagged > 0) append(" · ${p.alreadyTagged} already tagged")
                            },
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale allocation preview failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    local.update { it.copy(reviewInFlight = false, message = result.message) }
                }
            }
        }
    }

    private fun confirm() {
        val l = local.value
        if (l.picked.isEmpty() || l.confirmInFlight) return
        viewModelScope.launch {
            local.update { it.copy(confirmInFlight = true, message = null) }
            // The blocked animals are left out: confirming them would be refused whole.
            val blocked = l.reviewBlocked.map { it.goatId }.toSet()
            val ids = l.picked.keys.filterNot { it in blocked }
            when (val result = repository.confirmAllocation(l.confirmKey, SaleAllocationRequestDto(dealId, ids))) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_TAG_ANIMALS_CONFIRMED, mapOf(AnalyticsEvents.Params.REASON to result.value.allocated.toString()))
                    local.update {
                        it.copy(
                            confirmInFlight = false,
                            step = SaleTagStep.DONE,
                            reviewGroups = result.value.shedGroups.map { g -> g.toUi() },
                            doneLine = "${result.value.allocated} ${if (result.value.allocated == 1) "animal" else "animals"} marked sold.",
                        )
                    }
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale allocation confirm failed") }
                    analytics.track(AnalyticsEventsVendors.VENDORS_FAILURE, mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)))
                    // A refused confirm gets a FRESH key: the server may have recorded a partial
                    // state under the old one and an exact replay would return that, not retry.
                    local.update { it.copy(confirmInFlight = false, confirmKey = UUID.randomUUID().toString(), message = result.message) }
                }
            }
        }
    }

    private fun penKey(shedId: String, partition: String): String = if (partition.isBlank()) shedId else "$shedId|$partition"

    private fun splitPenKey(key: String): Pair<String?, String?> {
        if (key.isBlank()) return null to null
        val i = key.indexOf('|')
        return if (i < 0) key to null else key.substring(0, i) to key.substring(i + 1)
    }

    private companion object {
        const val SEARCH_DEBOUNCE_MS = 350L
        const val EMPTY_LOCATIONS_FAILED = "Could not load the parks and pens. Connect and try again."
        const val EMPTY_LOAD_FAILED = "Could not load the animals. Connect and try again."
        const val EMPTY_NO_PARK = "Pick a park to see its animals"
        const val EMPTY_NO_MATCH = "No animal matches that RFID or ID here"
        const val EMPTY_NONE = "No animals that can be sold in this pen"
    }
}
