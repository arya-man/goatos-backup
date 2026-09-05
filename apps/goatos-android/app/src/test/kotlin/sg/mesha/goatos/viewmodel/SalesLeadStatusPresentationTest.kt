package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Test

class SalesLeadStatusPresentationTest {
    @Test
    fun blankLeadStatusIsShownAsNotYetCalled() {
        assertEquals("Not yet called", leadStatusLabel(null))
        assertEquals("Not yet called", leadStatusLabel(""))
    }

    @Test
    fun storedLeadStatusIsRenderedVerbatim() {
        assertEquals("No Answer", leadStatusLabel("No Answer"))
    }
}
