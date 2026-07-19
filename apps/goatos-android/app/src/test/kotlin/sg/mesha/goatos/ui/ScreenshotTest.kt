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
import sg.mesha.goatos.feature.auth.LoginScreen
import sg.mesha.goatos.feature.calendar.CalendarScreen
import sg.mesha.goatos.feature.leadership.LeadershipScreen
import sg.mesha.goatos.feature.leadership.OverdueScreen
import sg.mesha.goatos.feature.leadership.RescheduleScreen
import sg.mesha.goatos.feature.leadership.VerificationClosureRow
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
    fun sheds() = shot("sheds") { ShedsScreen(state = sampleShedsState()) }

    @Test
    fun scan() = shot("scan") { ScanScreen(state = sampleScanState()) }

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
    fun overview() = shot("overview") { LeadershipScreen(state = sampleLeadershipState()) }

    @Test
    fun record() = shot("record") { RecordScreen(state = sampleRecordState()) }

    @Test
    fun rfid() = shot("rfid") { RfidScreen(state = sampleRfidState()) }

    @Test
    fun alerts() = shot("alerts") { AlertsScreen(state = sampleAlertsState()) }

    @Test
    fun you() = shot("you") { ProfileScreen(state = sampleProfileState()) }

    @Test
    fun overdue() = shot("overdue") { OverdueScreen(state = sampleOverdueState()) }

    @Test
    fun reschedule() = shot("reschedule") { RescheduleScreen(state = sampleRescheduleState()) }

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
            onToggle = { _, _ -> }, onText = { _, _ -> }, onScan = {}, onPick = { _, _ -> }, onCaptureVideo = {}, onSubmit = {},
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
                goatProofSynced = 40,
                goatProofUploading = 0,
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
    fun vaccination_leadership_close() = shot("vaccination_leadership_close") {
        LeadershipScreen(
            state = sampleLeadershipState().copy(
                todaySheds = emptyList(),
                backlog = emptyList(),
                needsDecision = emptyList(),
                verificationClosures = listOf(
                    VerificationClosureRow(
                        submissionId = "drive-1",
                        title = "Coimbatore · Vaccination drive",
                        subtitle = "18 goats verified · Gandhi 1 · Arun",
                    ),
                ),
                coverageByParkTitle = null,
                coverageByPark = emptyList(),
            ),
        )
    }
}
