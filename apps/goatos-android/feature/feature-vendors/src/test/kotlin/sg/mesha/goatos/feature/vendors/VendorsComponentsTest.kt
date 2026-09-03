package sg.mesha.goatos.feature.vendors

import org.junit.Assert.assertEquals
import org.junit.Test

class VendorsComponentsTest {
    @Test
    fun `dates render DD-MM-YYYY and pass anything else through`() {
        assertEquals("03-09-2026", displayDate("2026-09-03"))
        assertEquals("", displayDate(""))
        assertEquals("today", displayDate("today"))
    }
}
