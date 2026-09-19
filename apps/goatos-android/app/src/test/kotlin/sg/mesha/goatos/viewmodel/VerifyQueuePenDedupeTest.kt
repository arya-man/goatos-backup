package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The queue row appends the pen to the backend subject label unless the label already names it.
 * Weighing labels LEAD with the pen; death bundles, removal cards and pen moves END with it. The
 * original prefix-only check rendered "… · Mandela 1 - Part 3 · Mandela 1 - Part 3" on the Realme
 * (2026-09-19); this pins the segment-aware rule.
 */
class VerifyQueuePenDedupeTest {
    @Test
    fun `pen leading the label is recognised`() {
        assertTrue(subjectLabelNamesPen("Godel 2 - Part 1 · 412.5 kg · 38 goats", "Godel 2 - Part 1"))
    }

    @Test
    fun `pen ending the label is recognised (death bundle, removal card)`() {
        assertTrue(subjectLabelNamesPen("Death evidence · G-003071 · 19/09/2026 · Mandela 1 - Part 3", "Mandela 1 - Part 3"))
        assertTrue(subjectLabelNamesPen("Remove feed & water · Godel 2 - Part 1", "Godel 2 - Part 1"))
    }

    @Test
    fun `a pen that only shares a prefix with a segment is NOT a match`() {
        // "Mandela 1 - Part 3" must not swallow "Mandela 1 - Part 30", and a bare base name must
        // not match a partitioned segment.
        assertFalse(subjectLabelNamesPen("Death evidence · G-1 · Mandela 1 - Part 30", "Mandela 1 - Part 3"))
        assertFalse(subjectLabelNamesPen("Pen move · Gandhi 1 · from Gandhi 2 · 2 animals", "Castro 1"))
    }

    @Test
    fun `blank inputs never match`() {
        assertFalse(subjectLabelNamesPen(null, "Castro 1"))
        assertFalse(subjectLabelNamesPen("Castro 1 · 5 goats", ""))
    }
}
