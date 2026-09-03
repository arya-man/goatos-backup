package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.feature.vendors.VendorsTone

/**
 * The Vendors module's presentation layer (module vendors, maintainer decision 2026-09-03).
 *
 * What these hold: backend copy reaches the card and the detail rows VERBATIM (display name,
 * status label, capacity line, delivery and payment words), numbers and dates are formatted the
 * farm's way (Indian grouping, DD-MM-YYYY), and blanks are dropped rather than rendered as "".
 */
class VendorsPresentationTest {

    @Test
    fun `a vendor card renders backend copy verbatim`() {
        val card = VendorDto(
            vendorId = "v-1",
            recordType = "Feed Agent",
            businessName = "Kumar Traders",
            displayName = "Kumar Traders - Kumar",
            status = "negotiating",
            statusLabel = "Negotiating",
            state = "KA",
            locationDisplay = "Mysuru, KA",
            capacityDisplay = "5,000 kg · Every 2 weeks",
            voiceNoteProofRef = "11111111-0000-4000-8000-000000000001",
        ).toCardUi()

        assertEquals("v-1", card.listKey)
        assertEquals("Kumar Traders - Kumar", card.name)
        assertEquals("Feed Agent · Mysuru, KA", card.typeLine)
        assertEquals("5,000 kg · Every 2 weeks", card.capacityLine)
        assertEquals("Negotiating", card.statusLabel)
        assertEquals(VendorsTone.INFO, card.statusTone)
        assertTrue(card.hasVoiceNote)
    }

    @Test
    fun `a vendor detail drops blank rows and keeps the sections that have any`() {
        val sections = VendorDto(
            vendorId = "v-2",
            recordType = "Sheep Agent",
            businessName = "Ravi",
            displayName = "Ravi",
            status = "active",
            statusLabel = "Active",
            state = "TN",
            locationDisplay = "TN",
            pricePerGoat = "8500.00",
            etaAfterOrderDays = 3,
        ).sections()

        assertEquals(listOf("Who they are", "What they supply"), sections.map { it.title })
        val supply = sections[1].rows.associate { it.label to it.value }
        assertEquals("₹8500.00", supply["Price per goat"])
        assertEquals("3 days", supply["Lead time"])
        assertFalse(supply.containsKey("Capacity"))
    }

    @Test
    fun `a purchase card formats the farm's numbers and carries the delivery word`() {
        val card = FeedPurchaseDto(
            feedPurchaseId = "p-1",
            purchaseDate = "2026-09-01",
            farm = "CBE",
            feedItem = "Concentrate",
            batchNo = 328,
            quantityKg = 785714.5,
            totalCost = 23000.0,
            vendor = "QA Vendor",
            paymentStatus = "Pending",
            deliveryStatus = "purchased",
        ).toCardUi()

        assertEquals("CBE · Load 328", card.loadLine)
        assertEquals("7,85,714.5 kg · ₹23,000", card.quantityLine)
        assertEquals("Bought 01-09-2026 · QA Vendor", card.metaLine)
        assertEquals("On the road", card.deliveryLabel)
        assertEquals(VendorsTone.WARN, card.deliveryTone)
        assertEquals(VendorsTone.WARN, card.paymentTone)

        val reached = FeedPurchaseDto(feedPurchaseId = "p-2", deliveryStatus = "reached", reachedOn = "2026-09-03", paymentStatus = "Paid").toCardUi()
        assertEquals("Reached 03-09-2026", reached.deliveryLabel)
        assertEquals(VendorsTone.OK, reached.deliveryTone)
        assertEquals(VendorsTone.OK, reached.paymentTone)
    }

    @Test
    fun `indian grouping and rupees`() {
        assertEquals("1,60,23,173", indianNumber(16023173.0, 0))
        assertEquals("999", indianNumber(999.0, 1))
        assertEquals("1,000", indianNumber(1000.0, 1))
        assertEquals("2.5", indianNumber(2.5, 1))
        assertEquals("₹23,000", rupees(23000.0))
        assertEquals("", rupees(null))
        assertEquals("01-09-2026", farmDate("2026-09-01"))
        assertEquals("0:42", formatLength(42_000L))
    }
}
