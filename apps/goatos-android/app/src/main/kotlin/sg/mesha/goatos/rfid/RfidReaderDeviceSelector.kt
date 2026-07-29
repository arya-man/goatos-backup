package sg.mesha.goatos.rfid

internal object RfidReaderDeviceSelector {
    fun mergeVisibleDevices(
        inputDevices: List<RfidReaderDevice>,
        pairedDevices: List<RfidReaderDevice>,
    ): List<RfidReaderDevice> =
        (inputDevices + pairedDevices)
            .distinctBy { it.id }
            .sortedWith(compareByDescending<RfidReaderDevice> { it.signalLabel == "Ready" }.thenBy { it.name })

    fun disconnectedNameFilterBypassNames(pairedDevices: List<RfidReaderDevice>): Set<String> {
        val readyNames = pairedDevices
            .filter { it.signalLabel == "Ready" }
            .map { it.name.lowercase() }
            .toSet()
        val disconnectedNames = pairedDevices
            .filter { it.signalLabel == "Disconnected" }
            .map { it.name.lowercase() }
            .toSet()
        return readyNames intersect disconnectedNames
    }
}
