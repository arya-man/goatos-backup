package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsEventsAnimalPurchase
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.AnimalPurchaseRepository
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.SalesDealTotals
import sg.mesha.goatos.core.data.SalesLeadSide
import sg.mesha.goatos.core.data.SalesRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseAnimalDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseCountsDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadCreateRequestDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseLoadDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionDto
import sg.mesha.goatos.core.network.dto.AnimalPurchaseOptionsDto
import sg.mesha.goatos.core.network.dto.SaleAllocationDto
import sg.mesha.goatos.core.network.dto.SaleAllocationRequestDto
import sg.mesha.goatos.core.network.dto.SaleCandidatePageDto
import sg.mesha.goatos.core.network.dto.SaleLocationsDto
import sg.mesha.goatos.core.network.dto.SalePreviewDto
import sg.mesha.goatos.core.network.dto.SalesBuyerLeadDto
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesFpoLeadDto
import sg.mesha.goatos.core.network.dto.SalesLeadBoardMetaDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.VendorOptionDto
import sg.mesha.goatos.core.network.dto.VendorOptionsDto
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseAnimalField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadCreateEvent
import sg.mesha.goatos.feature.vendors.AnimalPurchaseLoadField
import sg.mesha.goatos.feature.vendors.AnimalPurchaseVideoStatus
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus

/**
 * The Animal purchases state holders (maintainer decision 2026-09-13,
 * docs/decisions/animal-purchases.md).
 *
 * What these hold: backend copy reaches the cards VERBATIM; a form never closes on enqueue and
 * opens the recorded load only under the SERVER's id; a refused write shows the server's own
 * sentence and stays open; and an animal cannot be queued without a video, while a real recording
 * reaches the outbox on the load's lane and the create references that very proof row.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class AnimalPurchaseViewModelsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `a load card renders backend title, summary and whole-load counts verbatim`() {
        val card = AnimalPurchaseLoadDto(
            loadId = "load-1",
            title = "Load 132 · Kumar Traders",
            summary = "CBE · about 40 animals · 12 recorded",
            counts = AnimalPurchaseCountsDto(total = 12, pending = 7, accepted = 4, rejected = 1),
        ).toCardUi()

        assertEquals("load-1", card.listKey)
        assertEquals("Load 132 · Kumar Traders", card.title)
        assertEquals("CBE · about 40 animals · 12 recorded", card.summary)
        assertEquals(7, card.pending)
        assertEquals(4, card.accepted)
        assertEquals(1, card.rejected)
    }

    @Test
    fun `an animal card keeps the server decision chip and tone and composes only the number line`() {
        val accepted = animal(decision = "accepted", decisionLabel = "Accepted", decisionTone = "ok", decidedBy = "Ravi", note = "Good frame")
            .copy(ageMonths = 8, weightKg = 24.0).toCardUi()
        assertEquals("Accepted", accepted.decisionLabel)
        assertEquals(VendorsTone.OK, accepted.decisionTone)
        assertEquals("Ravi", accepted.decidedByName)
        assertEquals("Good frame", accepted.decisionNote)
        assertEquals("8 months · 24 kg", accepted.ageWeightLine)

        val rejected = animal(decision = "rejected", decisionLabel = "Rejected", decisionTone = "bad").toCardUi()
        assertEquals(VendorsTone.DANGER, rejected.decisionTone)
        assertEquals("", rejected.ageWeightLine)

        val pending = animal(decision = "pending", decisionLabel = "Awaiting decision", decisionTone = "neutral").toCardUi()
        assertEquals(VendorsTone.NEUTRAL, pending.decisionTone)
        assertEquals("", pending.decidedByName)
    }

    @Test
    fun `saving a load follows the queued row and opens the load under the server id`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = loadCreateViewModel(sync)
        advanceUntilIdle()

        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "132"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, "vendor-1"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.FARM, "CBE"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.Submit)
        advanceUntilIdle()

        // Queued, not closed: the id the screen must open is the SERVER's, not known yet.
        assertEquals(1, sync.loadCreates.size)
        assertEquals("132", sync.loadCreates.single().request.loadRef)
        assertEquals(VendorsWriteStatus.QUEUED, vm.state.value.writeStatus)
        assertNull(vm.state.value.createdLoadId)

        sync.row("ap-row-1").value = item("ap-row-1", SyncItemStatus.SUCCEEDED, resultJson = """{"load_id":"load-9","title":"Load 132"}""")
        advanceUntilIdle()

        assertEquals(VendorsWriteStatus.SYNCED, vm.state.value.writeStatus)
        assertEquals("load-9", vm.state.value.createdLoadId)
    }

    @Test
    fun `a refused load shows the server sentence and leaves the form open`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = loadCreateViewModel(sync)
        advanceUntilIdle()

        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "132"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.VENDOR, "vendor-1"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.FARM, "CBE"))
        vm.onEvent(AnimalPurchaseLoadCreateEvent.Submit)
        advanceUntilIdle()

        sync.row("ap-row-1").value = item("ap-row-1", SyncItemStatus.FAILED, conflict = true, lastError = "Load number 132 is already used.")
        advanceUntilIdle()

        assertEquals(VendorsWriteStatus.FAILED, vm.state.value.writeStatus)
        assertEquals("Load number 132 is already used.", vm.state.value.writeMessage)
        assertNull(vm.state.value.createdLoadId)
        // The form is open again: a corrected number can be submitted as a NEW record.
        vm.onEvent(AnimalPurchaseLoadCreateEvent.FieldChanged(AnimalPurchaseLoadField.LOAD_REF, "133"))
        assertEquals("133", vm.state.value.values[AnimalPurchaseLoadField.LOAD_REF])
    }

    @Test
    fun `an animal without a video is refused before anything is queued`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val vm = animalCreateViewModel(sync = sync)
        advanceUntilIdle()

        fillAnimal(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.Submit)
        advanceUntilIdle()

        assertTrue(sync.animalCreates.isEmpty())
        assertTrue(vm.state.value.videoMissing)
        assertEquals(AnimalPurchaseVideoStatus.NONE, vm.state.value.videoStatus)
    }

    @Test
    fun `a recorded video reaches the load lane and the animal create references that proof row`() = runTest(dispatcher) {
        val sync = RecordingAnimalPurchaseSyncRepository()
        val captureSource = FakeProofCaptureSource()
        captureSource.queue(CapturedVideo(localUri = "file:///animal.mp4", startedAtMs = 1_000L, endedAtMs = 9_000L))
        val proofs = FakeProofCaptureRepository()
        val analytics = RecordingAnalytics()
        val vm = animalCreateViewModel(sync = sync, captureSource = captureSource, proofs = proofs, analytics = analytics)
        advanceUntilIdle()

        vm.onEvent(AnimalPurchaseAnimalCreateEvent.RecordVideo)
        advanceUntilIdle()

        assertEquals(1, captureSource.captureCount)
        val capture = proofs.captureCalls.single()
        // ONE FIFO lane per load, so the upload always drains before the create that resolves it.
        assertEquals("animal-purchase:load:$LOAD_ID", capture.uploadGroupKey)
        assertEquals(LOAD_ID, capture.subjectId)
        assertEquals(AnimalPurchaseVideoStatus.RECORDED, vm.state.value.videoStatus)
        assertEquals("file:///animal.mp4", vm.state.value.videoLocalUri)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsAnimalPurchase.ANIMAL_VIDEO_CAPTURED })

        fillAnimal(vm)
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.Submit)
        advanceUntilIdle()

        val queued = sync.animalCreates.single()
        assertEquals(LOAD_ID, queued.loadId)
        assertEquals("goat", queued.request.species)
        assertEquals("female", queued.request.sex)
        assertEquals("healthy", queued.request.condition)
        assertEquals(24.5, queued.request.weightKg)
        // The proof rides by REFERENCE; the dispatcher resolves the server id at drain time.
        assertTrue(queued.proofOutboxItemId.isNotBlank())
        assertEquals("", queued.request.videoProofRef)
        assertFalse(vm.state.value.videoMissing)
        assertEquals(VendorsWriteStatus.QUEUED, vm.state.value.writeStatus)

        sync.row("ap-animal-1").value = item("ap-animal-1", SyncItemStatus.SUCCEEDED, resultJson = "{}")
        advanceUntilIdle()
        assertEquals(VendorsWriteStatus.SYNCED, vm.state.value.writeStatus)
        assertTrue(vm.state.value.closeAfterSave)
    }

    // --- fixtures ---------------------------------------------------------------------------

    private fun fillAnimal(vm: AnimalPurchaseAnimalCreateViewModel) {
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.SPECIES, "goat"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.SEX, "female"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.CONDITION, "healthy"))
        vm.onEvent(AnimalPurchaseAnimalCreateEvent.FieldChanged(AnimalPurchaseAnimalField.WEIGHT_KG, "24.5"))
    }

    private fun TestScope.loadCreateViewModel(sync: RecordingAnimalPurchaseSyncRepository): AnimalPurchaseLoadCreateViewModel =
        AnimalPurchaseLoadCreateViewModel(
            savedStateHandle = SavedStateHandle(),
            repository = FakeAnimalPurchaseRepository(),
            salesRepository = StubSalesRepository(),
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun TestScope.animalCreateViewModel(
        sync: RecordingAnimalPurchaseSyncRepository,
        captureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        proofs: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        analytics: RecordingAnalytics = RecordingAnalytics(),
    ): AnimalPurchaseAnimalCreateViewModel = AnimalPurchaseAnimalCreateViewModel(
        savedStateHandle = SavedStateHandle(mapOf("load_id" to LOAD_ID)),
        repository = FakeAnimalPurchaseRepository(),
        proofCaptureRepository = proofs,
        proofCaptureSource = captureSource,
        syncRepository = sync,
        bootstrapRepository = TenantOnlyBootstrapRepository(),
        analytics = analytics,
        crashReporter = NoopCrashReporter(),
        appContext = RuntimeEnvironment.getApplication(),
    ).also { vm -> backgroundScope.launch { vm.state.collect { } } }

    private fun animal(decision: String, decisionLabel: String, decisionTone: String, decidedBy: String = "", note: String = "") =
        AnimalPurchaseAnimalDto(
            candidateId = "cand-1",
            loadId = LOAD_ID,
            title = "Animal 1 · Female goat",
            decision = decision,
            decisionLabel = decisionLabel,
            decisionTone = decisionTone,
            decidedByName = decidedBy,
            decisionNote = note,
        )

    private companion object {
        const val LOAD_ID = "load-1"

        fun item(id: String, status: SyncItemStatus, conflict: Boolean = false, lastError: String? = null, resultJson: String? = null) = SyncQueueItem(
            id = id, opType = "ANIMAL_PURCHASE_LOAD_CREATE", idempotencyKey = "k", groupKey = "g", status = status,
            attemptCount = 0, maxAttempts = 5, conflict = conflict, createdAt = 0L, updatedAt = 0L, lastError = lastError,
            resultJson = resultJson,
        )
    }
}

/** In-memory [AnimalPurchaseRepository]: Room's role is played by state flows. */
private class FakeAnimalPurchaseRepository : AnimalPurchaseRepository {
    private val options = MutableStateFlow<AnimalPurchaseOptionsDto?>(
        AnimalPurchaseOptionsDto(
            species = listOf(AnimalPurchaseOptionDto("goat", "Goat"), AnimalPurchaseOptionDto("sheep", "Sheep")),
            sexes = listOf(AnimalPurchaseOptionDto("female", "Female"), AnimalPurchaseOptionDto("male", "Male")),
            conditions = listOf(AnimalPurchaseOptionDto("healthy", "Healthy")),
            farms = listOf(AnimalPurchaseOptionDto("CBE", "CBE"), AnimalPurchaseOptionDto("CPT", "CPT")),
            copy = mapOf("animal.form.title" to "Add an animal", "animal.video.hint" to "Walk around the animal."),
        ),
    )
    private val load = MutableStateFlow<AnimalPurchaseLoadDto?>(AnimalPurchaseLoadDto(loadId = "load-1", title = "Load 132 · Kumar Traders"))

    override fun loads(): Flow<PagingData<AnimalPurchaseLoadDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeCanRecord(): Flow<Boolean> = flowOf(true)
    override suspend fun invalidateLoads() = Unit
    override fun observeOptions(): Flow<AnimalPurchaseOptionsDto?> = options
    override suspend fun refreshOptions() = Unit
    override fun observeLoad(loadId: String): Flow<AnimalPurchaseLoadDto?> = load.map { it?.takeIf { l -> l.loadId == loadId } }
    override fun animals(loadId: String): Flow<PagingData<AnimalPurchaseAnimalDto>> = flowOf(PagingData.from(emptyList()))
    override suspend fun invalidateAnimals(loadId: String) = Unit
    override suspend fun refreshLoad(loadId: String) = Unit
    override suspend fun persistServerLoad(load: AnimalPurchaseLoadDto) = Unit
    override suspend fun persistServerAnimal(animal: AnimalPurchaseAnimalDto) = Unit
}

/** Records every animal-purchase enqueue and lets a test drive each queued row's outcome. */
private class RecordingAnimalPurchaseSyncRepository : SyncRepository by RecordingToxinSyncRepository() {
    data class LoadCreate(val draftKey: String, val request: AnimalPurchaseLoadCreateRequestDto)
    data class AnimalCreate(val draftKey: String, val loadId: String, val request: AnimalPurchaseAnimalCreateRequestDto, val proofOutboxItemId: String)

    val loadCreates = mutableListOf<LoadCreate>()
    val animalCreates = mutableListOf<AnimalCreate>()
    private val rows = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()

    fun row(itemId: String): MutableStateFlow<SyncQueueItem?> = rows.getOrPut(itemId) { MutableStateFlow(null) }

    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = row(itemId)

    override suspend fun enqueueAnimalPurchaseLoadCreate(draftKey: String, request: AnimalPurchaseLoadCreateRequestDto): AppResult<String> {
        loadCreates += LoadCreate(draftKey, request)
        return AppResult.Ok("ap-row-${loadCreates.size}")
    }

    override suspend fun enqueueAnimalPurchaseAnimalCreate(
        draftKey: String,
        loadId: String,
        request: AnimalPurchaseAnimalCreateRequestDto,
        proofOutboxItemId: String,
    ): AppResult<String> {
        animalCreates += AnimalCreate(draftKey, loadId, request, proofOutboxItemId)
        return AppResult.Ok("ap-animal-${animalCreates.size}")
    }
}

/** The one bootstrap fact the add-animal form needs: the tenant the video is scoped to. */
private class TenantOnlyBootstrapRepository : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
    override suspend fun actorTenantId(): String = "tenant-1"
}

/** The vendor picklist and nothing else of the Sales module. */
private class StubSalesRepository : SalesRepository {
    override fun deals(farm: String): Flow<PagingData<SalesDealDto>> = flowOf(PagingData.from(emptyList()))
    override val dealTotals: StateFlow<SalesDealTotals> = MutableStateFlow(SalesDealTotals())
    override suspend fun invalidateDeals(farm: String) = Unit
    override fun observeDeal(dealId: String): Flow<SalesDealDto?> = flowOf(null)
    override fun observeOptions(): Flow<SalesOptionsDto?> = flowOf(null)
    override suspend fun refreshOptions() = Unit
    override fun observeVendorOptions(): Flow<VendorOptionsDto?> =
        flowOf(VendorOptionsDto(vendors = listOf(VendorOptionDto("vendor-1", "Kumar Traders"))))
    override suspend fun refreshVendorOptions() = Unit
    override suspend fun persistServerDeal(deal: SalesDealDto) = Unit
    override fun buyerLeads(search: String, status: String): Flow<PagingData<SalesBuyerLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun fpoLeads(search: String, status: String): Flow<PagingData<SalesFpoLeadDto>> = flowOf(PagingData.from(emptyList()))
    override fun observeLeadMeta(side: SalesLeadSide, search: String, status: String): Flow<SalesLeadBoardMetaDto?> = flowOf(null)
    override suspend fun refreshLeadMeta(side: SalesLeadSide) = Unit
    override suspend fun invalidateLeads(side: SalesLeadSide, search: String, status: String) = Unit
    override suspend fun persistServerBuyerLead(lead: SalesBuyerLeadDto) = Unit
    override suspend fun persistServerFpoLead(lead: SalesFpoLeadDto) = Unit
    override suspend fun saleLocations(): AppResult<SaleLocationsDto> = error("unused")
    override suspend fun saleCandidates(parkId: String, shedId: String?, partitionLabels: List<String>, query: String?, cursor: String?): AppResult<SaleCandidatePageDto> = error("unused")
    override suspend fun saleAllocation(dealId: String): AppResult<SaleAllocationDto> = error("unused")
    override suspend fun previewAllocation(request: SaleAllocationRequestDto): AppResult<SalePreviewDto> = error("unused")
    override suspend fun confirmAllocation(idempotencyKey: String, request: SaleAllocationRequestDto): AppResult<SaleAllocationDto> = error("unused")
}
