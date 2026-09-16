package sg.mesha.goatos.core.ui.sop

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The phone judges a card's answers the way the server's `sop/authored` ValidateAnswers does, so
 * a report the server would refuse (422 capture_answer_invalid) never leaves the phone -- offline
 * that refusal would only arrive at sync, long after the operator walked away.
 */
class SopCardAnswerCheckTest {

    private val cause = SopQuestionUi(
        id = "cause",
        kind = "choice",
        title = "Suspected cause",
        required = true,
        options = listOf("bloat" to "Bloat", "other" to "Other"),
        allowOther = true,
    )

    private fun card(vararg questions: SopQuestionUi, answers: Map<String, String> = emptyMap()) =
        SopCardUi(questions = questions.toList(), answers = answers)

    @Test
    fun `a card without questions has nothing to check`() {
        val c = card()
        assertNull(c.answerProblem)
        assertTrue(c.requiredAnswersGiven)
    }

    @Test
    fun `a required pick-one left blank names the question`() {
        val problem = card(cause).answerProblem
        assertEquals("cause", problem?.questionId)
        assertEquals(SopAnswerProblemKind.ANSWER, problem?.kind)
        assertEquals("Answer: Suspected cause", problem?.message)
    }

    @Test
    fun `other without its written answer is refused, with it is accepted`() {
        val blank = card(cause, answers = mapOf("cause" to "other", "cause_other" to "  "))
        assertEquals(SopAnswerProblemKind.WRITE_OTHER, blank.answerProblem?.kind)
        assertEquals("Write the other answer for: Suspected cause", blank.answerProblem?.message)
        assertFalse(blank.requiredAnswersGiven)

        val written = card(cause, answers = mapOf("cause" to "other", "cause_other" to "Snake bite"))
        assertNull(written.answerProblem)
        assertTrue(written.requiredAnswersGiven)
    }

    @Test
    fun `a pick-one outside the offered answers is refused`() {
        val c = card(cause, answers = mapOf("cause" to "retired"))
        assertEquals(SopAnswerProblemKind.PICK_OFFERED, c.answerProblem?.kind)
    }

    @Test
    fun `pick-many needs one tick when required and only offered values`() {
        val signs = SopQuestionUi(id = "signs", kind = "multi", title = "Signs", required = true, options = listOf("a" to "A", "b" to "B"))
        assertEquals(SopAnswerProblemKind.TICK_ONE, card(signs).answerProblem?.kind)
        assertEquals(SopAnswerProblemKind.PICK_ONLY_OFFERED, card(signs, answers = mapOf("signs" to "a,zzz")).answerProblem?.kind)
        assertNull(card(signs, answers = mapOf("signs" to "a,b")).answerProblem)
    }

    @Test
    fun `a number must be a number inside its range`() {
        val kids = SopQuestionUi(id = "kids", kind = "number", title = "Kids born", required = true, min = 1.0, max = 4.0)
        assertEquals(SopAnswerProblemKind.ENTER, card(kids).answerProblem?.kind)
        assertEquals(SopAnswerProblemKind.ENTER_NUMBER, card(kids, answers = mapOf("kids" to "-")).answerProblem?.kind)
        val high = card(kids, answers = mapOf("kids" to "7")).answerProblem
        assertEquals(SopAnswerProblemKind.OUT_OF_RANGE, high?.kind)
        assertEquals("Enter a value between 1 and 4 for: Kids born", high?.message)
        assertEquals(
            "Enter a value of at least 0.5 for: Weight",
            card(SopQuestionUi(id = "w", kind = "number", title = "Weight", min = 0.5), answers = mapOf("w" to "0.1")).answerProblem?.message,
        )
        assertNull(card(kids, answers = mapOf("kids" to "2")).answerProblem)
        // Optional and blank: nothing to judge.
        assertNull(card(kids.copy(required = false)).answerProblem)
    }

    @Test
    fun `required text must be written and not too long`() {
        val note = SopQuestionUi(id = "note", kind = "text", title = "What you saw", required = true)
        assertEquals(SopAnswerProblemKind.ENTER, card(note, answers = mapOf("note" to " ")).answerProblem?.kind)
        assertEquals(SopAnswerProblemKind.TOO_LONG, card(note, answers = mapOf("note" to "x".repeat(2001))).answerProblem?.kind)
        assertNull(card(note, answers = mapOf("note" to "Swollen belly")).answerProblem)
    }

    @Test
    fun `a hidden conditional question is never asked, down its whole chain`() {
        val died = SopQuestionUi(id = "where", kind = "choice", title = "Where", required = true, options = listOf("pen" to "Pen", "field" to "Field"))
        val field = SopQuestionUi(
            id = "field", kind = "choice", title = "Which field", required = true,
            options = listOf("north" to "North", "south" to "South"), onlyIfQuestion = "where", onlyIfValue = "field",
        )
        val north = SopQuestionUi(
            id = "gate", kind = "text", title = "Gate number", required = true, onlyIfQuestion = "field", onlyIfValue = "north",
        )
        // The operator first picked field → north, then changed their mind to pen: both follow-ups
        // are hidden, including the grandchild whose own parent still holds "north".
        val c = card(died, field, north, answers = mapOf("where" to "pen", "field" to "north"))
        assertFalse(c.appliesTo(north))
        assertNull(c.answerProblem)

        val shown = card(died, field, north, answers = mapOf("where" to "field", "field" to "north"))
        assertEquals("gate", shown.answerProblem?.questionId)
    }
}
