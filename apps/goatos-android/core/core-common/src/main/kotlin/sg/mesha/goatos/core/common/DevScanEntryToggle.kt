package sg.mesha.goatos.core.common

/**
 * Whether the DEV typed-tag entry is rendered on the BLE-only capture screens.
 *
 * Being on the dev FLAVOUR is not enough. A dev build is also what a maintainer installs to test
 * scanning with real tags and a real reader, and a stray "RFID tag (dev only)" box on that screen
 * is noise in exactly the flow they are trying to judge. So the field is off unless automation
 * explicitly asks for it:
 *
 *     adb shell setprop debug.goatos.scan_entry 1
 *
 * The property lives only in the device's runtime property store, is never set by the app, and is
 * absent on a fresh boot -- so the default is always off, and a release build cannot reach this at
 * all because the caller is already flavour-gated.
 */
object DevScanEntryToggle {
    private const val PROPERTY = "debug.goatos.scan_entry"

    fun isEnabled(): Boolean = runCatching {
        // exception:exempt reflective SystemProperties read; absent property means "off"
        val systemProperties = Class.forName("android.os.SystemProperties")
        val get = systemProperties.getMethod("get", String::class.java, String::class.java)
        (get.invoke(null, PROPERTY, "") as? String)?.trim() == "1"
    }.getOrDefault(false)
}
