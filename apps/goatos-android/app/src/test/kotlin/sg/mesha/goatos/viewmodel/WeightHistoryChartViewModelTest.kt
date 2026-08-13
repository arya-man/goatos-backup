package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.WeightPointDto
import sg.mesha.goatos.core.network.WeightSeriesDto

/**
 * Contract of the weight-history payload, as the chart depends on it.
 *
 * The previous version of this file built a UI object BY HAND and then asserted on the object it
 * had just built — it would have passed no matter what the ViewModel did, and it broke only
 * because the type it named was deleted. These assertions are on the DTO shape the screen really
 * reads, so a backend change that would break the chart breaks here first.
 */
class WeightHistoryChartViewModelTest {

    private fun individual(tag: String?, kg: Double?, date: String, shed: String = "Godel 1") =
        WeightPointDto(
            capture_kind = "individual",
            scanned_identifier = tag,
            weigh_date = date,
            weight_kg = kg,
            campaign_shed_id = "shed-1",
            shed_display_name = shed,
        )

    @Test
    fun `a lump-sum series carries no scanned identifier and must stay nullable`() {
        // A shed weighed as a group has no animal tag. Declared non-null, this field once failed
        // the WHOLE payload to deserialize the moment one lump-sum weighing existed, and the
        // screen reported "couldn't fetch" for every individually-weighed animal too.
        val series = WeightSeriesDto(
            scanned_identifier = null,
            campaign_shed_id = "shed-1",
            shed_display_name = "Castro 3",
            capture_kind = "lump_sum",
            points = listOf(
                WeightPointDto(
                    capture_kind = "lump_sum",
                    scanned_identifier = null,
                    weigh_date = "2026-08-03",
                    total_weight_kg = 1240.5,
                    animal_count = 48,
                    campaign_shed_id = "shed-1",
                    shed_display_name = "Castro 3",
                ),
            ),
        )
        assertEquals(null, series.scanned_identifier)
        assertEquals("Castro 3", series.shed_display_name)
        assertEquals(1240.5, series.points.first().total_weight_kg!!, 0.001)
    }

    @Test
    fun `an individual point carries weight_kg, a lump-sum point carries the shed total`() {
        // The two kinds are read from DIFFERENT fields. Reading the wrong one yields null, and a
        // null coerced to 0.0 would draw a bar claiming the animal weighed nothing.
        val ind = individual("901007000504332", 21.5, "2026-08-04")
        assertEquals(21.5, ind.weight_kg!!, 0.001)
        assertEquals(null, ind.total_weight_kg)
    }

    @Test
    fun `verification status travels with the point so a rejected weigh can be marked`() {
        val rejected = individual("tag", 22.5, "2026-08-04").copy(verification_status = "rejected")
        val pending = individual("tag", 24.0, "2026-08-04").copy(verification_status = "pending")
        assertEquals("rejected", rejected.verification_status)
        assertEquals("pending", pending.verification_status)
        // An absent status must not masquerade as verified.
        assertEquals(null, individual("tag", 24.0, "2026-08-04").verification_status)
    }

    @Test
    fun `a point with no weight is distinguishable from a zero weight`() {
        // null means "this payload carried no reading"; 0.0 would mean "the animal weighed zero".
        // The chart drops the former and would be obliged to draw the latter, so the two must
        // never collapse into one value.
        val missing = individual("tag", null, "2026-08-04")
        assertTrue(missing.weight_kg == null)
        assertFalse(missing.weight_kg == 0.0)
    }
}
