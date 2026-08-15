package sg.mesha.goatos.feature.counts

// telemetry:exempt purely presentational form primitives with no user action of their own; the
// owning screens' ViewModels (BirthDeathViewModel / ShiftingViewModel) wire analytics + Crashlytics.

import androidx.compose.foundation.background
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
import androidx.compose.foundation.interaction.MutableInteractionSource
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
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.time.Instant
import java.time.ZoneOffset
import java.time.format.DateTimeFormatter
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
    /** True for a picker-backed field (e.g. [CountsDateField]): blocks the keyboard so the only
     * way to change the value is the picker, while still rendering/tapping like every other field. */
    readOnly: Boolean = false,
    /** Optional end-slot control (e.g. the RFID scan toggle in [CountsRfidField]). */
    trailingIcon: @Composable (() -> Unit)? = null,
    /**
     * False for a free-text note that should wrap and grow instead of scrolling sideways in a
     * one-line box. Every identifier/quantity field stays single-line, which is why that is the
     * default.
     */
    singleLine: Boolean = true,
) {
    // Modernized to a tonal filled field (no hairline border) matching the mock's coherent
    // form chrome; behavior/params are unchanged so every screen benefits without call-site edits.
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 54.dp),
        singleLine = singleLine,
        // Bounded on both ends: tall enough that a note does not feel like a one-line box, capped
        // so a long note scrolls inside the field instead of pushing Submit off the form.
        minLines = if (singleLine) 1 else 3,
        maxLines = if (singleLine) 1 else 6,
        readOnly = readOnly,
        isError = isError,
        shape = RoundedCornerShape(14.dp),
        label = { Text(if (required) "$label *" else label) },
        supportingText = supporting?.let { { Text(it) } },
        trailingIcon = trailingIcon,
        keyboardOptions = KeyboardOptions(
            keyboardType = if (numeric) KeyboardType.Number else KeyboardType.Text,
        ),
        colors = OutlinedTextFieldDefaults.colors(
            focusedTextColor = MeshaColors.Ink,
            unfocusedTextColor = MeshaColors.Ink,
            focusedContainerColor = MeshaColors.Surf2,
            unfocusedContainerColor = MeshaColors.Surf2,
            disabledContainerColor = MeshaColors.Surf3,
            focusedBorderColor = Color.Transparent,
            unfocusedBorderColor = Color.Transparent,
            focusedLabelColor = MeshaColors.Brand,
            unfocusedLabelColor = MeshaColors.Muted,
            cursorColor = MeshaColors.Brand,
        ),
    )
}

/**
 * A permanent-RFID field: the ordinary [CountsTextField] plus a Bluetooth scan toggle, so an
 * operator attaching an ear tag can SCAN the identifier instead of reading 15 digits off a tag and
 * typing them into a phone in a shed.
 *
 * The reader is the V1 Bluetooth HID keyboard-wedge scanner
 * (`docs/mobile/rfid-keyboard-reader.md`): Android owns the HID connection, and the owning
 * ViewModel toggles capture through `ScanSource` — exactly the port the Submit recording form's
 * `goat_scan` field already uses. This composable stays presentational: it renders [scanning] and
 * reports taps, and never imports Bluetooth/InputManager itself (the device-port boundary).
 *
 * Typing stays available. The scanner is an input convenience, not a gate — a dead reader battery
 * must never block a birth record, so the field is never read-only.
 */
@Composable
internal fun CountsRfidField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    scanning: Boolean,
    onToggleScan: () -> Unit,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    supporting: String? = null,
    isError: Boolean = false,
) {
    Column(modifier = modifier.fillMaxWidth()) {
        CountsTextField(
            value = value,
            onValueChange = onValueChange,
            label = label,
            required = required,
            supporting = supporting,
            isError = isError,
            trailingIcon = {
                // 48dp touch target around a 38dp chip: an operator taps this wearing gloves, and
                // the a11y minimum is the TOUCH area, not the painted one.
                Box(
                    modifier = Modifier
                        .padding(end = 4.dp)
                        .size(48.dp)
                        .clip(RoundedCornerShape(14.dp))
                        .clickable(onClick = onToggleScan),
                    contentAlignment = Alignment.Center,
                ) {
                    Box(
                        modifier = Modifier
                            .size(38.dp)
                            .clip(RoundedCornerShape(11.dp))
                            .background(if (scanning) MeshaColors.Brand else MeshaColors.BrandTint),
                        contentAlignment = Alignment.Center,
                    ) {
                        Icon(
                            imageVector = MeshaIcons.Bluetooth,
                            contentDescription = stringResource(
                                if (scanning) R.string.counts_rfid_scan_stop else R.string.counts_rfid_scan_start,
                            ),
                            tint = if (scanning) MeshaColors.OnBrand else MeshaColors.Brand,
                            modifier = Modifier.size(18.dp),
                        )
                    }
                }
            },
        )
        if (scanning) {
            Text(
                text = stringResource(R.string.counts_rfid_scan_listening),
                color = MeshaColors.Brand,
                fontSize = 11.sp,
                fontWeight = FontWeight.W700,
                modifier = Modifier.padding(start = 14.dp, top = 2.dp),
            )
        }
    }
}

/**
 * A date field backed by a real Compose M3 [DatePicker], writing the SAME `YYYY-MM-DD` ISO string
 * the backend's date fields already expect (identity's `CreateAdminGoatRequest.dob`/`entry_date`
 * wire contract) — this changes only how the operator ENTERS the string, never its format.
 *
 * The field itself stays read-only text (tapping opens the picker) so a date can never be
 * hand-typed into an invalid shape; [onValueChange] receives the same plain ISO string a manual
 * `CountsTextField` would have produced, so callers do not need to know a picker is involved.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun CountsDateField(
    value: String,
    onValueChange: (String) -> Unit,
    label: String,
    modifier: Modifier = Modifier,
    required: Boolean = false,
    supporting: String? = null,
    isError: Boolean = false,
) {
    var pickerOpen by remember { mutableStateOf(false) }
    // A read-only OutlinedTextField still consumes the tap for focus, so a `.clickable` on the
    // field's own modifier never fires and the picker never opens. Overlay a transparent, top-most
    // click target (matchParentSize, no ripple) that reliably captures the tap and opens the picker.
    Box(modifier = modifier.fillMaxWidth()) {
        CountsTextField(
            value = value,
            onValueChange = {}, // read-only: the picker is the only way to change this field
            label = label,
            modifier = Modifier.fillMaxWidth(),
            required = required,
            supporting = supporting,
            isError = isError,
            readOnly = true,
        )
        Box(
            modifier = Modifier
                .matchParentSize()
                .clickable(
                    interactionSource = remember { MutableInteractionSource() },
                    indication = null,
                ) { pickerOpen = true },
        )
    }
    if (pickerOpen) {
        val initialMillis = value.toEpochMillisOrNull()
            ?: Instant.now().toEpochMilli()
        val pickerState = rememberDatePickerState(initialSelectedDateMillis = initialMillis)
        DatePickerDialog(
            onDismissRequest = { pickerOpen = false },
            confirmButton = {
                TextButton(onClick = {
                    pickerState.selectedDateMillis?.let { millis ->
                        onValueChange(millis.toIsoDateString())
                    }
                    pickerOpen = false
                }) {
                    Text(stringResource(id = android.R.string.ok))
                }
            },
            dismissButton = {
                TextButton(onClick = { pickerOpen = false }) {
                    Text(stringResource(id = android.R.string.cancel))
                }
            },
        ) {
            DatePicker(state = pickerState)
        }
    }
}

private val isoDateFormatter: DateTimeFormatter = DateTimeFormatter.ISO_LOCAL_DATE

/** Parses a `YYYY-MM-DD` string back to epoch millis (UTC midnight) for the picker's initial position. */
private fun String.toEpochMillisOrNull(): Long? = runCatching {
    java.time.LocalDate.parse(this, isoDateFormatter).atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli()
}.getOrNull()

/** Formats the picker's selected epoch millis as the `YYYY-MM-DD` string the backend expects. */
private fun Long.toIsoDateString(): String =
    Instant.ofEpochMilli(this).atZone(ZoneOffset.UTC).toLocalDate().format(isoDateFormatter)

/** A segmented choice. The option vocabulary is passed in by the caller from the backend contract. */
@Composable
internal fun CountsSegmented(
    options: List<Pair<String, String>>,
    selectedKey: String,
    onSelect: (String) -> Unit,
    modifier: Modifier = Modifier,
    /**
     * Option keys that are visible but NOT choosable — drawn dimmed and not clickable.
     *
     * Added for the shifting tag toggle, where "use destination tag" has to stay VISIBLE on a pen that
     * cannot supply one (so the operator can see the choice exists and read why it is unavailable)
     * while being impossible to select. Hiding the option instead would make the control silently
     * change shape between pens, and leaving it tappable would let an operator pick something that
     * does nothing.
     *
     * A disabled key that is also the selected key still renders as selected: callers keep their
     * state valid, and drawing the current selection as absent would be a worse lie than showing a
     * dimmed one.
     */
    disabledKeys: Set<String> = emptySet(),
) {
    // Filled track with a selected pill — same coherent segmented look across all Counts screens.
    Row(
        modifier = modifier
            .fillMaxWidth()
            .heightIn(min = 54.dp)
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf2)
            .padding(5.dp),
        horizontalArrangement = Arrangement.spacedBy(5.dp),
    ) {
        options.forEach { (key, label) ->
            val selected = key == selectedKey
            val disabled = key in disabledKeys
            Box(
                modifier = Modifier
                    .weight(1f)
                    .clip(RoundedCornerShape(12.dp))
                    .background(if (selected) MeshaColors.Brand else Color.Transparent)
                    .let { base -> if (disabled) base else base.clickable { onSelect(key) } }
                    .padding(vertical = 13.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = label,
                    color = when {
                        selected -> MeshaColors.OnBrand
                        disabled -> MeshaColors.Muted.copy(alpha = 0.4f)
                        else -> MeshaColors.Muted
                    },
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
            .heightIn(min = 54.dp)
            .clip(RoundedCornerShape(16.dp))
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

/**
 * One entry in a [CountsDropdownField].
 *
 * [key] is the identity the screen emits; [label] is only ever rendered. Keeping them distinct is
 * load-bearing for shed vocabularies, whose labels repeat across parks while their keys are unique
 * — a name-keyed entry collapses two real sheds into one.
 *
 * [count] is an optional facet count rendered on the trailing edge ("CBE  951"). Null means the
 * vocabulary carries no count for this entry (the "All …" sentinel, or a picker whose catalog is
 * not a facet), and nothing is drawn.
 */
@Immutable
internal data class CountsDropdownOption(
    val key: String,
    val label: String,
    val count: Int? = null,
)

/**
 * A single-choice dropdown over a backend-supplied vocabulary.
 *
 * Shared by the Counts census filter bar and the shifting destination picker so both read and
 * behave identically — the module has exactly one dropdown control, not one per screen.
 *
 * Only the open/closed state is local, per the frontend contract: the option vocabulary, the
 * labels, the counts, and the selection all come from backend-owned state passed in above.
 */
@Composable
internal fun CountsDropdownField(
    label: String,
    selectedLabel: String?,
    placeholder: String,
    options: List<CountsDropdownOption>,
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
                    .heightIn(min = 54.dp)
                    .clip(RoundedCornerShape(14.dp))
                    .background(if (enabled) MeshaColors.Surf2 else MeshaColors.Surf3)
                    .clickable(enabled = enabled) { expanded = true }
                    .padding(horizontal = 14.dp, vertical = 14.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = selectedLabel ?: placeholder,
                    color = if (selectedLabel != null) MeshaColors.Ink else MeshaColors.Faint,
                    fontSize = 14.sp,
                    fontWeight = if (selectedLabel != null) FontWeight.W600 else FontWeight.W400,
                    // A long breed or shed label truncates rather than wrapping the field to two
                    // lines and shifting every control below it.
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
                // Capped height so a park with many sheds scrolls inside the menu instead of
                // rendering a list taller than the screen.
                modifier = Modifier.heightIn(max = 320.dp),
            ) {
                options.forEach { option ->
                    DropdownMenuItem(
                        text = {
                            Row(
                                modifier = Modifier.widthIn(min = 180.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.spacedBy(12.dp),
                            ) {
                                Text(
                                    text = option.label,
                                    fontSize = 14.sp,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f),
                                )
                                // The facet's own head count, rendered only when the vocabulary
                                // supplies one. It is the backend's number, never re-derived here.
                                option.count?.let { count ->
                                    Text(
                                        text = count.toString(),
                                        color = MeshaColors.Faint,
                                        fontSize = 12.sp,
                                        fontWeight = FontWeight.W600,
                                    )
                                }
                            }
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
