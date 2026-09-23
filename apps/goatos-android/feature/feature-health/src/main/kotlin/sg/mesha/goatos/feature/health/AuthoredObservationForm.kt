package sg.mesha.goatos.feature.health

import androidx.compose.runtime.Immutable

/**
 * THE OBSERVATION FORM AS THE SERVER PUBLISHED IT.
 *
 * Maintainer, 2026-09-23: nothing on this form should be hard-coded, and a question added on
 * Health Config for a type must reach the phone on a refresh.
 *
 * What this replaces is a form that existed three times -- a Go struct, a field-per-question DTO,
 * and a thousand lines of Compose walked in `enum ObservationStep { VITALS, HEAD, BODY, FINAL }`.
 * Moving one question to another page was a release. These types hold what the server sent and
 * nothing else: the phone knows how to DRAW a question, never which questions exist.
 *
 * The answers are a MAP keyed by question id, not named fields, for the same reason. A named field
 * is a question compiled into the app; a map is a form the farm authors.
 */

@Immutable
data class AuthoredOption(val value: String, val label: String, val conflictsWith: List<String> = emptyList())

@Immutable
data class AuthoredQuestion(
    val id: String,
    val kind: String,
    val title: String,
    val hint: String = "",
    val options: List<AuthoredOption> = emptyList(),
    val unit: String = "",
    val min: Double? = null,
    val max: Double? = null,
    val onlyIfSex: String = "",
    val onlyIfStage: List<String> = emptyList(),
    val onlyIfQuestion: String = "",
    val onlyIfIn: List<String> = emptyList(),
) {
    /** A measurement takes a number; everything else is a pick. */
    val isNumber: Boolean get() = kind.equals("number", ignoreCase = true)

    /** Several answers may be ticked at once. */
    val isMulti: Boolean get() = kind.equals("multi", ignoreCase = true)
}

@Immutable
data class AuthoredPage(
    val id: String,
    val title: String,
    val hint: String = "",
    val questions: List<AuthoredQuestion> = emptyList(),
)

@Immutable
data class AuthoredForm(
    val goatId: String = "",
    val displayId: String = "",
    val tag: String = "",
    val typeKey: String = "",
    val typeLabel: String = "",
    val registerVersion: String = "",
    /**
     * The animal's OWN sex and stage, normalised by the server exactly as its engine reads them.
     *
     * They arrive with the form rather than being read off a search result, because the register
     * speaks `F`/`M` while the herd register stores `female`/`male`. Matching the wrong vocabulary
     * does not fail loudly -- it silently hides every sex-gated question, so the operator walks a
     * form with the udder questions missing and the submit is refused after the animal is back in
     * its pen. That shipped, and this field is why it cannot again.
     */
    val sex: String = "",
    val stage: String = "",
    val pages: List<AuthoredPage> = emptyList(),
) {
    val isEmpty: Boolean get() = pages.isEmpty()

    private val byId: Map<String, AuthoredQuestion> =
        pages.flatMap { it.questions }.associateBy { it.id }

    /**
     * The questions this page asks RIGHT NOW.
     *
     * Conditions are resolved over the WHOLE form, never within one page, because a follow-up is
     * routinely authored onto a later page than the question that triggers it -- the Drop test on
     * the Kid page turns on the Activity answer given back on Vitals. Resolving per page hid it,
     * the operator never saw it, and the server refused the submit for a question the phone had
     * decided not to ask.
     */
    fun applicableQuestions(page: AuthoredPage, answers: AuthoredAnswers): List<AuthoredQuestion> =
        page.questions.filter { applies(it, answers, HashSet()) }

    /**
     * What is still unanswered on this page.
     *
     * EVERY applicable question is compulsory. A blank cannot tell "nobody looked" from "normal",
     * and the unexplained-findings channel depends on that difference -- which is why a question
     * hidden by sex records nothing rather than being left empty: it was never asked.
     */
    fun missing(page: AuthoredPage, answers: AuthoredAnswers): List<String> =
        applicableQuestions(page, answers).filterNot { answers.answered(it.id) }.map { it.title }

    /**
     * A question applies when the animal matches its sex/stage gate AND its parent both applies
     * itself and holds one of the listed answers.
     *
     * `seen` breaks a cycle an authored document could contain; a question caught in one applies,
     * because hiding it would take a question away from the operator that the server still owes.
     */
    private fun applies(q: AuthoredQuestion, answers: AuthoredAnswers, seen: MutableSet<String>): Boolean {
        if (q.onlyIfSex.isNotBlank() && !q.onlyIfSex.equals(sex, ignoreCase = true)) return false
        if (q.onlyIfStage.isNotEmpty() && q.onlyIfStage.none { it.equals(stage, ignoreCase = true) }) return false
        if (q.onlyIfQuestion.isBlank()) return true
        if (!seen.add(q.id)) return true
        val parent = byId[q.onlyIfQuestion] ?: return false
        if (!applies(parent, answers, seen)) return false
        return answers.of(parent.id).any { it in q.onlyIfIn }
    }
}

/**
 * One animal's answers, keyed by question id.
 *
 * A pick-one holds a set of one so a single shape serves both; the wire sends a list either way,
 * which the backend's decoder already accepts.
 */
@Immutable
data class AuthoredAnswers(val values: Map<String, Set<String>> = emptyMap()) {
    fun of(questionId: String): Set<String> = values[questionId].orEmpty()

    fun answered(questionId: String): Boolean = of(questionId).isNotEmpty()

    /** A pick-one replaces; the form never leaves two answers behind one tap. */
    fun pick(questionId: String, value: String): AuthoredAnswers =
        AuthoredAnswers(values + (questionId to setOf(value)))

    /**
     * A pick-any toggles, and CLEARS the answers this one conflicts with.
     *
     * "None" beside a list of nervous signs conflicts with every one of them: the server refuses
     * the pair, so offering it here would be a tap the operator has to undo after a rejected
     * submit -- with the animal already put back.
     */
    fun toggle(question: AuthoredQuestion, value: String): AuthoredAnswers {
        val option = question.options.firstOrNull { it.value == value }
        val current = of(question.id)
        val next = if (value in current) current - value else current + value
        val cleared = option?.conflictsWith.orEmpty().toSet()
        val siblingsClearingThis = question.options
            .filter { value in it.conflictsWith }
            .map { it.value }
            .toSet()
        return AuthoredAnswers(
            values + (question.id to if (value in current) next else next - cleared - siblingsClearingThis + value),
        )
    }

    fun setNumber(questionId: String, raw: String): AuthoredAnswers =
        if (raw.isBlank()) AuthoredAnswers(values - questionId)
        else AuthoredAnswers(values + (questionId to setOf(raw.trim())))
}
