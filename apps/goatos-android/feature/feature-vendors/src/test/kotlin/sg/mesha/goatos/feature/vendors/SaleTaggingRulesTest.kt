package sg.mesha.goatos.feature.vendors

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The tag-only flow's client-side rules (maintainer decision 2026-09-11). Each pins a refusal the
 * backend makes so the screen holds Submit back for the same reason the server would refuse.
 */
class SaleTaggingRulesTest {
    private fun animal(id: String, weight: String = "30", rate: String = "5000", blocked: String = "") =
        SaleTaggingBasketAnimalUi(id, "tag-$id", "Castro 1", weight, rate, "", "", blocked)

    @Test
    fun `submit needs the sale filled exactly`() {
        assertFalse(SaleTaggingRules.submitGate(emptyList(), 2).enabled)
        assertEquals("1 more animal to tag before you can submit", SaleTaggingRules.submitGate(listOf(animal("a")), 2).hint)
        assertEquals("Remove 1: this sale needs only 2 more", SaleTaggingRules.submitGate(listOf(animal("a"), animal("b"), animal("c")), 2).hint)
        assertTrue(SaleTaggingRules.submitGate(listOf(animal("a"), animal("b")), 2).enabled)
    }

    @Test
    fun `submit needs a weight and a rate on every animal`() {
        assertEquals(SaleTaggingRules.HINT_FIGURES, SaleTaggingRules.submitGate(listOf(animal("a", weight = "")), 1).hint)
        assertEquals(SaleTaggingRules.HINT_FIGURES, SaleTaggingRules.submitGate(listOf(animal("a", rate = "0")), 1).hint)
        assertEquals(SaleTaggingRules.HINT_FIGURES, SaleTaggingRules.submitGate(listOf(animal("a", rate = "5000.125")), 1).hint)
        assertTrue(SaleTaggingRules.submitGate(listOf(animal("a", weight = "32.5", rate = "5200.50")), 1).enabled)
    }

    @Test
    fun `a refused animal holds submit until it is removed`() {
        assertEquals(SaleTaggingRules.HINT_BLOCKED, SaleTaggingRules.submitGate(listOf(animal("a", blocked = "In quarantine")), 1).hint)
    }

    @Test
    fun `a scanned tag resolves only on an exact identifier match`() {
        data class M(val ids: List<String>)
        val rows = listOf(M(listOf("982000123456789", "")), M(listOf("982000123456780", "T-42")))
        assertEquals(rows[0], SaleTaggingRules.exactMatch("982000123456789", rows) { it.ids })
        assertEquals(rows[1], SaleTaggingRules.exactMatch(" t-42 ", rows) { it.ids })
        // A substring hit is a different animal: the reader hands back the whole number.
        assertNull(SaleTaggingRules.exactMatch("12345678", rows) { it.ids })
        assertNull(SaleTaggingRules.exactMatch("", rows) { it.ids })
    }
}
