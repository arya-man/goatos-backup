package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import kotlinx.serialization.json.JsonPrimitive
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.DisciplineRepository
import sg.mesha.goatos.core.data.HrmsAccessDenied
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.network.dto.EnquiryDetailDto
import sg.mesha.goatos.core.network.dto.EnquiryDto
import sg.mesha.goatos.core.network.dto.EnquiryPageDto
import sg.mesha.goatos.core.network.dto.EnquiryQuestionDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.RecordViolationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.SubmitEnquiryRequestDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto
import sg.mesha.goatos.core.network.dto.ViolationPersonOptionDto
import sg.mesha.goatos.core.network.dto.ViolationTypeOptionDto
import sg.mesha.goatos.core.network.dto.ViolationsPageDto
import sg.mesha.goatos.core.network.dto.MonthOptionDto
import sg.mesha.goatos.core.network.dto.ViolationDto
import sg.mesha.goatos.core.network.dto.ViolationSummaryDto
import sg.mesha.goatos.feature.penvisits.EnquiryReportEvent
import sg.mesha.goatos.feature.penvisits.RecordViolationEvent
import sg.mesha.goatos.feature.penvisits.ViolationsListEvent

/**
 * HRMS on the phone (maintainer decisions 2026-09-30): the For me enquiries + record entry follow
 * the SERVER's 403, the report refuses an incomplete form locally and sends exactly what the park
 * head chose, and a refused write surfaces the server's words and spends its key.
 */
@OptIn(ExperimentalCoroutinesApi::class)
class HrmsViewModelsTest {

    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun forMeHidesWhatTheServerRefusesAndListsOpenEnquiries() = runTest {
        val repo = FakeDisciplineRepository(enquiries = Result.success(EnquiryPageDto(items = listOf(enquiry()))))
        repo.violations = Result.failure(HrmsAccessDenied(RuntimeException("403")))
        val vm = ForMeHrmsViewModel(repo, NoopAddAnalyticsPort(), NoopAddCrashReporter())
        vm.refresh()
        assertTrue(vm.state.value.showEnquiries)
        assertEquals(listOf("enq-1"), vm.state.value.enquiries.map { it.enquiryId })
        assertFalse("a 403 on violations hides Record a violation", vm.state.value.showViolations)

        repo.enquiries = Result.failure(HrmsAccessDenied(RuntimeException("403")))
        repo.violations = Result.success(ViolationsPageDto())
        vm.refresh()
        assertFalse(vm.state.value.showEnquiries)
        assertTrue(vm.state.value.enquiries.isEmpty())
        assertTrue(vm.state.value.showViolations)
    }

    @Test
    fun forMeKeepsTheListOnATransientFailure() = runTest {
        val repo = FakeDisciplineRepository(enquiries = Result.success(EnquiryPageDto(items = listOf(enquiry()))))
        val vm = ForMeHrmsViewModel(repo, NoopAddAnalyticsPort(), NoopAddCrashReporter())
        vm.refresh()
        repo.enquiries = Result.failure(java.io.IOException("offline"))
        vm.refresh()
        assertEquals(1, vm.state.value.enquiries.size)
    }

    @Test
    fun reportRefusesAMissingRequiredAnswerAndATwiceAddedPerson() = runTest {
        val repo = FakeDisciplineRepository()
        val sync = FakeHrmsSyncRepository()
        val vm = reportVm(repo, sync)

        vm.onEvent(EnquiryReportEvent.Submit)
        assertEquals(HrmsLocalError.ANSWER_REQUIRED, vm.localError.value)
        assertNull(repo.submitted)

        vm.onEvent(EnquiryReportEvent.AnswerText("what_happened", "  Left the gate open  "))
        vm.onEvent(EnquiryReportEvent.AnswerYesNo("reported", false))
        vm.onEvent(EnquiryReportEvent.AddPerson)
        vm.onEvent(EnquiryReportEvent.AddPerson)
        vm.onEvent(EnquiryReportEvent.SetPerson(0, "p-amit"))
        vm.onEvent(EnquiryReportEvent.SetType(0, "negligence"))
        vm.onEvent(EnquiryReportEvent.SetPerson(1, "p-amit"))
        vm.onEvent(EnquiryReportEvent.SetType(1, "negligence"))
        vm.onEvent(EnquiryReportEvent.Submit)
        assertEquals(HrmsLocalError.PENALTY_TWICE, vm.localError.value)
        assertNull(repo.submitted)

        vm.onEvent(EnquiryReportEvent.RemovePerson(1))
        vm.onEvent(EnquiryReportEvent.AddPerson)
        vm.onEvent(EnquiryReportEvent.Submit)
        assertEquals(HrmsLocalError.PENALTY_INCOMPLETE, vm.localError.value)
        assertNull(repo.submitted)
    }

    @Test
    fun reportSendsWhatWasChosenAndReadsTheSubmittedReportBack() = runTest {
        val repo = FakeDisciplineRepository()
        val sync = FakeHrmsSyncRepository()
        val vm = reportVm(repo, sync)

        vm.onEvent(EnquiryReportEvent.AnswerText("what_happened", "  Left the gate open  "))
        vm.onEvent(EnquiryReportEvent.AnswerYesNo("reported", true))
        vm.onEvent(EnquiryReportEvent.AddPerson)
        vm.onEvent(EnquiryReportEvent.SetPerson(0, "p-amit"))
        vm.onEvent(EnquiryReportEvent.SetType(0, "negligence"))
        assertEquals("picking a violation never fills in money", "", vm.state.value.penalties[0].fine)
        vm.onEvent(EnquiryReportEvent.SetFine(0, "750"))
        vm.onEvent(EnquiryReportEvent.Submit)

        val sent = requireNotNull(repo.submitted)
        assertEquals(JsonPrimitive("Left the gate open"), sent.answers["what_happened"])
        assertEquals(JsonPrimitive(true), sent.answers["reported"])
        assertEquals(750, sent.penalties.single().fineRupees)
        assertEquals("p-amit", sent.penalties.single().personId)
        assertEquals(3, sent.rowVersion)
        assertTrue(vm.state.value.submitting)

        repo.detail = detail(submitted = true)
        sync.item.value = item(SyncItemStatus.SUCCEEDED)
        assertTrue(vm.state.value.submitted)
        assertFalse(vm.state.value.submitting)
        assertEquals("Left the gate open", vm.state.value.questions.first { it.id == "what_happened" }.text)
    }

    @Test
    fun aRefusedReportShowsTheServersWordsAndMintsAFreshKey() = runTest {
        val repo = FakeDisciplineRepository()
        val sync = FakeHrmsSyncRepository()
        val vm = reportVm(repo, sync)
        vm.onEvent(EnquiryReportEvent.AnswerText("what_happened", "x"))
        vm.onEvent(EnquiryReportEvent.AnswerYesNo("reported", false))
        vm.onEvent(EnquiryReportEvent.Submit)
        val firstKey = repo.lastKey
        sync.item.value = item(SyncItemStatus.FAILED, conflict = true, error = "Someone already submitted this report.")
        assertEquals("Someone already submitted this report.", vm.state.value.message)
        assertFalse(vm.state.value.submitting)

        sync.item.value = null
        vm.onEvent(EnquiryReportEvent.Submit)
        assertNotEquals(firstKey, repo.lastKey)
    }

    @Test
    fun recordViolationNeverPricesTheMistakeAndSendsToday() = runTest {
        val repo = FakeDisciplineRepository()
        val sync = FakeHrmsSyncRepository()
        val vm = RecordViolationViewModel(repo, sync, NoopAddAnalyticsPort(), NoopAddCrashReporter(), SavedStateHandle())
        assertEquals(listOf("p-amit"), vm.state.value.people.map { it.key })
        assertEquals(7, vm.state.value.days.size)
        assertFalse(vm.state.value.canSubmit)

        vm.onEvent(RecordViolationEvent.SetPerson("p-amit"))
        vm.onEvent(RecordViolationEvent.SetType("negligence"))
        assertEquals("picking a violation never fills in money", "", vm.state.value.fine)
        vm.onEvent(RecordViolationEvent.Submit)
        val sent = requireNotNull(repo.recorded)
        assertNull("blank fine is no fine", sent.fineRupees)
        assertEquals(vm.state.value.days.first().key, sent.occurredOn)

        sync.item.value = item(SyncItemStatus.SUCCEEDED)
        assertTrue(vm.state.value.done)
        assertFalse(vm.state.value.queuedOffline)
    }

    @Test
    fun violationsListShowsTheBackendsMonthTotalsAndAppendsPages() = runTest {
        val repo = PagedViolationsRepository()
        val vm = ViolationsListViewModel(repo, NoopAddAnalyticsPort(), NoopAddCrashReporter())
        vm.onEvent(ViolationsListEvent.Refresh)
        var s = vm.state.value
        assertEquals("2026-09", s.month)
        assertEquals(listOf("v1", "v2"), s.rows.map { it.listKey })
        // The month's totals are the backend's whole-month figures, never summed from one page.
        assertEquals(5, s.count)
        assertEquals("₹2,600", s.fineLabel)
        assertTrue(s.rows[1].withdrawn)

        vm.onEvent(ViolationsListEvent.LoadMore)
        assertEquals(listOf("v1", "v2", "v3"), vm.state.value.rows.map { it.listKey })
        // A clock-in violation waiting for HR shows its status and its fact, and is never greyed.
        val waiting = vm.state.value.rows.last()
        assertTrue(waiting.waiting)
        assertFalse(waiting.withdrawn)
        assertTrue(waiting.detail.contains("40 min late"))
        vm.onEvent(ViolationsListEvent.LoadMore) // no further page: nothing requested
        assertEquals(2, repo.calls.size)

        vm.onEvent(ViolationsListEvent.SelectMonth("2026-08"))
        s = vm.state.value
        assertEquals("2026-08", repo.calls.last().first)
        assertEquals(listOf("a1"), s.rows.map { it.listKey })
    }

    private fun reportVm(repo: FakeDisciplineRepository, sync: FakeHrmsSyncRepository) = EnquiryReportViewModel(
        repo,
        sync,
        NoopAddAnalyticsPort(),
        NoopAddCrashReporter(),
        SavedStateHandle(mapOf(EnquiryReportViewModel.ARG_ENQUIRY_ID to "enq-1")),
    )

    private fun item(status: SyncItemStatus, conflict: Boolean = false, error: String? = null) = SyncQueueItem(
        id = "outbox-1",
        opType = "ENQUIRY_SUBMIT",
        idempotencyKey = "k",
        groupKey = "enquiry:enq-1",
        status = status,
        attemptCount = 1,
        maxAttempts = 5,
        conflict = conflict,
        createdAt = 0,
        updatedAt = 0,
        lastError = error,
    )
}

private fun enquiry(status: String = "open") = EnquiryDto(
    enquiryId = "enq-1",
    title = "Death enquiry",
    subjectLabel = "900215000123451 · Mandela 1 - Part 5",
    parkLabel = "Channapatna",
    dueAtLabel = "02/10/2026 10:00",
    status = status,
    statusLabel = if (status == "open") "Open" else "Submitted",
    rowVersion = 3,
)

private fun detail(submitted: Boolean = false) = EnquiryDetailDto(
    enquiry = enquiry(if (submitted) "submitted" else "open"),
    questions = listOf(
        EnquiryQuestionDto(id = "what_happened", kind = "text", title = "What happened?", required = true),
        EnquiryQuestionDto(id = "reported", kind = "yes_no", title = "Was it reported?", required = true),
    ),
    answers = if (submitted) {
        kotlinx.serialization.json.JsonObject(mapOf("what_happened" to JsonPrimitive("Left the gate open"), "reported" to JsonPrimitive(true)))
    } else {
        kotlinx.serialization.json.JsonObject(emptyMap())
    },
    types = listOf(ViolationTypeOptionDto(key = "negligence", title = "Negligence")),
    people = listOf(ViolationPersonOptionDto(personId = "p-amit", name = "Amit Kumar", designation = "Operator")),
    canSubmit = !submitted,
)

private class FakeDisciplineRepository(
    var enquiries: Result<EnquiryPageDto> = Result.success(EnquiryPageDto()),
) : DisciplineRepository {
    var detail: EnquiryDetailDto = detail()
    var violations: Result<ViolationsPageDto> = Result.success(
        ViolationsPageDto(
            types = listOf(ViolationTypeOptionDto(key = "negligence", title = "Negligence")),
            people = listOf(ViolationPersonOptionDto(personId = "p-amit", name = "Amit Kumar")),
        ),
    )
    var submitted: SubmitEnquiryRequestDto? = null
    var recorded: RecordViolationRequestDto? = null
    var lastKey: String? = null

    override suspend fun fetchOpenEnquiries(): Result<EnquiryPageDto> = enquiries
    override suspend fun fetchEnquiry(enquiryId: String): Result<EnquiryDetailDto> = Result.success(detail)
    override suspend fun fetchViolations(month: String?, cursor: String?): Result<ViolationsPageDto> = violations
    override suspend fun submitEnquiry(enquiryId: String, idempotencyKey: String, request: SubmitEnquiryRequestDto): AppResult<String> {
        submitted = request
        lastKey = idempotencyKey
        return AppResult.Ok("outbox-1")
    }
    override suspend fun recordViolation(idempotencyKey: String, request: RecordViolationRequestDto): AppResult<String> {
        recorded = request
        lastKey = idempotencyKey
        return AppResult.Ok("outbox-1")
    }
}

private class PagedViolationsRepository : DisciplineRepository {
    val calls = mutableListOf<Pair<String?, String?>>()
    override suspend fun fetchOpenEnquiries(): Result<EnquiryPageDto> = Result.success(EnquiryPageDto())
    override suspend fun fetchEnquiry(enquiryId: String): Result<EnquiryDetailDto> = Result.success(detail())
    override suspend fun fetchViolations(month: String?, cursor: String?): Result<ViolationsPageDto> {
        calls += month to cursor
        val months = listOf(MonthOptionDto("2026-09", "Sep 2026"), MonthOptionDto("2026-08", "Aug 2026"))
        val summary = ViolationSummaryDto(count = 5, fineRupees = 2600, fineLabel = "₹2,600", people = 4)
        return Result.success(
            when {
                month == "2026-08" -> ViolationsPageDto(months = months, month = "2026-08", items = listOf(ViolationDto(violationId = "a1")))
                cursor == "c2" -> ViolationsPageDto(months = months, month = "2026-09", summary = summary, items = listOf(ViolationDto(violationId = "v2"), ViolationDto(violationId = "v3", status = "pending", statusLabel = "Waiting for HR", detail = "Clocked in 9:10 am · General shift starts 8:30 am · 40 min late")))
                else -> ViolationsPageDto(
                    months = months,
                    month = "2026-09",
                    summary = summary,
                    items = listOf(ViolationDto(violationId = "v1"), ViolationDto(violationId = "v2", status = "withdrawn")),
                    nextCursor = "c2",
                )
            },
        )
    }
    override suspend fun submitEnquiry(enquiryId: String, idempotencyKey: String, request: SubmitEnquiryRequestDto): AppResult<String> = error("unused")
    override suspend fun recordViolation(idempotencyKey: String, request: RecordViolationRequestDto): AppResult<String> = error("unused")
}

private class FakeHrmsSyncRepository : SyncRepository {
    val item = MutableStateFlow<SyncQueueItem?>(null)
    private val status = MutableStateFlow(SyncStatus.empty(online = true))
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
}
