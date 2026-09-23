package sg.mesha.goatos.viewmodel

import kotlinx.serialization.json.Json
import kotlinx.serialization.encodeToString
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.SalesDealLineWriteDto
import sg.mesha.goatos.core.network.dto.SalesDealWriteDto
import sg.mesha.goatos.core.network.dto.SalesOptionsDto
import sg.mesha.goatos.core.network.dto.SalesProductOptionDto

/**
 * SELLING FEED AND OTHER ITEMS FROM THE PHONE (maintainer instruction 2026-09-23).
 *
 * WHAT the farm sells is its own registry, edited on the web's Sales Config, and the phone renders
 * it: the list of products, what each IS, and whether selling one asks for a quantity at a rate or
 * for a lot at a negotiated price. These pin the wire shape that carries all of it, because a
 * field dropped between the two surfaces is a sale recorded differently depending on where the
 * person stood when they recorded it.
 */
class FeedSaleLinePresentationTest {

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false; encodeDefaults = true }

    @Test
    fun `the options carry what each product is and how it is sold`() {
        val wire = """
            {"farms":["CBE","CPT"],"product_types":["Goat","Feed","Sheep tags"],
             "products":[
               {"name":"Goat","code":"goat","kind":"animal","unit":"number","priced_per_unit":false},
               {"name":"Feed","code":"feed","kind":"feed","unit":"kg","priced_per_unit":true},
               {"name":"Sheep tags","code":"sheep_tags","kind":"other","unit":"number","priced_per_unit":true}],
             "breeds":{"Feed":["Mesha Kids Concentrate","Dry Masoor Bhusa"],"Sheep tags":["Sheep tags"]},
             "statuses":[],"default_status":"Deal Closed","max_sale_date_days_ahead":60}
        """.trimIndent()
        val options = json.decodeFromString<SalesOptionsDto>(wire)

        val feed = options.products.first { it.code == "feed" }
        assertTrue("feed is sold at a rate per unit", feed.pricedPerUnit)
        assertEquals("kg", feed.unit)
        // The feed a sale names comes from the farm's own feed catalogue, never typed.
        assertEquals(listOf("Mesha Kids Concentrate", "Dry Masoor Bhusa"), options.breeds["Feed"])

        val goat = options.products.first { it.code == "goat" }
        assertFalse("a lot of goats is priced as a lot, not per head", goat.pricedPerUnit)

        // An item with no second dimension answers to itself, which is what lets the screen stop
        // asking a question whose list holds one entry.
        assertEquals(listOf("Sheep tags"), options.breeds["Sheep tags"])
    }

    @Test
    fun `a per-unit line posts its quantity and rate and no value of its own`() {
        val body = SalesDealWriteDto(
            saleDate = "2026-09-23", farm = "CPT", buyerName = "Ramesh Traders", buyerVendorId = "v-1",
            lines = listOf(
                SalesDealLineWriteDto(productType = "Feed", breed = "Mesha Kids Concentrate", quantity = 250.0, ratePerUnit = 42.5, salesValue = 0.0),
            ),
        )
        val wire = json.parseToJsonElement(json.encodeToString(body)).toString()
        assertTrue(wire.contains("\"quantity\":250.0"))
        assertTrue(wire.contains("\"rate_per_unit\":42.5"))
        // The MONEY is the backend's: it multiplies the two above. A figure the phone worked out
        // must never be what is recorded, or the two surfaces could disagree about one sale.
        assertTrue(wire.contains("\"sales_value\":0.0"))
        // Not confirmed unless a person confirmed it.
        assertTrue(wire.contains("\"stock_shortfall_acknowledged\":false"))
    }

    @Test
    fun `an animal line still posts its counts and the price agreed for the lot`() {
        val body = SalesDealWriteDto(
            saleDate = "2026-09-23", farm = "CBE", buyerName = "Irshad Bhai", buyerVendorId = "v-2",
            lines = listOf(
                SalesDealLineWriteDto(productType = "Goat", breed = "Sojat", animalCount = 12.0, totalWeightKg = 360.0, salesValue = 96000.0),
            ),
        )
        val line = body.lines.first()
        assertNull("an animal line carries no quantity", line.quantity)
        assertNull("and no rate", line.ratePerUnit)
        assertEquals(96000.0, line.salesValue, 0.0)
    }

    @Test
    fun `the acknowledgement rides the sale when the person has confirmed the store`() {
        val body = SalesDealWriteDto(
            saleDate = "2026-09-23", farm = "CPT", buyerName = "Over Ask", buyerVendorId = "v-3",
            lines = listOf(SalesDealLineWriteDto(productType = "Feed", breed = "Maize", quantity = 3000.0, ratePerUnit = 41.0, salesValue = 0.0)),
            stockShortfallAcknowledged = true,
        )
        assertTrue(json.encodeToString(body).contains("\"stock_shortfall_acknowledged\":true"))
    }

    @Test
    fun `a build that has never heard of the registry still decodes the options`() {
        // The older shape: names only, no products block. It must not fail to parse -- a phone
        // that cannot read the options cannot record any sale at all.
        val options = json.decodeFromString<SalesOptionsDto>(
            """{"farms":["CBE"],"product_types":["Goat"],"breeds":{"Goat":["Sojat"]},"statuses":[],"default_status":"Deal Closed"}""",
        )
        assertEquals(listOf("Goat"), options.productTypes)
        assertTrue("no registry rows is an empty list, never a crash", options.products.isEmpty())
    }
}
