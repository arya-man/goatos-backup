package sg.mesha.goatos.journeys

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.filters.LargeTest
import org.junit.After
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

/**
 * What a Firebase Test Lab VIRTUAL device can prove today with no secret and no
 * signed-in session: a phone that has just had the app installed.
 *
 * TWO of these are catalogue journeys and are counted as coverage, each of only the
 * half of its check named in the catalogue's `automation.coversPartially`. The other
 * two are harness proofs that claim no check at all — they exist so the levers the
 * sync journeys rely on are known to work. A test is counted as coverage only when
 * the catalogue names it, and the catalogue names these two and no others.
 *
 * Every assertion message in this file is the sentence a farm manager reads in
 * Slack. No class names, no selectors, no check codes — the catalogue entry's
 * `humanFailure` and nothing else.
 *
 * Catalogued in tools/dashboard-automation/android-journeys.json; each journey's
 * `automation.note` says exactly which half of its check this proves and which half
 * still needs a session.
 */
@LargeTest
@RunWith(AndroidJUnit4::class)
class GoatOsColdBootJourneys {

    @Before
    fun startFromAClosedApp() {
        GoatOsJourney.setNetwork(connected = true)
        GoatOsJourney.coldStart()
    }

    @After
    fun leaveTheDeviceAsWeFoundIt() {
        GoatOsJourney.setNetwork(connected = true)
    }

    /** login-and-session (fresh-install half) + app-update-gate (not-blank half). */
    @Test
    fun theAppOpensToSignInInsteadOfABlankScreen() {
        val name = "login-and-session"
        val reachedSignIn = GoatOsJourney.awaitText("Sign in")
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, GoatOsJourney.visibleText())

        assertFalse(
            "The app closes itself as soon as it is opened, so nobody can start their work.",
            GoatOsJourney.crashedOrDisappeared(),
        )
        assertFalse(
            "The app opens on an empty screen instead of asking the person to sign in.",
            GoatOsJourney.screenIsBlank(),
        )
        // The app dying or going blank is asserted ABOVE, because those are the app's
        // fault whatever the backend is doing. Only past that does "no server answered"
        // become the reason the sign-in screen was never reached.
        GoatOsJourney.requirePastStartupCheck(name)
        assertTrue(
            "The app does not offer a way to sign in when it is opened, so nobody can get in.",
            reachedSignIn,
        )

        // Signed out, closed, reopened: still signed out and still usable.
        GoatOsJourney.forceStopApp()
        GoatOsJourney.launchApp()
        assertTrue(
            "After the app is closed and reopened it no longer offers a way to sign in.",
            GoatOsJourney.awaitText("Sign in"),
        )
    }

    /**
     * NOT a journey. This proves the sign-in screen is readable at all, which every
     * journey above depends on. It deliberately claims NO catalogue check: it asserts
     * the ENGLISH strings are present, and the `language-switch` check is about Hindi,
     * Kannada and Telugu — a correctly translated build would fail this, so calling it
     * coverage of that check would be a green that means the opposite of what it says.
     */
    @Test
    fun theSignInScreenIsReadableInEnglish() {
        val name = "sign-in-readable-en"
        GoatOsJourney.awaitText("Sign in")
        GoatOsJourney.requirePastStartupCheck(name)
        val seen = GoatOsJourney.visibleText()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, seen)

        for (expected in listOf("Work email", "Password", "Sign in")) {
            assertTrue(
                "The sign-in screen does not say what the person is meant to type, so they cannot get in.",
                seen.contains(expected, ignoreCase = true),
            )
        }
        // Untranslated placeholder keys are the exact shape of "the app is unreadable
        // for staff who do not read English".
        assertFalse(
            "The sign-in screen shows internal wording instead of real words.",
            Regex("\\b[a-z]+_[a-z_]{3,}\\b").containsMatchIn(seen),
        )
    }

    /**
     * NOT a journey either. This is the proof that the OFFLINE LEVER works — that
     * taking the network away over the instrumentation shell really does reach the
     * app — which every sync journey depends on and which would otherwise only be
     * discovered the day a session exists. It claims no catalogue check. It is in
     * particular NOT the `app-update-gate` check: nothing here touches an
     * out-of-date build, and the app skips its own update gate on debug builds.
     */
    @Test
    fun takingTheNetworkAwayReachesTheApp() {
        val name = "offline-lever"
        GoatOsJourney.awaitText("Sign in")
        // Without this the proof is vacuous, and on the first real run it was: the app
        // was sitting on its own "Couldn't reach the server. Check your connection and
        // try again." startup screen the whole time, so the "connection" this test
        // looks for below was ALREADY on the screen before the network was touched.
        // It reported the offline lever proven while never exercising it.
        GoatOsJourney.requirePastStartupCheck(name)
        // What the screen said BEFORE the network was taken away. A message that was
        // already there proves nothing about taking the network away.
        val messageBefore = networkMessageOnScreen()
        GoatOsJourney.setNetwork(connected = false)

        val signIn = androidx.test.uiautomator.By.text("Sign in")
        if (GoatOsJourney.sees(signIn)) GoatOsJourney.device.findObject(signIn).click()
        GoatOsJourney.device.waitForIdle()
        val sawNetworkMessage = GoatOsJourney.awaitText("connection", timeoutMs = 20_000L) ||
            GoatOsJourney.awaitText("offline", timeoutMs = 1_000L) ||
            GoatOsJourney.awaitText("Network", timeoutMs = 1_000L)

        val seen = GoatOsJourney.visibleText()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, seen)
        GoatOsJourney.setNetwork(connected = true)

        assertFalse(
            "The app closes itself when the phone has no signal.",
            GoatOsJourney.crashedOrDisappeared(),
        )
        assertFalse(
            "With no signal the app goes blank instead of saying what is wrong.",
            GoatOsJourney.screenIsBlank(),
        )
        assertTrue(
            "With no signal the app gives no reason why signing in did not work, so the person just tries again.",
            sawNetworkMessage && !messageBefore,
        )
    }

    /** The app telling someone the network is why nothing happened. */
    private fun networkMessageOnScreen(): Boolean {
        val seen = GoatOsJourney.visibleText()
        return listOf("connection", "offline", "Network").any { seen.contains(it, ignoreCase = true) }
    }

    /** crash-free-on-the-covered-screens (the screens reachable without a session). */
    @Test
    fun theAppSurvivesForceStopRotationAndResume() {
        val name = "crash-free-on-the-covered-screens"
        GoatOsJourney.awaitText("Sign in")
        // This check's catalogue entry claims "the screens reachable without signing in:
        // the sign-in screen through rotate, background, resume and force-stop". On the
        // first real run it passed without ever seeing that screen — it rotated the
        // app's startup-error screen instead and reported the sign-in screen covered.
        // Surviving a rotation is worth knowing, but it is not this check, and a pass
        // here is read as this check.
        GoatOsJourney.requirePastStartupCheck(name)

        GoatOsJourney.rotateAndBack()
        assertFalse(
            "Turning the phone sideways closes the app.",
            GoatOsJourney.crashedOrDisappeared(),
        )

        GoatOsJourney.backgroundAndResume()
        assertFalse(
            "Coming back to the app after using another one closes it.",
            GoatOsJourney.crashedOrDisappeared(),
        )

        GoatOsJourney.forceStopApp()
        GoatOsJourney.launchApp()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, GoatOsJourney.visibleText())

        assertFalse(
            "The app closes itself or shows an error page while someone is trying to record work.",
            GoatOsJourney.crashedOrDisappeared(),
        )
        assertFalse(
            "The app comes back to an empty screen after being closed.",
            GoatOsJourney.screenIsBlank(),
        )
    }
}
