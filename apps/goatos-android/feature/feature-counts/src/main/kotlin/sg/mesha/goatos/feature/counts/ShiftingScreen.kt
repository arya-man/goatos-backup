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
import androidx.compose.ui.graphics.Color
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons
import sg.mesha.goatos.core.designsystem.theme.MeshaColors
import sg.mesha.goatos.core.ui.operationalLocationLabel

/**
 * Record a shifting — the movement of ONE animal to a new shed (`/counts/shifting/add`), a hosted
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
 *  3. **Destination** — the animal's current farm is selected automatically and read-only; the
 *     operator chooses only a destination shed inside that farm. Goats never shift between farms.
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
 * [rowVersion]/[sex]/[lifecycleStatus] come straight off the search result. Shifting uses lifecycle
 * status to reject terminal animals; the SAME picker also backs the Birth/Death screen's death
 * target, where the operator confirms sex + status and the write carries the animal's own
 * [rowVersion] — never a hand-typed record version.
 */
@Immutable
data class ShiftingAnimalUi(
    val goatId: String,
    val displayId: String,
    val tag: String,
    val parkId: String = "",
    val shedId: String = "",
    val parkName: String = "",
    val shedName: String = "",
    /**
     * The shed PARTITION the animal currently sits in, when its shed is partitioned. Null/blank/
     * "whole" (see [sg.mesha.goatos.core.ui.operationalLocationLabel]) means the shed is not
     * partitioned or the animal occupies the whole shed.
     */
    val partitionLabel: String? = null,
    val locationLabel: String = "",
    val rowVersion: Int = 0,
    val sex: String = "",
    val lifecycleStatus: String = "",
)

/**
 * One OPERATIONAL LOCATION a movement may target — either a whole shed or one of its partitions.
 * Identity is [shedId] + [partitionLabel] together: a partitioned shed offers one entry per
 * partition (never a bare whole-shed entry alongside them), and an unpartitioned shed offers
 * exactly one entry with a null [partitionLabel].
 */
@Immutable
data class ShiftingShedUi(
    val shedId: String,
    val name: String,
    val partitionLabel: String? = null,
    /**
     * The backend-composed operational-location label ("Yashoda", "Castro - 2",
     * "Godel 1 - Part 3"). Render this verbatim: the backend owns visible labels, and composing
     * shed + partition on the client duplicates that rule into a second language where it drifts.
     * Falls back to the local composer only when the server sends nothing, so an older backend
     * still renders something sensible instead of a blank row.
     */
    val operationalLocationDisplay: String = "",
    /**
     * The tag a movement into this pen would stamp, for the TAG TOGGLE. Blank when the pen cannot
     * supply one — [destinationStageReason] then says why, in the backend's own words.
     *
     * Backend-resolved. Never derive it from the pen's residents on-device: the rules behind the
     * answer (the pen's authored tag first, blank for a mixed or empty pen, never a clinical state)
     * live on the server, and a second implementation here would drift from the tag the raise
     * actually stamps.
     */
    val destinationStage: String = "",
    /** Farm-worded reason the pen's tag is unavailable, rendered verbatim. Blank when one exists. */
    val destinationStageReason: String = "",
) {
    /** What the operator should read for this option. */
    val displayLabel: String
        get() = operationalLocationDisplay.ifBlank { operationalLocationLabel(name, partitionLabel) }

    /** Whether the "use destination tag" side of the toggle is offerable for this pen. */
    val offersDestinationStage: Boolean
        get() = destinationStage.isNotBlank()

    /** Stable dropdown-option key: shed alone is not unique once a shed has partitions. */
    val optionKey: String
        get() = listOfNotNull(shedId, partitionLabel).joinToString("|")
}

/**
 * Where a newborn recorded in one park may be placed (maintainer decision 2026-08-20).
 *
 * Backend-owned: [mode] and [notice] are rendered verbatim, and [pens] is already filtered to the
 * park's kid pens. The form must not re-derive any of it from the shed list.
 */
@Immutable
data class BirthPlacementUi(
    val mode: String = MODE_RECORD_LATER,
    val notice: String = "",
    val pens: List<ShiftingShedUi> = emptyList(),
) {
    /** The park has exactly one kid pen: it is shown read-only and the operator does not choose. */
    val isAutomatic: Boolean get() = mode == MODE_AUTOMATIC && pens.size == 1

    /** The park has several kid pens: the picker offers only those. */
    val isChoice: Boolean get() = mode == MODE_CHOOSE && pens.isNotEmpty()

    /**
     * No kid pen is set for this park, so the operator picks freely from the full cascade and the
     * kid's care steps carry Record shed. This is also what an older cached payload decodes to,
     * which is the safe fallback: the form behaves exactly as it did before this contract existed.
     */
    val isRecordLater: Boolean get() = !isAutomatic && !isChoice

    /** The single kid pen in automatic mode, else null. */
    val automaticPen: ShiftingShedUi? get() = pens.singleOrNull()?.takeIf { isAutomatic }

    companion object {
        const val MODE_AUTOMATIC = "automatic"
        const val MODE_CHOOSE = "choose"
        const val MODE_RECORD_LATER = "record_later"
    }
}

/** One park a movement may target, with the sheds that belong to it. */
@Immutable
data class ShiftingParkUi(
    val parkId: String,
    val name: String,
    val sheds: List<ShiftingShedUi> = emptyList(),
    /** Newborn placement for this park. Only the birth form reads it; shifting ignores it. */
    val birthPlacement: BirthPlacementUi = BirthPlacementUi(),
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
    /** The chosen destination's partition, when the destination shed has partitions. */
    val destinationPartitionLabel: String? = null,
    /** Set when the catalog could not be loaded and no cached copy exists. */
    val destinationsMessage: String? = null,

    // --- 3b. tag toggle ----------------------------------------------------------------------
    /**
     * Which tag the moved animal ends up carrying: [SHIFTING_STAGE_MODE_DESTINATION] (adopt the
     * destination pen's tag) or [SHIFTING_STAGE_MODE_KEEP_CURRENT] (keep the one it has).
     *
     * Defaults to the destination pen's tag, which is what a movement did before the toggle
     * existed, so an operator who ignores the control gets exactly today's behaviour.
     *
     * The ViewModel forces this back to keep-current whenever the selected pen cannot supply a tag,
     * so this field can never claim a mode the destination does not support.
     */
    val stageMode: String = SHIFTING_STAGE_MODE_DESTINATION,

    // --- 4/5. classification -----------------------------------------------------------------
    val priority: String = SHIFTING_PRIORITY_LOW,
    val category: String = SHIFTING_CATEGORY_GROWTH,

    // --- 6. optional note --------------------------------------------------------------------
    /**
     * The raiser's optional note on why the animals are moving. Never gates [canSubmit] — it is
     * context for the park head approving and the verifier reviewing, not a required field.
     */
    val comment: String = "",

    val canSubmit: Boolean = false,
    val validationMessage: String? = null,
    val result: CountsWriteResultUi = CountsWriteResultUi(),
    /**
     * Transient success confirmation shown after a synced movement auto-clears the form, so the
     * operator sees the movement was raised on a fresh form. Cleared when they start the next entry.
     */
    val lastRecordedMessage: String? = null,
    /** One-shot navigation result consumed by AppNavHost after server-confirmed sync. */
    val returnToActions: Boolean = false,
    val submissionNotice: String? = null,
) {
    /**
     * The destination options for the currently chosen park — one entry per WHOLE shed or per
     * PARTITION of a partitioned shed, never both for the same shed. This is the second
     * dropdown's whole option set.
     */
    val shedsForSelectedPark: List<ShiftingShedUi>
        get() = destinationParks.firstOrNull { it.parkId == destinationParkId }?.sheds.orEmpty()

    /** The currently-selected destination option, if any. */
    val selectedDestination: ShiftingShedUi?
        get() = shedsForSelectedPark.firstOrNull {
            it.shedId == destinationShedId && it.partitionLabel == destinationPartitionLabel
        }

    /**
     * Whether the "use destination tag" side of the toggle may be tapped. False until a destination is
     * chosen (there is no pen to take a tag from yet) and false for a pen that cannot supply one.
     */
    val canUseDestinationStage: Boolean
        get() = selectedDestination?.offersDestinationStage == true

    /**
     * The reason to show under the disabled option, straight from the backend. Null when the option
     * is available, or when no destination is selected yet — an operator who has not picked a pen
     * is not owed an explanation for a choice they have not reached.
     */
    val destinationStageReason: String?
        get() = selectedDestination?.destinationStageReason?.takeIf { it.isNotBlank() }

    /** The pen's tag itself, for the option's supporting line. Null when there is none. */
    val destinationStageLabel: String?
        get() = selectedDestination?.destinationStage?.takeIf { it.isNotBlank() }
}

/**
 * The two priorities this screen offers. The backend's vocabulary is `high|low`; `low` is the
 * standard/default and `high` marks a movement that cannot wait.
 */
const val SHIFTING_PRIORITY_HIGH = "high"
const val SHIFTING_PRIORITY_LOW = "low"

/**
 * The four movement categories. `health` is why the picker must not filter by health status: live
 * sick, treated, quarantined, or ICU animals remain shiftable. Lifecycle status is independent;
 * dead/transferred/sold animals are terminal and are rejected.
 */
/**
 * The two positions of the raise form's TAG TOGGLE, matching the backend's `stage_mode` vocabulary.
 *
 * [SHIFTING_STAGE_MODE_DESTINATION] is the default and the pre-toggle behaviour. The client sends
 * only the mode; the server resolves which tag that actually means.
 */
const val SHIFTING_STAGE_MODE_DESTINATION = "destination_stage"
const val SHIFTING_STAGE_MODE_KEEP_CURRENT = "keep_current"

const val SHIFTING_CATEGORY_GROWTH = "growth"
const val SHIFTING_CATEGORY_HEALTH = "health"
const val SHIFTING_CATEGORY_BREEDING = "breeding"
const val SHIFTING_CATEGORY_DELIVERY = "delivery"

/**
 * Spacing and Flushing joined the vocabulary with the 2026-08-20 rewrite: the category is now the
 * shift TYPE, and the type decides what happens to the animals' tag on the backend. The form only
 * names WHY the animals move; every tag outcome and every refusal reason is backend-owned copy.
 */
const val SHIFTING_CATEGORY_SPACING = "spacing"
const val SHIFTING_CATEGORY_FLUSHING = "flushing"

sealed interface ShiftingEvent {
    data class EditAnimalQuery(val value: String) : ShiftingEvent
    data object LookupAnimals : ShiftingEvent

    /** Selects THE animal. Selecting another one replaces this; it never appends. */
    data class SelectAnimal(val goatId: String) : ShiftingEvent

    /** Compatibility event only; the ViewModel accepts only the selected animal's current park. */
    data class SelectDestinationPark(val parkId: String) : ShiftingEvent
    data class SelectDestinationShed(val shedId: String, val partitionLabel: String? = null) : ShiftingEvent

    /** Flips the tag toggle. Ignored by the ViewModel when the pen cannot supply a tag. */
    data class SelectStageMode(val stageMode: String) : ShiftingEvent

    data class SelectPriority(val priority: String) : ShiftingEvent
    data class SelectCategory(val category: String) : ShiftingEvent

    data class EditComment(val value: String) : ShiftingEvent

    data object Submit : ShiftingEvent
    data object NavigationHandled : ShiftingEvent
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
) {
    Column(modifier = modifier.fillMaxSize().background(MeshaColors.PageBg)) {
        CountsFormHeader(
            title = stringResource(R.string.counts_shifting_title),
            subtitle = stringResource(R.string.counts_shifting_subtitle),
            onBack = { onEvent(ShiftingEvent.Back) },
        )
        // The dropdowns below are the pickers; this is only what they currently mean, mirrored.
        val selectedParkName = state.destinationParks.firstOrNull { it.parkId == state.destinationParkId }?.name
        val selectedDestination = state.selectedDestination
        // The name is already formatted by the backend (operational_location_display); don't re-format
        val selectedShedLabel = selectedDestination?.name

        LazyColumn(
            modifier = Modifier.fillMaxSize().weight(1f).padding(horizontal = 16.dp),
            contentPadding = PaddingValues(bottom = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item(key = "result") { CountsResultBanner(state.result) }
            // A synced movement clears the form and leaves this confirmation above the fresh entry.
            state.lastRecordedMessage?.let { message ->
                item(key = "recorded") {
                    CountsResultBanner(CountsWriteResultUi(CountsWriteStatus.SYNCED, message))
                }
            }

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

            // --- 2. Animal hero: From (current, read-only) -> To (mirrors the destination
            // dropdowns below; never a second picker of its own) -------------------------------
            state.selectedAnimal?.let { animal ->
                item(key = "animal-hero") {
                    ShiftingAnimalHero(
                        animal = animal,
                        toParkLabel = selectedParkName,
                        toShedLabel = selectedShedLabel,
                    )
                }
            }

            // --- 3. Destination: current farm is locked; only its sheds are selectable --------
            item(key = "destination-title") {
                CountsFieldGroupTitle(text = stringResource(R.string.counts_group_to))
            }
            item(key = "destination-park") {
                CountsTextField(
                    value = selectedParkName.orEmpty(),
                    onValueChange = {},
                    label = stringResource(R.string.counts_field_farm),
                    supporting = stringResource(R.string.counts_shifting_farm_locked),
                    readOnly = true,
                )
            }
            item(key = "destination-shed") {
                // Entries are one per WHOLE shed or one per PARTITION of a partitioned shed —
                // never both. Keyed by shed_id + partition_label: shed NAMES repeat across
                // parks and a shed's own partitions share its shed_id, so either alone would
                // collapse distinct destinations into one entry.
                //
                // The name is already formatted by the backend (operational_location_display):
                // "Yashoda" (non-partitioned), "Castro 2" (numeric), or "Godel 1 - Part 3"
                // (worded). Do NOT re-format it — the ShiftingViewModel.toShiftingParkUi()
                // already applied the proper formatting when importing from the backend.
                val destinations = state.shedsForSelectedPark
                CountsDropdownField(
                    label = stringResource(R.string.counts_field_shed),
                    selectedLabel = selectedShedLabel,
                    placeholder = if (state.selectedAnimal == null) {
                        stringResource(R.string.counts_select_animal_first)
                    } else {
                        stringResource(R.string.counts_select_shed)
                    },
                    options = destinations.map {
                        CountsDropdownOption(it.optionKey, it.name)
                    },
                    onSelect = { key ->
                        val chosen = destinations.firstOrNull { it.optionKey == key }
                        onEvent(ShiftingEvent.SelectDestinationShed(chosen?.shedId.orEmpty(), chosen?.partitionLabel))
                    },
                    enabled = destinations.isNotEmpty(),
                )
            }
            state.destinationsMessage?.let { message ->
                item(key = "destinations-message") {
                    Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                }
            }

            // --- 3b. Destination tag context -------------------------------------------------
            // The TAG TOGGLE is retired (2026-08-20 rewrite): the movement's CATEGORY decides what
            // happens to the animals' tag on the backend, so the raiser is no longer asked. What
            // remains is CONTEXT about the chosen pen -- its tag when it has one, else the
            // backend's reason it has none. Both are backend-owned strings rendered verbatim; the
            // phone never composes either.
            if (state.destinationStageLabel != null || state.destinationStageReason != null) {
                item(key = "destination-tag-context") {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        state.destinationStageLabel?.let { tag ->
                            Text(text = tag, color = MeshaColors.Muted, fontSize = 12.sp)
                        }
                        state.destinationStageReason?.let { reason ->
                            Text(text = reason, color = MeshaColors.Muted, fontSize = 12.sp)
                        }
                    }
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
                            SHIFTING_CATEGORY_SPACING to stringResource(R.string.counts_category_spacing),
                            SHIFTING_CATEGORY_FLUSHING to stringResource(R.string.counts_category_flushing),
                        ),
                        selectedKey = state.category,
                        onSelect = { onEvent(ShiftingEvent.SelectCategory(it)) },
                    )
                }
            }

            // --- 6. Comment ------------------------------------------------------------------
            // Optional by design: it never gates Submit. It is read by the park head approving the
            // movement and by the verifier reviewing the evidence afterwards.
            item(key = "comment") {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    CountsFieldGroupTitle(text = stringResource(R.string.counts_group_comment))
                    CountsTextField(
                        value = state.comment,
                        onValueChange = { onEvent(ShiftingEvent.EditComment(it)) },
                        label = stringResource(R.string.counts_field_comment),
                        supporting = stringResource(R.string.counts_hint_comment),
                        singleLine = false,
                    )
                }
            }

            state.validationMessage?.let { message ->
                item(key = "validation") {
                    Text(text = message, color = MeshaColors.Warn, fontSize = 12.sp)
                }
            }
        }

        // Sticky submit + quiet pending line, pinned OUTSIDE the scroll so it is always reachable
        // without hunting for it at the bottom of a long form.
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .background(MeshaColors.PageBg)
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            CountsSubmitButton(
                label = stringResource(R.string.counts_submit_shifting),
                enabled = state.canSubmit,
                onClick = { onEvent(ShiftingEvent.Submit) },
            )
            Text(
                text = stringResource(R.string.counts_shifting_pending_note),
                color = MeshaColors.Faint,
                fontSize = 10.sp,
            )
        }
    }
}

/**
 * The animal hero: From (current park/shed, read-only) -> To (a live reflection of the
 * destination dropdowns below — NOT a second picker; selecting nothing yet renders an honest
 * placeholder rather than inventing a destination).
 */
@Composable
private fun ShiftingAnimalHero(
    animal: ShiftingAnimalUi,
    toParkLabel: String?,
    toShedLabel: String?,
) {
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(MeshaColors.Surf)
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(text = animal.displayId, color = MeshaColors.Ink, fontSize = 16.sp, fontWeight = FontWeight.W800)
        Text(text = animal.tag, color = MeshaColors.Muted, fontSize = 12.sp)
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            HeroLocationChip(
                modifier = Modifier.weight(1f),
                label = stringResource(R.string.counts_shifting_from),
                parkLabel = animal.parkName,
                // The operational location — shed name plus partition when the animal's shed is
                // partitioned (e.g. "Yashoda 2"), so a same-shed cross-partition move is visible.
                shedLabel = operationalLocationLabel(animal.shedName, animal.partitionLabel),
                fallback = animal.locationLabel,
                accent = MeshaColors.Faint,
            )
            Icon(
                imageVector = MeshaIcons.ChevronDown,
                contentDescription = null,
                tint = MeshaColors.Muted,
                modifier = Modifier.size(16.dp),
            )
            HeroLocationChip(
                modifier = Modifier.weight(1f),
                label = stringResource(R.string.counts_shifting_to),
                parkLabel = toParkLabel.orEmpty(),
                shedLabel = toShedLabel.orEmpty(),
                fallback = "",
                accent = MeshaColors.Brand,
            )
        }
    }
}

@Composable
private fun HeroLocationChip(
    label: String,
    parkLabel: String,
    shedLabel: String,
    fallback: String,
    accent: Color,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(12.dp))
            .background(MeshaColors.Surf2)
            .padding(horizontal = 10.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Text(text = label, color = MeshaColors.Faint, fontSize = 10.sp, fontWeight = FontWeight.W700)
        val hasParts = parkLabel.isNotBlank() || shedLabel.isNotBlank()
        val display = when {
            hasParts -> listOf(parkLabel, shedLabel).filter { it.isNotBlank() }.joinToString(" · ")
            fallback.isNotBlank() -> fallback
            else -> stringResource(R.string.counts_select_shed)
        }
        Text(text = display, color = accent, fontSize = 13.sp, fontWeight = FontWeight.W700)
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
