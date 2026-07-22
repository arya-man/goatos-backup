package sg.mesha.goatos.feature.feed

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

// Shared building blocks for the two Feed screens (Direction + Packing). Deliberately mirrors the
// Counts module's form kit so the two verticals read and behave identically; feature modules may not
// depend on one another, so the shape is duplicated rather than imported.

/** A backend-supplied option for one of the feed filter dropdowns (farm / shed / workflow). */
@Immutable
internal data class FeedDropdownOption(
    val key: String,
    val label: String,
)

/** A single-choice dropdown over a backend-supplied vocabulary. Only open/closed is local state;
 *  the options, labels, and selection all come from backend-owned state passed in. */
@Composable
internal fun FeedDropdownField(
    label: String,
    selectedLabel: String?,
    placeholder: String,
    options: List<FeedDropdownOption>,
    onSelect: (String) -> Unit,
    enabled: Boolean,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(text = label, color = MeshaColors.Muted, fontSize = 12.sp)
        Box(modifier = Modifier.fillMaxWidth()) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(12.dp))
                    .background(if (enabled) MeshaColors.Surf else MeshaColors.Surf3)
                    .border(1.dp, MeshaColors.Hair, RoundedCornerShape(12.dp))
                    .clickable(enabled = enabled) { expanded = true }
                    .padding(horizontal = 12.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = selectedLabel ?: placeholder,
                    color = if (selectedLabel != null) MeshaColors.Ink else MeshaColors.Faint,
                    fontSize = 14.sp,
                    fontWeight = if (selectedLabel != null) FontWeight.W600 else FontWeight.W400,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Icon(
                    imageVector = MeshaIcons.ChevronDown,
                    contentDescription = null,
                    tint = if (enabled) MeshaColors.Muted else MeshaColors.Faint,
                    modifier = Modifier.size(16.dp),
                )
            }
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
                modifier = Modifier.heightIn(max = 320.dp),
            ) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = {
                            Text(
                                text = option.label,
                                fontSize = 14.sp,
                                maxLines = 1,
                                overflow = TextOverflow.Ellipsis,
                                modifier = Modifier.widthIn(min = 180.dp),
                            )
                        },
                        onClick = {
                            expanded = false
                            onSelect(option.key)
                        },
                    )
                }
            }
        }
    }
}

/** One KPI tile (whole-scope total). */
@Composable
internal fun FeedStatTile(label: String, value: String, accent: androidx.compose.ui.graphics.Color, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf3)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(text = value, color = accent, fontSize = 18.sp, fontWeight = FontWeight.W800, maxLines = 1, overflow = TextOverflow.Ellipsis)
        Text(text = label, color = MeshaColors.Muted, fontSize = 11.sp, fontWeight = FontWeight.W600)
    }
}

/** A section caption line above a list. */
@Composable
internal fun FeedSectionCaption(text: String, modifier: Modifier = Modifier) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        modifier = modifier.padding(horizontal = 16.dp),
    )
}

