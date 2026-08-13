package sg.mesha.goatos.feature.scan

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Confirms a vaccination submission. The destructive choice (confirm) is the right button
 * to avoid accidental submission on a tap meant for dismiss.
 *
 * This dialog does NOT collect user-authored input — it is a simple yes/no gate before the
 * submit proceeds. The maintainer wanted clarity that submission is irreversible for the
 * operator and that only leadership can reopen a shed they submit.
 */
@Composable
internal fun SubmitConfirmationDialog(
    title: String,
    subtitle: String,
    dismissLabel: String,
    confirmLabel: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = {
            Text(text = title, color = MeshaColors.Ink, style = MeshaType.cardTitle)
        },
        text = {
            Column {
                Text(
                    text = subtitle,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.padding(bottom = 10.dp),
                )
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(
                    text = dismissLabel,
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(text = confirmLabel, color = MeshaColors.Danger, style = MeshaType.cardSubtitle)
            }
        },
        containerColor = MeshaColors.Surf,
    )
}
