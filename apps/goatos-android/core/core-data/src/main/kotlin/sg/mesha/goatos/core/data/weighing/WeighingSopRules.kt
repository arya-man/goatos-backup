package sg.mesha.goatos.core.data.weighing

import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.core.network.dto.WeighingSopRulesDto

/**
 * WEIGHING SOP (maintainer decision 2026-09-15): the compiled weighing.session rule set the
 * backend serves on the planner catalog (the PUBLISHED version a new task is stamped with) and on
 * the single-task read (the task's PINNED version). The phone renders it and holds no rule of its
 * own: which ways of weighing the planner may pick, the default cap, whether the evening-before
 * feed & water removal is required / optional / off, the removal card's instruction, proof-slot
 * copy and authored questions, and the lump-sum video window.
 */
data class WeighingSopRules(
    val version: Int,
    val modes: List<String>,
    val defaultCapPerDay: Int,
    val removalMode: String,
    /** The EFFECTIVE removal evening ("HH:MM" IST) the planner catalog served; blank = unknown. */
    val removalCutoffTime: String,
    val removalInstruction: String,
    val removalProofs: List<WeighingRemovalProofSlot>,
    val removalQuestions: List<WeighingSopQuestionDto>,
    val lumpSumVideoMin: Int,
    val lumpSumVideoMax: Int,
    // THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): TWO SEPARATE capture
    // sections, never merged. PER ANIMAL: named slots (video / photo / either, compulsory or
    // not; at least one compulsory; the FIRST is the primary) + questions answered per animal.
    // WHOLE PEN: counted slots (min..max each) + questions answered once per pen. Both lists
    // come from the served rules; an older server sends none and the seeded slots apply.
    val individualProofs: List<WeighingRemovalProofSlot> = Seeded.individualProofs,
    val individualQuestions: List<WeighingSopQuestionDto> = emptyList(),
    val lumpSumProofs: List<WeighingCountedProofSlot> = listOf(WeighingCountedProofSlot.seededPenVideo(lumpSumVideoMin, lumpSumVideoMax)),
    val lumpSumQuestions: List<WeighingSopQuestionDto> = emptyList(),
) {
    /** Every task carries the evening-before removal (the 2026-09-03 rule). */
    val removalRequired: Boolean get() = removalMode == REMOVAL_REQUIRED

    /** The planner decides per task; on unless switched off. */
    val removalOptional: Boolean get() = removalMode == REMOVAL_OPTIONAL

    /** No task carries the removal; the wizard offers no removal step. */
    val removalOff: Boolean get() = removalMode == REMOVAL_OFF

    fun modeAllowed(category: String): Boolean = modes.contains(category)

    /** The per-animal slot whose capture is the PRIMARY (stored on proof_artifact_id). */
    val primaryIndividualSlot: WeighingRemovalProofSlot? get() = individualProofs.firstOrNull()

    /** Compulsory per-animal slots, in row order. */
    val compulsoryIndividualProofs: List<WeighingRemovalProofSlot> get() = individualProofs.filter { it.required }

    /** The pinned Σmax over the whole-pen slots: the "N of M" denominator. */
    val lumpSumProofsTotalMax: Int get() = lumpSumProofs.sumOf { it.max }.coerceAtLeast(1)

    companion object {
        const val REMOVAL_REQUIRED = "required"
        const val REMOVAL_OPTIONAL = "optional"
        const val REMOVAL_OFF = "off"
        const val INDIVIDUAL_PROOF_ANIMAL_VIDEO = "animal_video"
        const val LUMP_SUM_PROOF_PEN_VIDEO = "pen_video"
        const val MAX_LUMP_SUM_PROOFS_TOTAL = 10

        /** The seeded per-animal slot: one compulsory "Weighing video". */
        val SeededIndividualProofs: List<WeighingRemovalProofSlot> = listOf(
            WeighingRemovalProofSlot(
                key = INDIVIDUAL_PROOF_ANIMAL_VIDEO,
                title = "Weighing video",
                hint = "Live-camera video of this animal on the scale; it opens on the scale reading 0 kg.",
                kind = "video",
                required = true,
            ),
        )

        /**
         * The seeded rules -- the pre-SOP behaviour -- for an older server that sends none:
         * removal required on every task, both capture modes, cap 100, 1..5 lump-sum videos.
         */
        val Seeded: WeighingSopRules = WeighingSopRules(
            version = 0,
            modes = listOf("individual_animal", "per_shed_partition"),
            defaultCapPerDay = 100,
            removalMode = REMOVAL_REQUIRED,
            removalCutoffTime = "",
            removalInstruction = "",
            removalProofs = emptyList(),
            removalQuestions = emptyList(),
            lumpSumVideoMin = 1,
            lumpSumVideoMax = 5,
            individualProofs = SeededIndividualProofs,
            individualQuestions = emptyList(),
            lumpSumProofs = listOf(WeighingCountedProofSlot.seededPenVideo(1, 5)),
            lumpSumQuestions = emptyList(),
        )
    }
}

data class WeighingRemovalProofSlot(val key: String, val title: String, val hint: String, val kind: String = "video", val required: Boolean = true) {
    val acceptsVideo: Boolean get() = kind == "video" || kind == "either"
    val acceptsPhoto: Boolean get() = kind == "photo" || kind == "either"
}

/** One WHOLE-PEN capture slot carrying min..max captures (min 0 = optional). */
data class WeighingCountedProofSlot(val key: String, val title: String, val hint: String, val kind: String = "video", val min: Int = 1, val max: Int = 5) {
    val required: Boolean get() = min >= 1
    val acceptsVideo: Boolean get() = kind == "video" || kind == "either"
    val acceptsPhoto: Boolean get() = kind == "photo" || kind == "either"

    companion object {
        /** The seeded whole-pen slot carrying the document's own video window. */
        fun seededPenVideo(min: Int, max: Int): WeighingCountedProofSlot = WeighingCountedProofSlot(
            key = WeighingSopRules.LUMP_SUM_PROOF_PEN_VIDEO,
            title = "Weighing video",
            hint = "Live-camera video of the pen on the scale; it opens on the scale reading 0 kg.",
            kind = "video",
            min = min.coerceAtLeast(1),
            max = max.coerceIn(min.coerceAtLeast(1), 5),
        )
    }
}

fun WeighingSopRulesDto.toRules(): WeighingSopRules = WeighingSopRules(
    version = version,
    modes = planning.modes,
    defaultCapPerDay = planning.defaultCapPerDay.takeIf { it > 0 } ?: WeighingSopRules.Seeded.defaultCapPerDay,
    removalMode = feedWaterRemoval.mode.ifBlank { WeighingSopRules.REMOVAL_REQUIRED },
    removalCutoffTime = feedWaterRemoval.cutoffTime,
    removalInstruction = feedWaterRemoval.instruction,
    removalProofs = feedWaterRemoval.proofs.map { WeighingRemovalProofSlot(it.key, it.title, it.hint, it.kind, it.required) },
    removalQuestions = feedWaterRemoval.questions,
    lumpSumVideoMin = capture.lumpSum.videoMin.coerceAtLeast(1),
    lumpSumVideoMax = capture.lumpSum.videoMax.coerceAtLeast(1),
    individualProofs = capture.individual.proofs
        .map { WeighingRemovalProofSlot(it.key, it.title, it.hint, it.kind, it.required) }
        .ifEmpty { WeighingSopRules.SeededIndividualProofs },
    individualQuestions = capture.individual.questions,
    lumpSumProofs = capture.lumpSum.proofs
        .map { WeighingCountedProofSlot(it.key, it.title, it.hint, it.kind, it.min.coerceAtLeast(0), it.max.coerceAtLeast(1)) }
        .ifEmpty { listOf(WeighingCountedProofSlot.seededPenVideo(capture.lumpSum.videoMin, capture.lumpSum.videoMax)) },
    lumpSumQuestions = capture.lumpSum.questions,
)
