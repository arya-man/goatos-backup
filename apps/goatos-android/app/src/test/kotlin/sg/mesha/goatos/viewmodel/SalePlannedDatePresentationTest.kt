package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.SalesDealDto

/**
 * Closing a sale stamps the close date and keeps the planned one (2026-09-25). The detail shows
 * "Planned for DD/MM/YYYY" only when the sale closed on a different day than it was planned for.
 */
class SalePlannedDatePresentationTest {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    private fun saleRows(deal: SalesDealDto) = deal.sections().first { it.title == "The sale" }.rows

    @Test
    fun `a sale closed on another day shows the day it was planned for`() {
        val deal = SalesDealDto(dealId = "d-1", saleDate = "2026-09-25", plannedSaleDate = "2026-09-20", farm = "CBE")
        assertEquals("20/09/2026", saleRows(deal).first { it.label == "Planned for" }.value)
        assertEquals("25/09/2026", saleRows(deal).first { it.label == "Sold on" }.value)
    }

    @Test
    fun `a sale closed on its planned day or recorded closed shows no planned row`() {
        val sameDay = SalesDealDto(dealId = "d-2", saleDate = "2026-09-25", plannedSaleDate = "2026-09-25")
        val recordedClosed = SalesDealDto(dealId = "d-3", saleDate = "2026-09-25")
        assertNull(saleRows(sameDay).firstOrNull { it.label == "Planned for" })
        assertNull(saleRows(recordedClosed).firstOrNull { it.label == "Planned for" })
    }

    @Test
    fun `the planned date decodes from the wire and is absent on an older server`() {
        val withPlan = json.decodeFromString<SalesDealDto>("""{"deal_id":"d","sale_date":"2026-09-25","planned_sale_date":"2026-09-21"}""")
        val older = json.decodeFromString<SalesDealDto>("""{"deal_id":"d","sale_date":"2026-09-25"}""")
        assertEquals("2026-09-21", withPlan.plannedSaleDate)
        assertNull(older.plannedSaleDate)
    }
}
