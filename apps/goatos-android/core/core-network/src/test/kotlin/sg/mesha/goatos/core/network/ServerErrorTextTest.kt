package sg.mesha.goatos.core.network

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import retrofit2.HttpException
import retrofit2.Response

/**
 * A refused write must explain ITSELF. The backend already writes farm-language copy into the
 * error envelope; these tests pin that the client renders that copy — message plus every named
 * field problem — and never the transport's status line.
 */
class ServerErrorTextTest {

    private fun refusal(status: Int, body: String): Throwable =
        HttpException(Response.error<Any>(status, body.toResponseBody("application/json".toMediaType())))

    @Test
    fun `a refused publish shows the server's reason and names each blocked shed, never the status line`() {
        val error = refusal(
            409,
            """
            {
              "code": "weighing_shed_already_scheduled",
              "message": "Some of these sheds are already scheduled on this date.",
              "field_errors": [
                {"field": "sheds", "code": "weighing_shed_already_scheduled", "message": "Shed A1 is already scheduled on 2026-08-04."},
                {"field": "sheds", "code": "weighing_shed_already_scheduled", "message": "Shed A2 is already scheduled on 2026-08-04."}
              ],
              "trace_id": "t-1"
            }
            """.trimIndent(),
        )

        val shown = error.userFacingMessage("Could not publish this weighing task.")

        assertEquals(
            "Some of these sheds are already scheduled on this date.\n" +
                "Shed A1 is already scheduled on 2026-08-04.\n" +
                "Shed A2 is already scheduled on 2026-08-04.",
            shown,
        )
        assertFalse("must not show the transport status line", shown.contains("HTTP"))
        assertFalse("must not show a bare status code", shown.contains("409"))
        assertFalse("must not fall back once the server explained itself", shown.contains("Could not publish"))
    }

    /**
     * A single-field refusal repeats its sentence in `field_errors` (the counts capture-card
     * handler does). The operator saw "Say which, for: Suspected cause" twice on Add death.
     */
    @Test
    fun `a field error that repeats the message is shown once`() {
        val error = refusal(
            422,
            """{"code":"capture_answer_invalid","message":"Write the other answer for: Suspected cause","field_errors":[{"field":"cause","code":"capture_answer_invalid","message":"Write the other answer for: Suspected cause"},{"field":"cause","code":"capture_answer_invalid","message":"Write the other answer for: Suspected cause"}]}""",
        )

        assertEquals("Write the other answer for: Suspected cause", error.userFacingMessage("fallback"))
    }

    @Test
    fun `an envelope with no field errors still shows the server's own sentence`() {
        val error = refusal(
            409,
            """{"code":"operator_outside_park","message":"One of the people chosen does not work in this park. Pick someone from this park, or a director who covers both.","trace_id":"t-2"}""",
        )

        val shown = error.userFacingMessage("Could not save this weighing task.")

        assertEquals(
            "One of the people chosen does not work in this park. Pick someone from this park, or a director who covers both.",
            shown,
        )
        assertFalse(shown.contains("HTTP"))
    }

    @Test
    fun `a refusal with no message falls back to farm language, never to a status line`() {
        val shown = refusal(409, """{"trace_id":"t-3"}""")
            .userFacingMessage("Could not publish this weighing task.")

        assertEquals("Could not publish this weighing task.", shown)
        assertFalse(shown.contains("HTTP"))
        assertFalse(shown.contains("409"))
    }

    @Test
    fun `an unreadable or empty body falls back to farm language`() {
        assertEquals(
            "Could not load this shed.",
            refusal(500, "").userFacingMessage("Could not load this shed."),
        )
        assertEquals(
            "Could not load this shed.",
            refusal(500, "<html>gateway</html>").userFacingMessage("Could not load this shed."),
        )
    }

    @Test
    fun `a local or transport failure never leaks its technical text`() {
        val shown = java.io.IOException("Unable to resolve host \"api.example\"")
            .userFacingMessage("Could not load weighing tasks.")

        assertEquals("Could not load weighing tasks.", shown)
    }

    @Test
    fun `retryable is read when the envelope declares it and stays unknown when it does not`() {
        val declared = refusal(409, """{"code":"x","message":"Try that again in a moment.","retryable":true}""")
            .serverErrorText()
        assertTrue("server said this can be retried", declared?.retryable == true)

        val silent = refusal(409, """{"code":"verification_pending","message":"This shed still has videos waiting to be checked."}""")
            .serverErrorText()
        assertNull("no claim means unknown, not false", silent?.retryable)
    }

    /**
     * THE ENVELOPE MUST SURVIVE BEING READ TWICE, and this is not a style point: the outbox
     * records a refusal by asking the SAME throwable three questions -- the sentence to show, the
     * code to act on, and the field to mark. An error body is a stream that can be consumed once,
     * so the first question answered and the other two came back empty.
     *
     * Found on the phone (2026-09-24): a feed sale the store could not cover was refused 422
     * `feed_stock_confirmation_required`, the outbox row kept the server's sentence and NULL for
     * the code, and the screen -- which keys on the CODE, as it must, because the sentence is farm
     * copy that may be reworded -- fell through to "Could not save this sale. Try again." instead
     * of offering to confirm it. The same read is what the verifier's packing-variance confirm
     * keys on, so it was one bug over two features.
     */
    @Test
    fun `the envelope answers the same three questions however many times it is asked`() {
        val error = refusal(
            422,
            """
            {"error":"feed_stock_confirmation_required","field":"lines[0].quantity","message":"CBE Dry Masoor Bhusa has 13720.1 kg in the store and this sale takes 99999 kg."}
            """.trimIndent(),
        )

        val first = error.serverErrorText()
        val second = error.serverErrorText()
        val third = error.serverErrorText()

        assertEquals("feed_stock_confirmation_required", first?.code)
        assertEquals("the code must survive a second read", "feed_stock_confirmation_required", second?.code)
        assertEquals("the field must survive a third read", "lines[0].quantity", third?.field)
        assertEquals(first?.message, third?.message)
    }

}
