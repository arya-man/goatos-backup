package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineDto

/**
 * What the phone says about a sale's date and what was sold (real-device E2E, 2026-09-26):
 * an Advance Paid sale for 30/09 read "Sold 30/09/2026" on 26/09, a manure line read
 * "Manure · Manure", and a feed line showed its value with no quantity.
 */
class SaleDatePresentationTest {

    private fun saleRows(deal: SalesDealDto) = deal.sections().first { it.title == "The sale" }.rows.associate { it.label to it.value }

    @Test
    fun `an open sale is planned, a failed one is dated, only a closed one is sold`() {
        val open = SalesDealDto(dealId = "d-1", saleDate = "2026-09-30", status = "Advance Paid", salesValue = 31800.0, paymentBalance = 26800.0)
        assertTrue(open.toCardUi().metaLine.startsWith("Planned 30/09/2026"))
        assertEquals("30/09/2026", saleRows(open)["Planned for"])
        assertFalse(saleRows(open).containsKey("Sold on"))

        val talking = SalesDealDto(dealId = "d-2", saleDate = "2026-09-30", status = "In Discussion")
        assertTrue(talking.toCardUi().metaLine.startsWith("Planned 30/09/2026"))

        val failed = SalesDealDto(dealId = "d-3", saleDate = "2026-08-12", status = "Deal Failed")
        assertTrue(failed.toCardUi().metaLine.startsWith("Sale date 12/08/2026"))
        assertEquals("12/08/2026", saleRows(failed)["Sale date"])

        val closed = SalesDealDto(dealId = "d-4", saleDate = "2026-09-21", status = "Deal Closed")
        assertTrue(closed.toCardUi().metaLine.startsWith("Sold 21/09/2026"))
        assertEquals("21/09/2026", saleRows(closed)["Sold on"])
    }

    @Test
    fun `a product whose breed is its own name says it once`() {
        assertEquals("Manure", productAndBreed("Manure", "Manure"))
        assertEquals("Goat · Beetal", productAndBreed("Goat", "Beetal"))
        val manure = SalesDealDto(dealId = "d-5", productType = "Manure", breed = "Manure", farm = "CPT", status = "Deal Closed")
        assertEquals("Manure · CPT", manure.toCardUi().productLine)
        assertFalse(saleRows(manure).containsKey("Breed"))
    }

    @Test
    fun `a line sold by the unit shows its quantity and rate`() {
        assertEquals("20 kg at ₹40/kg", quantityAtRate(20.0, "kg", 40.0))
        assertEquals("", quantityAtRate(null, "", null))
        val mixed = SalesDealDto(
            dealId = "d-6",
            status = "Advance Paid",
            lines = listOf(
                SalesDealLineDto(lineNo = 1, productType = "Goat", breed = "Beetal", animalCount = 2.0, totalWeightKg = 70.0, salesValue = 30000.0),
                SalesDealLineDto(lineNo = 2, productType = "Feed", breed = "Mesha Adult Concentrate", quantity = 20.0, unit = "kg", ratePerUnit = 40.0, salesValue = 800.0),
                SalesDealLineDto(lineNo = 3, productType = "Manure", breed = "Manure", quantity = 500.0, unit = "kg", ratePerUnit = 2.0, salesValue = 1000.0),
            ),
        )
        val sold = mixed.sections().first { it.title == "What was sold" }.rows.associate { it.label to it.value }
        assertEquals("2 animals · 70 kg · ₹30,000", sold["Goat · Beetal"])
        assertEquals("20 kg at ₹40/kg · ₹800", sold["Feed · Mesha Adult Concentrate"])
        assertEquals("500 kg at ₹2/kg · ₹1,000", sold["Manure"])
    }
}
