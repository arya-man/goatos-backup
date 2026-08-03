package sg.mesha.goatos.core.network

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import retrofit2.HttpException

/**
 * Renders the SERVER's own explanation of a refused write, instead of the transport's
 * `HTTP 409 Conflict` status line.
 *
 * The backend already writes farm-language copy into the standard error envelope
 * (`code` / `message` / `field_errors` / optional `retryable`), and for the weighing
 * shed-clash it also names each blocked shed and the date inside `field_errors`. Before
 * this, every repository did `throwable.message ?: fallback`; on a `retrofit2.HttpException`
 * that message IS the status line, so the whole envelope was thrown away and the operator
 * saw a bare status with no reason.
 *
 * Contract direction is deliberate: the backend OWNS the copy, the client only renders it.
 * There is no per-code Kotlin string table here — that would duplicate the server's wording
 * and drift from it. The only client-side text is each caller's generic fallback, used when
 * the response carries no message at all.
 */
data class ServerErrorText(
    val code: String,
    val message: String,
    val fieldMessages: List<String>,
    /**
     * Present only when the envelope declares it (proof/tasks do; the weighing envelope
     * currently does not). `null` means "the server did not say", which a caller must treat
     * as unknown rather than as `false`.
     */
    val retryable: Boolean?,
) {
    /** The message followed by one line per named field problem (e.g. each blocked shed). */
    val display: String
        get() = (listOfNotNull(message.takeIf { it.isNotBlank() }) + fieldMessages).joinToString("\n")
}

/**
 * Parses the standard error envelope off a failed app-api call, or returns null when this is
 * not an app-api failure (transport/local error) or the body carries no readable explanation.
 */
fun Throwable.serverErrorText(): ServerErrorText? {
    val http = this as? HttpException ?: return null
    val raw = runCatching { http.response()?.errorBody()?.string() }.getOrNull()
    if (raw.isNullOrBlank()) return null
    val dto = runCatching { LENIENT_JSON.decodeFromString<ServerErrorEnvelopeDto>(raw) }.getOrNull() ?: return null
    val message = dto.message.trim()
    val fieldMessages = dto.fieldErrors.mapNotNull { it.message.trim().takeIf(String::isNotBlank) }
    if (message.isBlank() && fieldMessages.isEmpty()) return null
    return ServerErrorText(
        code = dto.code.trim(),
        message = message,
        fieldMessages = fieldMessages,
        retryable = dto.retryable,
    )
}

/**
 * The text to put in front of the operator for a failed call: the server's explanation when
 * there is one, otherwise the caller's own farm-language fallback. It NEVER returns a raw
 * throwable message, so a status line, exception class name, or host/socket detail can no
 * longer reach the screen.
 */
fun Throwable.userFacingMessage(fallback: String): String = serverErrorText()?.display ?: fallback

private val LENIENT_JSON = Json {
    ignoreUnknownKeys = true
    coerceInputValues = true
}

@Serializable
private data class ServerErrorEnvelopeDto(
    val code: String = "",
    val message: String = "",
    @SerialName("field_errors") val fieldErrors: List<ServerFieldErrorDto> = emptyList(),
    val retryable: Boolean? = null,
)

@Serializable
private data class ServerFieldErrorDto(
    val field: String = "",
    val code: String = "",
    val message: String = "",
)
