package sg.mesha.goatos.core.data

import kotlinx.serialization.Serializable

/**
 * The add-animal form's answers to the served SOP questionnaire, AS TYPED: scalars (a choice's
 * option value, free text, a number's text, an "other" free text under `<id>_other`) and multis.
 * Serializable so the form can persist the whole map in its SavedStateHandle across a process
 * death; the wire `answers` JSON is built from it at submit, never stored here.
 */
@Serializable
data class AnimalPurchaseAnswers(
    val scalar: Map<String, String> = emptyMap(),
    val multi: Map<String, List<String>> = emptyMap(),
) {
    fun withScalar(id: String, value: String): AnimalPurchaseAnswers =
        copy(scalar = if (value.isEmpty()) scalar - id else scalar + (id to value))

    fun withMultiToggled(id: String, value: String, checked: Boolean): AnimalPurchaseAnswers {
        val current = multi[id].orEmpty()
        val next = if (checked) (current + value).distinct() else current - value
        return copy(multi = if (next.isEmpty()) multi - id else multi + (id to next))
    }

    /** Drops a question's answer and its "other" text. */
    fun without(id: String): AnimalPurchaseAnswers = copy(scalar = scalar - id - (id + OTHER_SUFFIX), multi = multi - id)

    /** The server's own reading of a choice answer: lower-cased and trimmed. */
    fun choice(id: String): String = scalar[id].orEmpty().trim().lowercase()

    companion object {
        /** The answer key suffix an "other" option's free text rides under. */
        const val OTHER_SUFFIX = "_other"
    }
}
