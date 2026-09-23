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

    private fun form(sex: String = "F", stage: String = "K2", vararg pages: AuthoredPage) =
        AuthoredForm(sex = sex, stage = stage, pages = pages.toList())

    private fun page(vararg questions: AuthoredQuestion) =
        AuthoredPage(id = "p", title = "Page", questions = questions.toList())

    private fun choice(id: String, vararg options: AuthoredOption) =
        AuthoredQuestion(id = id, kind = "choice", title = id, options = options.toList())

    @Test
    fun `a question for one sex is never asked of the other`() {
        val p = page(
            choice("temp"),
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "F"),
        )

        val female = form("F", "K2", p)
        val male = form("M", "K2", p)
        assertEquals(listOf("temp", "udder"), female.applicableQuestions(p, AuthoredAnswers()).map { it.id })
        assertEquals(listOf("temp"), male.applicableQuestions(p, AuthoredAnswers()).map { it.id })
    }

    @Test
    fun `the register speaks F and M, and the form is asked in that vocabulary`() {
        // The herd register stores `female`; the authored document gates on `F`. The served form
        // normalises, and this pins that the phone reads the normalised value -- comparing the
        // two vocabularies hides every udder question without any error at all.
        val p = page(AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "F"))

        assertEquals(listOf("udder"), form("F", "K2", p).applicableQuestions(p, AuthoredAnswers()).map { it.id })
        assertEquals(emptyList<String>(), form("female", "K2", p).applicableQuestions(p, AuthoredAnswers()).map { it.id })
    }

    @Test
    fun `a follow-up authored onto a later page still appears`() {
        // The Drop test sits on the Kid page and turns on the Activity answer given back on
        // Vitals. Resolving conditions within one page hid it, the operator never saw it, and the
        // server refused the whole submit for a question the phone had decided not to ask.
        val vitals = AuthoredPage(
            id = "vitals",
            title = "Vitals",
            questions = listOf(choice("activity", AuthoredOption("standing", "Standing"), AuthoredOption("down", "Down"))),
        )
        val kid = AuthoredPage(
            id = "kid",
            title = "Kid",
            questions = listOf(
                AuthoredQuestion(
                    id = "landing",
                    kind = "choice",
                    title = "Drop test",
                    onlyIfQuestion = "activity",
                    onlyIfIn = listOf("standing"),
                ),
            ),
        )
        val f = form("F", "K2", vitals, kid)

        assertEquals(emptyList<String>(), f.applicableQuestions(kid, AuthoredAnswers()).map { it.id })
        val standing = AuthoredAnswers().pick("activity", "standing")
        assertEquals(listOf("landing"), f.applicableQuestions(kid, standing).map { it.id })
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

        val f = form("F", "K2", p)
        assertEquals(listOf("lameness"), f.applicableQuestions(p, AuthoredAnswers()).map { it.id })

        val lame = AuthoredAnswers().pick("lameness", "yes")
        assertEquals(listOf("lameness", "which_leg"), f.applicableQuestions(p, lame).map { it.id })

        // Changing one's mind takes the follow-up away again rather than carrying a stale answer.
        val fine = lame.pick("lameness", "no")
        assertEquals(listOf("lameness"), f.applicableQuestions(p, fine).map { it.id })
    }

    @Test
    fun `a follow-up whose parent is hidden is hidden too`() {
        val p = page(
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "F"),
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

        assertEquals(emptyList<String>(), form("M", "K2", p).applicableQuestions(p, stale).map { it.id })
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
            AuthoredQuestion(id = "udder", kind = "choice", title = "Udder", onlyIfSex = "F"),
        )

        assertEquals(listOf("temp", "Udder"), form("F", "K2", p).missing(p, AuthoredAnswers()))
        // The male is not owed an udder check -- it was never asked, which is not the same as blank.
        assertEquals(listOf("temp"), form("M", "K2", p).missing(p, AuthoredAnswers()))
        assertEquals(emptyList<String>(), form("M", "K2", p).missing(p, AuthoredAnswers().setNumber("temp", "102")))
    }
}
