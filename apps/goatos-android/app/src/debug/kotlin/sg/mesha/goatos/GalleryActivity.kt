package sg.mesha.goatos

import android.Manifest
import android.os.Bundle
import android.os.Build
import android.view.KeyEvent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Text
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
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
import sg.mesha.goatos.feature.scan.ScanEvent
import sg.mesha.goatos.feature.scan.ScanScreen
import sg.mesha.goatos.feature.sheds.ShedsScreen
import sg.mesha.goatos.feature.submit.SubmitScreen
import sg.mesha.goatos.feature.timetable.TimetableScreen
import sg.mesha.goatos.rfid.KeyboardWedgeRfidReader
import sg.mesha.goatos.ui.sampleAlertsState
import sg.mesha.goatos.ui.sampleCalendarState
import sg.mesha.goatos.ui.sampleCalendarWithCoverageState
import sg.mesha.goatos.ui.sampleLeadershipState
import sg.mesha.goatos.ui.sampleOverdueState
import sg.mesha.goatos.ui.sampleProfileState
import sg.mesha.goatos.ui.sampleRecordState
import sg.mesha.goatos.ui.sampleRescheduleState
import sg.mesha.goatos.ui.sampleRfidState
import sg.mesha.goatos.ui.sampleScanState
import sg.mesha.goatos.ui.sampleShedsState
import sg.mesha.goatos.ui.sampleSubmitState
import sg.mesha.goatos.ui.sampleTimetableState

/**
 * Debug-only screenshot harness. Renders a single screen with mock-matching sample fixture data
 * so device captures can be pixel-diffed against `mock/vaccination-mobile-mock.html` with no
 * backend or role/data confound. Launch from debug builds with:
 *
 *   adb shell am start -n sg.mesha.goatos.dev/sg.mesha.goatos.GalleryActivity -e screen calendar
 *
 * screen in login|calendar|calendar_coverage|sheds|scan|submit|overview|record|rfid|alerts|
 *   you|overdue|reschedule|timetable
 */
class GalleryActivity : ComponentActivity() {
    private lateinit var rfidReader: KeyboardWedgeRfidReader
    private lateinit var bleScanner: BleRfidScanner

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        requestBlePermissionsIfNeeded()
        rfidReader = KeyboardWedgeRfidReader(this)
        bleScanner = BleRfidScanner(this)
        val initialScreen = intent.getStringExtra("screen")?.lowercase() ?: "calendar"
        setContent {
            GoatOsTheme {
                ProvideAppLocale {
                    var screen by remember { mutableStateOf(initialScreen) }
                    Box(Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        when (screen) {
                            "login" -> LoginScreen(onSignInEmail = { _, _ -> }, onGoogle = {}, onForgotPassword = {})
                            "calendar" -> CalendarScreen(state = sampleCalendarState())
                            "calendar_coverage" -> CalendarScreen(state = sampleCalendarWithCoverageState())
                            "sheds" -> ShedsScreen(state = sampleShedsState())
                            "scan" -> ScanScreen(
                                state = sampleScanState(),
                                onEvent = { event ->
                                    if (event == ScanEvent.ReconnectReader) screen = "rfid"
                                },
                            )
                            "submit" -> SubmitScreen(state = sampleSubmitState())
                            "overview", "leadership" -> LeadershipScreen(state = sampleLeadershipState())
                            "record" -> RecordScreen(state = sampleRecordState())
                            "rfid" -> BleRfidScannerScreen(
                                scanner = bleScanner,
                                hidReader = rfidReader,
                                openBluetoothSettings = rfidReader::openSystemPairing,
                            )
                            "rfid_scan" -> DebugVaccinationRfidScanScreen(reader = rfidReader)
                            "rfid_hid" -> StandaloneHidRfidScreen(reader = rfidReader)
                            "rfid_mock" -> RfidScreen(state = sampleRfidState())
                            "alerts" -> AlertsScreen(state = sampleAlertsState())
                            "you", "profile" -> ProfileScreen(state = sampleProfileState())
                            "overdue" -> OverdueScreen(state = sampleOverdueState())
                            "reschedule" -> RescheduleScreen(state = sampleRescheduleState())
                            "timetable" -> TimetableScreen(state = sampleTimetableState())
                            else -> Text("unknown screen: $screen", color = Color.White)
                        }
                    }
                }
            }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (::rfidReader.isInitialized && rfidReader.onKeyEvent(event)) return true
        return super.onKeyDown(keyCode, event)
    }

    override fun onKeyUp(keyCode: Int, event: KeyEvent): Boolean {
        if (::rfidReader.isInitialized && rfidReader.onKeyEvent(event)) return true
        return super.onKeyUp(keyCode, event)
    }

    private fun requestBlePermissionsIfNeeded() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.S) return
        val missing = listOf(
            Manifest.permission.BLUETOOTH_SCAN,
            Manifest.permission.BLUETOOTH_CONNECT,
        ).filter { permission ->
            ContextCompat.checkSelfPermission(this, permission) != android.content.pm.PackageManager.PERMISSION_GRANTED
        }
        if (missing.isNotEmpty()) {
            ActivityCompat.requestPermissions(this, missing.toTypedArray(), 77)
        }
    }
}
