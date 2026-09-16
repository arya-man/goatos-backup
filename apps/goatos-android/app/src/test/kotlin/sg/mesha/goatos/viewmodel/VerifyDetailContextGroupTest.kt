package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.VerificationContextRowDto
import sg.mesha.goatos.feature.verify.VerifyContextKind
import sg.mesha.goatos.feature.verify.VerifyContextRow
import sg.mesha.goatos.feature.verify.contextGroupHeaderAt

/**
 * A producer may section its context rows ("Crew answers"). The group rides the wire, passes
 * through the view model verbatim, and the card starts a header only where the group CHANGES.
 *
 * telemetry:exempt pure mapping unit test — renders no surface.
 */
class VerifyDetailContextGroupTest {
    @Test
    fun `backend context rows carry their group through verbatim`() {
        val rows = backendVerifyContextRows(
            listOf(
                VerificationContextRowDto(label = "Pen", value = "Castro 2"),
                VerificationContextRowDto(label = "Trough clean?", value = "Yes", group = " Crew answers "),
                VerificationContextRowDto(label = "Blank", value = " ", group = "Crew answers"),
            ),
        )
        assertEquals(2, rows.size)
        assertNull(rows[0].group)
        assertEquals("Crew answers", rows[1].group)
        assertEquals("Trough clean?", rows[1].backendLabel)
    }

    @Test
    fun `a header starts only where the group changes`() {
        val rows = listOf(
            VerifyContextRow(VerifyContextKind.SHED, "Castro 2"),
            VerifyContextRow(VerifyContextKind.RAISED_NOTE, "Yes", backendLabel = "Trough clean?", group = "Crew answers"),
            VerifyContextRow(VerifyContextKind.RAISED_NOTE, "No", backendLabel = "Water fresh?", group = "Crew answers"),
            VerifyContextRow(VerifyContextKind.RAISED_NOTE, "4 kg", backendLabel = "Leftover", group = "Measured"),
        )
        assertEquals(listOf(null, "Crew answers", null, "Measured"), rows.indices.map { contextGroupHeaderAt(rows, it) })
    }
}
