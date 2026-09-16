package sg.mesha.goatos.viewmodel

import sg.mesha.goatos.core.network.dto.ShiftingSopCardDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto

/**
 * SHIFTING SOP (maintainer decision 2026-09-16, docs/decisions/shifting-sop.md): the SEEDED shifting
 * cards, used ONLY when a movement cached before this build carries no `sop` (the server always
 * serves the pinned cards). A byte-for-byte mirror of
 * backend/internal/counts/domain/sopseed/shifting.json -- `make shifting-sop-guard` fails the build
 * when a key, title, kind or compulsory flag here drifts from that file. The slot keys are the proof
 * register field keys this app has always stamped, so a clip recorded before this build fills its
 * seeded slot.
 */
internal object ShiftingSopSeed {
    const val SLOT_SHIFTING_VIDEO = "shifting_shifting_video"
    const val SLOT_PACKING_VIDEO = "shifting_packing_video"
    const val SLOT_FEEDING_VIDEO = "shifting_feeding_video"

    /** The legacy draft steps an older build wrote, mapped onto the seeded slot keys. */
    val LEGACY_STEPS = mapOf("shifting" to SLOT_SHIFTING_VIDEO, "packing" to SLOT_PACKING_VIDEO, "feeding" to SLOT_FEEDING_VIDEO)

    fun completion(): ShiftingSopCardDto = ShiftingSopCardDto(
        version = 0,
        stage = "completion",
        instruction = "",
        proofs = listOf(
            WeighingRemovalProofSlotDto(
                key = SLOT_SHIFTING_VIDEO,
                title = "Shifting video",
                hint = "Live camera only. This evidence is reviewed after the task; it does not control the herd move.",
                kind = "video",
                required = true,
            ),
        ),
    )

    fun highPriority(): ShiftingSopCardDto = ShiftingSopCardDto(
        version = 0,
        stage = "high_priority",
        instruction = "This packing proof belongs only to this Shifting task.",
        proofs = listOf(
            WeighingRemovalProofSlotDto(
                key = SLOT_PACKING_VIDEO,
                title = "Feed packing video",
                hint = "Live camera only. The configured ration being weighed out and packed for the moved animals.",
                kind = "video",
                required = true,
            ),
            WeighingRemovalProofSlotDto(
                key = SLOT_FEEDING_VIDEO,
                title = "Feed given to animal video",
                hint = "Live camera only. The configured feed being given to the moved animal(s) in the destination pen.",
                kind = "video",
                required = true,
            ),
        ),
    )

    /** An empty raise card: the seed asks nothing at raise. */
    fun raise(): ShiftingSopCardDto = ShiftingSopCardDto(version = 0, stage = "raise")
}
