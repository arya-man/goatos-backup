package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.weighing.WeighingCountedProofSlot
import sg.mesha.goatos.core.data.weighing.WeighingRemovalProofSlot
import sg.mesha.goatos.core.data.weighing.WeighingSopRules
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.feature.scan.ProofUploadStatus
import sg.mesha.goatos.feature.weighing.WeighingProofUiRow
import sg.mesha.goatos.feature.weighing.WholePenCaptureLabel
import sg.mesha.goatos.feature.weighing.wholePenPrimaryCaptureLabel

/**
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the capture screen renders the
 * per-animal section on the animal row and the whole-pen section in the pen block, each from its
 * OWN slots and questions of the pinned rules, and never one list for both.
 */
class WeighingCaptureSopMappingTest {
    private val rules = WeighingSopRules.Seeded.copy(
        individualProofs = listOf(
            WeighingRemovalProofSlot("animal_video", "Weighing video", "", "video", true),
            WeighingRemovalProofSlot("scale_photo", "Scale display", "", "photo", true),
        ),
        individualQuestions = listOf(
            WeighingSopQuestionDto(id = "limp", kind = "choice", title = "Limping?", required = true, options = listOf(WeighingSopOptionDto("yes", "Yes"), WeighingSopOptionDto("no", "No"))),
        ),
        lumpSumSlots = listOf(
            WeighingCountedProofSlot("pen_video", "Pen video", "", "video", 1, 3),
            WeighingCountedProofSlot("gate_photo", "Gate photo", "", "photo", 1, 2),
        ),
        lumpSumQuestions = listOf(
            WeighingSopQuestionDto(id = "count_seen", kind = "number", title = "Animals seen", required = true),
            WeighingSopQuestionDto(id = "tags", kind = "multi", title = "Tags missing", options = listOf(WeighingSopOptionDto("left", "Left"), WeighingSopOptionDto("right", "Right"))),
        ),
    )

    @Test
    fun `the two sections map apart - animal extras on the row, pen extras in the pen block`() {
        val ui = rules.toCaptureSopUi(extraPenProofs = emptyMap(), primaryPenSynced = 0, penAnswers = null)
        assertEquals(listOf("scale_photo"), ui.animalExtraSlots.map { it.key })
        assertEquals(listOf("limp"), ui.animalQuestions.map { it.id })
        assertEquals(listOf("gate_photo"), ui.penExtraSlots.map { it.key })
        assertEquals(listOf("count_seen", "tags"), ui.penQuestions.map { it.id })
        assertEquals("Pen video", ui.primaryPenSlotTitle)
        assertEquals(3, ui.primaryPenSlotMax)
        assertTrue("no pen question leaks onto the animal row", ui.animalQuestions.none { it.id == "count_seen" })
        assertTrue("no animal slot leaks into the pen block", ui.penExtraSlots.none { it.key == "scale_photo" })
    }

    /**
     * DAY ONE (maintainer rule): with no SOP edited the whole-pen button must read exactly as before
     * -- the translated "Capture group video" -- not the seeded slot's English noun "Weighing video".
     */
    @Test
    fun `the seeded whole-pen slot keeps the translated group video label`() {
        val seeded = WeighingSopRules.Seeded.copy(lumpSumSlots = listOf(WeighingCountedProofSlot.seededPenVideo(1, 5)))
        val ui = seeded.toCaptureSopUi(emptyMap(), 0, null)
        assertEquals("", ui.primaryPenSlotTitle)
        assertEquals(WholePenCaptureLabel.GroupVideo, wholePenPrimaryCaptureLabel(ui, captured = 0))
    }

    @Test
    fun `an authored whole-pen title is the button label`() {
        val retitled = WeighingSopRules.Seeded.copy(lumpSumSlots = listOf(WeighingCountedProofSlot("pen_video", "Pen on scale video", "", "video", 1, 5)))
        assertEquals(WholePenCaptureLabel.Authored("Pen on scale video"), wholePenPrimaryCaptureLabel(retitled.toCaptureSopUi(emptyMap(), 0, null), captured = 0))
        val authored = WeighingSopRules.Seeded.copy(lumpSumSlots = listOf(WeighingCountedProofSlot("scale_clip", "Weighing video", "", "video", 1, 5)))
        assertEquals(WholePenCaptureLabel.Authored("Weighing video"), wholePenPrimaryCaptureLabel(authored.toCaptureSopUi(emptyMap(), 0, null), captured = 0))
    }

    @Test
    fun `the seeded shape renders today's screen with no extras`() {
        val ui = WeighingSopRules.Seeded.toCaptureSopUi(emptyMap(), 0, null)
        assertTrue(ui.animalExtraSlots.isEmpty() && ui.animalQuestions.isEmpty())
        assertTrue(ui.penExtraSlots.isEmpty() && ui.penQuestions.isEmpty())
        assertEquals(5, ui.primaryPenSlotMax)
    }

    @Test
    fun `an animal owes its secondary slot and required answer by title`() {
        assertEquals(listOf("Scale display", "Limping?"), rules.animalCapturesMissing(primarySynced = true, extraSynced = emptySet(), answers = null))
        val answered = null.withAnswer(rules.individualQuestions.first(), "no")
        assertEquals(emptyList<String>(), rules.animalCapturesMissing(true, setOf("scale_photo"), answered))
        assertEquals(listOf("Weighing video"), rules.animalCapturesMissing(false, setOf("scale_photo"), answered))
    }

    @Test
    fun `the pen owes each slot's minimum and its required answers`() {
        val gate = WeighingProofUiRow(id = "g1", label = "synced", status = ProofUploadStatus.SYNCED)
        val missing = rules.toCaptureSopUi(extraPenProofs = emptyMap(), primaryPenSynced = 1, penAnswers = null).penCapturesMissing
        assertEquals(listOf("Gate photo", "Animals seen"), missing)
        val answers = null.withAnswer(rules.lumpSumQuestions.first(), "12")
        assertEquals(JsonPrimitive(12.0), answers["count_seen"])
        val done = rules.toCaptureSopUi(mapOf("gate_photo" to listOf(gate)), primaryPenSynced = 1, penAnswers = answers)
        assertTrue(done.penCapturesMissing.isEmpty())
        assertEquals("12", done.penAnswers["count_seen"]?.value)
    }

    @Test
    fun `answers edit into the backend wire shape`() {
        val multi = rules.lumpSumQuestions[1]
        val a = null.withToggled(multi, "left").withToggled(multi, "right").withToggled(multi, "left")
        assertEquals(JsonArray(listOf(JsonPrimitive("right"))), a["tags"])
        val cleared = a.withAnswer(rules.lumpSumQuestions[0], "  ")
        assertFalse(cleared.containsKey("count_seen"))
        val other = null.withOther("why", "broken trough")
        assertEquals("broken trough", (other["why_other"] as JsonPrimitive).content)
    }

    @Test
    fun `whole pen capture caps are per slot with the pen total across slots`() {
        val caps = rules.penCaptureCaps(slotMax = 2)
        assertEquals(2, caps.perSlot)
        assertEquals(5, caps.perPen) // pen video 1..3 + gate photo 1..2
        val videoCaps = rules.penCaptureCaps(slotMax = 3)
        assertEquals(3, videoCaps.perSlot)
        assertEquals(5, videoCaps.perPen)
        assertEquals(5, WeighingSopRules.Seeded.penCaptureCaps(slotMax = 5).perPen)
    }
}
