package sg.mesha.goatos.rfid

internal object RfidReaderDeviceSelector {
    fun mergeVisibleDevices(
        inputDevices: List<RfidReaderDevice>,
        pairedDevices: List<RfidReaderDevice>,
    ): List<RfidReaderDevice> =
        (inputDevices + pairedDevices)
            .distinctBy { it.id }
            .sortedWith(compareByDescending<RfidReaderDevice> { it.signalLabel == "Ready" }.thenBy { it.name })

    fun shouldSuppressDisconnectedNameFilter(pairedDevices: List<RfidReaderDevice>): Boolean =
        pairedDevices.any { it.signalLabel == "Ready" }
}
