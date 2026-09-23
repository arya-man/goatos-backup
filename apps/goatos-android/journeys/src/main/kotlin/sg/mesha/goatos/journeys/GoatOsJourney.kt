package sg.mesha.goatos.journeys

import android.graphics.Point
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.uiautomator.By
import androidx.test.uiautomator.BySelector
import androidx.test.uiautomator.StaleObjectException
import androidx.test.uiautomator.UiDevice
import androidx.test.uiautomator.Until
import org.junit.Assume.assumeTrue
import java.io.File

/**
 * The shared harness for the lane 5 farm journeys.
 *
 * Everything here drives the phone the way a person or the operating system would:
 * launching the app, taking the network away, killing it, turning it sideways. There
 * is no back door into the app's own state, on purpose — a journey that can only be
 * proven by reading the app's internals is not a journey a farm manager can read.
 *
 * Shell commands go through UiAutomation, which runs as the `shell` user on the
 * device, so things an ordinary app may not do (airplane mode, force-stop) are
 * available without asking the app for a debug hook.
 */
internal object GoatOsJourney {

    /** devDebug: the flavour that carries the app's debug navigation + RFID receivers. */
    const val TARGET_PACKAGE = "sg.mesha.goatos.dev"

    /**
     * Where screenshots land, for Firebase Test Lab to pull with --directories-to-pull.
     *
     * NOT a bare /sdcard path. Under scoped storage (API 30+, and Test Lab's virtual
     * devices are 33) an app may only write inside its OWN external files directory
     * without MANAGE_EXTERNAL_STORAGE. Writing to /sdcard/<anything-else> throws, and
     * it would throw while capturing the evidence for a failure — losing exactly the
     * screenshot the farm manager is meant to see.
     */
    val evidenceDir: File by lazy {
        val dir = InstrumentationRegistry.getInstrumentation().context.getExternalFilesDir(null)
            ?: InstrumentationRegistry.getInstrumentation().context.filesDir
        dir.mkdirs()
        dir
    }

    const val UI_TIMEOUT_MS = 30_000L
    private const val SETTLE_MS = 1_500L

    val device: UiDevice
        get() = UiDevice.getInstance(InstrumentationRegistry.getInstrumentation())

    fun shell(command: String): String = device.executeShellCommand(command).trim()

    // --- the app -------------------------------------------------------------

    fun launchApp() {
        // `monkey -p <pkg> 1` is the launcher-intent equivalent that needs no
        // knowledge of the activity name, and it survives the activity being renamed.
        //
        // --pct-syskeys 0 is NOT a tuning knob, it is what makes this work at all on a
        // device with no hardware keys. monkey validates its event mix before it
        // launches anything, and the system-keys factor is non-zero by default; on a
        // device that reports no physical keys that check fails, monkey prints
        // "SYS_KEYS has no physical keys but with factor 2.0%" and exits -5 WITHOUT
        // EVER STARTING THE APP. Every virtual device is such a device — the local
        // emulator and Firebase Test Lab's virtual devices alike — so without this the
        // whole suite launches nothing, reads the launcher's home screen instead of the
        // app, and reports "the app closes itself as soon as it is opened" about an app
        // that never started. Zeroing the factor removes the events that fail the check.
        shell("monkey -p $TARGET_PACKAGE -c android.intent.category.LAUNCHER --pct-syskeys 0 1")
        device.wait(Until.hasObject(By.pkg(TARGET_PACKAGE).depth(0)), UI_TIMEOUT_MS)
        device.waitForIdle()
    }

    fun forceStopApp() {
        shell("am force-stop $TARGET_PACKAGE")
        device.waitForIdle()
    }

    fun coldStart() {
        forceStopApp()
        device.pressHome()
        launchApp()
    }

    /**
     * The app's own debug route jump (app/src/debug/.../DebugNavigationReceiver.kt).
     * Returns true only when the receiver reported that it actually navigated, so a
     * route the signed-in role is not allowed to see cannot be mistaken for a pass.
     */
    fun navigateTo(route: String): Boolean {
        val result = shell(
            "am broadcast -a sg.mesha.goatos.debug.NAVIGATE --es route $route -p $TARGET_PACKAGE",
        )
        device.waitForIdle()
        return Regex("result=(-?\\d+)").find(result)?.groupValues?.get(1) == "0"
    }

    /** The app's own RFID key-event injection (app/src/debug/.../DebugRfidInjectionReceiver.kt). */
    fun injectRfidTag(tag: String): Boolean {
        val result = shell(
            "am broadcast -a sg.mesha.goatos.debug.INJECT_RFID --es tag $tag -p $TARGET_PACKAGE",
        )
        device.waitForIdle()
        return Regex("result=(-?\\d+)").find(result)?.groupValues?.get(1) == "0"
    }

    // --- the world around the app -------------------------------------------

    /**
     * Takes the network away and gives it back, the way a shed with no signal does.
     * This is the only honest offline lever the app has today: it owns no
     * force-offline flag, so the network really is switched off underneath it.
     */
    fun setNetwork(connected: Boolean) {
        shell("cmd connectivity airplane-mode ${if (connected) "disable" else "enable"}")
        Thread.sleep(SETTLE_MS)
        device.waitForIdle()
    }

    fun rotateAndBack() {
        device.setOrientationLeft()
        Thread.sleep(SETTLE_MS)
        device.setOrientationNatural()
        Thread.sleep(SETTLE_MS)
        device.waitForIdle()
    }

    fun backgroundAndResume() {
        device.pressHome()
        Thread.sleep(SETTLE_MS)
        launchApp()
    }

    // --- reading the screen --------------------------------------------------

    fun awaitText(text: String, timeoutMs: Long = UI_TIMEOUT_MS): Boolean =
        device.wait(Until.hasObject(By.textContains(text)), timeoutMs)

    fun awaitDescription(description: String, timeoutMs: Long = UI_TIMEOUT_MS): Boolean =
        device.wait(Until.hasObject(By.desc(description)), timeoutMs)

    fun sees(selector: BySelector): Boolean = device.hasObject(selector)

    /**
     * One sweep of the screen's TextViews. [vanishedNodes] counts the nodes that
     * disappeared between being found and being read — an ordinary race on a live
     * phone, and not something the app did wrong.
     */
    data class ScreenText(val text: String, val vanishedNodes: Int)

    /**
     * Reads every TextView on screen.
     *
     * A node that goes stale between findObjects() and the read is skipped and
     * counted: the screen moved on underneath us, which is normal while an app is
     * still drawing. Anything else thrown is a real fault and is left to propagate.
     * Swallowing it here would turn a broken harness into an apparently empty
     * screen and accuse the app of a blank-screen bug it does not have.
     */
    fun readScreen(): ScreenText {
        var vanished = 0
        val texts = device.findObjects(By.clazz("android.widget.TextView"))
            .mapNotNull { node ->
                try {
                    node.text
                } catch (gone: StaleObjectException) {
                    vanished += 1
                    null
                }
            }
        return ScreenText(texts.joinToString(" | "), vanished)
    }

    fun visibleText(): String = readScreen().text

    /**
     * Something crashed or died: Android's own "keeps stopping" dialog, or the app
     * simply not being on top any more. Checked at the end of every journey, because
     * "the app closed itself while someone was recording work" is the single failure
     * the farm notices most.
     */
    fun crashedOrDisappeared(): Boolean {
        val crashDialog = device.hasObject(By.textContains("keeps stopping")) ||
            device.hasObject(By.textContains("has stopped")) ||
            device.hasObject(By.textContains("isn't responding"))
        return crashDialog || !device.hasObject(By.pkg(TARGET_PACKAGE).depth(0))
    }

    /**
     * A screen with nothing on it is a failure a person sees, and it is the exact
     * shape of "the app showed empty screens instead of telling them to update".
     */
    fun screenIsBlank(): Boolean {
        val screen = readScreen()
        return screen.text.isBlank() && screen.vanishedNodes == 0
    }

    // --- evidence ------------------------------------------------------------

    /**
     * One screenshot per journey, named after the journey, so the Slack reply can
     * carry the phone screen the farm manager is being told about.
     */
    fun captureEvidence(journeyName: String): File {
        val file = File(evidenceDir, "$journeyName.png")
        device.takeScreenshot(file)
        return file
    }

    /**
     * Records, beside the screenshot, what was on the screen when the journey ended.
     * The runner reads this back so a failure can be described without anybody
     * opening a log.
     */
    fun recordSeen(journeyName: String, note: String) {
        File(evidenceDir, "$journeyName.seen.txt").writeText(note)
    }

    // --- honesty -------------------------------------------------------------

    /**
     * A journey past the sign-in screen needs a session built into the APK
     * (`-PgoatosDevBearerToken=…`, which is a secret and lives nowhere in this repo).
     * Without one the journey reports SKIPPED. It must never report passed: a green
     * that only means "we could not try" is the thing this lane exists to prevent.
     */
    /**
     * The app's OWN "the startup call failed" screen
     * (app/src/main/res/values/strings.xml → bootstrap_error_connectivity + the Retry
     * beside it), which is what it shows when its boot-time call to the backend does
     * not answer.
     *
     * Matching the app's real string, not a guess: "connection" on its own also appears
     * in the sign-in screen's own network error, and telling those two apart is the
     * whole point of this check.
     */
    fun stoppedAtStartupCheck(): Boolean =
        device.hasObject(By.textContains("Couldn't reach the server")) &&
            device.hasObject(By.textContains("Retry"))

    /**
     * Learned on the first real run of this suite, on a local emulator.
     *
     * The app does not open on its sign-in screen. It makes a call to its backend
     * FIRST, and while that call is failing it shows its own "Couldn't reach the
     * server … Retry" screen and nothing else. So on a device with no reachable
     * backend the sign-in screen is not merely unsigned-in, it is UNREACHABLE — and
     * every journey whose subject is that screen has not been tried at all.
     *
     * That has to be a SKIP, and it has to be a skip for a stated reason:
     *  * reporting it as a PASS is the false green this lane exists to prevent — it
     *    is exactly what happened before this check existed, when a journey walked
     *    the startup-error screen and reported the sign-in screen as covered;
     *  * reporting it as a FAILURE blames the app for a backend that was never
     *    started, which is the mirror-image dishonesty the runner's own
     *    `reachedADevice` was added to stop.
     *
     * A crash, a dead app or a blank screen is NOT this. Those are still failures and
     * are still asserted before this is ever reached.
     */
    fun requirePastStartupCheck(journeyName: String) {
        val stopped = stoppedAtStartupCheck()
        if (stopped) {
            // Evidence FIRST, then the skip. A skip with nothing behind it is a claim
            // the reader has to take on trust; the screenshot and the screen's own words
            // are what let someone see that the app was stuck on its startup screen
            // without opening a log.
            captureEvidence(journeyName)
            recordSeen(journeyName, visibleText())
        }
        assumeTrue(
            "SKIPPED — the app never got past its own startup check because no backend answered, " +
                "so the screen this journey is about was never reached and is not covered by this run.",
            !stopped,
        )
    }

    fun requireSeededSession() {
        val seeded = InstrumentationRegistry.getArguments().getString("goatosSessionSeeded") == "true"
        assumeTrue(
            "SKIPPED — no signed-in session was built into this APK, so this journey was not attempted. " +
                "It is not covered by this run.",
            seeded,
        )
    }

    fun displaySize(): Point = Point(device.displayWidth, device.displayHeight)
}
