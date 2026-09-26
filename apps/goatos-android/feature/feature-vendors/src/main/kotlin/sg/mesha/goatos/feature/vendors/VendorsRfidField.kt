package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure presentational input; the state holder that owns the reader wires analytics.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens

/**
 * A tag field with the Bluetooth reader chip in its trailing slot: the chip switches the
 * keyboard-wedge reader on and off, and the SAME field takes a hand-typed tag when the reader is
 * not to hand (flat battery, unpaired, a tag that will not read). Both paths run the identical
 * lookup, so neither can reach validation the other cannot. Same shape as the Counts form's field.
 */
@Composable
internal fun VendorsRfidField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    scanning: Boolean,
    onToggleScan: () -> Unit,
    modifier: Modifier = Modifier,
    supporting: String? = null,
    error: String? = null,
) {
    VendorsTextField(
        value = value,
        onValueChange = onValueChange,
        label = label,
        modifier = modifier,
        error = error,
        supporting = supporting,
        trailing = {
            // 48dp touch target around a 36dp chip: the person taps this wearing gloves, and the
            // a11y minimum is the TOUCH area, not the painted one.
            Box(
                modifier = Modifier
                    .padding(end = 4.dp)
                    .size(48.dp)
                    .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                    .clickable(role = Role.Switch, onClick = onToggleScan),
                contentAlignment = Alignment.Center,
            ) {
                Box(
                    modifier = Modifier
                        .size(36.dp)
                        .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                        .background(if (scanning) MeshaColors.Brand else MeshaColors.Surf),
                    contentAlignment = Alignment.Center,
                ) {
                    Icon(
                        imageVector = MeshaIcons.Bluetooth,
                        contentDescription = if (scanning) STOP_SCAN else START_SCAN,
                        tint = if (scanning) MeshaColors.OnBrand else MeshaColors.Muted,
                        modifier = Modifier.size(MeshaDimens.iconMd),
                    )
                }
            }
        },
    )
}

private const val START_SCAN = "Scan tag with the reader"
private const val STOP_SCAN = "Stop the reader"
