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
import sg.mesha.goatos.feature.profile.AlertsScreen
import sg.mesha.goatos.feature.profile.ProfileScreen
import sg.mesha.goatos.feature.profile.RfidScreen
import sg.mesha.goatos.feature.record.RecordScreen
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.timetable.TimetableScreen

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
    fun login() = shot("login") { LoginScreen(onSignIn = {}) }

    @Test
    fun calendar() = shot("calendar") { CalendarScreen(state = sampleCalendarState()) }

    @Test
    fun sheds() = shot("sheds") { ShedsScreen(state = sampleShedsState()) }

    @Test
    fun scan() = shot("scan") { ScanScreen(state = sampleScanState()) }

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
}
