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

    /** Four sale statuses on one phone row clipped to "In Discussi…" / "Advance P…" (2026-09-26). */
    @Test
    fun `a crowded choice wraps into rows of two instead of clipping`() {
        assertEquals(listOf(listOf("CBE", "CPT")), segmentedRows(listOf("CBE", "CPT")))
        assertEquals(listOf(listOf("a", "b", "c")), segmentedRows(listOf("a", "b", "c")))
        assertEquals(
            listOf(listOf("Deal Closed", "Deal Failed"), listOf("In Discussion", "Advance Paid")),
            segmentedRows(listOf("Deal Closed", "Deal Failed", "In Discussion", "Advance Paid")),
        )
        assertEquals(listOf(listOf("a", "b"), listOf("c", "d"), listOf("e")), segmentedRows(listOf("a", "b", "c", "d", "e")))
        assertEquals(emptyList<List<String>>(), segmentedRows(emptyList<String>()))
    }
}
