package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.network.dto.VerificationVerdictMeasurementDto

import android.view.KeyEvent
import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.ChannelBackedProofCaptureSource
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.ProofCaptureSource
import sg.mesha.goatos.core.analytics.AnalyticsPort
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.sync.SyncItemStatus
import sg.mesha.goatos.core.data.sync.SyncQueueItem
import sg.mesha.goatos.core.data.sync.SyncRepository
import sg.mesha.goatos.core.data.sync.SyncStatus
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedWeighingDraft
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingCapabilities
import sg.mesha.goatos.core.data.weighing.WeighingCsvExport
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedCache
import sg.mesha.goatos.core.data.weighing.WeighingOperatorSummary
import sg.mesha.goatos.core.data.weighing.WeighingPage
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalogCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerParkBucketsCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WEIGHING_LEADERSHIP_PAGE_SIZE
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScanMatch
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.WeighingTask
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskLookup
import sg.mesha.goatos.core.data.weighing.WeighingParkRef
import sg.mesha.goatos.core.data.weighing.WeighingTaskShed
import sg.mesha.goatos.core.data.weighing.WeighingTaskPage
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_ALL
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_OPERATORS
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.feature.weighing.plan.WeighingWizardStep
import sg.mesha.goatos.ui.Routes
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

@OptIn(ExperimentalCoroutinesApi::class)
class WeighingViewModelTest {

    // The dev flavour namespaces every scan with its shed so a tester's 5 physical tags behave
    // like 5 animals per shed. Unit tests run on that same variant, so without pinning it off
    // they assert "SHED1-9010..." instead of the identifier the app really stores.
    @Before
    fun disableDevScanNamespacing() {
        WeighingViewModel.scanScopePrefixOverride = false
    }

    @After
    fun restoreDevScanNamespacing() {
        WeighingViewModel.scanScopePrefixOverride = null
    }

    private val dispatcher = UnconfinedTestDispatcher()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    @Test
    fun `individual submit ready survives navigation before accepted sync restores`() {
        val state = WeighingUiState(
            hasScope = true,
            category = "individual_animal",
            visibleRows = listOf(
                WeighingRosterUiRow(
                    id = "row-1",
                    animalId = "901007000504407",
                    displayAnimalId = "901007000504407",
                    status = "Scanned",
                    weightSaved = true,
                    proofUploadStatus = ProofUploadStatus.SYNCED,
                    backendSynced = false,
                ),
            ),
        )

        assertTrue(state.individualSubmitReady)
    }

    /**
     * The oversight surface answers "who did what", from the BACKEND's own numbers.
     *
     * The fixture is the phone-QA farm of 2026-08-03: six buckets across three people, five
     * individual and one lump-sum. The per-person totals are the ones the server computed over the
     * whole park filter — Amit 1 shed / 13 animals, Dinakar 3 sheds / 27, Pramod 2 sheds / 10 —
     * and the ViewModel hands them through UNCHANGED. It must never rebuild them by grouping the
     * shed page, which is a ~20-row keyset window: a total taken from it would describe how far the
     * reader scrolled rather than what the person did.
     */
    @Test
    fun `operators surface reports per-person work from backend totals`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(null to phoneQaBuckets()),
            operatorSummariesByPark = mapOf(
                null to listOf(
                    WeighingOperatorSummary(
                        operatorUserId = "user-Amit", operatorDisplayName = "Amit",
                        shedCount = 1, notStarted = 0, capturing = 0, submitted = 1, accepted = 0,
                        rework = 0, animalsWeighed = 13, animalsSubmitted = 13,
                    ),
                    WeighingOperatorSummary(
                        operatorUserId = "user-Dinakar", operatorDisplayName = "Dinakar",
                        shedCount = 3, notStarted = 0, capturing = 3, submitted = 0, accepted = 0,
                        rework = 0, animalsWeighed = 27, animalsSubmitted = 0,
                    ),
                    WeighingOperatorSummary(
                        operatorUserId = "user-Pramod", operatorDisplayName = "Pramod",
                        shedCount = 2, notStarted = 0, capturing = 0, submitted = 2, accepted = 0,
                        rework = 0, animalsWeighed = 10, animalsSubmitted = 10,
                    ),
                ),
            ),
        )
        val vm = weighingViewModel(repository = repository, surface = WEIGHING_SCOPE_OPERATORS)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }

        advanceUntilIdle()

        val people = vm.state.value.operatorSummaries
        assertEquals(listOf("Amit", "Dinakar", "Pramod"), people.map { it.name })
        assertEquals(listOf(1, 3, 2), people.map { it.shedCount })
        assertEquals(listOf(13, 27, 10), people.map { it.animalsWeighed })
        // The SECOND named fact, carried per person exactly as the backend reports it. Dinakar is
        // mid-shift: 27 animals weighed and NOTHING submitted. One number could not say that --
        // it would have read 27 here and 0 on the task detail for the same work at the same second.
        assertEquals(listOf(13, 0, 10), people.map { it.animalsSubmitted })
        // Disjoint and exhaustive: the four state counts add up to the person's sheds, which is
        // what lets the card draw discrete segments instead of an invented fraction.
        assertTrue(
            people.all { it.notStarted + it.capturing + it.submitted + it.accepted == it.shedCount },
        )
    }

    /**
     * Paging the shed list must not move a per-person total.
     *
     * The totals are whole-filter server truth. Appending page two adds shed rows and NOTHING else;
     * a client that recomputed the totals from what it holds would make Dinakar's animal count grow
     * as the reader scrolled.
     */
    @Test
    fun `appending a shed page leaves the per-person totals alone`() = runTest(dispatcher) {
        val summary = listOf(
            WeighingOperatorSummary(
                operatorUserId = "user-Dinakar", operatorDisplayName = "Dinakar",
                shedCount = 3, notStarted = 0, capturing = 0, submitted = 3, accepted = 0,
                rework = 0, animalsWeighed = 27, animalsSubmitted = 27,
            ),
        )
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(null to phoneQaBuckets()),
            operatorSummariesByPark = mapOf(null to summary),
        )
        val vm = weighingViewModel(repository = repository, surface = WEIGHING_SCOPE_OPERATORS)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val before = vm.state.value.operatorSummaries
        vm.onAssignmentRowVisible(vm.state.value.assignments.lastIndex)
        advanceUntilIdle()

        assertEquals(before, vm.state.value.operatorSummaries)
        assertEquals(27, vm.state.value.operatorSummaries.single().animalsWeighed)
        assertEquals(27, vm.state.value.operatorSummaries.single().animalsSubmitted)
    }

    @Test
    fun `read failure hides raw localhost transport detail from weighing screen`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            plannerCatalogResult = AppResult.Err("Failed to connect to localhost/127.0.0.1:8080"),
        )
        val vm = weighingViewModel(repository = repository, surface = WEIGHING_SCOPE_ALL)
        backgroundScope.launch(dispatcher) {
            vm.state.collect {}
        }

        advanceUntilIdle()

        assertEquals(
            "Couldn't load weighing. Check the laptop backend or network, then refresh.",
            vm.state.value.message,
        )
    }

    @Test
    fun `weight validation accepts positive decimals and rejects invalid values`() {
        assertEquals("12.75", sanitizeWeighingWeightInput("12.75"))
        assertEquals(12.75, parsePositiveWeighingWeight("12.75"))
        assertEquals("", sanitizeWeighingWeightInput("-12"))
        assertEquals("", sanitizeWeighingWeightInput("12.7.5"))
        assertNull(parsePositiveWeighingWeight(""))
        assertNull(parsePositiveWeighingWeight("0"))
        assertNull(parsePositiveWeighingWeight("-1"))
        assertNull(parsePositiveWeighingWeight("NaN"))
        assertNull(parsePositiveWeighingWeight("Infinity"))
    }

    @Test
    fun `animal count validation accepts positive integers only`() {
        assertEquals("125", sanitizeWeighingAnimalCountInput("125"))
        assertEquals(125, parsePositiveWeighingAnimalCount("125"))
        assertEquals("", sanitizeWeighingAnimalCountInput("1.5"))
        assertEquals("", sanitizeWeighingAnimalCountInput("-2"))
        assertEquals("", sanitizeWeighingAnimalCountInput("two"))
        assertNull(parsePositiveWeighingAnimalCount(""))
        assertNull(parsePositiveWeighingAnimalCount("0"))
        assertNull(parsePositiveWeighingAnimalCount("-1"))
        assertNull(parsePositiveWeighingAnimalCount("1.5"))
    }

    @Test
    fun `restored accepted weight shows updating only while edit request is active`() = runTest(dispatcher) {
        val original = acceptedDraft(weightKg = 12.0)
        val gate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(listOf(rosterRow()), listOf(original), emptyList(), 0),
            recordIndividualGate = gate,
        )
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val vm = weighingViewModel(repository, scoped = true, analytics = analytics)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.recordIndividual(TEST_TAG, "13.5")
        runCurrent()

        assertTrue(vm.state.value.visibleRows.single().weightUpdating)
        assertEquals(13.5, repository.lastCapture?.weightKg)

        gate.complete(AppResult.Ok(original.copy(weightKg = 13.5, syncedToBackend = false)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.single().weightUpdating)
        val weightEvents = analytics.events.filter {
            it.name == sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_ATTEMPT ||
                it.name == sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_SUCCESS
        }
        assertEquals(
            listOf(
                sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_ATTEMPT,
                sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_WEIGHT_CAPTURE_SUCCESS,
            ),
            weightEvents.map { it.name },
        )
        assertEquals(TEST_TAG, weightEvents.first().props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.RFID])
        assertEquals("13.5", weightEvents.first().props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.WEIGHT_KG])
    }

    @Test
    fun `restored accepted weight clears updating when edit request fails`() = runTest(dispatcher) {
        val original = acceptedDraft(weightKg = 12.0)
        val gate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(listOf(rosterRow()), listOf(original), emptyList(), 0),
            recordIndividualGate = gate,
        )
        val vm = weighingViewModel(repository, scoped = true)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.recordIndividual(TEST_TAG, "13.5")
        runCurrent()
        assertTrue(vm.state.value.visibleRows.single().weightUpdating)

        gate.complete(AppResult.Err("update failed"))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.single().weightUpdating)
        assertEquals("update failed", vm.state.value.message)
    }

    @Test
    fun `free flow saving one animal does not block saving the next animal`() = runTest(dispatcher) {
        val firstGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val secondGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow(), rosterRow(animalId = SECOND_TAG, rowId = "row-2")),
                emptyList(),
                emptyList(),
                0,
            ),
            recordIndividualGates = ArrayDeque(listOf(firstGate, secondGate)),
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, TEST_TAG)
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, SECOND_TAG)
        val vm = weighingViewModel(repository, scoped = true, scanCaptureRepository = scans)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.onAnimalWeightInputChange(TEST_TAG, "12.0")
        vm.onAnimalWeightInputChange(SECOND_TAG, "13.5")
        vm.recordIndividual(TEST_TAG, "12.0")
        runCurrent()

        val firstPendingState = vm.state.value.visibleRows.associateBy { it.animalId }
        assertTrue(firstPendingState.getValue(TEST_TAG).weightUpdating)
        assertTrue(firstPendingState.getValue(SECOND_TAG).canSaveWeight)

        vm.recordIndividual(SECOND_TAG, "13.5")
        runCurrent()

        assertEquals(listOf(TEST_TAG, SECOND_TAG), repository.captures.map { it.scannedIdentifier })
        val bothPendingState = vm.state.value.visibleRows.associateBy { it.animalId }
        assertTrue(bothPendingState.getValue(TEST_TAG).weightUpdating)
        assertTrue(bothPendingState.getValue(SECOND_TAG).weightUpdating)

        firstGate.complete(AppResult.Ok(acceptedDraft(weightKg = 12.0)))
        secondGate.complete(AppResult.Ok(acceptedDraft(animalId = SECOND_TAG, weightKg = 13.5)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.any { it.weightUpdating })
    }

    /**
     * The per-row weight save must bind the weight to the ROW it was typed against, not to whatever
     * tag the shared scan box happens to be holding.
     *
     * recordIndividualRow built its capture with `scannedIdentifier = scanInput.value.ifBlank {
     * row.primaryTag }`. scanInput is a single ViewModel-wide field written by every scan and by
     * the typed-scan box; the per-row save path deliberately runs WITHOUT the global busy gate
     * (`useGlobalBusyGate = false`), so nothing holds that field still while the save is dispatched.
     * The operator scans the next animal, then goes back and saves the weight for the previous row:
     * that row's weight leaves the phone under the OTHER animal's tag.
     *
     * The backend cannot catch this. RecordAnimalObservation clears AnimalID outright and takes
     * scanned_identifier verbatim (service.go:419-426) -- free-flow has no roster to cross-check
     * against, so the client's binding IS the record.
     */
    @Test
    fun `per-row weight save binds to its own row not the shared scan box`() = runTest(dispatcher) {
        val gate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow(), rosterRow(animalId = SECOND_TAG, rowId = "row-2")),
                emptyList(),
                emptyList(),
                0,
            ),
            recordIndividualGate = gate,
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, TEST_TAG)
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, SECOND_TAG)
        val vm = weighingViewModel(repository, scoped = true, scanCaptureRepository = scans)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // The operator has moved on: the scan box now holds the SECOND animal's tag.
        vm.onScanInputChange(SECOND_TAG)
        // ...and now saves the weight typed against the FIRST animal's row.
        vm.onAnimalWeightInputChange(TEST_TAG, "12.0")
        vm.recordIndividual(TEST_TAG, "12.0")
        runCurrent()

        assertEquals(TEST_TAG, repository.lastCapture?.scannedIdentifier)

        gate.complete(AppResult.Ok(acceptedDraft(weightKg = 12.0)))
        advanceUntilIdle()
    }

    @Test
    fun `free flow scanned animals stay newest first after merging restored drafts`() = runTest(dispatcher) {
        val olderDraftTag = "901007000504406"
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(
                    rosterRow(),
                    rosterRow(animalId = SECOND_TAG, rowId = "row-2"),
                    rosterRow(animalId = olderDraftTag, rowId = "row-older"),
                ),
                listOf(acceptedDraft(animalId = olderDraftTag, weightKg = 11.0, capturedAtMs = 500)),
                emptyList(),
                0,
            ),
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, TEST_TAG, capturedAtMs = 1_000)
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, SECOND_TAG, capturedAtMs = 2_000)
        val vm = weighingViewModel(repository, scoped = true, scanCaptureRepository = scans)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(
            listOf(SECOND_TAG, TEST_TAG, olderDraftTag),
            vm.state.value.visibleRows.map { it.animalId },
        )
    }

    @Test
    fun `replacement video proof appears as latest proof on completed row`() = runTest(dispatcher) {
        val replacementProof = ProofCaptureRow(
            id = "proof-replacement",
            fieldKey = "weighing_individual_video",
            proofSubject = ProofSubject.GOAT,
            subjectId = TEST_TAG,
            localUri = "file://replacement.mp4",
            mimeType = "video/mp4",
            caption = TEST_TAG,
            capturedAtMs = 3_600_000,
            capturedStartMs = 3_600_000,
            capturedEndMs = 3_601_000,
            capturedByPrincipalId = null,
            syncStatus = CaptureSyncStatus.SYNCED,
            serverProofId = "server-proof-replacement",
            lastError = null,
        )
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow()),
                listOf(acceptedDraft(weightKg = 12.0, capturedAtMs = 1_000, proofCaptureId = "proof-replacement")),
                emptyList(),
                0,
            ),
        )
        val proofs = FakeProofCaptureRepository().also {
            it.seedProofs(replacementProof)
        }
        val vm = weighingViewModel(repository, scoped = true, proofCaptureRepository = proofs)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.visibleRows.single()
        assertEquals("proof-replacement", row.proofCaptureId)
        assertTrue(row.proofStatusLabel.orEmpty().startsWith("Video synced"))
    }

    @Test
    fun `stale synced individual proof is ignored after backend reopens weighing row`() = runTest(dispatcher) {
        val staleProof = ProofCaptureRow(
            id = "proof-stale",
            fieldKey = "weighing_individual_video",
            proofSubject = ProofSubject.GOAT,
            subjectId = TEST_TAG,
            localUri = "file://stale.mp4",
            mimeType = "video/mp4",
            caption = TEST_TAG,
            capturedAtMs = 3_600_000,
            capturedStartMs = 3_600_000,
            capturedEndMs = 3_601_000,
            capturedByPrincipalId = null,
            syncStatus = CaptureSyncStatus.SYNCED,
            serverProofId = "server-proof-stale",
            lastError = null,
        )
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(listOf(rosterRow()), emptyList(), emptyList(), 0),
        )
        val scans = FakeScanCaptureRepository()
        scans.recordScan(SCOPE_KEY, WEIGHING_SCAN_FIELD_KEY, TEST_TAG, capturedAtMs = 4_000)
        val proofs = FakeProofCaptureRepository().also { it.seedProofs(staleProof) }
        val vm = weighingViewModel(repository, scoped = true, scanCaptureRepository = scans, proofCaptureRepository = proofs)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.visibleRows.single()
        assertNull(row.proofCaptureId)
        assertNull(row.proofStatusLabel)
    }

    @Test
    fun `stale synced shed proofs do not block new group video after backend reopen`() = runTest(dispatcher) {
        val proofs = FakeProofCaptureRepository(maxProofs = 10).also { repo ->
            repeat(5) { index ->
                repo.seedProofs(
                    ProofCaptureRow(
                        id = "old-shed-proof-$index",
                        fieldKey = "weighing_shed_partition_video",
                        proofSubject = ProofSubject.SHED,
                        subjectId = "shed-1",
                        localUri = "file://old-$index.mp4",
                        mimeType = "video/mp4",
                        caption = "old $index",
                        capturedAtMs = index.toLong(),
                        capturedStartMs = index.toLong(),
                        capturedEndMs = index.toLong() + 1,
                        capturedByPrincipalId = null,
                        syncStatus = CaptureSyncStatus.SYNCED,
                        serverProofId = "server-old-$index",
                        lastError = null,
                    ),
                )
            }
        }
        val proofSource = FakeProofCaptureSource()
        proofSource.queue(sg.mesha.goatos.capture.CapturedVideo(localUri = "file://new.mp4", startedAtMs = 10_000, endedAtMs = 12_000))
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(scopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0)),
            scoped = true,
            proofCaptureRepository = proofs,
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
            weighingCategory = "per_shed_partition",
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.captureShedVideo()
        advanceUntilIdle()

        val capture = proofs.captureCalls.single()
        assertEquals("weighing_shed_partition_video", capture.fieldKey)
        assertTrue(capture.caption.orEmpty().contains("Weighing"))
        assertNull("lump-sum shed proof must never carry an RFID", capture.rfidTag)
        assertTrue(analytics.events.any { it.name == sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_PROOF_CAPTURE_ATTEMPT })
    }

    @Test
    fun `stale caption-only proof is not reused after backend reset removes accepted draft`() = runTest(dispatcher) {
        val staleProof = proofRow(
            id = "proof-before-reset",
            syncStatus = CaptureSyncStatus.SYNCED,
            serverProofId = "server-proof-before-reset",
        )
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow()),
                listOf(
                    IndividualWeighingDraft(
                        observationId = "observation-after-reset",
                        scannedIdentifier = TEST_TAG,
                        weightKg = 12.0,
                        capturedAtMs = 3_000,
                        proofCaptureId = null,
                        proofReady = false,
                        readyToSubmit = false,
                        syncedToBackend = false,
                        idempotencyKey = "weighing:individual:after-reset",
                    ),
                ),
                emptyList(),
                1,
            ),
        )
        val proofs = FakeProofCaptureRepository().also { it.seedProofs(staleProof) }
        val vm = weighingViewModel(repository, scoped = true, proofCaptureRepository = proofs)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.visibleRows.single()
        assertNull(row.proofCaptureId)
        assertEquals(ProofUploadStatus.MISSING, row.proofUploadStatus)
    }

    @Test
    fun `pending individual proof survives viewmodel recreation without backend draft`() = runTest(dispatcher) {
        val pendingProof = proofRow(
            id = "proof-pending-restart",
            syncStatus = CaptureSyncStatus.PENDING,
            serverProofId = null,
        )
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                listOf(rosterRow()),
                listOf(
                    IndividualWeighingDraft(
                        observationId = "local-observation",
                        scannedIdentifier = TEST_TAG,
                        weightKg = 12.0,
                        capturedAtMs = 3_000,
                        proofCaptureId = null,
                        proofReady = false,
                        readyToSubmit = false,
                        syncedToBackend = false,
                        idempotencyKey = "weighing:individual:local",
                    ),
                ),
                emptyList(),
                1,
            ),
        )
        val proofs = FakeProofCaptureRepository().also { it.seedProofs(pendingProof) }
        val vm = weighingViewModel(repository, scoped = true, proofCaptureRepository = proofs)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val row = vm.state.value.visibleRows.single()
        assertEquals("proof-pending-restart", row.proofCaptureId)
        assertEquals(ProofUploadStatus.UPLOADING, row.proofUploadStatus)
    }

    @Test
    fun `pending shed proof survives viewmodel recreation without session id`() = runTest(dispatcher) {
        val pendingProof = proofRow(
            id = "shed-proof-pending-restart",
            fieldKey = "weighing_shed_partition_video",
            proofSubject = ProofSubject.SHED,
            subjectId = "shed-1",
            caption = "Weighing lump-sum · Gandhi 1 · video 1",
            syncStatus = CaptureSyncStatus.PENDING,
            serverProofId = null,
        )
        val proofs = FakeProofCaptureRepository(maxProofs = 10).also { it.seedProofs(pendingProof) }
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(scopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0)),
            scoped = true,
            proofCaptureRepository = proofs,
            weighingCategory = "per_shed_partition",
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val proof = vm.state.value.shedProofs.single()
        assertEquals("shed-proof-pending-restart", proof.id)
        assertEquals(ProofUploadStatus.UPLOADING, proof.status)
    }

    @Test
    fun `shed partition category is normalized before field gates run`() = runTest(dispatcher) {
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(),
            scoped = true,
            weighingCategory = " Per Shed Partition ",
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals("per_shed_partition", vm.state.value.category)
        assertTrue(vm.state.value.isShedPartition)
    }

    /**
     * CONFIRMED SHED DEFECT (maintainer, real shed use): "When I'm on camera recording and I use
     * the RFID reader to scan, it suddenly stops recording and comes out." Free-flow weighing had
     * the SAME cancel-on-scan behaviour vaccination had: scanning animal A opened the camera bound
     * to A, and a scan of animal B while A's recording was still in progress cancelled A's
     * unfinished recording and reopened the camera for B, destroying A's in-progress footage. This
     * is the invariant that replaces it: a scan for a different animal while a capture is in
     * flight must NEVER stop or cancel that capture. The new animal is queued instead and its
     * camera opens automatically once the in-flight capture's job completes.
     */
    @Test
    fun `scanning a second animal while the first video is recording must NOT stop or cancel the first camera -- it queues the second animal instead`() = runTest(dispatcher) {
        val proofSource = FakeProofCaptureSource()
        val proofs = FakeProofCaptureRepository()
        val scans = FakeScanCaptureRepository()
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(),
            scoped = true,
            scanCaptureRepository = scans,
            proofCaptureRepository = proofs,
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // Animal A is scanned; its camera opens and is still recording (captureVideo() has not
        // returned -- nothing written yet) when the operator walks to animal B and scans it.
        val gateA = proofSource.queueGate()
        vm.onScanInputChange(TEST_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()
        assertEquals("animal A's camera opened", 1, proofSource.captureCount)
        assertEquals(0, proofs.captureCalls.size)

        vm.onScanInputChange(SECOND_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()

        // THE INVARIANT: animal A's still-recording camera session is untouched -- no second
        // camera opens, nothing is stopped or cancelled. Animal B is queued, and the operator is
        // told so instead of the queued scan silently vanishing.
        assertEquals("a scan mid-recording must never open a second camera or disturb the first", 1, proofSource.captureCount)
        assertEquals(0, proofs.captureCalls.size)
        assertTrue(
            "the operator must be told animal B is queued behind A's still-recording video",
            vm.state.value.message.orEmpty().contains(SECOND_TAG) && vm.state.value.message.orEmpty().contains("queued"),
        )
        assertTrue(
            "a deferred scan must be recorded in analytics, not silently swallowed",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEvents.PROOF_CAPTURE_SCAN_DEFERRED),
        )

        // Animal A's recording finishes normally and saves under A's own tag -- the in-flight
        // capture was never touched by B's scan.
        gateA.complete(CapturedVideo(localUri = "file://animal-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        assertEquals(1, proofs.captureCalls.size)
        assertTrue(proofs.captureCalls[0].caption.orEmpty().contains("Weighing"))
        assertFalse("RFID is rendered by the dedicated rfidTag overlay line, not duplicated in caption", proofs.captureCalls[0].caption.orEmpty().contains(TEST_TAG))
        assertNull("free-flow weighing must not send raw RFID as backend proof subject_id", proofs.captureCalls[0].subjectId)
        assertEquals(TEST_TAG, proofs.captureCalls[0].rfidTag)
        assertEquals("file://animal-a.mp4", proofs.captureCalls[0].localUri)

        // The queued animal B's camera opens automatically the instant A's camera frees up.
        assertEquals("animal B's camera must open automatically once A's capture completed", 2, proofSource.captureCount)
    }

    /**
     * The same defect one layer down, on the shared singleton `DelegatingProofCaptureSource`.
     * [FakeProofCaptureSource]'s per-call gates cannot express it; production shares ONE buffered
     * result channel across every capture request (`VideoCaptureLauncher.kt`/`ProofCaptureRelay`).
     * Because a scan for a different animal no longer cancels the in-flight capture, animal B's
     * camera never opens while A's is still recording, so there is no way for A's
     * eventually-finalized clip to be misdelivered to B's request.
     */
    @Test
    fun `a scan for a different animal never stops the in-flight recording, and its own finalized clip still lands under its own tag`() = runTest(dispatcher) {
        val proofs = FakeProofCaptureRepository()
        val proofSource = ChannelBackedProofCaptureSource()
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(),
            scoped = true,
            scanCaptureRepository = FakeScanCaptureRepository(),
            proofCaptureRepository = proofs,
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.onScanInputChange(TEST_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()
        assertEquals("animal A's camera opened", 1, proofSource.captureCount)

        // THE INVARIANT: this scan must not stop A's camera -- no second request is opened; B is
        // queued instead.
        vm.onScanInputChange(SECOND_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()
        assertEquals("animal B's scan must not open a second camera while A is still recording", 1, proofSource.captureCount)

        // A's recording finalizes for request #1 -- the only request that exists -- and lands
        // under A's own tag.
        proofSource.deliverRecorderResult(
            requestOrdinal = 1,
            video = CapturedVideo(localUri = "file://animal-a.mp4", startedAtMs = 1, endedAtMs = 2),
        )
        advanceUntilIdle()
        assertEquals(1, proofs.captureCalls.size)
        assertTrue(proofs.captureCalls.single().caption.orEmpty().contains("Weighing"))
        assertFalse("RFID is rendered by the dedicated rfidTag overlay line, not duplicated in caption", proofs.captureCalls.single().caption.orEmpty().contains(TEST_TAG))
        assertNull("free-flow weighing must not send raw RFID as backend proof subject_id", proofs.captureCalls.single().subjectId)
        assertEquals(TEST_TAG, proofs.captureCalls.single().rfidTag)
        assertEquals("file://animal-a.mp4", proofs.captureCalls.single().localUri)

        // B's camera now opens automatically (queued animal), and its own recording lands under
        // B's own tag.
        assertEquals("animal B's camera opens once A's capture is done", 2, proofSource.captureCount)
        proofSource.deliverRecorderResult(
            requestOrdinal = 2,
            video = CapturedVideo(localUri = "file://animal-b.mp4", startedAtMs = 3, endedAtMs = 4),
        )
        advanceUntilIdle()
        assertEquals(2, proofs.captureCalls.size)
        assertTrue(proofs.captureCalls[1].caption.orEmpty().contains("Weighing"))
        assertFalse("RFID is rendered by the dedicated rfidTag overlay line, not duplicated in caption", proofs.captureCalls[1].caption.orEmpty().contains(SECOND_TAG))
        assertNull("free-flow weighing must not send raw RFID as backend proof subject_id", proofs.captureCalls[1].subjectId)
        assertEquals(SECOND_TAG, proofs.captureCalls[1].rfidTag)
        assertEquals("file://animal-b.mp4", proofs.captureCalls[1].localUri)
        val scans = analytics.events.filter { it.name == sg.mesha.goatos.core.analytics.AnalyticsEvents.WEIGHING_SCAN }
        assertEquals(listOf(TEST_TAG, SECOND_TAG), scans.map { it.props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.RFID] })
        assertTrue(scans.all { it.props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.OUTCOME] == "accepted" })
    }

    /**
     * The weight is the other half of the same window: the dropped scan also left the selected
     * animal pointing at A, so the next weight the operator typed — standing at B — was recorded
     * against A's tag.
     */
    @Test
    fun `weight typed after scanning a second animal mid-recording is recorded against that second animal`() = runTest(dispatcher) {
        val recordGate = CompletableDeferred<AppResult<IndividualWeighingDraft>>()
        val repository = FakeWeighingRepository(
            recordIndividualGates = ArrayDeque(listOf(recordGate)),
        )
        val proofSource = FakeProofCaptureSource()
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            scanCaptureRepository = FakeScanCaptureRepository(),
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        val gateA = proofSource.queueGate()
        vm.onScanInputChange(TEST_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()

        val gateB = proofSource.queueGate()
        vm.onScanInputChange(SECOND_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()

        gateA.complete(CapturedVideo(localUri = "file://animal-a.mp4", startedAtMs = 1, endedAtMs = 2))
        gateB.complete(CapturedVideo(localUri = "file://animal-b.mp4", startedAtMs = 3, endedAtMs = 4))
        advanceUntilIdle()

        vm.onWeightInputChange("14.5")
        vm.recordIndividual()
        advanceUntilIdle()
        recordGate.complete(AppResult.Ok(acceptedDraft(animalId = SECOND_TAG, weightKg = 14.5)))
        advanceUntilIdle()

        assertEquals(
            "the weight belongs to the animal the operator is standing at, not the one whose scan came first",
            SECOND_TAG,
            repository.lastCapture?.scannedIdentifier,
        )
        assertEquals(SECOND_TAG, repository.lastCapture?.scannedIdentifier)
    }

    @Test
    fun `park chips survive selecting a park and All parks is reachable again`() = runTest(dispatcher) {
        // A22: listAssignments is server-filtered by parkId, so once a park is selected
        // `assignments` collapses to that one park's rows -- deriving the chip list from that same
        // collapsed list left only one chip and no way back to "All parks" or the other park.
        fun assignment(parkId: String, parkName: String, shedId: String) = WeighingAssignment(
            campaignId = "campaign-1",
            tenantId = "tenant-1",
            parkId = parkId,
            parkName = parkName,
            workGroupId = shedId,
            campaignShedId = shedId,
            expectedLocationId = shedId,
            expectedLocationLabel = shedId,
            label = shedId,
            category = "individual_animal",
            operatorUserId = "operator-1",
            status = "in_progress",
            periodLabel = "2026-08-01 - 2026-08-07",
        )
        val parkA = assignment("park-a", "Park A", "shed-a")
        val parkB = assignment("park-b", "Park B", "shed-b")
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(
                null to listOf(parkA, parkB),
                "park-a" to listOf(parkA),
            ),
        )
        val vm = weighingViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // Unfiltered load: both parks are known and both assignments show.
        assertEquals(setOf("park-a", "park-b"), vm.state.value.parkFilters.map { it.parkId }.toSet())
        assertEquals(2, vm.state.value.assignments.size)

        vm.selectAssignmentPark("park-a")
        advanceUntilIdle()

        // Server-filtered: assignments collapse to park A, but the chip list must NOT collapse --
        // Park B (and the implicit "All parks" the screen always injects) must stay reachable.
        assertEquals(listOf("shed-a"), vm.state.value.assignments.map { it.campaignShedId })
        assertEquals(setOf("park-a", "park-b"), vm.state.value.parkFilters.map { it.parkId }.toSet())
        assertTrue(vm.state.value.parkFilters.single { it.parkId == "park-a" }.selected)
        assertFalse(vm.state.value.parkFilters.single { it.parkId == "park-b" }.selected)

        vm.selectAssignmentPark(null)
        advanceUntilIdle()

        // Reachable again: selecting "All parks" (null) restores both assignments.
        assertEquals(2, vm.state.value.assignments.size)
    }

    @Test
    fun `oversight park chips offer a park with no assignment on the loaded page`() = runTest(dispatcher) {
        // The chip row used to be built only from the parks the loaded assignment PAGES happened to
        // carry, so a park whose first row sits on page three had no chip -- and because selecting a
        // park is the only way to fetch that park's rows, its work was unreachable entirely.
        val onlyPagedPark = WeighingAssignment(
            campaignId = "campaign-1",
            tenantId = "tenant-1",
            parkId = "park-cpt",
            parkName = "CPT - Channapatna",
            workGroupId = "shed-a",
            campaignShedId = "shed-a",
            expectedLocationId = "shed-a",
            expectedLocationLabel = "shed-a",
            label = "shed-a",
            category = "individual_animal",
            operatorUserId = "operator-2",
            status = "in_progress",
            periodLabel = "2026-08-01 - 2026-08-07",
        )
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(null to listOf(onlyPagedPark)),
            // A Growth Director: monitors and oversees, never plans. The planner catalog is
            // weighing.plan-gated and can_publish IS that permission, so this viewer cannot read
            // it -- and used to be left with chips derived from whatever rows had loaded.
            assignmentCapabilities = WeighingCapabilities(canEnd = true, canReopen = true),
            parks = listOf(
                WeighingParkRef("park-cpt", "CPT - Channapatna"),
                WeighingParkRef("park-cbe", "CBE - Coimbatore"),
            ),
        )
        val vm = weighingViewModel(repository, surface = "operators")
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(listOf("shed-a"), vm.state.value.assignments.map { it.campaignShedId })
        assertEquals(
            "the backend's own park vocabulary, not the parks that happen to be on the loaded page",
            setOf("park-cpt", "park-cbe"),
            vm.state.value.parkFilters.map { it.parkId }.toSet(),
        )
        assertEquals(0, repository.plannerCatalogRefreshes)
    }

    @Test
    fun `oversight never calls the planner catalog without the planning capability`() = runTest(dispatcher) {
        // /app/weighing/planner/catalog requires weighing.plan. A Growth Director oversees without
        // it, so the old unconditional read was a guaranteed 403 for the very viewer this surface
        // exists for; the paged park fallback is what they keep instead.
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(null to listOf(oversightAssignment())),
            assignmentCapabilities = WeighingCapabilities(canEnd = true, canReopen = true),
        )
        val vm = weighingViewModel(repository, surface = "operators")
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertEquals(0, repository.plannerCatalogRefreshes)
        assertTrue(
            "the paged fallback still names the parks the loaded rows carry",
            vm.state.value.parkFilters.any { it.parkId == "park-cpt" },
        )
    }

    @Test
    fun `oversight reads the park vocabulary WITHOUT the planning capability`() = runTest(dispatcher) {
        // The whole point of GET /app/weighing/parks: the vocabulary read must not be gated on
        // the permission the viewer who needs it does not hold.
        val repository = FakeWeighingRepository(
            assignmentsByPark = mapOf(null to listOf(oversightAssignment())),
            assignmentCapabilities = WeighingCapabilities(canEnd = true, canReopen = true),
            parks = listOf(WeighingParkRef("park-cbe", "CBE - Coimbatore")),
        )
        val vm = weighingViewModel(repository, surface = "operators")
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(repository.parkVocabularyReads > 0)
        assertEquals(0, repository.plannerCatalogRefreshes)
        assertTrue(vm.state.value.parkFilters.any { it.parkId == "park-cbe" })
    }

    @Test
    fun `a deep-linked task is resolved by ONE single-task read, not a page walk`() = runTest(dispatcher) {
        // The list is a keyset page with no id filter. The old bounded walk spent its budget on
        // appends that early-returned while the cold-start refresh was still in flight.
        val deepLinked = WeighingTask(
            campaignId = "campaign-deep",
            tenantId = "tenant",
            parkId = "park-cpt",
            parkName = "CPT - Channapatna",
            weighDate = "2026-08-03",
            status = "in_progress",
            sheds = emptyList(),
        )
        val repository = FakeWeighingRepository(
            taskLookups = mapOf(
                "campaign-deep" to WeighingTaskLookup.Found(
                    task = deepLinked,
                    capabilities = WeighingCapabilities(canEnd = true),
                ),
            ),
        )
        val vm = weighingViewModel(repository, surface = "all")
        backgroundScope.launch(dispatcher) { vm.taskDetailState.collect {} }
        vm.selectTask("campaign-deep")
        advanceUntilIdle()

        assertEquals(listOf("campaign-deep"), repository.taskLookupCalls.toList())
        assertTrue(vm.taskDetailState.value.found)
        // The list never answered for this task, so the single read's capabilities are what stand.
        assertTrue(vm.taskDetailState.value.canEnd)
    }

    @Test
    fun `a deep-linked task the backend refuses is reported not found, and asked for once`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        val vm = weighingViewModel(repository, surface = "all")
        backgroundScope.launch(dispatcher) { vm.taskDetailState.collect {} }
        vm.selectTask("campaign-missing")
        advanceUntilIdle()

        assertEquals(listOf("campaign-missing"), repository.taskLookupCalls.toList())
        assertFalse(vm.taskDetailState.value.found)
    }

    @Test
    fun `scrolling the task list appends pages at a constant window size with no task skipped or repeated at the page boundary`() =
        runTest(dispatcher) {
            // Six pages -- one more than WEIGHING_LEADERSHIP_MAX_WINDOW's five -- proves the fix
            // holds past the old hard ceiling where the observed window used to stop growing and
            // scrolling stopped loading further rows.
            val allTasks = (1..(WEIGHING_LEADERSHIP_PAGE_SIZE * 6)).map { n ->
                WeighingTask(
                    campaignId = "campaign-%03d".format(n),
                    tenantId = "tenant",
                    parkId = "park-cpt",
                    parkName = "CPT - Channapatna",
                    weighDate = "2026-08-03",
                    status = "in_progress",
                    sheds = emptyList(),
                )
            }
            val repository = FakeWeighingRepository(pagedTasks = allTasks)
            val vm = weighingViewModel(repository, surface = "all")
            backgroundScope.launch(dispatcher) { vm.tasksState.collect {} }
            advanceUntilIdle()

            // Scroll to the tail repeatedly. Each trigger should append exactly one more page.
            repeat(5) {
                val visible = vm.tasksState.value.tasks.size
                vm.onTaskRowVisible(visible - 1)
                advanceUntilIdle()
            }

            // The window bounding observeTaskList's read is CONSTANT: it never grows, no matter how
            // many pages have been appended. The old defect grew this value every scroll and hard
            // capped it at WEIGHING_LEADERSHIP_MAX_WINDOW, after which further pages landed in Room
            // but never reached the observed window.
            assertEquals(
                "the observed window must stay a single fixed page size, never grow",
                setOf(WEIGHING_LEADERSHIP_PAGE_SIZE),
                repository.observedTaskListWindowSizes.toSet(),
            )

            // Five appended pages, each exactly one page size, that concatenate with the first page
            // into the FULL ordered keyset -- no task skipped, none repeated at a page boundary.
            assertEquals(5, repository.appendedTaskListPages.size)
            repository.appendedTaskListPages.forEach { page ->
                assertEquals(WEIGHING_LEADERSHIP_PAGE_SIZE, page.size)
            }
            val allIds = allTasks.map { it.campaignId }
            assertEquals(
                "no task may be skipped or repeated across the page boundary",
                allIds.drop(WEIGHING_LEADERSHIP_PAGE_SIZE),
                repository.appendedTaskListPages.flatten(),
            )

            // Every row is now visible in the rendered task list -- the old MAX_WINDOW ceiling that
            // made cached-but-unreachable rows is gone.
            assertEquals(allTasks.size, vm.tasksState.value.tasks.size)
        }

    @Test
    fun `each shed bucket on a task detail names the operator it is assigned to`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(taskListCache = splitTaskCache())
        val vm = weighingViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.taskDetailState.collect {} }
        vm.selectTask("campaign-cbe")
        advanceUntilIdle()

        val rows = vm.taskDetailState.value.sheds
        assertEquals(listOf("Gandhi 1", "Gandhi 2", "Godel 1", "Yashoda 1"), rows.map { it.shedName })
        assertEquals(
            "every bucket must carry the name of the person it was assigned to",
            listOf("Dinakar", "Dinakar", "Pramod", "Pramod"),
            rows.map { it.operatorLabel },
        )
        // A user id is never what the reader sees.
        assertTrue(rows.none { it.operatorLabel.startsWith("user-") })
    }

    // "Dinakar 2" cannot say whether the 2 is sheds or animals, and this screen shows both units:
    // the chip counts shed buckets while every card under it talks about animals captured. The
    // count therefore reaches the screen as a NUMBER so the screen can name the unit.
    @Test
    fun `operator chips carry the shed count as a number the screen can name a unit for`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(taskListCache = splitTaskCache())
        val vm = weighingViewModel(repository)
        backgroundScope.launch(dispatcher) { vm.taskDetailState.collect {} }
        vm.selectTask("campaign-cbe")
        advanceUntilIdle()

        val chips = vm.taskDetailState.value.operatorFilters
        assertEquals(listOf("Dinakar", "Pramod"), chips.map { it.name })
        assertEquals(listOf(2, 2), chips.map { it.shedCount })
        // The bare number must NOT be pre-baked into the name: an unlabelled "Dinakar 2" is exactly
        // the ambiguity this carries a separate field to avoid.
        assertTrue(chips.none { it.name.contains(it.shedCount.toString()) })
    }

    // The maintainer's exact scenario, straight off `weighing_campaign_sheds.operator_user_id` ->
    // `workforce_members.display_name`: four individual buckets in park CBE, 2 for Dinakar and
    // 2 for Pramod, none of them started yet.
    private fun splitTaskCache(): WeighingTaskListCache {
        fun bucket(shed: String, operatorUserId: String, operatorName: String) = WeighingTaskShed(
            campaignShedId = "cs-${shed.lowercase().replace(' ', '-')}",
            locationId = "loc-${shed.lowercase().replace(' ', '-')}",
            displayName = shed,
            category = "individual_animal",
            operatorUserId = operatorUserId,
            operatorDisplayName = operatorName,
            status = "published",
            pendingVerificationCount = 0,
            reworkCount = 0,
            readyToClose = false,
        )
        return WeighingTaskListCache(
            items = listOf(
                WeighingTask(
                    campaignId = "campaign-cbe",
                    tenantId = "tenant-1",
                    parkId = "park-cbe",
                    parkName = "CBE",
                    weighDate = "2026-08-03",
                    status = "published",
                    sheds = listOf(
                        bucket("Gandhi 1", "user-dinakar", "Dinakar"),
                        bucket("Gandhi 2", "user-dinakar", "Dinakar"),
                        bucket("Godel 1", "user-pramod", "Pramod"),
                        bucket("Yashoda 1", "user-pramod", "Pramod"),
                    ),
                ),
            ),
            hasCache = true,
        )
    }

    // The regression this covers: the planner could create and publish a weighing task but had no
    // way to CHANGE one afterward. WeighingViewModel.stageEditOfTask hands the campaign's park,
    // weigh date and shed buckets to the SAME authoring wizard "+ New task" uses, and the wizard's
    // commit() must write back through WeighingRepository.updatePlan(campaignId, ...) -- never
    // repository.createPlan, which would silently fabricate a second task.
    @Test
    fun `editing a task hydrates the wizard from that campaign and saves via updatePlan, not createPlan`() =
        runTest(dispatcher) {
            val sharedSeedStore = WeighingRepeatSeedStore()
            val listRepository = FakeWeighingRepository(taskListCache = splitTaskCache())
            val listVm = weighingViewModel(
                listRepository,
                surface = WEIGHING_SCOPE_ALL,
                repeatSeedStore = sharedSeedStore,
            )
            backgroundScope.launch(dispatcher) { listVm.taskDetailState.collect {} }
            listVm.selectTask("campaign-cbe")
            advanceUntilIdle()

            // The task-detail action hands the seed to the wizard exactly like Repeat does, except
            // the seed now carries the campaign id it was edited FROM.
            val stagedId = listVm.stageEditOfTask("campaign-cbe")
            assertEquals("campaign-cbe", stagedId)

            // The wizard reads the seed back through the SAME store, keyed on the campaign id the
            // route carries -- exactly how AppNavHost wires WEIGHING_TASK_NEW.
            val wizardRepository = FakeWeighingRepository(
                plannerCatalogResult = AppResult.Ok(
                    WeighingPlannerCatalog(
                        parks = listOf(
                            WeighingPlannerPark(
                                parkId = "park-cbe",
                                name = "CBE",
                                kidCount = 0,
                                shedCount = 4,
                                existingCampaign = null,
                            ),
                        ),
                        operators = listOf(
                            WeighingPlannerOperator("user-dinakar", "Dinakar", "DIN"),
                            WeighingPlannerOperator("user-pramod", "Pramod", "PRA"),
                        ),
                    ),
                ),
                plannerParkBuckets = WeighingPlannerParkBucketsCache(
                    parkId = "park-cbe",
                    hasCache = true,
                    sheds = listOf(
                        WeighingPlannerShed(
                            locationId = "loc-gandhi-1",
                            name = "Gandhi 1",
                            kidCount = 0,
                            category = "individual_animal",
                            operatorUserId = "user-dinakar",
                        ),
                        WeighingPlannerShed(
                            locationId = "loc-gandhi-2",
                            name = "Gandhi 2",
                            kidCount = 0,
                            category = "individual_animal",
                            operatorUserId = "user-dinakar",
                        ),
                        WeighingPlannerShed(
                            locationId = "loc-godel-1",
                            name = "Godel 1",
                            kidCount = 0,
                            category = "individual_animal",
                            operatorUserId = "user-pramod",
                        ),
                        WeighingPlannerShed(
                            locationId = "loc-yashoda-1",
                            name = "Yashoda 1",
                            kidCount = 0,
                            category = "individual_animal",
                            operatorUserId = "user-pramod",
                        ),
                    ),
                ),
            )
            val wizardVm = WeighingPlanWizardViewModel(
                repository = wizardRepository,
                repeatSeedStore = sharedSeedStore,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
                savedStateHandle = SavedStateHandle(
                    mapOf(Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
                ),
            )
            // `state` is WhileSubscribed(5_000): reading `.value` with nobody collecting would
            // observe only the pre-init default, never what [WeighingPlanWizardViewModel] actually
            // computed. A real collector is what the composable would be.
            backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
            advanceUntilIdle()
            val hydrated = wizardVm.state.value

            // Pre-hydrated: the edit did not land on the empty DATE step, it opened straight on
            // this campaign's own date, park and all four of its shed buckets.
            assertTrue(hydrated.isEditing)
            assertEquals("2026-08-03", hydrated.selectedDate)
            assertEquals("park-cbe", hydrated.selectedParkId)
            assertEquals(4, hydrated.addedCount)

            // The real regression Finding 1/2 caught: hydrating the right ANSWERS is not the same
            // as landing on the right STEP. An edit must open on BUCKETS -- MAINTAINER DECISION:
            // editing may only add/remove buckets, change an operator, or change a bucket's mode,
            // so DATE and PARK are not just pre-filled, they must never be a step the planner can
            // land on, land back on via Back, or advertise in the stepper.
            assertEquals(WeighingWizardStep.BUCKETS, hydrated.step)
            assertEquals(3, hydrated.stepCount)
            assertEquals(0, hydrated.stepDisplayIndex)
            assertTrue(hydrated.isFirstStep)

            // Back from the wizard's first reachable step must leave the screen (false), exactly
            // like Back from DATE does in create mode -- it must NEVER quietly reveal PARK or DATE.
            assertFalse(wizardVm.back())
            assertEquals(WeighingWizardStep.BUCKETS, wizardVm.state.value.step)

            // selectDate() and selectPark() must be refusals in edit mode, not validated writes --
            // even a superficially legal in-range date, or a real park id from the catalog, must
            // never move an edit off the campaign's own date/park. Real navigation (next()) drives
            // this to CONFIGURE first so the refusal is proven against a step the planner is
            // actually standing on, not just the initial hydrated state.
            wizardVm.next()
            assertEquals(WeighingWizardStep.CONFIGURE, wizardVm.state.value.step)
            wizardVm.selectDate("2099-08-10")
            assertEquals("2026-08-03", wizardVm.state.value.selectedDate)
            assertEquals(WeighingWizardStep.CONFIGURE, wizardVm.state.value.step)
            wizardVm.selectPark("park-cbe")
            assertEquals("park-cbe", wizardVm.state.value.selectedParkId)
            assertEquals(WeighingWizardStep.CONFIGURE, wizardVm.state.value.step)
            // Back off CONFIGURE must land on BUCKETS -- the wizard's real first step in edit mode
            // -- never PARK.
            assertTrue(wizardVm.back())
            assertEquals(WeighingWizardStep.BUCKETS, wizardVm.state.value.step)

            // Point 6: this campaign's OWN sheds must never read back as "already scheduled"
            // against themselves, so every bucket-availability read excludes it.
            assertTrue(
                "expected the wizard to exclude its own campaign id from every bucket read, got: " +
                    wizardRepository.plannerParkBucketExcludeCalls,
                wizardRepository.plannerParkBucketExcludeCalls.isNotEmpty() &&
                    wizardRepository.plannerParkBucketExcludeCalls.all { it == "campaign-cbe" },
            )

            wizardVm.commit(publish = true)
            advanceUntilIdle()

            // The real branch under test: saving from edit mode calls updatePlan against the
            // SAME campaign id, never createPlan -- a second task must never be fabricated.
            assertEquals("campaign-cbe", wizardVm.state.value.savedCampaignId)
            assertNull(wizardRepository.createdDraft)
            val saved = wizardRepository.updatedDraft
            requireNotNull(saved) { "expected commit(edit mode) to call updatePlan" }
            assertEquals("park-cbe", saved.parkId)
            assertEquals("2026-08-03", saved.periodStartDate)
            assertEquals(
                setOf("loc-gandhi-1", "loc-gandhi-2", "loc-godel-1", "loc-yashoda-1"),
                saved.sheds.map { it.locationId }.toSet(),
            )
        }

    // The regression this covers: WeighingRepeatSeedStore is a ONE-SHOT, in-memory map (see its own
    // doc) that does not survive process death, while the route's WEIGHING_REPEAT_OF_ARG does, via
    // SavedStateHandle. A process death between WeighingViewModel.stageEditOfTask and this
    // ViewModel's construction used to leave editCampaignId null with no sign anything was lost --
    // the wizard silently reopened as an ordinary, unrelated CREATE wizard. It must instead detect
    // the mismatch (route arg present, seed missing) and refuse to continue rather than fabricate a
    // task the planner never asked to create.
    @Test
    fun `a lost seed after process death blocks the wizard instead of silently becoming a create`() =
        runTest(dispatcher) {
            // A FRESH store: nothing was ever staged into it, exactly as it looks after process
            // death re-creates every Hilt singleton.
            val freshSeedStore = WeighingRepeatSeedStore()
            val wizardVm = WeighingPlanWizardViewModel(
                repository = FakeWeighingRepository(),
                repeatSeedStore = freshSeedStore,
                analytics = NoopAnalytics(),
                crashReporter = NoopCrashReporter(),
                savedStateHandle = SavedStateHandle(
                    // The route arg SURVIVES process death; the seed it should have paired with does
                    // not.
                    mapOf(Routes.WEIGHING_REPEAT_OF_ARG to "campaign-cbe"),
                ),
            )
            backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
            advanceUntilIdle()
            val state = wizardVm.state.value

            // Explicit recoverable state, not a blank create: a message is shown, and the wizard
            // was NOT quietly wired up as an edit either (there is nothing to edit).
            assertFalse(state.isEditing)
            requireNotNull(state.message) { "expected a recovery message when the seed is lost" }

            // The blank-create downgrade this guards against: even a fully legal date pick must not
            // be able to carry the wizard forward past the loss.
            wizardVm.selectDate("2099-08-10")
            assertFalse(wizardVm.state.value.canContinue)
            wizardVm.next()
            assertEquals(WeighingWizardStep.DATE, wizardVm.state.value.step)

            wizardVm.commit(publish = true)
            advanceUntilIdle()
            assertNull(wizardVm.state.value.savedCampaignId)
        }

    @Test
    fun `bucket search pages beyond the first cached window`() = runTest(dispatcher) {
        val pagedBuckets = (1..121).map { n ->
            WeighingPlannerShed(
                locationId = "loc-filler-$n",
                name = "Filler $n",
                kidCount = 0,
                category = "individual_animal",
                operatorUserId = "operator-amit",
            )
        } + WeighingPlannerShed(
            locationId = "loc-sumathi-1-part-1",
            name = "Sumathi 1 - Part 1",
            kidCount = 0,
            category = "individual_animal",
            operatorUserId = "operator-amit",
        )
        val repository = FakeWeighingRepository(
            plannerCatalogResult = AppResult.Ok(
                WeighingPlannerCatalog(
                    parks = listOf(
                        WeighingPlannerPark(
                            parkId = "park-cbe",
                            name = "CBE",
                            kidCount = 0,
                            shedCount = pagedBuckets.size,
                            existingCampaign = null,
                        ),
                    ),
                    operators = listOf(WeighingPlannerOperator("operator-amit", "Amit Kumar", "AMIT")),
                ),
            ),
            pagedPlannerParkBuckets = pagedBuckets,
        )
        val wizardVm = WeighingPlanWizardViewModel(
            repository = repository,
            repeatSeedStore = WeighingRepeatSeedStore(),
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(),
        )
        backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
        advanceUntilIdle()

        wizardVm.selectDate("2099-08-12")
        wizardVm.next()
        wizardVm.selectPark("park-cbe")
        wizardVm.next()
        advanceUntilIdle()
        assertEquals(WeighingWizardStep.BUCKETS, wizardVm.state.value.step)
        assertTrue(wizardVm.state.value.bucketRows.none { it.name.contains("Sumathi") })

        wizardVm.setBucketQuery("sumathi")
        advanceUntilIdle()

        assertEquals(listOf("Sumathi 1 - Part 1"), wizardVm.state.value.bucketRows.map { it.name })
        assertTrue(
            "search should have appended enough pages to reach the page-7 match",
            repository.appendedPlannerBucketPages.size >= 6,
        )
        assertTrue(
            "the wizard must observe the full selectable shed catalog while search pages",
            repository.observedPlannerBucketWindowSizes.any { it == Int.MAX_VALUE },
        )

        wizardVm.selectPark("park-cbe")
        advanceUntilIdle()

        assertEquals(
            7,
            repository.availabilityRefreshes.lastOrNull()?.third,
        )
    }

    // ---- weighing analytics coverage -------------------------------------------------------

    @Test
    fun `confirming the scope-level submit tracks a submit-succeeded event`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val scans = FakeScanCaptureRepository()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = listOf(acceptedDraft(animalId = TEST_TAG, weightKg = 12.5)),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            scanCaptureRepository = scans,
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        scans.recordScan(
            taskId = "campaign-1:group-1:campaign-shed-1",
            fieldKey = "weighing_free_flow_scan",
            tag = TEST_TAG,
        )
        advanceUntilIdle()

        var submitted = false
        vm.submitIndividualScope { submitted = true }
        advanceUntilIdle()
        assertTrue("the confirm sheet must be showing before it can be confirmed", vm.state.value.showSubmitConfirmation)

        vm.confirmSubmitIndividualScope()
        advanceUntilIdle()

        assertTrue(submitted)
        assertEquals(listOf(TEST_TAG), repository.submitIndividualScopeCalls.single())
        assertTrue(
            "a successful scope submit must be tracked",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_SUBMIT_SUCCEEDED),
        )
        assertTrue(
            "the attempt must be tracked before the outcome",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_SUBMIT_ATTEMPTED),
        )
        assertTrue(
            "the confirmation open must be tracked",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_SUBMIT_CONFIRMATION_OPENED),
        )
        assertTrue(
            "the confirmation accept must be tracked",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_SUBMIT_CONFIRMATION_CONFIRMED),
        )
    }

    // ---- weighing durable-state coverage (process-death simulation) -----------------------

    @Test
    fun `a lump-sum draft survives a simulated process death and reloads into the recreated VM`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        // The SAME SavedStateHandle instance is handed to two separately-constructed VMs -- how
        // a killed-and-recreated process is simulated (see weighingViewModel's doc comment):
        // production would restore this VM's SavedStateHandle from the Bundle the killed one last
        // wrote to, and this instance IS that Bundle-backed map.
        val handle = androidx.lifecycle.SavedStateHandle(
            mapOf(
                "campaignId" to "campaign-1",
                "workGroupId" to "group-1",
                "campaignShedId" to "campaign-shed-1",
                "weighingCategory" to "per_shed_partition",
                "tenantId" to "tenant-1",
                "expectedLocationId" to "shed-1",
                "expectedLocationLabel" to "Shed 1",
            ),
        )
        val firstVm = weighingViewModel(repository = repository, scoped = true, weighingCategory = "per_shed_partition", savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { firstVm.state.collect {} }
        advanceUntilIdle()

        firstVm.onWeightInputChange("42.5")
        firstVm.onAnimalCountInputChange("7")
        advanceUntilIdle()

        // Process death: the ViewModel instance is gone, but the SavedStateHandle's Bundle
        // survives (that is the whole point of SavedStateHandle). A NEW VM is built from it, the
        // way Android reconstructs the destination after the process is killed and relaunched.
        val recreatedVm = weighingViewModel(repository = repository, scoped = true, weighingCategory = "per_shed_partition", savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { recreatedVm.state.collect {} }
        advanceUntilIdle()

        assertEquals(
            "the lump-sum weight the operator had typed must reload after a simulated process death",
            "42.5",
            recreatedVm.state.value.weightInput,
        )
        assertEquals(
            "the lump-sum animal count the operator had typed must reload after a simulated process death",
            "7",
            recreatedVm.state.value.animalCountInput,
        )
    }

    @Test
    fun `typed per-animal weights survive a simulated process death and reload into the recreated VM`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        val scans = FakeScanCaptureRepository()
        val handle = androidx.lifecycle.SavedStateHandle(
            mapOf(
                "campaignId" to "campaign-1",
                "workGroupId" to "group-1",
                "campaignShedId" to "campaign-shed-1",
                "weighingCategory" to "individual_animal",
                "tenantId" to "tenant-1",
                "expectedLocationId" to "shed-1",
                "expectedLocationLabel" to "Shed 1",
            ),
        )
        val firstVm = weighingViewModel(repository = repository, scoped = true, scanCaptureRepository = scans, savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { firstVm.state.collect {} }
        advanceUntilIdle()

        scans.recordScan(taskId = "campaign-1:group-1:campaign-shed-1", fieldKey = "weighing_free_flow_scan", tag = "tag-1")
        scans.recordScan(taskId = "campaign-1:group-1:campaign-shed-1", fieldKey = "weighing_free_flow_scan", tag = "tag-2")
        advanceUntilIdle()

        // Type weights for BOTH animals, but never tap record -- this is the exact gap: typed-
        // but-unsubmitted weights that used to live only in the VM's in-heap animalWeightInputs.
        firstVm.onAnimalWeightInputChange("tag-1", "12.5")
        firstVm.onAnimalWeightInputChange("tag-2", "8.25")
        advanceUntilIdle()

        // Process death: a brand-new VM instance built from the SAME SavedStateHandle Bundle --
        // see the lump-sum draft test above for why this simulates it faithfully.
        val recreatedVm = weighingViewModel(repository = repository, scoped = true, scanCaptureRepository = scans, savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { recreatedVm.state.collect {} }
        advanceUntilIdle()

        val rows = recreatedVm.state.value.visibleRows.associateBy { it.animalId }
        assertEquals(
            "tag-1's typed-but-unsubmitted weight must reload after a simulated process death",
            "12.5",
            rows["tag-1"]?.weightInput,
        )
        assertEquals(
            "tag-2's typed-but-unsubmitted weight must reload after a simulated process death",
            "8.25",
            rows["tag-2"]?.weightInput,
        )
    }

    @Test
    fun `confirming submit after a simulated process death still enqueues the write, never a silent no-op`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = listOf(acceptedDraft(animalId = TEST_TAG, weightKg = 12.5)),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        val scans = FakeScanCaptureRepository()
        val handle = androidx.lifecycle.SavedStateHandle(
            mapOf(
                "campaignId" to "campaign-1",
                "workGroupId" to "group-1",
                "campaignShedId" to "campaign-shed-1",
                "weighingCategory" to "individual_animal",
                "tenantId" to "tenant-1",
                "expectedLocationId" to "shed-1",
                "expectedLocationLabel" to "Shed 1",
            ),
        )
        val firstVm = weighingViewModel(repository = repository, scoped = true, scanCaptureRepository = scans, savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { firstVm.state.collect {} }
        advanceUntilIdle()
        scans.recordScan(
            taskId = "campaign-1:group-1:campaign-shed-1",
            fieldKey = "weighing_free_flow_scan",
            tag = TEST_TAG,
        )
        advanceUntilIdle()

        // Arm the confirmation, then simulate the process dying BEFORE Confirm is tapped -- the
        // exact gap the old nullable `submitPendingIdentifiers`/`submitPendingCallback` var pair
        // could not survive.
        var navigatedOnFirstVm = false
        firstVm.submitIndividualScope { navigatedOnFirstVm = true }
        advanceUntilIdle()
        assertTrue(firstVm.state.value.showSubmitConfirmation)

        // Process death: a brand-new VM instance, same restored SavedStateHandle, same durable
        // Room-backed scope state (the fake mirrors that by construction). The recreated VM has
        // NO memory of firstVm's in-memory navigation callback.
        val recreatedVm = weighingViewModel(repository = repository, scoped = true, scanCaptureRepository = scans, savedStateHandleOverride = handle)
        backgroundScope.launch(dispatcher) { recreatedVm.state.collect {} }
        advanceUntilIdle()

        assertTrue(
            "the confirmation dialog must reopen on the recreated VM, not vanish silently",
            recreatedVm.state.value.showSubmitConfirmation,
        )

        recreatedVm.confirmSubmitIndividualScope()
        advanceUntilIdle()

        assertEquals(
            "the write must still reach the repository even though the original navigation " +
                "callback did not survive the simulated process death -- this is the exact bug " +
                "the old nullable-field handoff produced (a silent no-op)",
            listOf(TEST_TAG),
            repository.submitIndividualScopeCalls.single(),
        )
        assertFalse("the original VM's callback is stale and must never fire", navigatedOnFirstVm)
    }

    @Test
    fun `a scope already submitted in the outbox renders read-only on a fresh re-entry`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = listOf(acceptedDraft(animalId = TEST_TAG, weightKg = 12.5)),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        // The durable, Room-observed signal a fresh re-entry must derive read-only from --
        // this fake's `findPendingSubmit` mirrors WeighingRepository's real one, which resolves
        // the outbox row `submitIndividualScope` itself enqueued (see refreshScopeSubmitted()).
        repository.pendingSubmitResult = AppResult.Ok(
            SyncQueueItem(
                id = "outbox-row-1",
                idempotencyKey = "submit:campaign-1:campaign-shed-1",
                opType = "WEIGHING_SCOPE_SUBMIT",
                groupKey = "campaign-shed-1",
                status = SyncItemStatus.SUCCEEDED,
                attemptCount = 1,
                maxAttempts = 5,
                conflict = false,
                createdAt = 1_000,
                updatedAt = 1_000,
                lastError = null,
            ),
        )

        val vm = weighingViewModel(repository = repository, scoped = true)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertTrue(
            "a fresh VM re-entering a scope whose submit already SUCCEEDED must render read-only " +
                "from the FIRST emission, not only after some later user action",
            vm.state.value.isReadOnly,
        )
    }

    @Test
    fun `a scope's read-only state flips reactively while the screen stays open`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        val syncRepository = FakeWeighingSyncRepository()
        val vm = weighingViewModel(repository = repository, scoped = true, syncRepository = syncRepository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        assertFalse("nothing submitted yet -- must NOT be read-only", vm.state.value.isReadOnly)

        // Mutate the Room-observed outbox state LIVE, exactly as a real drain/reconcile would,
        // while this screen is still on top -- then nudge the same live sync-status stream this
        // VM already collects, the way a real Room Flow re-emits on any underlying row change.
        repository.pendingSubmitResult = AppResult.Ok(
            SyncQueueItem(
                id = "outbox-row-1",
                idempotencyKey = "submit:campaign-1:campaign-shed-1",
                opType = "WEIGHING_SCOPE_SUBMIT",
                groupKey = "campaign-shed-1",
                status = SyncItemStatus.SUCCEEDED,
                attemptCount = 1,
                maxAttempts = 5,
                conflict = false,
                createdAt = 1_000,
                updatedAt = 1_000,
                lastError = null,
            ),
        )
        syncRepository.touchScope(opType = "WEIGHING_SCOPE_SUBMIT", groupKey = "campaign-shed-1")
        advanceUntilIdle()

        assertTrue(
            "the screen must flip to read-only REACTIVELY once the outbox reflects the submit, " +
                "without the operator navigating away and back",
            vm.state.value.isReadOnly,
        )
    }

    @Test
    fun `refreshScopeSubmitted is NOT re-run for outbox activity unrelated to this scope`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository()
        val syncRepository = FakeWeighingSyncRepository()
        val vm = weighingViewModel(repository = repository, scoped = true, syncRepository = syncRepository)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        val callsAfterInit = repository.findPendingSubmitCallCount

        // A conflict on an unrelated animal-observation write in the same shed's outbox must NOT
        // re-trigger the scope-submitted check -- that query only ever answers a WEIGHING_SCOPE_SUBMIT
        // question, so re-running it on every unrelated write is pure waste, not correctness.
        syncRepository.conflict("some-other-idempotency-key")
        advanceUntilIdle()

        assertEquals(
            "an outbox emission with no WEIGHING_SCOPE_SUBMIT row for this shed must not re-query " +
                "findPendingSubmit",
            callsAfterInit,
            repository.findPendingSubmitCallCount,
        )
    }

    @Test
    fun `double-tapping confirm never enqueues the same submit twice`() = runTest(dispatcher) {
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = listOf(acceptedDraft(animalId = TEST_TAG, weightKg = 12.5)),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        val scans = FakeScanCaptureRepository()
        val vm = weighingViewModel(repository = repository, scoped = true, scanCaptureRepository = scans)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        scans.recordScan(
            taskId = "campaign-1:group-1:campaign-shed-1",
            fieldKey = "weighing_free_flow_scan",
            tag = TEST_TAG,
        )
        advanceUntilIdle()

        vm.submitIndividualScope {}
        advanceUntilIdle()
        assertTrue(vm.state.value.showSubmitConfirmation)

        // Two rapid taps on Confirm, exactly like a double-tap on the button before the dialog has
        // a chance to close. The FIRST call must close the gate synchronously (before suspending),
        // so the SECOND call reads it already shut and returns without enqueuing again.
        vm.confirmSubmitIndividualScope()
        vm.confirmSubmitIndividualScope()
        advanceUntilIdle()

        assertEquals(
            "a double-tap on Confirm must enqueue exactly one submit, never two",
            1,
            repository.submitIndividualScopeCalls.size,
        )
    }

    @Test
    fun `visible weight and synced video pair can arm submit even when draft readiness lags`() = runTest(dispatcher) {
        val scans = FakeScanCaptureRepository()
        val staleDraft = acceptedDraft(animalId = TEST_TAG, weightKg = 22.8).copy(
            readyToSubmit = false,
            syncedToBackend = false,
        )
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = listOf(rosterRow()),
                individualDrafts = listOf(staleDraft),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        val proofs = FakeProofCaptureRepository().also {
            it.seedProofs(
                proofRow(
                    id = "proof-$TEST_TAG",
                    syncStatus = CaptureSyncStatus.SYNCED,
                    serverProofId = "server-proof-$TEST_TAG",
                ),
            )
        }
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            scanCaptureRepository = scans,
            proofCaptureRepository = proofs,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        scans.recordScan(
            taskId = "campaign-1:group-1:campaign-shed-1",
            fieldKey = "weighing_free_flow_scan",
            tag = TEST_TAG,
        )
        advanceUntilIdle()

        assertTrue(vm.state.value.individualSubmitReady)

        var submitted = false
        vm.submitIndividualScope { submitted = true }
        advanceUntilIdle()

        assertTrue("the visible pair should arm the confirmation dialog", vm.state.value.showSubmitConfirmation)
        vm.confirmSubmitIndividualScope()
        advanceUntilIdle()

        assertTrue(submitted)
        assertEquals(listOf(TEST_TAG), repository.submitIndividualScopeCalls.single())
    }

    @Test
    fun `a rejected scope-level submit tracks a submit-failed event carrying the real reason`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val scans = FakeScanCaptureRepository()
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = listOf(acceptedDraft(animalId = TEST_TAG, weightKg = 12.5)),
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        repository.submitIndividualScopeResult = AppResult.Err("verification_pending")
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            scanCaptureRepository = scans,
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()
        scans.recordScan(
            taskId = "campaign-1:group-1:campaign-shed-1",
            fieldKey = "weighing_free_flow_scan",
            tag = TEST_TAG,
        )
        advanceUntilIdle()

        vm.submitIndividualScope {}
        advanceUntilIdle()
        vm.confirmSubmitIndividualScope()
        advanceUntilIdle()

        val failure = analytics.events.single {
            it.name == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_SUBMIT_FAILED
        }
        assertEquals(
            "the failure event must carry the repository's REAL message, never a fabricated code",
            "verification_pending",
            failure.props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.REASON],
        )
    }

    @Test
    fun `the client-side submit gate tracks submit_blocked when the scope is incomplete`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(),
            scoped = true,
            analytics = analytics,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // Nothing scanned yet, so the gate must refuse before anything is enqueued.
        vm.submitIndividualScope {}
        advanceUntilIdle()

        assertFalse(vm.state.value.showSubmitConfirmation)
        val blocked = analytics.events.single { it.name == sg.mesha.goatos.core.analytics.AnalyticsEvents.SUBMIT_BLOCKED }
        assertEquals("scope_incomplete", blocked.props[sg.mesha.goatos.core.analytics.AnalyticsEvents.Params.REASON])
    }

    @Test
    fun `shed-partition double-tap recordShedPartition enqueues exactly once`() = runTest(dispatcher) {
        // Guards against double-tap on WeighingViewModel.PER_SHED_PARTITION_CATEGORY's real submit
        // entry point, recordShedPartition() -- the lump-sum path, not the RFID-scoped
        // submitIndividualScope()/confirmSubmitIndividualScope() flow that category short-circuits.
        val recordGate = CompletableDeferred<AppResult<ShedWeighingDraft>>()
        val repository = FakeWeighingRepository(
            recordShedPartitionGate = recordGate,
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = emptyList(),
                // A non-empty shedDrafts list is what activeWeighingProofs() reads as "this scope
                // has an OPEN round" -- without it, a SYNCED shed proof is filtered out of
                // observedProofs entirely (a reopen-superseded clip guard), and recordShedPartition
                // never sees a syncedProof to submit at all.
                shedDrafts = listOf(
                    ShedWeighingDraft(
                        shedObservationId = "open-round-1",
                        resultJson = "{}",
                        proofReady = false,
                        readyToSubmit = false,
                        idempotencyKey = "weighing:shed:open-round-1",
                    ),
                ),
                totalExpected = 0,
            ),
        )
        // A SYNCED shed-partition video for THIS scope's expected location is the gate
        // recordShedPartition checks before it will submit at all (see
        // WeighingViewModel.recordShedPartition / activeWeighingProofs).
        val proofs = FakeProofCaptureRepository(maxProofs = 10).also {
            it.seedProofs(
                proofRow(
                    id = "shed-proof-synced",
                    fieldKey = "weighing_shed_partition_video",
                    proofSubject = ProofSubject.SHED,
                    subjectId = "shed-1",
                    caption = "Weighing lump-sum · Gandhi 1 · video 1",
                    syncStatus = CaptureSyncStatus.SYNCED,
                    serverProofId = "server-shed-proof-synced",
                ),
            )
        }
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            proofCaptureRepository = proofs,
            weighingCategory = WeighingViewModel.PER_SHED_PARTITION_CATEGORY,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.onWeightInputChange("120")
        vm.onAnimalCountInputChange("10")
        advanceUntilIdle()

        // Double-tap: the FIRST call's coroutine is held suspended on recordGate, so the
        // actionInFlight flag it set synchronously before launching is still true when the second
        // tap runs -- proving the guard, not racing it (an ungated fake completes the first call's
        // whole coroutine, including its `finally` reset, before the second synchronous call under
        // UnconfinedTestDispatcher, which would falsely pass a broken guard).
        vm.recordShedPartition {}
        vm.recordShedPartition {}
        assertEquals(
            "the second tap must be rejected while the first submit is still in flight",
            1,
            repository.recordShedPartitionCalls.size,
        )

        recordGate.complete(AppResult.Ok(ShedWeighingDraft("shed-obs-1", "{}", true, true, "weighing:shed:1")))
        advanceUntilIdle()

        assertEquals(1, repository.recordShedPartitionCalls.size)
    }

    @Test
    fun `reinstall after durable-draft fresh VM with no drafts rejects submit on already-submitted scope`() = runTest(dispatcher) {
        // Simulates: app killed after submit confirmation enqueued, relaunched fresh
        // The scope was submitted, but the VM is reconstructed empty (SavedStateHandle fresh, no local cache)
        val repository = FakeWeighingRepository(
            scopeState = WeighingScopeState(
                rosterWindow = emptyList(),
                individualDrafts = emptyList(), // Empty: fresh startup before cache loads
                shedDrafts = emptyList(),
                totalExpected = 1,
            ),
        )
        // The durable, Room-observed signal a fresh re-entry must derive read-only from -- the
        // scope is already submitted (server state), so the outbox row Room persisted resolves
        // SUCCEEDED even though the fresh VM has no local drafts to reconstruct from.
        repository.pendingSubmitResult = AppResult.Ok(
            SyncQueueItem(
                id = "outbox-row-2",
                idempotencyKey = "submit:campaign-1:campaign-shed-1",
                opType = "WEIGHING_SCOPE_SUBMIT",
                groupKey = "campaign-shed-1",
                status = SyncItemStatus.SUCCEEDED,
                attemptCount = 1,
                maxAttempts = 5,
                conflict = false,
                createdAt = 1_000,
                updatedAt = 1_000,
                lastError = null,
            ),
        )
        val freshHandle = androidx.lifecycle.SavedStateHandle(
            mapOf(
                "campaignId" to "campaign-1",
                "workGroupId" to "group-1",
                "campaignShedId" to "campaign-shed-1",
            ),
        )
        val vm = weighingViewModel(
            repository = repository,
            scoped = true,
            savedStateHandleOverride = freshHandle,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // The fresh VM should recognize the scope is already submitted and block capture
        assertFalse("fresh VM with submitted scope must not allow submit", vm.state.value.individualSubmitReady)
        assertTrue("fresh VM with submitted scope must render read-only", vm.state.value.isReadOnly)
        // No crash, just read-only state
        assertNotNull("view model should be stable without crashing", vm.state.value)
    }

    @Test
    fun `the plan wizard tracks a step-reached event each time the step actually changes`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val wizardVm = WeighingPlanWizardViewModel(
            repository = FakeWeighingRepository(),
            repeatSeedStore = WeighingRepeatSeedStore(),
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            savedStateHandle = SavedStateHandle(emptyMap()),
        )
        backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
        advanceUntilIdle()

        // The opening step is tracked on init, before any tap.
        assertEquals(listOf("date"), analytics.events.map { it.props["wizard_step"] }.filterNotNull())

        wizardVm.selectDate("2099-08-10")
        wizardVm.next()
        advanceUntilIdle()
        assertEquals(WeighingWizardStep.PARK, wizardVm.state.value.step)

        wizardVm.selectPark("park-1")
        wizardVm.next()
        advanceUntilIdle()
        assertEquals(WeighingWizardStep.BUCKETS, wizardVm.state.value.step)

        wizardVm.back()
        advanceUntilIdle()
        assertEquals(WeighingWizardStep.PARK, wizardVm.state.value.step)

        val stepsTracked = analytics.events
            .filter { it.name == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_STEP_REACHED }
            .map { it.props["wizard_step"] }
        // date (init) -> park (next) -> buckets (next) -> park (back). Re-entering the SAME step
        // is never re-tracked, but leaving and coming back to one already visited is a real step
        // transition and must be.
        assertEquals(listOf("date", "park", "buckets", "park"), stepsTracked)
    }

    @Test
    fun `leaving the wizard mid-flow without saving tracks an abandonment event, but saving does not`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val store = androidx.lifecycle.ViewModelStore()
        val factory = object : androidx.lifecycle.ViewModelProvider.Factory {
            override fun <T : androidx.lifecycle.ViewModel> create(modelClass: Class<T>): T {
                @Suppress("UNCHECKED_CAST")
                return WeighingPlanWizardViewModel(
                    repository = FakeWeighingRepository(),
                    repeatSeedStore = WeighingRepeatSeedStore(),
                    analytics = analytics,
                    crashReporter = NoopCrashReporter(),
                    savedStateHandle = SavedStateHandle(emptyMap()),
                ) as T
            }
        }
        val wizardVm = androidx.lifecycle.ViewModelProvider(store, factory)[WeighingPlanWizardViewModel::class.java]
        backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
        advanceUntilIdle()

        wizardVm.selectDate("2099-08-10")
        wizardVm.next()
        advanceUntilIdle()
        assertEquals(WeighingWizardStep.PARK, wizardVm.state.value.step)

        // The planner backs out here -- real progress (moved off DATE), never saved.
        store.clear()

        val abandoned = analytics.events.single {
            it.name == sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_ABANDONED
        }
        assertEquals("park", abandoned.props["wizard_step"])
    }

    @Test
    fun `a wizard that never leaves its opening step reports no abandonment`() = runTest(dispatcher) {
        val analytics = sg.mesha.goatos.boot.RecordingAnalytics()
        val store = androidx.lifecycle.ViewModelStore()
        val factory = object : androidx.lifecycle.ViewModelProvider.Factory {
            override fun <T : androidx.lifecycle.ViewModel> create(modelClass: Class<T>): T {
                @Suppress("UNCHECKED_CAST")
                return WeighingPlanWizardViewModel(
                    repository = FakeWeighingRepository(),
                    repeatSeedStore = WeighingRepeatSeedStore(),
                    analytics = analytics,
                    crashReporter = NoopCrashReporter(),
                    savedStateHandle = SavedStateHandle(emptyMap()),
                ) as T
            }
        }
        val wizardVm = androidx.lifecycle.ViewModelProvider(store, factory)[WeighingPlanWizardViewModel::class.java]
        backgroundScope.launch(dispatcher) { wizardVm.state.collect {} }
        advanceUntilIdle()

        store.clear()

        assertFalse(
            "nothing was started, so nothing was abandoned",
            analytics.names().contains(sg.mesha.goatos.core.analytics.AnalyticsEventsWeighing.WEIGHING_PLAN_WIZARD_ABANDONED),
        )
    }

    // --- Conflicted-write classification ------------------------------------------------
    //
    // When the server REFUSES a weight write with a conflict, the phone has to decide one thing:
    // does the operator have to walk back to that animal and do it again? Getting it wrong is
    // costly in both directions -- a false "record it again" sends someone to repeat work that is
    // already saved, and a swallowed rejection leaves an animal silently unrecorded behind a
    // success message. Every one of these tests drives the real collector: a conflicted row is
    // pushed into the write queue the ViewModel is observing, and the assertion is on what the
    // operator's screen then says.

    /** The draft the phone holds for [TEST_TAG], with the verifier's verdict on it. */
    private fun conflictDraft(verificationStatus: String?) =
        acceptedDraft(weightKg = 21.5).copy(
            idempotencyKey = CONFLICT_KEY,
            verificationStatus = verificationStatus,
        )

    private fun conflictScope(verificationStatus: String?) = WeighingScopeState(
        rosterWindow = listOf(rosterRow()),
        individualDrafts = listOf(conflictDraft(verificationStatus)),
        shedDrafts = emptyList(),
        totalExpected = 0,
    )

    @Test
    fun `a refused write whose refreshed record says the animal was sent back tells the operator to record it again`() =
        runTest(dispatcher) {
            val repository = FakeWeighingRepository(
                scopeState = conflictScope(verificationStatus = null),
                postRefreshScopeState = conflictScope(verificationStatus = "rework"),
            )
            val sync = FakeWeighingSyncRepository()
            val vm = weighingViewModel(repository, scoped = true, syncRepository = sync)
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()

            val refreshesBefore = repository.refreshScopeCalls
            repository.postRefreshArmed = true
            sync.conflict(CONFLICT_KEY)
            advanceUntilIdle()

            assertEquals("This animal was sent back. Record it again.", vm.state.value.message)
            assertTrue(vm.state.value.visibleRows.single().weightSyncConflict)
        }

    /**
     * The other conflict, and the reason the refetch exists at all: the weight is ALREADY STORED
     * server-side and the phone simply re-posted it. Nothing was lost, so the operator must not be
     * sent back -- and the row must not light up as a problem.
     */
    @Test
    fun `a refused write whose refreshed record is still standing leaves the animal alone`() =
        runTest(dispatcher) {
            val repository = FakeWeighingRepository(
                scopeState = conflictScope(verificationStatus = null),
                postRefreshScopeState = conflictScope(verificationStatus = "pending"),
            )
            val sync = FakeWeighingSyncRepository()
            val vm = weighingViewModel(repository, scoped = true, syncRepository = sync)
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()

            val refreshesBefore = repository.refreshScopeCalls
            repository.postRefreshArmed = true
            sync.conflict(CONFLICT_KEY)
            advanceUntilIdle()

            assertNull(vm.state.value.message)
            assertFalse(vm.state.value.visibleRows.single().weightSyncConflict)
        }

    /**
     * The refetch FAILED, so the phone knows nothing new. The local draft still reads "pending",
     * which is exactly the stale answer that would swallow a real rejection -- so a failed refresh
     * must NOT be treated as "the record is fine". An unnecessary re-record is recoverable; an
     * animal silently left unrecorded is not.
     */
    @Test
    fun `a refused write whose refetch fails flags the animal rather than trusting the stale record`() =
        runTest(dispatcher) {
            val repository = FakeWeighingRepository(
                // The phone's own copy says the animal is fine. It is the ONLY thing a failed
                // refresh leaves behind, and it must not be believed.
                scopeState = conflictScope(verificationStatus = "pending"),
                refreshScopeResult = AppResult.Err("Weighing roster sync is not configured."),
                postRefreshScopeState = conflictScope(verificationStatus = "pending"),
            )
            val sync = FakeWeighingSyncRepository()
            val vm = weighingViewModel(repository, scoped = true, syncRepository = sync)
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()

            val refreshesBefore = repository.refreshScopeCalls
            repository.postRefreshArmed = true
            sync.conflict(CONFLICT_KEY)
            advanceUntilIdle()

            assertEquals(refreshesBefore + 1, repository.refreshScopeCalls)
            assertEquals("This animal was sent back. Record it again.", vm.state.value.message)
            assertTrue(vm.state.value.visibleRows.single().weightSyncConflict)
        }

    /**
     * The race four earlier fixes failed to close. BEFORE the refetch the phone's draft says
     * "pending"; the verifier's send-back only arrives WITH the refetch. Classifying against the
     * pre-refresh copy reads "already stored", says nothing, and the operator never learns the
     * animal is owed. The fake deliberately answers differently before and after the refetch so a
     * classification reading the wrong one cannot pass.
     */
    @Test
    fun `the verdict is read from the refreshed record, not the copy the phone held before it`() =
        runTest(dispatcher) {
            val repository = FakeWeighingRepository(
                scopeState = conflictScope(verificationStatus = "pending"),
                postRefreshScopeState = conflictScope(verificationStatus = "rework"),
            )
            val sync = FakeWeighingSyncRepository()
            val vm = weighingViewModel(repository, scoped = true, syncRepository = sync)
            backgroundScope.launch(dispatcher) { vm.state.collect {} }
            advanceUntilIdle()

            // Proof the fixture really is a before/after pair: the pre-refresh answer is the
            // benign one, so a stale read would classify this as "nothing to do".
            assertEquals("pending", repository.individualDraftsSnapshot(CONFLICT_SCOPE_KEY).single().verificationStatus)

            val refreshesBefore = repository.refreshScopeCalls
            repository.postRefreshArmed = true
            sync.conflict(CONFLICT_KEY)
            advanceUntilIdle()

            assertEquals(refreshesBefore + 1, repository.refreshScopeCalls)
            assertEquals("rework", repository.individualDraftsSnapshot(CONFLICT_SCOPE_KEY).single().verificationStatus)
            assertEquals("This animal was sent back. Record it again.", vm.state.value.message)
            assertTrue(vm.state.value.visibleRows.single().weightSyncConflict)
        }

    private fun weighingViewModel(
        repository: FakeWeighingRepository,
        scoped: Boolean = false,
        // Which weighing SURFACE this ViewModel is standing in for. Planning belongs to the planner
        // surface, so a test exercising the planner has to say so -- the route declares it in the app.
        surface: String? = null,
        scanCaptureRepository: FakeScanCaptureRepository = FakeScanCaptureRepository(),
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        proofCaptureSource: ProofCaptureSource = FakeProofCaptureSource(),
        bootstrapRepository: BootstrapRepository = LeadershipBootstrapRepository,
        weighingCategory: String = "individual_animal",
        analytics: AnalyticsPort = NoopAnalytics(),
        // Shared with a WeighingPlanWizardViewModel in a test that exercises the repeat/edit
        // handoff -- the two ViewModels only agree on a seed if they hold the SAME store instance.
        repeatSeedStore: WeighingRepeatSeedStore = WeighingRepeatSeedStore(),
        // The write queue. Left null for every test that does not care, exactly as production
        // leaves it null in a unit test -- but the conflict watcher only STARTS when one is
        // supplied, so a test of that watcher has to hand one in.
        syncRepository: SyncRepository? = null,
        // Reuses a caller-held SavedStateHandle instead of minting a fresh one -- how a process-
        // death recreation is simulated: production Android would hand the RECREATED VM a
        // SavedStateHandle restored from the SAME Bundle the killed one last wrote to, and a
        // SavedStateHandle instance IS that restored map (it is what `savedStateHandle[key] = v`
        // writes into and `savedStateHandle[key]` reads back). Constructing a second VM against
        // the SAME instance exercises exactly that path without needing a real Activity/Bundle.
        savedStateHandleOverride: SavedStateHandle? = null,
    ): WeighingViewModel =
        WeighingViewModel(
            repository = repository,
            bootstrapRepository = bootstrapRepository,
            reader = FakeRfidReaderPort(),
            syncRepository = syncRepository,
            scanCaptureRepository = scanCaptureRepository,
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
            analytics = analytics,
            crashReporter = NoopCrashReporter(),
            repeatSeedStore = repeatSeedStore,
            exportFileWriter = FakeWeighingExportFileWriter(),
            savedStateHandle = savedStateHandleOverride ?: SavedStateHandle(
                if (scoped) {
                    mapOf(
                        "campaignId" to "campaign-1",
                        "workGroupId" to "group-1",
                        "campaignShedId" to "campaign-shed-1",
                        "weighingCategory" to weighingCategory,
                        "tenantId" to "tenant-1",
                        "expectedLocationId" to "shed-1",
                        "expectedLocationLabel" to "Shed 1",
                    )
                } else {
                    surface?.let { mapOf("weighingSurface" to it) } ?: emptyMap()
                },
            ),
        )

    private fun rosterRow(
        animalId: String = TEST_TAG,
        rowId: String = "row-1",
    ) = WeighingRosterRowEntity(
        id = rowId,
        scopeKey = "campaign-1:group-1:campaign-shed-1",
        tenantId = "tenant-1",
        campaignId = "campaign-1",
        workGroupId = "group-1",
        campaignShedId = "campaign-shed-1",
        expectedLocationId = "shed-1",
        expectedLocationLabel = "Shed 1",
        actualLocationId = null,
        actualLocationLabel = null,
        animalId = animalId,
        displayAnimalId = animalId,
        primaryTag = animalId,
        secondaryTag = null,
        normalizedPrimaryTag = animalId,
        normalizedSecondaryTag = null,
        status = "pending",
        availabilityStatus = null,
        seq = 1,
        updatedAt = 1_000,
    )

    private fun acceptedDraft(
        animalId: String = TEST_TAG,
        weightKg: Double,
        capturedAtMs: Long = 1_000,
        proofCaptureId: String = "proof-$animalId",
    ) = IndividualWeighingDraft(
        observationId = "observation-$animalId",
        scannedIdentifier = animalId,
        weightKg = weightKg,
        capturedAtMs = capturedAtMs,
        proofCaptureId = proofCaptureId,
        proofReady = true,
        readyToSubmit = true,
        syncedToBackend = true,
        idempotencyKey = "server:observation-1",
        serverProofId = proofCaptureId,
    )

    private fun proofRow(
        id: String,
        fieldKey: String = "weighing_individual_video",
        proofSubject: ProofSubject = ProofSubject.GOAT,
        subjectId: String? = TEST_TAG,
        caption: String? = TEST_TAG,
        syncStatus: CaptureSyncStatus,
        serverProofId: String?,
    ) = ProofCaptureRow(
        id = id,
        fieldKey = fieldKey,
        proofSubject = proofSubject,
        subjectId = subjectId,
        localUri = "file://$id.mp4",
        mimeType = "video/mp4",
        caption = caption,
        capturedAtMs = 1_000,
        capturedStartMs = 1_000,
        capturedEndMs = 2_000,
        capturedByPrincipalId = null,
        syncStatus = syncStatus,
        serverProofId = serverProofId,
        lastError = null,
    )

    private object LeadershipBootstrapRepository : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState.Empty
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? = null
    }

    private object OperatorBootstrapRepository : BootstrapRepository {
        override suspend fun loadNavState(): NavState = NavState.Empty
        override suspend fun operatorProfile(): BootstrapOperatorProfileDto? =
            BootstrapOperatorProfileDto(operatorId = "operator-1", displayName = "Operator 1", primaryRoleHint = "operator")
    }

    /** Records writes instead of touching disk/FileProvider, mirroring [FakeWeighingRepository]'s style. */
    private class FakeWeighingExportFileWriter : sg.mesha.goatos.export.WeighingExportFileWriter {
        val writes = mutableListOf<Pair<ByteArray, String>>()
        override fun write(bytes: ByteArray, fileName: String): android.net.Uri {
            writes += bytes to fileName
            return android.net.Uri.EMPTY
        }
    }

    private class FakeRfidReaderPort : RfidReaderPort {
        private val readsFlow = MutableSharedFlow<RfidRead>()
        override val status: StateFlow<RfidReaderStatus> = MutableStateFlow(RfidReaderStatus.READY)
        override val reads: SharedFlow<RfidRead> = readsFlow
        override val readerName: StateFlow<String?> = MutableStateFlow("Test reader")
        override val devices: StateFlow<List<RfidReaderDevice>> = MutableStateFlow(emptyList())
        override fun refreshStatus() {}
        override fun openSystemPairing() {}
        override fun setCaptureEnabled(enabled: Boolean) {}
        override fun setCompletionKeySwallowEnabled(enabled: Boolean) {}
        override fun onKeyEvent(event: KeyEvent): Boolean = false
    }

    /**
     * The write queue, as far as the weighing screen is concerned: a status stream it can push a
     * refused write into. Everything else is unused here and says so.
     */
    private class FakeWeighingSyncRepository : SyncRepository {
        private val status = MutableStateFlow(SyncStatus.empty(online = true))

        /** Reports that the weight write carrying [idempotencyKey] was REFUSED by the server. */
        fun conflict(idempotencyKey: String) {
            val now = 1_000L
            status.value = SyncStatus(
                online = true,
                pendingCount = 0,
                inFlightCount = 0,
                failedCount = 1,
                deadLetterCount = 0,
                lastSyncAt = now,
                items = listOf(
                    SyncQueueItem(
                        // A row uuid and a shed-scoped group key, exactly as production carries
                        // them -- neither can name the animal, so a match on either would find
                        // nothing. The idempotency key is the only join.
                        id = "outbox-row-uuid",
                        idempotencyKey = idempotencyKey,
                        opType = "WEIGHING_ANIMAL_OBSERVATION",
                        groupKey = "campaign-shed-1",
                        status = SyncItemStatus.FAILED,
                        attemptCount = 1,
                        maxAttempts = 5,
                        conflict = true,
                        createdAt = now,
                        updatedAt = now,
                        lastError = "Already recorded.",
                    ),
                ),
            )
        }

        /**
         * Forces a new emission on [observeStatus] without changing its meaning -- how a test
         * simulates "the outbox/Room state changed while this screen is already open" so the
         * live re-check WeighingViewModel already runs on every sync-status tick (see
         * refreshScopeSubmitted() being called inside its syncStatuses.collect) actually fires,
         * the same way a real Room Flow re-emits on any underlying row change.
         */
        fun touch() {
            status.value = status.value.copy(lastSyncAt = (status.value.lastSyncAt ?: 0L) + 1)
        }

        /** Same as [touch] but also carries a [SyncQueueItem] for the given (opType, groupKey) --
         *  WeighingViewModel now only re-derives scopeSubmitted when the live sync-status snapshot
         *  actually contains a row for THIS scope (see the `refreshScopeSubmitted()` scoping fix),
         *  so a test simulating a real "this scope's outbox row changed" tick must carry one. */
        fun touchScope(opType: String, groupKey: String) {
            val now = (status.value.lastSyncAt ?: 0L) + 1
            status.value = status.value.copy(
                lastSyncAt = now,
                items = status.value.items + SyncQueueItem(
                    id = "touch-$now",
                    idempotencyKey = "touch-$now",
                    opType = opType,
                    groupKey = groupKey,
                    status = SyncItemStatus.SUCCEEDED,
                    attemptCount = 1,
                    maxAttempts = 5,
                    conflict = false,
                    createdAt = now,
                    updatedAt = now,
                    lastError = null,
                ),
            )
        }

        override fun observeStatus(): StateFlow<SyncStatus> = status
        override fun observeItem(itemId: String): Flow<SyncQueueItem?> = flowOf(null)
        override suspend fun enqueueShedSubmit(
            taskId: String,
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto,
        ): AppResult<String> = error("unused")
        override suspend fun enqueueReschedule(
            obligationId: String,
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto,
        ): AppResult<String> = error("unused")
        override suspend fun enqueueProofUpload(
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.ProofUploadRequestDto,
            localFilePath: String,
            durationMs: Long?,
        ): AppResult<String> = error("unused")
        override suspend fun enqueueVerifyTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
        override suspend fun enqueueReworkTask(taskId: String, reason: String, rowVersion: Int): AppResult<String> = error("unused")
        override suspend fun enqueueVerificationVerdict(
            itemId: String,
            decision: String,
            reason: String?,
            rowVersion: Int,
            measurement: VerificationVerdictMeasurementDto?,
        ): AppResult<String> = error("unused")
        override suspend fun enqueueCountsShifting(
            groupKey: String,
            idempotencyKey: String,
            request: sg.mesha.goatos.core.network.dto.CountsShiftingEventRequestDto,
        ): AppResult<String> = error("unused")
        override suspend fun retry(itemId: String): AppResult<Unit> = error("unused")
        override suspend fun deleteOutboxItem(itemId: String): AppResult<Unit> = AppResult.Ok(Unit)
        override suspend fun triggerDrain() = Unit
    }

    private class FakeWeighingRepository(
        private val plannerCatalogResult: AppResult<WeighingPlannerCatalog>? = null,
        scopeState: WeighingScopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0),
        private val recordIndividualGate: CompletableDeferred<AppResult<IndividualWeighingDraft>>? = null,
        private val recordIndividualGates: ArrayDeque<CompletableDeferred<AppResult<IndividualWeighingDraft>>> = ArrayDeque(),
        // Same shape as [recordIndividualGate] but for the lump-sum path -- lets a double-tap test
        // hold the FIRST recordShedPartition call suspended so the second tap's actionInFlight
        // check is exercised under UnconfinedTestDispatcher, which otherwise races the guard by
        // completing the first call's whole coroutine (including its `finally` reset) before the
        // second synchronous call happens.
        private val recordShedPartitionGate: CompletableDeferred<AppResult<ShedWeighingDraft>>? = null,
        // A22 regression coverage: the backend filters `listAssignments` server-side by parkId, so
        // a fake that mimics that (rather than always returning the same full list regardless of
        // parkId) is needed to reproduce "selecting a park collapses the chip row".
        // Keyed by parkId; `null` is the unfiltered ("All parks") page.
        private val assignmentsByPark: Map<String?, List<WeighingAssignment>> = emptyMap(),
        // The OPERATOR-grain roll-up the backend answers the same request with. Held apart from
        // `assignmentsByPark` on purpose, exactly as the wire holds it apart from `items`: these
        // numbers are whole-filter truth and are never derivable from the returned page.
        private val operatorSummariesByPark: Map<String?, List<WeighingOperatorSummary>> = emptyMap(),
        // The leadership task list this fake's Room-backed stream answers with, so a test can put a
        // planner in front of a real task and read the detail state that task produces.
        private val taskListCache: WeighingTaskListCache = WeighingTaskListCache(),
        // What the backend says this viewer may do to the assignment rows. Defaults to nothing, so
        // a test that wants the oversight actions has to say so -- exactly like the real read.
        private val assignmentCapabilities: WeighingCapabilities = WeighingCapabilities(),
        // The single-task read behind a deep link, and the park vocabulary behind the chips. Both
        // default to "the backend has nothing to say", so a test that wants them says so.
        private val taskLookups: Map<String, WeighingTaskLookup> = emptyMap(),
        private val parks: List<WeighingParkRef> = emptyList(),
        private val exportResult: AppResult<WeighingCsvExport> = AppResult.Err("Export not configured in this fake."),
        // The wizard's bucket step reads this. Real callers page per park/date; this fake answers
        // every (park, date) with the SAME fixed page, which is enough for a test that only cares
        // about one park on one date.
        private val plannerParkBuckets: WeighingPlannerParkBucketsCache = WeighingPlannerParkBucketsCache(),
        private val pagedPlannerParkBuckets: List<WeighingPlannerShed>? = null,
        // The FULL server-side keyset for a paginated task list, used only by the append-cursor
        // regression test below. Every OTHER test leaves this null and gets the fixed single-page
        // [taskListCache] behavior unchanged. When set, [observeTaskList] mirrors Room's own bounded
        // window (`LIMIT :windowSize` over whatever the cursor has appended so far) so a test can
        // prove the observed window and the appended cursor pages stay in lockstep.
        private val pagedTasks: List<WeighingTask>? = null,
        private val pagedTasksPageSize: Int = WEIGHING_LEADERSHIP_PAGE_SIZE,
        // What the SERVER says when the scope is refetched, and what Room holds AFTERWARDS.
        //
        // The conflict watcher's whole job is to re-ask the server before telling an operator to
        // redo an animal, so a test of it has to be able to make the answer CHANGE across that
        // call -- otherwise "read the refreshed record" and "read the stale one" are
        // indistinguishable and the test cannot fail. `refreshScopeResult` is the AppResult the
        // refetch returns; `postRefreshScopeState` is what the fake's Room holds once an Ok
        // refresh has run.
        private val refreshScopeResult: AppResult<Int> = AppResult.Ok(0),
        private val postRefreshScopeState: WeighingScopeState? = null,
    ) : WeighingRepository {

        /** How many of [pagedTasks] the cursor has appended into the fake's "Room" so far. */
        private var pagedTasksLoaded = pagedTasks?.let { minOf(it.size, pagedTasksPageSize) } ?: 0
        private var pagedPlannerBucketsLoaded =
            pagedPlannerParkBuckets?.let { minOf(it.size, WEIGHING_LEADERSHIP_PAGE_SIZE) } ?: 0

        /** Every distinct windowSize [observeTaskList] was asked to bound its read to, in order. */
        val observedTaskListWindowSizes = mutableListOf<Int>()

        /** Every page [appendTaskList] fetched, as the campaign ids it returned, in call order. */
        val appendedTaskListPages = mutableListOf<List<String>>()
        val observedPlannerBucketWindowSizes = mutableListOf<Int>()
        val appendedPlannerBucketPages = mutableListOf<List<String>>()

        private val pagedTaskListState: MutableStateFlow<WeighingTaskListCache>? = pagedTasks?.let { all ->
            MutableStateFlow(
                WeighingTaskListCache(
                    items = all.take(pagedTasksLoaded),
                    activeCount = all.size,
                    completedCount = 0,
                    canLoadMore = pagedTasksLoaded < all.size,
                ),
            )
        }
        private val pagedPlannerBucketState: MutableStateFlow<WeighingPlannerParkBucketsCache>? =
            pagedPlannerParkBuckets?.let { all ->
                MutableStateFlow(
                    WeighingPlannerParkBucketsCache(
                        parkId = "park-cbe",
                        hasCache = false,
                        sheds = emptyList(),
                        canLoadMore = true,
                    ),
                )
            }

        // Cursor-append stubs. These fakes exercise the READ path; Ok(0) means "no further
        // page", which leaves every existing assertion about page CONTENTS unchanged.
        override suspend fun appendTaskList(scope: String, parkId: String?): AppResult<Int> {
            val all = pagedTasks ?: return AppResult.Ok(0)
            val state = pagedTaskListState ?: return AppResult.Ok(0)
            if (pagedTasksLoaded >= all.size) return AppResult.Ok(0)
            val page = all.drop(pagedTasksLoaded).take(pagedTasksPageSize)
            pagedTasksLoaded += page.size
            appendedTaskListPages += page.map { it.campaignId }
            state.value = state.value.copy(
                items = all.take(pagedTasksLoaded),
                canLoadMore = pagedTasksLoaded < all.size,
            )
            return AppResult.Ok(page.size)
        }

        override suspend fun appendTaskBuckets(campaignId: String): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendLeadershipShed(campaignId: String, campaignShedId: String): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendLeadershipVideos(): AppResult<Int> = AppResult.Ok(0)

        override suspend fun appendPlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            excludeCampaignId: String?,
        ): AppResult<Int> {
            val all = pagedPlannerParkBuckets ?: return AppResult.Ok(0)
            val state = pagedPlannerBucketState ?: return AppResult.Ok(0)
            if (pagedPlannerBucketsLoaded >= all.size) return AppResult.Ok(0)
            val page = all.drop(pagedPlannerBucketsLoaded).take(WEIGHING_LEADERSHIP_PAGE_SIZE)
            pagedPlannerBucketsLoaded += page.size
            appendedPlannerBucketPages += page.map { it.locationId }
            state.value = state.value.copy(
                parkId = parkId,
                hasCache = true,
                sheds = all.take(pagedPlannerBucketsLoaded),
                canLoadMore = pagedPlannerBucketsLoaded < all.size,
            )
            return AppResult.Ok(page.size)
        }


        /** Every campaign id this fake was asked to export, in order. */
        val exportCalls = mutableListOf<String>()

        /** Every single-task read this fake was asked for, in order. */
        val taskLookupCalls = mutableListOf<String>()

        /** How many times the park VOCABULARY read was issued. */
        var parkVocabularyReads = 0
            private set

        /** How many times the PLANNER catalog read was issued. It is gated on weighing.plan. */
        var plannerCatalogRefreshes = 0
            private set
        private val observedScope = MutableStateFlow(scopeState)
        var lastCapture: IndividualWeighingCapture? = null
            private set
        val captures = mutableListOf<IndividualWeighingCapture>()
        var createdDraft: WeighingPlanDraft? = null
            private set
        var updatedDraft: WeighingPlanDraft? = null
            private set

        override suspend fun individualDraftsSnapshot(scopeKey: String): List<IndividualWeighingDraft> =
            observeScope(scopeKey, Int.MAX_VALUE).first().individualDrafts

        override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
            observedScope

        override suspend fun listAssignments(
            cursor: String?,
            scope: String,
            parkId: String?,
        ): AppResult<WeighingPage<WeighingAssignment>> =
            AppResult.Ok(
                WeighingPage(
                    items = assignmentsByPark[parkId] ?: emptyList(),
                    nextCursor = null,
                    capabilities = assignmentCapabilities,
                    operatorSummaries = operatorSummariesByPark[parkId] ?: emptyList(),
                ),
            )


        // --- Leadership reads: Room-backed observe/refresh pairs -------------------------
        //
        // The weighing leadership surfaces render from Room. These tests exercise the CAPTURE
        // flow, which has no leadership cache, so the observed streams are empty and the refreshes
        // are no-ops -- never network calls dressed up as a cache.

        override fun observeTaskList(
            scope: String,
            parkId: String?,
            windowSize: Int,
        ): Flow<WeighingTaskListCache> {
            observedTaskListWindowSizes += windowSize
            return pagedTaskListState ?: MutableStateFlow(taskListCache)
        }

        override suspend fun refreshTaskList(scope: String, parkId: String?, reset: Boolean): AppResult<Int> =
            AppResult.Ok(0)

        override suspend fun getTask(campaignId: String): AppResult<WeighingTaskLookup> {
            taskLookupCalls += campaignId
            return AppResult.Ok(taskLookups[campaignId] ?: WeighingTaskLookup.NotFound)
        }

        override suspend fun listParks(): AppResult<List<WeighingParkRef>> {
            parkVocabularyReads++
            return AppResult.Ok(parks)
        }

        override suspend fun exportCampaignCsv(campaignId: String): AppResult<WeighingCsvExport> {
            exportCalls += campaignId
            return exportResult
        }

        override fun observeTaskBuckets(campaignId: String, windowSize: Int): Flow<WeighingTaskBucketCache> =
            MutableStateFlow(WeighingTaskBucketCache())

        override suspend fun refreshTaskBuckets(campaignId: String, reset: Boolean): AppResult<Int> =
            AppResult.Ok(0)

        override fun observeLeadershipShed(
            campaignId: String,
            campaignShedId: String,
            windowSize: Int,
        ): Flow<WeighingLeadershipShedCache> = MutableStateFlow(WeighingLeadershipShedCache())

        override suspend fun refreshLeadershipShed(
            campaignId: String,
            campaignShedId: String,
            reset: Boolean,
        ): AppResult<Int> = AppResult.Ok(0)

        override fun observeLeadershipVideos(windowSize: Int): Flow<List<WeighingLeadershipShed>> =
            MutableStateFlow(emptyList())

        override suspend fun refreshLeadershipVideos(reset: Boolean): AppResult<Int> = AppResult.Ok(0)

        override fun observePlannerCatalog(
            periodStartDate: String,
        ): Flow<WeighingPlannerCatalogCache> = MutableStateFlow(
            WeighingPlannerCatalogCache(catalog = cachedPlannerCatalog, hasCache = true),
        )

        /**
         * A catalog refresh reports the seeded outcome, and the cache above holds whatever it
         * produced — an Err seeds NOTHING, which is what a first read that never landed looks like.
         */
        override suspend fun refreshPlannerCatalog(periodStartDate: String): AppResult<Int> {
            plannerCatalogRefreshes += 1
            return when (val seeded = plannerCatalogResult) {
                is AppResult.Err -> AppResult.Err(seeded.message)
                else -> AppResult.Ok(cachedPlannerCatalog.parks.size)
            }
        }

        /** The catalog this fake's cached planner stream answers with. */
        private val cachedPlannerCatalog: WeighingPlannerCatalog =
            (plannerCatalogResult as? AppResult.Ok)?.value ?: (
                WeighingPlannerCatalog(
                    parks = listOf(
                        WeighingPlannerPark(
                            parkId = "park-cpt",
                            name = "CPT - Channapatna",
                            kidCount = 278,
                            // Park grain: the catalog carries a shed COUNT; the rows page per park.
                            shedCount = 4,
                            existingCampaign = null,
                        ),
                    ),
                    operators = listOf(WeighingPlannerOperator("operator-amit", "Amit Kumar", "AMIT")),
                ).takeIf { plannerCatalogResult == null } ?: WeighingPlannerCatalog(emptyList(), emptyList())
            )

        /** The `excludeCampaignId` passed on every bucket-page refresh, most recent last. */
        val plannerParkBucketExcludeCalls: MutableList<String?> = mutableListOf()

        override fun observePlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            windowSize: Int,
            excludeCampaignId: String?,
        ): Flow<WeighingPlannerParkBucketsCache> {
            observedPlannerBucketWindowSizes += windowSize
            val paged = pagedPlannerBucketState
            if (paged != null) {
                val bounded = windowSize.coerceAtLeast(1)
                return paged.map { cache ->
                    cache.copy(sheds = cache.sheds.take(bounded))
                }
            }
            return MutableStateFlow(plannerParkBuckets)
        }

        override suspend fun refreshPlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            reset: Boolean,
            excludeCampaignId: String?,
        ): AppResult<Int> {
            plannerParkBucketExcludeCalls += excludeCampaignId
            val all = pagedPlannerParkBuckets
            val state = pagedPlannerBucketState
            if (all != null && state != null) {
                if (reset) {
                    pagedPlannerBucketsLoaded = minOf(all.size, WEIGHING_LEADERSHIP_PAGE_SIZE)
                }
                state.value = state.value.copy(
                    parkId = parkId,
                    hasCache = true,
                    sheds = all.take(pagedPlannerBucketsLoaded),
                    canLoadMore = pagedPlannerBucketsLoaded < all.size,
                )
                return AppResult.Ok(pagedPlannerBucketsLoaded)
            }
            return AppResult.Ok(0)
        }

        /** Records the in-place availability re-reads the wizard asks for. */
        var availabilityRefreshes: MutableList<Triple<String, String, Int>> = mutableListOf()

        override suspend fun refreshPlannerParkBucketAvailability(
            periodStartDate: String,
            parkId: String,
            pages: Int,
            excludeCampaignId: String?,
        ): AppResult<Int> {
            availabilityRefreshes.add(Triple(periodStartDate, parkId, pages))
            plannerParkBucketExcludeCalls += excludeCampaignId
            return AppResult.Ok(0)
        }

        override suspend fun createAndPublishPlan(draft: WeighingPlanDraft): AppResult<WeighingAssignment?> {
            createdDraft = draft
            return AppResult.Ok(null)
        }

        override suspend fun createPlan(draft: WeighingPlanDraft, publish: Boolean): AppResult<String> {
            createdDraft = draft
            return AppResult.Ok("campaign-1")
        }

        override suspend fun updatePlan(campaignId: String, draft: WeighingPlanDraft): AppResult<WeighingAssignment?> {
            updatedDraft = draft
            return AppResult.Ok(null)
        }

        var publishedCampaignId: String? = null
            private set

        override suspend fun publishCampaign(campaignId: String): AppResult<Unit> {
            publishedCampaignId = campaignId
            return AppResult.Ok(Unit)
        }

        /** How many times the scope refetch was issued. */
        var refreshScopeCalls = 0
            private set

        /**
         * Whether the server has the NEW answer yet. Opening the screen already refetches once, so
         * a test that wants the verdict to arrive with a LATER refetch has to say when the server
         * changed its mind -- that gap is the race being tested.
         */
        var postRefreshArmed = false

        override suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, maxRows: Int): AppResult<Int> {
            refreshScopeCalls++
            // Room is written INSIDE the suspend call in production, so the fake does the same:
            // only a SUCCESSFUL refresh replaces what a later snapshot read will see.
            if (refreshScopeResult is AppResult.Ok && postRefreshArmed) {
                postRefreshScopeState?.let { observedScope.value = it }
            }
            return refreshScopeResult
        }

        override suspend fun replaceRoster(scopeKey: String, rows: List<WeighingRosterRowEntity>) {}

        override suspend fun matchTag(scopeKey: String, scannedTag: String): WeighingScanMatch =
            WeighingScanMatch(null, "unknown", null, null)

        override suspend fun recordIndividual(capture: IndividualWeighingCapture): AppResult<IndividualWeighingDraft> {
            lastCapture = capture
            captures += capture
            return recordIndividualGates.removeFirstOrNull()?.await()
                ?: recordIndividualGate?.await()
                ?: AppResult.Err("not used")
        }

        override suspend fun attachIndividualProof(scopeKey: String, scannedIdentifier: String, proofCaptureId: String, serverProofId: String?) {}

        /** Calls this fake received for the per-shed-partition (lump-sum) submit path -- default
         *  success, matching [submitIndividualScopeCalls]'s shape for the RFID-scoped path. */
        val recordShedPartitionCalls = mutableListOf<ShedPartitionWeighingCapture>()
        var recordShedPartitionResult: AppResult<ShedWeighingDraft> = AppResult.Ok(
            ShedWeighingDraft(
                shedObservationId = "fake-shed-observation-id",
                resultJson = "{}",
                proofReady = true,
                readyToSubmit = true,
                idempotencyKey = "fake-shed-idempotency-key",
            ),
        )

        override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> {
            recordShedPartitionCalls += capture
            return recordShedPartitionGate?.await() ?: recordShedPartitionResult
        }

        override suspend fun attachShedPartitionProof(
            scopeKey: String,
            proofCaptureId: String,
            serverProofId: String?,
            serverProofIds: List<String>,
        ) {}

        /** Submit calls this fake received, and how it should answer them -- default success, so
         *  only a test asserting the failure path has to say otherwise. Ok now carries an outbox
         *  item id (durable-queue semantics), not Unit -- see WeighingRepository.submitIndividualScope. */
        val submitIndividualScopeCalls = mutableListOf<List<String>>()
        var submitIndividualScopeResult: AppResult<String> = AppResult.Ok("fake-outbox-item-id")

        override suspend fun submitIndividualScope(
            campaignId: String,
            campaignShedId: String,
            scannedIdentifiers: List<String>,
        ): AppResult<String> {
            submitIndividualScopeCalls += scannedIdentifiers
            return submitIndividualScopeResult
        }

        /**
         * The LIVE outbox row this fake answers `findPendingSubmit` with -- a `var`, not a
         * constructor-only value, so a test can mutate it WHILE a VM is already collecting sync
         * status (mirroring Room's own live-query semantics) and assert the screen flips to
         * read-only reactively, not only on the next cold read.
         */
        var pendingSubmitResult: AppResult<sg.mesha.goatos.core.data.sync.SyncQueueItem?> = AppResult.Ok(null)

        /** Counts calls to [findPendingSubmit] -- asserts the scoped `refreshScopeSubmitted()`
         *  trigger does not re-query on outbox activity unrelated to this scope's own submit row. */
        var findPendingSubmitCallCount: Int = 0

        override suspend fun findPendingSubmit(
            campaignId: String,
            campaignShedId: String,
        ): AppResult<sg.mesha.goatos.core.data.sync.SyncQueueItem?> {
            findPendingSubmitCallCount++
            return pendingSubmitResult
        }

        override suspend fun reopenScope(
            campaignId: String,
            campaignShedId: String,
            reason: String,
        ): AppResult<Unit> =
            AppResult.Ok(Unit)

        override suspend fun closeShedCampaign(
            campaignId: String,
            campaignShedId: String,
            reason: String,
        ): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun closeCampaign(
            campaignId: String,
            reason: String,
        ): AppResult<Unit> = AppResult.Ok(Unit)

        override suspend fun discardEditableIndividual(scopeKey: String, scannedIdentifier: String) {}

        // The weight-history read is not exercised by these tests; the fake answers empty so the
        // interface stays satisfied without inventing chart data these assertions would then
        // silently depend on.
        override suspend fun fetchGrowthSummary(
            parkId: String?,
            from: String?,
            to: String?,
        ): AppResult<sg.mesha.goatos.core.network.GrowthSummaryDto> =
            AppResult.Ok(sg.mesha.goatos.core.network.GrowthSummaryDto())

        override suspend fun fetchWeightHistory(
            parkId: String?,
            campaignShedId: String?,
        ): AppResult<sg.mesha.goatos.core.network.WeightHistoryResponseDto> =
            AppResult.Ok(sg.mesha.goatos.core.network.WeightHistoryResponseDto())
    }

    private companion object {
        const val TEST_TAG = "901007000504407"

        /** The capture's own idempotency key -- the only field joining a refused write to an animal. */
        const val CONFLICT_KEY = "weighing-capture-key-1"

        /** The scoped ViewModel's Room key, mirroring the scoped savedStateHandle below. */
        const val CONFLICT_SCOPE_KEY = "campaign-1:group-1:campaign-shed-1"
        const val SECOND_TAG = "901007000504408"
        const val SCOPE_KEY = "campaign-1:group-1:campaign-shed-1"
        const val WEIGHING_SCAN_FIELD_KEY = "weighing_free_flow_scan"
    }

    // Shared by the two oversight park-vocabulary tests. It was removed with an abandon-only test
    // it happened to sit beside; the tests that still need it are about the PARK VOCABULARY, not
    // about any transition, so the fixture outlives the deleted feature.
    private fun oversightAssignment() = WeighingAssignment(
        campaignId = "campaign-1",
        tenantId = "tenant-1",
        parkId = "park-cpt",
        parkName = "CPT - Channapatna",
        workGroupId = "shed-a",
        campaignShedId = "shed-a",
        expectedLocationId = "shed-a",
        expectedLocationLabel = "shed-a",
        label = "Gandhi 1",
        category = "individual_animal",
        operatorUserId = "operator-2",
        status = "in_progress",
        periodLabel = "2026-08-01 - 2026-08-07",
    )

}
