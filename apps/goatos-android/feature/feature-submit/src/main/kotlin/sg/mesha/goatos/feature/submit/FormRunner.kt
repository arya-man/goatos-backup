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
import androidx.compose.ui.graphics.SolidColor
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

/** One captured proof video row under a [FieldKindUi.VIDEO_PROOF] field (MOB-002, Room-first —
 *  docs/mobile/proof-capture-sync-and-e2e.md §2/§3). [editableCaption] is true only for an
 *  operator-added EXTRA video (beyond the SOP's named/required subjects); a named subject's
 *  description comes from the server form (`FormField.helpText`), never a client caption. */
data class ProofItemUi(
    val id: String,
    val label: String,
    val caption: String = "",
    val editableCaption: Boolean = false,
    /** "PENDING" / "IN_FLIGHT" / "SYNCED" / "FAILED" — mirrors the Room row's sync status
     *  (Photos/Drive "uploading…/synced" model). */
    val syncStatus: String = "PENDING",
)

/** One render-ready field. The :app ViewModel maps `FormSpec` + current answers into these. */
data class FormFieldUi(
    val key: String,
    val label: String,
    val kind: FieldKindUi,
    val required: Boolean = false,
    /** Server-authored description (SOP form_dsl `help_text`) — e.g. which shed/vial/injection
     *  a `video_proof` field proves. Never hardcoded on the client. */
    val helpText: String? = null,
    val text: String = "",            // text / number
    val checked: Boolean = false,     // boolean
    val scannedCount: Int = 0,        // goat_scan
    /** True while [key]'s BT-HID scan capture is actively listening for tags. */
    val scanning: Boolean = false,
    val proofCaptured: Boolean = false, // video_proof: at least one row captured for this field
    /** Captured proof rows for this field (MOB-002) — [FieldKindUi.VIDEO_PROOF] only. */
    val proofItems: List<ProofItemUi> = emptyList(),
    /** True while this field may still accept another capture (repeat field under the total
     *  cap, or a non-repeat field with nothing captured yet). */
    val canCaptureMore: Boolean = true,
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
    onCaption: (key: String, proofId: String, caption: String) -> Unit = { _, _, _ -> },
    onRemoveProof: (key: String, proofId: String) -> Unit = { _, _ -> },
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
                FieldCard(field, onToggle, onText, onScan, onPick, onCaptureVideo, onCaption, onRemoveProof)
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
    onCaption: (key: String, proofId: String, caption: String) -> Unit = { _, _, _ -> },
    onRemoveProof: (key: String, proofId: String) -> Unit = { _, _ -> },
) {
    Column(modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        fields.forEach { field ->
            FieldCard(field, onToggle, onText, onScan, onPick, onCaptureVideo, onCaption, onRemoveProof)
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
    onCaption: (String, String, String) -> Unit,
    onRemoveProof: (String, String) -> Unit,
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
        // VIDEO_PROOF renders its own helpText as the proof-box hint subtitle (mock's
        // `.proofbox span`) — showing it again here would duplicate the same description.
        if (field.kind != FieldKindUi.VIDEO_PROOF) {
            field.helpText.takeIf { !it.isNullOrBlank() }?.let {
                Text(it, color = MeshaColors.Faint, fontSize = 11.5.sp)
            }
        }
        when (field.kind) {
            FieldKindUi.BOOLEAN -> ToggleControl(field, onToggle)
            FieldKindUi.NUMBER -> TextControl(field, numeric = true, onText)
            FieldKindUi.TEXT -> TextControl(field, numeric = false, onText)
            FieldKindUi.GOAT_SCAN -> ScanZoneControl(field, onScan)
            FieldKindUi.PICKER -> PickerControl(field, onPick)
            FieldKindUi.VIDEO_PROOF -> ProofBoxControl(field, onCaptureVideo, onCaption, onRemoveProof)
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

/** Mock-faithful `.scanzone` (mock/vaccination-mobile-mock.html): dashed 2dp border, a rounded
 *  brand-tinted icon chip, bold centered message — solid brand border + brand-tinted background
 *  once at least one tag is captured (mock's `.scanzone.read` state). */
@Composable
private fun ScanZoneControl(field: FormFieldUi, onScan: (String) -> Unit) {
    val hasScans = field.scannedCount > 0
    Column(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(if (hasScans) MeshaColors.OkX else MeshaColors.Surf)
            .border(
                width = if (hasScans) 2.dp else 2.dp,
                color = if (hasScans) MeshaColors.Brand else MeshaColors.Hair,
                shape = RoundedCornerShape(16.dp),
            )
            .clickable(onClick = { onScan(field.key) })
            .padding(vertical = 20.dp, horizontal = 14.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            Modifier
                .size(44.dp)
                .clip(RoundedCornerShape(13.dp))
                .background(if (field.scanning) MeshaColors.BrandGradient else androidx.compose.ui.graphics.SolidColor(MeshaColors.BrandTint)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                MeshaIcons.Search,
                contentDescription = null,
                tint = if (field.scanning) MeshaColors.OnBrand else MeshaColors.Brand,
                modifier = Modifier.size(20.dp),
            )
        }
        Spacer(Modifier.height(9.dp))
        Text(
            text = when {
                field.scanning -> "Scanning… tap to stop"
                hasScans -> "${field.scannedCount} scanned · tap to add more"
                else -> "Tap to scan goats"
            },
            color = MeshaColors.Ink,
            fontSize = 14.sp,
            fontWeight = FontWeight.W700,
        )
    }
}

/** Mock-faithful `.proofbox` (mock/vaccination-mobile-mock.html): dashed 1.5dp border box with a
 *  centered brand icon + bold title + hint subtitle; captured rows render below with a
 *  sync-status chip, and an operator-added EXTRA video gets an editable caption + remove
 *  affordance. [field.canCaptureMore] gates whether the box itself is still tappable (business
 *  cap reached is a real block, not styling). */
@Composable
private fun ProofBoxControl(
    field: FormFieldUi,
    onCaptureVideo: (String) -> Unit,
    onCaption: (String, String, String) -> Unit,
    onRemoveProof: (String, String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (field.canCaptureMore) {
            Column(
                Modifier
                    .fillMaxWidth()
                    .clip(RoundedCornerShape(16.dp))
                    .background(MeshaColors.Surf)
                    .border(1.5.dp, MeshaColors.Hair, RoundedCornerShape(16.dp))
                    .clickable(onClick = { onCaptureVideo(field.key) })
                    .padding(15.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Icon(MeshaIcons.Video, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(24.dp))
                Spacer(Modifier.height(8.dp))
                Text(
                    text = if (field.proofItems.isEmpty()) "Record ${field.label.lowercase()}" else "Add another video",
                    color = MeshaColors.Ink,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.W700,
                )
                Spacer(Modifier.height(2.dp))
                Text(
                    text = field.helpText ?: "tap to record",
                    color = MeshaColors.Muted,
                    fontSize = 11.sp,
                )
            }
        }
        field.proofItems.forEach { item -> ProofItemRow(field.key, item, onCaption, onRemoveProof) }
    }
}

@Composable
private fun ProofItemRow(
    fieldKey: String,
    item: ProofItemUi,
    onCaption: (String, String, String) -> Unit,
    onRemoveProof: (String, String) -> Unit,
) {
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf3)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(MeshaIcons.Check, contentDescription = null, tint = MeshaColors.Brand, modifier = Modifier.size(16.dp))
        Column(Modifier.weight(1f)) {
            if (item.editableCaption) {
                OutlinedTextField(
                    value = item.caption,
                    onValueChange = { onCaption(fieldKey, item.id, it) },
                    singleLine = true,
                    placeholder = { Text("Describe this video", fontSize = 12.sp) },
                    textStyle = androidx.compose.ui.text.TextStyle(fontSize = 12.5.sp),
                    modifier = Modifier.fillMaxWidth(),
                    colors = OutlinedTextFieldDefaults.colors(
                        focusedTextColor = MeshaColors.Ink,
                        unfocusedTextColor = MeshaColors.Ink,
                        focusedBorderColor = MeshaColors.Brand,
                        unfocusedBorderColor = MeshaColors.Hair,
                        cursorColor = MeshaColors.Brand,
                        focusedContainerColor = MeshaColors.Surf2,
                        unfocusedContainerColor = MeshaColors.Surf2,
                    ),
                )
            } else {
                Text(item.label, color = MeshaColors.Ink, fontSize = 12.5.sp, fontWeight = FontWeight.W600)
            }
            Text(syncStatusLabel(item.syncStatus), color = syncStatusColor(item.syncStatus), fontSize = 10.5.sp)
        }
        if (item.editableCaption) {
            Icon(
                MeshaIcons.Close,
                contentDescription = "Remove",
                tint = MeshaColors.Faint,
                modifier = Modifier
                    .size(16.dp)
                    .clip(CircleShape)
                    .clickable(onClick = { onRemoveProof(fieldKey, item.id) }),
            )
        }
    }
}

private fun syncStatusLabel(status: String): String = when (status) {
    "SYNCED" -> "Synced"
    "IN_FLIGHT" -> "Uploading…"
    "FAILED" -> "Failed · will retry"
    else -> "Queued"
}

private fun syncStatusColor(status: String) = when (status) {
    "SYNCED" -> MeshaColors.Brand
    "IN_FLIGHT" -> MeshaColors.Warn
    "FAILED" -> MeshaColors.Danger
    else -> MeshaColors.Muted
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
