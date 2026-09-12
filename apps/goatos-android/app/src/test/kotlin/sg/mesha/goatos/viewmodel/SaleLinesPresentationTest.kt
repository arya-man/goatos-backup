package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.SalesDealDto
import sg.mesha.goatos.core.network.dto.SalesDealLineDto
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto

/**
 * ONE sale, MANY lines (maintainer decision 2026-09-12). The detail screen shows a mixed sale's
 * lines one row each and drops the deal-level "Mixed" product/breed pair; the write DTO posts
 * `lines` and never a deal-level product; a row cached before lines existed still decodes.
 */
class SaleLinesPresentationTest {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }

    @Test
    fun `a mixed sale's detail lists what was sold line by line and hides the Mixed rollup`() {
        val deal = SalesDealDto(
            dealId = "d-1", saleDate = "2026-09-12", farm = "CPT", buyerName = "Tanveer",
            productType = "Mixed", breed = "Mixed", animalCount = 19.0, totalWeightKg = 540.0, salesValue = 221000.0,
            lines = listOf(
                SalesDealLineDto(lineId = "l1", lineNo = 1, productType = "Sheep", breed = "Anantapur", animalCount = 10.0, totalWeightKg = 300.0, salesValue = 120000.0),
                SalesDealLineDto(lineId = "l2", lineNo = 2, productType = "Sheep", breed = "Kenguri", animalCount = 5.0, totalWeightKg = 140.0, salesValue = 56000.0),
                SalesDealLineDto(lineId = "l3", lineNo = 3, productType = "Goat", breed = "Sirohi", animalCount = 4.0, totalWeightKg = 100.0, salesValue = 45000.0),
            ),
        )
        val sections = deal.sections()
        val sale = sections.first { it.title == "The sale" }
        assertNull(sale.rows.firstOrNull { it.label == "Product" })
        assertNull(sale.rows.firstOrNull { it.label == "Breed" })
        assertEquals("19", sale.rows.first { it.label == "Animals" }.value)

        val sold = sections.first { it.title == "What was sold" }
        assertEquals(3, sold.rows.size)
        assertEquals("Sheep · Anantapur", sold.rows[0].label)
        assertEquals("10 animals · 300 kg · ₹1,20,000", sold.rows[0].value)
        assertEquals("Goat · Sirohi", sold.rows[2].label)
    }

    @Test
    fun `a single-line sale keeps the product and breed rows and shows no lines section`() {
        val deal = SalesDealDto(
            dealId = "d-2", saleDate = "2026-09-12", farm = "CBE", buyerName = "Ramesh",
            productType = "Goat", breed = "Sojat", animalCount = 2.0, salesValue = 30000.0,
            lines = listOf(SalesDealLineDto(lineId = "l1", lineNo = 1, productType = "Goat", breed = "Sojat", animalCount = 2.0, salesValue = 30000.0)),
        )
        val sections = deal.sections()
        val sale = sections.first { it.title == "The sale" }
        assertEquals("Goat", sale.rows.first { it.label == "Product" }.value)
        assertEquals("Sojat", sale.rows.first { it.label == "Breed" }.value)
        assertNull(sections.firstOrNull { it.title == "What was sold" })
    }

    @Test
    fun `a deal row cached before lines existed still decodes with no lines`() {
        val cached = """{"deal_id":"d-3","sale_date":"2026-08-01","farm":"CBE","buyer_name":"Old","product_type":"Sheep","breed":"Nipani","sales_value":5000,"payment_balance":0,"payments":[],"status":"Deal Closed"}"""
        val deal = json.decodeFromString<SalesDealDto>(cached)
        assertTrue(deal.lines.isEmpty())
        assertEquals("Sheep", deal.sections().first { it.title == "The sale" }.rows.first { it.label == "Product" }.value)
    }

    @Test
    fun `the write body carries lines and leaves the deal-level product blank for the backend to roll up`() {
        val write = SalesDealWriteDto(
            saleDate = "2026-09-12", farm = "CPT", buyerName = "Tanveer", buyerVendorId = "3f1c2a5e-9b04-4d67-8a11-2c7e5d9f0b34",
            lines = listOf(
                SalesDealLineWriteDto(productType = "Sheep", breed = "Anantapur", animalCount = 10.0, totalWeightKg = 300.0, salesValue = 120000.0),
                SalesDealLineWriteDto(productType = "Goat", breed = "Sirohi", animalCount = 4.0, salesValue = 45000.0),
            ),
        )
        val encoded = json.encodeToString(write)
        assertTrue(encoded, encoded.contains("\"lines\":[{\"product_type\":\"Sheep\",\"breed\":\"Anantapur\""))
        assertTrue(encoded, encoded.contains("\"product_type\":\"\""))
        // explicitNulls=false: a count the desk left blank is ABSENT, never null and never 0.
        assertTrue(encoded, !encoded.contains("\"total_weight_kg\":null"))
    }
}
