package sg.mesha.goatos.capture

// telemetry:exempt This dialog is rendered INSIDE the shared recorder overlay, which reports its
// lifecycle through onCameraEvent -> VideoCaptureLauncher (PROOF_CAMERA_BRIEFING_SHOWN /
// PROOF_CAMERA_BRIEFING_ACKNOWLEDGED). It performs no write of its own.

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.R
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * The operator-facing copy and picture for one [ProofPreRecordBriefing]. Resolved in ONE place so
 * the recorder cannot show a briefing the enum does not know, and so adding a briefing means
 * adding strings in all four locales (a missing `when` branch is a compile error, a missing
 * translation falls back to English through the ordinary resource lookup).
 */
internal data class PreRecordBriefingCopy(
    val title: Int,
    val body: Int,
    val confirm: Int,
    val imageDescription: Int,
)

internal fun preRecordBriefingCopy(briefing: ProofPreRecordBriefing): PreRecordBriefingCopy = when (briefing) {
    ProofPreRecordBriefing.WEIGHING_SCALE_ZERO -> PreRecordBriefingCopy(
        title = R.string.proof_camera_scale_zero_title,
        body = R.string.proof_camera_scale_zero_body,
        confirm = R.string.proof_camera_scale_zero_confirm,
        imageDescription = R.string.proof_camera_scale_zero_image_description,
    )
}

/**
 * Modal over the live camera preview. Nothing is being recorded while it is up: the operator can
 * see the scale in the viewfinder through the scrim, positions the empty scale so its display is
 * readable, and only [onConfirm] starts the clip. Taps on the scrim do nothing (they are consumed
 * so the recorder controls underneath cannot be reached), and the recorder's own back handler
 * still cancels the capture -- so the only two ways out are confirming (record) or cancelling
 * (no clip), and a weighing clip can never begin without the acknowledgement.
 *
 * Drawn INLINE in the recorder's box rather than as a nested platform dialog: the recorder already
 * lives in a full-screen dialog window, a second window on top of it would swallow the back
 * gesture, and an inline layer is what the screenshot tests can render.
 */
@Composable
internal fun PreRecordBriefingDialog(
    briefing: ProofPreRecordBriefing,
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .fillMaxSize()
            .background(MeshaColors.ViewfinderBackdrop.copy(alpha = 0.72f))
            // Consume every tap so nothing underneath (start/stop, torch, cancel) is reachable
            // while the briefing is up. No ripple: the scrim is not a control.
            .clickable(
                interactionSource = remember { MutableInteractionSource() },
                indication = null,
                onClick = {},
            )
            .windowInsetsPadding(WindowInsets.safeDrawing)
            .padding(horizontal = 24.dp),
        contentAlignment = Alignment.Center,
    ) {
        PreRecordBriefingSheet(briefing = briefing, onConfirm = onConfirm, onCancel = onCancel)
    }
}

/** The briefing card: title, picture, instruction, one primary confirm and a quiet cancel. Rendered
 *  on its own by the screenshot tests. */
@Composable
internal fun PreRecordBriefingSheet(
    briefing: ProofPreRecordBriefing,
    onConfirm: () -> Unit,
    onCancel: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val copy = preRecordBriefingCopy(briefing)
    val imageDescription = stringResource(copy.imageDescription)
    Surface(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        color = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
        tonalElevation = 0.dp,
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 22.dp, vertical = 22.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = stringResource(copy.title),
                color = MeshaColors.Ink,
                style = MeshaType.headerTitle,
                textAlign = TextAlign.Center,
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(16.dp))
            when (briefing) {
                ProofPreRecordBriefing.WEIGHING_SCALE_ZERO -> ScaleZeroIllustration(
                    modifier = Modifier
                        .size(width = 168.dp, height = 132.dp)
                        .semantics { contentDescription = imageDescription },
                )
            }
            Spacer(Modifier.height(14.dp))
            Text(
                text = stringResource(copy.body),
                color = MeshaColors.Ink.copy(alpha = 0.88f),
                style = MeshaType.body,
                textAlign = TextAlign.Center,
            )
            Spacer(Modifier.height(20.dp))
            Button(
                onClick = onConfirm,
                colors = ButtonDefaults.buttonColors(
                    containerColor = MeshaColors.Brand,
                    contentColor = MeshaColors.OnBrand,
                ),
                shape = RoundedCornerShape(12.dp),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(
                    text = stringResource(copy.confirm),
                    style = MeshaType.button,
                    modifier = Modifier.padding(vertical = 4.dp),
                )
            }
            Spacer(Modifier.height(4.dp))
            TextButton(onClick = onCancel, modifier = Modifier.fillMaxWidth()) {
                Text(
                    text = stringResource(R.string.proof_camera_scale_zero_cancel),
                    color = MeshaColors.Muted,
                    style = MeshaType.cta,
                )
            }
        }
    }
}

/**
 * A platform weighing scale with an empty pan and a display reading "0.0 kg". Drawn rather than
 * shipped as a bitmap so it is crisp at every density, themed with the design tokens, and keeps
 * its digits Latin regardless of locale (design-system rule: readings and tags never localise
 * digits).
 */
@Composable
internal fun ScaleZeroIllustration(modifier: Modifier = Modifier) {
    Box(modifier = modifier, contentAlignment = Alignment.Center) {
        Canvas(modifier = Modifier.fillMaxWidth().height(132.dp)) {
            val w = size.width
            val h = size.height
            val panHeight = h * 0.16f
            val panTop = h * 0.30f
            val bodyTop = panTop + panHeight + h * 0.04f
            val bodyHeight = h - bodyTop - h * 0.06f
            val panInset = w * 0.06f
            val bodyInset = w * 0.12f

            // Body of the scale.
            drawRoundRect(
                color = MeshaColors.Hair,
                topLeft = Offset(bodyInset, bodyTop),
                size = Size(w - 2 * bodyInset, bodyHeight),
                cornerRadius = CornerRadius(h * 0.08f, h * 0.08f),
            )
            drawRoundRect(
                color = MeshaColors.Muted.copy(alpha = 0.55f),
                topLeft = Offset(bodyInset, bodyTop),
                size = Size(w - 2 * bodyInset, bodyHeight),
                cornerRadius = CornerRadius(h * 0.08f, h * 0.08f),
                style = Stroke(width = 2.dp.toPx()),
            )
            // Display window on the body (the text is laid over it below).
            val displayInset = w * 0.22f
            drawRoundRect(
                color = MeshaColors.ViewfinderBackdrop,
                topLeft = Offset(displayInset, bodyTop + bodyHeight * 0.22f),
                size = Size(w - 2 * displayInset, bodyHeight * 0.56f),
                cornerRadius = CornerRadius(h * 0.05f, h * 0.05f),
            )
            drawRoundRect(
                color = MeshaColors.Brand.copy(alpha = 0.7f),
                topLeft = Offset(displayInset, bodyTop + bodyHeight * 0.22f),
                size = Size(w - 2 * displayInset, bodyHeight * 0.56f),
                cornerRadius = CornerRadius(h * 0.05f, h * 0.05f),
                style = Stroke(width = 1.5.dp.toPx()),
            )
            // Empty pan on top, with the stem joining it to the body.
            drawRect(
                color = MeshaColors.Muted.copy(alpha = 0.7f),
                topLeft = Offset(w / 2 - 3.dp.toPx(), panTop + panHeight),
                size = Size(6.dp.toPx(), bodyTop - panTop - panHeight),
            )
            drawRoundRect(
                color = MeshaColors.Line,
                topLeft = Offset(panInset, panTop),
                size = Size(w - 2 * panInset, panHeight),
                cornerRadius = CornerRadius(panHeight / 2, panHeight / 2),
            )
            drawRoundRect(
                color = MeshaColors.Ink.copy(alpha = 0.75f),
                topLeft = Offset(panInset, panTop),
                size = Size(w - 2 * panInset, panHeight),
                cornerRadius = CornerRadius(panHeight / 2, panHeight / 2),
                style = Stroke(width = 2.dp.toPx()),
            )
            // Feet.
            val footY = h - h * 0.06f
            drawRoundRect(
                color = MeshaColors.Muted.copy(alpha = 0.7f),
                topLeft = Offset(bodyInset + w * 0.05f, footY),
                size = Size(w * 0.10f, h * 0.05f),
                cornerRadius = CornerRadius(4f, 4f),
            )
            drawRoundRect(
                color = MeshaColors.Muted.copy(alpha = 0.7f),
                topLeft = Offset(w - bodyInset - w * 0.15f, footY),
                size = Size(w * 0.10f, h * 0.05f),
                cornerRadius = CornerRadius(4f, 4f),
            )
        }
        // The reading, centred on the display window drawn above (window spans the body's 22%..78%
        // band, which sits in the lower half of the illustration).
        Text(
            text = stringResource(R.string.proof_camera_scale_zero_reading),
            color = MeshaColors.Brand,
            fontFamily = FontFamily.Monospace,
            fontWeight = FontWeight.W800, // design-system:ignore: fixed scale display glyph, matched to committed briefing goldens
            fontSize = 22.sp, // design-system:ignore: fixed scale display glyph, matched to committed briefing goldens
            letterSpacing = 1.sp,
            modifier = Modifier
                .align(Alignment.Center)
                .padding(top = 58.dp),
        )
    }
}
