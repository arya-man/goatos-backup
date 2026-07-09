package sg.mesha.goatos.core.designsystem.component

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Design-system components. Every screen composes these instead of re-styling surfaces,
 * pills, buttons, inputs, and icons. Colour/spacing/type all come from [MeshaColors] /
 * [MeshaDimens] / [MeshaType] — no hex or raw dp/sp at the call site.
 */

/** Semantic tone for pills/badges — maps to the mock's `.pill.*` variants. */
enum class MeshaTone { Ok, Warn, Danger, Teal, Purple, Muted }

private fun toneBg(tone: MeshaTone): Color = when (tone) {
    MeshaTone.Ok -> MeshaColors.OkX
    MeshaTone.Warn -> MeshaColors.WarnX
    MeshaTone.Danger -> MeshaColors.DangerX
    MeshaTone.Teal -> MeshaColors.TealX
    MeshaTone.Purple -> MeshaColors.PurpleX
    MeshaTone.Muted -> MeshaColors.Surf3
}

private fun toneFg(tone: MeshaTone): Color = when (tone) {
    MeshaTone.Ok -> MeshaColors.BrandD
    MeshaTone.Warn -> MeshaColors.Warn
    MeshaTone.Danger -> MeshaColors.Danger
    MeshaTone.Teal -> MeshaColors.Teal
    MeshaTone.Purple -> MeshaColors.Purple
    MeshaTone.Muted -> MeshaColors.Muted
}

/** `.pill` — rounded status chip. */
@Composable
fun MeshaStatusPill(label: String, tone: MeshaTone, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .clip(MeshaDimens.pill)
            .background(toneBg(tone))
            .padding(horizontal = 10.dp, vertical = 4.dp),
    ) {
        Text(text = label, color = toneFg(tone), style = MeshaType.pill, maxLines = 1)
    }
}

/** `.card` — surf surface, hair border, r18, optional 3dp brand left accent (`.evt`). */
@Composable
fun MeshaCard(
    modifier: Modifier = Modifier,
    accent: Color? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusCard))
            .then(if (accent != null) Modifier.drawBehind { drawRect(color = accent, size = size.copy(width = 3.dp.toPx())) } else Modifier)
            .padding(start = MeshaDimens.gutter, top = 15.dp, end = MeshaDimens.gutter, bottom = 15.dp),
        content = content,
    )
}

/** `.btn` — gradient primary; disabled falls to `.btn.block` (surf + hair). */
@Composable
fun MeshaPrimaryButton(
    text: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val shape = RoundedCornerShape(MeshaDimens.radiusButton)
    val base = modifier.fillMaxWidth().heightIn(min = MeshaDimens.minTapButton).clip(shape)
    val styled = if (enabled) {
        base.background(MeshaColors.BrandGradient, shape)
    } else {
        base.background(MeshaColors.Surf, shape).border(MeshaDimens.hairline, MeshaColors.Hair, shape)
    }
    Box(
        modifier = styled.clickable(enabled = enabled, onClick = onClick).padding(15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = text, color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint, style = MeshaType.button)
    }
}

/** `.vhead .ib` — 38dp surf2 rounded icon button. */
@Composable
fun MeshaIconButton(
    icon: ImageVector,
    contentDescription: String?,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .size(MeshaDimens.iconButton)
            .clip(RoundedCornerShape(MeshaDimens.radiusIcon))
            .background(MeshaColors.Surf2)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = contentDescription, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd))
    }
}

/** `.inp` / `.langbtn` shell — surf, hair border, r14, 13×15 padding, min-height 50, 10px gap. */
@Composable
fun MeshaInputShell(
    modifier: Modifier = Modifier,
    onClick: (() -> Unit)? = null,
    content: @Composable RowScope.() -> Unit,
) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
            .background(MeshaColors.Surf)
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusInput))
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .heightIn(min = MeshaDimens.minTapRow)
            .padding(horizontal = 15.dp, vertical = 13.dp),
        content = content,
    )
}

/** `.dl` — uppercase faint section label. */
@Composable
fun MeshaSectionLabel(text: String, modifier: Modifier = Modifier) {
    Text(text = text.uppercase(), color = MeshaColors.Faint, style = MeshaType.sectionLabel, modifier = modifier)
}
