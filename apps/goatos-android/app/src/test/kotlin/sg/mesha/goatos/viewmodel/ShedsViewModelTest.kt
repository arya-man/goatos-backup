package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.core.analytics.AnalyticsEvents
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.Resource
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.ExecutionRepository
import sg.mesha.goatos.core.data.cache.ScanRosterRowEntity
import sg.mesha.goatos.core.data.cache.StatusCount
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.core.network.dto.ExecutionFilterOptionsDto
import sg.mesha.goatos.core.network.dto.ExecutionParkOptionDto
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.dto.VaccinationExecutionResponseDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionRowDto
import sg.mesha.goatos.core.network.dto.VaccinationExecutionShedDrilldownDto
import sg.mesha.goatos.feature.sheds.ShedsEvent
import sg.mesha.goatos.feature.sheds.ShedStatus
import java.time.LocalDate

/**
 * ShedsViewModel unit tests covering the calendar-drilldown park-pin defect: a drive-card tap
 * from Calendar seeds `_selectedParkId` from the `parkId` nav arg (so the list scopes correctly
 * on first load), but [ShedsUiState.selectedParkId] used to be hardcoded to that seed value
 * forever — never reflecting [ShedsEvent.SelectPark] — so any "widen back to all parks" UI
 * built on it would look like it worked (the request goes cross-park) while still reporting the
 * stale pinned park. This asserts the state field tracks the live selection, including back to
 * null ("all parks") after SelectPark(null).
 */
@OptIn(ExperimentalCoroutinesApi::class)
class ShedsViewModelTest {

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `clearing the pinned park via SelectPark(null) returns state to all parks`() = runTest(dispatcher) {
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(shedId = "shed-1", parkId = "park-cbe", parkName = "Coimbatore"),
                ),
                filterOptions = ExecutionFilterOptionsDto(
                    parks = listOf(
                        ExecutionParkOptionDto(parkId = "park-cbe", name = "Coimbatore"),
                        ExecutionParkOptionDto(parkId = "park-mds", name = "Madurai"),
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsPinVmBootstrapRepository(),
            savedStateHandle = SavedStateHandle(
                mapOf("calendarHosted" to "true", "parkId" to "park-cbe"),
            ),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Seeded from the calendar drive-card's parkId nav arg: pinned on first load.
        assertEquals("park-cbe", vm.state.value.selectedParkId)
        assertTrue(vm.state.value.parkFilters.first { it.parkId == "park-cbe" }.isSelected)

        // Widen back to all parks — must go through the same selectPark() flow that drives the
        // query (setting _selectedParkId to null), not bypass it.
        vm.onEvent(ShedsEvent.SelectPark(null))
        advanceUntilIdle()

        assertNull(vm.state.value.selectedParkId)
        assertTrue(vm.state.value.parkFilters.none { it.isSelected })
    }

    @Test
    fun `VACCINATION_SHEDS_VIEWED fires once on first successful load with operator KIND`() = runTest(dispatcher) {
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(shedId = "shed-1", parkId = "park-cbe", parkName = "Coimbatore"),
                ),
            ),
        )
        val analytics = ShedsRecordingAnalytics()
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = analytics,
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val viewedEvents = analytics.events.filter { it.first == AnalyticsEvents.VACCINATION_SHEDS_VIEWED }
        assertEquals(1, viewedEvents.size)
        assertEquals("operator", viewedEvents.single().second[AnalyticsEvents.Params.KIND])
    }

    @Test
    fun `VACCINATION_SHEDS_VIEWED reports leadership KIND for a leadership role`() = runTest(dispatcher) {
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(shedId = "shed-1", parkId = "park-cbe", parkName = "Coimbatore"),
                ),
            ),
        )
        val analytics = ShedsRecordingAnalytics()
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = analytics,
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "ceo"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val viewedEvents = analytics.events.filter { it.first == AnalyticsEvents.VACCINATION_SHEDS_VIEWED }
        assertEquals(1, viewedEvents.size)
        assertEquals("leadership", viewedEvents.single().second[AnalyticsEvents.Params.KIND])
    }

    @Test
    fun `VACCINATION_SHEDS_VIEWED does not re-fire on park filter change, day change, or refresh`() = runTest(dispatcher) {
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(shedId = "shed-1", parkId = "park-cbe", parkName = "Coimbatore"),
                ),
                filterOptions = ExecutionFilterOptionsDto(
                    parks = listOf(
                        ExecutionParkOptionDto(parkId = "park-cbe", name = "Coimbatore"),
                        ExecutionParkOptionDto(parkId = "park-mds", name = "Madurai"),
                    ),
                ),
            ),
        )
        val analytics = ShedsRecordingAnalytics()
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = analytics,
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()
        fun viewedCount() = analytics.events.count { it.first == AnalyticsEvents.VACCINATION_SHEDS_VIEWED }
        assertEquals(1, viewedCount())

        // Re-emits state via a new query (SelectPark re-triggers observedResource).
        vm.onEvent(ShedsEvent.SelectPark("park-mds"))
        advanceUntilIdle()
        assertEquals(1, viewedCount())

        // Re-emits state via _selectedDay in the transientState combine.
        val yesterday = LocalDate.now().minusDays(1).toString()
        vm.onEvent(ShedsEvent.SelectDay(yesterday))
        advanceUntilIdle()
        assertEquals(1, viewedCount())

        // Re-emits state via _isRefreshing/_isOffline flags in the transientState combine.
        vm.onEvent(ShedsEvent.Refresh)
        advanceUntilIdle()
        assertEquals(1, viewedCount())
    }

    @Test
    fun `adherence card stays visible but incomplete when more pages can change full day totals`() = runTest(dispatcher) {
        val today = LocalDate.now().toString()
        val firstPageRows = listOf(
            VaccinationExecutionRowDto(
                shedId = "shed-page-1",
                shedName = "Godel 1",
                parkId = "park-cbe",
                parkName = "Coimbatore",
                dueDate = today,
                targetCount = 20,
                openCount = 10,
                doneCount = 10,
                acceptedCount = 8,
                reviewCount = 2,
                workState = "verification_pending",
                sopStatus = "submitted",
            ),
        )
        val secondPageRows = listOf(
            VaccinationExecutionRowDto(
                shedId = "shed-page-2",
                shedName = "Yashoda 1",
                parkId = "park-cbe",
                parkName = "Coimbatore",
                dueDate = today,
                targetCount = 40,
                openCount = 15,
                doneCount = 25,
                acceptedCount = 20,
                reviewCount = 5,
                workState = "verification_pending",
                sopStatus = "submitted",
            ),
        )
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = firstPageRows,
                nextCursor = "cursor-page-2",
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "ceo_internal"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val adherence = vm.state.value.adherence
        assertTrue("drive-day adherence card should still be present", adherence != null)
        assertEquals(
            "page-one counts must not be presented as final full-day adherence while another page exists",
            false,
            adherence!!.isComplete,
        )
        assertEquals(60, executionCounts(firstPageRows + secondPageRows).target)
        assertTrue(vm.state.value.hasMore)
    }

    /**
     * Pins the operator rollover rule: previous-date completed sheds must not appear on
     * today's list, while unfinished backlog remains visible.
     */
    @Test
    fun `a completed backlog shed is hidden from today's list`() = runTest(dispatcher) {
        val today = LocalDate.now()
        val completedDueDate = today.minusDays(2).toString()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    // The still-open shed today's list is built around.
                    VaccinationExecutionRowDto(
                        shedId = "shed-open",
                        shedName = "Gandhi 1",
                        parkId = "park-cbe",
                        parkName = "Coimbatore",
                        dueDate = today.toString(),
                        targetCount = 5,
                        openCount = 3,
                        doneCount = 2,
                        acceptedCount = 2,
                        workState = "open",
                        sopStatus = "open",
                    ),
                    // Finished days ago, fully accepted, no open/review work left.
                    VaccinationExecutionRowDto(
                        shedId = "shed-done",
                        shedName = "Godel 1",
                        parkId = "park-cbe",
                        parkName = "Coimbatore",
                        dueDate = completedDueDate,
                        targetCount = 5,
                        openCount = 0,
                        doneCount = 5,
                        acceptedCount = 5,
                        workState = "completed",
                        sopStatus = "accepted",
                        verificationStatus = "accepted",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val rows = vm.state.value.rows
        assertEquals(
            "completed prior-date shed must not appear on today's list",
            null,
            rows.firstOrNull { it.shedId == "shed-done" },
        )
        assertTrue(rows.any { it.shedId == "shed-open" })
    }

    /**
     * Maintainer-reported: Godel 1 rendered "3 TARGETED · 1 OPEN · 2 DONE · 2 ACCEPTED" and still
     * refused the tap even after the verifier sent work back.
     *
     * That is the verifier reject/reopen loop. A shed submitted, then partly sent back, may keep
     * terminal submission fields while its `workState` becomes rejected/deferred and its open COUNT
     * climbs back above zero. The record-only predicate must honor that explicit redo state.
     *
     * Remaining open work must always win -- otherwise the reopened animal can never be worked.
     */
    @Test
    fun `a submitted shed with work reopened stays openable`() = runTest(dispatcher) {
        val today = LocalDate.now()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(
                        shedId = "shed-reopened",
                        shedName = "Godel 1",
                        parkId = "park-cbe",
                        parkName = "Coimbatore",
                        dueDate = today.toString(),
                        targetCount = 3,
                        openCount = 1,
                        doneCount = 2,
                        acceptedCount = 2,
                        // Terminal status carried over from the original submission...
                        workState = "rejected",
                        sopStatus = "accepted",
                        verificationStatus = "accepted",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.rows.firstOrNull { it.shedId == "shed-reopened" }
        assertTrue("reopened shed must stay on the list", row != null)
        assertEquals(
            "a shed with open work must never be gated as already-submitted",
            false,
            row!!.opensRecordOnly,
        )
    }

    @Test
    fun `a submitted shed with lagging open count is NOT locked (card-lock invariant)`() = runTest(dispatcher) {
        // RECONCILED for the vaccination card-lock invariant (CORE INVARIANT: only a FINAL
        // SUBMIT locks the card; sopStatus="submitted"/"needs_review" describes evidence state,
        // not remaining work). This case predates operatorCanContinue: the fake DTO below omits
        // it, so opensSubmittedRecordOnly() falls back to the openCount guard — and openCount=3
        // means 3 animals still have no completion evidence, so the card must NOT lock. The old
        // expectation here (sopStatus="submitted" wins over a stale open count) was exactly the
        // field bug: a needs_review/submitted card with real open work got refused entry. The
        // backend-owned field (operatorCanContinue) is now the source of truth in production;
        // this fallback path only matters for API responses that predate the field, and even
        // there it must never lock on remaining open work.
        val today = LocalDate.now()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(
                        shedId = "shed-submitted",
                        shedName = "Castro 1",
                        parkId = "park-cpt",
                        parkName = "CPT",
                        dueDate = today.toString(),
                        targetCount = 3,
                        openCount = 3,
                        doneCount = 3,
                        acceptedCount = 0,
                        reviewCount = 3,
                        workState = "verification_pending",
                        sopStatus = "submitted",
                        verificationStatus = "pending",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.rows.firstOrNull { it.shedId == "shed-submitted" }
        assertTrue("submitted shed must stay visible for review", row != null)
        assertEquals(
            "open work must veto record-only even when sopStatus reads submitted/needs_review",
            false,
            row!!.opensRecordOnly,
        )
    }

    @Test
    fun `a shed with backend operatorCanContinue=false locks even though openCount is stale`() = runTest(dispatcher) {
        // Backend field wins over any local inference: a genuine final submission
        // (operatorCanContinue=false, operatorLockedReason=final_submitted) must lock the card
        // even if a stale openCount value were ever to disagree.
        val today = LocalDate.now()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(
                        shedId = "shed-final",
                        shedName = "Castro 1",
                        parkId = "park-cpt",
                        parkName = "CPT",
                        dueDate = today.toString(),
                        targetCount = 3,
                        openCount = 0,
                        doneCount = 3,
                        acceptedCount = 3,
                        reviewCount = 0,
                        workState = "completed",
                        sopStatus = "accepted",
                        verificationStatus = "accepted",
                        operatorCanContinue = false,
                        operatorLockedReason = "final_submitted",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.rows.firstOrNull { it.shedId == "shed-final" }
        assertTrue("finalized shed must stay visible for record", row != null)
        assertEquals(true, row!!.opensRecordOnly)
    }

    @Test
    fun `a proof uploaded shed before submit stays openable for finalize`() = runTest(dispatcher) {
        val today = LocalDate.now()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(
                        shedId = "shed-proof-ready",
                        shedName = "Castro 1",
                        parkId = "park-cbe",
                        parkName = "Coimbatore",
                        dueDate = today.toString(),
                        targetCount = 3,
                        openCount = 0,
                        doneCount = 3,
                        acceptedCount = 0,
                        workState = "open",
                        sopStatus = "open",
                        proofStatus = "uploaded",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.rows.firstOrNull { it.shedId == "shed-proof-ready" }
        assertTrue("proof-ready shed must stay on the list", row != null)
        assertTrue("proof-ready shed remains date-open", row!!.canOpen)
        assertEquals(
            "uploaded proof alone must not lock the card before finalize/submit",
            false,
            row.opensRecordOnly,
        )
    }

    @Test
    fun `verification pending alone without terminal sopStatus must not trigger record-only`() = runTest(dispatcher) {
        val today = LocalDate.now()
        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = listOf(
                    VaccinationExecutionRowDto(
                        shedId = "shed-verification-pending",
                        shedName = "Test Shed",
                        parkId = "park-test",
                        parkName = "Test Park",
                        dueDate = today.toString(),
                        targetCount = 5,
                        openCount = 4,
                        doneCount = 1,
                        acceptedCount = 0,
                        reviewCount = 1,
                        workState = "in_progress",
                        sopStatus = "in_progress",
                        verificationStatus = "pending",
                    ),
                ),
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.rows.firstOrNull { it.shedId == "shed-verification-pending" }
        assertTrue("partial proof shed must stay on the list", row != null)
        assertEquals(
            "verificationStatus=pending while operator can continue must not show In review",
            "In progress",
            row!!.statusLabel,
        )
        assertEquals(
            "verificationStatus=pending alone (partial evidence, no terminal sopStatus) must not lock",
            false,
            row.opensRecordOnly,
        )
    }

    /**
     * RED→GREEN: Shed card status must be computed from ALL rows matching its identity,
     * not from paginated subsets. Without backend summaries, a card whose rows straddle
     * an unloaded page would show incorrect status/counts based only on page-1 rows.
     * With backend summaries, the status stays authoritative even when only page 1 is loaded.
     *
     * Scenario: A card has 3 rows total (page 1: 2 rows, page 2: 1 row). Page-1 rows
     * show status=PENDING (2 open, 0 redo), but the card actually has 1 redo row on page 2.
     * Backend summary correctly computes status=SENT_BACK from all 3 rows. Client should
     * render SENT_BACK (backend summary) not PENDING (page-1 only).
     */
    @Test
    fun `backend card summary overrides page-1-only row computation when rows straddle pages`() = runTest(dispatcher) {
        val today = LocalDate.now().toString()
        // Simulate page 1 with 2 rows for the card (both show open work, no redo)
        val page1Rows = listOf(
            VaccinationExecutionRowDto(
                shedId = "shed-split",
                shedName = "Gandhi Multi",
                parkId = "park-cbe",
                parkName = "Coimbatore",
                dueDate = today,
                targetCount = 10,
                openCount = 8,
                doneCount = 2,
                workState = "due",
                sopStatus = "open",
            ),
            VaccinationExecutionRowDto(
                shedId = "shed-split",
                shedName = "Gandhi Multi",
                parkId = "park-cbe",
                parkName = "Coimbatore",
                partition = "Part 2",
                dueDate = today,
                targetCount = 10,
                openCount = 9,
                doneCount = 1,
                workState = "due",
                sopStatus = "open",
            ),
        )

        // Backend summary computed from ALL 3 rows (including page 2's 1 redo row)
        // tells us the card actually has needsRedo=true → status should be SENT_BACK
        val cardSummaries = mapOf(
            "shed:shed-split|partition:whole" to sg.mesha.goatos.core.network.dto.ShedCardSummaryDto(
                shedId = "shed-split",
                partitionLabel = null,
                status = "rejected",  // Backend computed from all rows: this card has rejected work
                doneCount = 3,
                targetCount = 30,    // Max from all 3 rows
                openCount = 18,      // Max from all 3 rows
                needsRedo = true,    // Set because one page-2 row has rejected/deferred state
                vaccineGroups = emptyList(),
            ),
        )

        val repo = FakeShedsPinVmExecutionRepository(
            VaccinationExecutionResponseDto(
                rows = page1Rows,
                nextCursor = "cursor-page-2",
                cardSummaries = cardSummaries,
            ),
        )
        val vm = ShedsViewModel(
            repo = repo,
            crashReporter = NoopCrashReporter(),
            analytics = NoopAnalytics(),
            bootstrapRepository = FakeShedsRoleBootstrapRepository(role = "operator"),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch { vm.state.collect {} }
        advanceUntilIdle()

        // Verify: card status is SENT_BACK (from backend summary), not PENDING (from page-1 only)
        val cardRow = vm.state.value.rows.first()
        assertEquals(
            "Card status must reflect backend summary (all rows), not page-1-only rows",
            ShedStatus.SENT_BACK,
            cardRow.status,
        )
        assertEquals(30, cardRow.inShed.toInt())  // From backend summary
        assertEquals(18, cardRow.due.toInt())    // From backend summary
        assertEquals(3, cardRow.done.toInt())    // From backend summary
    }
}

private class ShedsRecordingAnalytics : AnalyticsPort {
    val events = mutableListOf<Pair<String, Map<String, String>>>()

    override fun track(event: String, props: Map<String, String>) {
        events.add(event to props)
    }

    override fun setUserProperty(name: String, value: String?) {}

    override fun setUserId(id: String?) {}
}

private class FakeShedsRoleBootstrapRepository(
    private val role: String = "operator",
) : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")
    override suspend fun operatorProfile(): BootstrapOperatorProfileDto =
        BootstrapOperatorProfileDto(primaryRoleHint = role)
}

private class FakeShedsPinVmExecutionRepository(
    private val response: VaccinationExecutionResponseDto,
) : ExecutionRepository {
    override suspend fun rows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        cursor: String?,
        includeFilterOptions: Boolean,
    ): VaccinationExecutionResponseDto = response

    override fun observeRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Flow<Resource<VaccinationExecutionResponseDto>> =
        MutableStateFlow(
            // Fake mimics the real cache-first repo's park scoping: rows are filtered to the
            // requested parkId, but filterOptions.parks always lists every park (the full
            // cross-park option set), matching the real API contract this screen relies on.
            response.copy(rows = response.rows.filter { parkId == null || it.parkId == parkId }),
        ).map { Resource(data = it, lastSyncedAt = 1L) }

    override suspend fun refreshRows(
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = Result.success(Unit)

    override suspend fun appendRows(
        cursor: String,
        parkId: String?,
        workState: String?,
        asOf: String?,
        dueBefore: String?,
        openOnly: Boolean?,
        limit: Int?,
        includeFilterOptions: Boolean,
    ): Result<Unit> = Result.success(Unit)

    override suspend fun shed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): VaccinationExecutionShedDrilldownDto =
        error("unused")

    override fun observeShed(
        shedId: String,
        asOf: String?,
        dueBefore: String?,
        limit: Int?,
        partitionLabel: String?,
    ): Flow<Resource<VaccinationExecutionShedDrilldownDto>> = error("unused")

    override suspend fun refreshShed(shedId: String, asOf: String?, dueBefore: String?, limit: Int?, partitionLabel: String?): Result<Unit> =
        error("unused")

    override suspend fun findScanRosterByTag(
        shedId: String,
        taskId: String?,
        normalizedTag: String,
        partitionLabel: String?,
    ): ScanRosterRowEntity? = null

    override fun observeScanRosterRows(
        shedId: String,
        taskId: String?,
        windowSize: Int,
        partitionLabel: String?,
    ): Flow<List<ScanRosterRowEntity>> = kotlinx.coroutines.flow.flowOf(emptyList())

    override fun observeScanRosterTotal(shedId: String, taskId: String?, partitionLabel: String?): Flow<Int> = kotlinx.coroutines.flow.flowOf(0)

    override fun observeScanRosterDoneGoatIds(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<String>> =
        kotlinx.coroutines.flow.flowOf(emptyList())

    override suspend fun scanRosterRowsByGoatIds(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<ScanRosterRowEntity> = emptyList()

    override suspend fun refreshScanRoster(shedId: String, taskId: String?, limit: Int?, partitionLabel: String?): Result<Unit> = Result.success(Unit)

    override fun observeScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): Flow<List<StatusCount>> =
        kotlinx.coroutines.flow.flowOf(emptyList())

    override suspend fun getScanRosterStatusCountsFor(
        shedId: String,
        taskId: String?,
        goatIds: List<String>,
        partitionLabel: String?,
    ): List<StatusCount> = emptyList()

    override suspend fun getScanRosterStatusCounts(shedId: String, taskId: String?, partitionLabel: String?): List<StatusCount> = emptyList()

    override suspend fun openScanRosterRows(shedId: String, taskId: String?, partitionLabel: String?): List<ScanRosterRowEntity> =
        emptyList()

    override suspend fun siblingPartitionOpenRows(shedId: String, taskId: String?, activePartitionLabel: String?): List<ScanRosterRowEntity> =
        emptyList()

    override suspend fun otherShedOpenRows(shedId: String, taskId: String?): List<ScanRosterRowEntity> =
        emptyList()

}

private class FakeShedsPinVmBootstrapRepository : BootstrapRepository {
    override suspend fun loadNavState(): NavState = error("unused")

    override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
}
