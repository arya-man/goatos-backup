package sg.mesha.goatos.rfid

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class RfidReaderNameMatcherTest {
    @Test
    fun `matches known reader and scanner aliases exposed by Android bluetooth`() {
        listOf(
            "IDT RHLS-3",
            "Chainway R3",
            "UHF Scanner",
            "BLE Scanner",
            "BLE HID Keyboard",
            "RFID Reader",
        ).forEach { name ->
            assertTrue("Expected $name to match RFID reader hints", RfidReaderNameMatcher.matches(name))
        }
    }

    @Test
    fun `does not match unrelated bluetooth device names`() {
        listOf(
            "Mesha Phone",
            "Goat OS Speaker",
            "Wireless Headset",
            "Kitchen Scale",
        ).forEach { name ->
            assertFalse("Expected $name to stay out of RFID reader hints", RfidReaderNameMatcher.matches(name))
        }
    }
}
