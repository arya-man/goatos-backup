package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsEventsVendors
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.sync.SalesPipelineKind
import sg.mesha.goatos.core.data.sync.SalesPipelinePayload
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.network.dto.SalesBenchmarkWriteDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadWriteDto
import sg.mesha.goatos.core.network.dto.SalesLeadStatusWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.SalesSoldTagRowDto
import sg.mesha.goatos.core.network.dto.SalesSoldTagsWriteDto
import sg.mesha.goatos.core.network.dto.SalesWeightCheckWriteDto
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.feature.vendors.FIELD_ANIMAL_LABEL
import sg.mesha.goatos.feature.vendors.FIELD_FARM
import sg.mesha.goatos.feature.vendors.FIELD_TAG_NUMBER
import sg.mesha.goatos.feature.vendors.FIELD_WEIGHT_KG
import sg.mesha.goatos.feature.vendors.SalesBuyerLeadField
import sg.mesha.goatos.feature.vendors.SalesEvidenceEvent
import sg.mesha.goatos.feature.vendors.SalesEvidenceUiState
import sg.mesha.goatos.feature.vendors.SalesFpoLeadField
import sg.mesha.goatos.feature.vendors.SalesLeadBoardEvent
import sg.mesha.goatos.feature.vendors.SalesLeadBoardUiState
import sg.mesha.goatos.feature.vendors.SalesLeadCardUi
import sg.mesha.goatos.feature.vendors.SalesLeadFormUi
import sg.mesha.goatos.feature.vendors.SalesPipelineEntryUi
import sg.mesha.goatos.feature.vendors.SalesPipelineHubEvent
import sg.mesha.goatos.feature.vendors.SalesPipelineHubUiState
import sg.mesha.goatos.feature.vendors.SalesPipelinePanel
import sg.mesha.goatos.feature.vendors.SalesQuoteField
import sg.mesha.goatos.feature.vendors.SalesSoldTagRowUi
import sg.mesha.goatos.feature.vendors.SalesWeightCheckField
import sg.mesha.goatos.feature.vendors.VendorsOptionUi
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus
import sg.mesha.goatos.ui.Routes
import java.time.LocalDate
import java.util.UUID
import javax.inject.Inject

/**
 * Pipeline and evidence on the phone (maintainer instruction 2026-09-04): the five panels the web's
 * /sales/config carries beside the deals ledger, which the retired Sales DB sheet used to hold.
 *
 * The two lead boards are Room-first reads (a bounded newest-20 blob each) refreshed behind what is
 * already on screen; the three evidence forms are entry-only, because the server keeps no board of
 * them the phone needs to show. Every write goes through the outbox on a stable client id, so a
 * double tap or a lost response replays the same record rather than entering a second one.
 */
@HiltViewModel
class SalesPipelineHubViewModel @Inject constructor(
    private val repository: SalesRepository,
    private val analytics: AnalyticsPort,
) : ViewModel() {

    private val refreshing = MutableStateFlow(false)
    private val lastSynced = MutableStateFlow<Long?>(null)

    init {
        analytics.track(AnalyticsEventsVendors.VENDORS_PIPELINE_OPENED)
        refresh()
    }

    val state: StateFlow<SalesPipelineHubUiState> =
        combine(repository.observeBuyerLeads(), repository.observeFpoLeads(), refreshing, lastSynced) { buyers, fpos, isRefreshing, synced ->
            SalesPipelineHubUiState(
                entries = listOf(
                    SalesPipelineEntryUi(
                        SalesPipelinePanel.BUYER_LEADS, "Buyer leads", "Who called about buying, and where that conversation stands",
                        countLine = buyers?.total?.takeIf { it > 0 }?.let { "$it" }.orEmpty(),
                    ),
                    SalesPipelineEntryUi(
                        SalesPipelinePanel.FARMER_GROUPS, "Farmer groups", "Farmer producer groups and the crops they grow",
                        countLine = fpos?.total?.takeIf { it > 0 }?.let { "$it" }.orEmpty(),
                    ),
                    SalesPipelineEntryUi(SalesPipelinePanel.MARKET_QUOTE, "Market quote", "What a market is paying, beside our landed cost"),
                    SalesPipelineEntryUi(SalesPipelinePanel.SOLD_TAGS, "Sold tags", "The tag list of a lot that has been sold"),
                    SalesPipelineEntryUi(SalesPipelinePanel.WEIGHT_CHECK, "Weight check", "The book weight beside the weight read off the video"),
                ),
                isRefreshing = isRefreshing,
                lastSyncedAt = synced,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SalesPipelineHubUiState())

    fun onEvent(event: SalesPipelineHubEvent) {
        when (event) {
            SalesPipelineHubEvent.Refresh -> refresh()
            is SalesPipelineHubEvent.Open -> analytics.track(
                AnalyticsEventsVendors.VENDORS_PIPELINE_OPENED,
                mapOf(AnalyticsEvents.Params.REASON to event.panel.name.lowercase()),
            )
            SalesPipelineHubEvent.Back -> Unit
        }
    }

    private fun refresh() {
        viewModelScope.launch {
            refreshing.value = true
            try {
                repository.refreshBuyerLeads()
                repository.refreshFpoLeads()
                lastSynced.value = System.currentTimeMillis()
            } finally {
                refreshing.value = false
            }
        }
    }
}

/** One lead board — buyers, or farmer groups. The route's panel argument decides which. */
@HiltViewModel
class SalesLeadBoardViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val panel: SalesPipelinePanel =
        // exception:exempt an unreadable panel argument is a bad link, not a failure to report:
        // the screen simply opens on its default panel.
        runCatching { SalesPipelinePanel.valueOf(savedStateHandle.get<String>(Routes.SALES_PANEL_ARG).orEmpty()) }
            .getOrDefault(SalesPipelinePanel.BUYER_LEADS)

    private data class Local(
        val refreshing: Boolean = false,
        val lastSynced: Long? = null,
        val form: SalesLeadFormUi? = null,
        val statusPickerLeadId: String = "",
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
    )

    private val local = MutableStateFlow(Local())

    init {
        viewModelScope.launch { repository.refreshOptions() }
        refresh()
    }

    val state: StateFlow<SalesLeadBoardUiState> =
        combine(repository.observeBuyerLeads(), repository.observeFpoLeads(), repository.observeOptions(), local) { buyers, fpos, options, l ->
            val buyerBoard = panel == SalesPipelinePanel.BUYER_LEADS
            val cards = if (buyerBoard) {
                buyers?.leads.orEmpty().map { lead ->
                    SalesLeadCardUi(
                        leadId = lead.leadId,
                        title = lead.buyerName,
                        subtitle = dotJoin(lead.buyerPlace.orEmpty(), lead.animalType.orEmpty(), lead.breed.orEmpty()),
                        metaLine = dotJoin(lead.farm.orEmpty(), lead.recordedDate?.let(::farmDate).orEmpty()),
                        statusLabel = lead.callStatus.orEmpty(),
                        statusTone = leadStatusTone(lead.callStatus.orEmpty()),
                    )
                }
            } else {
                fpos?.leads.orEmpty().map { lead ->
                    SalesLeadCardUi(
                        leadId = lead.leadId,
                        title = lead.fpoName,
                        subtitle = dotJoin(lead.crops.orEmpty(), lead.district.orEmpty()),
                        metaLine = dotJoin(lead.taluk.orEmpty(), lead.state.orEmpty()),
                        statusLabel = lead.callStatus.orEmpty(),
                        statusTone = leadStatusTone(lead.callStatus.orEmpty()),
                    )
                }
            }
            val total = if (buyerBoard) buyers?.total ?: 0 else fpos?.total ?: 0
            val statusOptions = (if (buyerBoard) buyers?.statusOptions else fpos?.statusOptions).orEmpty()
            SalesLeadBoardUiState(
                title = if (buyerBoard) "Buyer leads" else "Farmer groups",
                cards = cards,
                statusOptions = statusOptions.map { VendorsOptionUi(it, it) },
                farms = options?.farms.orEmpty().map { VendorsOptionUi(it, it) },
                animalTypes = options?.productTypes.orEmpty().map { VendorsOptionUi(it, it) },
                breeds = breedOptions(options, l.form?.values?.get(SalesBuyerLeadField.ANIMAL_TYPE.name).orEmpty()),
                today = LocalDate.now().toString(),
                form = l.form,
                statusPickerLeadId = l.statusPickerLeadId,
                countLine = if (total > 0) "$total ${if (total == 1) "record" else "records"}" else "",
                emptyMessage = if (buyerBoard) "No buyer leads recorded yet" else "No farmer groups recorded yet",
                isRefreshing = l.refreshing,
                lastSyncedAt = l.lastSynced,
                writeStatus = l.writeStatus,
                writeMessage = l.writeMessage,
            )
        }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SalesLeadBoardUiState())

    fun onEvent(event: SalesLeadBoardEvent) {
        when (event) {
            SalesLeadBoardEvent.Refresh -> refresh()
            SalesLeadBoardEvent.Back -> Unit
            SalesLeadBoardEvent.OpenForm -> local.update {
                it.copy(form = SalesLeadFormUi(values = defaultFormValues()), writeMessage = "")
            }
            SalesLeadBoardEvent.CloseForm -> local.update { it.copy(form = null) }
            is SalesLeadBoardEvent.FieldChanged -> local.update { l ->
                val form = l.form ?: return@update l
                l.copy(form = form.copy(values = form.values + (event.field to event.value), fieldErrors = form.fieldErrors - event.field))
            }
            SalesLeadBoardEvent.Submit -> submit()
            is SalesLeadBoardEvent.OpenStatusPicker -> local.update {
                // Tapping the open card closes the picker again, so it never traps the row.
                it.copy(statusPickerLeadId = if (it.statusPickerLeadId == event.leadId) "" else event.leadId)
            }
            is SalesLeadBoardEvent.ChangeStatus -> changeStatus(event.leadId, event.status)
        }
    }

    private fun defaultFormValues(): Map<String, String> =
        if (panel == SalesPipelinePanel.BUYER_LEADS) {
            mapOf(SalesBuyerLeadField.RECORDED_DATE.name to LocalDate.now().toString())
        } else {
            emptyMap()
        }

    private fun submit() {
        val form = local.value.form ?: return
        val buyerBoard = panel == SalesPipelinePanel.BUYER_LEADS
        val nameField = if (buyerBoard) SalesBuyerLeadField.BUYER_NAME.name else SalesFpoLeadField.FPO_NAME.name
        if (form.values[nameField].orEmpty().isBlank()) {
            local.update { it.copy(form = form.copy(fieldErrors = form.fieldErrors + (nameField to REQUIRED))) }
            return
        }
        val clientId = UUID.randomUUID().toString()
        val payload = if (buyerBoard) {
            SalesPipelinePayload(
                clientId = clientId,
                kind = SalesPipelineKind.BUYER_LEAD,
                buyerLead = SalesBuyerLeadWriteDto(
                    recordedDate = form.values[SalesBuyerLeadField.RECORDED_DATE.name].orEmpty().trim(),
                    farm = form.values[SalesBuyerLeadField.FARM.name].orEmpty().trim(),
                    buyerName = form.values[SalesBuyerLeadField.BUYER_NAME.name].orEmpty().trim(),
                    buyerPlace = form.values[SalesBuyerLeadField.BUYER_PLACE.name].orEmpty().trim(),
                    animalType = form.values[SalesBuyerLeadField.ANIMAL_TYPE.name].orEmpty().trim(),
                    breed = form.values[SalesBuyerLeadField.BREED.name].orEmpty().trim(),
                    callStatus = form.values[SalesBuyerLeadField.CALL_STATUS.name].orEmpty().trim(),
                ),
            )
        } else {
            SalesPipelinePayload(
                clientId = clientId,
                kind = SalesPipelineKind.FPO_LEAD,
                fpoLead = SalesFpoLeadWriteDto(
                    fpoName = form.values[SalesFpoLeadField.FPO_NAME.name].orEmpty().trim(),
                    crops = form.values[SalesFpoLeadField.CROPS.name].orEmpty().trim(),
                    district = form.values[SalesFpoLeadField.DISTRICT.name].orEmpty().trim(),
                    taluk = form.values[SalesFpoLeadField.TALUK.name].orEmpty().trim(),
                    state = form.values[SalesFpoLeadField.STATE.name].orEmpty().trim(),
                    callStatus = form.values[SalesFpoLeadField.CALL_STATUS.name].orEmpty().trim(),
                ),
            )
        }
        enqueue(payload, MESSAGE_LEAD_SAVED, "sales lead enqueue failed") { local.update { it.copy(form = null) } }
    }

    private fun changeStatus(leadId: String, status: String) {
        val buyerBoard = panel == SalesPipelinePanel.BUYER_LEADS
        val payload = SalesPipelinePayload(
            clientId = UUID.randomUUID().toString(),
            kind = if (buyerBoard) SalesPipelineKind.BUYER_LEAD_STATUS else SalesPipelineKind.FPO_LEAD_STATUS,
            leadId = leadId,
            leadStatus = SalesLeadStatusWriteDto(callStatus = status),
        )
        enqueue(payload, MESSAGE_STATUS_SAVED, "sales lead status enqueue failed") {
            local.update { it.copy(statusPickerLeadId = "") }
        }
    }

    private fun enqueue(payload: SalesPipelinePayload, done: String, failure: String, onOk: () -> Unit) {
        viewModelScope.launch {
            local.update { it.copy(form = it.form?.copy(inFlight = true)) }
            when (val result = syncRepository.enqueueSalesPipelineWrite(payload)) {
                is AppResult.Ok -> {
                    analytics.track(
                        AnalyticsEventsVendors.VENDORS_PIPELINE_QUEUED,
                        mapOf(AnalyticsEvents.Params.REASON to payload.kind),
                    )
                    onOk()
                    local.update { it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = done) }
                    // The board is a SERVER read, so re-reading it the instant the row is queued
                    // shows the board WITHOUT the new lead -- the write has not reached the server
                    // yet. Follow the outbox row instead and re-read when it actually lands.
                    refreshWhenWriteLands(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, failure) }
                    analytics.track(
                        AnalyticsEventsVendors.VENDORS_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)),
                    )
                    local.update {
                        it.copy(form = it.form?.copy(inFlight = false), writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_FAILED)
                    }
                }
            }
        }
    }

    /**
     * Re-reads the board once the queued write reaches the server. A write still sitting in the
     * outbox after the grace period leaves the board as it is and says so, rather than showing a
     * board that silently lacks the row the operator just entered.
     */
    private fun refreshWhenWriteLands(outboxItemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(outboxItemId).collect { outcome ->
                when (outcome) {
                    QueuedWriteOutcome.Saved -> refresh()
                    QueuedWriteOutcome.StillQueued -> local.update { it.copy(writeMessage = MESSAGE_QUEUED_OFFLINE) }
                    is QueuedWriteOutcome.Rejected -> local.update {
                        it.copy(writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_FAILED)
                    }
                }
            }
        }
    }

    private fun refresh() {
        viewModelScope.launch {
            local.update { it.copy(refreshing = true) }
            try {
                if (panel == SalesPipelinePanel.BUYER_LEADS) repository.refreshBuyerLeads() else repository.refreshFpoLeads()
                local.update { it.copy(lastSynced = System.currentTimeMillis()) }
            } finally {
                local.update { it.copy(refreshing = false) }
            }
        }
    }

    private companion object {
        const val REQUIRED = "Required"
        const val MESSAGE_LEAD_SAVED = "Saved. It reaches the board when the phone is online."
        const val MESSAGE_QUEUED_OFFLINE = "Saved on this phone. It reaches the board when the phone is online."
        const val MESSAGE_STATUS_SAVED = "Status saved. It reaches the board when the phone is online."
        const val MESSAGE_FAILED = "Could not save that. Try again."
    }
}

/** A market quote, a sold-tag list, or a weight check. The route's panel argument decides which. */
@HiltViewModel
class SalesEvidenceViewModel @Inject constructor(
    savedStateHandle: SavedStateHandle,
    private val repository: SalesRepository,
    private val syncRepository: SyncRepository,
    private val analytics: AnalyticsPort,
    private val crashReporter: CrashReporter,
) : ViewModel() {

    private val panel: SalesPipelinePanel =
        // exception:exempt an unreadable panel argument is a bad link, not a failure to report:
        // the screen simply opens on its default panel.
        runCatching { SalesPipelinePanel.valueOf(savedStateHandle.get<String>(Routes.SALES_PANEL_ARG).orEmpty()) }
            .getOrDefault(SalesPipelinePanel.MARKET_QUOTE)

    private data class Local(
        val values: Map<String, String> = emptyMap(),
        val fieldErrors: Map<String, String> = emptyMap(),
        val tagRows: List<SalesSoldTagRowUi> = emptyList(),
        val inFlight: Boolean = false,
        val writeStatus: VendorsWriteStatus = VendorsWriteStatus.IDLE,
        val writeMessage: String = "",
        val closeAfterSave: Boolean = false,
    )

    private val local = MutableStateFlow(
        Local(
            values = if (panel == SalesPipelinePanel.WEIGHT_CHECK) mapOf(SalesWeightCheckField.FARM_BORN.name to "true") else emptyMap(),
            tagRows = if (panel == SalesPipelinePanel.SOLD_TAGS) listOf(SalesSoldTagRowUi(rowKey = UUID.randomUUID().toString())) else emptyList(),
        ),
    )

    init {
        viewModelScope.launch { repository.refreshOptions() }
    }

    val state: StateFlow<SalesEvidenceUiState> = combine(repository.observeOptions(), local) { options, l ->
        SalesEvidenceUiState(
            title = when (panel) {
                SalesPipelinePanel.MARKET_QUOTE -> "Market quote"
                SalesPipelinePanel.SOLD_TAGS -> "Sold tags"
                else -> "Weight check"
            },
            subtitle = when (panel) {
                SalesPipelinePanel.MARKET_QUOTE -> "What a market is paying"
                SalesPipelinePanel.SOLD_TAGS -> "The tags of a lot that has been sold"
                else -> "Book weight beside the video weight"
            },
            values = l.values,
            fieldErrors = l.fieldErrors,
            tagRows = l.tagRows,
            farms = options?.farms.orEmpty().map { VendorsOptionUi(it, it) },
            animalTypes = options?.productTypes.orEmpty().map { VendorsOptionUi(it, it) },
            breeds = breedOptions(options, l.values[SalesQuoteField.CATEGORY.name].orEmpty()),
            submitInFlight = l.inFlight,
            writeStatus = l.writeStatus,
            writeMessage = l.writeMessage,
            closeAfterSave = l.closeAfterSave,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), SalesEvidenceUiState())

    fun onEvent(event: SalesEvidenceEvent) {
        when (event) {
            SalesEvidenceEvent.Back -> Unit
            is SalesEvidenceEvent.FieldChanged -> local.update {
                it.copy(values = it.values + (event.field to event.value), fieldErrors = it.fieldErrors - event.field)
            }
            is SalesEvidenceEvent.TagRowChanged -> local.update { l ->
                l.copy(
                    tagRows = l.tagRows.map { row ->
                        if (row.rowKey != event.rowKey) {
                            row
                        } else {
                            when (event.field) {
                                FIELD_TAG_NUMBER -> row.copy(tagNumber = event.value, error = "")
                                FIELD_ANIMAL_LABEL -> row.copy(animalLabel = event.value)
                                else -> row.copy(weightKg = event.value)
                            }
                        }
                    },
                )
            }
            SalesEvidenceEvent.AddTagRow -> local.update { it.copy(tagRows = it.tagRows + SalesSoldTagRowUi(rowKey = UUID.randomUUID().toString())) }
            is SalesEvidenceEvent.RemoveTagRow -> local.update { l ->
                // Never leave the form with no row at all: an empty list has nothing to type into.
                val remaining = l.tagRows.filterNot { it.rowKey == event.rowKey }
                l.copy(tagRows = remaining.ifEmpty { listOf(SalesSoldTagRowUi(rowKey = UUID.randomUUID().toString())) })
            }
            SalesEvidenceEvent.Submit -> submit()
        }
    }

    private fun submit() {
        val l = local.value
        val clientId = UUID.randomUUID().toString()
        val payload = when (panel) {
            SalesPipelinePanel.MARKET_QUOTE -> {
                // BREED is the server's required field here, not the market name.
                val breed = l.values[SalesQuoteField.BREED.name].orEmpty().trim()
                if (breed.isBlank()) {
                    local.update { it.copy(fieldErrors = it.fieldErrors + (SalesQuoteField.BREED.name to REQUIRED)) }
                    return
                }
                SalesPipelinePayload(
                    clientId = clientId,
                    kind = SalesPipelineKind.BENCHMARK,
                    benchmark = SalesBenchmarkWriteDto(
                        market = l.values[SalesQuoteField.MARKET.name].orEmpty().trim(),
                        category = l.values[SalesQuoteField.CATEGORY.name].orEmpty().trim(),
                        breed = breed,
                        source = l.values[SalesQuoteField.SOURCE.name].orEmpty().trim(),
                        exFarmRate = l.values[SalesQuoteField.EX_FARM_RATE.name].orEmpty().trim(),
                        transportRate = l.values[SalesQuoteField.TRANSPORT_RATE.name].orEmpty().trim(),
                        // Blank stays ABSENT rather than becoming zero: a quote nobody costed and
                        // one costed at nothing are different facts.
                        landingCostPerKg = l.values[SalesQuoteField.LANDING_COST_PER_KG.name].orEmpty().trim().toDoubleOrNull(),
                        marketPricePerKg = l.values[SalesQuoteField.MARKET_PRICE_PER_KG.name].orEmpty().trim().toDoubleOrNull(),
                    ),
                )
            }
            SalesPipelinePanel.SOLD_TAGS -> {
                val farm = l.values[FIELD_FARM].orEmpty().trim()
                // The server requires an ANIMAL on every row; a row with nothing typed in it is
                // simply not part of the list.
                val rows = l.tagRows.filter { it.animalLabel.isNotBlank() }
                if (farm.isBlank() || rows.isEmpty()) {
                    local.update {
                        it.copy(
                            fieldErrors = if (farm.isBlank()) it.fieldErrors + (FIELD_FARM to REQUIRED) else it.fieldErrors,
                            tagRows = if (rows.isEmpty()) it.tagRows.map { row -> row.copy(error = REQUIRED) } else it.tagRows,
                        )
                    }
                    return
                }
                SalesPipelinePayload(
                    clientId = clientId,
                    kind = SalesPipelineKind.SOLD_TAGS,
                    soldTags = SalesSoldTagsWriteDto(
                        farm = farm,
                        rows = rows.map {
                            SalesSoldTagRowDto(
                                animalLabel = it.animalLabel.trim(),
                                tagNumber = it.tagNumber.trim(),
                                weightKg = it.weightKg.trim().toDoubleOrNull(),
                            )
                        },
                    ),
                )
            }
            else -> {
                val tag = l.values[SalesWeightCheckField.TAG_NUMBER.name].orEmpty().trim()
                val book = l.values[SalesWeightCheckField.BOOK_WEIGHT_KG.name].orEmpty().trim().toDoubleOrNull()
                val video = l.values[SalesWeightCheckField.VIDEO_WEIGHT_KG.name].orEmpty().trim().toDoubleOrNull()
                val errors = buildMap {
                    if (book == null || book <= 0.0) put(SalesWeightCheckField.BOOK_WEIGHT_KG.name, MORE_THAN_ZERO)
                    if (video == null || video <= 0.0) put(SalesWeightCheckField.VIDEO_WEIGHT_KG.name, MORE_THAN_ZERO)
                }
                if (errors.isNotEmpty()) {
                    local.update { it.copy(fieldErrors = it.fieldErrors + errors) }
                    return
                }
                SalesPipelinePayload(
                    clientId = clientId,
                    kind = SalesPipelineKind.WEIGHT_CHECK,
                    weightCheck = SalesWeightCheckWriteDto(
                        tagNumber = tag,
                        bookWeightKg = requireNotNull(book),
                        videoWeightKg = requireNotNull(video),
                        farmBorn = l.values[SalesWeightCheckField.FARM_BORN.name] != "false",
                    ),
                )
            }
        }
        viewModelScope.launch {
            local.update { it.copy(inFlight = true) }
            when (val result = syncRepository.enqueueSalesPipelineWrite(payload)) {
                is AppResult.Ok -> {
                    analytics.track(
                        AnalyticsEventsVendors.VENDORS_PIPELINE_QUEUED,
                        mapOf(AnalyticsEvents.Params.REASON to payload.kind),
                    )
                    local.update { it.copy(inFlight = false, writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_SAVING) }
                    // The outbox accepting the row is NOT the server accepting it. Closing here on
                    // the strength of the enqueue told an operator "Saved" over a write the server
                    // then refused (a market quote with no breed, 2026-09-04) and took the screen
                    // away with the typing still on it. Follow the row instead.
                    followWrite(result.value)
                }
                is AppResult.Err -> {
                    result.cause?.let { crashReporter.recordException(it, "sales evidence enqueue failed") }
                    analytics.track(
                        AnalyticsEventsVendors.VENDORS_FAILURE,
                        mapOf(AnalyticsEvents.Params.REASON to result.message.take(120)),
                    )
                    local.update { it.copy(inFlight = false, writeStatus = VendorsWriteStatus.FAILED, writeMessage = MESSAGE_FAILED) }
                }
            }
        }
    }

    /**
     * Follows the queued row: the screen closes only once the SERVER has it, says so plainly while
     * the phone is offline, and on a refusal puts the form back with the reason rather than
     * closing over a record that was never written.
     */
    private fun followWrite(outboxItemId: String) {
        viewModelScope.launch {
            syncRepository.followQueuedWrite(outboxItemId).collect { outcome ->
                local.update {
                    when (outcome) {
                        QueuedWriteOutcome.Saved ->
                            it.copy(writeStatus = VendorsWriteStatus.SYNCED, writeMessage = MESSAGE_SAVED, closeAfterSave = true)
                        QueuedWriteOutcome.StillQueued ->
                            it.copy(writeStatus = VendorsWriteStatus.QUEUED, writeMessage = MESSAGE_QUEUED_OFFLINE, closeAfterSave = false)
                        is QueuedWriteOutcome.Rejected -> it.copy(
                            writeStatus = VendorsWriteStatus.FAILED,
                            // The server's own farm copy when it sent one: "Breed required." says
                            // what to fix; a generic retry line does not.
                            writeMessage = outcome.reason?.takeIf { r -> r.isNotBlank() } ?: MESSAGE_FAILED,
                            closeAfterSave = false,
                        )
                    }
                }
            }
        }
    }

    private companion object {
        const val REQUIRED = "Required"
        const val MORE_THAN_ZERO = "Must be more than zero"
        const val MESSAGE_SAVING = "Saving…"
        const val MESSAGE_SAVED = "Saved to the ledger."
        const val MESSAGE_QUEUED_OFFLINE = "Saved on this phone. It reaches the ledger when the phone is online."
        const val MESSAGE_FAILED = "Could not save that. Try again."
    }
}

/** Breeds of the chosen product, or every breed the catalog knows when nothing is chosen yet. */
private fun breedOptions(options: SalesOptionsDto?, productType: String): List<VendorsOptionUi> {
    val breeds = options?.breeds.orEmpty()
    val list = breeds[productType] ?: breeds.values.flatten().distinct()
    return list.map { VendorsOptionUi(it, it) }
}

/** Lead call-status tones. The vocabulary is the backend's; only the colour is decided here. */
private fun leadStatusTone(status: String): VendorsTone = when {
    status.isBlank() -> VendorsTone.NEUTRAL
    status.contains("not", ignoreCase = true) -> VendorsTone.DANGER
    status.contains("interest", ignoreCase = true) -> VendorsTone.OK
    else -> VendorsTone.INFO
}
