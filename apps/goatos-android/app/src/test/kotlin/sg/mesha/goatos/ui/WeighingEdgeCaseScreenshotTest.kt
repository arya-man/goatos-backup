package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.weighing.WeighingDraftUiRow
import sg.mesha.goatos.feature.weighing.WeighingFastingCardUiRow
import sg.mesha.goatos.feature.weighing.WeighingFastingTone
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow
import sg.mesha.goatos.feature.weighing.WeighingRosterUiRow
import sg.mesha.goatos.feature.weighing.WeighingScreen
import sg.mesha.goatos.feature.weighing.WeighingUiState

class WeighingEdgeCaseScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun weighingIndividualWeightAndProof() = shot("weighing_case_01_individual_weight_proof") {
        WeighingScreen(state = individualState())
    }

    // Weighing is free-flow and has no herd "wrong shed" concept -- the only rule surfaced here is
    // the duplicate-identifier check within this task's shed bucket.
    @Test
    fun weighingIndividualDuplicateScan() = shot("weighing_case_02_individual_duplicate_scan") {
        WeighingScreen(
            state = individualState().copy(
                message = "Already scanned in this shed",
                visibleRows = weighingRows(duplicate = true),
            ),
        )
    }

    @Test
    fun weighingIndividualOfflineQueued() = shot("weighing_case_03_individual_offline_queued") {
        WeighingScreen(
            state = individualState().copy(
                message = "Saved on this phone. It will sync when network returns.",
                individualDrafts = listOf(
                    WeighingDraftUiRow("draft-1", "goat-407", "901007000504407 · 18.4 kg · queued", proofReady = false, readyToSubmit = false),
                    WeighingDraftUiRow("draft-2", "goat-418", "901007000504418 · 19.1 kg · queued", proofReady = false, readyToSubmit = false),
                ),
            ),
        )
    }

    @Test
    fun weighingLumpsumShedProof() = shot("weighing_case_04_lumpsum_shed_proof") {
        WeighingScreen(
            state = sampleWeighingOperatorState().copy(
                title = "Kid Shed B / Part 1",
                scopeLabel = "WEIGHING · Lumpsum",
                category = "per_shed_partition",
                totalExpected = 2,
                scanInput = "",
                selectedAnimalId = null,
                selectedAnimalLabel = null,
                weightInput = "74.5",
                individualDrafts = emptyList(),
                shedDrafts = listOf(
                    WeighingDraftUiRow("shed-proof", label = "Kid Shed B / Part 1 · 74.5 kg · shed proof ready", proofReady = true, readyToSubmit = true),
                ),
                visibleRows = emptyList(),
            ),
        )
    }

    @Test
    fun weighingLongTextStress() = shot("weighing_case_05_long_text_stress") {
        WeighingScreen(
            state = individualState().copy(
                title = "Kid Shed A / Part 1 / Very Long Shed Display Name",
                selectedAnimalLabel = "901007000504407 · secondary 901007000504407B",
                individualDrafts = listOf(
                    WeighingDraftUiRow(
                        id = "draft-long",
                        animalId = "goat-long",
                        label = "901007000504407 · secondary 901007000504407B · 18.4 kg · proof uploading",
                        proofReady = false,
                        readyToSubmit = false,
                    ),
                ),
            ),
        )
    }

    @Test
    fun weighingFeedWaterRemovalOnlyActionableCards() = shot("weighing_case_06_feed_water_removal_clean") {
        WeighingScreen(
            state = WeighingUiState(
                title = "My work",
                scopeLabel = "WEIGHING",
                hasScope = false,
                hasLoadedOnce = true,
                assignments = listOf(
                    weighingAssignment(
                        campaignShedId = "actual-gandhi-1",
                        expectedLocationLabel = "Gandhi 1",
                        category = "individual_animal",
                        periodLabel = "Today",
                    ),
                    weighingAssignment(
                        campaignShedId = "actual-mandela-2",
                        expectedLocationLabel = "Mandela 2 - Part 1",
                        category = "per_shed_partition",
                        periodLabel = "Today",
                    ),
                ),
            ),
            fastingCards = listOf(
                WeighingFastingCardUiRow(
                    uiKey = "removal|fasting-1|shed-yashoda-10",
                    fastingTaskId = "fasting-1",
                    campaignShedId = "shed-yashoda-10",
                    title = "Yashoda 10",
                    dateLabel = "Tonight",
                    status = "open",
                    statusLabel = "Open",
                    tone = WeighingFastingTone.ACTION,
                    openable = true,
                ),
                WeighingFastingCardUiRow(
                    uiKey = "removal|fasting-1|shed-godel-2-part-1",
                    fastingTaskId = "fasting-1",
                    campaignShedId = "shed-godel-2-part-1",
                    title = "Godel 2 - Part 1",
                    dateLabel = "Tonight",
                    status = "rework",
                    statusLabel = "Needs another video",
                    tone = WeighingFastingTone.SENT_BACK,
                    reworkReason = "Water video is too dark. Record again.",
                    openable = true,
                ),
            ),
        )
    }

    private fun shot(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }
}

private fun individualState(): WeighingUiState = sampleWeighingOperatorState().copy(
    title = "Kid Shed A / Part 1",
    scopeLabel = "WEIGHING · Individual",
    category = "individual_animal",
    totalExpected = 5,
    selectedAnimalId = "goat-407",
    selectedAnimalLabel = "901007000504407 · 2 tags",
    scanInput = "901007000504407",
    weightInput = "18.4",
    individualDrafts = listOf(
        WeighingDraftUiRow("draft-1", "goat-407", "901007000504407 · 18.4 kg · proof uploading", proofReady = false, readyToSubmit = false),
        WeighingDraftUiRow("draft-2", "goat-418", "901007000504418 · 19.1 kg · proof ready", proofReady = true, readyToSubmit = true),
    ),
    visibleRows = weighingRows(duplicate = false),
)

private fun weighingRows(duplicate: Boolean): List<WeighingRosterUiRow> = listOf(
    WeighingRosterUiRow(
        id = "row-1",
        animalId = "goat-407",
        displayAnimalId = "901007000504407",
        status = if (duplicate) "Duplicate scan" else "Accepted",
    ),
    WeighingRosterUiRow(
        id = "row-2",
        animalId = "goat-418",
        displayAnimalId = "901007000504418",
        status = "Pending",
    ),
)

private fun weighingAssignment(
    campaignShedId: String,
    expectedLocationLabel: String,
    category: String,
    periodLabel: String,
) = WeighingAssignmentUiRow(
    campaignId = "campaign-1",
    tenantId = "tenant-1",
    parkId = "park-1",
    parkLabel = "CBE",
    workGroupId = "week-37",
    campaignShedId = campaignShedId,
    expectedLocationId = campaignShedId,
    expectedLocationLabel = expectedLocationLabel,
    label = expectedLocationLabel,
    category = category,
    operatorName = "Kumar Sharath",
    backendStatus = "open",
    status = "open",
    periodLabel = periodLabel,
    plannedBusinessDate = "2026-09-08",
    dueBusinessDate = "2026-09-08",
)
