package sg.mesha.goatos.feature.counts

// telemetry:exempt purely presentational form primitives with no user action of their own; the
// owning screens' ViewModels (BirthDeathViewModel / ShiftingViewModel) wire analytics + Crashlytics.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Shared form chrome for the two Counts WRITE screens. Presentational only — no state is held
 * here, no validation decision is made here.
 *
 * Both write screens follow the same offline contract: the operator's submit does NOT wait on the
 * network. It writes to the durable outbox and returns immediately, and this chrome renders the
 * resulting [CountsWriteStatus] so "queued offline" reads as success, not as a pending spinner
 * that never resolves.
 */

/**
 * Lifecycle of one queued write, as the owning ViewModel derives it from the outbox row.
 *
 * [FAILED] is a TERMINAL server rejection (a validation error the operator must correct) or an
 * exhausted retry budget — never a transient offline blip, which stays [QUEUED] and drains later.
 */
enum class CountsWriteStatus { IDLE, QUEUED, SYNCED, FAILED }

/**
 * Result banner state for a submitted write.
 *
 * [message] is rendered VERBATIM: a backend rejection reason is the server's own copy and must not
 * be replaced with a generic client sentence.
 */
@Immutable
data class CountsWriteResultUi(
    val status: CountsWriteStatus = CountsWriteStatus.IDLE,
    val message: String? = null,
)

/**
 * Header for the two Counts WRITE screens.
 *
 * Both are backend `nav_items` of the counts module (`bootstrap_copy.go` → `/counts/birth-death`,
 * `/counts/shifting`), so for anyone holding more than one module they are L0 roots and must show
 * the module drawer, not Up. They are ALSO reachable as a push from the Counts landing screen, and
 * a single-module operator gets MINIMAL chrome with no drawer at all — which is why [onBack] is
 * still passed. [MeshaScreenHeader] picks between the two from the shell's own L0 membership, so
 * this screen never has to know which identity it is being rendered under.
 */
@Composable
internal fun CountsFormHeader(title: String, subtitle: String?, onBack: () -> Unit) {
    MeshaScreenHeader(title = title, subtitle = subtitle, onBack = onBack)
}

@Composable
internal fun CountsTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    numeric: Boolean = false,
    supporting: String? = null,
    isError: Boolean = false,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier.fillMaxWidth(),
        singleLine = true,
        isError = isError,
        label = { Text(if (required) "$label *" else label) },
        supportingText = supporting?.let { { Text(it) } },
        keyboardOptions = KeyboardOptions(
            keyboardType = if (numeric) KeyboardType.Number else KeyboardType.Text,
        ),
        colors = OutlinedTextFieldDefaults.colors(
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
            focusedBorderColor = MeshaColors.Brand,
            unfocusedBorderColor = MeshaColors.Hair,
            focusedLabelColor = MeshaColors.Brand,
            unfocusedLabelColor = MeshaColors.Muted,
            cursorColor = MeshaColors.Brand,
        ),
    )
}

/** A segmented choice. The option vocabulary is passed in by the caller from the backend contract. */
@Composable
internal fun CountsSegmented(
    options: List<Pair<String, String>>,
    selectedKey: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        options.forEach { (key, label) ->
            val selected = key == selectedKey
            Box(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(9.dp))
                    .background(if (selected) MeshaColors.Brand else Color.Transparent)
                    .clickable { onSelect(key) }
                    .padding(vertical = 9.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = label,
                    color = if (selected) MeshaColors.OnBrand else MeshaColors.Muted,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.W700,
                )
            }
        }
    }
}

@Composable
internal fun CountsSubmitButton(
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, onClick = onClick)
            .padding(vertical = 15.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text = label,
            color = if (enabled) MeshaColors.OnBrand else MeshaColors.Faint,
            fontSize = 15.sp,
            fontWeight = FontWeight.W800,
        )
    }
}

/**
 * Result banner. [CountsWriteStatus.QUEUED] is a SUCCESS tone on purpose: the operator's work is
 * durable in the outbox at that point, and telling them otherwise while they are offline in a shed
 * would push them to re-enter the same event.
 */
@Composable
internal fun CountsResultBanner(result: CountsWriteResultUi, modifier: Modifier = Modifier) {
    if (result.status == CountsWriteStatus.IDLE || result.message.isNullOrBlank()) return
    val (fg, bg) = when (result.status) {
        CountsWriteStatus.FAILED -> MeshaColors.Danger to MeshaColors.DangerX
        CountsWriteStatus.SYNCED -> MeshaColors.BrandD to MeshaColors.OkX
        else -> MeshaColors.Warn to MeshaColors.WarnX
    }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(bg)
            .padding(horizontal = 12.dp, vertical = 11.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(
            imageVector = if (result.status == CountsWriteStatus.FAILED) MeshaIcons.Warn else MeshaIcons.Check,
            contentDescription = null,
            tint = fg,
            modifier = Modifier.size(16.dp),
        )
        // Verbatim: backend owns rejection copy.
        Text(text = result.message, color = fg, fontSize = 12.sp, fontWeight = FontWeight.W600)
    }
}

@Composable
internal fun CountsFieldGroupTitle(text: String) {
    Text(
        text = text,
        color = MeshaColors.Faint,
        fontSize = 11.sp,
        fontWeight = FontWeight.W700,
        modifier = Modifier.padding(top = 6.dp),
    )
}
