package sg.mesha.goatos.rfid

internal object RfidReaderNameMatcher {
    val DEFAULT_HINTS = listOf(
        "rfid",
        "reader",
        "idt",
        "rhls",
        "chainway",
        "r3",
        "uhf",
        "scanner",
        "ble scanner",
        "ble hid",
        "hid scanner",
    )

    fun matches(name: String?, hints: List<String> = DEFAULT_HINTS): Boolean {
        val lower = name?.lowercase() ?: return false
        return hints.any { lower.contains(it) }
    }
}
