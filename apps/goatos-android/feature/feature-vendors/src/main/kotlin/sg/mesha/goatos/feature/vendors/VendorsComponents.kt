package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderers; the Vendors ViewModels in :app own the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.SelectableDates
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

/**
 * The Vendors module's form and list kit. ONE dropdown, ONE text field, ONE date field, ONE chip
 * row, ONE stepper for every screen in the module, so the register and the ledger read and behave
 * identically. Only open/closed state is local; every vocabulary, label and selection arrives from
 * the ViewModel.
 */

internal fun VendorsTone.colors(): Pair<Color, Color> = when (this) {
    VendorsTone.OK -> MeshaColors.Ok to MeshaColors.OkX
    VendorsTone.WARN -> MeshaColors.Warn to MeshaColors.WarnX
    VendorsTone.DANGER -> MeshaColors.Danger to MeshaColors.DangerX
    VendorsTone.INFO -> MeshaColors.Info to MeshaColors.InfoX
    VendorsTone.NEUTRAL -> MeshaColors.Muted to MeshaColors.Surf3
}

/** A small status chip; text is backend-owned copy. */
@Composable
internal fun VendorsChip(label: String, tone: VendorsTone, modifier: Modifier = Modifier) {
    val (ink, bg) = tone.colors()
    Text(
        text = label,
        color = ink,
        style = MeshaType.pillStrong,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier
            .clip(MeshaDimens.pill)
            .background(bg)
            .padding(horizontal = 9.dp, vertical = 4.dp),
    )
}

/** The scrolling filter-chip row; labels and order are backend-composed. */
@Composable
internal fun VendorsFilterRow(filters: List<VendorsFilterUi>, onSelect: (String) -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(horizontal = MeshaDimens.gutter, vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        filters.forEach { chip ->
            Text(
                text = chip.label,
                color = if (chip.selected) MeshaColors.BrandD else MeshaColors.Ink,
                style = MeshaType.pill,
                modifier = Modifier
                    .clip(MeshaDimens.pill)
                    .background(if (chip.selected) MeshaColors.BrandTint else MeshaColors.Surf)
                    .border(MeshaDimens.hairline, if (chip.selected) MeshaColors.BrandD else MeshaColors.Hair, MeshaDimens.pill)
                    .selectable(selected = chip.selected, role = Role.RadioButton, onClick = { onSelect(chip.key) })
                    .padding(horizontal = 12.dp, vertical = 8.dp),
            )
        }
    }
}

/** The register's search box: a plain field with the search glyph; the ViewModel debounces. */
@Composable
internal fun VendorsSearchField(value: String, placeholder: String, onValueChange: (String) -> Unit, modifier: Modifier = Modifier) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = MeshaDimens.gutter)
            .heightIn(min = MeshaDimens.minTapRow),
        singleLine = true,
        placeholder = { Text(placeholder, color = MeshaColors.Muted, style = MeshaType.body) },
        leadingIcon = { Icon(MeshaIcons.Search, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd)) },
        shape = RoundedCornerShape(MeshaDimens.radiusInput),
        colors = vendorsFieldColors(),
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
    )
}

@Composable
private fun vendorsFieldColors() = OutlinedTextFieldDefaults.colors(
    focusedContainerColor = MeshaColors.Surf2,
    unfocusedContainerColor = MeshaColors.Surf2,
    errorContainerColor = MeshaColors.Surf2,
    focusedBorderColor = MeshaColors.BrandD,
    unfocusedBorderColor = Color.Transparent,
    errorBorderColor = MeshaColors.Danger,
    cursorColor = MeshaColors.Brand,
    focusedLabelColor = MeshaColors.Muted,
    unfocusedLabelColor = MeshaColors.Muted,
    errorLabelColor = MeshaColors.Danger,
    focusedTextColor = MeshaColors.Ink,
    unfocusedTextColor = MeshaColors.Ink,
)

/** A tonal text field. [error] renders beneath the box; a blank numeric stays blank, never 0. */
@Composable
internal fun VendorsTextField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    keyboard: KeyboardType = KeyboardType.Text,
    error: String? = null,
    supporting: String? = null,
    singleLine: Boolean = true,
    readOnly: Boolean = false,
    trailing: (@Composable () -> Unit)? = null,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier.fillMaxWidth().heightIn(min = 54.dp),
        label = { Text(if (required) "$label *" else label) },
        singleLine = singleLine,
        minLines = if (singleLine) 1 else 3,
        maxLines = if (singleLine) 1 else 6,
        readOnly = readOnly,
        isError = error != null,
        supportingText = (error ?: supporting)?.let { text -> { Text(text, color = if (error != null) MeshaColors.Danger else MeshaColors.Muted, style = MeshaType.caption) } },
        keyboardOptions = KeyboardOptions(keyboardType = keyboard),
        trailingIcon = trailing,
        shape = RoundedCornerShape(MeshaDimens.radiusInput),
        colors = vendorsFieldColors(),
        textStyle = MeshaType.body.copy(color = MeshaColors.Ink),
    )
}

/** A tap-to-choose field over a bounded menu of backend options. */
@Composable
internal fun VendorsDropdownField(
    label: String,
    selectedValue: String,
    options: List<VendorsOptionUi>,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    error: String? = null,
    placeholder: String = "",
    allowClear: Boolean = false,
    clearLabel: String = "",
) {
    var expanded by remember { mutableStateOf(false) }
    val selectedLabel = options.firstOrNull { it.value == selectedValue }?.label ?: selectedValue
    Column(modifier = modifier.fillMaxWidth()) {
        Box(Modifier.fillMaxWidth()) {
            VendorsTextField(
                value = selectedLabel,
                onValueChange = {},
                label = label,
                required = required,
                readOnly = true,
                error = error,
                supporting = if (selectedLabel.isBlank()) placeholder else null,
                trailing = { Icon(MeshaIcons.ChevronDown, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd)) },
            )
            // A read-only field swallows the tap for focus, so a transparent overlay opens the menu.
            Box(
                Modifier
                    .matchParentSize()
                    .clickable(role = Role.DropdownList) { expanded = true },
            )
            DropdownMenu(
                expanded = expanded,
                onDismissRequest = { expanded = false },
                modifier = Modifier.heightIn(max = 320.dp).background(MeshaColors.Surf),
            ) {
                if (allowClear) {
                    DropdownMenuItem(
                        text = { Text(clearLabel, color = MeshaColors.Muted, style = MeshaType.body) },
                        onClick = { expanded = false; onSelect("") },
                    )
                }
                options.forEach { option ->
                    DropdownMenuItem(
                        text = {
                            Text(
                                option.label,
                                color = if (option.value == selectedValue) MeshaColors.BrandD else MeshaColors.Ink,
                                style = if (option.value == selectedValue) MeshaType.bodyStrong else MeshaType.body,
                            )
                        },
                        onClick = { expanded = false; onSelect(option.value) },
                    )
                }
            }
        }
    }
}

/** A two-to-four way choice drawn as a filled segmented track. */
@Composable
internal fun VendorsSegmented(
    options: List<VendorsOptionUi>,
    selectedValue: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusSeg))
            .background(MeshaColors.Surf2)
            .padding(4.dp),
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        options.forEach { option ->
            val selected = option.value == selectedValue
            Box(
                modifier = Modifier
                    .weight(1f)
                    .heightIn(min = 40.dp)
                    .clip(RoundedCornerShape(MeshaDimens.radiusSmall))
                    .background(if (selected) MeshaColors.Brand else Color.Transparent)
                    .selectable(selected = selected, role = Role.RadioButton, onClick = { onSelect(option.value) }),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = option.label,
                    color = if (selected) MeshaColors.OnBrand else MeshaColors.Ink,
                    style = MeshaType.pillStrong,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.padding(horizontal = 8.dp),
                )
            }
        }
    }
}

/**
 * A date field: read-only text over the Material date picker, values as ISO `YYYY-MM-DD`,
 * rendered DD-MM-YYYY like every other date in the app. [maxIso] / [minIso] bound the picker.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun VendorsDateField(
    label: String,
    valueIso: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    error: String? = null,
    supporting: String? = null,
    minIso: String = "",
    maxIso: String = "",
    clearLabel: String = "",
    doneLabel: String = "OK",
    cancelLabel: String = "Cancel",
) {
    var open by remember { mutableStateOf(false) }
    Box(modifier = modifier.fillMaxWidth()) {
        VendorsTextField(
            value = displayDate(valueIso),
            onValueChange = {},
            label = label,
            required = required,
            readOnly = true,
            error = error,
            supporting = supporting,
            trailing = { Icon(MeshaIcons.Calendar, contentDescription = null, tint = MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconMd)) },
        )
        Box(Modifier.matchParentSize().clickable(role = Role.Button) { open = true })
    }
    if (open) {
        val minMillis = minIso.toEpochMillisOrNull()
        val maxMillis = maxIso.toEpochMillisOrNull()
        val state = rememberDatePickerState(
            initialSelectedDateMillis = valueIso.toEpochMillisOrNull() ?: maxMillis,
            selectableDates = object : SelectableDates {
                override fun isSelectableDate(utcTimeMillis: Long): Boolean =
                    (minMillis == null || utcTimeMillis >= minMillis) && (maxMillis == null || utcTimeMillis <= maxMillis)
            },
        )
        DatePickerDialog(
            onDismissRequest = { open = false },
            confirmButton = {
                TextButton(onClick = {
                    state.selectedDateMillis?.let { onValueChange(it.toIsoDate()) }
                    open = false
                }) { Text(doneLabel, color = MeshaColors.BrandD, style = MeshaType.button) }
            },
            dismissButton = {
                Row {
                    if (clearLabel.isNotBlank() && valueIso.isNotBlank()) {
                        TextButton(onClick = { onValueChange(""); open = false }) { Text(clearLabel, color = MeshaColors.Muted, style = MeshaType.button) }
                    }
                    TextButton(onClick = { open = false }) { Text(cancelLabel, color = MeshaColors.Muted, style = MeshaType.button) }
                }
            },
        ) {
            DatePicker(state = state)
        }
    }
}

internal fun displayDate(iso: String): String {
    val parts = iso.split("-")
    return if (parts.size == 3) "${parts[2]}-${parts[1]}-${parts[0]}" else iso
}

private fun String.toEpochMillisOrNull(): Long? =
    runCatching { LocalDate.parse(this).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() }.getOrNull()

private fun Long.toIsoDate(): String = Instant.ofEpochMilli(this).atZone(ZoneOffset.UTC).toLocalDate().toString()

/** A titled card grouping a step's fields. */
@Composable
internal fun VendorsFormGroup(title: String, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(text = title.uppercase(), color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        content()
    }
}

/** Segment stepper: one bar per wizard step, filled up to the current one; a caption names the step. */
@Composable
internal fun VendorsStepper(stepCount: Int, currentIndex: Int, caption: String, modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxWidth().padding(horizontal = MeshaDimens.gutter, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(5.dp)) {
            repeat(stepCount) { index ->
                Box(
                    modifier = Modifier
                        .weight(1f)
                        .height(3.dp)
                        .clip(RoundedCornerShape(20.dp))
                        .background(if (index <= currentIndex) MeshaColors.Brand else MeshaColors.Surf3),
                )
            }
        }
        Text(text = caption, color = MeshaColors.Muted, style = MeshaType.caption)
    }
}

/** Sticky bottom bar: the context line (what is chosen so far) and the step's actions. */
@Composable
internal fun VendorsWizardBar(contextLine: String, modifier: Modifier = Modifier, content: @Composable RowScope.() -> Unit) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(MeshaColors.Bg)
            .padding(horizontal = MeshaDimens.gutter, vertical = 12.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (contextLine.isNotBlank()) {
            Text(text = contextLine, color = MeshaColors.Muted, style = MeshaType.caption, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp), content = content)
    }
}

/** Primary action. Disabled renders as a dead button, never hidden. */
@Composable
internal fun VendorsPrimaryButton(label: String, enabled: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .heightIn(min = MeshaDimens.minTapButton)
            .clip(RoundedCornerShape(MeshaDimens.radiusButton))
            .background(if (enabled) MeshaColors.Brand else MeshaColors.Surf3)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 18.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = label, color = if (enabled) MeshaColors.OnBrand else MeshaColors.Muted, style = MeshaType.cta)
    }
}

/** Secondary (ghost) action. */
@Composable
internal fun VendorsGhostButton(label: String, onClick: () -> Unit, modifier: Modifier = Modifier, enabled: Boolean = true) {
    Box(
        modifier = modifier
            .heightIn(min = MeshaDimens.minTapButton)
            .clip(RoundedCornerShape(MeshaDimens.radiusButton))
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusButton))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = 18.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(text = label, color = if (enabled) MeshaColors.Ink else MeshaColors.Muted, style = MeshaType.cta)
    }
}

/** The write-result banner. QUEUED is a success tone: the record is durable in the outbox. */
@Composable
internal fun VendorsResultBanner(status: VendorsWriteStatus, message: String, modifier: Modifier = Modifier) {
    if (status == VendorsWriteStatus.IDLE || message.isBlank()) return
    val (ink, bg) = when (status) {
        VendorsWriteStatus.FAILED -> MeshaColors.Danger to MeshaColors.DangerX
        VendorsWriteStatus.SYNCED -> MeshaColors.BrandD to MeshaColors.OkX
        else -> MeshaColors.Warn to MeshaColors.WarnX
    }
    Text(
        text = message,
        color = ink,
        style = MeshaType.bodyStrong,
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
            .background(bg)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    )
}

/** A key/value row of a detail section. */
@Composable
internal fun VendorsDetailRow(row: VendorsDetailRowUi, modifier: Modifier = Modifier) {
    Column(modifier = modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Text(text = row.label.uppercase(), color = MeshaColors.Muted, style = MeshaType.overline)
        Spacer(Modifier.height(2.dp))
        Text(text = row.value, color = MeshaColors.Ink, style = MeshaType.rowValue)
    }
}

/** A titled detail section as a card of rows. */
@Composable
internal fun VendorsDetailSection(section: VendorsDetailSectionUi, modifier: Modifier = Modifier) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .padding(14.dp),
    ) {
        Text(text = section.title.uppercase(), color = MeshaColors.Muted, style = MeshaType.sectionLabel)
        Spacer(Modifier.height(4.dp))
        section.rows.forEach { row -> VendorsDetailRow(row) }
    }
}

/** A list card shell with the module's tap affordance. */
@Composable
internal fun VendorsCard(onClick: (() -> Unit)?, modifier: Modifier = Modifier, content: @Composable () -> Unit) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = MeshaDimens.gutter)
            .clip(RoundedCornerShape(MeshaDimens.radiusCard))
            .background(MeshaColors.Surf)
            .border(MeshaDimens.hairline, MeshaColors.Hair, RoundedCornerShape(MeshaDimens.radiusCard))
            .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) { content() }
}

/** A rounded icon tile beside a card title. */
@Composable
internal fun VendorsIconTile(icon: ImageVector, tint: Color, background: Color, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .size(38.dp)
            .clip(RoundedCornerShape(MeshaDimens.radiusIcon))
            .background(background),
        contentAlignment = Alignment.Center,
    ) {
        Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(MeshaDimens.iconMd))
    }
}

/** A floating "add" button anchored bottom-right of a list. */
@Composable
internal fun VendorsAddButton(label: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Row(
        modifier = modifier
            .clip(MeshaDimens.pill)
            .background(MeshaColors.Brand)
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 18.dp, vertical = 14.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Icon(MeshaIcons.Plus, contentDescription = null, tint = MeshaColors.OnBrand, modifier = Modifier.size(MeshaDimens.iconMd))
        Text(text = label, color = MeshaColors.OnBrand, style = MeshaType.cta)
        Spacer(Modifier.width(2.dp))
    }
}
