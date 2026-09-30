package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.sop.SopQuestionUi
import sg.mesha.goatos.feature.pccare.PcCarePlanOption
import sg.mesha.goatos.feature.pccare.PcCarePlanStep
import sg.mesha.goatos.feature.pccare.PcCarePlanUiState
import sg.mesha.goatos.feature.pccare.PcCarePlanWizardScreen
import sg.mesha.goatos.feature.pccare.PcCareSlotChipUi
import sg.mesha.goatos.feature.pccare.PcCareSlotState
import sg.mesha.goatos.feature.pccare.PcCareTaskScreen
import sg.mesha.goatos.feature.pccare.PcCareTaskUiState

/**
 * The two feed & water removal faces, as the operator and the planner see them (PC CARE SOP,
 * 2026-09-22): the removal is decided WHILE ASSIGNING, with its own people, and it is recorded
 * as two videos per pen.
 */
class PcCareFeedWaterRemovalScreenshotTest {
    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_5)

    private val park = "00000000-0000-4000-8000-000000003002"
    private val operators = listOf(
        PcCarePlanOption("op-amit", "Amit Kumar", listOf(park)),
        PcCarePlanOption("op-sagar", "Sagar Mahoor", listOf(park)),
        PcCarePlanOption("op-bipin", "Bipin", listOf(park)),
        PcCarePlanOption("op-manoj", "Manoj Kumar", listOf(park)),
    )

    /** The ASSIGN step: who does the work, whether feed & water come out, and who removes them. */
    @Test
    fun assignStepAsksWhoRemovesFeedAndWater() {
        paparazzi.snapshot(name = "pc_care_assign_feed_water_removal") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        PcCarePlanWizardScreen(
                            state = PcCarePlanUiState(
                                step = PcCarePlanStep.OPERATORS,
                                selectedCategoryKey = "deworming",
                                selectedCategoryLabel = "Deworming",
                                // Pinned: without it the screen falls back to LocalDate.now(), and
                                // on 30/09/2026 the footer read "Tomorrow · Thu 01/10/2026" and the
                                // golden (recorded 22/09) stopped matching.
                                today = "2026-09-22",
                                selectedDate = "2026-10-01",
                                selectedParkId = park,
                                selectedParkLabel = "Channapatna",
                                selectedPenLabel = "Castro 1",
                                selectedPenKeys = setOf("castro-1"),
                                operators = operators,
                                selectedOperatorIds = setOf("op-amit"),
                                feedRemovalOffered = true,
                                feedRemovalIsAChoice = true,
                                feedRemovalRequired = true,
                                selectedRemovalOperatorIds = setOf("op-bipin", "op-manoj"),
                            ),
                        )
                    }
                }
            }
        }
    }

    /** The RECORD face: one card per authored capture, the feed video shot and the water still owed. */
    @Test
    fun removalCardRecordsAVideoPerCapture() {
        paparazzi.snapshot(name = "pc_care_record_feed_water_removal") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        PcCareTaskScreen(
                            state = PcCareTaskUiState(
                                title = "Remove feed & water · Castro 1",
                                locationDisplay = "Castro 1",
                                parkLabel = "Channapatna",
                                dateLabel = "2026-09-30",
                                assigneeLine = "Assigned to Bipin, Manoj Kumar",
                                taskProofMode = true,
                                taskProofSlots = listOf(
                                    PcCareSlotChipUi(
                                        fieldKey = "feed_video",
                                        label = "Feed removal video",
                                        state = PcCareSlotState.SYNCED,
                                        statusLabel = "Video captured",
                                        description = "Show the feed being taken out of this pen",
                                        canRecord = true,
                                    ),
                                    PcCareSlotChipUi(
                                        fieldKey = "water_video",
                                        label = "Water removal video",
                                        state = PcCareSlotState.EMPTY,
                                        statusLabel = "Not recorded",
                                        description = "Show the water being taken out of this pen",
                                        canRecord = true,
                                    ),
                                    // Authored as a PHOTO: the row must offer the photo verb, not
                                    // "Record video" (PR 349 review finding 2).
                                    PcCareSlotChipUi(
                                        fieldKey = "trough_photo",
                                        label = "Empty trough photo",
                                        state = PcCareSlotState.EMPTY,
                                        statusLabel = "Not recorded",
                                        description = "Show the trough with nothing left in it",
                                        kind = "photo",
                                        canRecord = true,
                                    ),
                                ),
                                submitEnabled = false,
                            ),
                        )
                    }
                }
            }
        }
    }

    /** A care card that ASKS: the questions must be on the screen, above the submit. */
    @Test
    fun theCareCardAsksTheQuestionsItAuthors() {
        paparazzi.snapshot(name = "pc_care_task_questions") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        PcCareTaskScreen(
                            state = PcCareTaskUiState(
                                title = "Deworming · Castro 1",
                                locationDisplay = "Castro 1",
                                parkLabel = "Channapatna",
                                dateLabel = "2026-10-01",
                                assigneeLine = "Assigned to Amit Kumar",
                                animalCountLabel = "1 animal",
                                questions = listOf(
                                    SopQuestionUi(
                                        id = "dose_taken",
                                        kind = "choice",
                                        title = "Did the animal take the full dose?",
                                        required = true,
                                        options = listOf("yes" to "Yes", "no" to "No"),
                                    ),
                                    SopQuestionUi(
                                        id = "why_not",
                                        kind = "text",
                                        title = "Why not?",
                                        required = true,
                                        onlyIfQuestion = "dose_taken",
                                        onlyIfValue = "no",
                                    ),
                                ),
                                answers = mapOf("dose_taken" to "no"),
                                submitEnabled = false,
                                submitBlockedReason = "Enter: Why not?",
                            ),
                        )
                    }
                }
            }
        }
    }
}
