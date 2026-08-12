package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; AddBirthViewModel (in :app) owns the
// counts_birth_* AnalyticsEvents + the CrashReporter non-fatal on every enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType

/**
 * Add-birth form (`/counts/birth/add` — docs/decisions/birth-death-workflows.md §"Birth submit
 * deltas", mock's "Add" column). An L1 hosted destination reached ONLY from the Birth work list's
 * ＋ button.
 *
 * Deltas from the retired combined form: NO RFID scan and NO permanent/temporary toggle (the
 * server auto-generates a provisional `CBE-#####` or `CPT-#####` tag; the kid is tagged later via the "Tag the kid"
 * action → the existing promote flow); DOB is LOCKED to today (shown disabled); a NEW editable
 * time-of-birth field (HH:MM, prefilled to now IST); the entry-date field is REMOVED (the client
 * sends today); breed, mother RFID, and litter size are required; the park → shed cascade stays
 * required. The offline outbox submit/QUEUED/SYNCED banner behavior is unchanged.
 */

@Immutable
data class AddBirthUiState(
    val species: String = "goat",
    val sex: String = "female",
    /** Locked display value — today's business date (Asia/Kolkata), stamped by the VM. */
    val dob: String = "",
    /** Editable time of birth, `HH:MM` 24h IST; prefilled to now by the VM. */
    val timeOfBirth: String = "",
    val breed: String = "",
    val breedOptions: List<CountsFilterOptionUi> = emptyList(),
    val destinationParks: List<ShiftingParkUi> = emptyList(),
    val parkId: String = "",
    val shedId: String = "",
    /**
     * The pen within [shedId], or null for a shed-level placement. Carried alongside [shedId]
     * because the destinations feed returns one option PER PARTITION and shedId is therefore not
     * a unique key.
     */
    val partitionLabel: String? = null,
    val destinationsMessage: String? = null,
    val damId: String = "",
    val litterSize: Int = 1,
    val scanningMotherRfid: Boolean = false,
    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val lastRecordedMessage: String? = null,
    /** One-shot navigation signal set only after the server accepts the approval request. */
    val returnToBirthList: Boolean = false,
    /** Backend-result-aware acknowledgement handed to the parent Birth list. */
    val submissionNotice: String? = null,
) {
    val shedsForSelectedPark: List<ShiftingShedUi>
        get() = destinationParks.firstOrNull { it.parkId == parkId }?.sheds.orEmpty()

    /** The dropdown key for the current exact shed selection. */
    val shedOptionKey: String
        get() = shedId
}

sealed interface AddBirthEvent {
    data class EditField(val field: AddBirthField, val value: String) : AddBirthEvent
    data class SelectPark(val parkId: String) : AddBirthEvent
    data class SelectShed(val shedId: String) : AddBirthEvent
    data class SelectLitterSize(val litterSize: Int) : AddBirthEvent
    data object ToggleMotherRfidScan : AddBirthEvent
    data object Submit : AddBirthEvent
    data object RecordAnother : AddBirthEvent
    data object NavigationHandled : AddBirthEvent
    data object Back : AddBirthEvent
}

/** Editable fields. DOB is deliberately absent — it is locked to today. */
enum class AddBirthField { SPECIES, SEX, TIME_OF_BIRTH, BREED, DAM_ID }

@Composable
fun AddBirthScreen(
    state: AddBirthUiState,
    onEvent: (AddBirthEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = stringResource(R.string.counts_workflow_add_birth),
            subtitle = stringResource(R.string.counts_add_birth_subtitle),
            onBack = { onEvent(AddBirthEvent.Back) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp),
            contentPadding = PaddingValues(bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { CountsResultBanner(state.result) }
            state.lastRecordedMessage?.let { message ->
                item(key = "recorded") {
                    CountsResultBanner(CountsWriteResultUi(CountsWriteStatus.SYNCED, message))
                }
            }

            // Identity — species/sex segmented; the provisional tag is SERVER-generated, so there
            // is no kid identifier input. Breed is visible and required.
            item(key = "identity") {
                AddFormGroupCard(title = stringResource(R.string.counts_group_identity)) {
                    Text(
                        text = stringResource(R.string.counts_add_birth_tag_note),
                        color = MeshaColors.Faint,
                        style = MeshaType.cardSubtitle,
                    )
                    CountsSegmented(
                        options = listOf(
                            "goat" to stringResource(R.string.counts_species_goat),
                            "sheep" to stringResource(R.string.counts_species_sheep),
                        ),
                        selectedKey = state.species,
                        onSelect = { onEvent(AddBirthEvent.EditField(AddBirthField.SPECIES, it)) },
                    )
                    CountsSegmented(
                        options = listOf(
                            "female" to stringResource(R.string.counts_sex_female),
                            "male" to stringResource(R.string.counts_sex_male),
                        ),
                        selectedKey = state.sex,
                        onSelect = { onEvent(AddBirthEvent.EditField(AddBirthField.SEX, it)) },
                    )
                    val selectedBreedLabel = state.breedOptions.firstOrNull { it.key == state.breed }?.label
                    CountsDropdownField(
                        label = stringResource(R.string.counts_field_breed_required),
                        selectedLabel = selectedBreedLabel,
                        placeholder = stringResource(R.string.counts_select_breed),
                        options = state.breedOptions.map { CountsDropdownOption(it.key, it.label, it.count) },
                        onSelect = { onEvent(AddBirthEvent.EditField(AddBirthField.BREED, it)) },
                        enabled = state.breedOptions.isNotEmpty(),
                    )
                }
            }

            // Born — DOB locked to today (births are recorded as they happen); editable HH:MM time.
            item(key = "born") {
                AddFormGroupCard(title = stringResource(R.string.counts_group_born)) {
                    CountsTextField(
                        value = state.dob,
                        onValueChange = {},
                        label = stringResource(R.string.counts_field_dob),
                        readOnly = true,
                        supporting = stringResource(R.string.counts_add_birth_dob_hint),
                    )
                    CountsTextField(
                        value = state.timeOfBirth,
                        onValueChange = { onEvent(AddBirthEvent.EditField(AddBirthField.TIME_OF_BIRTH, it)) },
                        label = stringResource(R.string.counts_field_time_of_birth),
                        required = true,
                        supporting = stringResource(R.string.counts_add_birth_time_hint),
                    )
                    Text(
                        text = stringResource(R.string.counts_field_litter_size),
                        color = MeshaColors.Muted,
                        style = MeshaType.pillStrong,
                    )
                    CountsSegmented(
                        options = listOf(
                            "1" to stringResource(R.string.counts_litter_single),
                            "2" to stringResource(R.string.counts_litter_twins),
                            "3" to stringResource(R.string.counts_litter_triplets),
                        ),
                        selectedKey = state.litterSize.toString(),
                        onSelect = { value -> value.toIntOrNull()?.let { onEvent(AddBirthEvent.SelectLitterSize(it)) } },
                    )
                }
            }

            // Placement — mother RFID and the park -> shed cascade are required.
            item(key = "placement") {
                AddFormGroupCard(title = stringResource(R.string.counts_group_placement)) {
                    CountsRfidField(
                        value = state.damId,
                        onValueChange = { onEvent(AddBirthEvent.EditField(AddBirthField.DAM_ID, it)) },
                        label = stringResource(R.string.counts_field_mother_rfid),
                        scanning = state.scanningMotherRfid,
                        onToggleScan = { onEvent(AddBirthEvent.ToggleMotherRfidScan) },
                        required = true,
                        supporting = stringResource(R.string.counts_mother_rfid_hint),
                    )
                    val selectedPark = state.destinationParks.firstOrNull { it.parkId == state.parkId }
                    CountsDropdownField(
                        label = stringResource(R.string.counts_field_park),
                        selectedLabel = selectedPark?.name,
                        placeholder = stringResource(R.string.counts_select_farm),
                        options = state.destinationParks.map { CountsDropdownOption(it.parkId, it.name) },
                        onSelect = { onEvent(AddBirthEvent.SelectPark(it)) },
                        enabled = state.destinationParks.isNotEmpty(),
                    )
                    val sheds = state.shedsForSelectedPark
                    // Keyed on exact shed id. Partitions are sheds in the destination feed.
                    val selectedShed = sheds.firstOrNull { it.optionKey == state.shedOptionKey }
                    CountsDropdownField(
                        label = stringResource(R.string.counts_field_shed),
                        selectedLabel = selectedShed?.displayLabel,
                        placeholder = if (state.parkId.isBlank()) {
                            stringResource(R.string.counts_select_farm_first)
                        } else {
                            stringResource(R.string.counts_select_shed)
                        },
                        options = sheds.map { CountsDropdownOption(it.optionKey, it.displayLabel) },
                        onSelect = { onEvent(AddBirthEvent.SelectShed(it)) },
                        enabled = sheds.isNotEmpty(),
                    )
                    state.destinationsMessage?.let { message ->
                        Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
                    }
                }
            }

            item(key = "workflow-note") {
                Text(
                    text = stringResource(R.string.counts_add_birth_workflow_note),
                    color = MeshaColors.Faint,
                    style = MeshaType.cardSubtitle,
                )
            }
            state.validationMessage?.let { message ->
                item(key = "validation") {
                    Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
                }
            }
            item(key = "submit") {
                val committed = state.result.status == CountsWriteStatus.QUEUED ||
                    state.result.status == CountsWriteStatus.SYNCED
                if (committed) {
                    CountsSubmitButton(
                        label = stringResource(R.string.counts_record_another_birth),
                        enabled = true,
                        onClick = { onEvent(AddBirthEvent.RecordAnother) },
                    )
                } else {
                    CountsSubmitButton(
                        label = stringResource(R.string.counts_submit_birth),
                        enabled = state.canSubmit,
                        onClick = { onEvent(AddBirthEvent.Submit) },
                    )
                }
            }
            item(key = "offline-note") {
                Text(
                    text = stringResource(R.string.counts_offline_note),
                    color = MeshaColors.Faint,
                    style = MeshaType.cardSubtitle,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}

/** A titled card grouping related fields — the same shape the retired combined form used. */
@Composable
internal fun AddFormGroupCard(
    title: String,
    content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        CountsFieldGroupTitle(text = title)
        content()
    }
}

/** Collapses an optional field group under an on-demand expander. */
@Composable
internal fun AddFormExpander(expanded: Boolean, onToggle: () -> Unit, label: String) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onToggle)
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = label,
            color = MeshaColors.Muted,
            style = MeshaType.pillStrong,
            modifier = Modifier.weight(1f),
        )
        Icon(
            imageVector = MeshaIcons.ChevronDown,
            contentDescription = null,
            tint = MeshaColors.Muted,
            modifier = Modifier.size(16.dp),
        )
    }
}
