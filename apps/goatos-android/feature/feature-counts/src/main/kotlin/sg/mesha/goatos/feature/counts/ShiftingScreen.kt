package sg.mesha.goatos.feature.counts

// telemetry:exempt pure stateless renderer; ShiftingViewModel owns the counts_shifting_*
// analytics events and the CrashReporter non-fatal on every enqueue failure.

import androidx.compose.foundation.background
import androidx.compose.foundation.border
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors

/**
 * Record a shifting — the movement of ONE animal to a new shed (`/counts/shifting`), a hosted
 * destination with Up/Back.
 *
 * The event is REPORTED, not authorized: the backend records it `authorization_state=pending` /
 * `verification_state=unverified`, so this screen offers no approve affordance and says so.
 *
 * The flow is deliberately linear and short, in this exact order:
 *
 *  1. **Find the animal** — search by RFID/tag, then tap ONE result. Selection is SINGLE: tapping a
 *     different result REPLACES the selection rather than appending to a list. An operator standing
 *     at a pen moves the animal in front of them; a multi-select basket invited a mis-tap to
 *     silently relocate an animal nobody looked at.
 *  2. **Current location** — the selected animal's park + shed, READ-ONLY, straight from the
 *     lookup. The operator confirms it; they never type it, and the client never asserts it.
 *  3. **Destination** — two CASCADING dropdowns from the backend catalog: farm (park) first, then
 *     that park's sheds. Changing the park RESETS the shed, because a shed id from another park is
 *     never a valid pairing (and shed NAMES repeat across parks, so the entries are keyed by
 *     `shed_id`, never by name).
 *  4. **Priority** — High or Low (default).
 *  5. **Category** — Growth / Health / Breeding / Delivery.
 *  6. **Create shifting**.
 *
 * What used to be here and is gone on purpose: the source park/shed text inputs (now derived from
 * the animal), the free-text `effective_at` instant, the multi-animal basket, and the whole cohort
 * IMPACTS editor. The backend derives the movement's impact from the selected animal's own
 * canonical breed/stage — an operator hand-typing a breed next to an animal the server already
 * knows the breed of was a second, contradictable source of truth for the same fact.
 */

// ---------------------------------------------------------------------------
// State + events
// ---------------------------------------------------------------------------

/**
 * One animal resolved from a scanned/typed tag.
 *
 * [goatId] is what the write carries. [parkName]/[shedName] are the animal's CURRENT location,
 * shown read-only so the operator can confirm they picked the right animal before it is relocated;
 * [locationLabel] is the backend's own composed fallback string for when the parts are absent.
 *
 * [rowVersion]/[sex]/[lifecycleStatus] come straight off the search result. Shifting does not use
 * them, but the SAME picker backs the Birth/Death screen's death target, where the operator must
 * confirm the animal's sex + status before recording a death and the write carries the animal's own
 * [rowVersion] — never a hand-typed record version.
 */
@Immutable
data class ShiftingAnimalUi(
    val goatId: String,
    val displayId: String,
    val tag: String,
    val parkName: String = "",
    val shedName: String = "",
    val locationLabel: String = "",
    val rowVersion: Int = 0,
    val sex: String = "",
    val lifecycleStatus: String = "",
)

/** One shed a movement may target. Identity is [shedId] — names repeat across parks. */
@Immutable
data class ShiftingShedUi(
    val shedId: String,
    val name: String,
)

/** One park a movement may target, with the sheds that belong to it. */
@Immutable
data class ShiftingParkUi(
    val parkId: String,
    val name: String,
    val sheds: List<ShiftingShedUi> = emptyList(),
)

@Immutable
data class ShiftingUiState(
    // --- 1. animal search + single selection -------------------------------------------------
    val animalQuery: String = "",
    val animalMatches: List<ShiftingAnimalUi> = emptyList(),
    val isLookingUpAnimals: Boolean = false,
    /** Lookup outcome copy (no match / lookup failed). Null while idle or successful. */
    val animalLookupMessage: String? = null,
    /** THE animal being moved. Exactly one, or none. */
    val selectedAnimal: ShiftingAnimalUi? = null,

    // --- 3. destination ----------------------------------------------------------------------
    /** Backend destination catalog. Empty until the first successful fetch or cache read. */
    val destinationParks: List<ShiftingParkUi> = emptyList(),
    val destinationParkId: String = "",
    val destinationShedId: String = "",
    /** Set when the catalog could not be loaded and no cached copy exists. */
    val destinationsMessage: String? = null,

    // --- 4/5. classification -----------------------------------------------------------------
    val priority: String = SHIFTING_PRIORITY_LOW,
    val category: String = SHIFTING_CATEGORY_GROWTH,

    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
) {
    /** The sheds of the currently chosen park — the second dropdown's whole option set. */
    val shedsForSelectedPark: List<ShiftingShedUi>
        get() = destinationParks.firstOrNull { it.parkId == destinationParkId }?.sheds.orEmpty()
}

/**
 * The two priorities this screen offers. The backend's vocabulary is `high|low`; `low` is the
 * standard/default and `high` marks a movement that cannot wait.
 */
const val SHIFTING_PRIORITY_HIGH = "high"
const val SHIFTING_PRIORITY_LOW = "low"

/**
 * The four movement categories. `health` is the reason the animal picker must NOT filter by
 * health/lifecycle status: that category exists precisely to move sick, treated, quarantined, or
 * ICU animals, and a picker that hid them would make the movements they describe impossible to
 * record.
 */
const val SHIFTING_CATEGORY_GROWTH = "growth"
const val SHIFTING_CATEGORY_HEALTH = "health"
const val SHIFTING_CATEGORY_BREEDING = "breeding"
const val SHIFTING_CATEGORY_DELIVERY = "delivery"

sealed interface ShiftingEvent {
    data class EditAnimalQuery(val value: String) : ShiftingEvent
    data object LookupAnimals : ShiftingEvent

    /** Selects THE animal. Selecting another one replaces this; it never appends. */
    data class SelectAnimal(val goatId: String) : ShiftingEvent

    /** Choosing a park RESETS the shed — a shed from another park is never a valid pairing. */
    data class SelectDestinationPark(val parkId: String) : ShiftingEvent
    data class SelectDestinationShed(val shedId: String) : ShiftingEvent

    data class SelectPriority(val priority: String) : ShiftingEvent
    data class SelectCategory(val category: String) : ShiftingEvent

    data object Submit : ShiftingEvent
    data object Back : ShiftingEvent
}

// ---------------------------------------------------------------------------
// Screen
// ---------------------------------------------------------------------------

@Composable
fun ShiftingScreen(
    state: ShiftingUiState,
    onEvent: (ShiftingEvent) -> Unit = {},
    modifier: Modifier = Modifier,
    // When false, the caller (the Shifting tab host) already renders the screen header + tab bar, so
    // the Raise form must not render a second header of its own.
    showHeader: Boolean = true,
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        if (showHeader) {
            CountsFormHeader(
                title = stringResource(R.string.counts_shifting_title),
                subtitle = stringResource(R.string.counts_shifting_subtitle),
                onBack = { onEvent(ShiftingEvent.Back) },
            )
        }
        LazyColumn(
            modifier = Modifier.fillMaxSize().padding(horizontal = 16.dp),
            contentPadding = PaddingValues(bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { CountsResultBanner(state.result) }

            // --- 1. Find the animal -----------------------------------------------------------
            item(key = "animal-title") {
                CountsFieldGroupTitle(text = stringResource(R.string.counts_group_animal))
            }
            item(key = "animal-lookup") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    CountsTextField(
                        value = state.animalQuery,
                        onValueChange = { onEvent(ShiftingEvent.EditAnimalQuery(it)) },
                        label = stringResource(R.string.counts_field_animal_lookup),
                        supporting = stringResource(R.string.counts_hint_animal_lookup),
                    )
                    CountsSubmitButton(
                        label = if (state.isLookingUpAnimals) {
                            stringResource(R.string.counts_animal_searching)
                        } else {
                            stringResource(R.string.counts_animal_search)
                        },
                        enabled = state.animalQuery.isNotBlank() && !state.isLookingUpAnimals,
                        onClick = { onEvent(ShiftingEvent.LookupAnimals) },
                    )
                    state.animalLookupMessage?.let { message ->
                        Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                    }
                }
            }
            // Matches are a bounded one-screen page from the backend, never a cohort pull.
            items(state.animalMatches, key = { "match-${it.goatId}" }) { match ->
                AnimalRow(
                    animal = match,
                    selected = state.selectedAnimal?.goatId == match.goatId,
                    onClick = { onEvent(ShiftingEvent.SelectAnimal(match.goatId)) },
                )
            }

            // --- 2. Current location (read-only, derived from the animal) ---------------------
            state.selectedAnimal?.let { animal ->
                item(key = "current-location") {
                    CurrentLocationCard(animal = animal)
                }
            }

            // --- 3. Destination: two cascading dropdowns -------------------------------------
            item(key = "destination-title") {
                CountsFieldGroupTitle(text = stringResource(R.string.counts_group_to))
            }
            item(key = "destination-park") {
                val selectedPark = state.destinationParks.firstOrNull { it.parkId == state.destinationParkId }
                CountsDropdownField(
                    label = stringResource(R.string.counts_field_farm),
                    selectedLabel = selectedPark?.name,
                    placeholder = stringResource(R.string.counts_select_farm),
                    // Keyed by park_id, and disabled until the catalog is in hand so an operator
                    // cannot open an empty menu and conclude the farm has no parks.
                    options = state.destinationParks.map { CountsDropdownOption(it.parkId, it.name) },
                    onSelect = { onEvent(ShiftingEvent.SelectDestinationPark(it)) },
                    enabled = state.destinationParks.isNotEmpty(),
                )
            }
            item(key = "destination-shed") {
                val sheds = state.shedsForSelectedPark
                val selectedShed = sheds.firstOrNull { it.shedId == state.destinationShedId }
                CountsDropdownField(
                    label = stringResource(R.string.counts_field_shed),
                    selectedLabel = selectedShed?.name,
                    placeholder = if (state.destinationParkId.isBlank()) {
                        stringResource(R.string.counts_select_farm_first)
                    } else {
                        stringResource(R.string.counts_select_shed)
                    },
                    // Entries are keyed by shed_id: shed NAMES repeat across parks, so a
                    // name-keyed menu would collapse two real sheds into one entry.
                    options = sheds.map { CountsDropdownOption(it.shedId, it.name) },
                    onSelect = { onEvent(ShiftingEvent.SelectDestinationShed(it)) },
                    enabled = sheds.isNotEmpty(),
                )
            }
            state.destinationsMessage?.let { message ->
                item(key = "destinations-message") {
                    Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                }
            }

            // --- 4. Priority -----------------------------------------------------------------
            item(key = "priority") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    CountsFieldGroupTitle(text = stringResource(R.string.counts_group_priority))
                    CountsSegmented(
                        options = listOf(
                            SHIFTING_PRIORITY_HIGH to stringResource(R.string.counts_priority_high),
                            SHIFTING_PRIORITY_LOW to stringResource(R.string.counts_priority_low),
                        ),
                        selectedKey = state.priority,
                        onSelect = { onEvent(ShiftingEvent.SelectPriority(it)) },
                    )
                }
            }

            // --- 5. Category -----------------------------------------------------------------
            item(key = "category") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    CountsFieldGroupTitle(text = stringResource(R.string.counts_group_category))
                    CountsSegmented(
                        options = listOf(
                            SHIFTING_CATEGORY_GROWTH to stringResource(R.string.counts_category_growth),
                            SHIFTING_CATEGORY_HEALTH to stringResource(R.string.counts_category_health),
                            SHIFTING_CATEGORY_BREEDING to stringResource(R.string.counts_category_breeding),
                            SHIFTING_CATEGORY_DELIVERY to stringResource(R.string.counts_category_delivery),
                        ),
                        selectedKey = state.category,
                        onSelect = { onEvent(ShiftingEvent.SelectCategory(it)) },
                    )
                }
            }

            state.validationMessage?.let { message ->
                item(key = "validation") {
                    Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                }
            }

            // --- 6. Create -------------------------------------------------------------------
            item(key = "submit") {
                CountsSubmitButton(
                    label = stringResource(R.string.counts_submit_shifting),
                    enabled = state.canSubmit,
                    onClick = { onEvent(ShiftingEvent.Submit) },
                )
            }
            item(key = "notes") {
                Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    Text(
                        text = stringResource(R.string.counts_shifting_pending_note),
                        color = MeshaColors.Faint,
                        fontSize = 11.sp,
                    )
                    Text(
                        text = stringResource(R.string.counts_offline_note),
                        color = MeshaColors.Faint,
                        fontSize = 11.sp,
                    )
                }
            }
        }
    }
}

/**
 * A lookup match. Tapping SELECTS it — selecting another match replaces this one, so the screen
 * always carries exactly zero or one animal. Deliberately not a toggle: an operator who taps the
 * wrong row corrects it by tapping the right one, which is the same gesture rather than a
 * deselect-then-reselect pair.
 */
@Composable
internal fun AnimalRow(
    animal: ShiftingAnimalUi,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(12.dp))
            .background(if (selected) MeshaColors.Brand.copy(alpha = 0.10f) else MeshaColors.Surf)
            .border(1.dp, if (selected) MeshaColors.Brand else MeshaColors.Hair, RoundedCornerShape(12.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 12.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(animal.displayId, color = MeshaColors.Ink, fontSize = 13.sp, fontWeight = FontWeight.W700)
            Text(animal.tag, color = MeshaColors.Muted, fontSize = 11.sp)
            if (animal.locationLabel.isNotBlank()) {
                Text(animal.locationLabel, color = MeshaColors.Faint, fontSize = 11.sp)
            }
        }
        if (selected) {
            Icon(
                imageVector = MeshaIcons.Check,
                contentDescription = null,
                tint = MeshaColors.Brand,
                modifier = Modifier.size(16.dp),
            )
        }
    }
}

/**
 * Where the selected animal stands RIGHT NOW — fetched with the animal, never typed. This is the
 * operator's confirmation that they picked the right animal, and it is also the movement's source:
 * the backend reads it from the animal itself, so there is nothing here for the client to assert.
 */
@Composable
private fun CurrentLocationCard(animal: ShiftingAnimalUi) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(14.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 12.dp, vertical = 11.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(
            text = stringResource(R.string.counts_group_current_location),
            color = MeshaColors.Faint,
            fontSize = 11.sp,
            fontWeight = FontWeight.W700,
        )
        Text(
            text = animal.displayId,
            color = MeshaColors.Ink,
            fontSize = 14.sp,
            fontWeight = FontWeight.W700,
        )
        // Park and shed are shown as separate labelled facts when the backend supplies them, and
        // fall back to its own composed location string when it does not — the app never assembles
        // a location label of its own.
        val hasParts = animal.parkName.isNotBlank() || animal.shedName.isNotBlank()
        if (hasParts) {
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_farm),
                value = animal.parkName.ifBlank { stringResource(R.string.counts_location_unknown) },
            )
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_shed),
                value = animal.shedName.ifBlank { stringResource(R.string.counts_location_unknown) },
            )
        } else {
            ReadOnlyFact(
                label = stringResource(R.string.counts_field_current_location),
                value = animal.locationLabel.ifBlank { stringResource(R.string.counts_location_unknown) },
            )
        }
    }
}

@Composable
internal fun ReadOnlyFact(label: String, value: String) {
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(text = label, color = MeshaColors.Muted, fontSize = 12.sp, modifier = Modifier.weight(1f))
        Text(text = value, color = MeshaColors.Ink, fontSize = 12.sp, fontWeight = FontWeight.W600)
    }
}
