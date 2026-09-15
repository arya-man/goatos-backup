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
) {
    /** Every task carries the evening-before removal (the 2026-09-03 rule). */
    val removalRequired: Boolean get() = removalMode == REMOVAL_REQUIRED

    /** The planner decides per task; on unless switched off. */
    val removalOptional: Boolean get() = removalMode == REMOVAL_OPTIONAL

    /** No task carries the removal; the wizard offers no removal step. */
    val removalOff: Boolean get() = removalMode == REMOVAL_OFF

    fun modeAllowed(category: String): Boolean = modes.contains(category)

    companion object {
        const val REMOVAL_REQUIRED = "required"
        const val REMOVAL_OPTIONAL = "optional"
        const val REMOVAL_OFF = "off"

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
        )
    }
}

data class WeighingRemovalProofSlot(val key: String, val title: String, val hint: String, val kind: String = "video", val required: Boolean = true)

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
)
