package sg.mesha.goatos.core.data.sync

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

class VendorsPayloadsTest {
    @Test
    fun `a vendor edit shares its vendor lane but keys each saved commit apart`() {
        // Same target vendor -> same FIFO lane even when two form instances save while offline, so
        // whole-row replaces cannot race each other. Each saved edit commit still carries a different
        // idempotency key, so retained offline commits cannot collapse into the first queued edit.
        assertEquals(vendorUpdateGroupKey("vendor-9"), vendorUpdateGroupKey("vendor-9"))
        assertEquals(vendorUpdateGroupKey("vendor-9"), vendorUpdateGroupKey("vendor-9"))
        assertNotEquals(vendorUpdateGroupKey("vendor-9"), vendorCreateGroupKey("form-9"))
        assertNotEquals(vendorUpdateGroupKey("vendor-9"), vendorUpdateGroupKey("vendor-10"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorCreateIdempotencyKey("form-9"))
        assertEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorUpdateIdempotencyKey("form-9", "commit-1"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorUpdateIdempotencyKey("form-9", "commit-2"))
        assertNotEquals(vendorUpdateIdempotencyKey("form-9", "commit-1"), vendorUpdateIdempotencyKey("form-10", "commit-1"))
    }

    @Test
    fun `pipeline lead status lanes are keyed by the target lead`() {
        val firstTap = salesPipelineLeadGroupKey("lead-17")
        val secondTap = salesPipelineLeadGroupKey("lead-17")

        assertEquals(firstTap, secondTap)
        assertNotEquals(firstTap, salesPipelineGroupKey("client-random"))
    }
}
