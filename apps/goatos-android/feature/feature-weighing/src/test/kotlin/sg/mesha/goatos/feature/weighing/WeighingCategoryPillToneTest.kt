package sg.mesha.goatos.feature.weighing

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * The work-list category pill exists for exactly one reason: an operator must be able to tell a
 * lump-sum shed from an individual-scan shed at a glance. It shipped painting BOTH categories the
 * same purple, which made the only distinguishing chip on the card carry no information.
 *
 * Paparazzi could not catch this -- an all-purple pill renders perfectly well and simply gets
 * re-recorded as the new golden. The invariant has to be asserted directly.
 */
class WeighingCategoryPillToneTest {

    @Test
    fun `lump-sum and individual never share a tone`() {
        val lumpSum = categoryPillTone("per_shed_partition")
        val individual = categoryPillTone("individual")

        assertNotEquals("category pill foreground must differ", lumpSum.first, individual.first)
        assertNotEquals("category pill background must differ", lumpSum.second, individual.second)
    }

    @Test
    fun `lump-sum detection tolerates casing and padding from the backend`() {
        val canonical = categoryPillTone("per_shed_partition")

        assertEquals(canonical, categoryPillTone("  per_shed_partition  "))
        assertEquals(canonical, categoryPillTone("PER_SHED_PARTITION"))
    }

    @Test
    fun `anything that is not lump-sum reads as individual`() {
        val individual = categoryPillTone("individual")

        // Weighing has exactly two categories; an unknown value must not silently acquire the
        // lump-sum look, because that would misreport a scan shed as a video-only one.
        assertEquals(individual, categoryPillTone(""))
        assertEquals(individual, categoryPillTone("something_new"))
    }
}
