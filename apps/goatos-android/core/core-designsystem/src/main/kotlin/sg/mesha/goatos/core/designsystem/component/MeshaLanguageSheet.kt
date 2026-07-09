package sg.mesha.goatos.core.designsystem.component

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.locale.SUPPORTED_LANGUAGES
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Language picker bottom sheet (mock `ovl-lang`). Lists every [SUPPORTED_LANGUAGES] option in
 * its own script, checks the current one, and calls [onSelect] with the chosen BCP-47 tag.
 * The caller applies it app-wide via AppLocaleState — the whole tree recomposes in the new
 * locale. Shared by Login and You/Settings.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MeshaLanguageSheet(
    currentTag: String,
    onSelect: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(
        onDismissRequest = onDismiss,
        containerColor = MeshaColors.Surf,
        contentColor = MeshaColors.Ink,
    ) {
        Column(Modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter, vertical = MeshaDimens.space2)) {
            Text(
                text = "App language".uppercase(),
                color = MeshaColors.Faint,
                style = MeshaType.sectionLabel,
                modifier = Modifier.padding(bottom = MeshaDimens.space3),
            )
            SUPPORTED_LANGUAGES.forEach { lang ->
                val selected = lang.tag == currentTag
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    modifier = Modifier
                        .fillMaxWidth()
                        .heightIn(min = MeshaDimens.minTapRow)
                        .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                        .background(if (selected) MeshaColors.OkX else MeshaColors.Surf2)
                        .clickable { onSelect(lang.tag) }
                        .padding(horizontal = 15.dp, vertical = 13.dp),
                ) {
                    Text(
                        text = lang.label,
                        color = if (selected) MeshaColors.BrandD else MeshaColors.Ink,
                        style = MeshaType.body.copy(fontWeight = MeshaType.bodyStrong.fontWeight),
                        modifier = Modifier.weight(1f),
                    )
                    if (selected) {
                        Icon(MeshaIcons.Check, contentDescription = "Selected", tint = MeshaColors.Brand, modifier = Modifier.size(MeshaDimens.iconMd))
                    }
                }
                Spacer(Modifier.size(MeshaDimens.space2))
            }
            Spacer(Modifier.size(MeshaDimens.space5))
        }
    }
}
