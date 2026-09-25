package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
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
import org.robolectric.annotation.Config
import sg.mesha.goatos.boot.RecordingAnalytics
import sg.mesha.goatos.core.analytics.AnalyticsEventsMarket
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.MarketRepository
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.MarketSurveyAnswerDto
import sg.mesha.goatos.core.network.dto.MarketSurveyCardDto
import sg.mesha.goatos.core.network.dto.MarketSurveyCardQuestionDto
import sg.mesha.goatos.core.network.dto.MarketSurveyDayDto
import sg.mesha.goatos.core.network.dto.MarketSurveyEntryRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.vendors.MarketCityEntryEvent
import sg.mesha.goatos.feature.vendors.MarketQuestionFieldUi
import sg.mesha.goatos.feature.vendors.VendorsTone
import sg.mesha.goatos.feature.vendors.VendorsWriteStatus

/**
 * Market survey ViewModels (maintainer decision 2026-09-14). What these pin: the day view renders
 * the SERVER's status and counts verbatim; a save queues ONE outbox write carrying only the
 * figures typed (a blank field is not an answer), overlays them at once, and follows the row.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
@OptIn(ExperimentalCoroutinesApi::class)
class MarketViewModelsTest {
    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private val goatLive = MarketSurveyCardQuestionDto("q-goat", "Goat live price", "₹/kg", 620.0)
    private val sheepLive = MarketSurveyCardQuestionDto("q-sheep", "Sheep live price", "₹/kg", null)
    private val chennai = MarketSurveyCardDto("c-chennai", "Chennai", "pending", answered = 1, total = 2, questions = listOf(goatLive, sheepLive))
    private val salem = MarketSurveyCardDto("c-salem", "Salem", "done", answered = 2, total = 2, questions = listOf(goatLive.copy(price = 600.0), sheepLive.copy(price = 540.0)))
    private val day = MarketSurveyDayDto(businessDate = "2026-09-14", cards = listOf(chennai, salem), pending = 1, done = 1, canRecord = true)

    @Test
    fun `the day view renders the server's status, counts and can_record verbatim`() {
        val state = composeDayState("Market", day, isRefreshing = false, lastSyncedAt = null, loadError = false)

        assertEquals(listOf("Chennai", "Salem"), state.cards.map { it.cityName })
        assertEquals(listOf("Pending", "Done"), state.cards.map { it.statusLabel })
        assertEquals(listOf(VendorsTone.WARN, VendorsTone.OK), state.cards.map { it.statusTone })
        assertEquals("1 of 2 answered", state.cards[0].progressLine)
        assertEquals("1 of 2 cities done", state.summaryLine)
        assertTrue(state.canRecord)
        assertNull(state.emptyMessage)
    }

    @Test
    fun `an empty cache after a failed refresh shows the error and an empty server day names the config`() {
        val failed = composeDayState("Market", null, isRefreshing = false, lastSyncedAt = null, loadError = true)
        assertEquals(MarketSurveyViewModel.EMPTY_LOAD_FAILED, failed.emptyMessage)
        assertTrue(failed.isErrorEmpty)

        val noCities = composeDayState("Market", day.copy(cards = emptyList(), pending = 0, done = 0), false, null, false)
        assertEquals(MarketSurveyViewModel.EMPTY_NO_CITIES, noCities.emptyMessage)
        assertFalse(noCities.isErrorEmpty)
        assertFalse(composeDayState("Market", day.copy(canRecord = false), false, null, false).canRecord)

        // Before the configured call time the server holds the cards back and names the time.
        val closed = composeDayState("Market", day.copy(cards = emptyList(), pending = 2, done = 0, open = false, opensAt = "08:30"), false, null, false)
        assertEquals("Today's market calls open at 08:30", closed.emptyMessage)
        assertEquals("2 cities to call", closed.summaryLine)
        assertFalse(closed.isErrorEmpty)
    }

    @Test
    fun `the form prefills recorded prices and a blank field is not an answer`() {
        val fields = chennai.fields(typed = emptyMap(), errors = emptyMap())
        assertEquals(listOf("620", ""), fields.map { it.value })
        assertEquals(listOf("₹/kg", "₹/kg"), fields.map { it.unitLabel })

        val (answers, errors) = parseAnswers(listOf(fields[0].copy(value = "640"), fields[1]))
        assertEquals(listOf(MarketSurveyAnswerDto("q-goat", 640.0)), answers)
        assertTrue(errors.isEmpty())

        val (_, refused) = parseAnswers(listOf(fields[0].copy(value = "abc"), fields[1].copy(value = "-5")))
        assertEquals(MarketCityEntryViewModel.ERROR_NOT_A_NUMBER, refused["q-goat"])
        assertEquals(MarketCityEntryViewModel.ERROR_NEGATIVE, refused["q-sheep"])
    }

    @Test
    fun `saving queues one write with the typed figures and overlays them until the row lands`() = runTest {
        val repository = FakeMarketRepository(day)
        val sync = RecordingMarketSyncRepository()
        val analytics = RecordingAnalytics()
        val vm = MarketCityEntryViewModel(
            savedStateHandle = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai")),
            repository = repository,
            syncRepository = sync,
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
        )
        val collector = vm.state.launchIn(TestScope(dispatcher))
        assertEquals("Chennai", vm.state.value.cityName)
        assertEquals("14/09/2026", vm.state.value.dateLine)

        vm.onEvent(MarketCityEntryEvent.PriceChanged("q-sheep", "560"))
        vm.onEvent(MarketCityEntryEvent.Save)

        val queued = sync.records.single()
        assertEquals("c-chennai", queued.cityId)
        assertEquals("2026-09-14", queued.businessDate)
        // Every figure the form SHOWS rides -- the prefilled goat price and the typed sheep price --
        // so what the reporter saw is exactly what was saved; only a blank field is left out.
        val expected = listOf(MarketSurveyAnswerDto("q-goat", 620.0), MarketSurveyAnswerDto("q-sheep", 560.0))
        assertEquals(expected, queued.request.answers)
        assertEquals(listOf("c-chennai" to expected), repository.overlays)
        assertEquals(VendorsWriteStatus.QUEUED, vm.state.value.writeStatus)
        assertTrue(analytics.events.any { it.name == AnalyticsEventsMarket.CITY_QUEUED })

        sync.succeed(queued.itemId)
        assertEquals(VendorsWriteStatus.SYNCED, vm.state.value.writeStatus)
        assertEquals(MarketCityEntryViewModel.MESSAGE_SAVED, vm.state.value.writeMessage)
        assertTrue(vm.state.value.closeAfterSave)
        collector.cancel()
    }

    @Test
    fun `rejected save refreshes the day so optimistic prices do not stick`() = runTest {
        val repository = FakeMarketRepository(day)
        val sync = RecordingMarketSyncRepository()
        val vm = MarketCityEntryViewModel(
            savedStateHandle = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai")),
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val collector = vm.state.launchIn(TestScope(dispatcher))

        vm.onEvent(MarketCityEntryEvent.PriceChanged("q-sheep", "560"))
        vm.onEvent(MarketCityEntryEvent.Save)
        val queued = sync.records.single()

        sync.reject(queued.itemId, "city closed")

        assertEquals(listOf("2026-09-14"), repository.refreshes)
        assertEquals("", vm.state.value.fields.first { it.questionId == "q-sheep" }.value)
        assertEquals(VendorsWriteStatus.FAILED, vm.state.value.writeStatus)
        assertEquals("city closed", vm.state.value.writeMessage)
        collector.cancel()
    }

    @Test
    fun `typed prices survive process recreation before save`() = runTest {
        val repository = FakeMarketRepository(day)
        val sync = RecordingMarketSyncRepository()
        val savedState = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai"))
        val first = MarketCityEntryViewModel(
            savedStateHandle = savedState,
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val firstCollector = first.state.launchIn(TestScope(dispatcher))
        first.onEvent(MarketCityEntryEvent.PriceChanged("q-sheep", "560"))
        assertEquals("560", first.state.value.fields.first { it.questionId == "q-sheep" }.value)
        firstCollector.cancel()

        val recreated = MarketCityEntryViewModel(
            savedStateHandle = savedState,
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val secondCollector = recreated.state.launchIn(TestScope(dispatcher))
        assertEquals("560", recreated.state.value.fields.first { it.questionId == "q-sheep" }.value)
        assertTrue(sync.records.isEmpty())
        secondCollector.cancel()
    }

    @Test
    fun `retrying an active market correction after recreation reuses the same client id`() = runTest {
        val repository = FakeMarketRepository(day)
        val sync = RecordingMarketSyncRepository()
        val savedState = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai"))
        val first = MarketCityEntryViewModel(
            savedStateHandle = savedState,
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val firstCollector = first.state.launchIn(TestScope(dispatcher))
        first.onEvent(MarketCityEntryEvent.PriceChanged("q-sheep", "560"))
        first.onEvent(MarketCityEntryEvent.Save)
        firstCollector.cancel()

        val recreated = MarketCityEntryViewModel(
            savedStateHandle = savedState,
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val secondCollector = recreated.state.launchIn(TestScope(dispatcher))
        recreated.onEvent(MarketCityEntryEvent.Save)

        assertEquals(2, sync.records.size)
        assertEquals(sync.records[0].clientId, sync.records[1].clientId)
        assertEquals(sync.records[0].request, sync.records[1].request)
        secondCollector.cancel()
    }

    @Test
    fun `reopening a city with a durable pending correction reuses its client id`() = runTest {
        val repository = FakeMarketRepository(day)
        val sync = RecordingMarketSyncRepository()
        val first = MarketCityEntryViewModel(
            savedStateHandle = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai")),
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val firstCollector = first.state.launchIn(TestScope(dispatcher))
        first.onEvent(MarketCityEntryEvent.PriceChanged("q-sheep", "560"))
        first.onEvent(MarketCityEntryEvent.Save)
        val firstQueued = sync.records.single()
        sync.pending.value = listOf(
            sg.mesha.goatos.core.data.sync.MarketSurveyRecordPayload(
                clientId = firstQueued.clientId,
                cityId = firstQueued.cityId,
                businessDate = firstQueued.businessDate,
                request = firstQueued.request,
            ),
        )
        firstCollector.cancel()

        val reopened = MarketCityEntryViewModel(
            savedStateHandle = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai")),
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val secondCollector = reopened.state.launchIn(TestScope(dispatcher))
        reopened.onEvent(MarketCityEntryEvent.Save)

        assertEquals(2, sync.records.size)
        assertEquals(firstQueued.clientId, sync.records[1].clientId)
        assertEquals(firstQueued.request, sync.records[1].request)
        secondCollector.cancel()
    }

    @Test
    fun `typed prices are dropped when the cached day changes before save`() = runTest {
        val repository = FakeMarketRepository(day.copy(businessDate = "2026-09-15"))
        val sync = RecordingMarketSyncRepository()
        val savedState = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai"))
        savedState["market_city_entry.typed"] = "{\"q-sheep\":\"560\"}"
        savedState["market_city_entry.typed_business_date"] = "2026-09-14"
        savedState["market_city_entry.typed_city_id"] = "c-chennai"
        val vm = MarketCityEntryViewModel(
            savedStateHandle = savedState,
            repository = repository,
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val collector = vm.state.launchIn(TestScope(dispatcher))

        assertEquals("", vm.state.value.fields.first { it.questionId == "q-sheep" }.value)
        vm.onEvent(MarketCityEntryEvent.Save)

        assertEquals(listOf(MarketSurveyAnswerDto("q-goat", 620.0)), sync.records.single().request.answers)
        assertEquals("2026-09-15", sync.records.single().businessDate)
        collector.cancel()
    }

    @Test
    fun `saving with nothing typed and nothing recorded is refused on the phone`() = runTest {
        val empty = day.copy(cards = listOf(chennai.copy(questions = listOf(sheepLive), answered = 0, total = 1)))
        val sync = RecordingMarketSyncRepository()
        val vm = MarketCityEntryViewModel(
            savedStateHandle = SavedStateHandle(mapOf(MarketCityEntryViewModel.ARG_CITY_ID to "c-chennai")),
            repository = FakeMarketRepository(empty),
            syncRepository = sync,
            analytics = RecordingAnalytics(),
            crashReporter = NoopCrashReporter(),
        )
        val collector = vm.state.launchIn(TestScope(dispatcher))
        vm.onEvent(MarketCityEntryEvent.Save)
        assertTrue(sync.records.isEmpty())
        assertEquals(VendorsWriteStatus.FAILED, vm.state.value.writeStatus)
        assertEquals(MarketCityEntryViewModel.MESSAGE_NOTHING_TO_SAVE, vm.state.value.writeMessage)
        collector.cancel()
    }
}

/** In-memory [MarketRepository]: Room's role is played by a [MutableStateFlow] of the day. */
private class FakeMarketRepository(private val initial: MarketSurveyDayDto?) : MarketRepository {
    private val day = MutableStateFlow(initial)
    val overlays = mutableListOf<Pair<String, List<MarketSurveyAnswerDto>>>()
    val refreshes = mutableListOf<String>()

    override fun observeDay(businessDate: String): Flow<MarketSurveyDayDto?> = day
    override suspend fun refreshDay(businessDate: String): Result<MarketSurveyDayDto> {
        refreshes += businessDate
        day.value = initial
        return day.value?.let { Result.success(it) } ?: Result.failure(IllegalStateException("offline"))
    }

    override suspend fun applyLocalAnswers(businessDate: String, cityId: String, answers: List<MarketSurveyAnswerDto>) {
        overlays += cityId to answers
        val current = day.value ?: return
        day.value = current.copy(
            cards = current.cards.map { card ->
                if (card.cityId != cityId) {
                    card
                } else {
                    val byQuestion = answers.associateBy { it.questionId }
                    card.copy(
                        questions = card.questions.map { question ->
                            byQuestion[question.questionId]?.let { question.copy(price = it.price) } ?: question
                        },
                    )
                }
            },
        )
    }

    override suspend fun persistServerCard(businessDate: String, card: MarketSurveyCardDto) = Unit
}

/** Records every market outbox enqueue so a test can assert the durable write actually happened. */
private class RecordingMarketSyncRepository : SyncRepository {
    data class Record(val itemId: String, val clientId: String, val cityId: String, val businessDate: String, val request: MarketSurveyEntryRequestDto)

    val records = mutableListOf<Record>()
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val items = mutableMapOf<String, MutableStateFlow<SyncQueueItem?>>()
    val pending = MutableStateFlow<List<sg.mesha.goatos.core.data.sync.MarketSurveyRecordPayload>>(emptyList())

    override fun observeStatus(): StateFlow<SyncStatus> = status
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = items.getOrPut(itemId) { MutableStateFlow(null) }
    override fun observePendingMarketSurveyRecords(): Flow<List<sg.mesha.goatos.core.data.sync.MarketSurveyRecordPayload>> = pending

    fun succeed(itemId: String) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId, opType = "MARKET_SURVEY_RECORD", idempotencyKey = "k", groupKey = "g",
            status = SyncItemStatus.SUCCEEDED, attemptCount = 1, maxAttempts = 5, conflict = false,
            createdAt = 0L, updatedAt = 0L, lastError = null,
        )
    }

    fun reject(itemId: String, reason: String) {
        items.getOrPut(itemId) { MutableStateFlow(null) }.value = SyncQueueItem(
            id = itemId, opType = "MARKET_SURVEY_RECORD", idempotencyKey = "k", groupKey = "g",
            status = SyncItemStatus.FAILED, attemptCount = 5, maxAttempts = 5, conflict = true,
            createdAt = 0L, updatedAt = 0L, lastError = reason,
        )
    }

    override suspend fun enqueueMarketSurveyRecord(
        clientId: String,
        cityId: String,
        businessDate: String,
        request: MarketSurveyEntryRequestDto,
    ): AppResult<String> {
        val id = "market-${records.size + 1}"
        records += Record(id, clientId, cityId, businessDate, request)
        return AppResult.Ok(id)
    }

    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueFeedTransportSubmit(groupKey: String, idempotencyKey: String, taskId: String, proofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedPackingComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, partitionLabel: String?, sessionNo: Int, targetDate: String, workflow: String, packingProofOutboxItemId: String, slotProofs: Map<String, sg.mesha.goatos.core.data.sync.FeedSlotProofSourcePayload>, answers: kotlinx.serialization.json.JsonObject): AppResult<String> = error("unused")
    override suspend fun enqueueFeedDirectionComplete(groupKey: String, idempotencyKey: String, parkId: String?, shedId: String, sessionNo: Int, targetDate: String, workflow: String): AppResult<String> = error("unused")
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
}
