package sg.mesha.goatos.core.data.weighing

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WeighingCountedProofSlotDto
import sg.mesha.goatos.core.network.dto.WeighingRemovalProofSlotDto
import sg.mesha.goatos.core.network.dto.WeighingSopCaptureDto
import sg.mesha.goatos.core.network.dto.WeighingSopIndividualCaptureDto
import sg.mesha.goatos.core.network.dto.WeighingSopLumpSumCaptureDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.core.network.dto.WeighingSopRulesDto

/**
 * THE WEIGH CAPTURES ARE AUTHORED (2026-09-16): the whole-pen capture rules, and the two sections
 * staying separate when the served rules are read.
 */
class WeighingLumpSumSlotCaptureTest {
    private val authored = WeighingSopRules.Seeded.copy(
        lumpSumSlots = listOf(
            WeighingCountedProofSlot("pen_video", "Weighing video", "", "video", 1, 3),
            WeighingCountedProofSlot("scale_photo", "Scale display photo", "", "photo", 1, 1),
            WeighingCountedProofSlot("gate", "Gate", "", "either", 0, 2),
        ),
    )

    @Test
    fun `whole-pen slots are gated per slot on min and max`() {
        assertEquals(listOf("pen_video", "scale_photo"), authored.lumpSumSlotProblems(emptyMap()).map { it.key })
        assertEquals(listOf("scale_photo"), authored.lumpSumSlotProblems(mapOf("pen_video" to listOf("a"))).map { it.key })
        assertEquals(listOf("pen_video"), authored.lumpSumSlotProblems(mapOf("pen_video" to listOf("a", "b", "c", "d"), "scale_photo" to listOf("p"))).map { it.key })
        assertEquals(emptyList<String>(), authored.lumpSumSlotProblems(mapOf("pen_video" to listOf("a", "b"), "scale_photo" to listOf("p"))).map { it.key })
        assertEquals(10, WeighingSopRules.MAX_LUMP_SUM_PROOFS_TOTAL)
        assertEquals(6, authored.lumpSumProofsTotalMax)
    }

    @Test
    fun `whole-pen request lists captures in slot order and the seeded shape stays legacy`() {
        val slots = mapOf("scale_photo" to listOf("p"), "pen_video" to listOf("a", "b"))
        assertEquals(listOf("a", "b", "p"), authored.lumpSumFlatProofs(slots))
        assertEquals(linkedMapOf("pen_video" to listOf("a", "b"), "scale_photo" to listOf("p")), authored.lumpSumRequestProofs(slots))
        val seeded = WeighingSopRules.Seeded
        assertTrue(seeded.lumpSumCaptureIsSeededShape())
        assertNull(seeded.lumpSumRequestProofs(mapOf("pen_video" to listOf("a"))))
        assertEquals(mapOf("x" to listOf("1", "2")), decodeLumpSumSlotProofs(encodeLumpSumSlotProofs(mapOf("x" to listOf("1", "2")))))
    }

    @Test
    fun `served rules keep per-animal and whole-pen slots and questions apart`() {
        val dto = WeighingSopRulesDto(
            version = 4,
            capture = WeighingSopCaptureDto(
                individual = WeighingSopIndividualCaptureDto(
                    proofs = listOf(WeighingRemovalProofSlotDto("animal_video", "Weighing video", "", "video", true), WeighingRemovalProofSlotDto("scale_photo", "Scale display", "", "photo", true)),
                    questions = listOf(WeighingSopQuestionDto(id = "limp", kind = "text", title = "Limping?")),
                ),
                lumpSum = WeighingSopLumpSumCaptureDto(
                    videoMin = 1, videoMax = 3,
                    proofs = listOf(WeighingCountedProofSlotDto("pen_video", "Weighing video", "", "video", 1, 3)),
                    questions = listOf(WeighingSopQuestionDto(id = "all_on", kind = "text", title = "Every animal on?")),
                ),
            ),
        )
        val rules = dto.toRules()
        assertEquals(listOf("animal_video", "scale_photo"), rules.individualProofs.map { it.key })
        assertEquals(listOf("limp"), rules.individualQuestions.map { it.id })
        assertEquals(listOf("pen_video"), rules.lumpSumProofs.map { it.key })
        assertEquals(listOf("all_on"), rules.lumpSumQuestions.map { it.id })
        // An older server sends no slots: the seeded ones, carrying the served window.
        val legacy = WeighingSopRulesDto(capture = WeighingSopCaptureDto(lumpSum = WeighingSopLumpSumCaptureDto(videoMin = 2, videoMax = 4))).toRules()
        assertEquals(listOf("animal_video"), legacy.individualProofs.map { it.key })
        assertEquals(2, legacy.lumpSumProofs.single().min)
        assertEquals(4, legacy.lumpSumProofs.single().max)
    }
}
