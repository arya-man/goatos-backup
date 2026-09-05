package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The vendor register has TWO SIDES (maintainer decision 2026-09-05, migration 000256), and the
 * phone hosts both on the SAME screens — Procurement > Vendors and Sales > Vendors. Nothing but the
 * cache scope keeps them apart on the device.
 *
 * What this holds: the side is part of BOTH cache keys. If it were not, the two tabs would share
 * one `vendor_items.queryKey` and each would render the other's rows, and one catalog blob would
 * hand the Add-vendor form on the selling page the buying desk's forty supply categories — the
 * exact mix the split was made to remove.
 */
class VendorRegisterSideScopeTest {

    @Test
    fun `the two sides never share a list scope key`() {
        val procurement = vendorScopeKey(VendorRegisterSide.PROCUREMENT, search = "", status = "")
        val sales = vendorScopeKey(VendorRegisterSide.SALES, search = "", status = "")

        assertNotEquals(procurement, sales)
        // Same on every filter the register offers, not only the unfiltered view: a shared key
        // under one particular search would be just as wrong and far harder to notice.
        assertNotEquals(
            vendorScopeKey(VendorRegisterSide.PROCUREMENT, search = "Kumar", status = "active"),
            vendorScopeKey(VendorRegisterSide.SALES, search = "Kumar", status = "active"),
        )
        assertTrue(procurement.contains(VendorRegisterSide.PROCUREMENT.wireValue))
        assertTrue(sales.contains(VendorRegisterSide.SALES.wireValue))
    }

    @Test
    fun `a side's list scope still collapses the same filter written differently`() {
        // The side is added to the key, it does not change how the rest of the key behaves: search
        // is still trimmed and lower-cased, so one typist's spacing is not a second cached scope.
        assertEquals(
            vendorScopeKey(VendorRegisterSide.SALES, search = "  Butcher ", status = "active"),
            vendorScopeKey(VendorRegisterSide.SALES, search = "butcher", status = "active"),
        )
    }

    @Test
    fun `the two sides never share a catalog blob key`() {
        assertNotEquals(
            vendorCatalogCacheKey(VendorRegisterSide.PROCUREMENT),
            vendorCatalogCacheKey(VendorRegisterSide.SALES),
        )
    }

    @Test
    fun `each side names itself on the wire exactly as the server spells it`() {
        // These strings are the `side` query param. The server refuses an unknown value outright
        // (400 vendor_side_unknown) rather than widening to the whole register, so a typo here is
        // an empty page, not a quietly mixed one.
        assertEquals("procurement", VendorRegisterSide.PROCUREMENT.wireValue)
        assertEquals("sales", VendorRegisterSide.SALES.wireValue)
    }
}
