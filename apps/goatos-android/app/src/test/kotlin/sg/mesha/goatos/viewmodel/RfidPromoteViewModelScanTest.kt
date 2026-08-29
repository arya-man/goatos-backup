package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.ViewModelStore
import androidx.paging.PagingData
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.StandardTestDispatcher
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
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.AwaitingRfidFilter
import sg.mesha.goatos.core.data.AwaitingRfidRepository
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.TemporaryTaggedGoatDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.feature.counts.RfidPromoteEvent
import sg.mesha.goatos.feature.counts.RfidPromoteField
import sg.mesha.goatos.rfid.FakeScanSource

/**
 * Promoting a temporary tag to a permanent RFID is done with the new ear tag in hand, so the two
 * identifier inputs accept a Bluetooth scan instead of a 15-digit transcription
 * (`docs/mobile/rfid-keyboard-reader.md` — the V1 BT-HID keyboard-wedge reader, reached through the
 * same `ScanSource` port the Submit recording form uses).
 *
 * These lock in the three things that make the scanner safe on a RETAG screen:
 *  - a completed read lands in the field the operator armed, and NOTHING else;
 *  - capture stops the moment a tag is captured, so the next animal's tag cannot silently overwrite
 *    the identifier about to be written to a goat;
 *  - the reader is released on submit and on leaving the screen — capture consumes hardware key
 *    events app-wide while enabled, so a leaked listener would eat another screen's input.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class RfidPromoteViewModelScanTest {
    private val dispatcher = StandardTestDispatcher()

    private lateinit var repo: FakeAwaitingRfidRepository
    private lateinit var syncRepository: RecordingPromoteSyncRepository
    private lateinit var scanSource: FakeScanSource
    private lateinit var savedStateHandle: SavedStateHandle

    @Before
    fun setUp() {
        Dispatchers.setMain(dispatcher)
        repo = FakeAwaitingRfidRepository()
        syncRepository = RecordingPromoteSyncRepository()
        scanSource = FakeScanSource()
        savedStateHandle = SavedStateHandle(mapOf("goat_id" to GOAT_ID))
    }

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun newViewModel() = RfidPromoteViewModel(
        repo,
        syncRepository,
        scanSource,
        NoopPromoteAnalyticsPort(),
        NoopPromoteCrashReporter(),
        savedStateHandle,
    )

    @Test
    fun `a scanned tag fills the permanent RFID and releases the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY))
        advanceUntilIdle()
        assertTrue("The reader is listening", scanSource.isStarted)
        assertEquals(RfidPromoteField.PRIMARY, vm.state.value.scanningField)

        scanSource.emit("982000123456789")
        advanceUntilIdle()

        assertEquals("982000123456789", vm.state.value.rfidInput)
        assertTrue("A scanned RFID enables the promote button", vm.state.value.canSubmit)
        assertNull("Capture released after the read", vm.state.value.scanningField)
        assertFalse("The BT-HID reader was stopped", scanSource.isStarted)
    }

    @Test
    fun `scanning the second identifier hands the reader over and leaves the first alone`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.RfidChanged("982000111111111"))
        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.SECONDARY))
        advanceUntilIdle()
        scanSource.emit("982000222222222")
        advanceUntilIdle()

        assertEquals("982000222222222", vm.state.value.rfid2Input)
        assertEquals("The primary identifier is untouched", "982000111111111", vm.state.value.rfidInput)
    }

    @Test
    fun `tapping the scanning field again stops the reader`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY))
        advanceUntilIdle()
        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY))
        advanceUntilIdle()

        assertNull(vm.state.value.scanningField)
        assertFalse(scanSource.isStarted)
    }

    @Test
    fun `a scanned identifier promotes exactly as a typed one would`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY))
        advanceUntilIdle()
        scanSource.emit("982000123456789")
        advanceUntilIdle()
        vm.onEvent(RfidPromoteEvent.Submit)
        advanceUntilIdle()

        assertEquals("982000123456789", syncRepository.lastPermanentIdentifier)
        assertEquals("row_version comes from the cached row, never the operator", ROW_VERSION, syncRepository.lastRowVersion)
        assertFalse("Submitting releases the reader", scanSource.isStarted)
    }

    @Test
    fun `birth tag route loads without the removed awaiting RFID page cache`() = runTest(dispatcher) {
        repo.cachedGoat = null
        savedStateHandle = SavedStateHandle(
            mapOf(
                "goat_id" to GOAT_ID,
                "display_id" to "G-77",
                "temporary_identifier" to "CPT-00042",
                "location_display" to "CPT / K0",
                "row_version" to ROW_VERSION,
            ),
        )

        val vm = newViewModel()
        advanceUntilIdle()

        assertFalse(vm.state.value.notFound)
        assertEquals("G-77", vm.state.value.displayId)
        assertEquals("CPT-00042", vm.state.value.temporaryIdentifier)
        assertEquals("CPT / K0", vm.state.value.locationDisplay)

        vm.onEvent(RfidPromoteEvent.RfidChanged("982000123456789"))
        vm.onEvent(RfidPromoteEvent.Submit)
        advanceUntilIdle()

        assertEquals(ROW_VERSION, syncRepository.lastRowVersion)
    }

    @Test
    fun `terminal promote failure keeps the awaiting RFID row and permits correction`() = runTest(dispatcher) {
        val vm = newViewModel()
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.RfidChanged("982000123456789"))
        vm.onEvent(RfidPromoteEvent.Submit)
        advanceUntilIdle()

        assertNull("enqueue alone must not destructively delete the cached goat", repo.forgotten)

        syncRepository.emitTerminalFailure("RFID is already assigned.")
        advanceUntilIdle()

        assertNull(repo.forgotten)
        assertTrue(vm.state.value.canSubmit)
    }

    @Test
    fun `leaving the screen releases the reader`() = runTest(dispatcher) {
        // Through the real store, so the release runs on the SAME path the nav host takes when the
        // destination is popped — not a hand-called cleanup method.
        val store = ViewModelStore()
        @Suppress("UNCHECKED_CAST")
        val factory = object : ViewModelProvider.Factory {
            override fun <T : ViewModel> create(modelClass: Class<T>): T = newViewModel() as T
        }
        val vm = ViewModelProvider(store, factory)[RfidPromoteViewModel::class.java]
        advanceUntilIdle()

        vm.onEvent(RfidPromoteEvent.ToggleRfidScan(RfidPromoteField.PRIMARY))
        advanceUntilIdle()
        assertTrue(scanSource.isStarted)

        store.clear()

        assertFalse("A popped screen must not keep consuming hardware key events", scanSource.isStarted)
    }

    private companion object {
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val ROW_VERSION = 7
    }
}

// --- Fakes -----------------------------------------------------------------------------------

/** Mirrors Room: the tapped awaiting-RFID row is already cached, carrying its own row_version. */
private class FakeAwaitingRfidRepository : AwaitingRfidRepository {
    var forgotten: String? = null
    var cachedGoat: TemporaryTaggedGoatDto? = TemporaryTaggedGoatDto(
        goatId = GOAT_ID,
        displayId = "G-77",
        temporaryIdentifier = "TEMP-42",
        locationDisplay = "North Park / Shed A",
        rowVersion = ROW_VERSION,
    )

    override fun awaiting(filter: AwaitingRfidFilter): Flow<PagingData<TemporaryTaggedGoatDto>> =
        flowOf(PagingData.empty())

    override suspend fun forgetPromoted(goatId: String) {
        forgotten = goatId
    }

    override suspend fun findCached(goatId: String): TemporaryTaggedGoatDto? = cachedGoat

    private companion object {
        const val GOAT_ID = "44444444-4444-4444-4444-444444444444"
        const val ROW_VERSION = 7
    }
}

/** Captures the enqueued promote so a test can assert the exact wire values. */
private class RecordingPromoteSyncRepository : SyncRepository {
    var lastPermanentIdentifier: String? = null
    var lastSecondaryIdentifier: String? = null
    var lastRowVersion: Int? = null

    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    private val item = MutableStateFlow<SyncQueueItem?>(null)
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int, measurement: VerificationVerdictMeasurementDto?): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = item
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueuePromoteIdentifier(
        groupKey: String,
        idempotencyKey: String,
        permanentIdentifier: String,
        rowVersion: Int,
        secondaryIdentifier: String?,
    ): AppResult<String> {
        lastPermanentIdentifier = permanentIdentifier
        lastSecondaryIdentifier = secondaryIdentifier
        lastRowVersion = rowVersion
        return AppResult.Ok("outbox-promote-1")
    }

    fun emitTerminalFailure(message: String) {
        item.value = SyncQueueItem(
            id = "outbox-promote-1",
            opType = "COUNTS_PROMOTE_IDENTIFIER",
            idempotencyKey = "promote-key",
            groupKey = "goat-1",
            status = sg.mesha.goatos.core.data.sync.SyncItemStatus.FAILED,
            attemptCount = 1,
            maxAttempts = 3,
            conflict = true,
            createdAt = 1L,
            updatedAt = 2L,
            lastError = message,
        )
    }
}

private class NoopPromoteAnalyticsPort : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopPromoteCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
