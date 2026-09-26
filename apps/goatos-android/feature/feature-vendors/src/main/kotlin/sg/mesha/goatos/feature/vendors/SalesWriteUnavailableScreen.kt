package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer with no control but Back; the hosting route in :app
// decides it is shown from the backend's sales flags and the Sales list/detail VMs own analytics.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * A hosted Sales write route (Record sale, Tag animals) opened by someone who may only read Sales
 * -- through a deep link or a back stack from before their access changed. It says so, in farm
 * words, with Back, and mounts NO form: no field, no Save, nothing the server would refuse.
 */
@Composable
fun SalesWriteUnavailableScreen(
    title: String,
    message: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(title = title, onBack = onBack)
        Text(
            text = message,
            color = MeshaColors.Muted,
            style = MeshaType.body,
            modifier = Modifier
                .padding(horizontal = MeshaDimens.gutter, vertical = 12.dp)
                .fillMaxWidth()
                .clip(RoundedCornerShape(14.dp))
                .background(MeshaColors.Surf)
                .padding(14.dp),
        )
    }
}
