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
import sg.mesha.goatos.feature.verify.VerifyWeightCorrection

/**
 * BLIND WEIGHING VERIFICATION, as the verifier's phone renders it before she types anything
 * (maintainer decision 2026-09-21).
 *
 * Two facts are pinned by this golden, and both were real defects:
 *
 *  1. THE SUBJECT CARRIES NO WEIGHT. She reads the scale off the video herself, and her reading
 *     becomes the recorded weight; being shown the operator's number first would make her a rubber
 *     stamp on it.
 *  2. APPROVE IS VISIBLY GREYED. `enabled` used to reach only .clickable(), so the held Approve
 *     rendered at full strength and silently swallowed the tap -- on this screen that is the COMMON
 *     state, and a live-looking button that does nothing reads as a frozen app. The admin-web drawer
 *     greys its Accept here, and the two surfaces must not disagree about whether an action is
 *     available. Reject stays FULL strength beside it: a clip she cannot read is exactly the one
 *     that must go back, so rejection is never held on the number.
 */
class VerifyBlindWeighingApproveScreenshotTest {

    @get:Rule
    val paparazzi = Paparazzi(deviceConfig = DeviceConfig.PIXEL_6)

    @Test
    fun blind_weighing_holds_approve_until_the_verifier_types_a_weight() {
        // No media in the fixture on purpose: the video player spawns a HandlerThread layoutlib
        // cannot run. The subject of this golden is the measurement card and the decision row.
        val media = emptyList<VerifyMediaItem>()
        val correction = VerifyWeightCorrection(
            refType = "weighing_shed_observation",
            observationId = "obs-1",
            // Backend-owned copy, rendered verbatim -- the phone words none of it.
            title = "Record the weight",
            help = "Enter the weight you can see in the video. Your reading becomes the recorded weight.",
            valueLabel = "Weight (kg)",
            submitLabel = "Save weight",
            requiredForApprove = true,
        )
        paparazzi.snapshot(name = "verify_blind_weighing_approve_held") {
            GoatOsTheme {
                ProvideAppLocale {
                    Box(androidx.compose.ui.Modifier.fillMaxSize().background(MeshaColors.Bg)) {
                        VerifyDetailScreen(
                            state = VerifyDetailUiState(
                                itemId = "item-weighing-1",
                                category = "weighing_proof",
                                categoryLabel = "Weighing proof",
                                // Pen and head count, and NO WEIGHT. The frozen head count stays:
                                // it is snapshotted from the herd register at submit, so it is not
                                // a number the operator typed and it anchors nobody.
                                subjectLabel = "Godel 2 - Part 2 · 38 goats",
                                media = media,
                                context = listOf(
                                    VerifyContextRow(VerifyContextKind.PARK, "Channapatna"),
                                    VerifyContextRow(VerifyContextKind.OPERATOR, "Amit Kumar"),
                                    VerifyContextRow(VerifyContextKind.CAPTURED_AT, "2026-09-14T09:50:00Z"),
                                ),
                                statusTone = VerifyTone.PENDING,
                                isApproveEnabled = true,
                                isRejectEnabled = true,
                                hasLoadedOnce = true,
                                entries = listOf(
                                    VerifyDetailEntryUiState(
                                        itemId = "item-weighing-1",
                                        weightCorrection = correction,
                                        subjectLabel = "Godel 2 - Part 2 · 38 goats",
                                        media = media,
                                        statusTone = VerifyTone.PENDING,
                                        // BOTH true on the entry: the hold comes from the EMPTY
                                        // measurement field, not from the item being undecidable.
                                        // That is the whole point -- an entry that is perfectly
                                        // decidable still greys Approve until she reads the scale.
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
