package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.PcCareSlotDto
import sg.mesha.goatos.core.ui.sop.SopCardUi
import sg.mesha.goatos.core.network.dto.PcCareTaskDto
import sg.mesha.goatos.core.network.dto.PcCareSopDto
import sg.mesha.goatos.core.network.dto.PcCareSopCategoryDto
import sg.mesha.goatos.core.network.dto.WeighingSopQuestionDto
import sg.mesha.goatos.core.network.dto.WeighingSopOptionDto

/**
 * A task-proof capture is whatever the CARD says it is (PC CARE SOP, 2026-09-22). The removal face
 * used to be video-shaped by slot KEY, so a capture authored as a photo — or a third one the card
 * added — was rendered, and recorded, as a video.
 */
class PcCareTaskProofSlotKindTest {
    private fun task(category: String, sop: PcCareSopDto) = PcCareTaskDto(
        taskId = "t", category = category, parkId = "p", parkLabel = "Park", shedId = "s",
        shedLabel = "Shed", plannedBusinessDate = "2026-09-22", dueBusinessDate = "2026-09-22",
        workState = "scheduled", status = "open", rowVersion = 1,
        sop = sop,
    )

    private fun chip(slot: PcCareSlotDto) =
        pcCareBuildTaskProofSlot(slot = slot, proofs = emptyList(), taskProofs = emptyList(), capturingSlotKey = null)

    @Test
    fun theChipCarriesTheKindTheCardAuthored() {
        assertEquals("photo", chip(PcCareSlotDto(fieldKey = "trough_photo", label = "Empty trough photo", kind = "photo")).kind)
        assertEquals("video", chip(PcCareSlotDto(fieldKey = "feed_video", label = "Feed removal video", kind = "video")).kind)
        assertEquals("either", chip(PcCareSlotDto(fieldKey = "gate_shot", label = "Gate", kind = "either")).kind)
    }

    @Test
    fun aServerThatSendsNoKindStillMeansVideo() {
        // Every seeded slot is a live-camera video, so blank must not become "no capture at all".
        assertEquals("video", chip(PcCareSlotDto(fieldKey = "water_video", label = "Water removal video")).kind)
    }

    @Test
    fun theChipCarriesWhetherTheCardNeedsIt() {
        assertEquals(false, chip(PcCareSlotDto(fieldKey = "trough_photo", label = "Optional", required = false)).required)
        assertEquals(true, chip(PcCareSlotDto(fieldKey = "feed_video", label = "Compulsory", required = true)).required)
    }

    /**
     * The card's QUESTIONS must reach the phone's state, or a card that asks one cannot be
     * submitted at all: the operator has nowhere to answer, the submit carries `{}`, and the
     * server refuses it (`pc_care_answer_invalid`). PR 349 review finding 1 — the helpers existed
     * and nothing fed them.
     */
    @Test
    fun aCardThatAsksAQuestionReachesThePhone() {
        val detail = task(
            category = "deworming",
            sop = PcCareSopDto(
                version = 3,
                categories = mapOf(
                    "deworming" to PcCareSopCategoryDto(
                        questions = listOf(
                            WeighingSopQuestionDto(
                                id = "dose_taken", kind = "choice", title = "Did the animal take the full dose?",
                                required = true,
                                options = listOf(
                                    WeighingSopOptionDto(value = "yes", label = "Yes"),
                                    WeighingSopOptionDto(value = "no", label = "No"),
                                ),
                            ),
                        ),
                    ),
                ),
            ),
        )
        val questions = pcCareTaskQuestions(detail)
        assertEquals(1, questions.size)
        assertEquals("Did the animal take the full dose?", questions[0].title)

        // Unanswered, the phone must refuse the submit with the SERVER's words rather than
        // sending an empty answer body into a rejection.
        val unanswered = SopCardUi(questions = questions, answers = emptyMap())
        assertEquals("Answer: Did the animal take the full dose?", unanswered.answerProblem?.message)

        // Answered, nothing stands in the way.
        val answered = SopCardUi(questions = questions, answers = mapOf("dose_taken" to "no"))
        assertEquals(null, answered.answerProblem)
    }

    /** A card that asks nothing must never gate the submit — that is the seeded card, every farm. */
    @Test
    fun aCardThatAsksNothingNeverGatesTheSubmit() {
        val detail = task(category = "deworming", sop = PcCareSopDto(version = 1))
        assertEquals(0, pcCareTaskQuestions(detail).size)
        assertEquals(null, SopCardUi(questions = pcCareTaskQuestions(detail), answers = emptyMap()).answerProblem)
    }
}
