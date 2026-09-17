package sg.mesha.goatos.feature.counts

import org.junit.Assert.assertEquals
import org.junit.Test

/** Realme E2E 2026-09-17: an approval answer row read "Why move these animals?: Space". */
class ApprovalAnswerLineTest {
    @Test
    fun `a question label keeps its own question mark and gets no extra colon`() {
        assertEquals("Why move these animals? Space", approvalAnswerLine("Why move these animals?", "Space"))
    }

    @Test
    fun `a label already ending in a colon is not doubled`() {
        assertEquals("Delivery type: Assisted", approvalAnswerLine("Delivery type:", "Assisted"))
    }

    @Test
    fun `a plain label is joined with a colon`() {
        assertEquals("Delivery type: Assisted", approvalAnswerLine("Delivery type", "Assisted"))
    }
}
