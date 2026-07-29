package sg.mesha.goatos.rfid

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
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
    fun `same-name disconnected reader does not hide input device when another paired reader is connected`() {
        val pairedDevices = listOf(
            RfidReaderDevice(id = "bt-00:11", name = "IDT RHLS-3", detail = "Connected Bluetooth keyboard", signalLabel = "Ready"),
            RfidReaderDevice(id = "bt-22:33", name = "IDT RHLS-3", detail = "Paired not connected", signalLabel = "Disconnected"),
        )

        assertTrue(RfidReaderDeviceSelector.shouldSuppressDisconnectedNameFilter(pairedDevices))
    }
}
