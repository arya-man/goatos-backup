package sg.mesha.goatos.rfid

import org.junit.Assert.assertEquals
import org.junit.Test

class RfidReaderDeviceSelectorTest {
    @Test
    fun `keeps two paired readers with the same bluetooth name separate`() {
        val devices = RfidReaderDeviceSelector.mergeVisibleDevices(
            inputDevices = emptyList(),
            pairedDevices = listOf(
                RfidReaderDevice(id = "bt-00:11", name = "IDT RHLS-3", detail = "Connected Bluetooth keyboard", signalLabel = "Ready"),
                RfidReaderDevice(id = "bt-22:33", name = "IDT RHLS-3", detail = "Paired not connected", signalLabel = "Disconnected"),
            ),
        )

        assertEquals(2, devices.size)
        assertEquals(listOf("bt-00:11", "bt-22:33"), devices.map { it.id })
    }

    @Test
    fun `same-name disconnected reader bypasses stale input filter when matching paired reader is ready`() {
        val pairedDevices = listOf(
            RfidReaderDevice(id = "bt-00:11", name = "IDT RHLS-3", detail = "Connected Bluetooth keyboard", signalLabel = "Ready"),
            RfidReaderDevice(id = "bt-22:33", name = "IDT RHLS-3", detail = "Paired not connected", signalLabel = "Disconnected"),
        )

        assertEquals(
            setOf("idt rhls-3"),
            RfidReaderDeviceSelector.disconnectedNameFilterBypassNames(pairedDevices),
        )
    }

    @Test
    fun `different-name ready reader does not bypass disconnected stale input filter`() {
        val pairedDevices = listOf(
            RfidReaderDevice(id = "bt-00:11", name = "IDT RHLS-3", detail = "Connected Bluetooth keyboard", signalLabel = "Ready"),
            RfidReaderDevice(id = "bt-22:33", name = "Chainway R3", detail = "Paired not connected", signalLabel = "Disconnected"),
        )

        assertEquals(
            emptySet<String>(),
            RfidReaderDeviceSelector.disconnectedNameFilterBypassNames(pairedDevices),
        )
    }
}
