package sg.mesha.goatos.core.data.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class VendorsPayloadsTest {
    @Test
    fun `a vendor edit shares its form lane but keys each saved commit apart`() {
        // Same client id -> same FIFO lane, so a re-recorded voice note's upload drains before the
        // update that references it; but each saved edit commit carries a DIFFERENT idempotency key,
        // so one month's retained offline commits cannot collapse into the first queued edit.
        assertEquals(vendorCreateGroupKey("form-9"), vendorCreateGroupKey("form-9"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorCreateIdempotencyKey("form-9"))
        assertEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorUpdateIdempotencyKey("form-9", "commit-1"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorUpdateIdempotencyKey("form-9", "commit-2"))
    }

    @Test
    fun `pipeline lead status lanes are keyed by the target lead`() {
        val firstTap = salesPipelineLeadGroupKey("lead-17")
        val secondTap = salesPipelineLeadGroupKey("lead-17")

        assertEquals(firstTap, secondTap)
        assertNotEquals(firstTap, salesPipelineGroupKey("client-random"))
    }
}
