package sg.mesha.goatos.core.ui.filters

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.R

// telemetry:exempt Pure presentational picker; the hosting screen tracks the applied filter.

/**
 * The Pen control's sheet: "All pens" then every pen the backend offered, grouped by park, each
 * with its count of work in the window. Picking a row applies at once -- one tap, no Apply --
 * because a pen is a single choice, unlike a date range that needs two.
 *
 * The list is the backend's whole-window vocabulary (bounded by the farm's pen catalog), never
 * a pen set derived from the loaded page.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorklistPenSheet(
    options: List<WorklistPenOption>,
    selected: WorklistPen?,
    onSelect: (WorklistPen?) -> Unit,
    onDismiss: () -> Unit,
) {
    val grouped = remember(options) { options.groupBy { it.parkName } }
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
    ) {
        Column(modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 24.dp)) {
            Text(text = stringResource(R.string.filters_pen_label), style = MeshaType.headerTitle, color = MeshaColors.Ink)
            Spacer(Modifier.height(12.dp))
            LazyColumn(
                modifier = Modifier.fillMaxWidth().heightIn(max = 480.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                item(key = "all-pens") {
                    PenRow(
                        label = stringResource(R.string.filters_all_pens),
                        trailing = "",
                        selected = selected == null,
                        onClick = { onSelect(null) },
                    )
                }
                if (options.isEmpty()) {
                    item(key = "no-pens") {
                        Text(
                            text = stringResource(R.string.filters_pen_none),
                            style = MeshaType.caption,
                            color = MeshaColors.Muted,
                            modifier = Modifier.padding(vertical = 8.dp),
                        )
                    }
                }
                grouped.forEach { (park, pens) ->
                    if (park.isNotBlank()) {
                        item(key = "park-$park") {
                            Text(
                                text = park.uppercase(),
                                style = MeshaType.sectionLabel,
                                color = MeshaColors.Muted,
                                modifier = Modifier.padding(top = 6.dp, bottom = 2.dp),
                            )
                        }
                    }
                    items(pens, key = { "pen-${it.pen.key}" }) { option ->
                        PenRow(
                            label = option.label,
                            trailing = option.count.toString(),
                            selected = selected?.key == option.pen.key,
                            onClick = { onSelect(option.pen) },
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun PenRow(label: String, trailing: String, selected: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .height(52.dp)
            .clip(RoundedCornerShape(14.dp))
            .background(if (selected) MeshaColors.BrandTint else MeshaColors.Surf2)
            .border(1.dp, if (selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(14.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = label, style = MeshaType.bodyStrong, color = MeshaColors.Ink, modifier = Modifier.weight(1f), maxLines = 1)
        if (trailing.isNotBlank()) {
            Text(text = trailing, style = MeshaType.caption, color = MeshaColors.Muted)
        }
        if (selected) {
            Spacer(Modifier.size(8.dp))
            Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(18.dp))
        }
    }
}
