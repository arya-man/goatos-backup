package sg.mesha.goatos.feature.feed

// telemetry:exempt purely presentational scaffold with no action of its own; the feed capture
// screens and their :app ViewModels own the AnalyticsEvents + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * The one anatomy every feed proof-capture destination renders.
 *
 * Feed's four capture screens (transport, distribution, packing, and the legacy complete path) each
 * hand-rolled their own chrome: a bare `‹ Back` Text pinned under the status bar, a raw title, and
 * full-bleed buttons on the page background. Every other capture surface in the app — Milk Prep,
 * Milk Feeding, Workflow detail, Health, Shifting — renders [MeshaScreenHeader] (Up affordance,
 * title/subtitle block, action slot) over card-contained sections, so Feed read as a different,
 * unfinished product the moment an operator tapped a row (maintainer report 2026-07-30).
 *
 * The screens keep their own state/event contracts; only the shell moved here, so the anatomy can
 * no longer drift screen by screen.
 *
 * @param title the shed/task the proof belongs to.
 * @param subtitle session · workflow, or whatever secondary identity the screen has (may be blank).
 * @param instruction what the operator must record, and who reviews it.
 */
@Composable
internal fun FeedCaptureScaffold(
    title: String,
    subtitle: String?,
    instruction: String,
    onBack: () -> Unit,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MeshaColors.Bg),
    ) {
        MeshaScreenHeader(
            title = title,
            subtitle = subtitle?.takeIf(String::isNotBlank),
            onBack = onBack,
        )
        Column(
            modifier = Modifier
                .fillMaxSize()
                .verticalScroll(rememberScrollState())
                .padding(horizontal = 16.dp)
                .padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Text(
                text = instruction,
                color = MeshaColors.Faint,
                fontSize = 12.sp,
            )
            content()
        }
    }
}

/**
 * One proof step, in the card the rest of the app puts a step in.
 *
 * @param hint optional qualifier rendered beside the title (e.g. "Photo or video").
 */
@Composable
internal fun FeedProofCard(
    title: String,
    hint: String? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .padding(12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(
                text = title,
                color = MeshaColors.Ink,
                fontSize = 13.sp,
                fontWeight = FontWeight.W700,
            )
            hint?.takeIf(String::isNotBlank)?.let {
                Text(text = "· $it", color = MeshaColors.Faint, fontSize = 12.sp)
            }
        }
        content()
    }
}
