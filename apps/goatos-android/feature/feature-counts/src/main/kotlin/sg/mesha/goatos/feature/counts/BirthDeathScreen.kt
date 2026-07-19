package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; BirthDeathViewModel owns the counts_birth_* /
// counts_death_* analytics events and the CrashReporter non-fatal on every enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.Immutable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Record a birth or a death (`/counts/birth-death`) — a hosted destination with Up/Back.
 *
 * One screen, two mutually exclusive events, because they are the two count-DECREASING/INCREASING
 * lifecycle facts an operator reports from the same moment in the shed. They are NOT merged into
 * one request: each posts to its own backend route with its own guardrails.
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

/**
 * Draft fields for both modes. Every field is a plain string so a partially-typed value is never
 * silently coerced — a blank numeric field stays blank (and blocks submit) rather than becoming
 * `0`, which the authored-config rule in AGENTS.md forbids.
 */
@Immutable
data class BirthDeathUiState(
    val mode: BirthDeathMode = BirthDeathMode.BIRTH,
    // Birth
    val tag: String = "",
    val secondTag: String = "",
    val species: String = "goat",
    val sex: String = "female",
    val breed: String = "",
    val dob: String = "",
    val entryDate: String = "",
    val parkId: String = "",
    val shedId: String = "",
    val damId: String = "",
    // Death
    val goatId: String = "",
    val rowVersion: String = "",
    val reason: String = "",
    // Shared
    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
)

sealed interface BirthDeathEvent {
    data class SelectMode(val mode: BirthDeathMode) : BirthDeathEvent
    data class EditField(val field: BirthDeathField, val value: String) : BirthDeathEvent
    data object Submit : BirthDeathEvent
    data object Back : BirthDeathEvent
}

enum class BirthDeathField {
    TAG, SECOND_TAG, SPECIES, SEX, BREED, DOB, ENTRY_DATE, PARK_ID, SHED_ID, DAM_ID,
    GOAT_ID, ROW_VERSION, REASON,
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun BirthDeathScreen(
    state: BirthDeathUiState,
    onEvent: (BirthDeathEvent) -> Unit = {},
    modifier: Modifier = Modifier,
) {
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

            when (state.mode) {
                BirthDeathMode.BIRTH -> birthFields(state, onEvent)
                BirthDeathMode.DEATH -> deathFields(state, onEvent)
            }

            state.validationMessage?.let { message ->
                item(key = "validation") {
                    Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                }
            }
            item(key = "submit") {
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
            item(key = "offline-note") {
                Text(
                    text = stringResource(R.string.counts_offline_note),
                    color = MeshaColors.Faint,
                    fontSize = 11.sp,
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
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CountsFieldGroupTitle(text = stringResource(R.string.counts_group_identity))
            CountsTextField(
                value = state.tag,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.TAG, it)) },
                label = stringResource(R.string.counts_field_tag1),
                required = true,
            )
            CountsTextField(
                value = state.secondTag,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.SECOND_TAG, it)) },
                label = stringResource(R.string.counts_field_tag2),
            )
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
            CountsTextField(
                value = state.breed,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.BREED, it)) },
                label = stringResource(R.string.counts_field_breed),
            )
        }
    }
    item(key = "birth-dates") {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CountsFieldGroupTitle(text = stringResource(R.string.counts_group_dates))
            // The backend requires dob <= entry_date and rejects a violation; the hint states the
            // rule so an operator can fix it before submitting, but the server stays the authority.
            CountsTextField(
                value = state.dob,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.DOB, it)) },
                label = stringResource(R.string.counts_field_dob),
                required = true,
                supporting = stringResource(R.string.counts_hint_dob),
            )
            CountsTextField(
                value = state.entryDate,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.ENTRY_DATE, it)) },
                label = stringResource(R.string.counts_field_entry_date),
                required = true,
            )
        }
    }
    item(key = "birth-placement") {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CountsFieldGroupTitle(text = stringResource(R.string.counts_group_placement))
            CountsTextField(
                value = state.parkId,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.PARK_ID, it)) },
                label = stringResource(R.string.counts_field_park),
            )
            CountsTextField(
                value = state.shedId,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.SHED_ID, it)) },
                label = stringResource(R.string.counts_field_shed),
            )
            CountsTextField(
                value = state.damId,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.DAM_ID, it)) },
                label = stringResource(R.string.counts_field_dam),
            )
        }
    }
}

private fun androidx.compose.foundation.lazy.LazyListScope.deathFields(
    state: BirthDeathUiState,
    onEvent: (BirthDeathEvent) -> Unit,
) {
    item(key = "death-fields") {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            CountsFieldGroupTitle(text = stringResource(R.string.counts_group_animal))
            CountsTextField(
                value = state.goatId,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.GOAT_ID, it)) },
                label = stringResource(R.string.counts_field_goat),
                required = true,
            )
            // Optimistic-concurrency guard. The contract's GoatSummary carries no row_version, so
            // it cannot be round-tripped from a prior response and is entered explicitly. Left
            // blank it blocks submit — it is never defaulted to 1, which would silently overwrite
            // a concurrent edit.
            CountsTextField(
                value = state.rowVersion,
                onValueChange = { onEvent(BirthDeathEvent.EditField(BirthDeathField.ROW_VERSION, it)) },
                label = stringResource(R.string.counts_field_row_version),
                required = true,
                numeric = true,
                supporting = stringResource(R.string.counts_hint_row_version),
            )
            CountsFieldGroupTitle(text = stringResource(R.string.counts_group_account_of_death))
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
                fontSize = 11.sp,
            )
        }
    }
}
