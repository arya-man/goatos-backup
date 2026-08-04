// telemetry:exempt presentational wizard chrome (step rail, header, footer buttons) — it holds
// no state and performs no action of its own; every step transition and submit it renders is
// recorded by WeighingPlanWizardViewModel, which is where the action actually happens.
package sg.mesha.goatos.feature.weighing.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * The wizard's progress indicator: one segment per step, filled up to the step being shown.
 *
 * A NEW component because nothing in the design system shows position inside a multi-step flow —
 * the existing chips and pills all express filters or state, never progress through a sequence.
 */
@Composable
fun WeighingStepper(
    stepCount: Int,
    currentIndex: Int,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 18.dp, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        repeat(stepCount) { index ->
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(3.dp)
                    .clip(RoundedCornerShape(20.dp))
                    .background(if (index <= currentIndex) MeshaColors.Brand else MeshaColors.Surf3),
            )
        }
    }
}

/**
 * The wizard's sticky bottom bar: a context line naming what is still missing or already chosen,
 * then the actions for this step.
 *
 * A NEW component because it must stay reachable above a list of a hundred buckets; every existing
 * button row in this feature scrolls with its content.
 */
@Composable
fun WeighingWizardActionBar(
    contextLine: String,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg)
            .padding(horizontal = 16.dp, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(
            text = contextLine,
            color = MeshaColors.Muted,
            style = MeshaType.caption,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) { content() }
    }
}

/** The wizard's primary action. Disabled renders as a reason-carrying dead button, never hidden. */
@Composable
fun WeighingWizardPrimaryButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 52.dp)
            .clip(RoundedCornerShape(15.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 16.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/** The wizard's secondary action, matching the ghost buttons used across the weighing surfaces. */
@Composable
fun WeighingWizardGhostButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .heightIn(min = 52.dp)
            .clip(RoundedCornerShape(15.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(15.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 12.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.Ink else MeshaColors.Faint,
            style = MeshaType.button,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
    }
}

/**
 * A single-line search field with a clear affordance.
 *
 * A NEW component: the weighing feature and the design system have text inputs for capture and for
 * forms, but no search box that can be cleared in one tap — which a list of buckets needs.
 */
@Composable
fun WeighingSearchField(
    value: String,
    placeholder: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 50.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf)
            .border(1.dp, MeshaColors.Hair, RoundedCornerShape(14.dp))
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Box(modifier = Modifier.weight(1f)) {
            if (value.isEmpty()) {
                Text(text = placeholder, color = MeshaColors.Faint, style = MeshaType.body)
            }
            BasicTextField(
                value = value,
                onValueChange = onValueChange,
                singleLine = true,
                textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
                cursorBrush = SolidColor(MeshaColors.Brand),
                modifier = Modifier.fillMaxWidth(),
            )
        }
        if (value.isNotEmpty()) {
            Text(
                text = "✕",
                color = MeshaColors.Muted,
                style = MeshaType.bodyStrong,
                textAlign = TextAlign.Center,
                modifier = Modifier
                    .minimumInteractiveComponentSize()
                    .clip(RoundedCornerShape(999.dp))
                    .clickable(role = Role.Button) { onValueChange("") },
            )
        }
    }
}
