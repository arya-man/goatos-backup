package sg.mesha.goatos.core.network.dto

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * Wire contract for the Health diagnosis engine.
 *
 * The loop these types serve: the health manager records one head-to-toe
 * observation, the backend's deterministic register returns a ranked PROPOSAL,
 * and the Health Director confirms before any treatment course opens. Submitting
 * opens nothing.
 *
 * Two properties survive into this layer and are easy to erase by accident:
 *
 *  - The request names the animal by id ONLY. Species, sex, age band and status
 *    are read server-side because the register gates whole diagnoses on them.
 *  - [HealthDiagnosisProposalDto.emergencies] and [field actions] do NOT wait for
 *    a confirmation. Everything else on the proposal does.
 */

/**
 * One field of the observation form that accepts either a single value or
 * several, matching the paper form's multi-tick boxes.
 *
 * Serialised as a plain list. The backend's own decoder accepts a bare string or
 * an array, so a single-value field could be sent either way; always sending the
 * array keeps one shape on the wire and one shape in the tests.
 */
typealias HealthFindingValues = List<String>

/**
 * The observation form.
 *
 * Every field is nullable and defaults to null so a partially-filled draft can be
 * held locally. The PRODUCT rule is that the form is compulsory and complete
 * before submit -- sex-hidden fields record N/A rather than blank, because a
 * blank cannot distinguish "nobody looked" from "normal", and the
 * unexplained-findings channel depends on that distinction. That completeness is
 * enforced at the screen, not by the wire type, so a draft can be saved.
 */
@Serializable
data class HealthObservationFindingsDto(
    /** Rectal temperature in Fahrenheit, one decimal. */
    val temp: Double? = null,
    val eating: HealthFindingValues? = null,
    val activity: String? = null,
    val breathing: HealthFindingValues? = null,
    val nasal: Boolean? = null,
    @SerialName("left_stomach") val leftStomach: HealthFindingValues? = null,
    @SerialName("frothy_mouth") val frothyMouth: Boolean? = null,
    @SerialName("rumen_movement") val rumenMovement: String? = null,
    val diarrhea: Boolean? = null,
    @SerialName("skin_tent") val skinTent: String? = null,
    val cmt: String? = null,
    val lactation: String? = null,
    val udder: String? = null,
    val vulva: String? = null,
    val famacha: Int? = null,
    val yellow: Boolean? = null,
    val straining: String? = null,
    @SerialName("red_urine") val redUrine: Boolean? = null,
    @SerialName("body_edema") val bodyEdema: Boolean? = null,
    val competition: Boolean? = null,
    @SerialName("stomach_inside") val stomachInside: Boolean? = null,
    val mouth: String? = null,
    val eyes: HealthFindingValues? = null,
    @SerialName("locked_jaw") val lockedJaw: Boolean? = null,
    val neuro: HealthFindingValues? = null,
    @SerialName("rash_character") val rashCharacter: String? = null,
    val hairloss: Boolean? = null,
    val leg: String? = null,
    val lumps: String? = null,
    val wounds: HealthFindingValues? = null,
    val flystrike: Boolean? = null,
    @SerialName("eartag_flystrike") val eartagFlystrike: Boolean? = null,
    @SerialName("eartag_wound") val eartagWound: Boolean? = null,
    val ticks: Boolean? = null,
)

/**
 * Follow-up state a single form cannot carry.
 *
 * There is deliberately no `open` field. The animal's open courses are resolved
 * server-side; a client able to assert them could suppress the reconcile and make
 * a follow-up read as a fresh diagnosis, duplicating every course under way.
 */
@Serializable
data class HealthObservationContextDto(
    /** Follow-up day; day 1 is the day treatment started. */
    val day: Int? = null,
    @SerialName("shed_similar") val shedSimilar: Int? = null,
    val hour: Int? = null,
)

@Serializable
data class SubmitHealthObservationRequestDto(
    @SerialName("goat_id") val goatId: String,
    val findings: HealthObservationFindingsDto,
    val context: HealthObservationContextDto = HealthObservationContextDto(),
)

/**
 * Where the animal should be, and which shift lists it appears on.
 *
 * A DIRECTIVE only. Health never moves an animal or changes its containment --
 * the workflow that owns location is the only writer, so this is something the
 * app SHOWS, never something it acts on.
 */
@Serializable
data class HealthHousingDirectiveDto(
    val acuity: String = "",
    val containment: String = "",
    @SerialName("low_competition") val lowCompetition: Boolean = false,
    @SerialName("morning_walk") val morningWalk: Boolean = false,
    @SerialName("evening_walk") val eveningWalk: Boolean = false,
    @SerialName("no_due_overnight") val noDueOvernight: Boolean = true,
)

@Serializable
data class HealthDiagnosisProposalDto(
    val valid: Boolean = false,
    @SerialName("reject_reason") val rejectReason: String? = null,
    val scope: String = "",
    /** The rule table this run used, pinned so the proposal stays readable after an edit. */
    @SerialName("register_version") val registerVersion: String = "",
    /** Do this now. Actionable without waiting for the Director. */
    val emergencies: List<String> = emptyList(),
    /** Ranked severity first, then confidence. */
    val problems: List<String> = emptyList(),
    /** Merged into another diagnosis's treatment; kept so it can be re-opened first if the animal does not improve. */
    val covered: List<String> = emptyList(),
    val rechecks: List<String> = emptyList(),
    /** Treated in place and once. No daily follow-up. */
    @SerialName("field_actions") val fieldActions: List<String> = emptyList(),
    /** Abnormal findings no diagnosis accounts for. Show prominently, never as a footnote. */
    val unexplained: List<String> = emptyList(),
    val ongoing: List<String> = emptyList(),
    @SerialName("new") val newProblems: List<String> = emptyList(),
    @SerialName("propose_close") val proposeClose: List<String> = emptyList(),
    @SerialName("propose_extend") val proposeExtend: List<String> = emptyList(),
    val tiers: Map<String, String> = emptyMap(),
    val sop: Map<String, String> = emptyMap(),
    @SerialName("course_type") val courseType: Map<String, String> = emptyMap(),
    val housing: HealthHousingDirectiveDto = HealthHousingDirectiveDto(),
    @SerialName("hd_flags") val directorFlags: List<String> = emptyList(),
    val hints: List<String> = emptyList(),
    @SerialName("no_meloxicam") val noMeloxicam: Boolean = false,
    val club: Boolean = false,
)

/**
 * One proposed diagnosis, with everything the Director needs to decide.
 *
 * [sopAvailable] is the honest half: some diagnoses point at a treatment plan
 * nobody has written yet. Confirming one of those cannot open a course, and the
 * Director must see that BEFORE deciding rather than hit it as a failure after.
 */
@Serializable
data class HealthConfirmableProblemDto(
    val id: String = "",
    val tier: String = "",
    @SerialName("sop_ref") val sopRef: String = "",
    @SerialName("exit_type") val exitType: String = "",
    @SerialName("disease_key") val diseaseKey: String = "",
    @SerialName("sop_available") val sopAvailable: Boolean = false,
    @SerialName("blocked_reason") val blockedReason: String? = null,
)

@Serializable
data class HealthDiagnosisProposalResponseDto(
    @SerialName("health_diagnosis_run_id") val diagnosisRunId: String = "",
    val status: String = "",
    val proposal: HealthDiagnosisProposalDto = HealthDiagnosisProposalDto(),
    val confirmable: List<HealthConfirmableProblemDto> = emptyList(),
    /**
     * Whether this device's user may also decide. Normally false — the health
     * manager records, the Director confirms — but a Director recording an
     * observation themselves collapses the two acts into one visit. Defaults false
     * so a response that omits it hides the control rather than offering one that fails.
     */
    @SerialName("may_confirm") val mayConfirm: Boolean = false,
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

/**
 * The Director's decision.
 *
 * An EMPTY list is legitimate: it declines the whole proposal, which is an
 * override and still decides the run. It is not the same as not answering.
 */
@Serializable
data class ConfirmHealthDiagnosisRequestDto(
    @SerialName("confirmed_problems") val confirmedProblems: List<String> = emptyList(),
)

@Serializable
data class HealthOpenedCaseDto(
    @SerialName("case_id") val caseId: String = "",
    @SerialName("disease_key") val diseaseKey: String = "",
    @SerialName("exit_type") val exitType: String = "",
    /** Present only for a fixed-length course; null when it closes on a test or on review. */
    @SerialName("duration_days") val durationDays: Int? = null,
    @SerialName("session_count") val sessionCount: Int = 0,
)

@Serializable
data class ConfirmHealthDiagnosisResponseDto(
    @SerialName("health_diagnosis_run_id") val diagnosisRunId: String = "",
    val status: String = "",
    @SerialName("opened_cases") val openedCases: List<HealthOpenedCaseDto> = emptyList(),
    /** What the Director chose not to confirm. Shown rather than dropped. */
    val declined: List<String> = emptyList(),
    @SerialName("idempotent_replay") val idempotentReplay: Boolean = false,
)

@Serializable
data class HealthDiagnosisRunDto(
    @SerialName("health_diagnosis_run_id") val diagnosisRunId: String = "",
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("register_version") val registerVersion: String = "",
    @SerialName("observed_by") val observedBy: String = "",
    @SerialName("observed_at") val observedAt: String = "",
    @SerialName("business_date") val businessDate: String = "",
    val proposal: HealthDiagnosisProposalDto = HealthDiagnosisProposalDto(),
    val status: String = "",
    /**
     * What the Director may still act on. Carried on the READ, so this device can
     * offer the decision without ever having seen the submit response — which is
     * the normal case: the manager submits from their phone, the Director decides
     * from theirs. Empty once the run is decided.
     */
    val confirmable: List<HealthConfirmableProblemDto> = emptyList(),
    /**
     * Whether THIS device's user may cast the decision. A separate fact from
     * [confirmable] — the manager who recorded the observation receives the same
     * list and is precisely the person who must not confirm it. Defaults false so
     * a response that omits it hides the control rather than offering one that fails.
     */
    @SerialName("may_confirm") val mayConfirm: Boolean = false,
    @SerialName("confirmed_by") val confirmedBy: String? = null,
    @SerialName("confirmed_at") val confirmedAt: String? = null,
)

/**
 * One row of the Health Director's queue.
 *
 * Carries enough to decide WHAT TO OPEN FIRST and nothing more — the animal, where
 * it is, when it was seen, the ranked problems, and how bad it looks. The full
 * proposal is a separate read, because shipping every proposal in full would make
 * the queue a page fetch that grows with the register.
 */
@Serializable
data class HealthDiagnosisQueueItemDto(
    @SerialName("health_diagnosis_run_id") val diagnosisRunId: String = "",
    @SerialName("goat_id") val goatId: String = "",
    @SerialName("goat_display_id") val goatDisplayId: String = "",
    @SerialName("shed_name") val shedName: String = "",
    @SerialName("partition_label") val partitionLabel: String = "",
    /** Backend-composed `shed - partition`. Rendered verbatim, never re-derived here. */
    @SerialName("operational_location_display") val operationalLocationDisplay: String = "",
    @SerialName("observed_at") val observedAt: String = "",
    @SerialName("business_date") val businessDate: String = "",
    val status: String = "",
    /** Ranked severity first, then confidence. The order is the backend's — never re-sort it. */
    val problems: List<String> = emptyList(),
    /** Things that need doing NOW. On the ROW because they do not wait for the Director. */
    @SerialName("emergency_count") val emergencyCount: Int = 0,
    @SerialName("unexplained_count") val unexplainedCount: Int = 0,
)

@Serializable
data class HealthDiagnosisQueuePageDto(
    val items: List<HealthDiagnosisQueueItemDto> = emptyList(),
    /** Null on the last page. A keyset cursor, never an offset. */
    @SerialName("next_cursor") val nextCursor: String? = null,
    /** Whether this device's user may decide any of it. Defaults false — fail closed. */
    @SerialName("may_confirm") val mayConfirm: Boolean = false,
)
