package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; BirthDeathViewModel owns the counts_birth_* /
// counts_death_* analytics events and the CrashReporter non-fatal on every enqueue failure.

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
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.time.Clock
import java.time.LocalDate
import java.time.ZoneId
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.operationalLocationLabel

/**
 * Record a birth or a death (`/counts/birth-death`) — a hosted destination with Up/Back.
 *
 * One screen, two mutually exclusive events, because they are the two count-DECREASING/INCREASING
 * lifecycle facts an operator reports from the same moment in the shed. They are NOT merged into
 * one request: each posts to its own backend route with its own guardrails.
 *
 * Both modes are SELECTOR/SCAN-driven, never free-text UUID entry:
 *  - Birth PLACEMENT is a park -> shed cascade from the shifting-destinations catalog, the SAME
 *    picker the Shifting screen uses (names shown, ids submitted). An operator knows a shed by name,
 *    not by UUID, and placement is required so a newborn can never be recorded into no shed.
 *  - Death TARGET is a tag/RFID search -> select-one-animal step. The selected animal carries its
 *    own `row_version`; the operator never types a record version or an internal goat id. The
 *    animal's tag, current park/shed, sex, and status are shown so the operator confirms the right
 *    animal before recording an irreversible death.
 *
 * Medical-safety note carried into the UI: a death is recorded through identity's guardrailed
 * critical-death exit, where `lifecycle_status=dead` + `exit_reason=died` is a fixed pairing the
 * SERVER enforces. This screen therefore never offers those as editable choices — showing them as
 * a dropdown would imply an operator could record a death as something else.
 */

// ---------------------------------------------------------------------------
// State + events
// ---------------------------------------------------------------------------

enum class BirthDeathMode { BIRTH, DEATH }

/** Whether the newborn's tag is a permanent RFID or a provisional temporary tag. */
const val BIRTH_ID_KIND_PERMANENT = "permanent"
const val BIRTH_ID_KIND_TEMPORARY = "temporary"

/**
 * Draft fields for both modes.
 *
 * Free-text fields stay plain strings so a partially-typed value is never silently coerced — a
 * blank numeric field stays blank (and blocks submit) rather than becoming `0`, which the
 * authored-config rule in AGENTS.md forbids. Placement (park/shed) and the death target are NOT
 * free text: they are ids chosen from backend-owned vocabularies, so a mis-placed goat or a
 * hand-guessed record version is impossible.
 */
@Immutable
data class BirthDeathUiState(
    val mode: BirthDeathMode = BirthDeathMode.BIRTH,
    // Birth — identity + dates
    /** [BIRTH_ID_KIND_PERMANENT] (RFID) or [BIRTH_ID_KIND_TEMPORARY] (provisional tag). */
    val idKind: String = BIRTH_ID_KIND_PERMANENT,
    val tag: String = "",
    /**
     * An optional SECOND permanent RFID (animal_identifier_2) for the permanent-RFID path only — a
     * newborn given two ear tags. Blank = attach only the primary. Only sent in
     * [BIRTH_ID_KIND_PERMANENT] mode; a temporary tag never carries a second permanent RFID.
     */
    val tag2: String = "",
    /**
     * Which permanent-RFID field is currently listening to the Bluetooth reader
     * ([BirthDeathField.TAG] or [BirthDeathField.TAG2]), or null when nothing is scanning. Only one
     * field at a time — Android holds a single BT-HID connection either way.
     */
    val scanningField: BirthDeathField? = null,
    val species: String = "goat",
    val sex: String = "female",
    // Breed is CHOSEN from the herd's own backend-supplied breed vocabulary (the same Room-cached
    // Counts facet the census filter uses), never typed. [breed] holds the selected facet key.
    val breed: String = "",
    val breedOptions: List<CountsFilterOptionUi> = emptyList(),
    val dob: String = "",
    // Entry date (the day this record is made). Defaults to today's business date (stamped by the
    // ViewModel) but is editable via the M3 date picker. The backend enforces dob <= entry_date.
    val entryDate: String = "",
    // Birth — placement (park -> shed cascade, ids chosen from the destinations catalog)
    val destinationParks: List<ShiftingParkUi> = emptyList(),
    val parkId: String = "",
    val shedId: String = "",
    /** Set when the destination catalog could not be loaded and no cached copy exists. */
    val destinationsMessage: String? = null,
    val damId: String = "",
    // Death — tag/RFID search -> single animal selection (carries goat_id + row_version)
    val animalQuery: String = "",
    val animalMatches: List<ShiftingAnimalUi> = emptyList(),
    val isLookingUpAnimals: Boolean = false,
    /** Lookup outcome copy (no match / lookup failed). Null while idle or successful. */
    val animalLookupMessage: String? = null,
    /** THE animal whose death is being recorded. Exactly one, or none. */
    val selectedAnimal: ShiftingAnimalUi? = null,
    val reason: String = "",
    // Shared
    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    /**
     * Transient success confirmation shown after a synced write auto-clears the form, so the operator
     * sees the record landed on a fresh form. Cleared when they start the next entry.
     */
    val lastRecordedMessage: String? = null,
) {
    /** The sheds of the currently chosen park — the second placement dropdown's whole option set. */
    val shedsForSelectedPark: List<ShiftingShedUi>
        get() = destinationParks.firstOrNull { it.parkId == parkId }?.sheds.orEmpty()
}

sealed interface BirthDeathEvent {
    data class SelectMode(val mode: BirthDeathMode) : BirthDeathEvent
    data class EditField(val field: BirthDeathField, val value: String) : BirthDeathEvent

    /**
     * Start/stop the Bluetooth RFID reader for one permanent-identifier field
     * ([BirthDeathField.TAG] / [BirthDeathField.TAG2]). Tapping the field that is already scanning
     * stops it; tapping the other one hands the reader over.
     */
    data class ToggleRfidScan(val field: BirthDeathField) : BirthDeathEvent

    // Birth placement — park -> shed cascade. Choosing a park resets the shed.
    data class SelectPark(val parkId: String) : BirthDeathEvent
    data class SelectShed(val shedId: String) : BirthDeathEvent

    // Death target — tag/RFID search then single selection.
    data class EditAnimalQuery(val value: String) : BirthDeathEvent
    data object LookupAnimals : BirthDeathEvent
    data class SelectAnimal(val goatId: String) : BirthDeathEvent

    data object Submit : BirthDeathEvent

    /** Reset the form after a committed write so the operator can record the next animal. */
    data object RecordAnother : BirthDeathEvent
    data object Back : BirthDeathEvent
}

/**
 * Editable birth/death fields. Placement, the death target, and breed are NOT free text — they are
 * chosen from backend-owned vocabularies. Entry date is not here either: it is stamped
 * automatically to the day the entry is recorded (see [BirthDeathViewModel]).
 */
enum class BirthDeathField {
    ID_KIND, TAG, TAG2, SPECIES, SEX, BREED, DOB, ENTRY_DATE, DAM_ID, REASON,
}

/** The zone the whole counts/birth-death path stamps entry dates on (BirthDeathViewModel,
 *  AddBirthViewModel) -- never UTC. See A24. */
private val BIRTH_DEATH_BUSINESS_ZONE: ZoneId = ZoneId.of("Asia/Kolkata")

/**
 * Today's Asia/Kolkata business date, as an ISO string. A [clock] parameter (rather than the bare
 * `LocalDate.now(ZoneId)` call this replaced) so a test can simulate a wall-clock instant -- e.g.
 * 01:00 IST, which is still the PREVIOUS day in UTC -- without needing to change the device's
 * system clock.
 */
internal fun birthEntryIstBusinessDate(clock: Clock = Clock.systemUTC()): String =
    LocalDate.now(clock.withZone(BIRTH_DEATH_BUSINESS_ZONE)).toString()

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun BirthDeathScreen(
    state: BirthDeathUiState,
    onEvent: (BirthDeathEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    // Absent-field default only: prefill the entry date to today ONCE, the moment the birth form
    // is opened with no entry date set yet. This fires a normal EditField event, exactly what the
    // operator would have produced by typing it -- it never overwrites a value the operator
    // already entered or cleared.
    LaunchedEffect(state.mode) {
        if (state.mode == BirthDeathMode.BIRTH && state.entryDate.isBlank()) {
            // A24: the rest of the counts/birth-death path (BirthDeathViewModel.todayBusinessDate,
            // AddBirthViewModel.IST) stamps entry date on the Asia/Kolkata business day, never UTC.
            // This LaunchedEffect used to prefill UTC's date and there is no server-side rescue for
            // it (BirthDeathViewModel only substitutes the business date when entryDate is BLANK,
            // and this effect had already filled it) -- between 00:00-05:29 IST that recorded the
            // PREVIOUS business date.
            onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, birthEntryIstBusinessDate()))
        }
    }

    Column(
        modifier = modifier.fillMaxSize().background(MeshaColors.PageBg),
    ) {
        CountsFormHeader(
            title = stringResource(R.string.counts_birth_death_title),
            subtitle = stringResource(R.string.counts_birth_death_subtitle),
            onBack = { onEvent(BirthDeathEvent.Back) },
        )
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp),
            contentPadding = PaddingValues(bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "mode") {
                CountsSegmented(
                    options = listOf(
                        BirthDeathMode.BIRTH.name to stringResource(R.string.counts_mode_birth),
                        BirthDeathMode.DEATH.name to stringResource(R.string.counts_mode_death),
                    ),
                    selectedKey = state.mode.name,
                    onSelect = { key -> onEvent(BirthDeathEvent.SelectMode(BirthDeathMode.valueOf(key))) },
                )
            }
            item(key = "result") { CountsResultBanner(state.result) }
            // A synced write clears the form and leaves this confirmation above the fresh entry.
            state.lastRecordedMessage?.let { message ->
                item(key = "recorded") {
                    CountsResultBanner(CountsWriteResultUi(CountsWriteStatus.SYNCED, message))
                }
            }

            when (state.mode) {
                BirthDeathMode.BIRTH -> birthFields(state, onEvent)
                BirthDeathMode.DEATH -> deathFields(state, onEvent)
            }

            state.validationMessage?.let { message ->
                item(key = "validation") {
                    Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
                }
            }
            item(key = "submit") {
                // Once the write is committed (queued offline or synced) the form is locked; swap the
                // Submit button for a "Record another" action that clears the form for the next animal
                // instead of leaving a disabled button and stale values on screen.
                val committed = state.result.status == CountsWriteStatus.QUEUED ||
                    state.result.status == CountsWriteStatus.SYNCED
                if (committed) {
                    CountsSubmitButton(
                        label = stringResource(
                            if (state.mode == BirthDeathMode.BIRTH) {
                                R.string.counts_record_another_birth
                            } else {
                                R.string.counts_record_another_death
                            },
                        ),
                        enabled = true,
                        onClick = { onEvent(BirthDeathEvent.RecordAnother) },
                    )
                } else {
                    CountsSubmitButton(
                        label = stringResource(
                            if (state.mode == BirthDeathMode.BIRTH) {
                                R.string.counts_submit_birth
                            } else {
                                R.string.counts_submit_death
                            },
                        ),
                        enabled = state.canSubmit,
                        onClick = { onEvent(BirthDeathEvent.Submit) },
                    )
                }
            }
            item(key = "offline-note") {
                Text(
                    text = stringResource(R.string.counts_offline_note),
                    color = MeshaColors.Faint,
                    style = MeshaType.caption,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.birthFields(
    state: BirthDeathUiState,
    onEvent: (BirthDeathEvent) -> Unit,
) {
    item(key = "birth-identity") {
        val isTemporary = state.idKind == BIRTH_ID_KIND_TEMPORARY
        FormGroupCard(title = stringResource(R.string.counts_group_identity)) {
            // Permanent RFID vs temporary tag: a kid born before its permanent RFID is available is
            // recorded with a provisional tag now and promoted by the final "Tag the kid" action.
            CountsSegmented(
                options = listOf(
                    BIRTH_ID_KIND_PERMANENT to stringResource(R.string.counts_id_kind_permanent),
                    BIRTH_ID_KIND_TEMPORARY to stringResource(R.string.counts_id_kind_temporary),
                ),
                selectedKey = state.idKind,
                onSelect = { onEvent(BirthDeathEvent.EditField(BirthDeathField.ID_KIND, it)) },
            )
            // The permanent path is scannable: the operator holds the Bluetooth reader to the ear
            // tag instead of typing a 15-digit RFID in a shed. A TEMPORARY tag is a hand-written
            // provisional label with nothing to read, so it stays a plain typed field.
            if (isTemporary) {
                CountsTextField(
                    value = state.tag,
                    onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, it)) },
                    label = stringResource(R.string.counts_field_temp_tag),
                    required = true,
                )
            } else {
                CountsRfidField(
                    value = state.tag,
                    onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, it)) },
                    label = stringResource(R.string.counts_field_tag1),
                    scanning = state.scanningField == BirthDeathField.TAG,
                    onToggleScan = { onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG)) },
                    required = true,
                )
                // A newborn given two permanent ear tags carries an optional second RFID
                // (animal_identifier_2). Only offered on the permanent path — a provisional temporary
                // tag never carries a second permanent RFID (the backend rejects that pairing).
                CountsRfidField(
                    value = state.tag2,
                    onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG2, it)) },
                    label = stringResource(R.string.counts_field_tag2),
                    scanning = state.scanningField == BirthDeathField.TAG2,
                    onToggleScan = { onEvent(BirthDeathEvent.ToggleRfidScan(BirthDeathField.TAG2)) },
                    required = false,
                    supporting = stringResource(R.string.counts_hint_tag2),
                )
            }
            CountsSegmented(
                options = listOf(
                    "goat" to stringResource(R.string.counts_species_goat),
                    "sheep" to stringResource(R.string.counts_species_sheep),
                ),
                selectedKey = state.species,
                onSelect = { onEvent(BirthDeathEvent.EditField(BirthDeathField.SPECIES, it)) },
            )
            CountsSegmented(
                options = listOf(
                    "female" to stringResource(R.string.counts_sex_female),
                    "male" to stringResource(R.string.counts_sex_male),
                ),
                selectedKey = state.sex,
                onSelect = { onEvent(BirthDeathEvent.EditField(BirthDeathField.SEX, it)) },
            )
            // Breed is chosen from the herd's own backend breed vocabulary (never typed). The list is
            // the same Room-cached Counts facet the census filter uses; the facet key is submitted.
            val selectedBreedLabel = state.breedOptions.firstOrNull { it.key == state.breed }?.label
            CountsDropdownField(
                label = stringResource(R.string.counts_field_breed),
                selectedLabel = selectedBreedLabel,
                placeholder = stringResource(R.string.counts_select_breed),
                options = state.breedOptions.map { CountsDropdownOption(it.key, it.label, it.count) },
                onSelect = { onEvent(BirthDeathEvent.EditField(BirthDeathField.BREED, it)) },
                // Disabled until the vocabulary is cached so an operator cannot open an empty menu.
                enabled = state.breedOptions.isNotEmpty(),
            )
        }
    }
    item(key = "birth-dates") {
        FormGroupCard(title = stringResource(R.string.counts_group_dates)) {
            // The backend requires dob <= entry_date and rejects a violation; the hint states the
            // rule so an operator can fix it before submitting, but the server stays the authority.
            // Entry date defaults to today (stamped by the ViewModel) and is editable via the M3
            // picker. Both fields write the SAME YYYY-MM-DD ISO string the wire contract expects.
            CountsDateField(
                value = state.dob,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, it)) },
                label = stringResource(R.string.counts_field_dob),
                required = true,
                supporting = stringResource(R.string.counts_hint_dob),
            )
            CountsDateField(
                value = state.entryDate,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, it)) },
                label = stringResource(R.string.counts_field_entry_date),
                required = true,
            )
        }
    }
    // Placement: park -> shed cascade from the destinations catalog. Names are shown; ids submit.
    // Changing the park resets the shed — a shed id from another park is never a valid pairing, and
    // shed NAMES repeat across parks so the entries are keyed by shed_id.
    item(key = "birth-placement") {
        var damExpanded by remember { mutableStateOf(state.damId.isNotBlank()) }
        FormGroupCard(title = stringResource(R.string.counts_group_placement)) {
            val selectedPark = state.destinationParks.firstOrNull { it.parkId == state.parkId }
            CountsDropdownField(
                label = stringResource(R.string.counts_field_park),
                selectedLabel = selectedPark?.name,
                placeholder = stringResource(R.string.counts_select_farm),
                options = state.destinationParks.map { CountsDropdownOption(it.parkId, it.name) },
                onSelect = { onEvent(BirthDeathEvent.SelectPark(it)) },
                // Disabled until the catalog is in hand so an operator cannot open an empty menu and
                // conclude the farm has no parks.
                enabled = state.destinationParks.isNotEmpty(),
            )
            val sheds = state.shedsForSelectedPark
            val selectedShed = sheds.firstOrNull { it.shedId == state.shedId }
            CountsDropdownField(
                label = stringResource(R.string.counts_field_shed),
                selectedLabel = selectedShed?.name,
                placeholder = if (state.parkId.isBlank()) {
                    stringResource(R.string.counts_select_farm_first)
                } else {
                    stringResource(R.string.counts_select_shed)
                },
                options = sheds.map { CountsDropdownOption(it.shedId, it.name) },
                onSelect = { onEvent(BirthDeathEvent.SelectShed(it)) },
                enabled = sheds.isNotEmpty(),
            )
            state.destinationsMessage?.let { message ->
                Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
            }
            FormExpanderToggle(expanded = damExpanded, onToggle = { damExpanded = !damExpanded }, label = stringResource(R.string.counts_field_dam_optional))
            if (damExpanded) {
                // Dam stays an OPTIONAL free-text field (see report): unlike placement and the
                // death target it is not a required, safety-critical id, and it is left blank in
                // practice.
                CountsTextField(
                    value = state.damId,
                    onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.DAM_ID, it)) },
                    label = stringResource(R.string.counts_field_dam_optional),
                )
            }
        }
    }
}

/** A titled card grouping a set of related fields — Identity/Dates/Placement, per the redesign. */
@Composable
private fun FormGroupCard(title: String, content: @Composable androidx.compose.foundation.layout.ColumnScope.() -> Unit) {
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

/** Collapses an optional field group under an on-demand expander rather than always showing it. */
@Composable
private fun FormExpanderToggle(expanded: Boolean, onToggle: () -> Unit, label: String? = null) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onToggle)
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = label ?: stringResource(R.string.counts_more_fields),
            color = MeshaColors.Muted,
            style = MeshaType.cta,
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

private fun androidx.compose.foundation.lazy.LazyListScope.deathFields(
    state: BirthDeathUiState,
    onEvent: (BirthDeathEvent) -> Unit,
) {
    // 1. Find the animal — search by RFID/tag, then tap ONE result. Single selection: tapping a
    // different result REPLACES the selection. The tag is resolved to a goat_id server-side, so an
    // RFID string never reaches the death write, and the write carries the animal's own row_version.
    item(key = "death-animal-title") {
        CountsFieldGroupTitle(text = stringResource(R.string.counts_group_animal))
    }
    item(key = "death-animal-lookup") {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CountsTextField(
                value = state.animalQuery,
                onValueChange = { onEvent(BirthDeathEvent.EditAnimalQuery(it)) },
                label = stringResource(R.string.counts_field_animal_lookup),
                supporting = stringResource(R.string.counts_hint_death_lookup),
            )
            CountsSubmitButton(
                label = if (state.isLookingUpAnimals) {
                    stringResource(R.string.counts_animal_searching)
                } else {
                    stringResource(R.string.counts_animal_search)
                },
                enabled = state.animalQuery.isNotBlank() && !state.isLookingUpAnimals,
                onClick = { onEvent(BirthDeathEvent.LookupAnimals) },
            )
            state.animalLookupMessage?.let { message ->
                Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
            }
        }
    }
    // Matches are a bounded one-screen page from the backend, never a cohort pull.
    items(state.animalMatches, key = { "death-match-${it.goatId}" }) { match ->
        AnimalRow(
            animal = match,
            selected = state.selectedAnimal?.goatId == match.goatId,
            onClick = { onEvent(BirthDeathEvent.SelectAnimal(match.goatId)) },
        )
    }

    // 2. Confirm the animal — tag, current park/shed, sex, status. Read-only, straight from the
    // lookup. This is the operator's confirmation before an irreversible death; they never type it.
    state.selectedAnimal?.let { animal ->
        item(key = "death-target") { DeathTargetCard(animal = animal) }
    }

    // 3. Account of death.
    item(key = "death-account") {
        FormGroupCard(title = stringResource(R.string.counts_group_account_of_death)) {
            CountsTextField(
                value = state.reason,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.REASON, it)) },
                label = stringResource(R.string.counts_field_reason),
                required = true,
                supporting = stringResource(R.string.counts_hint_reason),
            )
        }
    }
    item(key = "death-guardrail") {
        Row(modifier = Modifier.fillMaxWidth()) {
            Text(
                text = stringResource(R.string.counts_death_guardrail_note),
                color = MeshaColors.Faint,
                style = MeshaType.caption,
            )
        }
    }
}

/**
 * The animal a death is being recorded against — tag, current park/shed, sex, and status, all
 * read-only from the search result. The write carries this animal's `row_version` verbatim, so the
 * operator confirms an animal here rather than typing an id or a record version.
 */
@Composable
private fun DeathTargetCard(animal: ShiftingAnimalUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(18.dp))
            .background(MeshaColors.Surf)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            text = stringResource(R.string.counts_group_recording_death_for),
            color = MeshaColors.Faint,
            style = MeshaType.pill,
        )
        Text(
            text = animal.displayId.ifBlank { animal.tag },
            color = MeshaColors.Ink,
            style = MeshaType.screenTitle,
        )
        if (animal.tag.isNotBlank()) {
            ReadOnlyFact(label = stringResource(R.string.counts_field_tag1), value = animal.tag)
        }
        // Park and shed as separate labelled facts when the backend supplies them, else its own
        // composed location string — the app never assembles a location label of its own. The shed
        // fact carries the partition (operationalLocationLabel: "Castro 2", never bare "Castro") —
        // a death record is terminal, so an operator confirming the wrong-looking shed here cannot
        // be corrected later (AGENTS.md: OperationalLocation = park + shed + partition).
        val hasParts = animal.parkName.isNotBlank() || animal.shedName.isNotBlank()
        if (hasParts) {
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_park),
                value = animal.parkName.ifBlank { stringResource(R.string.counts_location_unknown) },
            )
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_shed),
                value = operationalLocationLabel(animal.shedName, animal.partitionLabel)
                    .ifBlank { stringResource(R.string.counts_location_unknown) },
            )
        } else if (animal.locationLabel.isNotBlank()) {
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_current_location),
                value = animal.locationLabel,
            )
        }
        if (animal.sex.isNotBlank()) {
            ReadOnlyFact(label = stringResource(R.string.counts_field_sex), value = animal.sex)
        }
        if (animal.lifecycleStatus.isNotBlank()) {
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_status),
                value = animal.lifecycleStatus,
            )
        }
    }
}
