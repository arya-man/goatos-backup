package sg.mesha.goatos.core.data.weighing

import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.WeighingSopConditionDto
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto

/**
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the phone's per-animal
 * capture rules. The seeded shape keeps today's request and idempotency key byte for byte; an
 * authored shape is queued only once every compulsory slot and required answer is in.
 */
class WeighingIndividualSlotCaptureTest {
    private val authored = WeighingSopRules.Seeded.copy(
        individualProofs = listOf(
            WeighingRemovalProofSlot("animal_video", "Weighing video", "", "video", true),
            WeighingRemovalProofSlot("scale_photo", "Scale display", "", "photo", true),
            WeighingRemovalProofSlot("ear_tag", "Ear tag", "", "either", false),
        ),
        individualQuestions = listOf(
            WeighingSopQuestionDto(id = "limp", kind = "choice", title = "Limping?", required = true, options = listOf(WeighingSopOptionDto("yes", "Yes"), WeighingSopOptionDto("no", "No"))),
            WeighingSopQuestionDto(id = "which_leg", kind = "text", title = "Which leg?", required = true, onlyIf = WeighingSopConditionDto("limp", "yes")),
        ),
    )

    @Test
    fun `the seeded per-animal shape is the legacy request and key`() {
        val seeded = WeighingSopRules.Seeded
        assertTrue(seeded.individualCaptureIsSeededShape())
        assertNull(seeded.individualRequestProofs("srv-1", emptyMap()))
        assertEquals("", seeded.individualSlotKeySuffix(emptyMap(), null))
        assertEquals(emptyList<WeighingRemovalProofSlot>(), seeded.missingIndividualCaptures("srv-1", emptyMap()))
    }

    @Test
    fun `an authored animal waits for every compulsory slot and required answer`() {
        assertFalse(authored.individualCaptureIsSeededShape())
        assertEquals(listOf("scale_photo"), authored.missingIndividualCaptures("srv-1", emptyMap()).map { it.key })
        assertEquals(listOf("animal_video", "scale_photo"), authored.missingIndividualCaptures(null, emptyMap()).map { it.key })
        assertEquals(emptyList<String>(), authored.missingIndividualCaptures("srv-1", mapOf("scale_photo" to "srv-2")).map { it.key })
        // Required answers follow the only_if ancestry: "Which leg?" is owed only after "yes".
        assertEquals(listOf("limp"), authored.missingIndividualAnswers(null).map { it.id })
        val no = buildJsonObject { put("limp", JsonPrimitive("no")) }
        assertEquals(emptyList<String>(), authored.missingIndividualAnswers(no).map { it.id })
        val yes = buildJsonObject { put("limp", JsonPrimitive("yes")) }
        assertEquals(listOf("which_leg"), authored.missingIndividualAnswers(yes).map { it.id })
    }

    @Test
    fun `an authored request carries every slot primary first and a key digest of the evidence`() {
        val proofs = authored.individualRequestProofs("srv-1", mapOf("scale_photo" to "srv-2", "stale_slot" to "srv-9"))
        assertEquals(linkedMapOf("animal_video" to "srv-1", "scale_photo" to "srv-2"), proofs)
        assertEquals(listOf("animal_video", "scale_photo"), proofs!!.keys.toList())
        val answers = buildJsonObject { put("limp", JsonPrimitive("no")) }
        val a = authored.individualSlotKeySuffix(mapOf("scale_photo" to "srv-2"), answers)
        val b = authored.individualSlotKeySuffix(mapOf("scale_photo" to "srv-3"), answers)
        val c = authored.individualSlotKeySuffix(mapOf("scale_photo" to "srv-2"), buildJsonObject { put("limp", JsonPrimitive("yes")) })
        assertTrue(a.startsWith(":slots:"))
        assertTrue("a changed secondary capture is a new request", a != b)
        assertTrue("a changed answer is a new request", a != c)
        assertEquals("same evidence, same key", a, authored.individualSlotKeySuffix(mapOf("scale_photo" to "srv-2"), answers))
        // Only applicable answers are sent.
        val sent = authored.applicableIndividualAnswers(buildJsonObject { put("limp", JsonPrimitive("no")); put("which_leg", JsonPrimitive("left")) })
        assertEquals(setOf("limp"), sent.keys)
    }

    @Test
    fun `slot JSON round-trips and tolerates a blank or corrupt column`() {
        val json = encodeIndividualSlotProofs(mapOf("scale_photo" to "srv-2"))
        assertEquals(mapOf("scale_photo" to "srv-2"), decodeIndividualSlotProofs(json))
        assertEquals(emptyMap<String, String>(), decodeIndividualSlotProofs("{}"))
        assertEquals(emptyMap<String, String>(), decodeIndividualSlotProofs("not json"))
    }
}
