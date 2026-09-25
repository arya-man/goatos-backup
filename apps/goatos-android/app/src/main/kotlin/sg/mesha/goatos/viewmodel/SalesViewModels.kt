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
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SaleRefreshResult
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.WorkflowsRepository
import sg.mesha.goatos.core.data.sync.SalesPaymentOp
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidateDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SaleShedGroupDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.WorkflowDetailResponseDto
import sg.mesha.goatos.feature.vendors.SaleBuyerListState
import sg.mesha.goatos.feature.vendors.SaleBuyerOptionUi
import sg.mesha.goatos.feature.vendors.SaleCandidateUi
import sg.mesha.goatos.feature.vendors.SaleCardUi
import sg.mesha.goatos.feature.vendors.SaleCreateEvent
import sg.mesha.goatos.feature.vendors.SaleCreateUiState
import sg.mesha.goatos.feature.vendors.SaleDetailEvent
import sg.mesha.goatos.feature.vendors.SaleDetailUiState
import sg.mesha.goatos.feature.vendors.SaleField
import sg.mesha.goatos.feature.vendors.SaleLineDraftUi
import sg.mesha.goatos.feature.vendors.SaleLineField
import sg.mesha.goatos.feature.vendors.SalePaymentEditorUi
import sg.mesha.goatos.feature.vendors.SalePaymentField
import sg.mesha.goatos.feature.vendors.SalePaymentUi
import sg.mesha.goatos.feature.vendors.SaleReviewAnimalUi
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

/** The recorded line kind survives catalog renames and kind changes; old cached rows lack it. */
internal fun SalesDealDto.hasAnimalsToTag(): Boolean {
    fun legacyAnimal(product: String) = product == "Sheep" || product == "Goat"
    return if (lines.isEmpty()) legacyAnimal(productType) else lines.any { line ->
        if (line.productKind.isBlank()) legacyAnimal(line.productType) else line.productKind == "animal"
    }
}

/**
 * The ONE input a sales refusal names: the envelope's `field` when the server sent one, otherwise
 * the field the sales module's stable `sales_invalid_<field>` code carries. Null when it names none.
 */
internal fun salesRefusedField(field: String?, code: String?): String? =
    field?.trim()?.takeIf { it.isNotEmpty() }
        ?: code?.trim()?.takeIf { it.startsWith(SALES_INVALID_PREFIX) }?.removePrefix(SALES_INVALID_PREFIX)?.takeIf { it.isNotEmpty() }

private const val SALES_INVALID_PREFIX = "sales_invalid_"

/** Where on the record-sale form a refused field lives. */
internal sealed interface SalesRefusedTarget {
    data class Deal(val field: SaleField) : SalesRefusedTarget
    /** [index] is 0-based; the backend numbers lines from 1. */
    data class Line(val index: Int, val field: SaleLineField) : SalesRefusedTarget
}

private val REFUSED_LINE = Regex("""^lines\[(\d+)]\.([a-z_]+)$""")

/** Maps a backend field name (`lines[2].rate_per_unit`, `buyer_name`) onto the form; null when it is not a box here. */
internal fun salesRefusedTarget(field: String?): SalesRefusedTarget? {
    if (field.isNullOrBlank()) return null
    fun lineField(name: String): SaleLineField? = when (name) {
        "product_type" -> SaleLineField.PRODUCT_TYPE
        "breed" -> SaleLineField.BREED
        "animal_count", "male_count", "female_count" -> SaleLineField.ANIMAL_COUNT
        "total_weight_kg" -> SaleLineField.TOTAL_WEIGHT_KG
        "sales_value" -> SaleLineField.SALES_VALUE
        "quantity" -> SaleLineField.QUANTITY
        "rate_per_unit" -> SaleLineField.RATE_PER_UNIT
        else -> null
    }
    REFUSED_LINE.matchEntire(field)?.let { m ->
        val number = m.groupValues[1].toIntOrNull() ?: return null
        val name = lineField(m.groupValues[2]) ?: return null
        return if (number >= 1) SalesRefusedTarget.Line(number - 1, name) else null
    }
    return when (field) {
        "sale_date" -> SalesRefusedTarget.Deal(SaleField.SALE_DATE)
        "farm" -> SalesRefusedTarget.Deal(SaleField.FARM)
        "buyer_vendor_id" -> SalesRefusedTarget.Deal(SaleField.BUYER_VENDOR_ID)
        "buyer_name" -> SalesRefusedTarget.Deal(SaleField.BUYER_NAME)
        "buyer_place" -> SalesRefusedTarget.Deal(SaleField.BUYER_PLACE)
        "advance_amount" -> SalesRefusedTarget.Deal(SaleField.ADVANCE_AMOUNT)
        "status" -> SalesRefusedTarget.Deal(SaleField.STATUS)
        "comments" -> SalesRefusedTarget.Deal(SaleField.COMMENTS)
        // A bare line field is the legacy single-product body's; it is the first line here.
        else -> lineField(field)?.let { SalesRefusedTarget.Line(0, it) }
    }
}

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

    /** The selected farm's OWN count, persisted beside its rows; never another filter's. */
    @OptIn(ExperimentalCoroutinesApi::class)
    private val scopeMeta = scope.map { it.farm }.distinctUntilChanged().flatMapLatest { repository.observeDealScope(it) }

    val state: StateFlow<SalesListUiState> = combine(_isRefreshing, scope, repository.observeOptions(), scopeMeta) { refreshing, current, options, meta ->
        val total = meta?.total ?: 0
        SalesListUiState(
            title = current.title,
            isRefreshing = refreshing,
            lastSyncedAt = meta?.syncedAt?.takeIf { it > 0L },
            filters = listOf(VendorsFilterUi("", FILTER_ALL, current.farm.isBlank())) +
                options?.farms.orEmpty().map { VendorsFilterUi(it, it, it == current.farm) },
            countLine = if (total > 0) "$total ${if (total == 1) COUNT_ONE else COUNT_MANY}" else "",
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
            SalesListEvent.OpenPipeline -> analytics.track(AnalyticsEventsVendors.VENDORS_PIPELINE_OPENED)
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
    productLine = dotJoin(soldSummary(), farm),
    valueLine = dotJoin(animalsLine(animalCount), kilograms(totalWeightKg), rupees(salesValue)),
    // Sheet-imported deals carry paise dust (₹0.18 on a fully paid sale); under a rupee reads as paid.
    metaLine = dotJoin("${saleDateWord(status, options).card} ${farmDate(saleDate)}", if (paymentBalance >= 1.0) "Balance ${rupees(paymentBalance)}" else "Fully paid"),
    statusLabel = status,
    statusTone = saleStatusTone(status, options),
)

/**
 * "Sheep · Anantapur" for a single-line sale; "Sheep + Goat · 3 lines" for a mixed one. The
 * deal-level product/breed read "Mixed" on a mixed sale -- a rollup word, not farm language --
 * so cards and subtitles say what was actually sold instead.
 */
internal fun SalesDealDto.soldSummary(): String {
    if (lines.size <= 1) return productAndBreed(productType, breed)
    val products = lines.map { it.productType }.distinct().joinToString(" + ")
    return dotJoin(products, "${lines.size} lines")
}

/**
 * The planned day of a sale that closed on a DIFFERENT day, else null: a sale recorded already
 * closed has none, and one closed on its planned day would only repeat "Sold on".
 */
/** The word a sale's date carries, on the card and on the detail. */
internal data class SaleDateWord(val card: String, val detail: String)

/**
 * A sale's date says what it is: an OPEN sale (In Discussion / Advance Paid) has not happened, so
 * its date is the day it is planned for, never "Sold"; a failed sale was never sold either. On the
 * phone (2026-09-26) an Advance Paid sale for 30/09 read "Sold 30/09/2026" four days before it.
 * Read through the same status grouping that colours the chip.
 */
internal fun saleDateWord(status: String, options: SalesOptionsDto? = null): SaleDateWord = when (saleStatusTone(status, options)) {
    VendorsTone.INFO, VendorsTone.WARN -> SaleDateWord("Planned", "Planned for")
    VendorsTone.DANGER -> SaleDateWord("Sale date", "Sale date")
    else -> SaleDateWord("Sold", "Sold on")
}

/** "Goat · Beetal"; a product whose "breed" is only its own name again ("Manure · Manure") says it once. */
internal fun productAndBreed(product: String, breed: String): String =
    if (breed.trim().equals(product.trim(), ignoreCase = true)) product else dotJoin(product, breed)

/** "20 kg at ₹40/kg" for a line sold by the unit; blank for a line sold by the head. */
internal fun quantityAtRate(quantity: Double?, unit: String, rate: Double?): String {
    if (quantity == null) return ""
    val amount = dotJoin(indianNumber(quantity, 1), unit).replace(" · ", " ")
    return if (rate == null) amount else "$amount at ${rupees(rate)}${unit.takeIf { it.isNotBlank() }?.let { "/$it" }.orEmpty()}"
}

/** A workflow that will not run any further (the sale failed and its steps were stopped). */
internal fun isStoppedWorkflow(state: String): Boolean = state.trim().lowercase() in setOf("canceled", "cancelled")

/**
 * The steps card's counter, from the backend's own counters. A STOPPED workflow does not read as
 * finished: on the phone (2026-09-26) a failed sale's workflow, cancelled after its tag step,
 * said "1 of 1 done" -- the cancelled steps drop out of the total, so the card claimed the work
 * was complete.
 */
internal fun saleStepsProgressLine(steps: WorkflowDetailResponseDto): String =
    if (isStoppedWorkflow(steps.state)) "Stopped · ${steps.actionsDone} done" else "${steps.actionsDone} of ${steps.actionsTotal} done"

/** A status a sale can never leave (Deal Failed), read through the same grouping as its chip. */
internal fun isFinalSaleStatus(status: String, options: SalesOptionsDto? = null): Boolean =
    saleStatusTone(status, options) == VendorsTone.DANGER

internal fun SalesDealDto.plannedSaleDateIfDifferent(): String? =
    plannedSaleDate?.trim()?.takeIf { it.isNotEmpty() && it != saleDate }

internal fun SalesDealDto.sections(): List<VendorsDetailSectionUi> {
    fun rows(vararg pairs: Pair<String, String?>) = pairs.mapNotNull { (label, value) -> value?.takeIf { it.isNotBlank() }?.let { VendorsDetailRowUi(label, it) } }
    // A mixed sale's product/breed read "Mixed" on the deal: the lines below say what was sold,
    // so the deal-level pair is shown only when there is one line for it to describe.
    val single = lines.size <= 1
    val sale = rows(
        saleDateWord(status).detail to farmDate(saleDate),
        "Planned for" to plannedSaleDateIfDifferent()?.let(::farmDate),
        "Farm" to farm,
        "Product" to productType.takeIf { single },
        "Breed" to breed.takeIf { single && !it.equals(productType, ignoreCase = true) },
        "Animals" to animalCount?.let { indianNumber(it, 0) },
        "Total weight" to totalWeightKg?.let(::kilograms),
    )
    // One row per line: "Sheep · Anantapur" -> "10 animals · 300 kg · ₹1,20,000".
    val sold = if (single) emptyList() else lines.map { line ->
        VendorsDetailRowUi(
            productAndBreed(line.productType, line.breed),
            dotJoin(animalsLine(line.animalCount), line.totalWeightKg?.let(::kilograms), quantityAtRate(line.quantity, line.unit, line.ratePerUnit), rupees(line.salesValue)),
        )
    }
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
        VendorsDetailSectionUi("What was sold", sold).takeIf { sold.isNotEmpty() },
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
    private val syncRepository: SyncRepository,
    private val workflows: WorkflowsRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {
    private val dealId: String = savedStateHandle.get<String>(Routes.SALE_ID_ARG).orEmpty()

    private data class Local(
        val refreshing: Boolean = false,
        val loaded: Boolean = false,
        // The sale's SOP workflow (SALES SOP, 2026-09-19): resolved by subject on refresh, then
        // observed from the Room detail cache under its workflow id like every other workflow.
        val stepsWorkflowId: String = "",
        val stepsUnavailable: Boolean = false,
        /** The server answered 404: the sale no longer exists. */
        val gone: Boolean = false,
        val taggedLine: String = "",
        val taggedGroups: List<SaleShedGroupUi> = emptyList(),
        val allocationRead: Boolean = false,
        val allocated: Int = 0,
        val message: String? = null,
        val paymentEditor: SalePaymentEditorUi? = null,
        /**
         * The idempotency keys of the receipt editor that is open, minted when it OPENS and dropped
         * when it closes. A double tap therefore replays one write, a refused save corrected and
         * saved again re-sends on the same key (the outbox re-opens its dead row), and the next
         * receipt -- a new editor -- gets a key of its own.
         */
        val paymentSaveKey: String = "",
        val paymentDeleteKey: String = "",
        val editInFlight: Boolean = false,
        val editMessage: String = "",
        // The feed store's refusal, and the status change waiting on the answer. Closing a sale is
        // when its feed actually leaves the store, so the store may refuse a close it never saw
        // when the sale was recorded.
        val stockConfirmMessage: String = "",
        val finalStatusPending: String = "",
        val stockPendingStatus: String = "",
        val stockPendingItemId: String = "",
    )

    private val local = MutableStateFlow(Local())

    init {
        refresh()
    }

    @OptIn(ExperimentalCoroutinesApi::class)
    private val stepsDetail: Flow<WorkflowDetailResponseDto?> = local.map { it.stepsWorkflowId }.distinctUntilChanged()
        .flatMapLatest { id -> if (id.isBlank()) flowOf(null) else workflows.observeDetail(id) }

    val state: StateFlow<SaleDetailUiState> = combine(repository.observeDeal(dealId), repository.observeOptions(), local, stepsDetail) { deal, options, l, steps ->
        if (deal == null) {
            // Loaded and still nothing: say so, with a way to try again, rather than an empty
            // screen with a blank title and chip. There is no per-sale read on the server yet, so
            // the phone knows a sale only through the ledger pages it has loaded.
            SaleDetailUiState(isRefreshing = l.refreshing, isLoading = !l.loaded && !l.gone, notFound = l.loaded && !l.gone, gone = l.gone, message = l.message)
        } else {
            val declared = deal.animalCount?.toInt() ?: 0
            val complete = declared > 0 && l.allocated >= declared
            val hasAnimals = deal.hasAnimalsToTag()
            val live = hasAnimals && deal.status != "Deal Failed" && !complete
            SaleDetailUiState(
                title = deal.buyerName,
                subtitle = dotJoin(deal.soldSummary(), deal.farm, farmDate(deal.saleDate)),
                statusLabel = deal.status,
                statusTone = saleStatusTone(deal.status, options),
                sections = deal.sections(),
                taggedLine = l.taggedLine,
                taggedGroups = l.taggedGroups,
                payments = deal.payments.map { it.toUi() },
                // The BACKEND's balance, formatted. Sheet-imported deals carry paise dust, so
                // under a rupee reads as paid -- the same rule the ledger card uses.
                balanceLine = if (deal.paymentBalance >= 1.0) "${rupees(deal.paymentBalance)} still due" else "Fully paid",
                // The backend decides what THIS deal may move to; an empty list hides the editor.
                statuses = options?.statuses.orEmpty()
                    .filter { option -> deal.statusOptions?.contains(option.key) ?: true }
                    .map { VendorsOptionUi(it.key, it.label) },
                paymentEditor = l.paymentEditor,
                today = LocalDate.now().toString(),
                editInFlight = l.editInFlight,
                editMessage = l.editMessage,
                stockConfirmMessage = l.stockConfirmMessage,
                finalStatusPending = l.finalStatusPending,
                canTagAnimals = live,
                tagDisabledReason = when {
                    !hasAnimals -> TAG_NON_ANIMAL
                    deal.status == "Deal Failed" -> TAG_FAILED
                    complete -> "All $declared ${if (declared == 1) "animal is" else "animals are"} tagged. Tag more on the web if the count changes."
                    else -> ""
                },
                stepsWorkflowId = l.stepsWorkflowId,
                // The backend card's counters and next step, verbatim; the phone never recounts.
                stepsProgressLine = steps?.let(::saleStepsProgressLine).orEmpty(),
                stepsNextLine = steps?.takeUnless { isStoppedWorkflow(it.state) }?.nextAction?.title?.takeIf { it.isNotBlank() }?.let { "Next: $it" }.orEmpty(),
                stepsUnavailable = l.stepsUnavailable,
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
            is SaleDetailEvent.OpenSteps -> analytics.track(AnalyticsEventsVendors.VENDORS_SALE_STEPS_OPENED)
            SaleDetailEvent.DismissMessage -> local.update { it.copy(message = null) }
            SaleDetailEvent.Back -> Unit
            is SaleDetailEvent.OpenPayment -> openPaymentEditor(event.paymentId)
            SaleDetailEvent.ClosePayment -> local.update { it.copy(paymentEditor = null) }
            is SaleDetailEvent.PaymentFieldChanged -> local.update { l ->
                val editor = l.paymentEditor ?: return@update l
                l.copy(
                    paymentEditor = editor.copy(
                        values = editor.values + (event.field to event.value),
                        fieldErrors = editor.fieldErrors - event.field,
                    ),
                )
            }
            SaleDetailEvent.SavePayment -> savePayment()
            SaleDetailEvent.DeletePayment -> deletePayment()
            // A final status (Deal Failed) cannot be taken back and releases the sale's animals, so
            // picking it asks first; every other status is written as it is picked.
            is SaleDetailEvent.ChangeStatus ->
                if (isFinalSaleStatus(event.status)) {
                    local.update { it.copy(finalStatusPending = event.status) }
                } else {
                    changeStatus(event.status)
                }
            SaleDetailEvent.ConfirmFinalStatus -> {
                val pending = local.value.finalStatusPending
                local.update { it.copy(finalStatusPending = "") }
                if (pending.isNotBlank()) changeStatus(pending)
            }
            SaleDetailEvent.DismissFinalStatus -> local.update { it.copy(finalStatusPending = "") }
            SaleDetailEvent.ConfirmStatusStock -> {
                val pending = local.value.stockPendingStatus
                val itemId = local.value.stockPendingItemId
                if (itemId.isNotBlank() && !local.value.editInFlight) enqueueEdit(null, MESSAGE_STATUS_SAVED, "sale confirmation failed", pending) {
                    when (val result = syncRepository.confirmSalesStock(itemId)) {
                        is AppResult.Ok -> {
                            local.update { it.copy(stockConfirmMessage = "", stockPendingStatus = "", stockPendingItemId = "") }
                            AppResult.Ok(itemId)
                        }
                        is AppResult.Err -> result
                    }
                }
            }
            SaleDetailEvent.DismissStatusStock ->
                local.update { it.copy(stockConfirmMessage = "", stockPendingStatus = "", stockPendingItemId = "") }
        }
    }

    private fun openPaymentEditor(paymentId: String) {
        val existing = state.value.payments.firstOrNull { it.paymentId == paymentId }
        analytics.track(AnalyticsEventsVendors.VENDORS_SALE_PAYMENT_OPENED)
        local.update {
            it.copy(
                editMessage = "",
                paymentSaveKey = UUID.randomUUID().toString(),
                paymentDeleteKey = UUID.randomUUID().toString(),
                paymentEditor = SalePaymentEditorUi(
                    paymentId = paymentId,
                    // Correcting a receipt starts from what it says now; adding one starts on
                    // today, because a receipt is normally entered the day the money arrives.
                    values = if (existing == null) {
                        mapOf(SalePaymentField.RECEIVED_ON to LocalDate.now().toString())
                    } else {
                        mapOf(
                            SalePaymentField.RECEIVED_ON to existing.receivedOnIso,
                            SalePaymentField.AMOUNT to existing.amountRaw,
                            SalePaymentField.NOTE to existing.note,
                        )
                    },
                ),
            )
        }
    }

    private fun savePayment() {
        val current = local.value
        val editor = current.paymentEditor ?: return
        // A second tap while the first is still on its way is the same save, not another receipt.
        if (current.editInFlight || editor.inFlight) return
        val errors = validatePayment(editor.values)
        if (errors.isNotEmpty()) {
            local.update { it.copy(paymentEditor = editor.copy(fieldErrors = errors)) }
            return
        }
        val amount = editor.values[SalePaymentField.AMOUNT].orEmpty().trim().toDoubleOrNull() ?: return
        val request = sg.mesha.goatos.core.network.dto.SalesDealPaymentWriteDto(
            receivedOn = editor.values[SalePaymentField.RECEIVED_ON].orEmpty().trim(),
            amountRupees = amount,
            note = editor.values[SalePaymentField.NOTE].orEmpty().trim(),
        )
        val creating = editor.paymentId.isBlank()
        enqueueEdit(
            editor = editor,
            done = if (creating) MESSAGE_PAYMENT_ADDED else MESSAGE_PAYMENT_SAVED,
            failure = "payment write enqueue failed",
        ) {
            syncRepository.enqueueSalesDealPaymentWrite(
                // The editor's key: stable across taps and a refused-then-corrected save, fresh for
                // each editor opened -- so correcting a receipt twice still records both corrections.
                clientId = current.paymentSaveKey,
                dealId = dealId,
                op = if (creating) SalesPaymentOp.CREATE else SalesPaymentOp.UPDATE,
                paymentId = editor.paymentId,
                request = request,
            )
        }
    }

    private fun deletePayment() {
        val current = local.value
        val editor = current.paymentEditor ?: return
        if (editor.paymentId.isBlank() || current.editInFlight || editor.inFlight) return
        enqueueEdit(editor = editor, done = MESSAGE_PAYMENT_REMOVED, failure = "payment delete enqueue failed") {
            syncRepository.enqueueSalesDealPaymentWrite(
                clientId = current.paymentDeleteKey,
                dealId = dealId,
                op = SalesPaymentOp.DELETE,
                paymentId = editor.paymentId,
            )
        }
    }

    private fun changeStatus(status: String, acknowledgeStock: Boolean = false) {
        if (status.isBlank() || local.value.editInFlight) return
        enqueueEdit(
            editor = null,
            done = MESSAGE_STATUS_SAVED,
            failure = "deal status enqueue failed",
            // Closing a sale takes its feed off the store, and the store may say it does not hold
            // it. That is a QUESTION and not a failure, so the row is not left reading as rejected
            // -- the screen asks it, and the same change is sent again with the answer.
            stockStatus = status,
        ) {
            syncRepository.enqueueSalesDealStatusSet(
                clientId = UUID.randomUUID().toString(),
                dealId = dealId,
                status = status,
                acknowledgeStock = acknowledgeStock,
            )
        }
    }

    /**
     * Queues one edit, then follows the exact outbox row it created. Enqueue means durable on this
     * phone, not accepted by the server, so the editor only closes once the row is saved or after
     * the offline grace period. A terminal rejection puts the fields back with the server reason.
     */
    private fun enqueueEdit(
        editor: SalePaymentEditorUi?,
        done: String,
        failure: String,
        stockStatus: String = "",
        block: suspend () -> AppResult<String>,
    ) {
        // In flight BEFORE the coroutine starts, so a second tap in the same frame is refused by
        // the guard in its caller rather than racing this one to the outbox.
        local.update { it.copy(editInFlight = true, paymentEditor = editor?.copy(inFlight = true) ?: it.paymentEditor) }
        viewModelScope.launch {
            when (val result = block()) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_SALE_EDITED)
                    local.update { it.copy(editMessage = MESSAGE_SAVING, message = null) }
                    followWrite(result.value, editor, done, stockStatus)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, failure) }
                    analytics.track(
                        AnalyticsEventsVendors.VENDORS_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)),
                    )
                    local.update {
                        it.copy(
                            editInFlight = false,
                            paymentEditor = editor?.copy(inFlight = false) ?: it.paymentEditor,
                            message = MESSAGE_EDIT_FAILED,
                            editMessage = "",
                        )
                    }
                }
            }
        }
    }

    private fun followWrite(outboxItemId: String, editor: SalePaymentEditorUi?, done: String, stockStatus: String = "") {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(outboxItemId).collect { outcome ->
                local.update {
                    // The feed store's question, not a refusal: keyed on the server's CODE, never
                    // on its sentence, which is farm copy and may be reworded.
                    if (outcome is QueuedWriteOutcome.Rejected && outcome.code == CODE_STOCK_CONFIRM && stockStatus.isNotBlank()) {
                        return@update it.copy(
                            editInFlight = false,
                            editMessage = "",
                            message = null,
                            stockConfirmMessage = outcome.reason.orEmpty(),
                            stockPendingStatus = stockStatus,
                            stockPendingItemId = outboxItemId,
                        )
                    }
                    when (outcome) {
                        QueuedWriteOutcome.Saved -> it.copy(
                            editInFlight = false,
                            paymentEditor = null,
                            editMessage = done,
                            message = null,
                        )
                        QueuedWriteOutcome.StillQueued -> it.copy(
                            editInFlight = false,
                            paymentEditor = null,
                            editMessage = MESSAGE_QUEUED_OFFLINE,
                            message = null,
                        )
                        is QueuedWriteOutcome.Rejected -> it.copy(
                            editInFlight = false,
                            paymentEditor = editor?.copy(inFlight = false) ?: it.paymentEditor,
                            editMessage = "",
                            message = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_EDIT_FAILED,
                        )
                    }
                }
            }
        }
    }

    private fun validatePayment(values: Map<SalePaymentField, String>): Map<SalePaymentField, String> {
        val errors = mutableMapOf<SalePaymentField, String>() // mobile-guard:ignore: at most one entry per form field, returned and dropped
        val date = values[SalePaymentField.RECEIVED_ON].orEmpty().trim()
        if (date.isBlank()) errors[SalePaymentField.RECEIVED_ON] = REQUIRED
        val amount = values[SalePaymentField.AMOUNT].orEmpty().trim()
        val parsed = amount.toDoubleOrNull()
        when {
            amount.isBlank() -> errors[SalePaymentField.AMOUNT] = REQUIRED
            parsed == null -> errors[SalePaymentField.AMOUNT] = NOT_A_NUMBER
            parsed <= 0.0 -> errors[SalePaymentField.AMOUNT] = MORE_THAN_ZERO
        }
        return errors
    }

    private fun refresh() {
        viewModelScope.launch {
            local.update { it.copy(refreshing = true) }
            try {
                // Re-read THIS sale (GET /sales/deals/{id}) into the ledger row the screen reads.
                // Dropping the All-scope paging cursor here instead used to stop the list paging.
                when (repository.refreshDeal(dealId)) {
                    SaleRefreshResult.GONE -> local.update { it.copy(gone = true) }
                    SaleRefreshResult.FRESH -> local.update { it.copy(gone = false) }
                    SaleRefreshResult.UNREACHABLE -> Unit
                }
                // The sale's SOP steps, keyed on the deal. Blank = not opened yet (the recorded
                // event still in flight); failure = offline, the cached detail stays visible.
                workflows.refreshDetailBySubject(SALE_WORKFLOW_TEMPLATE_KEY, dealId)
                    .onSuccess { id -> local.update { it.copy(stepsWorkflowId = id.ifBlank { it.stepsWorkflowId }, stepsUnavailable = false) } }
                    .onFailure { t ->
                        crashReporter.recordException(t, "sale steps read failed")
                        local.update { it.copy(stepsUnavailable = it.stepsWorkflowId.isBlank()) }
                    }
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
        /** tasks/domain.TemplateKeySalesDeal -- the sale workflow's template key. */
        const val SALE_WORKFLOW_TEMPLATE_KEY = "sales_deal"
        const val TAG_NON_ANIMAL = "This sale has no animals to tag."
        const val TAG_FAILED = "A failed deal has no animals to tag."
        /** The feed store's own refusal, which the screen offers to answer. */
        const val CODE_STOCK_CONFIRM = "feed_stock_confirmation_required"
        const val KIND_FEED = "feed"
        const val REQUIRED = "Required"
        const val NOT_A_NUMBER = "Enter a number"
        const val MORE_THAN_ZERO = "Must be more than zero"
        // Shown only once the server HAS the change (QueuedWriteOutcome.Saved); a change still on
        // the phone says MESSAGE_QUEUED_OFFLINE instead. No future tense here.
        const val MESSAGE_PAYMENT_ADDED = "Payment added."
        const val MESSAGE_PAYMENT_SAVED = "Payment saved."
        const val MESSAGE_PAYMENT_REMOVED = "Payment removed."
        const val MESSAGE_STATUS_SAVED = "Status saved."
        const val MESSAGE_SAVING = "Saving change…"
        const val MESSAGE_QUEUED_OFFLINE = "Saved on this phone. It reaches the ledger when the phone is online."
        const val MESSAGE_EDIT_FAILED = "Could not save that change. Try again."
    }
}

/** One recorded receipt, formatted for the card and carrying its raw values for the editor. */
private fun sg.mesha.goatos.core.network.dto.SalesDealPaymentDto.toUi(): SalePaymentUi = SalePaymentUi(
    paymentId = paymentId,
    receivedOn = farmDate(receivedOn),
    amount = rupees(amountRupees),
    note = note.orEmpty(),
    receivedOnIso = receivedOn,
    // Trailing ".0" would be typed back into the field verbatim; a whole rupee shows as one.
    amountRaw = if (amountRupees % 1.0 == 0.0) amountRupees.toLong().toString() else amountRupees.toString(),
)

/** The record-sale wizard: three steps, offline-first write with a stable client id. */
@HiltViewModel
class SaleCreateViewModel @Inject constructor(
    private val savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    /** One product line as typed; validation errors ride on it so the card shows its own. */
    private data class LineDraft(
        val id: Int,
        val product: String = "",
        val breed: String = "",
        val animals: String = "",
        val weightKg: String = "",
        val value: String = "",
        /** A line priced by the unit: how much, and at what rate. */
        val quantity: String = "",
        val rate: String = "",
        val errors: Map<SaleLineField, String> = emptyMap(),
    )

    private data class Local(
        val step: Int = 0,
        val values: Map<SaleField, String> = mapOf(SaleField.SALE_DATE to todayIst()),
        /** ONE sale, MANY lines (maintainer decision 2026-09-12). Starts with one blank line. */
        val lines: List<LineDraft> = listOf(LineDraft(id = 1)),
        val buyerSearch: String = "",
        val fieldErrors: Map<SaleField, String> = emptyMap(),
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val closeAfterSave: Boolean = false,
        val submitInFlight: Boolean = false,
        val message: String? = null,
        val buyersRefreshed: Boolean = false,
        val stockPendingItemId: String = "",
        /** The feed store asked for this sale to be confirmed; its sentence, shown verbatim. */
        val stockConfirmMessage: String = "",
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
        // A line with no product yet is offered the first product, so the common one-product
        // sale needs no tap on the product row.
        val defaultProduct = o.productTypes.firstOrNull().orEmpty()
        val lines = l.lines.map { line ->
            val product = line.product.ifBlank { defaultProduct }
            // What the line ASKS comes from the registry row, never from this screen reading the
            // kind: an item the farm adds itself is asked the same questions the web asks.
            val row = o.products.firstOrNull { it.name == product }
            val variants = o.breeds[product].orEmpty()
            // An item whose only variant is its own name -- manure, tags -- is not a choice. It is
            // filled in rather than asked, so the line is complete without a list of one.
            val breed = line.breed.ifBlank { product.takeIf { variants.size == 1 && variants.first() == product }.orEmpty() }
            SaleLineDraftUi(
                id = line.id, product = product, breed = breed,
                animals = line.animals, weightKg = line.weightKg, value = line.value,
                quantity = line.quantity, rate = line.rate,
                pricedPerUnit = row?.pricedPerUnit ?: false,
                unit = row?.unit.orEmpty(),
                isFeed = row?.kind == KIND_FEED,
                breeds = variants.map { VendorsOptionUi(it, it) },
                errors = line.errors,
            )
        }
        val totals = lineTotals(l.lines)
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
            lines = lines,
            totalLine = dotJoin(rupees(totals.value), animalsLine(totals.animals.takeIf { it > 0 }), totals.weightKg.takeIf { it > 0 }?.let(::kilograms)),
            totalValueLine = rupees(totals.value),
            canAddLine = l.lines.size < MAX_LINES,
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
            contextLine = dotJoin(values[SaleField.FARM], linesSummary(lines), values[SaleField.BUYER_NAME], totals.value.takeIf { it > 0 }?.let(::rupees)),
            today = todayIst(),
            maxDate = LocalDate.now(VENDORS_IST).plusDays(o.maxSaleDateDaysAhead.toLong()).toString(),
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            stockConfirmMessage = l.stockConfirmMessage,
            closeAfterSave = l.closeAfterSave,
            submitInFlight = l.submitInFlight,
            message = l.message,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SaleCreateUiState())

    fun onEvent(event: SaleCreateEvent) {
        // A queued or accepted write is read-only: the last step stays on screen for the banner
        // and closes by itself, so an edit or a second submit in that window has nothing to land on.
        val locked = local.value.submitInFlight || local.value.writeStatus == VendorsWriteStatus.QUEUED || local.value.writeStatus == VendorsWriteStatus.SYNCED
        if (locked && event !is SaleCreateEvent.Back && event !is SaleCreateEvent.RecordAnother && event !is SaleCreateEvent.DismissMessage) return
        if (event is SaleCreateEvent.FieldChanged || event is SaleCreateEvent.LineChanged ||
            event is SaleCreateEvent.BuyerPicked || event is SaleCreateEvent.RemoveLine || event == SaleCreateEvent.AddLine) {
            // Editing means a new question must be checked; never confirm an older saved payload.
            local.update { it.copy(stockConfirmMessage = "", stockPendingItemId = "") }
        }
        when (event) {
            is SaleCreateEvent.FieldChanged -> local.update { l ->
                l.copy(values = l.values + (event.field to event.value), fieldErrors = l.fieldErrors - event.field)
            }
            is SaleCreateEvent.LineChanged -> local.update { l ->
                l.copy(lines = l.lines.map { line ->
                    if (line.id != event.lineId) line else when (event.field) {
                        // A product change empties everything it decided the shape of: a variant
                        // from the previous product's list, or kilograms typed against a feed,
                        // must never ride along into the submit of a different product.
                        SaleLineField.PRODUCT_TYPE ->
                            if (line.product == event.value) line.copy(product = event.value, errors = line.errors - event.field)
                            else LineDraft(id = line.id, product = event.value)
                        SaleLineField.BREED -> line.copy(breed = event.value, errors = line.errors - event.field)
                        SaleLineField.ANIMAL_COUNT -> line.copy(animals = event.value, errors = line.errors - event.field)
                        SaleLineField.TOTAL_WEIGHT_KG -> line.copy(weightKg = event.value, errors = line.errors - event.field)
                        SaleLineField.SALES_VALUE -> line.copy(value = event.value, errors = line.errors - event.field)
                        SaleLineField.QUANTITY -> line.copy(quantity = event.value, errors = line.errors - event.field)
                        SaleLineField.RATE_PER_UNIT -> line.copy(rate = event.value, errors = line.errors - event.field)
                    }
                })
            }
            SaleCreateEvent.AddLine -> local.update { l ->
                if (l.lines.size >= MAX_LINES) l
                // A new line starts on the LAST line's product: two breeds of one product is the
                // common mixed sale, so it needs one fewer tap than two products.
                else l.copy(lines = l.lines + LineDraft(id = (l.lines.maxOfOrNull { it.id } ?: 0) + 1, product = state.value.lines.lastOrNull()?.product.orEmpty()))
            }
            is SaleCreateEvent.RemoveLine -> local.update { l ->
                if (l.lines.size <= 1) l else l.copy(lines = l.lines.filterNot { it.id == event.lineId })
            }
            is SaleCreateEvent.BuyerSearchChanged -> local.update { it.copy(buyerSearch = event.text) }
            is SaleCreateEvent.BuyerPicked -> pickBuyer(event.vendorId)
            SaleCreateEvent.Next -> next()
            SaleCreateEvent.Previous -> local.update { it.copy(step = (it.step - 1).coerceAtLeast(0)) }
            SaleCreateEvent.Submit -> submit()
            SaleCreateEvent.ConfirmStockAndSubmit -> {
                val itemId = local.value.stockPendingItemId
                if (itemId.isNotBlank() && !local.value.submitInFlight) {
                    local.update { it.copy(submitInFlight = true) }
                    viewModelScope.launch {
                        when (val result = syncRepository.confirmSalesStock(itemId)) {
                            is AppResult.Ok -> {
                                local.update { it.copy(submitInFlight = false, stockConfirmMessage = "", writeStatus = VendorsWriteStatus.QUEUED) }
                                followWrite(itemId)
                            }
                            is AppResult.Err -> local.update { it.copy(submitInFlight = false, message = result.message) }
                        }
                    }
                }
            }
            SaleCreateEvent.DismissStockConfirm -> local.update { it.copy(stockConfirmMessage = "") }
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
        val lineErrors = if (local.value.step == 0) validateLines(state.value.lines) else emptyMap()
        if (errors.isNotEmpty() || lineErrors.isNotEmpty()) {
            local.update { it.copy(fieldErrors = errors, lines = it.lines.withErrors(lineErrors)) }
            return
        }
        local.update { it.copy(step = (it.step + 1).coerceAtMost(STEP_COUNT - 1), fieldErrors = emptyMap()) }
    }

    private fun submit(acknowledgeStock: Boolean = false) {
        val values = state.value.values
        val errors = (0 until STEP_COUNT).fold(emptyMap<SaleField, String>()) { acc, step -> acc + validate(step, values) }
        val lineErrors = validateLines(state.value.lines)
        if (errors.isNotEmpty() || lineErrors.isNotEmpty()) {
            val firstStep = if (lineErrors.isNotEmpty()) 0 else (0 until STEP_COUNT).first { validate(it, values).isNotEmpty() }
            local.update { it.copy(step = firstStep, fieldErrors = errors, lines = it.lines.withErrors(lineErrors)) }
            return
        }
        viewModelScope.launch {
            local.update { it.copy(submitInFlight = true, message = null) }
            when (val result = syncRepository.enqueueSalesDealCreate(clientId, values.toWrite(state.value.lines, acknowledgeStock))) {
                is AppResult.Ok -> {
                    analytics.track(AnalyticsEventsVendors.VENDORS_SALE_QUEUED)
                    local.update { it.copy(submitInFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_SAVING) }
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sale create enqueue failed") }
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
                        is QueuedWriteOutcome.Rejected ->
                            // A SHORT FEED SALE is not a failure: the store's ledger disagrees, and
                            // the person may well be right -- the load reached the farm and nobody
                            // has recorded it yet. The sale stays on screen with what the store
                            // says, and they can send it again saying they checked. Keyed on the
                            // server's CODE; the sentence is its copy and is shown verbatim.
                            if (outcome.code == CODE_STOCK_CONFIRM) {
                                it.copy(
                                    writeStatus = VendorsWriteStatus.IDLE,
                                    writeMessage = "",
                                    closeAfterSave = false,
                                    stockConfirmMessage = outcome.reason.orEmpty(),
                                    stockPendingItemId = itemId,
                                )
                            } else {
                                it.refusedBy(outcome)
                            }
                    }
                }
            }
        }
    }
    /**
     * The server refused the sale. Its sentence is backend-owned copy and is shown verbatim, and
     * when it names a field the sentence also lands on THAT box -- a line's box when it names a
     * line -- with the wizard back on the step that holds it. The form keeps everything typed, and
     * the next submit re-sends on the same key (the outbox re-opens its dead row).
     */
    private fun Local.refusedBy(outcome: QueuedWriteOutcome.Rejected): Local {
        val reason = outcome.reason?.trim().orEmpty().ifBlank { MESSAGE_NOT_SAVED }
        val base = copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = reason)
        return when (val target = salesRefusedTarget(salesRefusedField(outcome.field, outcome.code))) {
            null -> base
            is SalesRefusedTarget.Deal -> base.copy(
                step = SALE_FIELD_STEP[target.field] ?: base.step,
                fieldErrors = base.fieldErrors + (target.field to reason),
            )
            is SalesRefusedTarget.Line -> {
                val line = base.lines.getOrNull(target.index) ?: return base
                // A line sold by the unit shows no value box: its value IS quantity x rate, so a
                // refused value is the rate the person typed.
                val priced = line.quantity.isNotBlank() || line.rate.isNotBlank()
                val field = if (target.field == SaleLineField.SALES_VALUE && priced) SaleLineField.RATE_PER_UNIT else target.field
                base.copy(
                    step = 0,
                    lines = base.lines.map { if (it.id == line.id) it.copy(errors = it.errors + (field to reason)) else it },
                )
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
            }
            1 -> {
                if (v[SaleField.BUYER_VENDOR_ID].isNullOrBlank()) errors[SaleField.BUYER_VENDOR_ID] = PICK_BUYER
                val name = v[SaleField.BUYER_NAME].orEmpty().trim()
                if (name.isBlank()) errors[SaleField.BUYER_NAME] = REQUIRED
                else if (name.length > MAX_SHORT) errors[SaleField.BUYER_NAME] = TOO_LONG
                if (v[SaleField.BUYER_PLACE].orEmpty().trim().length > MAX_SHORT) errors[SaleField.BUYER_PLACE] = TOO_LONG
            }
            else -> {
                // The sale value is the SUM of the lines (validated on step 0); the advance is
                // checked against that sum, never against a figure typed here.
                val value = lineTotals(local.value.lines).value
                nonNegative(SaleField.ADVANCE_AMOUNT)
                val advance = v[SaleField.ADVANCE_AMOUNT].orEmpty().trim().toDoubleOrNull()
                if (advance != null && value > 0.0 && advance > value) errors[SaleField.ADVANCE_AMOUNT] = ADVANCE_OVER
                if (v[SaleField.STATUS].isNullOrBlank()) errors[SaleField.STATUS] = REQUIRED
                if (v[SaleField.COMMENTS].orEmpty().length > MAX_COMMENTS) errors[SaleField.COMMENTS] = TOO_LONG
            }
        }
        return errors
    }

    /** Per-line checks; keyed by line id, only the lines with a problem appear. */
    private fun validateLines(lines: List<SaleLineDraftUi>): Map<Int, Map<SaleLineField, String>> {
        val out = mutableMapOf<Int, Map<SaleLineField, String>>() // mobile-guard:ignore: per-call validation result, bounded by MAX_LINES, returned and dropped
        for (line in lines) {
            val errors = mutableMapOf<SaleLineField, String>() // mobile-guard:ignore: per-call validation result, at most one entry per line field
            if (line.product.isBlank()) errors[SaleLineField.PRODUCT_TYPE] = REQUIRED
            if (line.breed.isBlank()) errors[SaleLineField.BREED] = REQUIRED
            if (line.pricedPerUnit) {
                // The quantity IS the sale for an item sold by the kilogram or by the piece: a
                // line without one takes the money and says nothing about what left the farm.
                val quantity = line.quantity.trim().toDoubleOrNull()
                if (quantity == null || quantity <= 0.0) errors[SaleLineField.QUANTITY] = MORE_THAN_ZERO
                // The backend works the line's value out as quantity x rate and refuses a value
                // that is not more than zero (deal_lines.go), so a rate of zero is refused here
                // rather than queued to be refused later.
                val rate = line.rate.trim().toDoubleOrNull()
                if (rate == null || rate <= 0.0) errors[SaleLineField.RATE_PER_UNIT] = MORE_THAN_ZERO
                if (errors.isNotEmpty()) out[line.id] = errors
                continue
            }
            val animals = line.animals.trim()
            if (animals.isNotBlank()) {
                val n = animals.toDoubleOrNull()
                if (n == null || n < 0.0 || n != Math.floor(n)) errors[SaleLineField.ANIMAL_COUNT] = WHOLE_NUMBER
            }
            val weight = line.weightKg.trim()
            if (weight.isNotBlank()) {
                val n = weight.toDoubleOrNull()
                if (n == null || n < 0.0) errors[SaleLineField.TOTAL_WEIGHT_KG] = AMOUNT
            }
            val value = line.value.trim().toDoubleOrNull()
            if (value == null || value <= 0.0) errors[SaleLineField.SALES_VALUE] = MORE_THAN_ZERO
            if (errors.isNotEmpty()) out[line.id] = errors
        }
        return out
    }

    private fun List<LineDraft>.withErrors(errors: Map<Int, Map<SaleLineField, String>>): List<LineDraft> =
        map { it.copy(errors = errors[it.id].orEmpty()) }

    private data class LineTotals(val value: Double, val animals: Double, val weightKg: Double)

    /** The running total the phone previews. The recorded figure is the backend's rollup. */
    private fun lineTotals(lines: List<LineDraft>): LineTotals {
        fun num(raw: String) = raw.trim().toDoubleOrNull()?.takeIf { it >= 0.0 } ?: 0.0
        // A line priced by the unit is worth quantity x rate, worked out the way the backend works
        // it out -- so the running total the person watches while typing is the total recorded.
        fun value(l: LineDraft) =
            if (l.quantity.isNotBlank() && l.rate.isNotBlank()) num(l.quantity) * num(l.rate) else num(l.value)
        return LineTotals(
            value = lines.sumOf(::value),
            animals = lines.sumOf { num(it.animals) },
            weightKg = lines.sumOf { num(it.weightKg) },
        )
    }

    /** "Sheep · Anantapur" for one line, "3 lines" for several. */
    private fun linesSummary(lines: List<SaleLineDraftUi>): String = when {
        lines.size == 1 -> productAndBreed(lines[0].product, lines[0].breed)
        else -> "${lines.size} lines"
    }

    private fun Map<SaleField, String>.toWrite(lines: List<SaleLineDraftUi>, acknowledgeStock: Boolean = false): SalesDealWriteDto {
        fun number(raw: String): Double? = raw.trim().ifBlank { null }?.toDoubleOrNull()
        fun number(f: SaleField): Double? = number(get(f).orEmpty())
        return SalesDealWriteDto(
            saleDate = get(SaleField.SALE_DATE).orEmpty(),
            farm = get(SaleField.FARM).orEmpty(),
            lines = lines.map { line ->
                // A line priced by the unit sends its quantity and rate and NO value: the backend
                // works the money out, so a figure this screen computed can never be what is
                // recorded. An animal line sends its counts and the price agreed for the lot.
                if (line.pricedPerUnit) {
                    SalesDealLineWriteDto(
                        productType = line.product,
                        breed = line.breed,
                        quantity = number(line.quantity),
                        ratePerUnit = number(line.rate),
                        salesValue = 0.0,
                    )
                } else {
                    SalesDealLineWriteDto(
                        productType = line.product,
                        breed = line.breed,
                        animalCount = number(line.animals),
                        totalWeightKg = number(line.weightKg),
                        salesValue = line.value.trim().toDouble(),
                    )
                }
            },
            buyerName = get(SaleField.BUYER_NAME).orEmpty().trim(),
            buyerPlace = get(SaleField.BUYER_PLACE).orEmpty().trim(),
            buyerVendorId = get(SaleField.BUYER_VENDOR_ID).orEmpty(),
            advanceAmount = number(SaleField.ADVANCE_AMOUNT),
            comments = get(SaleField.COMMENTS).orEmpty().trim(),
            status = get(SaleField.STATUS).orEmpty(),
            stockShortfallAcknowledged = acknowledgeStock,
        )
    }

    private companion object {
        const val KEY_CLIENT_ID = "sale_create_client_id"
        const val STEP_COUNT = 3
        /** Which wizard step holds each deal-level box. */
        val SALE_FIELD_STEP = mapOf(
            SaleField.SALE_DATE to 0, SaleField.FARM to 0,
            SaleField.BUYER_VENDOR_ID to 1, SaleField.BUYER_NAME to 1, SaleField.BUYER_PLACE to 1,
            SaleField.ADVANCE_AMOUNT to 2, SaleField.STATUS to 2, SaleField.COMMENTS to 2,
        )
        /** Mirrors the backend's MaxDealLines. */
        const val MAX_LINES = 20
        const val BUYER_LIST_PREVIEW = 8
        const val BUYER_LIST_MAX = 20
        const val MAX_SHORT = 160
        const val MAX_COMMENTS = 2000
        /** The feed store's own refusal, which the screen offers to answer. */
        const val CODE_STOCK_CONFIRM = "feed_stock_confirmation_required"
        const val KIND_FEED = "feed"
        const val REQUIRED = "Required"
        const val PICK_BUYER = "Pick the buyer from the vendor register"
        const val MORE_THAN_ZERO = "Must be more than zero"
        const val AMOUNT = "Enter an amount, zero or more"
        const val WHOLE_NUMBER = "Whole number, zero or more"
        const val ADVANCE_OVER = "Cannot be more than the sale value"
        const val TOO_FAR = "Too far ahead — within 60 days of today"
        const val TOO_LONG = "Too long"
        const val MESSAGE_SAVING = "Saving sale…"
        const val MESSAGE_SAVED = "Sale saved to the ledger."
        const val MESSAGE_QUEUED = "Saved on this phone. It will reach the ledger when the phone is online."
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
        /** Live weight per cleared goat id as typed (maintainer decision 2026-09-08). */
        val weights: Map<String, String> = emptyMap(),
        val weightErrors: Map<String, String> = emptyMap(),
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
            saleLine = deal?.let { dotJoin(it.buyerName, it.soldSummary(), animalsLine(it.animalCount)) }.orEmpty(),
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
            reviewAnimals = clearedPicks(l).map { c ->
                SaleReviewAnimalUi(
                    goatId = c.goatId,
                    tag = c.tagNumber.ifBlank { c.secondaryTagNumber }.ifBlank { c.displayId },
                    location = c.operationalLocationDisplay,
                    weight = l.weights[c.goatId].orEmpty(),
                    error = l.weightErrors[c.goatId].orEmpty(),
                )
            },
            allWeighed = clearedPicks(l).let { picks -> picks.isNotEmpty() && picks.all { weightLooksValid(l.weights[it.goatId].orEmpty()) } },
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
            is SaleTagAnimalsEvent.WeightChanged -> local.update { l ->
                l.copy(weights = l.weights + (event.goatId to event.value), weightErrors = l.weightErrors - event.goatId, message = null)
            }
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
            // The blocked animals are left out: confirming them would be refused whole.
            val ids = clearedPicks(l).map { it.goatId }
            // Every cleared animal must carry a weight (maintainer decision 2026-09-08); the
            // backend refuses the whole confirm otherwise, so the refusal is shown here first,
            // against the box that is empty or wrong.
            val errors = ids.filterNot { weightLooksValid(l.weights[it].orEmpty()) }.associateWith { WEIGHT_NEEDED }
            if (errors.isNotEmpty()) {
                local.update { it.copy(weightErrors = errors, message = MESSAGE_WEIGHTS_NEEDED) }
                return@launch
            }
            local.update { it.copy(confirmInFlight = true, message = null) }
            val weights = ids.associateWith { l.weights[it].orEmpty().trim() }
            when (val result = repository.confirmAllocation(l.confirmKey, SaleAllocationRequestDto(dealId, ids, weights))) {
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

    /** The picked animals the review did not refuse: the ones a weight is owed for and confirm sends. */
    private fun clearedPicks(l: Local): List<SaleCandidateDto> {
        val blocked = l.reviewBlocked.map { it.goatId }.toSet()
        return l.picked.values.filterNot { it.goatId in blocked }
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

/** A weight the backend would accept: kg more than zero, up to two decimals (numeric(7,2)). */
private fun weightLooksValid(raw: String): Boolean {
    val v = raw.trim()
    return Regex("""^\d{1,5}(\.\d{1,2})?$""").matches(v) && (v.toDoubleOrNull() ?: 0.0) > 0.0
}

private const val WEIGHT_NEEDED = "Enter the weight in kg"
private const val MESSAGE_WEIGHTS_NEEDED = "Enter every animal's weight in kg before confirming."
