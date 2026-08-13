package sg.mesha.goatos.core.data.forms

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.intOrNull
import sg.mesha.goatos.core.data.capture.MAX_PROOFS_PER_GOAT
import sg.mesha.goatos.core.data.capture.ProofSubject
import sg.mesha.goatos.core.network.dto.SopVersionDto

/**
 * Typed, render-ready view of a SOP `proof_policy` (R50-027). The backend owns the schema
 * (golden frontend rule); this is a STRUCTURAL walk of the free-form JSON, same pattern as
 * [FormSpec] for `form_dsl`. A row-level policy may be a bare token array (e.g. `["video"]`) or
 * absent entirely — both parse to [ProofPolicy.Default] rather than throwing, so an
 * older/partial backend payload degrades to the same safe defaults the client hardcoded before
 * this policy existed.
 *
 * Real backend shape (baseline 000001):
 * `{"types":["video"],"required":true,"subject_scope":"goat","expected_subjects":["goat"],
 *   "minimum_count":1,"minimum_count_per_subject":1,"maximum_count_per_subject":5,
 *   "capture_source":"in_app_camera"}`
 */
data class ProofPolicy(
    val types: List<String> = listOf("video"),
    val required: Boolean = true,
    val proofMode: String = "per_goat_video",
    val subjectScope: String = "goat",
    val expectedSubjects: List<String> = listOf("goat"),
    val minimumCount: Int = 0,
    val maximumCount: Int = MAX_PROOFS_PER_GOAT,
    val minimumCountPerSubject: Int = 0,
    /** Falls back to the historical hardcoded cap ([MAX_PROOFS_PER_GOAT]) when the backend has
     *  not published this field yet. */
    val maximumCountPerSubject: Int = MAX_PROOFS_PER_GOAT,
    /**
     * Cap ONE capture slot, instead of pooling every slot under the subject cap. Null keeps the
     * historical per-subject behaviour.
     *
     * Set this when a screen's slots are DISTINCT required steps rather than repeat takes of one
     * thing. Feed distribution is the case it was added for: its weight photo, feed video and water
     * video all carry the shed as their subject, so the per-subject cap gave three slots ONE shared
     * budget of five. Re-capturing spends that budget — and until 2026-08-13 a re-capture after
     * leaving the screen leaked its old row instead of replacing it, so pens reached five and every
     * later capture was refused, writing no row at all. The operator saw a proof that simply never
     * came back.
     *
     * A per-slot cap of 1 also makes the leak unrepresentable: a slot cannot hold a second row, so a
     * discard that does not happen blocks the capture loudly instead of quietly accumulating.
     */
    val maximumCountPerField: Int? = null,
    /** Falls back to the historical hardcoded value the client always sent before this policy
     *  existed. */
    val captureSource: String = "in_app_camera",
    val allowedCaptureSources: List<String> = listOf("in_app_camera"),
) {
    /** The subject a capture defaults to when the caller does not pick one explicitly (e.g. a
     *  per-goat "Capture proof" button) — [subjectScope] when it names a known subject,
     *  otherwise the first of [expectedSubjects], otherwise [ProofSubject.GOAT]. */
    val defaultSubject: ProofSubject
        get() = ProofSubject.entries.firstOrNull { it.wireValue == subjectScope }
            ?: expectedSubjects.firstNotNullOfOrNull { raw -> ProofSubject.entries.firstOrNull { it.wireValue == raw } }
            ?: ProofSubject.GOAT

    companion object {
        val Default = ProofPolicy()
    }

    val isPerGoatVideo: Boolean
        get() = proofMode == "per_goat_video" || subjectScope == "goat"

    val isShedLevelVideo: Boolean
        get() = proofMode == "shed_level_video" || subjectScope == "shed"

    val allowsGalleryPicker: Boolean
        get() = "gallery_picker" in allowedCaptureSources
}

/** Parses this SOP version's `proof_policy` into a [ProofPolicy]; malformed/absent policy falls
 *  back to [ProofPolicy.Default] field-by-field rather than an all-or-nothing default. */
fun SopVersionDto.toProofPolicy(): ProofPolicy = proofPolicy.toProofPolicy()

/** Parses a raw `proof_policy` map. Handles the bare-token-array row-level shape gracefully: if
 *  the map has no recognizable keyed fields, any array present is read as [ProofPolicy.types]. */
fun Map<String, JsonElement>.toProofPolicy(): ProofPolicy {
    if (isEmpty()) return ProofPolicy.Default
    val typesArray = (this["types"] as? JsonArray)
    val types = typesArray?.mapNotNull { it.asStringOrNull() } ?: ProofPolicy.Default.types
    val expectedSubjects = (this["expected_subjects"] as? JsonArray)
        ?.mapNotNull { it.asStringOrNull() }
        ?.takeIf { it.isNotEmpty() }
        ?: ProofPolicy.Default.expectedSubjects
    val allowedSources = (this["allowed_capture_sources"] as? JsonArray)
        ?.mapNotNull { it.asStringOrNull() }
        ?.takeIf { it.isNotEmpty() }
        ?: listOf(this["capture_source"].asStringOrEmpty().ifBlank { ProofPolicy.Default.captureSource })
    return ProofPolicy(
        types = types,
        required = this["required"]?.asBool() ?: ProofPolicy.Default.required,
        proofMode = this["proof_mode"].asStringOrEmpty().ifBlank {
            if (this["subject_scope"].asStringOrEmpty() == "shed") "shed_level_video" else ProofPolicy.Default.proofMode
        },
        subjectScope = this["subject_scope"].asStringOrEmpty().ifBlank { ProofPolicy.Default.subjectScope },
        expectedSubjects = expectedSubjects,
        minimumCount = this["minimum_count"].asIntOrDefault(ProofPolicy.Default.minimumCount),
        maximumCount = this["maximum_count"].asIntOrDefault(ProofPolicy.Default.maximumCount),
        minimumCountPerSubject = this["minimum_count_per_subject"].asIntOrDefault(ProofPolicy.Default.minimumCountPerSubject),
        maximumCountPerSubject = this["maximum_count_per_subject"].asIntOrDefault(ProofPolicy.Default.maximumCountPerSubject),
        captureSource = this["capture_source"].asStringOrEmpty().ifBlank { ProofPolicy.Default.captureSource },
        allowedCaptureSources = allowedSources,
    )
}

private fun JsonElement?.asStringOrEmpty(): String =
    (this as? JsonPrimitive)?.takeIf { it.isString }?.content ?: ""

private fun JsonElement?.asStringOrNull(): String? =
    (this as? JsonPrimitive)?.takeIf { it.isString }?.content

private fun JsonElement?.asBool(): Boolean =
    (this as? JsonPrimitive)?.booleanOrNull ?: false

private fun JsonElement?.asIntOrDefault(default: Int): Int =
    (this as? JsonPrimitive)?.intOrNull ?: default
