package sg.mesha.goatos.feature.health

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The form the farm authors, as the phone reads it.
 *
 * These pin the three decisions the renderer makes on its own -- which questions apply, what a
 * tap does to the ones it conflicts with, and what is still owed -- because everything else on
 * this screen is the server's text drawn verbatim.
 */
class AuthoredObservationFormTest {

    private fun page(vararg questions: AuthoredQuestion) =
        AuthoredPage(id = "p", title = "Page", questions = questions.toList())

    private fun choice(id: String, vararg options: AuthoredOption) =
        AuthoredQuestion(id = id, kind = "choice", title = id, options = options.toList())

    @Test
    fun `a question for one sex is never asked of the other`() {
        val p = page(
            choice("temp"),
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "female"),
        )

        assertEquals(listOf("temp", "udder"), p.applicableQuestions(AuthoredAnswers(), "Female", "K2").map { it.id })
        assertEquals(listOf("temp"), p.applicableQuestions(AuthoredAnswers(), "male", "K2").map { it.id })
    }

    @Test
    fun `a follow-up waits for its parent's answer and leaves with it`() {
        val p = page(
            choice("lameness", AuthoredOption("yes", "Yes"), AuthoredOption("no", "No")),
            AuthoredQuestion(
                id = "which_leg",
                kind = "choice",
                title = "Which leg",
                onlyIfQuestion = "lameness",
                onlyIfIn = listOf("yes"),
            ),
        )

        assertEquals(listOf("lameness"), p.applicableQuestions(AuthoredAnswers(), "female", "K2").map { it.id })

        val lame = AuthoredAnswers().pick("lameness", "yes")
        assertEquals(listOf("lameness", "which_leg"), p.applicableQuestions(lame, "female", "K2").map { it.id })

        // Changing one's mind takes the follow-up away again rather than carrying a stale answer.
        val fine = lame.pick("lameness", "no")
        assertEquals(listOf("lameness"), p.applicableQuestions(fine, "female", "K2").map { it.id })
    }

    @Test
    fun `a follow-up whose parent is hidden is hidden too`() {
        val p = page(
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "female"),
            AuthoredQuestion(
                id = "udder_side",
                kind = "choice",
                title = "Which side",
                onlyIfQuestion = "udder",
                onlyIfIn = listOf("hard"),
            ),
        )
        // An answer left over from an earlier animal must not resurrect a follow-up whose parent
        // this animal was never asked.
        val stale = AuthoredAnswers().pick("udder", "hard")

        assertEquals(emptyList<String>(), p.applicableQuestions(stale, "male", "K2").map { it.id })
    }

    @Test
    fun `ticking None clears the signs it contradicts, and a sign clears None`() {
        val q = AuthoredQuestion(
            id = "neuro",
            kind = "multi",
            title = "Nervous signs",
            options = listOf(
                AuthoredOption("none", "None", conflictsWith = listOf("seizure", "circling")),
                AuthoredOption("seizure", "Seizure"),
                AuthoredOption("circling", "Circling"),
            ),
        )

        val signs = AuthoredAnswers().toggle(q, "seizure").toggle(q, "circling")
        assertEquals(setOf("seizure", "circling"), signs.of("neuro"))

        // None wins over the signs it names.
        assertEquals(setOf("none"), signs.toggle(q, "none").of("neuro"))

        // And a sign ticked after None clears None, though None does not name it on this side.
        val none = AuthoredAnswers().toggle(q, "none")
        assertEquals(setOf("seizure"), none.toggle(q, "seizure").of("neuro"))
    }

    @Test
    fun `a pick-one replaces rather than accumulates`() {
        val a = AuthoredAnswers().pick("activity", "standing").pick("activity", "down")
        assertEquals(setOf("down"), a.of("activity"))
    }

    @Test
    fun `a blank number is no answer at all`() {
        val a = AuthoredAnswers().setNumber("temp", "103.5")
        assertEquals(setOf("103.5"), a.of("temp"))
        assertTrue(a.setNumber("temp", "  ").of("temp").isEmpty())
    }

    @Test
    fun `only the questions this animal was asked are owed`() {
        val p = page(
            choice("temp"),
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "female"),
        )

        assertEquals(listOf("temp", "Udder"), p.missing(AuthoredAnswers(), "female", "K2"))
        // The male is not owed an udder check -- it was never asked, which is not the same as blank.
        assertEquals(listOf("temp"), p.missing(AuthoredAnswers(), "male", "K2"))
        assertEquals(emptyList<String>(), p.missing(AuthoredAnswers().setNumber("temp", "102"), "male", "K2"))
    }
}
