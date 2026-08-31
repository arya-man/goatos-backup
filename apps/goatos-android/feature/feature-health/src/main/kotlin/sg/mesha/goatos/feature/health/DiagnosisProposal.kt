package sg.mesha.goatos.feature.health

import androidx.compose.runtime.Immutable

/**
 * What came back after a manager recorded one observation.
 *
 * PURE, like [ObservationFormState]: no network types, no Compose state holders.
 * The wire-to-model mapping lives beside the view model, because feature modules
 * do not depend on core-network.
 *
 * The shape here is the product's, not the wire's, and the ordering below is the
 * order a person in a shed needs the information:
 *
 *  1. [emergencies] — do this NOW. They do not wait for the Director, and they do
 *     not wait for a network round trip either.
 *  2. [unexplained] — what the assessment could NOT account for. Prominent by
 *     design: naming a disease compresses a set of findings down to one label, and
 *     anything the label does not cover is exactly where a wrong diagnosis hides.
 *  3. [problems] — ranked, most serious first.
 *  4. [fieldActions] — treated in place, once, no follow-up.
 *  5. The rest — housing, rechecks, notes.
 */
@Immutable
data class DiagnosisProposalState(
    val loading: Boolean = false,
    val refreshing: Boolean = false,
    val goatDisplayId: String = "",
    /** `proposed`, `confirmed` or `declined`; blank until the assessment arrives. */
    val status: String = "",
    val emergencies: List<String> = emptyList(),
    val unexplained: List<String> = emptyList(),
    val problems: List<ProposedProblem> = emptyList(),
    val fieldActions: List<String> = emptyList(),
    val rechecks: List<String> = emptyList(),
    val covered: List<String> = emptyList(),
    val housing: HousingDirective = HousingDirective(),
    val notes: List<String> = emptyList(),
    /** Whether THIS user may decide. Backend-owned; never inferred from a role here. */
    val mayConfirm: Boolean = false,
    /** Which problems the Director has ticked. Local until they send the decision. */
    val selected: Set<String> = emptySet(),
    val sending: Boolean = false,
    val message: String? = null,
) {
    val decided: Boolean get() = status.isNotBlank() && status != "proposed"

    /**
     * Whether the decision can be sent.
     *
     * An EMPTY selection is allowed and meaningful: it declines the whole
     * assessment, which is a real decision and is recorded as one. What blocks the
     * send is a selection containing something that cannot open a course.
     */
    val canSend: Boolean
        get() = mayConfirm && !decided && !sending &&
            problems.none { it.id in selected && !it.canConfirm }

    /** The problems ticked that have no treatment plan, so the screen can name them. */
    val blockedSelections: List<ProposedProblem>
        get() = problems.filter { it.id in selected && !it.canConfirm }
}

/**
 * One proposed diagnosis as the Director sees it.
 *
 * [canConfirm] is the honest half. Some diagnoses point at a treatment plan nobody
 * has written yet; confirming one cannot open a course, so the Director must see
 * that BEFORE deciding rather than hit it as a failure after.
 */
@Immutable
data class ProposedProblem(
    val id: String,
    val label: String,
    /** CONFIRMED / PROBABLE / POSSIBLE, in farm words. */
    val confidence: String,
    val canConfirm: Boolean,
    /** Why it cannot be confirmed, in the operator's language. Blank when it can. */
    val blockedReason: String = "",
)

/**
 * Where the animal should be kept.
 *
 * A DIRECTIVE the screen SHOWS. Health never moves an animal or changes its
 * containment — the workflow that owns location is the only writer — so nothing
 * here is acted on by this screen.
 */
@Immutable
data class HousingDirective(
    val acuity: String = "",
    val containment: String = "",
    val lowCompetition: Boolean = false,
) {
    val hasDirective: Boolean
        get() = acuity.isNotBlank() || containment.isNotBlank() || lowCompetition
}

sealed interface DiagnosisProposalEvent {
    data object Refresh : DiagnosisProposalEvent
    data class ToggleProblem(val id: String) : DiagnosisProposalEvent
    data object Send : DiagnosisProposalEvent
    data object Back : DiagnosisProposalEvent
}

/**
 * Ticks or unticks one problem.
 *
 * A problem with no treatment plan is still selectable, so the screen can explain
 * WHY it cannot be sent rather than silently ignoring a tap on it — a control that
 * does nothing reads as a broken screen.
 */
fun toggleSelection(current: Set<String>, id: String): Set<String> =
    if (id in current) current - id else current + id
