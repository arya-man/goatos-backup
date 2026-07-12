package sg.mesha.goatos.feature.submit

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/** Render kind for one SOP form field (maps from core-data `FormFieldType` in the :app layer). */
enum class FieldKindUi { BOOLEAN, NUMBER, TEXT, GOAT_SCAN, PICKER, VIDEO_PROOF, UNKNOWN }

/** One selectable option for a [FieldKindUi.PICKER] field ([value] is submitted, [label] is shown). */
data class FormPickerOptionUi(val value: String, val label: String)

/** One render-ready field. The :app ViewModel maps `FormSpec` + current answers into these. */
data class FormFieldUi(
    val key: String,
    val label: String,
    val kind: FieldKindUi,
    val required: Boolean = false,
    val text: String = "",            // text / number
    val checked: Boolean = false,     // boolean
    val scannedCount: Int = 0,        // goat_scan
    val proofCaptured: Boolean = false, // video_proof
    val selectedLabel: String = "",   // picker (human label of chosen option)
    val options: List<FormPickerOptionUi> = emptyList(), // picker (selectable options)
    val error: String? = null,
)

data class FormRunnerState(
    val title: String = "Record drive",
    val subtitle: String = "",
    val fields: List<FormFieldUi> = emptyList(),
    val submitLabel: String = "Submit drive",
    /** null = submit enabled; non-null = disabled, shown as the reason (mock `block_submission_if`). */
    val blockedReason: String? = null,
)

/**
 * Stateless operator drive-form runner (recording-form slice 3). Renders each SOP `form_dsl` field
 * as an on-brand card by type; the host owns state + rule evaluation and passes the reduced
 * [FormRunnerState] down. Submit is disabled with the backend's block reason when the form isn't
 * complete — no fake success.
 */
@Composable
fun FormRunner(
    state: FormRunnerState,
    onToggle: (key: String, checked: Boolean) -> Unit,
    onText: (key: String, value: String) -> Unit,
    onScan: (key: String) -> Unit,
    onPick: (key: String, value: String) -> Unit,
    onCaptureVideo: (key: String) -> Unit,
    onSubmit: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxWidth().background(MeshaColors.Bg)) {
        Column(Modifier.padding(start = 20.dp, end = 20.dp, top = 18.dp, bottom = 6.dp)) {
            Text(state.title, color = MeshaColors.Ink, fontSize = 20.sp, fontWeight = FontWeight.W800)
            if (state.subtitle.isNotBlank()) {
                Spacer(Modifier.height(2.dp))
                Text(state.subtitle, color = MeshaColors.Muted, fontSize = 12.5.sp)
            }
        }
        LazyColumn(
            Modifier.fillMaxWidth().weight(1f, fill = false),
            contentPadding = PaddingValues(horizontal = 20.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            items(state.fields, key = { it.key }) { field ->
                FieldCard(field, onToggle, onText, onScan, onPick, onCaptureVideo)
            }
        }
        SubmitBar(state, onSubmit)
    }
}

/**
 * Non-scrolling field list — the same field cards [FormRunner] draws, without owning a
 * [LazyColumn]/header/submit bar of its own. Lets a host screen with a single scroll container
 * (e.g. Submit's shed-record list, TRD §14 dumb-renderer: one scroll container per screen) embed
 * the recording form inline via its own `items { }` block instead of nesting scrollables.
 */
@Composable
fun FormFieldsColumn(
    fields: List<FormFieldUi>,
    onToggle: (key: String, checked: Boolean) -> Unit,
    onText: (key: String, value: String) -> Unit,
    onScan: (key: String) -> Unit,
    onPick: (key: String, value: String) -> Unit,
    onCaptureVideo: (key: String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        fields.forEach { field ->
            FieldCard(field, onToggle, onText, onScan, onPick, onCaptureVideo)
        }
    }
}

@Composable
private fun FieldCard(
    field: FormFieldUi,
    onToggle: (String, Boolean) -> Unit,
    onText: (String, String) -> Unit,
    onScan: (String) -> Unit,
    onPick: (String, String) -> Unit,
    onCaptureVideo: (String) -> Unit,
) {
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf2)
            .border(1.dp, if (field.error != null) MeshaColors.Danger else MeshaColors.Hair, RoundedCornerShape(16.dp))
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        FieldLabel(field.label, field.required)
        when (field.kind) {
            FieldKindUi.BOOLEAN -> ToggleControl(field, onToggle)
            FieldKindUi.NUMBER -> TextControl(field, numeric = true, onText)
            FieldKindUi.TEXT -> TextControl(field, numeric = false, onText)
            FieldKindUi.GOAT_SCAN -> ActionControl(
                text = if (field.scannedCount > 0) "${field.scannedCount} scanned" else "Scan goats",
                icon = MeshaIcons.Search,
                done = field.scannedCount > 0,
                onClick = { onScan(field.key) },
            )
            FieldKindUi.PICKER -> PickerControl(field, onPick)
            FieldKindUi.VIDEO_PROOF -> ActionControl(
                text = if (field.proofCaptured) "Video recorded" else "Record video proof",
                icon = MeshaIcons.Video,
                done = field.proofCaptured,
                onClick = { onCaptureVideo(field.key) },
            )
            FieldKindUi.UNKNOWN -> Text(
                "Unsupported field — update the app to record this.",
                color = MeshaColors.Faint,
                fontSize = 12.sp,
            )
        }
        field.error?.let { Text(it, color = MeshaColors.Danger, fontSize = 11.5.sp) }
    }
}

@Composable
private fun FieldLabel(label: String, required: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(label, color = MeshaColors.Ink, fontSize = 13.5.sp, fontWeight = FontWeight.W700)
        if (required) {
            Spacer(Modifier.size(4.dp))
            Text("*", color = MeshaColors.Danger, fontSize = 13.5.sp, fontWeight = FontWeight.W800)
        }
    }
}

@Composable
private fun ToggleControl(field: FormFieldUi, onToggle: (String, Boolean) -> Unit) {
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(
            if (field.checked) "Yes" else "No",
            color = if (field.checked) MeshaColors.BrandD else MeshaColors.Muted,
            fontSize = 13.sp,
            fontWeight = FontWeight.W600,
            modifier = Modifier.weight(1f),
        )
        Switch(
            checked = field.checked,
            onCheckedChange = { onToggle(field.key, it) },
            colors = SwitchDefaults.colors(
                checkedThumbColor = MeshaColors.Bg,
                checkedTrackColor = MeshaColors.Brand,
                uncheckedThumbColor = MeshaColors.Muted,
                uncheckedTrackColor = MeshaColors.Surf3,
            ),
        )
    }
}

@Composable
private fun TextControl(field: FormFieldUi, numeric: Boolean, onText: (String, String) -> Unit) {
    OutlinedTextField(
        value = field.text,
        onValueChange = { onText(field.key, it) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = if (numeric) KeyboardType.Number else KeyboardType.Text),
        modifier = Modifier.fillMaxWidth(),
        colors = OutlinedTextFieldDefaults.colors(
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
            focusedBorderColor = MeshaColors.Brand,
            unfocusedBorderColor = MeshaColors.Hair,
            cursorColor = MeshaColors.Brand,
            focusedContainerColor = MeshaColors.Surf,
            unfocusedContainerColor = MeshaColors.Surf,
        ),
    )
}

/** Real (not fire-only) picker: taps open a [DropdownMenu] over [field]'s backend-supplied
 *  [FormFieldUi.options] and report the chosen option's VALUE — never a fabricated selection. A
 *  field with no options (backend expects an out-of-band picker flow) stays inert on tap. */
@Composable
private fun PickerControl(field: FormFieldUi, onPick: (String, String) -> Unit) {
    var expanded by remember(field.key) { mutableStateOf(false) }
    Box {
        ActionControl(
            text = field.selectedLabel.ifBlank { "Select…" },
            icon = MeshaIcons.Chevron,
            done = field.selectedLabel.isNotBlank(),
            onClick = { if (field.options.isNotEmpty()) expanded = true },
        )
        DropdownMenu(expanded = expanded, onDismissRequest = { expanded = false }) {
            field.options.forEach { option ->
                DropdownMenuItem(
                    text = { Text(option.label) },
                    onClick = {
                        expanded = false
                        onPick(field.key, option.value)
                    },
                )
            }
        }
    }
}

@Composable
private fun ActionControl(text: String, icon: androidx.compose.ui.graphics.vector.ImageVector, done: Boolean, onClick: () -> Unit) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf3)
            .clickable(onClick = onClick)
            .padding(horizontal = 13.dp, vertical = 12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(icon, contentDescription = null, tint = if (done) MeshaColors.Brand else MeshaColors.Muted, modifier = Modifier.size(18.dp))
        Text(text, color = if (done) MeshaColors.BrandD else MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W600, modifier = Modifier.weight(1f))
        if (done) Icon(MeshaIcons.Check, contentDescription = "Done", tint = MeshaColors.Brand, modifier = Modifier.size(16.dp))
    }
}

@Composable
private fun SubmitBar(state: FormRunnerState, onSubmit: () -> Unit) {
    val enabled = state.blockedReason == null
    Column(Modifier.fillMaxWidth().padding(20.dp)) {
        state.blockedReason?.let {
            Text(it, color = MeshaColors.Warn, fontSize = 11.5.sp, modifier = Modifier.padding(bottom = 8.dp))
        }
        Box(
            Modifier
                .fillMaxWidth()
                .height(50.dp)
                .clip(RoundedCornerShape(14.dp))
                .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
                .clickable(enabled = enabled, onClick = onSubmit),
            contentAlignment = Alignment.Center,
        ) {
            Text(
                state.submitLabel,
                color = if (enabled) MeshaColors.Bg else MeshaColors.Faint,
                fontSize = 15.sp,
                fontWeight = FontWeight.W800,
            )
        }
    }
}
