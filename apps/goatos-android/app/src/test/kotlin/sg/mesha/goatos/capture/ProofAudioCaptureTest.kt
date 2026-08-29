package sg.mesha.goatos.capture

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.io.File

/**
 * Pins the three facts that make proof-video audio COMPULSORY on the in-app recorder.
 *
 * Until 2026-08-08 every in-app clip was silent — not muted, no audio track in the container at
 * all. Real STG proofs pulled from `gs://goatos-stg-media` showed 344 in-app captures with zero
 * `soun`/`mp4a` boxes, while the 17 gallery-imported ones carried AAC because the phone's own
 * camera app had recorded them. The cause was a single missing CameraX call plus a comment
 * asserting the absence was intentional, so nothing in the tree contradicted it.
 *
 * Three independent things have to hold, and each is asserted separately because any one of them
 * silently reverts the whole feature:
 *
 *  1. the merged manifest declares RECORD_AUDIO — without it the runtime request is a no-op;
 *  2. RECORD_AUDIO is in the BLOCKING mandatory capture set — that is what makes audio
 *     compulsory rather than best-effort, since the gate never composes capture content while a
 *     required permission is denied;
 *  3. the recorder actually calls `withAudioEnabled()` — CameraX defaults to no audio track, so
 *     the permission alone changes nothing.
 *
 * Deliberately NOT asserted here: that a recorded file contains an audio track. A JVM test cannot
 * drive CameraX, and treating a configuration assertion as proof of a recorded file is exactly
 * the gap that let the silent proofs ship. Device proof (record a clip, pull it, count `soun` /
 * `mp4a` boxes) is the closure evidence and lives in the change's handoff notes.
 *
 * Scope note: the gallery picker is untouched. Audio on a gallery-imported video is OPTIONAL
 * (maintainer decision 2026-08-08) and nothing on the upload path validates for it.
 */
@RunWith(RobolectricTestRunner::class)
class ProofAudioCaptureTest {

    @Test
    fun `merged manifest declares the microphone permission`() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        val declared = context.packageManager
            .getPackageInfo(context.packageName, PackageManager.GET_PERMISSIONS)
            .requestedPermissions
            ?.toSet()
            .orEmpty()

        assertTrue(
            "The merged manifest must declare ${Manifest.permission.RECORD_AUDIO}; without it " +
                "the runtime microphone request is a silent no-op and every in-app proof clip " +
                "records with no audio track. Declared: $declared",
            Manifest.permission.RECORD_AUDIO in declared,
        )
    }

    @Test
    fun `the microphone is in the blocking mandatory capture permission set`() {
        assertTrue(
            "RECORD_AUDIO must sit in MANDATORY_CAPTURE_PERMISSIONS. That list is what makes " +
                "audio compulsory: CaptureAccessGate never composes capture content while any " +
                "entry is denied. Dropping it back out would leave the recorder asking for a " +
                "permission nobody ever grants, and CameraX would throw at record time. " +
                "Current set: $MANDATORY_CAPTURE_PERMISSIONS",
            Manifest.permission.RECORD_AUDIO in MANDATORY_CAPTURE_PERMISSIONS,
        )
    }

    @Test
    fun `android 10 capture gate requires location for bluetooth but never notifications`() {
        assertEquals(
            listOf(
                Manifest.permission.CAMERA,
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.ACCESS_FINE_LOCATION,
            ),
            mandatoryCapturePermissionsForSdk(29),
        )
    }

    @Test
    fun `android 12 capture gate requires bluetooth connect but never notifications`() {
        assertEquals(
            listOf(
                Manifest.permission.CAMERA,
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.ACCESS_FINE_LOCATION,
                Manifest.permission.BLUETOOTH_CONNECT,
                Manifest.permission.BLUETOOTH_SCAN,
            ),
            mandatoryCapturePermissionsForSdk(31),
        )
    }

    @Test
    fun `android 12L capture gate still never requires notifications`() {
        assertEquals(
            listOf(
                Manifest.permission.CAMERA,
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.ACCESS_FINE_LOCATION,
                Manifest.permission.BLUETOOTH_CONNECT,
                Manifest.permission.BLUETOOTH_SCAN,
            ),
            mandatoryCapturePermissionsForSdk(32),
        )
    }

    @Test
    fun `android 13 capture gate adds runtime notifications`() {
        assertEquals(
            listOf(
                Manifest.permission.CAMERA,
                Manifest.permission.RECORD_AUDIO,
                Manifest.permission.ACCESS_FINE_LOCATION,
                Manifest.permission.POST_NOTIFICATIONS,
                Manifest.permission.BLUETOOTH_CONNECT,
                Manifest.permission.BLUETOOTH_SCAN,
            ),
            mandatoryCapturePermissionsForSdk(33),
        )
    }

    @Test
    fun `the microphone row reads as farm language, not a platform constant`() {
        assertEquals(
            "The permission gate renders this label to a field operator, so it must be farm " +
                "language (copy firewall) — never the raw RECORD_AUDIO constant.",
            "Microphone",
            permissionLabel(Manifest.permission.RECORD_AUDIO),
        )
    }

    @Test
    fun `precise location request includes coarse without making coarse a blocker`() {
        assertEquals(
            listOf(
                Manifest.permission.ACCESS_COARSE_LOCATION,
                Manifest.permission.ACCESS_FINE_LOCATION,
            ),
            requestPermissionsFor(setOf(Manifest.permission.ACCESS_FINE_LOCATION)),
        )
        assertFalse(
            "Coarse must not be a separately blocking capture permission. If precise location " +
                "is already granted after an update, a false coarse grant must not keep asking.",
            Manifest.permission.ACCESS_COARSE_LOCATION in mandatoryCapturePermissionsForSdk(33),
        )
    }

    @Test
    fun `the in-app recorder enables audio on the recording it prepares`() {
        val source = findRecorderSource()
        assertNotNull(
            "Could not locate InAppVideoRecorder.kt from ${File("").absolutePath}",
            source,
        )
        val text = source!!.readText()

        assertTrue(
            "InAppVideoRecorder must call withAudioEnabled() on the PendingRecording. CameraX " +
                "defaults to NO audio track, so holding RECORD_AUDIO changes nothing on its own " +
                "— this one call is the difference between a proof a verifier can hear and the " +
                "344 silent clips already in STG.",
            RECORDING_WITH_AUDIO.containsMatchIn(text),
        )
        assertFalse(
            "InAppVideoRecorder still carries the old 'No audio' comment. A comment that " +
                "contradicts the code is how the next reader concludes silence was intentional " +
                "— which is precisely what happened here.",
            text.contains("No audio"),
        )
    }

    @Test
    fun `the in-app recorder does not auto-start a second clip while finalizing validation`() {
        val source = findRecorderSource()
        assertNotNull(
            "Could not locate InAppVideoRecorder.kt from ${File("").absolutePath}",
            source,
        )
        val text = source!!.readText()

        assertTrue(
            "A finalized clip sets isRecording=false before off-main validation completes. The " +
                "preview-streaming auto-start gate must include pendingValidation, otherwise it " +
                "can start a second recording and overwrite startedAtMs before the first clip is " +
                "delivered, producing 0:00 proof durations.",
            FINALIZING_BLOCKS_AUTO_START.containsMatchIn(text),
        )
        assertTrue(
            "startRecording itself must also refuse to run while a previous clip is validating, " +
                "so retrying/recomposition cannot create a second active file before delivery.",
            text.contains("if (pendingValidation != null) return"),
        )
    }

    /** Walks up from the test's working directory so the test is independent of the Gradle CWD. */
    private fun findRecorderSource(): File? {
        var dir: File? = File("").absoluteFile
        while (dir != null) {
            val candidate = File(dir, "app/src/main/kotlin/$RECORDER_PATH")
            if (candidate.isFile) return candidate
            val self = File(dir, "src/main/kotlin/$RECORDER_PATH")
            if (self.isFile) return self
            dir = dir.parentFile
        }
        return null
    }

    private companion object {
        const val RECORDER_PATH = "sg/mesha/goatos/capture/InAppVideoRecorder.kt"

        /**
         * `prepareRecording(...)` then `withAudioEnabled()`, tolerating the comment lines that sit
         * between them today. Anchored to `prepareRecording` rather than matching
         * `withAudioEnabled` anywhere in the file so a mention inside a comment cannot satisfy it.
         */
        val RECORDING_WITH_AUDIO = Regex(
            """prepareRecording\s*\([^)]*\)(?:\s*//[^\n]*\n)*\s*\.withAudioEnabled\s*\(\s*\)""",
        )
        val FINALIZING_BLOCKS_AUTO_START = Regex(
            """LaunchedEffect\s*\(\s*previewStreaming\s*,\s*isRecording\s*,\s*pendingValidation\s*,\s*resultDelivered\s*\)\s*\{\s*if\s*\(\s*previewStreaming\s*&&\s*!isRecording\s*&&\s*pendingValidation\s*==\s*null\s*&&\s*!resultDelivered\s*\)""",
            RegexOption.DOT_MATCHES_ALL,
        )
    }
}
