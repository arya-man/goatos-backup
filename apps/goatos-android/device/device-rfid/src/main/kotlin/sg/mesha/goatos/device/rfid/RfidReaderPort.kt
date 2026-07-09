package sg.mesha.goatos.device.rfid

// Vendor RFID-reader SDK stays isolated behind this port (TRD: vendor SDKs live
// only in device-*). The fake lets features + previews run without hardware.

interface RfidReaderPort {
    fun startScan()
    fun stopScan()
}

class FakeRfidReader : RfidReaderPort {
    override fun startScan() {}
    override fun stopScan() {}
}
