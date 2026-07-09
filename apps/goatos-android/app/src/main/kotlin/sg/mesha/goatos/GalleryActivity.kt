package sg.mesha.goatos

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
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
import sg.mesha.goatos.ui.sampleAlertsState
import sg.mesha.goatos.ui.sampleCalendarState
import sg.mesha.goatos.ui.sampleLeadershipState
import sg.mesha.goatos.ui.sampleOverdueState
import sg.mesha.goatos.ui.sampleProfileState
import sg.mesha.goatos.ui.sampleRecordState
import sg.mesha.goatos.ui.sampleRescheduleState
import sg.mesha.goatos.ui.sampleRfidState
import sg.mesha.goatos.ui.sampleScanState
import sg.mesha.goatos.ui.sampleShedsState
import sg.mesha.goatos.ui.sampleSubmitState

/**
 * DEV-ONLY screenshot harness. Renders any single screen with its mock-matching sample
 * fixture (operator data — Arun Kumar, RFID connected, Due-now, etc.) so a device capture
 * can be pixel-diffed against `mock/vaccination-mobile-mock.html` with NO backend and NO
 * role/data confound. Not in the launcher; launch with:
 *
 *   adb shell am start -n sg.mesha.goatos.dev/sg.mesha.goatos.GalleryActivity -e screen calendar
 *
 * screen ∈ login|calendar|sheds|scan|submit|overview|record|rfid|alerts|you|overdue|reschedule
 */
class GalleryActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // DEV-ONLY. Exported for adb launch, so hard-gate to debug builds — a release build
        // must never expose a launchable activity to other apps.
        if (!BuildConfig.DEBUG) {
            finish()
            return
        }
        enableEdgeToEdge()
        val screen = intent.getStringExtra("screen")?.lowercase() ?: "calendar"
        setContent {
            GoatOsTheme {
                Box(Modifier.fillMaxSize().background(Color(0xFF0B100D))) {
                    when (screen) {
                        "login" -> LoginScreen(onSignIn = {})
                        "calendar" -> CalendarScreen(state = sampleCalendarState())
                        "sheds" -> ShedsScreen(state = sampleShedsState())
                        "scan" -> ScanScreen(state = sampleScanState())
                        "submit" -> SubmitScreen(state = sampleSubmitState())
                        "overview", "leadership" -> LeadershipScreen(state = sampleLeadershipState())
                        "record" -> RecordScreen(state = sampleRecordState())
                        "rfid" -> RfidScreen(state = sampleRfidState())
                        "alerts" -> AlertsScreen(state = sampleAlertsState())
                        "you", "profile" -> ProfileScreen(state = sampleProfileState())
                        "overdue" -> OverdueScreen(state = sampleOverdueState())
                        "reschedule" -> RescheduleScreen(state = sampleRescheduleState())
                        else -> Text("unknown screen: $screen", color = Color.White)
                    }
                }
            }
        }
    }
}
