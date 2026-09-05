package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The lead boards are SEARCHED and PAGED (maintainer instruction 2026-09-05), and the Room rows a
 * board renders are found by ONE thing: `sales_lead_items.queryKey`.
 *
 * What this holds: the search text and the status word are part of that key, and so is the side.
 * If they were not, a board narrowed to "kumar" and the unfiltered board would share one window --
 * whichever fetched last would decide what BOTH of them showed, and a search would either return
 * two hundred unmatched leads or wipe the board down to somebody else's four. That is not a
 * cosmetic collision: the operator cannot tell a wrong answer from a right one here, because a
 * lead board looks the same either way.
 */
class SalesLeadScopeTest {

    @Test
    fun `a search never shares its window with the unfiltered board`() {
        val unfiltered = salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = "")
        val searched = salesLeadScopeKey(SalesLeadSide.BUYER, search = "kumar", status = "")

        assertNotEquals(unfiltered, searched)
        // Two different searches are two different boards as well -- sharing between them would
        // show the previous search's leads under the current search's box.
        assertNotEquals(searched, salesLeadScopeKey(SalesLeadSide.BUYER, search = "erode", status = ""))
    }

    @Test
    fun `a status filter never shares its window with the unfiltered board`() {
        val unfiltered = salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = "")
        val interested = salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = "Interested")

        assertNotEquals(unfiltered, interested)
        assertNotEquals(interested, salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = "Not interested"))
        // And the two narrowings compose: a search inside a status is its own board again.
        assertNotEquals(interested, salesLeadScopeKey(SalesLeadSide.BUYER, search = "kumar", status = "Interested"))
    }

    @Test
    fun `the two boards never share a window, on any filter`() {
        assertNotEquals(
            salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = ""),
            salesLeadScopeKey(SalesLeadSide.FARMER_GROUP, search = "", status = ""),
        )
        // Same under a filter, not only unfiltered: a shared key under one particular search would
        // be just as wrong and far harder to notice.
        assertNotEquals(
            salesLeadScopeKey(SalesLeadSide.BUYER, search = "erode", status = "Interested"),
            salesLeadScopeKey(SalesLeadSide.FARMER_GROUP, search = "erode", status = "Interested"),
        )
        assertTrue(salesLeadScopeKey(SalesLeadSide.BUYER, "", "").contains(SalesLeadSide.BUYER.wireValue))
    }

    @Test
    fun `the same search written differently is one window, not two`() {
        // Trimmed and lower-cased, so one typist's spacing is not a second cached scope holding a
        // second copy of the same two hundred rows.
        assertEquals(
            salesLeadScopeKey(SalesLeadSide.BUYER, search = "  Kumar ", status = "Interested"),
            salesLeadScopeKey(SalesLeadSide.BUYER, search = "kumar", status = "Interested"),
        )
    }

    @Test
    fun `the count and status vocabulary follow the same scope as the rows`() {
        // The count under the title answers the filter in force. Caching it per side alone would
        // print the unfiltered 208 above a board showing four searched leads.
        assertNotEquals(
            salesLeadMetaCacheKey(SalesLeadSide.BUYER, search = "", status = ""),
            salesLeadMetaCacheKey(SalesLeadSide.BUYER, search = "kumar", status = ""),
        )
        // ...and it is never the rows' own key, or the meta blob would be read as a row window.
        assertNotEquals(
            salesLeadMetaCacheKey(SalesLeadSide.BUYER, search = "", status = ""),
            salesLeadScopeKey(SalesLeadSide.BUYER, search = "", status = ""),
        )
    }
}
