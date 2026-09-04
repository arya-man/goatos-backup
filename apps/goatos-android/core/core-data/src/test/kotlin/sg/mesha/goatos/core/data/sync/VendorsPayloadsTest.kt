package sg.mesha.goatos.core.data.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class VendorsPayloadsTest {
    @Test
    fun `pipeline lead status lanes are keyed by the target lead`() {
        val firstTap = salesPipelineLeadStatusGroupKey("lead-17")
        val secondTap = salesPipelineLeadStatusGroupKey("lead-17")

        assertEquals(firstTap, secondTap)
        assertNotEquals(firstTap, salesPipelineGroupKey("client-random"))
    }
}
