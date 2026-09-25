package sg.mesha.goatos.feature.vendors

// telemetry:exempt pure stateless renderer; VendorCreateViewModel (in :app) owns the vendors_*
// AnalyticsEventsVendors + CrashReporter wiring for the capture and the queued write.

import androidx.activity.compose.BackHandler
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
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
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
    // System Back on a later step goes to the step before, like the Back button beside Next: one
    // stray press on step 3 used to drop the whole typed record with no way back.
    BackHandler(enabled = state.step > 0 && !locked) { onEvent(VendorCreateEvent.Previous) }
    LaunchedEffect(state.closeAfterSave) {
        if (state.closeAfterSave) {
            delay(CLOSE_AFTER_SAVE_MS)
            onEvent(VendorCreateEvent.Back)
        }
    }
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        val formDriven = state.form.isNotEmpty()
        MeshaScreenHeader(
            title = if (state.isEditing) TITLE_EDIT else TITLE,
            // The step's heading is the published page's title (backend words) when the form drives the wizard.
            subtitle = if (formDriven) state.form.getOrNull(state.step)?.title?.ifBlank { null } else STEP_TITLES.getOrNull(state.step),
            onBack = { onEvent(VendorCreateEvent.Back) },
        )
        VendorsStepper(stepCount = state.stepCount, currentIndex = state.step, caption = "Step ${state.step + 1} of ${state.stepCount}")
        LazyColumn(
            state = rememberStepListState(state.step),
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = MeshaDimens.gutter, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { VendorsResultBanner(status = state.writeStatus, message = state.writeMessage) }
            state.message?.let { message ->
                item(key = "message") { VendorsResultBanner(status = VendorsWriteStatus.FAILED, message = message) }
            }
            if (formDriven) {
                // VENDOR FORM IS AUTHORED (2026-09-19): one step per published page, one control
                // per question by kind; the voice-note slot stays on the last page.
                val page = state.form[state.step.coerceIn(0, state.form.size - 1)]
                item(key = "page-" + page.key) {
                    AuthoredFormPage(
                        page = page,
                        answers = state.answers,
                        answerErrors = state.answerErrors,
                        onAnswer = { id, value -> onEvent(VendorCreateEvent.AnswerChanged(id, value)) },
                        fallbackTitle = "Step ${state.step + 1}",
                        trailing = if (state.step == state.form.size - 1) {
                            { VoiceNoteSlot(state = state.voiceNote, length = state.voiceNoteLength, onEvent = onEvent) }
                        } else {
                            null
                        },
                    )
                }
            } else {
                when (state.step) {
                    0 -> item(key = "who") { WhoStep(state, onEvent) }
                    1 -> item(key = "where") { WhereStep(state, onEvent) }
                    else -> item(key = "supply") { SupplyStep(state, onEvent) }
                }
            }
        }
        VendorsWizardBar(contextLine = state.contextLine) {
            if (locked) {
                // The record is on its way or landed; the banner above says which. Nothing to press:
                // the screen closes on its own once the write is durable (CLOSE_AFTER_SAVE_MS).
                // "Saved" keeps the brand green so the success reads at a glance; "Saving…" is muted.
                val saved = state.writeStatus == VendorsWriteStatus.SYNCED
                VendorsPrimaryButton(label = if (saved) SAVED else SAVING, enabled = saved, onClick = {}, modifier = Modifier.weight(1f))
            } else {
                if (state.step > 0) {
                    VendorsGhostButton(label = PREVIOUS, onClick = { onEvent(VendorCreateEvent.Previous) })
                }
                val last = state.step == state.stepCount - 1
                VendorsPrimaryButton(
                    label = if (last) (if (state.isEditing) SAVE_CHANGES else SAVE) else NEXT,
                    enabled = !state.submitInFlight && state.voiceNote != VoiceNoteSlotState.WORKING,
                    onClick = { onEvent(if (last) VendorCreateEvent.Submit else VendorCreateEvent.Next) },
                    modifier = Modifier.weight(1f),
                )
            }
        }
    }
}

/**
 * One published page: its questions in order, each drawn by kind; hidden ones (ask only when)
 * skipped.
 *
 * SHARED BY BOTH ENTRY FORMS (PROCUREMENT IS SOP-DRIVEN END TO END, 2026-09-20). It takes the
 * answers, their errors and one callback rather than a wizard's whole state, so the feed purchase
 * wizard renders its authored questions through the SAME renderer the vendor wizard does -- a
 * second copy would drift on the next question kind, and the kinds are the SOP's, not a screen's.
 */
@Composable
internal fun AuthoredFormPage(
    page: VendorFormPageUi,
    answers: Map<String, String>,
    answerErrors: Map<String, String>,
    onAnswer: (String, String) -> Unit,
    fallbackTitle: String,
    trailing: (@Composable () -> Unit)? = null,
) {
    val a = answers
    val e = answerErrors
    VendorsFormGroup(title = page.title.ifBlank { fallbackTitle }) {
        if (page.hint.isNotBlank()) Text(text = page.hint, color = MeshaColors.Muted, style = MeshaType.caption)
        for (q in page.questions) {
            val asked = q.onlyIfQuestion.isBlank() || (q.onlyIfValue.isNotBlank() && a.containsKey(q.onlyIfQuestion) && a[q.onlyIfQuestion].orEmpty().trim() == q.onlyIfValue)
            if (!asked) continue
            val value = a[q.id].orEmpty()
            val change: (String) -> Unit = { onAnswer(q.id, it) }
            when (q.kind) {
                VendorQuestionKind.CHOICE -> {
                    if (q.id == QUESTION_STATUS && q.options.size in 2..4 && !q.allowOther) {
                        Text(text = q.title, color = MeshaColors.Muted, style = MeshaType.fieldLabel)
                        VendorsSegmented(options = q.options, selectedValue = value, onSelect = change)
                    } else {
                        VendorsDropdownField(
                            q.title, value, q.options, change,
                            required = q.required, error = e[q.id],
                            placeholder = if (q.required) HINT_PICK else HINT_OPTIONAL,
                            allowClear = !q.required, clearLabel = CLEAR,
                        )
                    }
                    if (q.allowOther && value == OTHER_VALUE) {
                        VendorsTextField(a[q.id + OTHER_TEXT_SUFFIX].orEmpty(), { onAnswer(q.id + OTHER_TEXT_SUFFIX, it) }, HINT_OTHER, required = true)
                    }
                    if (q.hint.isNotBlank()) Text(text = q.hint, color = MeshaColors.Muted, style = MeshaType.caption)
                }
                VendorQuestionKind.MULTI -> {
                    Text(text = q.title + if (q.required) " *" else "", color = MeshaColors.Muted, style = MeshaType.fieldLabel)
                    val chosen = value.split('|').map { it.trim() }.filter { it.isNotBlank() }.toSet()
                    for (o in q.options) {
                        val on = o.value in chosen
                        Row(
                            modifier = Modifier
                                .fillMaxWidth()
                                .clip(RoundedCornerShape(MeshaDimens.radiusInput))
                                .background(if (on) MeshaColors.BrandTint else MeshaColors.Surf2)
                                .clickable(role = Role.Checkbox) {
                                    val next = if (on) chosen - o.value else chosen + o.value
                                    change(q.options.map { it.value }.filter { it in next }.joinToString("|"))
                                }
                                .padding(horizontal = 12.dp, vertical = 10.dp),
                            verticalAlignment = Alignment.CenterVertically,
                            horizontalArrangement = Arrangement.spacedBy(10.dp),
                        ) {
                            Icon(if (on) MeshaIcons.Check else MeshaIcons.Plus, contentDescription = null, tint = if (on) MeshaColors.BrandD else MeshaColors.Muted, modifier = Modifier.size(MeshaDimens.iconSm))
                            Text(text = o.label, color = MeshaColors.Ink, style = MeshaType.body)
                        }
                    }
                    e[q.id]?.let { Text(text = it, color = MeshaColors.Danger, style = MeshaType.caption) }
                    if (q.hint.isNotBlank()) Text(text = q.hint, color = MeshaColors.Muted, style = MeshaType.caption)
                }
                VendorQuestionKind.NUMBER -> VendorsTextField(
                    value, change, if (q.unit.isNotBlank()) "${q.title} (${q.unit})" else q.title,
                    required = q.required, keyboard = KeyboardType.Decimal, error = e[q.id],
                    supporting = q.hint.ifBlank { null },
                )
                VendorQuestionKind.TEXT -> VendorsTextField(
                    value, change, q.title,
                    required = q.required,
                    keyboard = if (q.id == QUESTION_PHONE) KeyboardType.Phone else KeyboardType.Text,
                    error = e[q.id],
                    supporting = q.hint.ifBlank { null },
                    singleLine = q.id != QUESTION_COMMENTS && q.id != QUESTION_DETAILS,
                )
            }
        }
        trailing?.invoke()
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
        // Optional (maintainer decision 2026-09-08): what size of animal this buyer expects. A plain
        // recorded value, connected to nothing else; blank means "not recorded", never 0.
        VendorsTextField(
            v[VendorField.AVERAGE_ANIMAL_WEIGHT].orEmpty(),
            { onEvent(VendorCreateEvent.FieldChanged(VendorField.AVERAGE_ANIMAL_WEIGHT, it)) },
            LABEL_AVERAGE_WEIGHT,
            keyboard = KeyboardType.Decimal,
            error = e[VendorField.AVERAGE_ANIMAL_WEIGHT],
            supporting = HINT_AVERAGE_WEIGHT,
        )
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
private const val TITLE_EDIT = "Edit vendor"
private const val SAVE_CHANGES = "Save changes"
private val STEP_TITLES = listOf("Who they are", "Where they are", "What they supply")
private const val NEXT = "Next"
private const val PREVIOUS = "Back"
private const val SAVE = "Save vendor"
/** How long the saved banner stays on the finished form before the screen closes itself. */
private const val CLOSE_AFTER_SAVE_MS = 2_000L
private const val SAVING = "Saving…"
private const val SAVED = "Saved"
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
private const val LABEL_AVERAGE_WEIGHT = "Average animal weight (kg)"
private const val HINT_AVERAGE_WEIGHT = "Optional. The weight per animal they expect."
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
private const val HINT_OTHER = "Say what the other is"
private const val OTHER_VALUE = "other"
private const val OTHER_TEXT_SUFFIX = "_other"
private const val QUESTION_STATUS = "status"
private const val QUESTION_PHONE = "phone_number"
private const val QUESTION_COMMENTS = "comments"
private const val QUESTION_DETAILS = "details"
