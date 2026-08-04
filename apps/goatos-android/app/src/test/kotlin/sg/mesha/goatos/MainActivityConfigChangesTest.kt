package sg.mesha.goatos

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Guards the one manifest attribute that keeps an operator inside the scan screen while the
 * RFID reader flaps.
 *
 * The V1 reader is a Bluetooth HID KEYBOARD, so a connect or a disconnect changes
 * `Configuration.keyboard` / `hardKeyboardHidden` / `navigation`. An Activity that does not
 * declare those configs is destroyed and relaunched by the platform on every edge; that
 * relaunch tears down the Activity-scoped BootstrapViewModel and rebuilds the shell's NavHost
 * at the operator's landing route (the sheds list), throwing them out of a live scan session.
 * Observed on a POCO X5 Pro 5G (MIUI/HyperOS) on both edges, ~250-350 ms of frozen screen each.
 *
 * Asserted against the manifest source rather than a launched Activity because the failure is a
 * missing declaration, and a Robolectric Activity would happily run without it.
 */
class MainActivityConfigChangesTest {

    private val requiredConfigs = listOf("keyboard", "keyboardHidden", "navigation")

    @Test
    fun `MainActivity handles hardware-keyboard config changes in-process`() {
        val manifest = findManifest()
        assertNotNull(
            "Could not locate app/src/main/AndroidManifest.xml from ${File("").absolutePath}",
            manifest,
        )
        val activity = mainActivityElement(manifest!!.readText())
        assertNotNull("No <activity android:name=\".MainActivity\"> found in the manifest", activity)

        val configChanges = CONFIG_CHANGES.find(activity!!)?.groupValues?.get(1)
        assertNotNull(
            "MainActivity must declare android:configChanges — without it a Bluetooth HID RFID " +
                "reader connect/disconnect relaunches the Activity and pops the operator out of " +
                "the scan screen.",
            configChanges,
        )
        val declared = configChanges!!.split('|').map(String::trim).toSet()
        requiredConfigs.forEach { config ->
            assertTrue(
                "MainActivity android:configChanges is missing '$config' (declared: $configChanges). " +
                    "An RFID reader connect/disconnect would relaunch the Activity.",
                config in declared,
            )
        }
    }

    /** The Activity block, from its opening tag up to the first `>` that closes that tag. */
    private fun mainActivityElement(manifest: String): String? {
        val start = manifest.indexOf("<activity")
            .let { if (it < 0) return null else it }
        var cursor = start
        while (cursor >= 0) {
            val end = manifest.indexOf('>', cursor)
            if (end < 0) return null
            val element = manifest.substring(cursor, end)
            if (Regex("""android:name\s*=\s*"\.MainActivity"""").containsMatchIn(element)) return element
            cursor = manifest.indexOf("<activity", end)
        }
        return null
    }

    /** Walks up from the test's working directory so the test is independent of the Gradle CWD. */
    private fun findManifest(): File? {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            val candidate = File(dir, "app/src/main/AndroidManifest.xml")
            if (candidate.isFile) return candidate
            val self = File(dir, "src/main/AndroidManifest.xml")
            if (self.isFile) return self
            dir = dir.parentFile
        }
        return null
    }

    private companion object {
        val CONFIG_CHANGES = Regex("""android:configChanges\s*=\s*"([^"]*)"""")
    }
}
