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
    val pages: List<AuthoredPage> = emptyList(),
) {
    val isEmpty: Boolean get() = pages.isEmpty()
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

/**
 * The questions this page asks RIGHT NOW.
 *
 * A conditional question applies only when its parent is itself applicable AND holds one of the
 * listed answers -- the same chain the server walks, so a follow-up whose parent was hidden by a
 * change of mind is hidden too. Sex and stage come from the ANIMAL, never from the form: they
 * decide how an answer is read, and a manager who could type them could change the reading.
 */
fun AuthoredPage.applicableQuestions(
    answers: AuthoredAnswers,
    sex: String,
    stage: String,
): List<AuthoredQuestion> {
    val shown = LinkedHashSet<String>()
    val out = ArrayList<AuthoredQuestion>(questions.size)
    for (q in questions) {
        if (q.onlyIfSex.isNotBlank() && !q.onlyIfSex.equals(sex, ignoreCase = true)) continue
        if (q.onlyIfStage.isNotEmpty() && q.onlyIfStage.none { it.equals(stage, ignoreCase = true) }) continue
        if (q.onlyIfQuestion.isNotBlank()) {
            if (q.onlyIfQuestion !in shown) continue
            if (answers.of(q.onlyIfQuestion).none { it in q.onlyIfIn }) continue
        }
        shown += q.id
        out += q
    }
    return out
}

/**
 * What is still unanswered on this page.
 *
 * EVERY applicable question is compulsory. A blank cannot tell "nobody looked" from "normal", and
 * the unexplained-findings channel depends on that difference -- which is why a question hidden by
 * sex records nothing rather than being left empty: it was never asked.
 */
fun AuthoredPage.missing(answers: AuthoredAnswers, sex: String, stage: String): List<String> =
    applicableQuestions(answers, sex, stage)
        .filterNot { answers.answered(it.id) }
        .map { it.title }
