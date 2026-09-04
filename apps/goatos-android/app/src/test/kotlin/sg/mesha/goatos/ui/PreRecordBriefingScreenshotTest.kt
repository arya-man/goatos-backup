package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.After
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.capture.PreRecordBriefingSheet
import sg.mesha.goatos.capture.ProofPreRecordBriefing
import sg.mesha.goatos.core.designsystem.locale.AppLocaleState
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * The weighing "show the scale at 0 kg" briefing, in every language the app ships. The maintainer
 * asked for it "in all four languages", and Kannada/Telugu/Hindi copy is wider than English, so
 * each locale gets its own golden: a translation that wraps badly or clips the confirm button is a
 * regression a JVM test cannot see.
 *
 * Only the SHEET is snapshotted (not the Dialog window): layoutlib does not render platform dialog
 * windows, and the sheet is the whole of what the operator reads.
 */
class PreRecordBriefingScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @After
    fun resetLocale() = AppLocaleState.set("en")

    @Test
    fun weighingScaleZeroBriefing_en() = shot("en")

    @Test
    fun weighingScaleZeroBriefing_hi() = shot("hi")

    @Test
    fun weighingScaleZeroBriefing_kn() = shot("kn")

    @Test
    fun weighingScaleZeroBriefing_te() = shot("te")

    private fun shot(locale: String) {
        // Two switches, because they answer different questions: AppLocaleState is what the app
        // itself flips (ProvideAppLocale re-reads strings through an override Configuration), and
        // Paparazzi's own locale is what layoutlib resolves `values-<tag>/` against. Layoutlib
        // ignores the app's runtime override, so without the second switch every golden would be
        // English regardless of the tag -- which is exactly what the first recording produced.
        AppLocaleState.set(locale)
        paparazzi.unsafeUpdateConfig(deviceConfig = DeviceConfig.PIXEL_6.copy(locale = locale))
        paparazzi.snapshot(name = "weighing_scale_zero_briefing_$locale") {
            GoatOsTheme {
                ProvideAppLocale {
                    // The viewfinder backdrop stands in for the live camera preview the dialog
                    // floats over in production.
                    Box(
                        Modifier.fillMaxSize().background(MeshaColors.ViewfinderBackdrop),
                        contentAlignment = Alignment.Center,
                    ) {
                        PreRecordBriefingSheet(
                            briefing = ProofPreRecordBriefing.WEIGHING_SCALE_ZERO,
                            onConfirm = {},
                            onCancel = {},
                            modifier = Modifier.padding(horizontal = 24.dp),
                        )
                    }
                }
            }
        }
    }
}
