package sg.mesha.goatos.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import app.cash.paparazzi.DeviceConfig
import app.cash.paparazzi.Paparazzi
import org.junit.Rule
import org.junit.Test
import sg.mesha.goatos.core.designsystem.theme.GoatOsTheme
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.locale.ProvideAppLocale
import sg.mesha.goatos.feature.verify.VerifyContextKind
import sg.mesha.goatos.feature.verify.VerifyContextRow
import sg.mesha.goatos.feature.verify.VerifyDetailEntryUiState
import sg.mesha.goatos.feature.verify.VerifyDetailScreen
import sg.mesha.goatos.feature.verify.VerifyDetailUiState
import sg.mesha.goatos.feature.verify.VerifyMediaItem
import sg.mesha.goatos.feature.verify.VerifyTone

/**
 * Pins the milk-preparation verify detail WITH the producer's entered-quantity context rows
 * ("Milk used", "Citric acid") — the backend now attaches what the operator ENTERED so the
 * verifier judges each quantity video against a claimed number instead of only confirming a
 * clip exists. Backend labels render verbatim through the generic backendLabel path.
 */
class VerifyMilkPreparationContextScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun milk_preparation_verify_detail_shows_entered_quantities() {
        // No media in the fixture on purpose: the video player spawns a HandlerThread that
        // layoutlib cannot run (NoSuchMethodError: Thread.setPosixNicenessInternal), and the
        // subject of this golden is the CONTEXT CARD, not the player.
        val media = emptyList<VerifyMediaItem>()
        paparazzi.snapshot(name = "verify_milk_preparation_context") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        VerifyDetailScreen(
                            state = VerifyDetailUiState(
                                itemId = "item-milk-1",
                                category = "milk_preparation",
                                categoryLabel = "Milk preparation",
                                subjectLabel = "Milk preparation · attempt 1 · 5 step videos",
                                media = media,
                                context = listOf(
                                    VerifyContextRow(VerifyContextKind.PARK, "Coimbatore"),
                                    VerifyContextRow(VerifyContextKind.OPERATOR, "Amit Kumar"),
                                    VerifyContextRow(VerifyContextKind.CAPTURED_AT, "2026-09-03T09:52:00Z"),
                                    VerifyContextRow(
                                        kind = VerifyContextKind.RAISED_NOTE,
                                        value = "20.5 L (goat 8 L + UHT 12.5 L)",
                                        backendLabel = "Milk used",
                                    ),
                                    VerifyContextRow(
                                        kind = VerifyContextKind.RAISED_NOTE,
                                        value = "112.75 g",
                                        backendLabel = "Citric acid",
                                    ),
                                ),
                                statusTone = VerifyTone.PENDING,
                                isApproveEnabled = true,
                                isRejectEnabled = true,
                                hasLoadedOnce = true,
                                entries = listOf(
                                    VerifyDetailEntryUiState(
                                        itemId = "item-milk-1",
                                        subjectLabel = "Milk preparation · attempt 1 · 5 step videos",
                                        media = media,
                                        statusTone = VerifyTone.PENDING,
                                        isApproveEnabled = true,
                                        isRejectEnabled = true,
                                    ),
                                ),
                            ),
                        )
                    }
                }
            }
        }
    }
}
