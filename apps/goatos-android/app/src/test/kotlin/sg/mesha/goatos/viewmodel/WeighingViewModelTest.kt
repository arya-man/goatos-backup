package sg.mesha.goatos.viewmodel

import android.view.KeyEvent
import androidx.lifecycle.SavedStateHandle
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flowOf
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
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import sg.mesha.goatos.capture.CapturedVideo
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.common.AppResult
import sg.mesha.goatos.core.data.BootstrapRepository
import sg.mesha.goatos.core.data.capture.CaptureSyncStatus
import sg.mesha.goatos.core.data.capture.ProofCaptureRow
import sg.mesha.goatos.core.data.capture.ProofCaptureRepository
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.data.weighing.IndividualWeighingCapture
import sg.mesha.goatos.core.data.weighing.IndividualWeighingDraft
import sg.mesha.goatos.core.data.weighing.ShedPartitionWeighingCapture
import sg.mesha.goatos.core.data.weighing.ShedWeighingDraft
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShed
import sg.mesha.goatos.core.data.weighing.WeighingLeadershipShedCache
import sg.mesha.goatos.core.data.weighing.WeighingPage
import sg.mesha.goatos.core.data.weighing.WeighingPlanDraft
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalog
import sg.mesha.goatos.core.data.weighing.WeighingPlannerCatalogCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerParkBucketsCache
import sg.mesha.goatos.core.data.weighing.WeighingPlannerOperator
import sg.mesha.goatos.core.data.weighing.WeighingPlannerPark
import sg.mesha.goatos.core.data.weighing.WeighingPlannerShed
import sg.mesha.goatos.core.data.weighing.WeighingRepository
import sg.mesha.goatos.core.data.weighing.WeighingRosterRowEntity
import sg.mesha.goatos.core.data.weighing.WeighingScanMatch
import sg.mesha.goatos.core.data.weighing.WeighingScopeState
import sg.mesha.goatos.core.data.weighing.WeighingTaskBucketCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskListCache
import sg.mesha.goatos.core.data.weighing.WeighingTaskPage
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.network.WEIGHING_SCOPE_ALL
import sg.mesha.goatos.core.network.BootstrapOperatorProfileDto
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingUiState
import sg.mesha.goatos.feature.weighing.plan.WeighingRepeatSeedStore
import sg.mesha.goatos.rfid.RfidRead
import sg.mesha.goatos.rfid.RfidReaderDevice
import sg.mesha.goatos.rfid.RfidReaderPort
import sg.mesha.goatos.rfid.RfidReaderStatus

@OptIn(ExperimentalCoroutinesApi::class)
class WeighingViewModelTest {
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
                    expectedLocationLabel = "Kid Shed B",
                    actualLocationLabel = null,
                    status = "Scanned",
                    availabilityStatus = null,
                    wrongShed = false,
                    weightSaved = true,
                    proofUploadStatus = ProofUploadStatus.SYNCED,
                    backendSynced = false,
                ),
            ),
        )

        assertTrue(state.individualSubmitReady)
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
        val vm = weighingViewModel(repository, scoped = true)
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.recordIndividual(TEST_TAG, "13.5")
        runCurrent()

        assertTrue(vm.state.value.visibleRows.single().weightUpdating)
        assertEquals(13.5, repository.lastCapture?.weightKg)

        gate.complete(AppResult.Ok(original.copy(weightKg = 13.5, syncedToBackend = false)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.single().weightUpdating)
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

        assertEquals(listOf(TEST_TAG, SECOND_TAG), repository.captures.map { it.animalId })
        val bothPendingState = vm.state.value.visibleRows.associateBy { it.animalId }
        assertTrue(bothPendingState.getValue(TEST_TAG).weightUpdating)
        assertTrue(bothPendingState.getValue(SECOND_TAG).weightUpdating)

        firstGate.complete(AppResult.Ok(acceptedDraft(weightKg = 12.0)))
        secondGate.complete(AppResult.Ok(acceptedDraft(animalId = SECOND_TAG, weightKg = 13.5)))
        advanceUntilIdle()

        assertFalse(vm.state.value.visibleRows.any { it.weightUpdating })
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
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(scopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0)),
            scoped = true,
            proofCaptureRepository = proofs,
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
            weighingCategory = "per_shed_partition",
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        vm.captureShedVideo()
        advanceUntilIdle()

        assertEquals(1, proofs.captureCalls.size)
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
                        animalId = TEST_TAG,
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
                        animalId = TEST_TAG,
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
            caption = "Lump-sum group video 1",
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
     * Free-flow weighing has the SAME wrong-animal capture window vaccination had (fixed in
     * 2db1eb207): scanning animal A opened the camera bound to A, and a scan of animal B while A's
     * recording was still in progress was dropped on the floor with no camera, no message and no
     * retarget — so footage actually shot at B was saved under A's tag, and the weight typed next
     * landed on A too. Free-flow does not make this safe: it just means the wrong SCANNED TAG is
     * written instead of the wrong animal id.
     */
    @Test
    fun `scanning a second animal while the first video is recording never saves that video under the first animal`() = runTest(dispatcher) {
        val proofSource = FakeProofCaptureSource()
        val proofs = FakeProofCaptureRepository()
        val scans = FakeScanCaptureRepository()
        val vm = weighingViewModel(
            repository = FakeWeighingRepository(),
            scoped = true,
            scanCaptureRepository = scans,
            proofCaptureRepository = proofs,
            proofCaptureSource = proofSource,
            bootstrapRepository = OperatorBootstrapRepository,
        )
        backgroundScope.launch(dispatcher) { vm.state.collect {} }
        advanceUntilIdle()

        // Animal A is scanned; its camera opens and is still recording (captureVideo() has not
        // returned — nothing written yet) when the operator walks to animal B and scans it.
        val gateA = proofSource.queueGate()
        vm.onScanInputChange(TEST_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()
        assertEquals("animal A's camera opened", 1, proofSource.captureCount)
        assertEquals(0, proofs.captureCalls.size)

        val gateB = proofSource.queueGate()
        vm.onScanInputChange(SECOND_TAG)
        vm.submitTypedScan()
        advanceUntilIdle()

        assertEquals(
            "scanning animal B must cancel A's unfinished recording and reopen the camera for B",
            2,
            proofSource.captureCount,
        )
        assertEquals("no video may be saved until a recording actually finishes", 0, proofs.captureCalls.size)
        assertTrue(
            "the operator must be told animal A was left without its video, not silently ignored",
            vm.state.value.message.orEmpty().contains(TEST_TAG),
        )

        // A's cancelled recording finishing late must never be saved under anyone.
        gateA.complete(CapturedVideo(localUri = "file://animal-a.mp4", startedAtMs = 1, endedAtMs = 2))
        advanceUntilIdle()
        assertEquals("a cancelled recording's late result must never be saved", 0, proofs.captureCalls.size)

        gateB.complete(CapturedVideo(localUri = "file://animal-b.mp4", startedAtMs = 3, endedAtMs = 4))
        advanceUntilIdle()

        assertEquals(1, proofs.captureCalls.size)
        assertEquals(SECOND_TAG, proofs.captureCalls.single().caption)
        assertEquals("file://animal-b.mp4", proofs.captureCalls.single().localUri)
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
            repository.lastCapture?.animalId,
        )
        assertEquals(SECOND_TAG, repository.lastCapture?.scannedIdentifier)
    }

    private fun weighingViewModel(
        repository: FakeWeighingRepository,
        scoped: Boolean = false,
        // Which weighing SURFACE this ViewModel is standing in for. Planning belongs to the planner
        // surface, so a test exercising the planner has to say so -- the route declares it in the app.
        surface: String? = null,
        scanCaptureRepository: FakeScanCaptureRepository = FakeScanCaptureRepository(),
        proofCaptureRepository: FakeProofCaptureRepository = FakeProofCaptureRepository(),
        proofCaptureSource: FakeProofCaptureSource = FakeProofCaptureSource(),
        bootstrapRepository: BootstrapRepository = LeadershipBootstrapRepository,
        weighingCategory: String = "individual_animal",
    ): WeighingViewModel =
        WeighingViewModel(
            repository = repository,
            bootstrapRepository = bootstrapRepository,
            reader = FakeRfidReaderPort(),
            scanCaptureRepository = scanCaptureRepository,
            proofCaptureRepository = proofCaptureRepository,
            proofCaptureSource = proofCaptureSource,
            analytics = NoopAnalytics(),
            crashReporter = NoopCrashReporter(),
            repeatSeedStore = WeighingRepeatSeedStore(),
            savedStateHandle = SavedStateHandle(
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
        animalId = animalId,
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

    private class FakeWeighingRepository(
        private val plannerCatalogResult: AppResult<WeighingPlannerCatalog>? = null,
        scopeState: WeighingScopeState = WeighingScopeState(emptyList(), emptyList(), emptyList(), 0),
        private val recordIndividualGate: CompletableDeferred<AppResult<IndividualWeighingDraft>>? = null,
        private val recordIndividualGates: ArrayDeque<CompletableDeferred<AppResult<IndividualWeighingDraft>>> = ArrayDeque(),
    ) : WeighingRepository {
        private val observedScope = MutableStateFlow(scopeState)
        var lastCapture: IndividualWeighingCapture? = null
            private set
        val captures = mutableListOf<IndividualWeighingCapture>()
        var createdDraft: WeighingPlanDraft? = null
            private set
        var updatedDraft: WeighingPlanDraft? = null
            private set

        override fun observeScope(scopeKey: String, windowSize: Int): Flow<WeighingScopeState> =
            observedScope

        override suspend fun listAssignments(
            cursor: String?,
            scope: String,
            parkId: String?,
        ): AppResult<WeighingPage<WeighingAssignment>> =
            AppResult.Ok(WeighingPage(emptyList(), null))

        // --- Leadership reads: Room-backed observe/refresh pairs -------------------------
        //
        // The weighing leadership surfaces render from Room. These tests exercise the CAPTURE
        // flow, which has no leadership cache, so the observed streams are empty and the refreshes
        // are no-ops -- never network calls dressed up as a cache.

        override fun observeTaskList(
            scope: String,
            parkId: String?,
            windowSize: Int,
        ): Flow<WeighingTaskListCache> = MutableStateFlow(WeighingTaskListCache())

        override suspend fun refreshTaskList(scope: String, parkId: String?, reset: Boolean): AppResult<Int> =
            AppResult.Ok(0)

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
        override suspend fun refreshPlannerCatalog(periodStartDate: String): AppResult<Int> =
            when (val seeded = plannerCatalogResult) {
                is AppResult.Err -> AppResult.Err(seeded.message)
                else -> AppResult.Ok(cachedPlannerCatalog.parks.size)
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

        override fun observePlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            windowSize: Int,
        ): Flow<WeighingPlannerParkBucketsCache> = MutableStateFlow(WeighingPlannerParkBucketsCache())

        override suspend fun refreshPlannerParkBuckets(
            periodStartDate: String,
            parkId: String,
            reset: Boolean,
        ): AppResult<Int> = AppResult.Ok(0)

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

        override suspend fun refreshScope(campaignId: String, workGroupId: String, campaignShedId: String, maxRows: Int): AppResult<Int> =
            AppResult.Ok(0)

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

        override suspend fun attachIndividualProof(scopeKey: String, animalId: String, proofCaptureId: String, serverProofId: String?) {}

        override suspend fun recordShedPartition(capture: ShedPartitionWeighingCapture): AppResult<ShedWeighingDraft> =
            AppResult.Err("not used")

        override suspend fun attachShedPartitionProof(
            scopeKey: String,
            proofCaptureId: String,
            serverProofId: String?,
            serverProofIds: List<String>,
        ) {}

        override suspend fun submitIndividualScope(
            campaignId: String,
            campaignShedId: String,
            scannedIdentifiers: List<String>,
        ): AppResult<Unit> =
            AppResult.Ok(Unit)

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

        override suspend fun discardEditableIndividual(scopeKey: String, animalId: String) {}
    }

    private companion object {
        const val TEST_TAG = "901007000504407"
        const val SECOND_TAG = "901007000504408"
        const val SCOPE_KEY = "campaign-1:group-1:campaign-shed-1"
        const val WEIGHING_SCAN_FIELD_KEY = "weighing_free_flow_scan"
    }
}
