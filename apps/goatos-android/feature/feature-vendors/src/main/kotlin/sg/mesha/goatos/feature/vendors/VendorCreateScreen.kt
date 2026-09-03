package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; VendorCreateViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for the capture and the queued write.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaDimens
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Add a vendor (L1 drill): THREE short steps rather than one long scroll, each a card of the
 * fields that belong together.
 *
 *   1. Who      business, type, contact, phone
 *   2. Where    state, city, status
 *   3. Supply   capacity + how often, feed/breed, price, lead time, a typed note, a voice note
 *
 * Payment instruments are never entered on the phone. Validation is per step, shown under the
 * box, and the primary button reads "Next" until the last step, where it reads "Save vendor".
 */
@Composable
fun VendorCreateScreen(
    state: VendorCreateUiState,
    onEvent: (VendorCreateEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    val locked = state.writeStatus == VendorsWriteStatus.QUEUED || state.writeStatus == VendorsWriteStatus.SYNCED
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        MeshaScreenHeader(
            title = TITLE,
            subtitle = STEP_TITLES.getOrNull(state.step),
            onBack = { onEvent(VendorCreateEvent.Back) },
        )
        VendorsStepper(stepCount = state.stepCount, currentIndex = state.step, caption = "Step ${state.step + 1} of ${state.stepCount}")
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            state.message?.let { message ->
                item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) }
            }
            if (!locked) {
                when (state.step) {
                    0 -> item(key = "who") { WhoStep(state, onEvent) }
                    1 -> item(key = "where") { WhereStep(state, onEvent) }
                    else -> item(key = "supply") { SupplyStep(state, onEvent) }
                }
            }
        }
        VendorsWizardBar(contextLine = state.contextLine) {
            if (locked) {
                VendorsPrimaryButton(label = RECORD_ANOTHER, enabled = true, onClick = { onEvent(VendorCreateEvent.RecordAnother) }, modifier = Modifier.weight(1f))
            } else {
                if (state.step > 0) {
                    VendorsGhostButton(label = PREVIOUS, onClick = { onEvent(VendorCreateEvent.Previous) })
                }
                val last = state.step == state.stepCount - 1
                VendorsPrimaryButton(
                    label = if (last) SAVE else NEXT,
                    enabled = !state.submitInFlight && state.voiceNote != VoiceNoteSlotState.WORKING,
                    onClick = { onEvent(if (last) VendorCreateEvent.Submit else VendorCreateEvent.Next) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

@Composable
private fun WhoStep(state: VendorCreateUiState, onEvent: (VendorCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = STEP_TITLES[0]) {
        VendorsTextField(v[VendorField.BUSINESS_NAME].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.BUSINESS_NAME, it)) }, LABEL_BUSINESS, required = true, error = e[VendorField.BUSINESS_NAME])
        VendorsDropdownField(LABEL_TYPE, v[VendorField.RECORD_TYPE].orEmpty(), state.recordTypes, { onEvent(VendorCreateEvent.FieldChanged(VendorField.RECORD_TYPE, it)) }, required = true, error = e[VendorField.RECORD_TYPE], placeholder = HINT_PICK)
        VendorsTextField(v[VendorField.CONTACT_PERSON].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.CONTACT_PERSON, it)) }, LABEL_CONTACT, required = true, error = e[VendorField.CONTACT_PERSON])
        VendorsTextField(v[VendorField.PHONE].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.PHONE, it)) }, LABEL_PHONE, required = true, keyboard = KeyboardType.Phone, error = e[VendorField.PHONE])
    }
}

@Composable
private fun WhereStep(state: VendorCreateUiState, onEvent: (VendorCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = STEP_TITLES[1]) {
        VendorsDropdownField(LABEL_STATE, v[VendorField.STATE].orEmpty(), state.states, { onEvent(VendorCreateEvent.FieldChanged(VendorField.STATE, it)) }, required = true, error = e[VendorField.STATE], placeholder = HINT_PICK)
        VendorsTextField(v[VendorField.CITY].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.CITY, it)) }, LABEL_CITY, required = true, error = e[VendorField.CITY])
        Text(text = LABEL_STATUS, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
        VendorsSegmented(options = state.statuses, selectedValue = v[VendorField.STATUS].orEmpty(), onSelect = { onEvent(VendorCreateEvent.FieldChanged(VendorField.STATUS, it)) })
    }
}

@Composable
private fun SupplyStep(state: VendorCreateUiState, onEvent: (VendorCreateEvent) -> Unit) {
    val v = state.values
    val e = state.fieldErrors
    VendorsFormGroup(title = LABEL_CAPACITY) {
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsTextField(
                v[VendorField.CAPACITY_QUANTITY].orEmpty(),
                { onEvent(VendorCreateEvent.FieldChanged(VendorField.CAPACITY_QUANTITY, it)) },
                LABEL_CAPACITY_QUANTITY,
                keyboard = KeyboardType.Decimal,
                error = e[VendorField.CAPACITY_QUANTITY],
                modifier = Modifier.weight(3f),
            )
            VendorsDropdownField(
                LABEL_CAPACITY_UNIT,
                v[VendorField.CAPACITY_UNIT].orEmpty(),
                state.capacityUnits,
                { onEvent(VendorCreateEvent.FieldChanged(VendorField.CAPACITY_UNIT, it)) },
                error = e[VendorField.CAPACITY_UNIT],
                modifier = Modifier.weight(2f),
                allowClear = true,
                clearLabel = CLEAR,
            )
        }
        VendorsDropdownField(LABEL_FREQUENCY, v[VendorField.SUPPLY_FREQUENCY].orEmpty(), state.supplyFrequencies, { onEvent(VendorCreateEvent.FieldChanged(VendorField.SUPPLY_FREQUENCY, it)) }, placeholder = HINT_OPTIONAL, allowClear = true, clearLabel = CLEAR)
        Text(text = HINT_CAPACITY, color = MeshaColors.Muted, style = MeshaType.caption)
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_DEALS_IN) {
        VendorsDropdownField(LABEL_FEED, v[VendorField.FEED].orEmpty(), state.feeds, { onEvent(VendorCreateEvent.FieldChanged(VendorField.FEED, it)) }, placeholder = HINT_OPTIONAL, allowClear = true, clearLabel = CLEAR)
        VendorsDropdownField(LABEL_BREED, v[VendorField.BREED].orEmpty(), state.breeds, { onEvent(VendorCreateEvent.FieldChanged(VendorField.BREED, it)) }, placeholder = HINT_OPTIONAL, allowClear = true, clearLabel = CLEAR)
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            VendorsTextField(v[VendorField.PRICE_PER_GOAT].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.PRICE_PER_GOAT, it)) }, LABEL_PRICE, keyboard = KeyboardType.Decimal, error = e[VendorField.PRICE_PER_GOAT], modifier = Modifier.weight(1f))
            VendorsTextField(v[VendorField.ETA_DAYS].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.ETA_DAYS, it)) }, LABEL_ETA, keyboard = KeyboardType.Number, error = e[VendorField.ETA_DAYS], modifier = Modifier.weight(1f))
        }
    }
    Spacer(Modifier.height(10.dp))
    VendorsFormGroup(title = LABEL_NOTES) {
        VendorsTextField(v[VendorField.NOTE].orEmpty(), { onEvent(VendorCreateEvent.FieldChanged(VendorField.NOTE, it)) }, LABEL_NOTE, singleLine = false, supporting = HINT_NOTE)
        VoiceNoteSlot(state = state.voiceNote, length = state.voiceNoteLength, onEvent = onEvent)
    }
}

/** The voice-note capture slot: empty -> record; recorded -> re-record or remove; failed -> retry. */
@Composable
private fun VoiceNoteSlot(state: VoiceNoteSlotState, length: String, onEvent: (VendorCreateEvent) -> Unit) {
    val (border, iconBg, iconTint) = when (state) {
        VoiceNoteSlotState.EMPTY -> Triple(MeshaColors.Hair, MeshaColors.Surf3, MeshaColors.Ink)
        VoiceNoteSlotState.WORKING -> Triple(MeshaColors.Brand, MeshaColors.BrandTint, MeshaColors.BrandD)
        VoiceNoteSlotState.RECORDED -> Triple(MeshaColors.Ok, MeshaColors.OkX, MeshaColors.Ok)
        VoiceNoteSlotState.FAILED -> Triple(MeshaColors.Danger, MeshaColors.DangerX, MeshaColors.Danger)
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(MeshaDimens.radiusInput))
            .background(MeshaColors.Surf2)
            .clickable(enabled = state != VoiceNoteSlotState.WORKING, role = Role.Button) { onEvent(VendorCreateEvent.RecordVoiceNote) }
            .padding(12.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Box(
            modifier = Modifier.size(40.dp).clip(RoundedCornerShape(MeshaDimens.radiusIcon)).background(iconBg),
            contentAlignment = Alignment.Center,
        ) {
            if (state == VoiceNoteSlotState.WORKING) {
                CircularProgressIndicator(color = iconTint, modifier = Modifier.size(MeshaDimens.iconMd))
            } else {
                Icon(if (state == VoiceNoteSlotState.RECORDED) MeshaIcons.Check else MeshaIcons.Microphone, contentDescription = null, tint = iconTint, modifier = Modifier.size(MeshaDimens.iconMd))
            }
        }
        Column(Modifier.weight(1f)) {
            Text(
                text = when (state) {
                    VoiceNoteSlotState.EMPTY -> VOICE_RECORD
                    VoiceNoteSlotState.WORKING -> VOICE_SAVING
                    VoiceNoteSlotState.RECORDED -> VOICE_RECORDED + if (length.isNotBlank()) " · $length" else ""
                    VoiceNoteSlotState.FAILED -> VOICE_FAILED
                },
                color = if (state == VoiceNoteSlotState.FAILED) MeshaColors.Danger else MeshaColors.Ink,
                style = MeshaType.bodyStrong,
            )
            Text(
                text = if (state == VoiceNoteSlotState.RECORDED) VOICE_RERECORD_HINT else VOICE_HINT,
                color = MeshaColors.Muted,
                style = MeshaType.caption,
            )
        }
        if (state == VoiceNoteSlotState.RECORDED) {
            Text(
                text = REMOVE,
                color = MeshaColors.Danger,
                style = MeshaType.pillStrong,
                modifier = Modifier
                    .minimumInteractiveComponentSize()
                    .clip(MeshaDimens.pill)
                    .border(MeshaDimens.hairline, border, MeshaDimens.pill)
                    .clickable(role = Role.Button) { onEvent(VendorCreateEvent.RemoveVoiceNote) }
                    .padding(horizontal = 10.dp, vertical = 6.dp),
            )
        }
    }
}

private const val TITLE = "Add vendor"
private val STEP_TITLES = listOf("Who they are", "Where they are", "What they supply")
private const val NEXT = "Next"
private const val PREVIOUS = "Back"
private const val SAVE = "Save vendor"
private const val RECORD_ANOTHER = "Add another vendor"
private const val CLEAR = "Not recorded"
private const val LABEL_BUSINESS = "Business name"
private const val LABEL_TYPE = "Vendor type"
private const val LABEL_CONTACT = "Contact person"
private const val LABEL_PHONE = "Phone number"
private const val LABEL_STATE = "State"
private const val LABEL_CITY = "City"
private const val LABEL_STATUS = "Status"
private const val LABEL_CAPACITY = "Capacity"
private const val LABEL_CAPACITY_QUANTITY = "How much per delivery"
private const val LABEL_CAPACITY_UNIT = "Unit"
private const val LABEL_FREQUENCY = "How often"
private const val LABEL_DEALS_IN = "Deals in"
private const val LABEL_FEED = "Feed"
private const val LABEL_BREED = "Breed"
private const val LABEL_PRICE = "Price per goat (₹)"
private const val LABEL_ETA = "Lead time (days)"
private const val LABEL_NOTES = "Notes"
private const val LABEL_NOTE = "Note"
private const val HINT_PICK = "Tap to choose"
private const val HINT_OPTIONAL = "Optional"
private const val HINT_CAPACITY = "Optional. Fill in whatever is known."
private const val HINT_NOTE = "Anything the desk should remember about this vendor."
private const val VOICE_RECORD = "Record a voice note"
private const val VOICE_SAVING = "Saving voice note…"
private const val VOICE_RECORDED = "Voice note recorded"
private const val VOICE_FAILED = "Voice note not saved"
private const val VOICE_HINT = "Speak instead of typing. Optional."
private const val VOICE_RERECORD_HINT = "Tap to record again"
private const val REMOVE = "Remove"
