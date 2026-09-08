package sg.mesha.goatos.core.data.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class VendorsPayloadsTest {
    @Test
    fun `a vendor edit shares its form's lane with the create and keys idempotency apart from it`() {
        // Same client id -> same FIFO lane, so a re-recorded voice note's upload drains before the
        // update that references it; but a DIFFERENT idempotency key, so an edit can never be
        // read by the server as a replay of the create.
        assertEquals(vendorCreateGroupKey("form-9"), vendorCreateGroupKey("form-9"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9"), vendorCreateIdempotencyKey("form-9"))
        assertEquals(vendorUpdateIdempotencyKey("form-9"), vendorUpdateIdempotencyKey("form-9"))
    }

    @Test
    fun `pipeline lead status lanes are keyed by the target lead`() {
        val firstTap = salesPipelineLeadGroupKey("lead-17")
        val secondTap = salesPipelineLeadGroupKey("lead-17")

        assertEquals(firstTap, secondTap)
        assertNotEquals(firstTap, salesPipelineGroupKey("client-random"))
    }
}
