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
 * The sync architecture and upload-failure journeys — the biggest repeat bugs in
 * this lane (proof/media and the offline sync queue between them account for most of
 * the 1051 Android commits since 1 August).
 *
 * Every journey here needs a signed-in session, which means a bearer token built
 * into the APK. That token is a secret and is not in this repo, so on a run without
 * one these report SKIPPED and the runner records them as NOT covered. They are
 * never reported as passed. The levers they use — taking the network away, killing
 * the app mid-flight, jumping to a route — are all real and are proven to work by
 * the cold-boot set, so the day a session is available these run as written.
 */
@LargeTest
@RunWith(AndroidJUnit4::class)
class GoatOsSyncJourneys {

    @Before
    fun startSignedIn() {
        GoatOsJourney.requireSeededSession()
        GoatOsJourney.setNetwork(connected = true)
        GoatOsJourney.coldStart()
    }

    @After
    fun leaveTheDeviceAsWeFoundIt() {
        GoatOsJourney.setNetwork(connected = true)
    }

    /**
     * offline-queue-survives-force-stop — the one the farm notices: a shed with no
     * signal, the app closed, and the day's work still there when the signal returns.
     */
    @Test
    fun workRecordedOfflineSurvivesAForceStop() {
        val name = "offline-queue-survives-force-stop"
        assertTrue(
            "The list of the day's jobs cannot be opened on the phone, so the work cannot be started.",
            GoatOsJourney.navigateTo("/work"),
        )

        GoatOsJourney.setNetwork(connected = false)
        assertTrue(
            "The phone does not say it is offline, so nobody knows their work is only on the handset.",
            GoatOsJourney.awaitText("offline"),
        )

        val queuedBefore = queuedCount()
        assertTrue(
            "Work recorded with no signal is not held on the phone at all.",
            queuedBefore > 0,
        )

        GoatOsJourney.forceStopApp()
        GoatOsJourney.launchApp()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, GoatOsJourney.visibleText())

        assertTrue(
            "Work recorded on the phone with no signal is lost when the app is closed, so the day's work never reaches the office.",
            queuedCount() >= queuedBefore,
        )

        GoatOsJourney.setNetwork(connected = true)
        assertTrue(
            "When the signal comes back the work on the phone is never sent to the office.",
            awaitAllSynced(),
        )
    }

    /** sync-status-screen — the screen must not claim everything is sent while it is not. */
    @Test
    fun theSyncSheetTellsTheTruthAboutWhatIsWaiting() {
        val name = "sync-status-screen"
        GoatOsJourney.setNetwork(connected = false)
        val waiting = queuedCount()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, GoatOsJourney.visibleText())

        assertFalse(
            "The sync screen says everything is sent while work is still sitting on the phone.",
            waiting > 0 && GoatOsJourney.awaitText("All synced", timeoutMs = 2_000L),
        )

        GoatOsJourney.setNetwork(connected = true)
        assertTrue(
            "Work sitting on the phone is never cleared, so the sync screen never settles.",
            awaitAllSynced(),
        )
    }

    /** upload-killed-mid-flight-resumes — a photo half-sent when Android killed the app. */
    @Test
    fun anUploadKilledHalfwayIsStillThereAfterwards() {
        val name = "upload-killed-mid-flight-resumes"
        val uploading = GoatOsJourney.awaitText("Uploading proof", timeoutMs = 20_000L)
        assertTrue(
            "The phone never starts sending the photo that proves the work was done.",
            uploading,
        )

        GoatOsJourney.forceStopApp()
        GoatOsJourney.launchApp()
        GoatOsJourney.captureEvidence(name)
        GoatOsJourney.recordSeen(name, GoatOsJourney.visibleText())

        assertTrue(
            "A photo that was half-uploaded when the app closed is lost, so the job goes in with no proof attached.",
            queuedCount() > 0 || GoatOsJourney.awaitText("Uploading proof", timeoutMs = 20_000L),
        )
        assertTrue(
            "The photo that proves the work was done never finishes being sent.",
            awaitAllSynced(),
        )
    }

    // --- reading the sync sheet, the only place the queue is visible to a person ---

    private fun queuedCount(): Int {
        val seen = GoatOsJourney.visibleText()
        return Regex("(\\d+)\\s+queued").find(seen)?.groupValues?.get(1)?.toIntOrNull()
            ?: Regex("Syncing\\s+(\\d+)").find(seen)?.groupValues?.get(1)?.toIntOrNull()
            ?: 0
    }

    private fun awaitAllSynced(): Boolean =
        GoatOsJourney.awaitText("All synced", timeoutMs = 60_000L)
}
