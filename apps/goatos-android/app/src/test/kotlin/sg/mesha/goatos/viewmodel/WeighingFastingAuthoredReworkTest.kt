package sg.mesha.goatos.viewmodel

import androidx.lifecycle.SavedStateHandle
import androidx.test.core.app.ApplicationProvider
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Before
import org.junit.Test
import org.junit.Assert.assertFalse
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import sg.mesha.goatos.capture.FakeProofCaptureSource
import sg.mesha.goatos.capture.FakePhotoCaptureSource
import sg.mesha.goatos.core.analytics.NoopAnalytics
import sg.mesha.goatos.core.analytics.NoopCrashReporter
import sg.mesha.goatos.core.data.weighing.WeighingFastingCard
import sg.mesha.goatos.core.network.dto.WeighingFastingShedCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.ui.Routes

@OptIn(ExperimentalCoroutinesApi::class)
@RunWith(RobolectricTestRunner::class)
class WeighingFastingAuthoredReworkTest {
    private val dispatcher = UnconfinedTestDispatcher()
    @Before fun setUp() = Dispatchers.setMain(dispatcher)
    @After fun tearDown() = Dispatchers.resetMain()

    @Test fun `restored authored capture is cleared when first card is rework`() = runTest(dispatcher) {
        val repo = FakeWeighingFastingRepository()
        val vm = WeighingFastingDetailViewModel(
            fastingRepository = repo,
            proofCaptureRepository = FakeProofCaptureRepository(),
            proofCaptureSource = FakeProofCaptureSource(),
            photoCaptureSource = FakePhotoCaptureSource(),
            syncRepository = RecordingFastingSyncRepository(),
            analytics = NoopAnalytics(), crashReporter = NoopCrashReporter(),
            appContext = ApplicationProvider.getApplicationContext(),
            savedStateHandle = SavedStateHandle(mapOf(
                Routes.WEIGHING_FASTING_TASK_ARG to "task-1",
                Routes.WEIGHING_FASTING_SHED_ARG to "shed-b",
                "weighing_fasting_proof_item_id:shed-b:gate" to "old-gate-upload",
                "weighing_fasting_proof_preview_path:shed-b:gate" to "/old-gate.jpg",
                "weighing_fasting_submit_outbox_item_id" to "old-submit",
            )),
        )
        repo.cardFlow.value = WeighingFastingCard(WeighingFastingShedCardDto(
            fastingTaskId = "task-1", campaignShedId = "shed-b", shedLabel = "Shed",
            subjectLabel = "Remove feed", status = "rework", removalBusinessDate = "2026-09-03",
            proofs = listOf(WeighingRemovalProofSlotDto(key = "gate", title = "Gate", kind = "photo", required = true)),
        ))
        advanceUntilIdle()
        assertFalse("Judged authored capture must be cleared after process restoration", vm.state.value.slots.single().captured)
        assertFalse("Rework needs a fresh required capture", vm.state.value.submitEnabled)
    }
}
