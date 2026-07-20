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
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.scan.RosterRow
import sg.mesha.goatos.feature.scan.ScanListSheet
import sg.mesha.goatos.feature.scan.ScanStatus

/** Typical phone-width guard for wrapping/target regressions in the goat-proof action row. */
class ScanProofCompactScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(
        // Pixel 2 density makes 1080 px roughly 411 dp: wide enough to reproduce the
        // physical-phone layout that was incorrectly treated as an expanded row.
        deviceConfig = DeviceConfig.PIXEL_2.copy(screenWidth = 1080, screenHeight = 2400),
    )

    @Test
    fun allProofStatesAtCompactWidth() {
        paparazzi.snapshot(name = "vaccination_scan_proof_compact") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        ScanListSheet(
                            title = "Gandhi 1 · goat proof",
                            rows = proofRows(),
                            captureEnabled = true,
                        )
                    }
                }
            }
        }
    }
}

/** Expanded-width guard: proof state and camera action remain one aligned row. */
class ScanProofExpandedScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_TABLET)

    @Test
    fun allProofStatesAtExpandedWidth() {
        paparazzi.snapshot(name = "vaccination_scan_proof_expanded") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        ScanListSheet(
                            title = "Gandhi 1 · goat proof",
                            rows = proofRows(),
                            captureEnabled = true,
                        )
                    }
                }
            }
        }
    }
}

private fun proofRows(): List<RosterRow> = ProofUploadStatus.entries.mapIndexed { index, status ->
    RosterRow(
        primaryTag = "RFID 0048${21 + index}",
        secondaryTag = "Ear tag ${118 + index}",
        vaccineLabel = "Enterotoxaemia · Tetanus toxoid",
        status = ScanStatus.DONE,
        goatId = "goat-$index",
        proofClipCount = if (status == ProofUploadStatus.MISSING) 0 else 1,
        proofUploadStatus = status,
    )
}
