package sg.mesha.goatos.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class ScanDisplayTitleTest {
    @Test
    fun `whole partition is not rendered as Part whole`() {
        assertEquals(
            "Old Yashoda",
            scanDisplayTitle(name = "Old Yashoda", physicalShed = "", partition = "whole"),
        )
    }

    @Test
    fun `numbered partition is rendered as part suffix`() {
        assertEquals(
            "Godel 1 - Part 3",
            scanDisplayTitle(name = "Godel 1", physicalShed = "", partition = "3"),
        )
    }
}
