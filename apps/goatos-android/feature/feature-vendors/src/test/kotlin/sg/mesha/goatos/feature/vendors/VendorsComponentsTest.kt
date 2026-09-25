package sg.mesha.goatos.feature.vendors

import org.junit.Assert.assertEquals
import org.junit.Test

class VendorsComponentsTest {
    /**
     * Every visible date is DD/MM/YYYY (docs/decisions/date-display-format.md). The sale-date box
     * on the phone's Record sale read `26-09-2026` -- the retired dash form -- while the sale it
     * produced read `Sold 26/09/2026` on the list.
     */
    @Test
    fun `dates render DD slash MM slash YYYY and pass anything else through`() {
        assertEquals("03/09/2026", displayDate("2026-09-03"))
        assertEquals("", displayDate(""))
        assertEquals("today", displayDate("today"))
    }
}
