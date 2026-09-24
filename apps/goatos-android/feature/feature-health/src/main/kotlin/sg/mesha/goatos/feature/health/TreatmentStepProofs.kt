package sg.mesha.goatos.feature.health

import androidx.compose.runtime.Immutable

/**
 * ONE VIDEO PER TREATMENT STEP, as the screen holds it.
 *
 * Maintainer, 2026-09-23: every step of a session gets its own video; the operator submits once
 * when they are all recorded; the verifier receives the set as one item.
 *
 * THE STATE THAT MATTERS IS [RECORDED], AND IT IS NOT THE UPLOAD. A blob reaching storage is not
 * the business fact -- `docs/decisions/proof-business-ack-contract.md`, written after PC Care
 * showed "Video sent" off an upload row while the feature row's proof ref was empty. A step here
 * reads recorded only once its REGISTER has landed, so a step whose video uploaded and whose
 * register then failed shows as still owing, which is the truth.
 */
enum class StepProofState {
    /** Nothing filmed yet. */
    NONE,

    /** The video is uploading, or uploaded and the register has not landed. NOT recorded. */
    SENDING,

    /** The register landed. The server holds this step's clip. */
    RECORDED,

    /** Terminal: the operator must act. Never left sitting as "sending". */
    FAILED,
}

/** One step's capture, and how far it has actually got. */
@Immutable
data class TreatmentStepProof(
    val stepId: String,
    val state: StepProofState = StepProofState.NONE,
    /** The PROOF_UPLOAD outbox row carrying the bytes. */
    val uploadOutboxItemId: String = "",
    /** The register row that attaches the uploaded clip to this step -- the business write. */
    val registerOutboxItemId: String = "",
    /** Backend or sync wording for a failure, shown verbatim. */
    val message: String = "",
) {
    val recorded: Boolean get() = state == StepProofState.RECORDED
    val readyForSubmit: Boolean get() = state == StepProofState.RECORDED || state == StepProofState.SENDING
}

/** Every step's capture on one session's card. */
@Immutable
data class TreatmentStepProofs(val byStep: Map<String, TreatmentStepProof> = emptyMap()) {

    fun of(stepId: String): TreatmentStepProof =
        byStep[stepId] ?: TreatmentStepProof(stepId = stepId)

    fun with(proof: TreatmentStepProof): TreatmentStepProofs =
        TreatmentStepProofs(byStep + (proof.stepId to proof))

    /**
     * The steps that still owe a recorded video, in the order the operator works them.
     *
     * A step whose upload succeeded but whose register failed is STILL OWED. That is the whole
     * point: the server does not hold that clip against the step, so submitting would be refused
     * anyway -- and telling the operator otherwise is how a proof goes missing quietly.
     */
    fun missing(stepIds: List<String>): List<String> =
        stepIds.filterNot { of(it).recorded }

    fun missingForSubmit(stepIds: List<String>): List<String> =
        stepIds.filterNot { of(it).readyForSubmit }

    /** Whether the card can be submitted: every step recorded, and at least one step to record. */
    fun complete(stepIds: List<String>): Boolean =
        stepIds.isNotEmpty() && missing(stepIds).isEmpty()

    fun readyForSubmit(stepIds: List<String>): Boolean =
        stepIds.isNotEmpty() && missingForSubmit(stepIds).isEmpty()
}
