package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.CrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.CountsBirthEventRequestDto
import sg.mesha.goatos.core.network.dto.CountsDeathEventRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.feature.counts.BirthDeathEvent
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathMode

/**
 * Validates that BirthDeathViewModel enforces UUID requirements for backend-owned identifiers.
 *
 * Birth form: parkId and shedId must be valid UUIDs when present.
 * Death form: goatId must be a valid UUID.
 *
 * FIX 2 follow-up from PR#11 review (#2/#3): ensures the form cannot enqueue requests with
 * invalid identifiers that the backend will reject with a terminal 400.
 */
class BirthDeathViewModelValidationTest {
    private lateinit var syncRepository: SyncRepository
    private lateinit var analytics: AnalyticsPort
    private lateinit var crashReporter: CrashReporter
    private lateinit var savedStateHandle: SavedStateHandle
    private lateinit var viewModel: BirthDeathViewModel

    @Before
    fun setUp() {
        syncRepository = NoopCountsSyncRepository()
        analytics = NoopAnalyticsPort()
        crashReporter = NoopTestCrashReporter()
        savedStateHandle = SavedStateHandle()
        viewModel = BirthDeathViewModel(syncRepository, analytics, crashReporter, savedStateHandle)
    }

    // --- Birth Mode Tests ---

    @Test
    fun `birth submit disabled when parkId is non-blank non-UUID`() = runTest {
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.PARK_ID, "Main Farm"))

        assertFalse(
            "Submit should be disabled when parkId is not a valid UUID",
            viewModel.state.value.canSubmit,
        )
        assertTrue(
            "Validation message should mention selector/lookup not yet available",
            viewModel.state.value.validationMessage?.contains("Selector-backed park/shed") ?: false,
        )
    }

    @Test
    fun `birth submit disabled when shedId is non-blank non-UUID`() = runTest {
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.SHED_ID, "Shed A"))

        assertFalse(
            "Submit should be disabled when shedId is not a valid UUID",
            viewModel.state.value.canSubmit,
        )
    }

    @Test
    fun `birth submit enabled when parkId and shedId are valid UUIDs`() = runTest {
        val validUuid = "550e8400-e29b-41d4-a716-446655440000"

        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.PARK_ID, validUuid))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.SHED_ID, validUuid))

        assertTrue(
            "Submit should be enabled when all required fields are present with valid UUID identifiers",
            viewModel.state.value.canSubmit,
        )
    }

    @Test
    fun `birth submit enabled when parkId and shedId are blank`() = runTest {
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, "Goat001"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, "2026-01-01"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, "2026-01-15"))

        assertTrue(
            "Submit should be enabled when park/shed are blank (optional fields)",
            viewModel.state.value.canSubmit,
        )
    }

    // --- Death Mode Tests ---

    @Test
    fun `death submit disabled when goatId is blank`() = runTest {
        viewModel.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.GOAT_ID, ""))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ROW_VERSION, "1"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Cause of death"))

        assertFalse(
            "Submit should be disabled when goatId is blank",
            viewModel.state.value.canSubmit,
        )
    }

    @Test
    fun `death submit disabled when goatId is non-UUID string`() = runTest {
        viewModel.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.GOAT_ID, "Goat123"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ROW_VERSION, "1"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Cause of death"))

        assertFalse(
            "Submit should be disabled when goatId is not a valid UUID",
            viewModel.state.value.canSubmit,
        )
        assertTrue(
            "Validation message should mention selector/lookup not yet available",
            viewModel.state.value.validationMessage?.contains("Selector-backed park/shed") ?: false,
        )
    }

    @Test
    fun `death submit enabled when goatId is valid UUID`() = runTest {
        val validUuid = "550e8400-e29b-41d4-a716-446655440000"

        viewModel.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.GOAT_ID, validUuid))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ROW_VERSION, "1"))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Cause of death"))

        assertTrue(
            "Submit should be enabled when goatId is a valid UUID with other required fields filled",
            viewModel.state.value.canSubmit,
        )
    }

    @Test
    fun `death submit disabled when rowVersion is blank`() = runTest {
        val validUuid = "550e8400-e29b-41d4-a716-446655440000"

        viewModel.onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.DEATH))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.GOAT_ID, validUuid))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.ROW_VERSION, ""))
        viewModel.onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, "Cause of death"))

        assertFalse(
            "Submit should be disabled when rowVersion is blank",
            viewModel.state.value.canSubmit,
        )
    }
}

// --- Fakes matching the real interface signatures (see SyncRepository / AnalyticsPort /
// CrashReporter). Only enqueueCountsBirth/enqueueCountsDeath return Ok; the rest are unused. ---

private class NoopCountsSyncRepository : SyncRepository {
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
    override fun observeStatus(): StateFlow<SyncStatus> = status
    override suspend fun enqueueShedSubmit(taskId: String, groupKey: String, idempotencyKey: String, request: SubmitTaskRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueReschedule(obligationId: String, groupKey: String, idempotencyKey: String, request: RescheduleObligationRequestDto): AppResult<String> = error("unused")
    override suspend fun enqueueProofUpload(groupKey: String, idempotencyKey: String, request: ProofUploadRequestDto, localFilePath: String, durationMs: Long?): AppResult<String> = error("unused")
    override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun enqueueVerificationVerdict(itemId: String, decision: String, reason: String?, rowVersion: Int): AppResult<String> = error("unused")
    override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
    override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
    override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
    override suspend fun triggerDrain() = Unit
    override suspend fun enqueueCountsBirth(groupKey: String, idempotencyKey: String, request: CountsBirthEventRequestDto): AppResult<String> = AppResult.Ok("outbox-birth-1")
    override suspend fun enqueueCountsDeath(groupKey: String, idempotencyKey: String, request: CountsDeathEventRequestDto): AppResult<String> = AppResult.Ok("outbox-death-1")
}

private class NoopAnalyticsPort : AnalyticsPort {
    override fun track(event: String, props: Map<String, String>) {}
    override fun setUserProperty(name: String, value: String?) {}
    override fun setUserId(id: String?) {}
}

private class NoopTestCrashReporter : CrashReporter {
    override fun recordException(throwable: Throwable, message: String?) {}
    override fun log(message: String) {}
    override fun setCustomKey(key: String, value: String) {}
}
