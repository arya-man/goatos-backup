package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; AddDeathViewModel (in :app) owns the
// counts_death_* AnalyticsEvents + the CrashReporter non-fatal on every enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.designsystem.theme.MeshaType
import sg.mesha.goatos.core.ui.operationalLocationLabel

/**
 * Add-death form (`/counts/death/add` — docs/decisions/birth-death-workflows.md, mock's "Add"
 * column). An L1 hosted destination reached ONLY from the Death work list's ＋ button. The form is
 * the retired combined screen's death mode UNCHANGED: tag/RFID search → select ONE animal (which
 * carries its own `goat_id` + `row_version`) → reason (3–500 chars) → sticky danger-tinted submit.
 * Submit and approval are untouched — the exit still applies at CEO approval; the death workflow's
 * evidence trail opens from the `goat.exited` event, never from this form directly.
 */

@Immutable
data class AddDeathUiState(
    val animalQuery: String = "",
    val animalMatches: List<ShiftingAnimalUi> = emptyList(),
    val isLookingUpAnimals: Boolean = false,
    val animalLookupMessage: String? = null,
    /** THE animal whose death is being recorded. Exactly one, or none. */
    val selectedAnimal: ShiftingAnimalUi? = null,
    val reason: String = "",
    // Was this a normal death, or due to a disease? See [DeathCauseKind].
    val deathCauseKind: DeathCauseKind = DeathCauseKind.NORMAL,
    /** Every disease the register names, loaded once and searched on the device. */
    val deathCauseOptions: List<DeathCauseOptionUi> = emptyList(),
    /** What the operator has typed into the disease search. Filters [deathCauseOptions] only. */
    val deathCauseQuery: String = "",
    /** The disease named as the cause, or null while none is chosen. */
    val selectedDeathCause: DeathCauseOptionUi? = null,
    /**
     * Set when the disease list could not be loaded. The DISEASE choice is then unavailable rather
     * than empty: an empty dropdown reads as "this farm has no diseases", and the operator would
     * record a normal death for an animal that died of something nameable.
     */
    val deathCauseMessage: String? = null,
    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    val lastRecordedMessage: String? = null,
) {
    /**
     * The diseases matching what the operator has typed, matched on the LABEL only.
     *
     * The rule id is deliberately not searched: it is a machine key the operator never sees, so a
     * match on it would surface a row whose visible text does not contain what they typed. A blank
     * query lists everything, which is the whole vocabulary and small enough to scroll.
     */
    val matchingDeathCauses: List<DeathCauseOptionUi>
        get() {
            val needle = deathCauseQuery.trim()
            if (needle.isEmpty()) return deathCauseOptions
            return deathCauseOptions.filter { it.label.contains(needle, ignoreCase = true) }
        }

    /**
     * The cause the WRITE carries, or null for a normal death.
     *
     * THE TOGGLE IS THE GATE, not merely the thing that shows the list. A disease chosen and then
     * abandoned by switching back to NORMAL must never ride along on the write, and putting that
     * rule here rather than at the call site means every future caller inherits it -- the screen
     * cannot forget, and a second submit path cannot disagree with the first.
     */
    val submittedDeathCause: DeathCauseOptionUi?
        get() = selectedDeathCause.takeIf { deathCauseKind == DeathCauseKind.DISEASE }
}

sealed interface AddDeathEvent {
    data class EditAnimalQuery(val value: String) : AddDeathEvent
    data object LookupAnimals : AddDeathEvent
    data class SelectAnimal(val goatId: String) : AddDeathEvent
    data class EditReason(val value: String) : AddDeathEvent

    // Cause of death — the normal/disease toggle and the searchable disease list.
    data class SelectDeathCauseKind(val kind: DeathCauseKind) : AddDeathEvent
    data class EditDeathCauseQuery(val value: String) : AddDeathEvent
    data class SelectDeathCause(val key: String) : AddDeathEvent
    data object Submit : AddDeathEvent
    data object RecordAnother : AddDeathEvent
    data object Back : AddDeathEvent
}

@Composable
fun AddDeathScreen(
    state: AddDeathUiState,
    onEvent: (AddDeathEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = stringResource(R.string.counts_workflow_add_death),
            subtitle = stringResource(R.string.counts_add_death_subtitle),
            onBack = { onEvent(AddDeathEvent.Back) },
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

            // 1. Find the animal — search by RFID/tag, then tap ONE result.
            item(key = "animal-title") {
                CountsFieldGroupTitle(text = stringResource(R.string.counts_group_animal))
            }
            item(key = "animal-lookup") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    CountsTextField(
                        value = state.animalQuery,
                        onValueChange = { onEvent(AddDeathEvent.EditAnimalQuery(it)) },
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
                        onClick = { onEvent(AddDeathEvent.LookupAnimals) },
                    )
                    state.animalLookupMessage?.let { message ->
                        Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
                    }
                }
            }
            // Matches are a bounded one-screen page from the backend, never a cohort pull.
            items(state.animalMatches, key = { "match-${it.goatId}" }) { match ->
                AnimalRow(
                    animal = match,
                    selected = state.selectedAnimal?.goatId == match.goatId,
                    onClick = { onEvent(AddDeathEvent.SelectAnimal(match.goatId)) },
                )
            }

            // 2. Confirm the animal — read-only facts straight from the lookup.
            state.selectedAnimal?.let { animal ->
                item(key = "target") { AddDeathTargetCard(animal) }
            }

            // 3. WHY the animal died — normal, or due to a disease the register names.
            //
            // The toggle sits ABOVE the account because it changes what the account is FOR: on a
            // normal death the written account is the only record of why the animal died and is
            // required; once a disease is named the coded cause has answered that and the note
            // becomes optional colour.
            item(key = "cause-kind") {
                AddFormGroupCard(title = stringResource(R.string.counts_group_cause_of_death)) {
                    CountsSegmented(
                        options = listOf(
                            DeathCauseKind.NORMAL.name to stringResource(R.string.counts_death_cause_normal),
                            DeathCauseKind.DISEASE.name to stringResource(R.string.counts_death_cause_disease),
                        ),
                        selectedKey = state.deathCauseKind.name,
                        onSelect = { key ->
                            onEvent(AddDeathEvent.SelectDeathCauseKind(DeathCauseKind.valueOf(key)))
                        },
                        // A list that failed to load leaves the choice VISIBLE and dimmed with its
                        // reason underneath, never hidden: the operator is owed the knowledge that
                        // the product could have recorded a disease and cannot reach the list.
                        disabledKeys = if (state.deathCauseOptions.isEmpty()) {
                            setOf(DeathCauseKind.DISEASE.name)
                        } else {
                            emptySet()
                        },
                    )
                    state.deathCauseMessage?.let { message ->
                        Text(text = message, color = MeshaColors.Warn, style = MeshaType.cardSubtitle)
                    }
                }
            }

            // The searchable disease list, shown only once the operator has said this was a
            // disease death. The whole vocabulary is already on the device, so typing filters
            // locally with no round trip — which is what makes it work in a pen with no signal.
            if (state.deathCauseKind == DeathCauseKind.DISEASE) {
                item(key = "cause-search") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        CountsTextField(
                            value = state.deathCauseQuery,
                            onValueChange = { onEvent(AddDeathEvent.EditDeathCauseQuery(it)) },
                            label = stringResource(R.string.counts_field_death_cause_search),
                            required = true,
                            supporting = stringResource(R.string.counts_hint_death_cause_search),
                        )
                        // A search that matches nothing says so. The way out is the NORMAL choice
                        // plus a note — the product refuses to store a disease it cannot name,
                        // because one death filed as "Mastitus" beside another as "MASTITIS" is
                        // two diseases on the board and one in the barn.
                        if (state.matchingDeathCauses.isEmpty()) {
                            Text(
                                text = stringResource(R.string.counts_death_cause_no_match),
                                color = MeshaColors.Warn,
                                style = MeshaType.cardSubtitle,
                            )
                        }
                    }
                }
                items(state.matchingDeathCauses, key = { "cause-${it.key}" }) { option ->
                    DeathCauseRow(
                        option = option,
                        selected = state.selectedDeathCause?.key == option.key,
                        onClick = { onEvent(AddDeathEvent.SelectDeathCause(option.key)) },
                    )
                }
            }

            // 4. Account of death. Required for a normal death (it is the only record of why),
            // optional once a disease has been named. Backend-enforced either way at 500 chars.
            item(key = "account") {
                AddFormGroupCard(title = stringResource(R.string.counts_group_account_of_death)) {
                    CountsTextField(
                        value = state.reason,
                        onValueChange = { onEvent(AddDeathEvent.EditReason(it)) },
                        label = stringResource(R.string.counts_field_reason),
                        required = state.deathCauseKind == DeathCauseKind.NORMAL,
                        supporting = if (state.deathCauseKind == DeathCauseKind.NORMAL) {
                            stringResource(R.string.counts_hint_reason)
                        } else {
                            stringResource(R.string.counts_hint_reason_disease)
                        },
                    )
                }
            }
            item(key = "guardrail") {
                Row(modifier = Modifier.fillMaxWidth()) {
                    Text(
                        text = stringResource(R.string.counts_death_guardrail_note),
                        color = MeshaColors.Faint,
                        style = MeshaType.caption,
                    )
                }
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
                        label = stringResource(R.string.counts_record_another_death),
                        enabled = true,
                        onClick = { onEvent(AddDeathEvent.RecordAnother) },
                    )
                } else {
                    CountsSubmitButton(
                        label = stringResource(R.string.counts_submit_death),
                        enabled = state.canSubmit,
                        onClick = { onEvent(AddDeathEvent.Submit) },
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

/** The animal a death is being recorded against — read-only facts from the search result. */
@Composable
private fun AddDeathTargetCard(animal: ShiftingAnimalUi) {
    AddFormGroupCard(title = stringResource(R.string.counts_group_recording_death_for)) {
        Text(
            text = animal.displayId.ifBlank { animal.tag },
            color = MeshaColors.Ink,
            // screenTitle is the design-system entry for this heading. The literal it replaces was
            // 20.sp/W800, which existed in no scale entry; screenTitle (22.sp/W700) is the nearest
            // semantic match for the animal identifier that heads this card.
            style = MeshaType.screenTitle,
        )
        if (animal.tag.isNotBlank()) {
            ReadOnlyFact(label = stringResource(R.string.counts_field_tag1), value = animal.tag)
        }
        // Shed value carries the partition (operationalLocationLabel: "Castro 2", never bare
        // "Castro") — a death record is terminal, so a wrong-looking shed here cannot be corrected
        // later (AGENTS.md: OperationalLocation = park + shed + partition).
        // Check shedId (uuid) rather than shedName to determine if location information exists.
        val hasLocationInfo = animal.parkId.isNotBlank() || animal.shedId.isNotBlank()
        if (hasLocationInfo) {
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
