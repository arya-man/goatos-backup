package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.feature.vendors.SalesBuyerLeadField
import sg.mesha.goatos.feature.vendors.SalesFpoLeadField

/**
 * A lead the server refuses. On the phone (2026-09-26) a buyer lead with a 176-letter place came
 * back `sales_invalid_buyer_place` / "Buyer place too long." and the sentence sat in a banner
 * scrolled off the top of the form, with no box marked: the person at Save saw nothing happen.
 * The refusal now also lands on the box its code names.
 */
class SalesLeadRefusalTest {

    @Test
    fun theRefusalCodeNamesTheBuyerLeadBox() {
        val field = salesRefusedField(field = null, code = "sales_invalid_buyer_place")
        assertEquals(SalesBuyerLeadField.BUYER_PLACE.name, refusedLeadField(buyerBoard = true, backendField = field))
        assertEquals(SalesBuyerLeadField.PHONE_NUMBER.name, refusedLeadField(true, "phone_number"))
    }

    @Test
    fun theFarmerGroupBoardKeysItsOwnBoxes() {
        assertEquals(SalesFpoLeadField.TALUK.name, refusedLeadField(buyerBoard = false, backendField = "taluk"))
        // A buyer-only box is not on the farmer-group form.
        assertNull(refusedLeadField(buyerBoard = false, backendField = "buyer_place"))
    }

    @Test
    fun aRefusalNamingNoBoxMarksNone() {
        assertNull(refusedLeadField(true, null))
        assertNull(refusedLeadField(true, ""))
        assertNull(refusedLeadField(true, salesRefusedField(null, "sales_write_failed")))
    }
}
