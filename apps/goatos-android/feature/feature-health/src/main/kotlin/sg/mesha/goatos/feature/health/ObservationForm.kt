package sg.mesha.goatos.feature.health

import androidx.compose.runtime.Immutable

/**
 * The observation form: one head-to-toe pass over one sick animal.
 *
 * This file is deliberately PURE — no Compose state holders, no coroutines, and
 * no network types: feature modules do not depend on core-network, so the
 * form-to-wire mapping lives beside the view model instead. The completeness rule and the four exclusivity rules below decide
 * whether a form may be submitted at all, and they are the kind of thing worth
 * proving in a plain JVM test rather than through a screen.
 *
 * Two product rules shape the whole design:
 *
 *  1. EVERY FIELD IS COMPULSORY. A blank cannot distinguish "nobody looked" from
 *     "normal", and the engine's unexplained-findings channel — the thing that
 *     catches what the diagnosis failed to account for — depends on that
 *     distinction. Sex-hidden fields record "not applicable" rather than nothing.
 *     Wrong ticks still happen; missing ticks must not.
 *
 *  2. THE FORM NEVER NAMES A DISEASE. The manager records what they see; the
 *     engine proposes and the Director confirms. There is no disease field here
 *     and there must never be one.
 */

/** Values a single-choice field can take, in the order they appear on the form. */
object ObservationOptions {
    val eating = listOf("normal", "not_eating", "concentrate", "green_feed", "dry_feed")
    val activity = listOf("standing", "down", "limping", "back_leg_drag", "front_knees", "weak")
    val breathing = listOf("normal", "fast", "labored", "cough", "pant")
    val leftStomach = listOf("normal", "bloating", "acidosis")
    val rumenMovement = listOf("felt", "not_felt")
    val skinTent = listOf("lt2", "2-4", "gt4")
    val eyes = listOf("normal", "red", "cloudy", "discharge")
    val mouth = listOf("normal", "orf_scabs")
    val neuro = listOf("none", "circling", "head_tilt", "star_gazing", "blind", "tremors", "ataxia")
    val leg = listOf("normal", "arthritis", "fracture", "foot_rot")
    val wounds = listOf("no", "horn", "neck", "body", "legs")
    val lumps = listOf("no", "neck", "body")
    val rashCharacter = listOf("none", "flat_itchy", "nodular")
    val famacha = listOf("1", "2", "3", "4", "5")

    /** Female-only. */
    val udder = listOf("normal", "swollen_hard", "rashes", "wound", "lumps")
    val lactation = listOf("no", "milk", "colostrum", "water", "pus")
    val cmt = listOf("pos", "neg")
    val vulva = listOf("none", "lochia_normal", "discharge_bad_smell", "pus", "prolapse")

    /** Male-only. */
    val straining = listOf("no", "straining", "no_urine")
}

/**
 * Why a form cannot be submitted yet. Each maps to a rule the backend also
 * enforces; catching them here means the manager fixes the tick in front of the
 * animal rather than receiving a rejection later.
 */
enum class ObservationBlocker {
    /** A required field has not been answered. */
    INCOMPLETE,

    /** "Not eating" ticked alongside a feed the animal took. */
    NOT_EATING_WITH_FEED,

    /** "No wounds" ticked alongside a wound site. */
    WOUNDS_EXCLUSIVE,

    /** A female cannot be recorded as straining to urinate. */
    FEMALE_STRAINING,

    /** CMT is a milk test; it cannot be read when there is no milk. */
    CMT_WITHOUT_MILK,
}

/**
 * The form as the operator has filled it so far.
 *
 * Multi-select fields are sets so "normal" and a real finding cannot both be
 * silently held; the exclusivity rules below reject that combination explicitly
 * rather than quietly dropping one.
 */
@Immutable
data class ObservationFormState(
    val sex: String = "",
    val temp: String = "",
    val eating: Set<String> = emptySet(),
    val activity: String = "",
    val breathing: Set<String> = emptySet(),
    val nasal: Boolean? = null,
    val leftStomach: Set<String> = emptySet(),
    val frothyMouth: Boolean? = null,
    val rumenMovement: String = "",
    val diarrhea: Boolean? = null,
    val skinTent: String = "",
    val famacha: String = "",
    val yellow: Boolean? = null,
    val eyes: Set<String> = emptySet(),
    val mouth: String = "",
    val lockedJaw: Boolean? = null,
    val neuro: Set<String> = emptySet(),
    val leg: String = "",
    val wounds: Set<String> = emptySet(),
    val lumps: String = "",
    val rashCharacter: String = "",
    val hairloss: Boolean? = null,
    val ticks: Boolean? = null,
    val flystrike: Boolean? = null,
    val eartagFlystrike: Boolean? = null,
    val eartagWound: Boolean? = null,
    val redUrine: Boolean? = null,
    val bodyEdema: Boolean? = null,
    val competition: Boolean? = null,
    val stomachInside: Boolean? = null,
    // Female only
    val udder: String = "",
    val lactation: String = "",
    val cmt: String = "",
    val vulva: String = "",
    // Male only
    val straining: String = "",
) {
    val isFemale: Boolean get() = sex.equals("female", ignoreCase = true)
    val isMale: Boolean get() = sex.equals("male", ignoreCase = true)

    /** CMT is only asked when there is milk to test. */
    val cmtApplies: Boolean get() = isFemale && lactation.isNotBlank() && lactation != "no"
}

/**
 * Everything standing between this form and submission, in the order a person
 * would fix them. Empty means ready.
 *
 * Returning the whole set rather than the first problem is deliberate: a
 * 30-field form corrected one complaint at a time is not fillable in a shed.
 */
fun ObservationFormState.blockers(): List<ObservationBlocker> {
    val out = mutableListOf<ObservationBlocker>()

    // 1. Exclusivity, checked before completeness so a contradiction is named as
    //    a contradiction rather than reported as a missing answer.
    if (eating.contains("not_eating") &&
        eating.any { it == "normal" || it == "concentrate" || it == "green_feed" || it == "dry_feed" }
    ) {
        out += ObservationBlocker.NOT_EATING_WITH_FEED
    }
    if (wounds.contains("no") && wounds.any { it != "no" }) {
        out += ObservationBlocker.WOUNDS_EXCLUSIVE
    }
    if (isFemale && (straining == "straining" || straining == "no_urine")) {
        out += ObservationBlocker.FEMALE_STRAINING
    }
    if (lactation == "no" && cmt.isNotBlank()) {
        out += ObservationBlocker.CMT_WITHOUT_MILK
    }

    if (missingFields().isNotEmpty()) out += ObservationBlocker.INCOMPLETE
    return out
}

/**
 * The form is walked in four steps rather than as one 29-question scroll.
 *
 * The order is still the order a person walks an animal — take the temperature,
 * look at the head, work down the body, then the sex-specific checks. What the
 * steps add is a horizon: a manager in a shed can see how much is left and finish
 * one part before the next, instead of scrolling a wall of questions to find the
 * two they have not answered.
 *
 * Steps DO NOT change what is required. Every field is still compulsory and the
 * blockers are unchanged; a step is a view onto the same completeness rule, which
 * is why [missingFields] is now assembled from the per-step lists rather than kept
 * as a second copy that could drift from them.
 */
enum class ObservationStep { VITALS, HEAD, BODY, FINAL }

/**
 * The step's heading. The last one names the sex whose questions it is showing,
 * because that is the step where the form stops being the same for every animal.
 */
fun ObservationFormState.stepHeading(step: ObservationStep): String = when (step) {
    ObservationStep.VITALS -> "Vitals"
    ObservationStep.HEAD -> "Head · eyes · breathing"
    ObservationStep.BODY -> "Gut · skin · legs"
    ObservationStep.FINAL -> when {
        isFemale -> "Female — udder · vulva"
        isMale -> "Male — urine"
        else -> "Other checks"
    }
}

/**
 * The fields still unanswered in ONE step, so the screen can hold the operator
 * there until that part of the animal has actually been checked.
 *
 * Sex-scoped fields are only required for the sex they apply to — that is what
 * "hidden fields record N/A" means in practice. CMT is required only when there
 * is milk, because a CMT on a dry doe is the CMT_WITHOUT_MILK contradiction.
 */
fun ObservationFormState.missingFields(step: ObservationStep): List<String> {
    val missing = mutableListOf<String>()
    fun require(condition: Boolean, name: String) { if (!condition) missing += name }

    when (step) {
        ObservationStep.VITALS -> {
            require(temp.toDoubleOrNull() != null, "temperature")
            require(famacha.isNotBlank(), "FAMACHA")
            require(yellow != null, "yellow membranes")
            require(skinTent.isNotBlank(), "skin tent")
            require(stomachInside != null, "sunken flank")
        }
        ObservationStep.HEAD -> {
            require(eyes.isNotEmpty(), "eyes")
            // The mouth is asked as one question with several answers, so all three
            // of its facts are required together.
            require(mouth.isNotBlank(), "mouth")
            require(frothyMouth != null, "frothy mouth")
            require(lockedJaw != null, "locked jaw")
            require(breathing.isNotEmpty(), "breathing")
            require(nasal != null, "nasal discharge")
        }
        ObservationStep.BODY -> {
            require(leftStomach.isNotEmpty(), "left stomach")
            require(rumenMovement.isNotBlank(), "rumen movement")
            require(diarrhea != null, "loose motion")
            require(eating.isNotEmpty(), "eating")
            require(ticks != null, "ticks")
            require(hairloss != null, "hair loss")
            require(wounds.isNotEmpty(), "wounds")
            require(lumps.isNotBlank(), "lumps")
            require(rashCharacter.isNotBlank(), "rashes")
            require(flystrike != null, "maggots")
            require(eartagFlystrike != null, "ear tag maggots")
            require(eartagWound != null, "ear tag wound")
            require(activity.isNotBlank(), "activity")
            require(leg.isNotBlank(), "legs")
            require(neuro.isNotEmpty(), "nervous signs")
        }
        ObservationStep.FINAL -> {
            if (isFemale) {
                require(udder.isNotBlank(), "udder")
                require(lactation.isNotBlank(), "milk")
                require(vulva.isNotBlank(), "vulva")
                if (cmtApplies) require(cmt.isNotBlank(), "CMT")
            }
            if (isMale) {
                require(straining.isNotBlank(), "urine")
            }
            require(redUrine != null, "red urine")
            require(bodyEdema != null, "swelling under the jaw")
            require(competition != null, "pushed off feed")
        }
    }
    return missing
}

/**
 * Everything still unanswered across the whole form.
 *
 * Assembled from the per-step lists so the two can never disagree: a field added
 * to a step is required by the form, and a field in no step is required by
 * neither, which is a visible hole rather than a silent one.
 */
fun ObservationFormState.missingFields(): List<String> =
    ObservationStep.entries.flatMap { missingFields(it) }

/** A step is done when nothing in it is unanswered. */
fun ObservationFormState.isStepComplete(step: ObservationStep): Boolean =
    missingFields(step).isEmpty()

/** A form is submittable only when nothing blocks it. */
fun ObservationFormState.canSubmit(): Boolean = blockers().isEmpty()

/**
 * Toggles one value in a multi-select field, keeping the "nothing abnormal"
 * option exclusive.
 *
 * Picking a real finding clears "normal", and picking "normal" clears everything
 * else. That is a convenience, not the rule: the exclusivity BLOCKERS above are
 * what actually prevent a contradictory form reaching the engine, and they stay
 * in force for the combinations this helper cannot reach.
 */
fun toggleMultiValue(current: Set<String>, value: String, clearsOthers: Set<String>): Set<String> = when {
    value in clearsOthers -> setOf(value)
    current.contains(value) -> current - value
    else -> (current - clearsOthers) + value
}
