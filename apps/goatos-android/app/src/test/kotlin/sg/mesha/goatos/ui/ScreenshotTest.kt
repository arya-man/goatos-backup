package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.nav.LocalIsTopLevelRoot
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.profile.AlertsScreen
import sg.mesha.goatos.feature.profile.ProfileScreen
import sg.mesha.goatos.feature.profile.RfidScreen
import sg.mesha.goatos.feature.record.RecordScreen
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.FormRunner
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanStatus
import sg.mesha.goatos.feature.timetable.TimetableScreen
import sg.mesha.goatos.feature.verify.VerificationQueueRow
import sg.mesha.goatos.feature.verify.VerifyModuleTab
import sg.mesha.goatos.feature.verify.VerifyQueueScreen
import sg.mesha.goatos.feature.verify.VerifyQueueUiState
import sg.mesha.goatos.feature.verify.VerifyTone
import sg.mesha.goatos.feature.weighing.WeighingScreen
import androidx.compose.runtime.CompositionLocalProvider
import androidx.paging.PagingData
import androidx.paging.compose.collectAsLazyPagingItems
import kotlinx.coroutines.flow.flowOf
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_PERMANENT
import sg.mesha.goatos.feature.counts.BIRTH_ID_KIND_TEMPORARY
import sg.mesha.goatos.feature.counts.BirthDeathField
import sg.mesha.goatos.feature.counts.BirthDeathScreen
import sg.mesha.goatos.feature.counts.BirthDeathUiState
import sg.mesha.goatos.feature.counts.RfidPromoteField
import sg.mesha.goatos.feature.counts.RfidPromoteScreen
import sg.mesha.goatos.feature.counts.RfidPromoteUiState
import sg.mesha.goatos.feature.counts.ShiftingExecuteAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingExecuteScreen
import sg.mesha.goatos.feature.counts.ShiftingExecuteUiState
import sg.mesha.goatos.feature.counts.ShiftingActionsScreen
import sg.mesha.goatos.feature.counts.ShiftingPendingRowUi
import sg.mesha.goatos.feature.counts.ShiftingPendingStatusUi
import sg.mesha.goatos.feature.counts.ShiftingPendingUiState
import sg.mesha.goatos.feature.counts.ShiftingPreviousDateUi
import sg.mesha.goatos.feature.counts.ShiftingAnimalUi
import sg.mesha.goatos.feature.counts.ShiftingParkUi
import sg.mesha.goatos.feature.counts.ShiftingScreen
import sg.mesha.goatos.feature.counts.ShiftingShedUi
import sg.mesha.goatos.feature.counts.ShiftingUiState

/**
 * Screenshot tests for every screen in the gallery (item 7). Each test renders the EXACT same
 * screen + mock-accurate sample state GalleryActivity uses for manual device capture
 * (adb shell am start -n sg.mesha.goatos.dev/sg.mesha.goatos.GalleryActivity -e screen <name>),
 * so there is one source of truth for "what does mock-accurate data look like" (ScreenSamples.kt)
 * shared by manual device QA and this automated Paparazzi suite.
 *
 * Run: ./gradlew :app:recordPaparazziDevDebug   (first run / after an intentional UI change —
 *   writes golden PNGs to app/src/test/snapshots/images)
 *      ./gradlew :app:verifyPaparazziDevDebug   (CI — fails on any pixel diff from the goldens)
 *
 * Gallery: tools/android/build-screenshot-gallery.py assembles every golden PNG produced here
 * into a single static HTML page for GitHub Pages / CI artifact review.
 */
class ScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    private fun shot(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        paparazzi.snapshot(name = name) {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        content()
                    }
                }
            }
        }
    }

    @Test
    fun login() = shot("login") { LoginScreen(onSignInEmail = { _, _ -> }, onGoogle = {}, onForgotPassword = {}) }

    @Test
    fun calendar() = shot("calendar") { CalendarScreen(state = sampleCalendarState()) }

    @Test
    fun sheds() = shot("sheds") {
        CompositionLocalProvider(LocalIsTopLevelRoot provides true) {
            ShedsScreen(state = sampleShedsState())
        }
    }

    @Test
    fun vaccination_sheds_initial_loading_uses_skeleton() = shot("vaccination_sheds_initial_loading_uses_skeleton") {
        CompositionLocalProvider(LocalIsTopLevelRoot provides true) {
            ShedsScreen(
                state = sampleShedsState().copy(
                    date = "",
                    window = "",
                    shedCountLabel = "",
                    dueLabel = "",
                    dayProgressLabel = "",
                    dayProgressFraction = 0f,
                    daySummary = "",
                    shedCount = 0,
                    dueCount = 0,
                    doneCount = 0,
                    caption = "Loading…",
                    rows = emptyList(),
                    rosterChanges = emptyList(),
                    kernelInfo = null,
                    lastSyncedAt = null,
                    isInitialLoading = true,
                    isRefreshing = true,
                ),
            )
        }
    }

    @Test
    fun scan() = shot("scan") { ScanScreen(state = sampleScanState()) }

    @Test
    fun weighing_plan() = shot("weighing_plan") { WeighingScreen(state = sampleWeighingPlanState()) }

    @Test
    fun weighing_operator_capture() = shot("weighing_operator_capture") {
        WeighingScreen(state = sampleWeighingOperatorState())
    }

    @Test
    fun vaccination_scan_loading_state() = shot("vaccination_scan_loading_state") {
        ScanScreen(
            state = sampleScanState().copy(
                shedLabel = "",
                cohortLabel = "",
                listTitle = "",
                submitLabel = "",
                feed = emptyList(),
                roster = emptyList(),
                lastSyncedAt = null,
                isRefreshing = true,
                scanEnabled = false,
                canSubmit = false,
            ),
        )
    }

    @Test
    fun submit() = shot("submit") { SubmitScreen(state = sampleSubmitState()) }

    @Test
    fun vaccination_submit_unanswered_draft_has_no_empty_sync_banner() =
        shot("vaccination_submit_unanswered_draft_has_no_empty_sync_banner") {
            SubmitScreen(
                state = sampleSubmitState().copy(
                    syncState = sg.mesha.goatos.feature.submit.SyncState.DRAFT,
                    canSubmit = false,
                    syncLabel = "",
                    groups = emptyList(),
                    proofSummaryTitle = "",
                    proofSummarySyncedLabel = "",
                    proofSummaryFinalizeHint = "",
                    proofTotal = 0,
                ),
            )
        }

    @Test
    fun vaccination_shed_completion_summary() = shot("vaccination_shed_completion_summary") {
        // Read-only shed acknowledgement summary (no manual medical form): human shed + drive
        // name, expected/handled/proof-ready counts, per-vaccine breakdown, and the Submit button
        // gated on backend submit_enabled.
        SubmitScreen(
            state = sampleSubmitState().copy(
                syncState = sg.mesha.goatos.feature.submit.SyncState.DRAFT,
                syncLabel = "",
                shed = "",
                cohort = "",
                date = "",
                summaryItems = emptyList(),
                groups = emptyList(),
                proofSummaryTitle = "",
                proofSummarySyncedLabel = "",
                proofSummaryFinalizeHint = "",
                proofTotal = 0,
                formRunner = null,
                submitLabel = "Submit",
                canSubmit = true,
                shedCompletionSummary = sg.mesha.goatos.feature.submit.ShedCompletionSummary(
                    taskId = "task-shed",
                    shedName = "Shed A — Weaners",
                    driveName = "Vaccination · July 2026",
                    expectedCount = 50,
                    handledCount = 50,
                    proofReadyCount = 1,
                    proofMode = "shed_level_video",
                    submitState = "draft",
                ),
                vaccineBreakdown = listOf(
                    sg.mesha.goatos.feature.submit.VaccineSummaryItem("FMD", 50),
                    sg.mesha.goatos.feature.submit.VaccineSummaryItem("PPR", 48),
                ),
                blockingReason = null,
            ),
        )
    }

    @Test
    fun record() = shot("record") { RecordScreen(state = sampleRecordState()) }

    @Test
    fun rfid() = shot("rfid") { RfidScreen(state = sampleRfidState()) }

    @Test
    fun alerts() = shot("alerts") { AlertsScreen(state = sampleAlertsState()) }

    @Test
    fun you() = shot("you") { ProfileScreen(state = sampleProfileState()) }

    @Test
    fun timetable() = shot("timetable") { TimetableScreen(state = sampleTimetableState()) }

    @Test
    fun calendar_coverage_banner() = shot("calendar_coverage_banner") {
        CalendarScreen(state = sampleCalendarWithCoverageState())
    }

    @Test
    fun offline_banner() = shot("offline_banner") { OfflineBanner(visible = true, onOpenDetails = {}) }

    @Test
    fun form_runner() = shot("form_runner") {
        FormRunner(
            state = sampleFormRunnerState(),
            onToggle = { _, _ -> }, onText = { _, _ -> }, onScan = {}, onPick = { _, _ -> }, onCaptureVideo = { _, _ -> }, onSubmit = {},
        )
    }

    @Test
    fun vaccination_scan_row_proof() = shot("vaccination_scan_row_proof") {
        ScanListSheet(
            title = "Gandhi 1 · goat proof",
            rows = listOf(
                RosterRow(
                    primaryTag = "RFID 004821",
                    secondaryTag = "Ear tag 118",
                    vaccineLabel = "PPR · FMD",
                    status = ScanStatus.DONE,
                    goatId = "goat-1",
                    proofClipCount = 2,
                    proofUploadStatus = ProofUploadStatus.SYNCED,
                ),
                RosterRow(
                    primaryTag = "RFID 004839",
                    secondaryTag = "Ear tag 126",
                    vaccineLabel = "PPR",
                    status = ScanStatus.DONE,
                    goatId = "goat-2",
                    proofClipCount = 1,
                    proofUploadStatus = ProofUploadStatus.UPLOADING,
                ),
                RosterRow(
                    primaryTag = "RFID 004847",
                    secondaryTag = "Ear tag 132",
                    vaccineLabel = "Enterotoxaemia · Tetanus toxoid",
                    status = ScanStatus.DONE,
                    goatId = "goat-3",
                    proofClipCount = 0,
                    proofUploadStatus = ProofUploadStatus.MISSING,
                ),
                RosterRow(
                    primaryTag = "RFID 004851",
                    secondaryTag = "Ear tag 137",
                    vaccineLabel = "PPR booster",
                    status = ScanStatus.DONE,
                    goatId = "goat-4",
                    proofClipCount = 1,
                    proofUploadStatus = ProofUploadStatus.FAILED,
                ),
            ),
            captureEnabled = true,
        )
    }

    @Test
    fun vaccination_shed_review() = shot("vaccination_shed_review") {
        SubmitScreen(
            state = sampleSubmitState().copy(
                syncState = sg.mesha.goatos.feature.submit.SyncState.ACKED,
                syncProgress = 1f,
                submitLabel = "Finalize shed",
                proofSummarySyncedLabel = "40 of 40 goats synced",
                proofSynced = 40,
                proofUploading = 0,
            ),
        )
    }

    @Test
    fun vaccination_verifier_queue() = shot("vaccination_verifier_queue") {
        VerifyQueueScreen(
            state = VerifyQueueUiState(
                selectedModule = VerifyModuleTab.VACCINATION,
                rows = listOf(
                    VerificationQueueRow(
                        id = "proof-1",
                        category = "vaccination_proof",
                        categoryLabel = "Vaccination",
                        title = "Goat CBE-0412 · Gandhi 1",
                        subtitle = "Coimbatore · 2 camera clips · Arun",
                        statusTone = VerifyTone.PENDING,
                    ),
                    VerificationQueueRow(
                        id = "proof-2",
                        category = "vaccination_proof",
                        categoryLabel = "Vaccination",
                        title = "Goat CBE-0419 · Gandhi 1",
                        subtitle = "Coimbatore · 1 camera clip · Arun",
                        statusTone = VerifyTone.PENDING,
                    ),
                ),
                lastSyncedAt = System.currentTimeMillis(),
            ),
        )
    }

    @Test
    fun weighing_verifier_queue() = shot("weighing_verifier_queue") {
        VerifyQueueScreen(
            state = VerifyQueueUiState(
                selectedModule = VerifyModuleTab.WEIGHING,
                rows = listOf(
                    VerificationQueueRow(
                        id = "weighing-1",
                        category = "weighing_proof",
                        categoryLabel = "Weighing",
                        title = "Kid Shed B · 5 animal videos",
                        subtitle = "Coimbatore · Pramod",
                        statusTone = VerifyTone.PENDING,
                    ),
                    VerificationQueueRow(
                        id = "weighing-2",
                        category = "weighing_proof",
                        categoryLabel = "Weighing",
                        title = "Kid Shed C · lump-sum videos",
                        subtitle = "Coimbatore · Pramod",
                        statusTone = VerifyTone.PENDING,
                    ),
                ),
                lastSyncedAt = System.currentTimeMillis(),
            ),
        )
    }

    @Test
    fun shifting_add_locks_current_farm() = shot("shifting_add_locks_current_farm") {
        val animal = ShiftingAnimalUi(
            goatId = "d8337607-6e21-41c9-a703-a7b73ae4e545",
            displayId = "G-000326",
            tag = "CBE-ASSUMED-RFID-00002",
            parkId = "00000000-0000-4000-8000-000000003001",
            shedId = "43071c6e-3b00-47a9-860c-1bbacb570575",
            parkName = "Coimbatore",
            shedName = "Castro 1",
            lifecycleStatus = "alive",
        )
        ShiftingScreen(
            state = ShiftingUiState(
                animalQuery = animal.tag,
                animalMatches = listOf(animal),
                selectedAnimal = animal,
                destinationParks = listOf(
                    ShiftingParkUi(
                        parkId = animal.parkId,
                        name = animal.parkName,
                        sheds = listOf(
                            ShiftingShedUi("43071c6e-3b00-47a9-860c-1bbacb570576", "Castro 2"),
                        ),
                    ),
                ),
                destinationParkId = animal.parkId,
            ),
        )
    }

    @Test
    fun shifting_pending() = shot("shifting_pending") {
        val rows = flowOf(
            PagingData.from(
                listOf(
                    ShiftingPendingRowUi(
                        shiftingEventId = "move-1",
                        sourceLabel = "Shed A · Weaners",
                        destinationLabel = "Shed C · Growers",
                        priority = "High",
                        category = "Growth",
                        animalCount = 12,
                        approvedAtLabel = "2026-07-22",
                        actionStateLabel = "Approved",
                        primaryActionKey = "execute",
                    ),
                    ShiftingPendingRowUi(
                        shiftingEventId = "move-2",
                        sourceLabel = "Shed B · Kids",
                        destinationLabel = "Shed A · Weaners",
                        priority = "Low",
                        category = "Health",
                        animalCount = 1,
                        approvedAtLabel = "2026-07-22",
                        actionStateLabel = "Completed",
                        primaryActionKey = "none",
                    ),
                ),
            ),
        ).collectAsLazyPagingItems()
        ShiftingActionsScreen(
            state = ShiftingPendingUiState(
                dateIso = "2026-07-22",
                dateLabel = "Today · 22 Jul",
                statuses = listOf(
                    ShiftingPendingStatusUi("all", "All", true),
                    ShiftingPendingStatusUi("pending", "Pending", false),
                    ShiftingPendingStatusUi("authorized", "Approved", false),
                    ShiftingPendingStatusUi("rework", "Rework", false),
                    ShiftingPendingStatusUi("completed", "Completed", false),
                ),
                previousDates = listOf(
                    ShiftingPreviousDateUi("2026-07-21", "21 Jul", 3),
                    ShiftingPreviousDateUi("2026-07-20", "20 Jul", 1),
                ),
                lastSyncedAt = 0L,
            ),
            rows = rows,
        )
    }

    @Test
    fun birth_temporary_tag() = shot("birth_temporary_tag") {
        BirthDeathScreen(
            state = BirthDeathUiState(
                idKind = BIRTH_ID_KIND_TEMPORARY,
                tag = "TEMP-42",
                species = "goat",
                sex = "female",
                dob = "2026-07-20",
                canSubmit = true,
            ),
        )
    }

    /** The permanent-RFID path: both identifiers carry the Bluetooth scan toggle, the primary one
     *  mid-scan (brand-filled chip + listening caption). */
    @Test
    fun birth_permanent_rfid_scanning() = shot("birth_permanent_rfid_scanning") {
        BirthDeathScreen(
            state = BirthDeathUiState(
                idKind = BIRTH_ID_KIND_PERMANENT,
                tag = "",
                tag2 = "",
                scanningField = BirthDeathField.TAG,
                species = "goat",
                sex = "female",
                dob = "2026-07-20",
            ),
        )
    }

    /** Birth -> Tag the kid: the temp tag is retired by a scanned (or typed) permanent RFID. */
    @Test
    fun rfid_promote_scanning() = shot("rfid_promote_scanning") {
        RfidPromoteScreen(
            state = RfidPromoteUiState(
                goatId = "g-1",
                loading = false,
                displayId = "G-77",
                temporaryIdentifier = "TEMP-42",
                locationDisplay = "North Park / Shed A",
                rfidInput = "982000123456789",
                scanningField = RfidPromoteField.SECONDARY,
                canSubmit = true,
            ),
        )
    }

    @Test
    fun shifting_execute() = shot("shifting_execute") {
        ShiftingExecuteScreen(
            state = ShiftingExecuteUiState(
                shiftingEventId = "move-1",
                loading = false,
                sourceLabel = "Shed A · Weaners",
                destinationLabel = "Shed C · Growers",
                priority = "High",
                category = "Growth",
                animalCount = 3,
                animals = listOf(
                    ShiftingExecuteAnimalUi("g1", "CBE-0412", "RFID 004821"),
                    ShiftingExecuteAnimalUi("g2", "CBE-0419", "RFID 004839"),
                    ShiftingExecuteAnimalUi("g3", "CBE-0421", null),
                ),
                videoCaptured = true,
                videoMessage = "Video saved on this phone. It will upload automatically.",
                canComplete = true,
            ),
        )
    }

}
