package sg.mesha.goatos.feature.weighing

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Confirms a weighing state transition and collects the reason recorded against it.
 *
 * ONE dialog for close, reopen and abandon, because the requirement they share is the requirement
 * that matters: the reason is kept forever on the audit trail, so a HUMAN authors it. Every one of
 * these used to be a single tap on a small text target inside a scrolling list, sending a constant
 * the client made up -- which put a sentence nobody wrote into the record.
 *
 * [reason] is HOISTED rather than remembered here so the caller can persist it across process
 * death: the typed sentence is the one thing in this flow that cannot be recovered by re-reading
 * the backend.
 */
@Composable
internal fun WeighingReasonDialog(
    title: String,
    subtitle: String,
    placeholder: String,
    confirmLabel: String,
    confirmColor: Color,
    reason: String,
    onReasonChange: (String) -> Unit,
    onConfirm: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    // Purely presentational: the caller's reason is the durable state, this only says whether the
    // person has already tried to confirm an empty one.
    var showError by rememberSaveable { mutableStateOf(false) }

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
                OutlinedTextField(
                    value = reason,
                    onValueChange = {
                        onReasonChange(it)
                        if (it.isNotBlank()) showError = false
                    },
                    placeholder = { Text(placeholder) },
                    isError = showError,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedBorderColor = MeshaColors.Brand,
                        unfocusedBorderColor = MeshaColors.Hair,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (showError) {
                    Text(
                        text = stringResource(R.string.weighing_abandon_dialog_error_required),
                        color = MeshaColors.Danger,
                        style = MeshaType.cardSubtitle,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = {
                val trimmed = reason.trim()
                if (trimmed.isBlank()) showError = true else onConfirm(trimmed)
            }) {
                Text(text = confirmLabel, color = confirmColor, style = MeshaType.cardSubtitle)
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(
                    text = stringResource(R.string.weighing_abandon_dialog_cancel),
                    color = MeshaColors.Muted,
                    style = MeshaType.cardSubtitle,
                )
            }
        },
        containerColor = MeshaColors.Surf,
    )
}
